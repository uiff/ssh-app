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
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"syscall"
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

// Host ist die Adresse ohne Port (fuer Meldungen).
func (z Ziel) Host() string { return strings.TrimSpace(z.Adresse) }

// HostKeyPruefer entscheidet ueber einen Host-Schluessel. ziel = host:port.
type HostKeyPruefer func(ziel string, key ssh.PublicKey) error

// Fehlerarten, die die Oberflaeche verstaendlich anzeigt.
var (
	ErrAnmeldung   = errors.New("Anmeldung abgelehnt — Benutzer, Passwort oder Schlüssel prüfen")
	ErrPassphrase  = errors.New("Der Schlüssel ist mit einer Passphrase geschützt — im Zugang hinterlegen")
	ErrAbgebrochen = errors.New("Verbindungsaufbau abgebrochen")
)

// Fristen fuer den Verbindungsaufbau. Die Wartezeit auf Rueckfragen an den Benutzer
// (Host-Schluessel) zaehlt nicht mit. Variablen, damit Tests sie kuerzen koennen.
var (
	// VerbindenFrist begrenzt den TCP-Aufbau je Hop.
	VerbindenFrist = 15 * time.Second
	// AufbauFrist begrenzt den ganzen Aufbau (alle Hops, Handshake, Anmeldung).
	AufbauFrist = 20 * time.Second
)

