package sshx

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// sshServer ist ein minimaler SSH-Dienst (nur Anmeldung, keine Kanaele).
func sshServer(t *testing.T) (host string, port int) {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		if c.User() == "pi" && string(pw) == "himbeere" {
			return nil, nil
		}
		return nil, fmt.Errorf("nein")
	}}
	cfg.AddHostKey(signer)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			nc, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for nch := range chans {
					nch.Reject(ssh.Prohibited, "")
				}
			}()
		}
	}()
	a := l.Addr().(*net.TCPAddr)
	return a.IP.String(), a.Port
}

func freierPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return p
}

func alleAnnehmen(string, ssh.PublicKey) error { return nil }

func fristen(t *testing.T, verbinden, aufbau time.Duration) {
	t.Helper()
	v, a := VerbindenFrist, AufbauFrist
	VerbindenFrist, AufbauFrist = verbinden, aufbau
	t.Cleanup(func() { VerbindenFrist, AufbauFrist = v, a })
}

func pi(host string, port int) Ziel {
	return Ziel{Adresse: host, Port: port, Anmeldung: Anmeldung{Benutzer: "pi", Passwort: "himbeere"}}
}

func TestVerbindenUndAnmeldung(t *testing.T) {
	host, port := sshServer(t)
	c, zu, err := Verbinden(context.Background(), pi(host, port), nil, alleAnnehmen)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	zu()

	z := pi(host, port)
	z.Anmeldung.Passwort = "falsch"
	if _, _, err := Verbinden(context.Background(), z, nil, alleAnnehmen); !errors.Is(err, ErrAnmeldung) {
		t.Fatalf("falsches Passwort: %v", err)
	}
	// Abgelehnter Host-Schluessel: die Meldung des Pruefers, ohne «ssh: handshake failed».
	nein := errors.New("Host-Schlüssel nicht bestätigt — Verbindung abgebrochen")
	_, _, err = Verbinden(context.Background(), pi(host, port), nil, func(string, ssh.PublicKey) error { return nein })
	if err == nil || err.Error() != nein.Error() {
		t.Fatalf("abgelehnter Host-Schlüssel: %v", err)
	}
}

// V6: kein «dial tcp …: connection refused», sondern ein deutscher Satz.
func TestVerbindungAbgelehnt(t *testing.T) {
	port := freierPort(t)
	_, _, err := Verbinden(context.Background(), pi("127.0.0.1", port), nil, alleAnnehmen)
	want := "Verbindung abgelehnt — läuft auf 127.0.0.1:" + strconv.Itoa(port) + " ein SSH-Dienst?"
	if err == nil || err.Error() != want {
		t.Fatalf("Meldung: %v", err)
	}
	// Ueber einen Jump-Host mit Praefix.
	j := pi("127.0.0.1", port)
	if _, _, err := Verbinden(context.Background(), pi("127.0.0.1", 22), &j, alleAnnehmen); err == nil || !strings.HasPrefix(err.Error(), "Jump-Host: Verbindung abgelehnt") {
		t.Fatalf("Jump-Host: %v", err)
	}
}

// BACKEND-07: Ein Port, der annimmt und schweigt, haelt den Aufbau nicht minutenlang.
func TestKeineAntwortNachFrist(t *testing.T) {
	fristen(t, time.Second, 400*time.Millisecond)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			defer c.Close() // annehmen und schweigen
		}
	}()
	port := l.Addr().(*net.TCPAddr).Port
	start := time.Now()
	_, _, err = Verbinden(context.Background(), pi("127.0.0.1", port), nil, alleAnnehmen)
	if err == nil || !strings.HasPrefix(err.Error(), "Keine Antwort von 127.0.0.1:"+strconv.Itoa(port)+" — läuft dort ein SSH-Dienst?") {
		t.Fatalf("Meldung: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("Aufbau dauerte %s", d)
	}
}

// Die Rueckfrage zum Host-Schluessel zaehlt nicht zur Aufbaufrist.
func TestRueckfrageZaehltNicht(t *testing.T) {
	fristen(t, time.Second, 300*time.Millisecond)
	host, port := sshServer(t)
	langsam := func(string, ssh.PublicKey) error { time.Sleep(700 * time.Millisecond); return nil }
	c, zu, err := Verbinden(context.Background(), pi(host, port), nil, langsam)
	if err != nil {
		t.Fatalf("Rückfrage wurde der Frist angerechnet: %v", err)
	}
	c.Close()
	zu()
}

