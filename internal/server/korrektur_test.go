package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/crypto/ssh"

	"github.com/uiff/ssh-app/internal/store"
	"github.com/uiff/ssh-app/internal/vault"
)

// --- Hilfen ---

const testPasswort = "geheim-geheim"

// angemeldeteApp richtet eine App ein; der Client ist angemeldet.
func angemeldeteApp(t *testing.T, opt Optionen) (*httptest.Server, *Server, *client, string) {
	t.Helper()
	ts, s := testAppMit(t, opt)
	c := neuerClient(t, ts.URL, ts.Client())
	var ein struct{ Sicherheitsschluessel string }
	if code := c.rufe("POST", "/api/einrichten", map[string]string{"benutzer": "admin", "passwort": testPasswort}, &ein); code != 200 {
		t.Fatalf("einrichten: %d", code)
	}
	return ts, s, c, ein.Sicherheitsschluessel
}

// hostAnlegen legt einen Host fuer den Test-SSH-Server an (mit Zugang pi/himbeere, wenn mitZugang).
func hostAnlegen(t *testing.T, c *client, addr string, mitZugang bool, extra map[string]any) store.Host {
	t.Helper()
	host, port, _ := net.SplitHostPort(addr)
	p, _ := strconv.Atoi(port)
	h := map[string]any{"name": "testpi", "adresse": host, "port": p}
	if mitZugang {
		var z store.Zugang
		if code := c.rufe("POST", "/api/zugaenge", map[string]string{"name": "pi", "benutzer": "pi", "passwort": "himbeere"}, &z); code != 200 {
			t.Fatalf("Zugang: %d", code)
		}
		h["zugangId"] = z.ID
	}
	for k, v := range extra {
		h[k] = v
	}
	var out store.Host
	if code := c.rufe("POST", "/api/hosts", h, &out); code != 200 {
		t.Fatalf("Host: %d", code)
	}
	return out
}

// naechste liefert die naechste Text-Nachricht (Binaerdaten werden uebersprungen).
func naechste(t *testing.T, ws *websocket.Conn) nachricht {
	t.Helper()
	for i := 0; i < 50; i++ {
		mt, b := lies(t, ws)
		if mt != websocket.MessageText {
			continue
		}
		var n nachricht
		json.Unmarshal(b, &n)
		return n
	}
	t.Fatal("keine Text-Nachricht")
	return nachricht{}
}

func senden(t *testing.T, ws *websocket.Conn, n nachricht) {
	t.Helper()
	b, _ := json.Marshal(n)
	if err := ws.Write(context.Background(), websocket.MessageText, b); err != nil {
		t.Fatalf("WS schreiben: %v", err)
	}
}

// schlusscode liest bis zum Schliessen und liefert den Code.
func schlusscode(t *testing.T, ws *websocket.Conn, frist time.Duration) websocket.StatusCode {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), frist)
	defer cancel()
	for {
		if _, _, err := ws.Read(ctx); err != nil {
			return websocket.CloseStatus(err)
		}
	}
}

// terminalOeffnen verbindet einen neuen Tab und bestaetigt den Host-Schluessel, falls gefragt.
func terminalOeffnen(t *testing.T, ts *httptest.Server, c *client, hostID int64) (*websocket.Conn, nachricht) {
	t.Helper()
	ws := wsOeffnen(t, ts, c, fmt.Sprintf("host=%d&c=80&r=24", hostID))
	for {
		n := naechste(t, ws)
		switch n.T {
		case "hostkey":
			senden(t, ws, nachricht{T: "hostkey", OK: true})
		case "bereit":
			return ws, n
		case "fehler", "ende", "anmeldung":
			t.Fatalf("statt bereit: %+v", n)
		}
	}
}

func backupLaden(t *testing.T, c *client) []byte {
	t.Helper()
	req, _ := http.NewRequest("GET", c.base+"/api/sicherung", nil)
	res, err := c.h.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("Backup laden: %v %v", err, res.Status)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return b
}

// einspielenAn laedt eine Backup-Datei hoch (pfad /api/einspielen oder /api/sicherung).
func einspielenAn(c *client, pfad string, datei []byte, schluessel string) (int, string) {
	var mp bytes.Buffer
	mw := multipart.NewWriter(&mp)
	mw.WriteField("schluessel", schluessel)
	fw, _ := mw.CreateFormFile("datei", "backup.json")
	fw.Write(datei)
	mw.Close()
	req, _ := http.NewRequest("POST", c.base+pfad, &mp)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-SSH-App", "1")
	res, err := c.h.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer res.Body.Close()
	var f struct{ Fehler string }
	json.NewDecoder(res.Body).Decode(&f)
	return res.StatusCode, f.Fehler
}

