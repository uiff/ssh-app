package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/uiff/ssh-app/internal/help"
	"github.com/uiff/ssh-app/internal/store"
	"github.com/uiff/ssh-app/internal/term"
	"github.com/uiff/ssh-app/internal/vault"
)

// --- eingebetteter SSH-Server: Echo-Shell + SFTP ---

// sshOptionen steuern das Verhalten des eingebetteten SSH-Servers.
type sshOptionen struct {
	benutzer, passwort string
	// exec: "" = POSIX-Shell (fuehrt den Startbefehl aus), "ablehnen" = exec verboten,
	// "fish" = die Login-Shell versteht den Startbefehl nicht (Fehlertext, Code 127).
	exec     string
	ohneSFTP bool
}

type testSSH struct {
	addr    string
	hostKey ssh.PublicKey
	kanaele atomic.Int32 // je geoeffnetem Session-Kanal +1
	offen   atomic.Int32 // gerade offene Session-Kanaele
}

func sshTestServer(t *testing.T, benutzer, passwort string) (addr string) {
	return sshTestServerMit(t, sshOptionen{benutzer: benutzer, passwort: passwort}).addr
}

func sshTestServerMit(t *testing.T, o sshOptionen) *testSSH {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		if c.User() == o.benutzer && string(pw) == o.passwort {
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
	ts := &testSSH{addr: l.Addr().String(), hostKey: signer.PublicKey()}
	ende := func(ch ssh.Channel, code uint32) {
		ch.SendRequest("exit-status", false, ssh.Marshal(struct{ C uint32 }{code}))
		ch.Close()
	}
	shell := func(ch ssh.Channel) {
		ch.Write([]byte("willkommen\r\n$ "))
		go func() {
			buf := make([]byte, 1024)
			for {
				n, err := ch.Read(buf)
				if err != nil {
					return
				}
				if strings.Contains(string(buf[:n]), "exit") {
					ende(ch, 0)
					return
				}
				ch.Write(append([]byte("echo:"), buf[:n]...))
			}
		}()
	}
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
					if nch.ChannelType() != "session" {
						nch.Reject(ssh.UnknownChannelType, "")
						continue
					}
					ch, rq, _ := nch.Accept()
					ts.kanaele.Add(1)
					ts.offen.Add(1)
					go func() {
						defer ts.offen.Add(-1)
						defer ch.Close()
						for req := range rq {
							switch req.Type {
							case "pty-req", "window-change", "env":
								req.Reply(true, nil)
							case "shell":
								req.Reply(true, nil)
								shell(ch)
							case "exec":
								var befehl struct{ Text string }
								ssh.Unmarshal(req.Payload, &befehl)
								switch {
								case strings.Contains(befehl.Text, "authorized_keys"):
									req.Reply(true, nil)
									ch.Write([]byte("SSHAPP-OK\n"))
									ende(ch, 0)
								case o.exec == "ablehnen":
									req.Reply(false, nil)
								case o.exec == "fish":
									req.Reply(true, nil)
									ch.Write([]byte("fish: $$ is not the pid. In fish, please use $fish_pid.\r\n"))
									ende(ch, 127)
								case befehl.Text == term.StartBefehl:
									req.Reply(true, nil)
									ch.Write([]byte("\x1b]7770;pid=4242\x07"))
									shell(ch)
								default:
									req.Reply(false, nil)
								}
							case "subsystem":
								if o.ohneSFTP {
									req.Reply(false, nil)
									continue
								}
								req.Reply(true, nil)
								go func() {
									srv, _ := sftp.NewServer(ch)
									srv.Serve()
									ch.Close()
								}()
							default:
								req.Reply(false, nil)
							}
						}
					}()
				}
			}()
		}
	}()
	return ts
}

// --- Test-Client ---

type client struct {
	t    *testing.T
	base string
	h    *http.Client
}

func neuerClient(t *testing.T, base string, hc *http.Client) *client {
	jar, _ := cookiejar.New(nil)
	c := *hc
	c.Jar = jar
	return &client{t: t, base: base, h: &c}
}