func TestAbbruchDurchAufrufer(t *testing.T) {
	host, port := sshServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	pruefer := func(string, ssh.PublicKey) error { cancel(); time.Sleep(50 * time.Millisecond); return nil }
	if _, _, err := Verbinden(ctx, pi(host, port), nil, pruefer); !errors.Is(err, ErrAbgebrochen) {
		t.Fatalf("Abbruch: %v", err)
	}
}

func TestNetzFehlerTexte(t *testing.T) {
	u, _ := neueUhr(context.Background(), time.Minute)
	defer u.stop()
	z := Ziel{Adresse: "plc.halle", Port: 22}
	op := func(e error) error {
		return &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", e)}
	}
	faelle := []struct {
		err  error
		want string
	}{
		{&net.DNSError{Err: "no such host", Name: "plc.halle", IsNotFound: true}, "Name plc.halle unbekannt"},
		{&net.DNSError{Err: "i/o timeout", Name: "plc.halle", IsTimeout: true}, "Name plc.halle lässt sich nicht auflösen (Namensdienst antwortet nicht)"},
		{op(syscall.EHOSTUNREACH), "plc.halle ist nicht erreichbar (kein Weg ins Netz)"},
		{op(syscall.ENETUNREACH), "plc.halle ist nicht erreichbar (kein Weg ins Netz)"},
		{op(syscall.ECONNREFUSED), "Verbindung abgelehnt — läuft auf plc.halle:22 ein SSH-Dienst?"},
		{&net.OpError{Op: "dial", Net: "tcp", Err: zeitFehler{}}, "Keine Antwort von plc.halle (Zeitüberschreitung nach 15 s)"},
		{&weiterleitFehler{&ssh.OpenChannelError{Reason: ssh.ConnectionFailed, Message: "Connection refused"}}, "Verbindung abgelehnt — läuft auf plc.halle:22 ein SSH-Dienst?"},
		{&weiterleitFehler{&ssh.OpenChannelError{Reason: ssh.Prohibited, Message: "administratively prohibited"}}, "Der Jump-Host erlaubt keine Weiterleitung (AllowTcpForwarding)"},
		{errors.New("irgendwas Neues"), "Verbindung zu plc.halle:22 fehlgeschlagen"},
	}
	for _, f := range faelle {
		if got := netzFehler(context.Background(), u, z, f.err); got.Error() != f.want {
			t.Errorf("%v: %q, erwartet %q", f.err, got, f.want)
		}
	}
}

type zeitFehler struct{}

func (zeitFehler) Error() string   { return "i/o timeout" }
func (zeitFehler) Timeout() bool   { return true }
func (zeitFehler) Temporary() bool { return true }

func TestSchluesselFehlerVerstaendlich(t *testing.T) {
	if _, _, _, err := SchluesselInfo([]byte("kein Schlüssel"), "", ""); err == nil || strings.Contains(err.Error(), "ssh:") {
		t.Fatalf("Meldung: %v", err)
	}
	priv, err := NeuerSchluessel("test")
	if err != nil {
		t.Fatal(err)
	}
	if typ, _, fp, err := SchluesselInfo(priv, "", "test"); err != nil || typ != "ssh-ed25519" || !strings.HasPrefix(fp, "SHA256:") {
		t.Fatalf("Schlüssel: %s %s %v", typ, fp, err)
	}
}

func TestFalschePassphrase(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	blk, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "test", []byte("richtig"))
	if err != nil {
		t.Fatal(err)
	}
	daten := pem.EncodeToMemory(blk)
	if _, _, _, err := SchluesselInfo(daten, "", ""); !errors.Is(err, ErrPassphrase) {
		t.Fatalf("ohne Passphrase: %v", err)
	}
	if _, _, _, err := SchluesselInfo(daten, "falsch", ""); err == nil || err.Error() != "Die Passphrase passt nicht zum Schlüssel" {
		t.Fatalf("falsche Passphrase: %v", err)
	}
	if _, _, _, err := SchluesselInfo(daten, "richtig", ""); err != nil {
		t.Fatalf("richtige Passphrase: %v", err)
	}
}