// backupUmbauen entschluesselt ein Backup, laesst aendern den Inhalt bearbeiten und
// versiegelt es wieder mit dem Backup-Schluessel der Quell-App.
func backupUmbauen(t *testing.T, quelle *Server, datei []byte, sk string, aendern func(map[string]any)) []byte {
	t.Helper()
	var d sicherungsDatei
	if err := json.Unmarshal(datei, &d); err != nil {
		t.Fatal(err)
	}
	klar, err := vault.BackupOeffnen(sk, d.Daten)
	if err != nil {
		t.Fatal(err)
	}
	var inhalt map[string]any
	json.Unmarshal(klar, &inhalt)
	aendern(inhalt)
	klar, _ = json.Marshal(inhalt)
	if d.Daten, err = quelle.tr.BackupVersiegeln(klar); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(d)
	return b
}

// --- V4 / BACKEND-08: Abloesen ---

func TestZweiterBrowserLoestErstenAb(t *testing.T) {
	ssh := sshTestServer(t, "pi", "himbeere")
	ts, _, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	h := hostAnlegen(t, c, ssh, true, nil)
	ws1, bereit := terminalOeffnen(t, ts, c, h.ID)
	if bereit.Wieder == nil || *bereit.Wieder {
		t.Fatalf("neue Sitzung: wieder = %v", bereit.Wieder)
	}
	liesBisText(t, ws1, "willkommen")

	ws2 := wsOeffnen(t, ts, c, "sitzung="+bereit.ID)
	b2 := liesNachricht(t, ws2, "bereit")
	if b2.Wieder == nil || !*b2.Wieder {
		t.Fatalf("Wiederanhängen: wieder = %v", b2.Wieder)
	}
	// V5: direkt nach «bereit» kommt der Puffer.
	if mt, puffer := lies(t, ws2); mt != websocket.MessageBinary || !strings.Contains(string(puffer), "willkommen") {
		t.Fatalf("nach bereit kein Puffer: %v %q", mt, puffer)
	}
	if n := naechste(t, ws1); n.T != "abgeloest" {
		t.Fatalf("alter Browser bekam %+v statt abgeloest", n)
	}
	if code := schlusscode(t, ws1, 5*time.Second); code != wsAbgeloest {
		t.Fatalf("Schliesscode des alten Browsers: %d", code)
	}
	ws2.Write(context.Background(), websocket.MessageBinary, []byte("pwd\r"))
	liesBisText(t, ws2, "echo:pwd")
	ws2.Close(websocket.StatusNormalClosure, "")
}

// --- V2: Zugangsdaten speichern ---

func TestZugangsdatenNurNachErfolgSpeichern(t *testing.T) {
	ssh := sshTestServer(t, "pi", "himbeere")
	ts, _, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	h := hostAnlegen(t, c, ssh, false, map[string]any{"name": "werkstatt-pi"})

	verbinden := func(benutzer, passwort string, speichern bool) nachricht {
		ws := wsOeffnen(t, ts, c, fmt.Sprintf("host=%d", h.ID))
		defer ws.Close(websocket.StatusNormalClosure, "")
		if n := naechste(t, ws); n.T != "anmeldung" {
			t.Fatalf("statt Rückfrage: %+v", n)
		}
		senden(t, ws, nachricht{T: "anmeldung", Benutzer: benutzer, Passwort: passwort, Speichern: speichern})
		for {
			n := naechste(t, ws)
			if n.T == "hostkey" {
				senden(t, ws, nachricht{T: "hostkey", OK: true})
				continue
			}
			if n.T == "fehler" || n.T == "bereit" {
				return n
			}
		}
	}
	daten := func() (store.Host, []store.Zugang) {
		var d struct {
			Hosts    []store.Host
			Zugaenge []store.Zugang
		}
		c.rufe("GET", "/api/daten", nil, &d)
		return d.Hosts[0], d.Zugaenge
	}

	if n := verbinden("pi", "falsch", true); n.T != "fehler" || !strings.HasPrefix(n.Text, "Anmeldung am Host abgelehnt") {
		t.Fatalf("falsches Passwort: %+v", n)
	}
	if host, zs := daten(); host.ZugangID != 0 || len(zs) != 0 || host.Benutzer != "" {
		t.Fatalf("nach Fehlanmeldung gespeichert: %+v %+v", host, zs)
	}
	if n := verbinden("pi", "himbeere", false); n.T != "bereit" || n.Gespeichert {
		t.Fatalf("ohne Speichern: %+v", n)
	}
	if _, zs := daten(); len(zs) != 0 {
		t.Fatalf("ohne Haken gespeichert: %+v", zs)
	}
	if n := verbinden("pi", "himbeere", true); n.T != "bereit" || !n.Gespeichert {
		t.Fatalf("mit Speichern: %+v", n)
	}
	host, zs := daten()
	if len(zs) != 1 || host.ZugangID != zs[0].ID || zs[0].Name != "werkstatt-pi" || zs[0].Benutzer != "pi" || !zs[0].HatPasswort || zs[0].Art != "passwort" {
		t.Fatalf("gespeicherter Zugang: %+v %+v", host, zs)
	}
	// Danach ohne Rueckfrage.
	ws, _ := terminalOeffnen(t, ts, c, h.ID)
	ws.Close(websocket.StatusNormalClosure, "")
}

