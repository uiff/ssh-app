// Package sshx baut SSH-Verbindungen auf: Anmeldung mit Passwort oder
// Schluessel, Jump-Host, und Pruefung des Host-Schluessels ueber einen
// Rueckruf, der den Benutzer fragen kann.
package sshx

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Anmeldung sind die entschluesselten Zugangsdaten fuer genau einen Verbindungsaufbau.
type Anmeldung struct {
	Benutzer   string
	Passwort   string
	Schluessel []byte // PEM / OpenSSH
	Passphrase string
}

// Ziel beschreibt einen Hop.
type Ziel struct {
	Adresse   string
	Port      int
	Anmeldung Anmeldung
}

func (z Ziel) HostPort() string {
	p := z.Port
	if p == 0 {
		p = 22
	}
	return net.JoinHostPort(strings.TrimSpace(z.Adresse), strconv.Itoa(p))
}

// HostKeyPruefer entscheidet ueber einen Host-Schluessel. ziel = host:port.
type HostKeyPruefer func(ziel string, key ssh.PublicKey) error

// Fehlerarten, die die Oberflaeche verstaendlich anzeigt.
var (
	ErrAnmeldung  = errors.New("Anmeldung abgelehnt — Benutzer, Passwort oder Schlüssel prüfen")
	ErrPassphrase = errors.New("Der Schlüssel ist mit einer Passphrase geschützt — im Zugang hinterlegen")
)

func authMethoden(a Anmeldung) ([]ssh.AuthMethod, error) {
	var m []ssh.AuthMethod
	if len(a.Schluessel) > 0 {
		var signer ssh.Signer
		var err error
		if a.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(a.Schluessel, []byte(a.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(a.Schluessel)
		}
		var pp *ssh.PassphraseMissingError
		if errors.As(err, &pp) {
			return nil, ErrPassphrase
		}
		if err != nil {
			return nil, fmt.Errorf("Schlüssel lesen: %w", err)
		}
		m = append(m, ssh.PublicKeys(signer))
	}
	if a.Passwort != "" {
		pw := a.Passwort
		m = append(m, ssh.Password(pw), ssh.KeyboardInteractive(func(_, _ string, fragen []string, _ []bool) ([]string, error) {
			antworten := make([]string, len(fragen))
			for i := range fragen {
				antworten[i] = pw
			}
			return antworten, nil
		}))
	}
	if len(m) == 0 {
		return nil, errors.New("Kein Passwort und kein Schlüssel hinterlegt")
	}
	return m, nil
}

func clientConfig(z Ziel, pruefer HostKeyPruefer) (*ssh.ClientConfig, error) {
	m, err := authMethoden(z.Anmeldung)
	if err != nil {
		return nil, err
	}
	hp := z.HostPort()
	return &ssh.ClientConfig{
		User: z.Anmeldung.Benutzer,
		Auth: m,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			return pruefer(hp, key)
		},
		Timeout: 15 * time.Second,
	}, nil
}

// Verbinden baut die Verbindung auf; ist jump gesetzt, laeuft sie ueber diesen Host.
// Zurueck kommen der Ziel-Client und eine Schliessfunktion fuer alle Hops.
func Verbinden(ctx context.Context, ziel Ziel, jump *Ziel, pruefer HostKeyPruefer) (*ssh.Client, func(), error) {
	var jc *ssh.Client
	dial := func(addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", addr)
	}
	if jump != nil {
		cfg, err := clientConfig(*jump, pruefer)
		if err != nil {
			return nil, nil, fmt.Errorf("Jump-Host: %w", err)
		}
		conn, err := dial(jump.HostPort())
		if err != nil {
			return nil, nil, fmt.Errorf("Jump-Host nicht erreichbar: %w", err)
		}
		c, ch, rq, err := handshake(ctx, conn, jump.HostPort(), cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("Jump-Host: %w", err)
		}
		jc = ssh.NewClient(c, ch, rq)
		dial = func(addr string) (net.Conn, error) { return jc.DialContext(ctx, "tcp", addr) }
	}
	schliessen := func() {
		if jc != nil {
			jc.Close()
		}
	}
	cfg, err := clientConfig(ziel, pruefer)
	if err != nil {
		schliessen()
		return nil, nil, err
	}
	conn, err := dial(ziel.HostPort())
	if err != nil {
		schliessen()
		return nil, nil, fmt.Errorf("%s nicht erreichbar: %w", ziel.HostPort(), err)
	}
	c, ch, rq, err := handshake(ctx, conn, ziel.HostPort(), cfg)
	if err != nil {
		schliessen()
		return nil, nil, err
	}
	client := ssh.NewClient(c, ch, rq)
	return client, func() { client.Close(); schliessen() }, nil
}

// handshake fuehrt den SSH-Handshake mit Abbruch ueber ctx durch.
func handshake(ctx context.Context, conn net.Conn, addr string, cfg *ssh.ClientConfig) (ssh.Conn, <-chan ssh.NewChannel, <-chan *ssh.Request, error) {
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	c, ch, rq, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		if strings.Contains(err.Error(), "unable to authenticate") {
			return nil, nil, nil, ErrAnmeldung
		}
		return nil, nil, nil, err
	}
	return c, ch, rq, nil
}

// SchluesselInfo liefert Typ, oeffentlichen Teil (authorized_keys-Zeile) und Fingerprint.
func SchluesselInfo(pemDaten []byte, passphrase, kommentar string) (typ, oeffentlich, fp string, err error) {
	var signer ssh.Signer
	if passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(pemDaten, []byte(passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey(pemDaten)
	}
	var pp *ssh.PassphraseMissingError
	if errors.As(err, &pp) {
		return "", "", "", ErrPassphrase
	}
	if err != nil {
		return "", "", "", fmt.Errorf("Das ist kein lesbarer privater Schlüssel: %w", err)
	}
	pub := signer.PublicKey()
	zeile := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	if kommentar != "" {
		zeile += " " + kommentar
	}
	return pub.Type(), zeile, ssh.FingerprintSHA256(pub), nil
}

// NeuerSchluessel erzeugt ein ed25519-Paar im OpenSSH-Format.
func NeuerSchluessel(kommentar string) (privPEM []byte, err error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	blk, err := ssh.MarshalPrivateKey(priv, kommentar)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(blk), nil
}

// AuthorizedKeysBefehl haengt einen oeffentlichen Schluessel an, falls er fehlt.
func AuthorizedKeysBefehl(oeffentlich string) (string, error) {
	pub, kommentar, _, _, err := ssh.ParseAuthorizedKey([]byte(oeffentlich))
	if err != nil {
		return "", fmt.Errorf("öffentlicher Schlüssel ungültig: %w", err)
	}
	zeile := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	if kommentar != "" && !strings.ContainsAny(kommentar, `'"\$`+"`") {
		zeile += " " + kommentar
	}
	// zeile besteht nur aus Typ, base64 und gefiltertem Kommentar: sicher in einfachen Anfuehrungszeichen.
	q := "'" + zeile + "'"
	return "umask 077; mkdir -p ~/.ssh && touch ~/.ssh/authorized_keys && " +
		"(grep -qxF " + q + " ~/.ssh/authorized_keys || echo " + q + " >> ~/.ssh/authorized_keys) && echo SSHAPP-OK", nil
}

// Ausfuehren startet einen Befehl ohne Terminal und liefert die Ausgabe.
func Ausfuehren(c *ssh.Client, befehl string) (string, error) {
	s, err := c.NewSession()
	if err != nil {
		return "", err
	}
	defer s.Close()
	out, err := s.CombinedOutput(befehl)
	return string(out), err
}