func (c *client) rufe(methode, pfad string, rumpf any, aus any) int {
	c.t.Helper()
	var r io.Reader
	if rumpf != nil {
		b, _ := json.Marshal(rumpf)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(methode, c.base+pfad, r)
	req.Header.Set("X-SSH-App", "1")
	res, err := c.h.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if aus != nil {
		json.Unmarshal(b, aus)
	}
	return res.StatusCode
}

func testApp(t *testing.T) (*httptest.Server, *Server) {
	return testAppMit(t, Optionen{Version: "test"})
}

func testAppMit(t *testing.T, opt Optionen) (*httptest.Server, *Server) {
	t.Helper()
	st, err := store.Oeffnen(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	tr, _ := vault.Neu(nil)
	tm := term.NeuerManager()
	hi, _ := help.Laden(t.TempDir())
	s := Neu(st, tr, tm, hi, opt)
	ts := httptest.NewServer(s)
	t.Cleanup(func() { ts.Close(); s.Close(); tm.Close(); st.Close() })
	return ts, s
}

func lies(t *testing.T, c *websocket.Conn) (websocket.MessageType, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ, b, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("WS lesen: %v", err)
	}
	return typ, b
}

func liesNachricht(t *testing.T, c *websocket.Conn, typ string) nachricht {
	t.Helper()
	for i := 0; i < 20; i++ {
		mt, b := lies(t, c)
		if mt != websocket.MessageText {
			continue
		}
		var n nachricht
		json.Unmarshal(b, &n)
		if n.T == typ {
			return n
		}
		if n.T == "fehler" {
			t.Fatalf("Server-Fehler statt %s: %s", typ, n.Text)
		}
	}
	t.Fatalf("keine Nachricht %s", typ)
	return nachricht{}
}

func liesBisText(t *testing.T, c *websocket.Conn, will string) {
	t.Helper()
	var alles strings.Builder
	for i := 0; i < 30; i++ {
		mt, b := lies(t, c)
		if mt == websocket.MessageBinary {
			alles.Write(b)
			if strings.Contains(alles.String(), will) {
				return
			}
		}
	}
	t.Fatalf("%q nicht erhalten, nur %q", will, alles.String())
}

func wsOeffnen(t *testing.T, ts *httptest.Server, c *client, query string) *websocket.Conn {
	t.Helper()
	u := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/ws?" + query
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPClient: c.h})
	if err != nil {
		t.Fatalf("WS: %v", err)
	}
	return conn
}