// --- V3: Leerlauf ---

// leser liest alle Nachrichten im Hintergrund (damit der WebSocket nicht staut).
type leser struct {
	mu    sync.Mutex
	typen []string
	ende  chan websocket.StatusCode
}

func lesenImHintergrund(ws *websocket.Conn) *leser {
	l := &leser{ende: make(chan websocket.StatusCode, 1)}
	go func() {
		for {
			mt, b, err := ws.Read(context.Background())
			if err != nil {
				l.ende <- websocket.CloseStatus(err)
				return
			}
			if mt == websocket.MessageText {
				var n nachricht
				json.Unmarshal(b, &n)
				l.mu.Lock()
				l.typen = append(l.typen, n.T)
				l.mu.Unlock()
			}
		}
	}()
	return l
}

func (l *leser) hat(typ string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Contains(l.typen, typ)
}

func TestLeerlaufSchliesstWebSocket(t *testing.T) {
	ssh := sshTestServer(t, "pi", "himbeere")
	ts, _, c, _ := angemeldeteApp(t, Optionen{Version: "test", Leerlauf: 1500 * time.Millisecond})
	h := hostAnlegen(t, c, ssh, true, nil)
	ws, _ := terminalOeffnen(t, ts, c, h.ID)
	l := lesenImHintergrund(ws)

	// Tippen ist Bedienung: die Anmeldung bleibt ueber die Leerlaufzeit hinaus.
	bis := time.Now().Add(2500 * time.Millisecond)
	for time.Now().Before(bis) {
		ws.Write(context.Background(), websocket.MessageBinary, []byte("x"))
		time.Sleep(250 * time.Millisecond)
	}
	select {
	case code := <-l.ende:
		t.Fatalf("trotz Tippen geschlossen (%d)", code)
	default:
	}
	// Ping ist keine Bedienung: danach laeuft die Anmeldung ab.
	start := time.Now()
	for {
		select {
		case code := <-l.ende:
			if code != wsAbgemeldet || !l.hat("abgemeldet") {
				t.Fatalf("Schliesscode %d, Nachrichten %v", code, l.typen)
			}
			if d := time.Since(start); d < time.Second {
				t.Fatalf("zu früh abgemeldet (%s)", d)
			}
			var st struct{ Angemeldet bool }
			c.rufe("GET", "/api/status", nil, &st)
			if st.Angemeldet {
				t.Fatal("nach Ablauf noch angemeldet")
			}
			return
		case <-time.After(250 * time.Millisecond):
			senden(t, ws, nachricht{T: "ping"})
		}
		if time.Since(start) > 5*time.Second {
			t.Fatal("WebSocket nach Leerlauf nicht geschlossen")
		}
	}
}

