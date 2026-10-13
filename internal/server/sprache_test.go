package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/uiff/ssh-app/internal/store"
)

// rufeMit ruft die API mit zusaetzlichen Koepfen und liefert Status und Fehlertext.
func rufeMit(t *testing.T, c *client, methode, pfad, rumpf string, koepfe map[string]string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(methode, c.base+pfad, strings.NewReader(rumpf))
	req.Header.Set("X-SSH-App", "1")
	for k, v := range koepfe {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	res, err := c.h.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var f struct{ Fehler string }
	json.NewDecoder(res.Body).Decode(&f)
	return res.StatusCode, f.Fehler
}

var englisch = map[string]string{"X-SSH-App-Sprache": "en"}

// Eine Anfrage mit «X-SSH-App-Sprache: en» bekommt englische Meldungen, ohne Kopf deutsche.
func TestHTTPFehlerInDerSpracheDerAnfrage(t *testing.T) {
	ts, _, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	host := `{"name":"x","adresse":"10.0.0.1","port":70000}`
	if code, text := rufeMit(t, c, "POST", "/api/hosts", host, englisch); code != 400 || text != "Port must be between 1 and 65535" {
		t.Fatalf("Port englisch: %d %q", code, text)
	}
	if code, text := rufeMit(t, c, "POST", "/api/hosts", host, nil); code != 400 || text != "Port muss zwischen 1 und 65535 liegen" {
		t.Fatalf("Port deutsch: %d %q", code, text)
	}
	if code, text := rufeMit(t, c, "POST", "/api/hosts", `{"adresse":"a b"}`, englisch); code != 400 || !strings.HasPrefix(text, "Address is missing") {
		t.Fatalf("Adresse englisch: %d %q", code, text)
	}
	if code, text := rufeMit(t, c, "PUT", "/api/hosts/999", `{"adresse":"10.0.0.1"}`, englisch); code != 404 || text != "Entry not found" {
		t.Fatalf("unbekannter Host englisch: %d %q", code, text)
	}
	if code, text := rufeMit(t, c, "POST", "/api/hosts", `kein json`, englisch); code != 400 || text != "The request isn’t valid JSON" {
		t.Fatalf("JSON englisch: %d %q", code, text)
	}
	if code, text := rufeMit(t, c, "POST", "/api/hosts", host, map[string]string{"X-SSH-App": "", "X-SSH-App-Sprache": "en"}); code != 403 || text != "Request rejected: app header missing" {
		t.Fatalf("CSRF englisch: %d %q", code, text)
	}

	// Falsches Passwort: Kopf, Accept-Language und Vorgabe.
	c.rufe("POST", "/api/abmelden", nil, nil)
	falsch := `{"benutzer":"admin","passwort":"falsch-falsch"}`
	for _, f := range []struct {
		koepfe map[string]string
		want   string
	}{
		{nil, "Benutzername oder Passwort ist falsch"},
		{englisch, "Incorrect username or password"},
		{map[string]string{"Accept-Language": "en-US,en;q=0.9,de;q=0.5"}, "Incorrect username or password"},
		{map[string]string{"X-SSH-App-Sprache": "de", "Accept-Language": "en-US"}, "Benutzername oder Passwort ist falsch"},
	} {
		if code, text := rufeMit(t, c, "POST", "/api/anmelden", falsch, f.koepfe); code != 401 || text != f.want {
			t.Fatalf("Anmelden %v: %d %q", f.koepfe, code, text)
		}
	}
	if code, text := rufeMit(t, c, "GET", "/api/daten", "", englisch); code != 401 || text != "Please sign in" {
		t.Fatalf("ohne Anmeldung englisch: %d %q", code, text)
	}
	if code, text := rufeMit(t, c, "GET", "/api/daten", "", nil); code != 401 || text != "Bitte anmelden" {
		t.Fatalf("ohne Anmeldung deutsch: %d %q", code, text)
	}

	// Ein Download ohne Kopf: Parameter «sprache».
	if code, text := rufeMit(t, c, "GET", "/api/zugaenge/1/privat?sprache=en", "", nil); code != 401 || text != "Please sign in" {
		t.Fatalf("Parameter sprache: %d %q", code, text)
	}

	// Schutz vor DNS-Rebinding: auch die Textseite (ausserhalb /api) in der Sprache des Browsers.
	ts2, _ := testAppMit(t, Optionen{Version: "test", HostPruefung: true})
	req, _ := http.NewRequest("GET", ts2.URL+"/", nil)
	req.Host = "angreifer.example"
	req.Header.Set("Accept-Language", "en")
	res, err := ts2.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	buf := make([]byte, 512)
	n, _ := res.Body.Read(buf)
	b.Write(buf[:n])
	res.Body.Close()
	if res.StatusCode != http.StatusMisdirectedRequest || !strings.HasPrefix(b.String(), "Access via the name “angreifer.example”") {
		t.Fatalf("Rebinding englisch: %d %q", res.StatusCode, b.String())
	}
	_ = ts
}

// Der WebSocket meldet in der Sprache aus dem Parameter «sprache».
func TestWebSocketInDerSpracheDesBrowsers(t *testing.T) {
	// Verbindung abgelehnt.
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	l.Close()
	ts, _, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	h := hostAnlegen(t, c, addr, true, nil)
	ws := wsOeffnen(t, ts, c, fmt.Sprintf("host=%d&sprache=en", h.ID))
	n := naechste(t, ws)
	if n.T != "status" || n.Text != "Connecting to "+addr+"…" {
		t.Fatalf("Status englisch: %+v", n)
	}
	if n = naechste(t, ws); n.T != "fehler" || n.Text != "Connection refused — is an SSH server running on "+addr+"?" {
		t.Fatalf("abgelehnt englisch: %+v", n)
	}
	ws.Close(websocket.StatusNormalClosure, "")

	// Falsches Passwort, Abbruch der Rueckfrage, Ende der Shell.
	srv := sshTestServer(t, "pi", "himbeere")
	h2 := hostAnlegen(t, c, srv, false, map[string]any{"name": "ohne-zugang"})
	rueckfrage := func(sprache string, antwort nachricht) nachricht {
		ws := wsOeffnen(t, ts, c, fmt.Sprintf("host=%d%s", h2.ID, sprache))
		defer ws.Close(websocket.StatusNormalClosure, "")
		if n := naechste(t, ws); n.T != "anmeldung" {
			t.Fatalf("statt Rückfrage: %+v", n)
		}
		senden(t, ws, antwort)
		for {
			n := naechste(t, ws)
			switch n.T {
			case "hostkey":
				senden(t, ws, nachricht{T: "hostkey", OK: true})
			case "fehler", "bereit":
				return n
			}
		}
	}
	if n := rueckfrage("&sprache=en", nachricht{T: "anmeldung", Benutzer: "pi", Passwort: "falsch"}); n.T != "fehler" || n.Text != "Login rejected — check the username, password, or key" {
		t.Fatalf("falsches Passwort englisch: %+v", n)
	}
	if n := rueckfrage("", nachricht{T: "anmeldung", Abbrechen: true}); n.T != "fehler" || n.Text != "Anmeldung am Host abgebrochen — «Neu\u00a0verbinden», um es erneut zu versuchen." {
		t.Fatalf("Abbruch deutsch: %+v", n)
	}
	if n := rueckfrage("&sprache=en", nachricht{T: "anmeldung", Abbrechen: true}); n.T != "fehler" || n.Text != "Login canceled — choose “Reconnect” to try again." {
		t.Fatalf("Abbruch englisch: %+v", n)
	}

	h3 := hostAnlegen(t, c, srv, true, nil)
	ws3, bereit := terminalOeffnen(t, ts, c, h3.ID)
	ws3.Close(websocket.StatusNormalClosure, "")
	ws4 := wsOeffnen(t, ts, c, "sitzung="+bereit.ID+"&sprache=en")
	liesNachricht(t, ws4, "bereit")
	ws4.Write(context.Background(), websocket.MessageBinary, []byte("exit\r"))
	if n := liesNachricht(t, ws4, "ende"); n.Text != "Session ended (shell exited)" {
		t.Fatalf("Ende englisch: %+v", n)
	}
	var liste []struct{ ID, Grund string }
	req, _ := http.NewRequest("GET", ts.URL+"/api/sitzungen", nil)
	req.Header.Set("X-SSH-App-Sprache", "en")
	res, err := c.h.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	json.NewDecoder(res.Body).Decode(&liste)
	res.Body.Close()
	if len(liste) != 1 || liste[0].Grund != "Session ended (shell exited)" {
		t.Fatalf("Sitzungsliste englisch: %+v", liste)
	}
	ws5 := wsOeffnen(t, ts, c, "sitzung=gibtsnicht&sprache=en")
	if n := naechste(t, ws5); n.T != "ende" || n.Text != "The session no longer exists" {
		t.Fatalf("unbekannte Sitzung englisch: %+v", n)
	}
	_ = store.Host{}
}