func TestEndeZuEnde(t *testing.T) {
	sshAddr := sshTestServer(t, "pi", "himbeere")
	host, port, _ := net.SplitHostPort(sshAddr)
	ts, _ := testApp(t)
	c := neuerClient(t, ts.URL, ts.Client())

	// Ohne Anmeldung gesperrt, ohne App-Kopf abgelehnt.
	if code := c.rufe("GET", "/api/daten", nil, nil); code != 401 {
		t.Fatalf("ohne Anmeldung: %d", code)
	}
	req, _ := http.NewRequest("POST", ts.URL+"/api/einrichten", strings.NewReader(`{}`))
	if res, _ := ts.Client().Do(req); res.StatusCode != 403 {
		t.Fatalf("ohne CSRF-Kopf: %d", res.StatusCode)
	}

	var ein struct{ Sicherheitsschluessel string }
	if code := c.rufe("POST", "/api/einrichten", map[string]string{"benutzer": "admin", "passwort": "geheim-geheim"}, &ein); code != 200 || ein.Sicherheitsschluessel == "" {
		t.Fatalf("einrichten: %d %+v", code, ein)
	}
	if code := c.rufe("POST", "/api/einrichten", map[string]string{"benutzer": "x", "passwort": "geheim-geheim"}, nil); code != 409 {
		t.Fatalf("zweites Einrichten: %d", code)
	}

	var z store.Zugang
	c.rufe("POST", "/api/zugaenge", map[string]string{"name": "pi", "benutzer": "pi", "passwort": "himbeere"}, &z)
	var h store.Host
	p := 0
	fmt.Sscan(port, &p)
	c.rufe("POST", "/api/hosts", map[string]any{"name": "testpi", "adresse": host, "port": p, "zugangId": z.ID, "pfadFolgen": true}, &h)
	var d struct{ Zugaenge []map[string]any }
	c.rufe("GET", "/api/daten", nil, &d)
	if len(d.Zugaenge) != 1 || d.Zugaenge[0]["hosts"].(float64) != 1 {
		t.Fatalf("Daten: %+v", d)
	}
	if strings.Contains(fmt.Sprint(d), "himbeere") {
		t.Fatal("Passwort in der API-Antwort")
	}

	// Terminal: Host-Schluessel bestaetigen, Eingabe, Echo.
	ws := wsOeffnen(t, ts, c, fmt.Sprintf("host=%d&c=100&r=30", h.ID))
	hk := liesNachricht(t, ws, "hostkey")
	if hk.Status != "neu" || !strings.HasPrefix(hk.FP, "SHA256:") {
		t.Fatalf("hostkey: %+v", hk)
	}
	b, _ := json.Marshal(nachricht{T: "hostkey", OK: true})
	ws.Write(context.Background(), websocket.MessageText, b)
	bereit := liesNachricht(t, ws, "bereit")
	liesBisText(t, ws, "willkommen")
	ws.Write(context.Background(), websocket.MessageBinary, []byte("ls\r"))
	liesBisText(t, ws, "echo:ls")
	ws.Close(websocket.StatusNormalClosure, "")

	// Wiederanhaengen liefert den Puffer, die PID-Marke ist herausgefiltert.
	ws2 := wsOeffnen(t, ts, c, "sitzung="+bereit.ID)
	liesNachricht(t, ws2, "bereit")
	_, puffer := lies(t, ws2)
	if !strings.Contains(string(puffer), "echo:ls") || strings.Contains(string(puffer), "7770") {
		t.Fatalf("Puffer: %q", puffer)
	}

	// SFTP ueber dieselbe Verbindung (Testserver = lokales Dateisystem).
	verz := t.TempDir()
	os.WriteFile(filepath.Join(verz, "a.txt"), []byte("hallo"), 0o644)
	var liste struct {
		Pfad      string
		Eintraege []eintrag
	}
	if code := c.rufe("GET", "/api/sitzungen/"+bereit.ID+"/dateien?pfad="+url.QueryEscape(verz), nil, &liste); code != 200 || len(liste.Eintraege) != 1 {
		t.Fatalf("SFTP-Liste: %d %+v", code, liste)
	}
	var mp bytes.Buffer
	mw := multipart.NewWriter(&mp)
	fw, _ := mw.CreateFormFile("datei", "neu.txt")
	fw.Write([]byte("hochgeladen"))
	mw.Close()
	req, _ = http.NewRequest("POST", ts.URL+"/api/sitzungen/"+bereit.ID+"/hochladen?pfad="+url.QueryEscape(verz), &mp)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-SSH-App", "1")
	res, err := c.h.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("Hochladen: %v %v", err, res.Status)
	}
	if b, _ := os.ReadFile(filepath.Join(verz, "neu.txt")); string(b) != "hochgeladen" {
		t.Fatalf("Datei: %q", b)
	}
	if code := c.rufe("POST", "/api/sitzungen/"+bereit.ID+"/aktion", map[string]string{"art": "loeschen", "pfad": filepath.Join(verz, "neu.txt")}, nil); code != 200 {
		t.Fatalf("Loeschen: %d", code)
	}

	// Shell beenden -> Ende-Nachricht.
	ws2.Write(context.Background(), websocket.MessageBinary, []byte("exit\r"))
	liesNachricht(t, ws2, "ende")

	// Backup ziehen und auf einem frischen System einspielen.
	req, _ = http.NewRequest("GET", ts.URL+"/api/sicherung", nil)
	res, _ = c.h.Do(req)
	backup, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if bytes.Contains(backup, []byte("testpi")) || bytes.Contains(backup, []byte("himbeere")) {
		t.Fatal("Backup enthaelt Klartext")
	}

	ts2, _ := testApp(t)
	c2 := neuerClient(t, ts2.URL, ts2.Client())
	einspielen := func(schluessel string) int {
		var mp bytes.Buffer
		mw := multipart.NewWriter(&mp)
		mw.WriteField("schluessel", schluessel)
		fw, _ := mw.CreateFormFile("datei", "backup.json")
		fw.Write(backup)
		mw.Close()
		req, _ := http.NewRequest("POST", ts2.URL+"/api/einspielen", &mp)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("X-SSH-App", "1")
		res, err := c2.h.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := einspielen(vault.SchluesselFormatieren(make([]byte, 32))); code != 400 {
		t.Fatalf("falscher Schluessel: %d", code)
	}
	if code := einspielen(ein.Sicherheitsschluessel); code != 200 {
		t.Fatalf("Einspielen: %d", code)
	}
	if code := c2.rufe("POST", "/api/anmelden", map[string]string{"benutzer": "admin", "passwort": "geheim-geheim"}, nil); code != 200 {
		t.Fatalf("Anmelden nach Einspielen: %d", code)
	}
	var d2 struct{ Hosts []store.Host }
	c2.rufe("GET", "/api/daten", nil, &d2)
	if len(d2.Hosts) != 1 || d2.Hosts[0].Name != "testpi" {
		t.Fatalf("Hosts nach Einspielen: %+v", d2)
	}
	// Der Host-Schluessel reist mit: keine erneute Rueckfrage, Passwort aus dem Tresor.
	ws3 := wsOeffnen(t, ts2, c2, fmt.Sprintf("host=%d", d2.Hosts[0].ID))
	liesNachricht(t, ws3, "bereit")
	ws3.Close(websocket.StatusNormalClosure, "")
	if code := einspielen(ein.Sicherheitsschluessel); code != 409 {
		t.Fatalf("Einspielen ohne Anmeldung auf eingerichtetem System: %d", code)
	}
}