func TestAutomatischeAbfragenVerlaengernNicht(t *testing.T) {
	ts, _, auto, _ := angemeldeteApp(t, Optionen{Version: "test", Leerlauf: time.Second})
	bedient := neuerClient(t, ts.URL, ts.Client())
	if code := bedient.rufe("POST", "/api/anmelden", map[string]string{"benutzer": "admin", "passwort": testPasswort}, nil); code != 200 {
		t.Fatalf("zweite Anmeldung: %d", code)
	}
	abfrage := func(c *client, pfad string, automatisch bool) int {
		req, _ := http.NewRequest("GET", ts.URL+pfad, nil)
		req.Header.Set("X-SSH-App", "1")
		if automatisch {
			req.Header.Set(autoKopf, "1")
		}
		res, err := c.h.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	bis := time.Now().Add(1600 * time.Millisecond)
	for time.Now().Before(bis) {
		abfrage(auto, "/api/sitzungen/abc/pfad", true)
		abfrage(auto, "/api/status", true)
		abfrage(bedient, "/api/sitzungen/abc/pfad", false)
		time.Sleep(150 * time.Millisecond)
	}
	// Auch das automatische Wiederverbinden (WebSocket mit auto=1) zaehlt nicht.
	if code := abfrage(auto, "/api/ws?sitzung=abc&auto=1", false); code != http.StatusUnauthorized {
		t.Fatalf("WebSocket-Aufbau nach Ablauf: %d", code)
	}
	var st struct{ Angemeldet bool }
	auto.rufe("GET", "/api/status", nil, &st)
	if st.Angemeldet {
		t.Fatal("automatische Abfragen haben die Anmeldung verlängert")
	}
	bedient.rufe("GET", "/api/status", nil, &st)
	if !st.Angemeldet {
		t.Fatal("Bedienung hat die Anmeldung nicht verlängert")
	}
}

// --- SICHERHEIT-01/02: Sperre und Argon2-Warteschlange ---

func TestParalleleFehlversucheUmgehenSperreNicht(t *testing.T) {
	ts, _, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	c.rufe("POST", "/api/abmelden", nil, nil)
	codes := map[int]int{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("POST", ts.URL+"/api/anmelden", strings.NewReader(`{"benutzer":"admin","passwort":"falsch-falsch"}`))
			req.Header.Set("X-SSH-App", "1")
			res, err := ts.Client().Do(req)
			if err != nil {
				return
			}
			res.Body.Close()
			mu.Lock()
			codes[res.StatusCode]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	if codes[401] > maxFehlversuche || codes[401]+codes[429] != 30 {
		t.Fatalf("parallele Fehlversuche: %v", codes)
	}
	if code := c.rufe("POST", "/api/anmelden", map[string]string{"benutzer": "admin", "passwort": testPasswort}, nil); code != 429 {
		t.Fatalf("Sperre nach parallelem Schwall: %d", code)
	}
}

func TestArgonWarteschlangeBegrenztUndAbbrechbar(t *testing.T) {
	s := &Server{argon: make(chan struct{}, 2)}
	frei := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < maxArgonWartend; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.mitArgon(context.Background(), func() { <-frei })
		}()
	}
	for s.wartend.Load() < maxArgonWartend {
		time.Sleep(time.Millisecond)
	}
	if err := s.mitArgon(context.Background(), func() { t.Error("über der Grenze gerechnet") }); err != errBelegt {
		t.Fatalf("über der Grenze: %v", err)
	}
	close(frei)
	wg.Wait()
	// Ein abgebrochener Browser rechnet nicht mehr.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.argon <- struct{}{}
	s.argon <- struct{}{}
	if err := s.mitArgon(ctx, func() { t.Error("abgebrochene Anfrage gerechnet") }); err == nil {
		t.Fatal("abgebrochene Anfrage nicht erkannt")
	}
}

// --- SICHERHEIT-04/05: Einspielen ---