func authMethoden(a Anmeldung) ([]ssh.AuthMethod, error) {
	var m []ssh.AuthMethod
	if len(a.Schluessel) > 0 {
		signer, err := schluesselLesen(a.Schluessel, a.Passphrase)
		if err != nil {
			return nil, err
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

// schluesselLesen liest einen privaten Schluessel; Fehler kommen als verstaendlicher Satz.
func schluesselLesen(pemDaten []byte, passphrase string) (ssh.Signer, error) {
	var signer ssh.Signer
	var err error
	if passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(pemDaten, []byte(passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey(pemDaten)
	}
	if err == nil {
		return signer, nil
	}
	var pp *ssh.PassphraseMissingError
	switch {
	case errors.As(err, &pp):
		return nil, ErrPassphrase
	case strings.Contains(err.Error(), "decryption password incorrect"): // x509.IncorrectPasswordError
		return nil, errors.New("Die Passphrase passt nicht zum Schlüssel")
	}
	log.Printf("Schlüssel lesen: %v", err)
	return nil, errors.New("Das ist kein lesbarer privater Schlüssel (OpenSSH- oder PEM-Format erwartet)")
}

// hop ist ein Teilstueck der Verbindung samt Konfiguration.
type hop struct {
	ziel     Ziel
	cfg      *ssh.ClientConfig
	hkFehler error // Ablehnung des Host-Schluessels (schon verstaendlich formuliert)
}

func neuerHop(z Ziel, pruefer HostKeyPruefer, u *uhr) (*hop, error) {
	m, err := authMethoden(z.Anmeldung)
	if err != nil {
		return nil, err
	}
	h := &hop{ziel: z}
	hp := z.HostPort()
	h.cfg = &ssh.ClientConfig{
		User: z.Anmeldung.Benutzer,
		Auth: m,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			// Die Rueckfrage an den Benutzer zaehlt nicht zur Aufbaufrist.
			u.anhalten()
			defer u.weiter()
			if err := pruefer(hp, key); err != nil {
				h.hkFehler = err
				return err
			}
			return nil
		},
	}
	return h, nil
}

// Verbinden baut die Verbindung auf; ist jump gesetzt, laeuft sie ueber diesen Host.
// Zurueck kommen der Ziel-Client und eine Schliessfunktion fuer alle Hops. Fehler
// sind verstaendliche deutsche Saetze; der rohe Text geht ins Log.
func Verbinden(ctx context.Context, ziel Ziel, jump *Ziel, pruefer HostKeyPruefer) (*ssh.Client, func(), error) {
	u, fctx := neueUhr(ctx, AufbauFrist)
	defer u.stop()
	var jc *ssh.Client
	dial := func(z Ziel) (net.Conn, error) {
		d := net.Dialer{Timeout: VerbindenFrist}
		return d.DialContext(fctx, "tcp", z.HostPort())
	}
	if jump != nil {
		h, err := neuerHop(*jump, pruefer, u)
		if err != nil {
			return nil, nil, fmt.Errorf("Jump-Host: %w", err)
		}
		conn, err := dial(*jump)
		if err != nil {
			return nil, nil, fmt.Errorf("Jump-Host: %w", netzFehler(ctx, u, *jump, err))
		}
		c, ch, rq, err := handshake(fctx, conn, h)
		if err != nil {
			return nil, nil, fmt.Errorf("Jump-Host: %w", sshFehler(ctx, u, h, err))
		}
		jc = ssh.NewClient(c, ch, rq)
		dial = func(z Ziel) (net.Conn, error) {
			dctx, cancel := context.WithTimeout(fctx, VerbindenFrist)
			defer cancel()
			conn, err := jc.DialContext(dctx, "tcp", z.HostPort())
			var oce *ssh.OpenChannelError
			if errors.As(err, &oce) {
				return nil, &weiterleitFehler{oce}
			}
			return conn, err
		}
	}
	schliessen := func() {
		if jc != nil {
			jc.Close()
		}
	}
	h, err := neuerHop(ziel, pruefer, u)
	if err != nil {
		schliessen()
		return nil, nil, err
	}
	conn, err := dial(ziel)
	if err != nil {
		schliessen()
		return nil, nil, netzFehler(ctx, u, ziel, err)
	}
	c, ch, rq, err := handshake(fctx, conn, h)
	if err != nil {
		schliessen()
		return nil, nil, sshFehler(ctx, u, h, err)
	}
	client := ssh.NewClient(c, ch, rq)
	return client, func() { client.Close(); schliessen() }, nil
}

// handshake fuehrt den SSH-Handshake mit Abbruch ueber ctx durch.
func handshake(ctx context.Context, conn net.Conn, h *hop) (ssh.Conn, <-chan ssh.NewChannel, <-chan *ssh.Request, error) {
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	c, ch, rq, err := ssh.NewClientConn(conn, h.ziel.HostPort(), h.cfg)
	if err != nil {
		conn.Close()
		return nil, nil, nil, err
	}
	return c, ch, rq, nil
}

// --- Fristen ---

// uhr misst die Aufbaufrist. Laeuft sie ab, wird fctx abgebrochen (Verbinden bzw.
// Handshake brechen ab). anhalten/weiter klammern Rueckfragen an den Benutzer.
type uhr struct {
	mu         sync.Mutex
	rest       time.Duration
	seit       time.Time // Beginn des laufenden Abschnitts; Null = angehalten
	t          *time.Timer
	cancel     context.CancelFunc
	abgelaufen bool
}

func neueUhr(ctx context.Context, frist time.Duration) (*uhr, context.Context) {
	fctx, cancel := context.WithCancel(ctx)
	u := &uhr{rest: frist, seit: time.Now(), cancel: cancel}
	u.t = time.AfterFunc(frist, u.ablaufen)
	return u, fctx
}

func (u *uhr) ablaufen() {
	u.mu.Lock()
	if u.seit.IsZero() {
		// Waehrend einer Rueckfrage angehalten: zaehlt nicht.
		u.mu.Unlock()
		return
	}
	u.abgelaufen = true
	u.mu.Unlock()
	u.cancel()
}

func (u *uhr) anhalten() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.seit.IsZero() || u.abgelaufen {
		return
	}
	u.t.Stop()
	u.rest -= time.Since(u.seit)
	u.seit = time.Time{}
}

func (u *uhr) weiter() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.seit.IsZero() || u.abgelaufen {
		return
	}
	if u.rest < 0 {
		u.rest = 0
	}
	u.seit = time.Now()
	u.t.Reset(u.rest)
}

func (u *uhr) istAbgelaufen() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.abgelaufen
}

func (u *uhr) stop() {
	u.mu.Lock()
	u.t.Stop()
	u.seit = time.Time{}
	u.mu.Unlock()
	u.cancel()
}

// --- verstaendliche Fehler ---

// weiterleitFehler: der Jump-Host konnte das Ziel nicht erreichen.
type weiterleitFehler struct{ oce *ssh.OpenChannelError }

func (w *weiterleitFehler) Error() string { return w.oce.Error() }

var (
	errVerweigert     = syscall.ECONNREFUSED
	errKeinWeg        = syscall.EHOSTUNREACH
	errNetzWeg        = syscall.ENETUNREACH
	errZurueckgesetzt = syscall.ECONNRESET
)

func sekunden(d time.Duration) int { return int((d + time.Second/2) / time.Second) }