func TestAnmeldungSperreUndZuruecksetzen(t *testing.T) {
	ts, _ := testApp(t)
	c := neuerClient(t, ts.URL, ts.Client())
	var ein struct{ Sicherheitsschluessel string }
	c.rufe("POST", "/api/einrichten", map[string]string{"benutzer": "admin", "passwort": "geheim-geheim"}, &ein)
	c.rufe("POST", "/api/abmelden", nil, nil)
	if code := c.rufe("GET", "/api/daten", nil, nil); code != 401 {
		t.Fatalf("nach Abmelden: %d", code)
	}
	for i := 0; i < maxFehlversuche; i++ {
		if code := c.rufe("POST", "/api/anmelden", map[string]string{"benutzer": "admin", "passwort": "falsch-falsch"}, nil); code != 401 {
			t.Fatalf("Fehlversuch %d: %d", i, code)
		}
	}
	if code := c.rufe("POST", "/api/anmelden", map[string]string{"benutzer": "admin", "passwort": "geheim-geheim"}, nil); code != 429 {
		t.Fatalf("Sperre greift nicht: %d", code)
	}
}

func TestZuruecksetzen(t *testing.T) {
	ts, _ := testApp(t)
	c := neuerClient(t, ts.URL, ts.Client())
	var ein struct{ Sicherheitsschluessel string }
	c.rufe("POST", "/api/einrichten", map[string]string{"benutzer": "admin", "passwort": "geheim-geheim"}, &ein)
	if code := c.rufe("POST", "/api/zuruecksetzen", map[string]string{"schluessel": ein.Sicherheitsschluessel, "benutzer": "neu", "passwort": "anderes-passwort"}, nil); code != 200 {
		t.Fatalf("zuruecksetzen: %d", code)
	}
	if code := c.rufe("GET", "/api/daten", nil, nil); code != 401 {
		t.Fatalf("Zuruecksetzen muss alle abmelden: %d", code)
	}
	if code := c.rufe("POST", "/api/anmelden", map[string]string{"benutzer": "neu", "passwort": "anderes-passwort"}, nil); code != 200 {
		t.Fatalf("Anmelden neu: %d", code)
	}
}