func TestEinspielenGegenEinrichten(t *testing.T) {
	_, _, quelle, sk := angemeldeteApp(t, Optionen{Version: "test"})
	quelle.rufe("POST", "/api/hosts", map[string]any{"name": "a", "adresse": "10.0.0.1"}, nil)
	backup := backupLaden(t, quelle)
	for runde := 0; runde < 3; runde++ {
		ts, s := testApp(t)
		c := neuerClient(t, ts.URL, ts.Client())
		var wg sync.WaitGroup
		var codeEinspielen, codeEinrichten int
		wg.Add(2)
		go func() { defer wg.Done(); codeEinspielen, _ = einspielenAn(c, "/api/einspielen", backup, sk) }()
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("POST", ts.URL+"/api/einrichten", strings.NewReader(`{"benutzer":"neuer","passwort":"anderes-passwort"}`))
			req.Header.Set("X-SSH-App", "1")
			if res, err := ts.Client().Do(req); err == nil {
				codeEinrichten = res.StatusCode
				res.Body.Close()
			}
		}()
		wg.Wait()
		if !(codeEinspielen == 200 && codeEinrichten == 409) && !(codeEinspielen == 409 && codeEinrichten == 200) {
			t.Fatalf("Runde %d: einspielen %d, einrichten %d", runde, codeEinspielen, codeEinrichten)
		}
		// Was auf der Platte steht, muss zum Tresor im Speicher passen (sonst nach Neustart unlesbar).
		meta, _ := s.st.Meta(MetaTresor)
		if !bytes.Equal(meta, s.tr.KopfJSON()) {
			t.Fatalf("Runde %d: Kopf auf der Platte ≠ Kopf im Speicher", runde)
		}
		nachNeustart, err := vault.Neu(meta)
		if err != nil {
			t.Fatal(err)
		}
		benutzer, pw := "admin", testPasswort
		if codeEinrichten == 200 {
			benutzer, pw = "neuer", "anderes-passwort"
		}
		if err := nachNeustart.Anmelden(benutzer, pw); err != nil {
			t.Fatalf("Runde %d: nach Neustart: %v", runde, err)
		}
	}
}

func TestEinspielenPrueftTresorVorDemErsetzen(t *testing.T) {
	_, quellApp, quelle, sk := angemeldeteApp(t, Optionen{Version: "test"})
	backup := backupLaden(t, quelle)
	kaputt := backupUmbauen(t, quellApp, backup, sk, func(sg map[string]any) {
		roh, _ := base64.StdEncoding.DecodeString(sg["tresorKopf"].(string))
		var k map[string]any
		json.Unmarshal(roh, &k)
		k["v"] = 2 // kuenftiges Kopf-Format
		roh, _ = json.Marshal(k)
		sg["tresorKopf"] = base64.StdEncoding.EncodeToString(roh)
	})

	_, ziel, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	c.rufe("POST", "/api/hosts", map[string]any{"name": "bleibt", "adresse": "10.0.0.9"}, nil)
	vorher, _ := ziel.st.Meta(MetaTresor)
	code, text := einspielenAn(c, "/api/sicherung", kaputt, sk)
	if code != 400 || !strings.Contains(text, "nichts geändert") {
		t.Fatalf("kaputter Tresor-Kopf: %d %q", code, text)
	}
	var d struct{ Hosts []store.Host }
	c.rufe("GET", "/api/daten", nil, &d)
	nachher, _ := ziel.st.Meta(MetaTresor)
	if len(d.Hosts) != 1 || d.Hosts[0].Name != "bleibt" || !bytes.Equal(vorher, nachher) {
		t.Fatalf("Bestand verändert: %+v", d.Hosts)
	}
}

// --- V1: Geraetetyp ---