// netzFehler uebersetzt Fehler beim TCP-Aufbau.
func netzFehler(ctx context.Context, u *uhr, z Ziel, err error) error {
	hp, host := z.HostPort(), z.Host()
	log.Printf("Verbinden mit %s: %v", hp, err)
	var dns *net.DNSError
	var ne net.Error
	var wf *weiterleitFehler
	switch {
	case u.istAbgelaufen():
		return fmt.Errorf("Keine Antwort von %s (Zeitüberschreitung nach %d s)", host, sekunden(AufbauFrist))
	case ctx.Err() != nil:
		return ErrAbgebrochen
	case errors.As(err, &wf):
		return weiterleitText(z, wf.oce)
	case errors.As(err, &dns):
		if dns.IsNotFound {
			return fmt.Errorf("Name %s unbekannt", dns.Name)
		}
		return fmt.Errorf("Name %s lässt sich nicht auflösen (Namensdienst antwortet nicht)", dns.Name)
	case errors.Is(err, errVerweigert):
		return fmt.Errorf("Verbindung abgelehnt — läuft auf %s ein SSH-Dienst?", hp)
	case errors.Is(err, errKeinWeg), errors.Is(err, errNetzWeg):
		return fmt.Errorf("%s ist nicht erreichbar (kein Weg ins Netz)", host)
	case errors.Is(err, errZurueckgesetzt):
		return fmt.Errorf("Die Verbindung zu %s wurde zurückgesetzt", hp)
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return fmt.Errorf("Keine Antwort von %s (Zeitüberschreitung nach %d s)", host, sekunden(VerbindenFrist))
	}
	return fmt.Errorf("Verbindung zu %s fehlgeschlagen", hp)
}

// weiterleitText: Antwort des Jump-Hosts auf die Weiterleitung zum Ziel.
func weiterleitText(z Ziel, oce *ssh.OpenChannelError) error {
	hp, host := z.HostPort(), z.Host()
	text := strings.ToLower(oce.Message)
	switch {
	case oce.Reason == ssh.Prohibited:
		return errors.New("Der Jump-Host erlaubt keine Weiterleitung (AllowTcpForwarding)")
	case strings.Contains(text, "refused"):
		return fmt.Errorf("Verbindung abgelehnt — läuft auf %s ein SSH-Dienst?", hp)
	case strings.Contains(text, "unreachable"), strings.Contains(text, "no route"):
		return fmt.Errorf("%s ist vom Jump-Host aus nicht erreichbar (kein Weg ins Netz)", host)
	case strings.Contains(text, "timed out"), strings.Contains(text, "timeout"):
		return fmt.Errorf("Keine Antwort von %s (vom Jump-Host aus)", host)
	case strings.Contains(text, "resolve"), strings.Contains(text, "not known"), strings.Contains(text, "no address"):
		return fmt.Errorf("Name %s unbekannt (vom Jump-Host aus)", host)
	}
	return fmt.Errorf("Der Jump-Host erreicht %s nicht", hp)
}

// sshFehler uebersetzt Fehler aus Handshake und Anmeldung.
func sshFehler(ctx context.Context, u *uhr, h *hop, err error) error {
	hp := h.ziel.HostPort()
	if h.hkFehler != nil {
		return h.hkFehler
	}
	if strings.Contains(err.Error(), "unable to authenticate") {
		return ErrAnmeldung
	}
	log.Printf("SSH-Aufbau mit %s: %v", hp, err)
	text := err.Error()
	switch {
	case u.istAbgelaufen():
		return fmt.Errorf("Keine Antwort von %s — läuft dort ein SSH-Dienst? (Zeitüberschreitung nach %d s)", hp, sekunden(AufbauFrist))
	case ctx.Err() != nil:
		return ErrAbgebrochen
	case errors.Is(err, errZurueckgesetzt):
		return fmt.Errorf("Die Verbindung zu %s wurde zurückgesetzt", hp)
	case strings.HasSuffix(text, "EOF"), errors.Is(err, net.ErrClosed):
		return fmt.Errorf("%s hat die Verbindung beendet — läuft dort ein SSH-Dienst, und lässt er diese Adresse zu?", hp)
	case strings.Contains(text, "no common algorithm"):
		return fmt.Errorf("%s bietet keine gemeinsamen Verschlüsselungsverfahren an (Gerät zu alt?)", hp)
	case strings.Contains(text, "version"), strings.Contains(text, "overflow"), strings.Contains(text, "unexpected"):
		return fmt.Errorf("%s spricht kein SSH", hp)
	}
	return fmt.Errorf("SSH-Verbindung zu %s fehlgeschlagen", hp)
}

// SchluesselInfo liefert Typ, oeffentlichen Teil (authorized_keys-Zeile) und Fingerprint.
func SchluesselInfo(pemDaten []byte, passphrase, kommentar string) (typ, oeffentlich, fp string, err error) {
	signer, err := schluesselLesen(pemDaten, passphrase)
	if err != nil {
		return "", "", "", err
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
		return "", errors.New("Der öffentliche Schlüssel ist ungültig")
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