func TestGeraetSpeichernPruefenUndSichern(t *testing.T) {
	_, quellApp, c, sk := angemeldeteApp(t, Optionen{Version: "test"})
	var h store.Host
	if code := c.rufe("POST", "/api/hosts", map[string]any{"name": "plc", "adresse": "10.0.0.2", "geraet": "sps"}, &h); code != 200 || h.Geraet != "sps" {
		t.Fatalf("Gerät speichern: %d %+v", code, h)
	}
	var f struct{ Fehler string }
	if code := c.rufe("PUT", fmt.Sprintf("/api/hosts/%d", h.ID), map[string]any{"name": "plc", "adresse": "10.0.0.2", "geraet": "toaster"}, &f); code != 400 || !strings.Contains(f.Fehler, "Gerätetyp") {
		t.Fatalf("unbekanntes Gerät: %d %q", code, f.Fehler)
	}
	backup := backupLaden(t, c)

	// Rundlauf: der Typ reist mit.
	ts2, _ := testApp(t)
	c2 := neuerClient(t, ts2.URL, ts2.Client())
	if code, text := einspielenAn(c2, "/api/einspielen", backup, sk); code != 200 {
		t.Fatalf("Einspielen: %d %s", code, text)
	}
	c2.rufe("POST", "/api/anmelden", map[string]string{"benutzer": "admin", "passwort": testPasswort}, nil)
	var d struct{ Hosts []store.Host }
	c2.rufe("GET", "/api/daten", nil, &d)
	if len(d.Hosts) != 1 || d.Hosts[0].Geraet != "sps" {
		t.Fatalf("nach Rundlauf: %+v", d.Hosts)
	}

	// Altes Backup ohne Feld und Backup mit unbekanntem Typ: beides wird «Allgemein».
	umgebaut := backupUmbauen(t, quellApp, backup, sk, func(sg map[string]any) {
		hosts := sg["hosts"].([]any)
		delete(hosts[0].(map[string]any), "geraet")
		sg["hosts"] = append(hosts, map[string]any{"id": 77, "name": "neu", "adresse": "10.0.0.7", "geraet": "zukunft"})
	})
	ts3, _ := testApp(t)
	c3 := neuerClient(t, ts3.URL, ts3.Client())
	if code, text := einspielenAn(c3, "/api/einspielen", umgebaut, sk); code != 200 {
		t.Fatalf("altes Backup: %d %s", code, text)
	}
	c3.rufe("POST", "/api/anmelden", map[string]string{"benutzer": "admin", "passwort": testPasswort}, nil)
	c3.rufe("GET", "/api/daten", nil, &d)
	if len(d.Hosts) != 2 || d.Hosts[0].Geraet != "" || d.Hosts[1].Geraet != "" {
		t.Fatalf("altes/fremdes Backup: %+v", d.Hosts)
	}
}

// Die Liste im Server muss zu GERAETE in geraete.js passen.
func TestGeraeteWieImBrowser(t *testing.T) {
	quelle, err := os.ReadFile("../../web/static/js/geraete.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(quelle)
	i := strings.Index(s, "const GERAETE = [")
	j := strings.Index(s[i:], "\n];")
	if i < 0 || j < 0 {
		t.Fatal("GERAETE in geraete.js nicht gefunden")
	}
	var browser []string
	for _, m := range regexp.MustCompile(`\{\s*id:\s*'([^']*)'`).FindAllStringSubmatch(s[i:i+j], -1) {
		browser = append(browser, m[1])
	}
	if len(browser) < 5 || !slices.Equal(browser, geraeteIDs) {
		t.Fatalf("Gerätetypen weichen ab:\n  Browser: %q\n  Server:  %q", browser, geraeteIDs)
	}
}

// --- V6 / BACKEND-07/15: verstaendliche Fehler ---

func TestVerbindungAbgelehntAufDeutsch(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	l.Close()
	ts, _, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	h := hostAnlegen(t, c, addr, true, nil)
	ws := wsOeffnen(t, ts, c, fmt.Sprintf("host=%d", h.ID))
	for {
		n := naechste(t, ws)
		if n.T == "status" {
			continue
		}
		want := "Verbindung abgelehnt — läuft auf " + addr + " ein SSH-Dienst?"
		if n.T != "fehler" || n.Text != want {
			t.Fatalf("Meldung: %+v", n)
		}
		return
	}
}

// --- BACKEND-05: Startbefehl ---

func TestStartbefehlFaelltAufShellZurueck(t *testing.T) {
	for _, art := range []string{"ablehnen", "fish"} {
		t.Run(art, func(t *testing.T) {
			srv := sshTestServerMit(t, sshOptionen{benutzer: "pi", passwort: "himbeere", exec: art})
			ts, _, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
			h := hostAnlegen(t, c, srv.addr, true, map[string]any{"pfadFolgen": true})
			ws, _ := terminalOeffnen(t, ts, c, h.ID)
			var alles strings.Builder
			for !strings.Contains(alles.String(), "willkommen") {
				mt, b := lies(t, ws)
				if mt == websocket.MessageText {
					t.Fatalf("statt Terminal: %s", b)
				}
				alles.Write(b)
			}
			if strings.Contains(alles.String(), "fish:") {
				t.Fatalf("Fehlertext des Startbefehls angezeigt: %q", alles.String())
			}
			ws.Write(context.Background(), websocket.MessageBinary, []byte("ls\r"))
			liesBisText(t, ws, "echo:ls")
			ws.Close(websocket.StatusNormalClosure, "")
		})
	}
}

// --- BACKEND-11: SFTP abgelehnt ---

func TestSFTPAbgelehntOhneKanalLeck(t *testing.T) {
	srv := sshTestServerMit(t, sshOptionen{benutzer: "pi", passwort: "himbeere", ohneSFTP: true})
	ts, _, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	h := hostAnlegen(t, c, srv.addr, true, map[string]any{"pfadFolgen": true})
	ws, bereit := terminalOeffnen(t, ts, c, h.ID)
	defer ws.Close(websocket.StatusNormalClosure, "")
	liesBisText(t, ws, "willkommen")
	for i := 0; i < 5; i++ {
		if code := c.rufe("GET", "/api/sitzungen/"+bereit.ID+"/pfad", nil, nil); code != 200 {
			t.Fatalf("Pfad-Abfrage: %d", code)
		}
	}
	ende := time.Now().Add(2 * time.Second)
	for srv.offen.Load() != 1 && time.Now().Before(ende) {
		time.Sleep(10 * time.Millisecond)
	}
	if k, o := srv.kanaele.Load(), srv.offen.Load(); k != 2 || o != 1 {
		t.Fatalf("SFTP-Versuche: %d Kanäle geöffnet, %d noch offen (erwartet 2 und 1)", k, o)
	}
}

// --- BACKEND-14: Schluessel eintragen ohne Benutzer am Host ---

func TestSchluesselEintragenMitBenutzerDesZugangs(t *testing.T) {
	srv := sshTestServerMit(t, sshOptionen{benutzer: "pi", passwort: "himbeere"})
	_, s, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	h := hostAnlegen(t, c, srv.addr, false, nil)
	hp := net.JoinHostPort(h.Adresse, strconv.Itoa(h.Port))
	s.st.BekanntSetzen(store.BekannterHost{Ziel: hp, Typ: srv.hostKey.Type(),
		Schluessel: base64.StdEncoding.EncodeToString(srv.hostKey.Marshal()), Fingerprint: ssh.FingerprintSHA256(srv.hostKey)})

	var mit, ohne store.Zugang
	c.rufe("POST", "/api/zugaenge/erzeugen", map[string]string{"name": "neu", "benutzer": "pi"}, &mit)
	c.rufe("POST", "/api/zugaenge/erzeugen", map[string]string{"name": "ohne"}, &ohne)
	var f struct{ Fehler string }
	if code := c.rufe("POST", fmt.Sprintf("/api/zugaenge/%d/installieren", mit.ID), map[string]any{"hostId": h.ID, "passwort": "himbeere"}, &f); code != 200 {
		t.Fatalf("Eintragen mit Benutzer des Zugangs: %d %s", code, f.Fehler)
	}
	if code := c.rufe("POST", fmt.Sprintf("/api/zugaenge/%d/installieren", ohne.ID), map[string]any{"hostId": h.ID, "passwort": "himbeere"}, &f); code != 400 || !strings.Contains(f.Fehler, "kein Benutzer") {
		t.Fatalf("ohne Benutzer: %d %q", code, f.Fehler)
	}
}

// --- SICHERHEIT-07 / CONTAINER-07: Proxy und Adressen ---

func TestClientIPHinterProxy(t *testing.T) {
	s := &Server{opt: Optionen{Proxys: []netip.Prefix{netip.MustParsePrefix("10.0.0.1/32")}}}
	faelle := []struct{ remote, xff, real, want string }{
		{"10.0.0.1:5555", "203.0.113.9, 192.0.2.7", "", "192.0.2.7"},
		{"10.0.0.1:5555", "203.0.113.9, 10.0.0.1", "", "203.0.113.9"},
		{"192.0.2.1:1234", "203.0.113.9", "", "192.0.2.1"}, // nicht vertraut: Kopf zaehlt nicht
		{"10.0.0.1:5555", "", "198.51.100.4", "198.51.100.4"},
		{"10.0.0.1:5555", "kaputt", "", "10.0.0.1"},
	}
	for _, f := range faelle {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = f.remote
		if f.xff != "" {
			r.Header.Set("X-Forwarded-For", f.xff)
		}
		if f.real != "" {
			r.Header.Set("X-Real-IP", f.real)
		}
		if got := s.clientIP(r); got != f.want {
			t.Errorf("%+v: %s", f, got)
		}
	}
	s.schnittstellen = func() []netip.Prefix { return nil }
	stufen := func(ip string) string { return fmt.Sprint(s.sperrStufen(ip)) }
	if got := stufen("2001:db8:1:2:3:4:5:6"); got != "[{2001:db8:1:2:3:4:5:6 5} {2001:db8:1:2::/64 30}]" {
		t.Errorf("IPv6: %s", got)
	}
	if got := stufen("::ffff:192.0.2.1"); got != "[{192.0.2.1 5}]" {
		t.Errorf("IPv4 in IPv6: %s", got)
	}
	if got := stufen("fe80::1%eth0"); got != "[{fe80::1 5}]" {
		t.Errorf("link-lokal: %s", got)
	}
}

func TestSperreHinterProxyJeBrowser(t *testing.T) {
	ts, _, c, _ := angemeldeteApp(t, Optionen{Version: "test", Proxys: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}})
	c.rufe("POST", "/api/abmelden", nil, nil)
	anmelden := func(von, passwort string) int {
		req, _ := http.NewRequest("POST", ts.URL+"/api/anmelden", strings.NewReader(`{"benutzer":"admin","passwort":"`+passwort+`"}`))
		req.Header.Set("X-SSH-App", "1")
		req.Header.Set("X-Forwarded-For", von)
		res, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	for i := 0; i < maxFehlversuche; i++ {
		anmelden("198.51.100.1", "falsch-falsch")
	}
	if code := anmelden("198.51.100.1", testPasswort); code != 429 {
		t.Fatalf("Angreifer nicht gesperrt: %d", code)
	}
	if code := anmelden("198.51.100.2", testPasswort); code != 200 {
		t.Fatalf("Besitzer hinter demselben Proxy ausgesperrt: %d", code)
	}
}

// --- SICHERHEIT-08: Host-Kopf ---

func TestHostKopfGegenRebinding(t *testing.T) {
	ts, _ := testAppMit(t, Optionen{Version: "test", HostPruefung: true, Hosts: []string{"ssh.example.ch", "*.firma.ch"}})
	status := func(host string) int {
		req, _ := http.NewRequest("GET", ts.URL+"/api/status", nil)
		req.Host = host
		res, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	for host, want := range map[string]int{
		"angreifer.example:8443": http.StatusMisdirectedRequest,
		"ssh.example.ch":         200,
		"pi.firma.ch":            200,
		"raspberrypi:8443":       200,
		"pi.local":               200,
		"192.168.1.5:8443":       200,
		"[fe80::1]:8443":         200,
		"localhost:8443":         200,
	} {
		if got := status(host); got != want {
			t.Errorf("Host %s: %d, erwartet %d", host, got, want)
		}
	}
	// Ohne Pruefung (TLS-Betrieb) wie bisher.
	ts2, _ := testApp(t)
	req, _ := http.NewRequest("GET", ts2.URL+"/api/status", nil)
	req.Host = "angreifer.example"
	if res, _ := ts2.Client().Do(req); res.StatusCode != 200 {
		t.Fatalf("ohne Prüfung: %d", res.StatusCode)
	}
}

// --- CONTAINER-06: WebSocket hinter einem Proxy mit anderem Host ---

func TestWebSocketOriginsHinterProxy(t *testing.T) {
	for _, mitOrigins := range []bool{false, true} {
		opt := Optionen{Version: "test"}
		if mitOrigins {
			opt.Origins = []string{"ssh.example.ch"}
		}
		ts, _, c, _ := angemeldeteApp(t, opt)
		u := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/ws?sitzung=abc"
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		ws, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPClient: c.h, HTTPHeader: http.Header{"Origin": {"https://ssh.example.ch"}}})
		cancel()
		if mitOrigins && err != nil {
			t.Fatalf("mit SSHAPP_ORIGINS abgelehnt: %v", err)
		}
		if !mitOrigins && err == nil {
			t.Fatal("fremde Origin ohne Freigabe angenommen")
		}
		if ws != nil {
			ws.Close(websocket.StatusNormalClosure, "")
		}
	}
}

// --- SICHERHEIT-11: HSTS nur auf Wunsch ---

func TestHSTSNurMitSchalter(t *testing.T) {
	for _, hsts := range []bool{false, true} {
		ts, _ := testAppMit(t, Optionen{Version: "test", SicheresCookie: true, HSTS: hsts})
		res, err := ts.Client().Get(ts.URL + "/api/status")
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if got := res.Header.Get("Strict-Transport-Security") != ""; got != hsts {
			t.Fatalf("HSTS=%v: Kopf gesetzt = %v", hsts, got)
		}
	}
}
