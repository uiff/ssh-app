package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/uiff/ssh-app/internal/store"
	"github.com/uiff/ssh-app/internal/vnc/testvnc"
)

// --- Hilfen ---

func testVNC(t *testing.T, o testvnc.Optionen) *testvnc.Server {
	t.Helper()
	s, err := testvnc.Starten(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// vncHost legt einen Host mit VNC an. addr ist die SSH-Adresse (Weg ssh) oder egal.
func vncHost(t *testing.T, c *client, addr string, mitZugang bool, felder map[string]any) store.Host {
	t.Helper()
	if addr == "" {
		addr = "127.0.0.1:22"
	}
	f := map[string]any{"vnc": true}
	for k, v := range felder {
		f[k] = v
	}
	return hostAnlegen(t, c, addr, mitZugang, f)
}

func vncOeffnen(t *testing.T, ts *httptest.Server, c *client, query string) *websocket.Conn {
	t.Helper()
	u := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/vnc?" + query
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPClient: c.h})
	if err != nil {
		t.Fatalf("VNC-WebSocket: %v", err)
	}
	conn.SetReadLimit(16 << 20) // Bildaktualisierungen kommen in Bloecken bis 64 KiB
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}

// fehlerErwartet liest bis zur Fehlermeldung und schliesst (sonst wartet der Server
// beim Schliessen auf die Antwort des Browsers).
func fehlerErwartet(t *testing.T, ws *websocket.Conn) nachricht {
	t.Helper()
	n, _ := vncBis(t, ws)
	ws.CloseNow()
	return n
}

// vncBis liest Text-Nachrichten, bestaetigt Host-Schluessel und liefert die erste
// Nachricht vom Typ bereit, fehler oder vnc-passwort (oder anmeldung).
func vncBis(t *testing.T, ws *websocket.Conn) (nachricht, []nachricht) {
	t.Helper()
	var alle []nachricht
	for {
		n := naechste(t, ws)
		alle = append(alle, n)
		switch n.T {
		case "hostkey":
			senden(t, ws, nachricht{T: "hostkey", OK: true})
		case "bereit", "fehler", "vnc-passwort", "anmeldung", "ende":
			return n, alle
		}
	}
}

// wsLeitung macht aus den Binaerframes eines WebSockets einen Datenstrom (Seite des
// Browsers). Text-Nachrichten merkt sie sich, das Schliessen auch.
type wsLeitung struct {
	ws   *websocket.Conn
	rest []byte

	mu      sync.Mutex
	texte   []nachricht
	schluss websocket.StatusCode
}

func (l *wsLeitung) Read(p []byte) (int, error) {
	for len(l.rest) == 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		mt, b, err := l.ws.Read(ctx)
		cancel()
		if err != nil {
			l.mu.Lock()
			l.schluss = websocket.CloseStatus(err)
			l.mu.Unlock()
			return 0, err
		}
		if mt == websocket.MessageText {
			var n nachricht
			json.Unmarshal(b, &n)
			l.mu.Lock()
			l.texte = append(l.texte, n)
			l.mu.Unlock()
			continue
		}
		l.rest = b
	}
	n := copy(p, l.rest)
	l.rest = l.rest[n:]
	return n, nil
}

func (l *wsLeitung) Write(p []byte) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := l.ws.Write(ctx, websocket.MessageBinary, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (l *wsLeitung) hatText(typ string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.ContainsFunc(l.texte, func(n nachricht) bool { return n.T == typ })
}

func (l *wsLeitung) text(typ string) nachricht {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, n := range l.texte {
		if n.T == typ {
			return n
		}
	}
	return nachricht{}
}

// betrachten macht nach «bereit» die Begruessung wie noVNC (Keyway spielt RFB 3.8 mit
// Typ None) und liest ServerInit des VNC-Servers.
func betrachten(t *testing.T, ws *websocket.Conn) (*testvnc.Betrachter, *wsLeitung) {
	t.Helper()
	l := &wsLeitung{ws: ws}
	b, err := testvnc.Begruessen(l, "")
	if err != nil {
		t.Fatalf("Begrüssung durch Keyway: %v", err)
	}
	if b.Version != "003.008" || !bytes.Equal(b.Typen, []byte{1}) {
		t.Fatalf("Keyway spielt nicht RFB 3.8 mit None: %+v", b)
	}
	if err := b.Init(); err != nil {
		t.Fatalf("ServerInit: %v", err)
	}
	return b, l
}

func warteBis(t *testing.T, frist time.Duration, was string, ok func() bool) {
	t.Helper()
	bis := time.Now().Add(frist)
	for !ok() {
		if time.Now().After(bis) {
			t.Fatalf("nach %s nicht erfüllt: %s", frist, was)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// keineVncGoroutinen: nach dem Abbau laeuft nichts mehr aus vnc.go.
func keineVncGoroutinen(t *testing.T) {
	t.Helper()
	warteBis(t, 3*time.Second, "VNC-Goroutinen beendet", func() bool {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		s := string(buf[:n])
		return !strings.Contains(s, "server.(*Server).vncWS") && !strings.Contains(s, "server.(*Server).vncWeiterleiten")
	})
}

// --- V1: Host-Felder ---

func TestVncHostFelder(t *testing.T) {
	_, quellApp, c, sk := angemeldeteApp(t, Optionen{Version: "test"})
	var h store.Host
	if code := c.rufe("POST", "/api/hosts", map[string]any{"name": "hmi", "adresse": "10.0.0.9", "vnc": true}, &h); code != 200 {
		t.Fatalf("anlegen: %d", code)
	}
	if !h.VNC || h.VncPort != 5900 || h.VncWeg != "ssh" || h.HatVncPasswort {
		t.Fatalf("Vorgaben: %+v", h)
	}
	for _, f := range []struct {
		felder map[string]any
		de, en string
	}{
		{map[string]any{"vncPort": 70000}, "VNC-Port muss zwischen 1 und 65535 liegen", "VNC port must be between 1 and 65535"},
		{map[string]any{"vncPort": -1}, "VNC-Port muss zwischen 1 und 65535 liegen", "VNC port must be between 1 and 65535"},
		{map[string]any{"vncWeg": "telnet"}, "Unbekannter VNC-Weg «telnet» (erlaubt: ssh, direkt)", "Unknown VNC connection type “telnet” (allowed: ssh, direkt)"},
	} {
		f.felder["name"], f.felder["adresse"] = "hmi", "10.0.0.9"
		rumpf, _ := json.Marshal(f.felder)
		if code, text := rufeMit(t, c, "PUT", fmt.Sprintf("/api/hosts/%d", h.ID), string(rumpf), nil); code != 400 || text != f.de {
			t.Errorf("%v deutsch: %d %q", f.felder, code, text)
		}
		if code, text := rufeMit(t, c, "PUT", fmt.Sprintf("/api/hosts/%d", h.ID), string(rumpf), englisch); code != 400 || text != f.en {
			t.Errorf("%v englisch: %d %q", f.felder, code, text)
		}
	}

	// Passwort setzen: verschluesselt, nie in einer Antwort.
	var roh map[string]any
	if code := c.rufe("PUT", fmt.Sprintf("/api/hosts/%d", h.ID), map[string]any{"name": "hmi", "adresse": "10.0.0.9", "vnc": true,
		"vncPort": 5901, "vncWeg": "direkt", "vncPasswort": "pw-geheim-42"}, &roh); code != 200 || roh["hatVncPasswort"] != true {
		t.Fatalf("Passwort setzen: %d %v", code, roh)
	}
	if strings.Contains(fmt.Sprint(roh), "pw-geheim-42") || roh["vncPasswort"] != nil {
		t.Fatalf("Passwort in der Antwort: %v", roh)
	}
	var d struct{ Hosts []map[string]any }
	c.rufe("GET", "/api/daten", nil, &d)
	if strings.Contains(fmt.Sprint(d), "pw-geheim-42") || d.Hosts[0]["hatVncPasswort"] != true || d.Hosts[0]["vncPort"] != 5901.0 || d.Hosts[0]["vncWeg"] != "direkt" {
		t.Fatalf("Daten: %v", d)
	}
	gespeichert, _ := quellApp.st.Host(h.ID)
	if len(gespeichert.VncPasswortEnc) == 0 || bytes.Contains(gespeichert.VncPasswortEnc, []byte("pw-geheim-42")) {
		t.Fatalf("Passwort nicht verschlüsselt: %q", gespeichert.VncPasswortEnc)
	}
	// Ohne Angabe bleibt es; hatVncPasswort vom Browser zaehlt nicht.
	if c.rufe("PUT", fmt.Sprintf("/api/hosts/%d", h.ID), map[string]any{"name": "hmi 2", "adresse": "10.0.0.9", "vnc": true, "vncWeg": "direkt",
		"hatVncPasswort": false}, &roh); roh["hatVncPasswort"] != true || roh["vncPort"] != 5900.0 {
		t.Fatalf("Passwort ohne Angabe verloren: %v", roh)
	}
	backup := backupLaden(t, c)
	if bytes.Contains(backup, []byte("pw-geheim-42")) {
		t.Fatal("Backup enthält das VNC-Passwort im Klartext")
	}
	if c.rufe("PUT", fmt.Sprintf("/api/hosts/%d", h.ID), map[string]any{"name": "hmi", "adresse": "10.0.0.9", "vncPasswortEntfernen": true}, &roh); roh["hatVncPasswort"] != false || roh["vnc"] != false {
		t.Fatalf("Passwort entfernen: %v", roh)
	}

	// Rundlauf: Felder und verschluesseltes Passwort reisen mit.
	ts2, _ := testApp(t)
	c2 := neuerClient(t, ts2.URL, ts2.Client())
	if code, text := einspielenAn(c2, "/api/einspielen", backup, sk); code != 200 {
		t.Fatalf("Einspielen: %d %s", code, text)
	}
	c2.rufe("POST", "/api/anmelden", map[string]string{"benutzer": "admin", "passwort": testPasswort}, nil)
	var d2 struct{ Hosts []store.Host }
	c2.rufe("GET", "/api/daten", nil, &d2)
	if len(d2.Hosts) != 1 || !d2.Hosts[0].VNC || d2.Hosts[0].VncWeg != "direkt" || !d2.Hosts[0].HatVncPasswort {
		t.Fatalf("nach Rundlauf: %+v", d2.Hosts)
	}

	// Altes Backup ohne VNC-Felder und eines mit unbekanntem Weg.
	umgebaut := backupUmbauen(t, quellApp, backup, sk, func(sg map[string]any) {
		hosts := sg["hosts"].([]any)
		for _, k := range []string{"vnc", "vncPort", "vncWeg", "vncPasswortEnc", "hatVncPasswort"} {
			delete(hosts[0].(map[string]any), k)
		}
		sg["hosts"] = append(hosts, map[string]any{"id": 77, "name": "neu", "adresse": "10.0.0.7", "vnc": true, "vncWeg": "zukunft", "vncPort": 99999})
	})
	ts3, _ := testApp(t)
	c3 := neuerClient(t, ts3.URL, ts3.Client())
	if code, text := einspielenAn(c3, "/api/einspielen", umgebaut, sk); code != 200 {
		t.Fatalf("altes Backup: %d %s", code, text)
	}
	c3.rufe("POST", "/api/anmelden", map[string]string{"benutzer": "admin", "passwort": testPasswort}, nil)
	var d3 struct{ Hosts []store.Host }
	c3.rufe("GET", "/api/daten", nil, &d3)
	if len(d3.Hosts) != 2 {
		t.Fatalf("Hosts: %+v", d3.Hosts)
	}
	alt, neu := d3.Hosts[0], d3.Hosts[1]
	if alt.VNC || alt.VncPort != 5900 || alt.VncWeg != "ssh" || alt.HatVncPasswort {
		t.Fatalf("altes Backup: %+v", alt)
	}
	if !neu.VNC || neu.VncWeg != "ssh" || neu.VncPort != 5900 {
		t.Fatalf("fremder Weg/Port: %+v", neu)
	}
}

// --- V2: Ende zu Ende, Weg direkt ---

func TestVncDirektEndeZuEnde(t *testing.T) {
	tv := testVNC(t, testvnc.Optionen{Name: "Direkt-HMI"})
	ts, _, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	h := vncHost(t, c, "", false, map[string]any{"name": "hmi", "vncPort": tv.Port(), "vncWeg": "direkt"})

	ws := vncOeffnen(t, ts, c, fmt.Sprintf("host=%d&sprache=de", h.ID))
	n, alle := vncBis(t, ws)
	if n.T != "bereit" {
		t.Fatalf("statt bereit: %+v", alle)
	}
	if alle[0].T != "status" || alle[0].Text != "Verbinde mit "+tv.Adresse()+" (VNC) …" {
		t.Fatalf("Status: %+v", alle[0])
	}
	if n.Weg != "direkt" || n.Durchreichen == nil || *n.Durchreichen || n.HostID != h.ID || n.Titel != "hmi" {
		t.Fatalf("bereit: %+v", n)
	}
	b, l := betrachten(t, ws)
	if b.Name != "Direkt-HMI" || b.Breite != 1024 || b.Hoehe != 768 {
		t.Fatalf("ServerInit des Test-Servers: %+v", b)
	}
	b.Kodierungen(0, -308, -239)
	b.BildAnfordern(false)
	r, err := b.Aktualisierung()
	if err != nil {
		t.Fatal(err)
	}
	if i := slices.IndexFunc(r, func(x testvnc.Rechteck) bool { return x.Kodierung == 0 }); i < 0 || r[i].B != 1024 || r[i].H != 768 {
		t.Fatalf("Bild: %+v", r)
	}
	b.Zeiger(1, 500, 600)
	b.Zeiger(0, 500, 600)
	b.Taste(true, 'k')
	warteBis(t, 3*time.Second, "Klick und Taste beim VNC-Server", func() bool {
		ev := strings.Join(tv.Ereignisse(), "|")
		return strings.Contains(ev, "maus links 500,600") && strings.Contains(ev, "taste 6b k")
	})
	if l.hatText("fehler") {
		t.Fatal("Textnachricht im Datenstrom")
	}

	// Abbau: WebSocket zu => TCP zum VNC-Server zu, keine Goroutinen uebrig.
	ws.Close(websocket.StatusNormalClosure, "")
	warteBis(t, 3*time.Second, "TCP zum VNC-Server zu", func() bool { return tv.Offen.Load() == 0 })
	keineVncGoroutinen(t)

	// VNC-Server beendet: «ende» mit Text, dann Schliessen.
	ws2 := vncOeffnen(t, ts, c, fmt.Sprintf("host=%d&sprache=en", h.ID))
	if n, alle := vncBis(t, ws2); n.T != "bereit" {
		t.Fatalf("statt bereit: %+v", alle)
	}
	_, l2 := betrachten(t, ws2)
	tv.Close()
	io.Copy(io.Discard, l2)
	if e := l2.text("ende"); e.Text != "The VNC server closed the connection" {
		t.Fatalf("Ende: %+v (%v)", e, l2.texte)
	}
	if l2.schluss != websocket.StatusNormalClosure {
		t.Fatalf("Schliesscode %d", l2.schluss)
	}
	keineVncGoroutinen(t)
}

// --- V2: Weg ssh (Tunnel ueber direct-tcpip) ---

func TestVncSSHTunnel(t *testing.T) {
	tv := testVNC(t, testvnc.Optionen{Name: "Hinter-SSH", Passwort: "vnc-pw"})
	srv := sshTestServerMit(t, sshOptionen{benutzer: "pi", passwort: "himbeere"})
	ts, _, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	h := vncHost(t, c, srv.addr, true, map[string]any{"name": "pi-desktop", "vncPort": tv.Port(), "vncPasswort": "vnc-pw"})
	if h.VncWeg != "ssh" {
		t.Fatalf("Vorgabe-Weg: %q", h.VncWeg)
	}
	ws := vncOeffnen(t, ts, c, fmt.Sprintf("host=%d", h.ID))
	n, alle := vncBis(t, ws)
	if n.T != "bereit" || n.Weg != "ssh" || !strings.HasPrefix(n.FP, "SHA256:") {
		t.Fatalf("bereit: %+v (%+v)", n, alle)
	}
	if !slices.ContainsFunc(alle, func(x nachricht) bool {
		return x.T == "status" && x.Text == fmt.Sprintf("Öffne den VNC-Tunnel über SSH zu Port %d …", tv.Port())
	}) {
		t.Fatalf("Status Tunnel fehlt: %+v", alle)
	}
	b, _ := betrachten(t, ws)
	if b.Name != "Hinter-SSH" {
		t.Fatalf("ServerInit: %+v", b)
	}
	if z := srv.weiterleitungsZiele(); !slices.Equal(z, []string{fmt.Sprintf("127.0.0.1:%d", tv.Port())}) {
		t.Fatalf("Tunnelziel: %v", z)
	}
	ws.Close(websocket.StatusNormalClosure, "")
	warteBis(t, 3*time.Second, "Tunnel und TCP zu", func() bool { return tv.Offen.Load() == 0 && srv.tunnel.Load() == 0 })
	keineVncGoroutinen(t)

	// SSH-Server ohne Weiterleitung, Port ohne VNC-Server.
	verboten := sshTestServerMit(t, sshOptionen{benutzer: "pi", passwort: "himbeere", ohneWeiterleitung: true})
	h2 := vncHost(t, c, verboten.addr, true, map[string]any{"name": "gesperrt", "vncPort": tv.Port()})
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	frei := l.Addr().(*net.TCPAddr).Port
	l.Close()
	h3 := vncHost(t, c, srv.addr, true, map[string]any{"name": "ohne-vnc", "vncPort": frei})
	for _, f := range []struct {
		id     int64
		de, en string
	}{
		{h2.ID, "Der SSH-Server von gesperrt erlaubt keine Weiterleitung (AllowTcpForwarding) — im Host den VNC-Weg «direkt» wählen oder die Weiterleitung freigeben",
			"The SSH server on gesperrt doesn’t allow forwarding (AllowTcpForwarding) — choose the VNC connection type “direct” for the host or allow forwarding"},
		{h3.ID, fmt.Sprintf("Auf ohne-vnc antwortet kein VNC-Server auf Port %d (lokal über SSH geprüft)", frei),
			fmt.Sprintf("No VNC server is responding on ohne-vnc, port %d (checked locally over SSH)", frei)},
	} {
		for sprache, will := range map[string]string{"de": f.de, "en": f.en} {
			ws := vncOeffnen(t, ts, c, fmt.Sprintf("host=%d&sprache=%s", f.id, sprache))
			if n := fehlerErwartet(t, ws); n.T != "fehler" || n.Text != will {
				t.Errorf("%s: %+v", sprache, n)
			}
		}
	}
	keineVncGoroutinen(t)
}

// --- V2: VNC-Passwort: Rueckfrage, falsch, speichern nur nach Erfolg ---

func TestVncPasswortRueckfrage(t *testing.T) {
	tv := testVNC(t, testvnc.Optionen{Passwort: "richtig"})
	ts, s, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	h := vncHost(t, c, "", false, map[string]any{"name": "panel", "vncPort": tv.Port(), "vncWeg": "direkt"})
	hatPasswort := func() bool {
		x, _ := s.st.Host(h.ID)
		return len(x.VncPasswortEnc) > 0
	}
	oeffnen := func(sprache string) *websocket.Conn {
		return vncOeffnen(t, ts, c, fmt.Sprintf("host=%d&sprache=%s", h.ID, sprache))
	}

	// Fehlt das Passwort: Rueckfrage ohne Fehler. Falsch mit «speichern»: neue Rueckfrage
	// mit Fehler, nichts gespeichert. Abbrechen: Fehlertext.
	ws := oeffnen("de")
	n, _ := vncBis(t, ws)
	if n.T != "vnc-passwort" || n.Ziel != "panel" || n.Fehler != "" {
		t.Fatalf("Rückfrage: %+v", n)
	}
	senden(t, ws, nachricht{T: "vnc-passwort", Passwort: "falsch", Speichern: true})
	if n, _ = vncBis(t, ws); n.T != "vnc-passwort" || n.Fehler != "VNC-Passwort falsch" {
		t.Fatalf("nach falschem Passwort: %+v", n)
	}
	senden(t, ws, nachricht{T: "vnc-passwort", Abbrechen: true})
	if n = fehlerErwartet(t, ws); n.T != "fehler" || n.Text != "Anmeldung am VNC-Server abgebrochen — «Neu verbinden», um es erneut zu versuchen." {
		t.Fatalf("Abbruch: %+v", n)
	}
	if hatPasswort() {
		t.Fatal("falsches Passwort gespeichert")
	}

	// Richtig ohne «speichern»: verbunden, nichts gespeichert (englische Texte).
	ws = oeffnen("en")
	vncBis(t, ws)
	senden(t, ws, nachricht{T: "vnc-passwort", Passwort: "falsch"})
	if n, _ = vncBis(t, ws); n.Fehler != "Incorrect VNC password" {
		t.Fatalf("englisch: %+v", n)
	}
	senden(t, ws, nachricht{T: "vnc-passwort", Passwort: "richtig"})
	if n, _ = vncBis(t, ws); n.T != "bereit" || n.Gespeichert {
		t.Fatalf("ohne speichern: %+v", n)
	}
	betrachten(t, ws)
	ws.Close(websocket.StatusNormalClosure, "")
	if hatPasswort() {
		t.Fatal("ohne «speichern» gespeichert")
	}

	// Richtig mit «speichern»: gespeichert; beim naechsten Mal keine Rueckfrage.
	ws = oeffnen("de")
	vncBis(t, ws)
	senden(t, ws, nachricht{T: "vnc-passwort", Passwort: "richtig", Speichern: true})
	if n, _ = vncBis(t, ws); n.T != "bereit" || !n.Gespeichert {
		t.Fatalf("mit speichern: %+v", n)
	}
	ws.Close(websocket.StatusNormalClosure, "")
	if !hatPasswort() {
		t.Fatal("nicht gespeichert")
	}
	ws = oeffnen("de")
	if n, alle := vncBis(t, ws); n.T != "bereit" || n.Gespeichert {
		t.Fatalf("mit gespeichertem Passwort: %+v", alle)
	}
	betrachten(t, ws)
	ws.Close(websocket.StatusNormalClosure, "")

	// Gespeichertes Passwort falsch: Rueckfrage mit Fehler; das neue ersetzt es.
	c.rufe("PUT", fmt.Sprintf("/api/hosts/%d", h.ID), map[string]any{"name": "panel", "adresse": "127.0.0.1", "port": 22, "vnc": true,
		"vncPort": tv.Port(), "vncWeg": "direkt", "vncPasswort": "veraltet"}, nil)
	ws = oeffnen("de")
	if n, _ = vncBis(t, ws); n.T != "vnc-passwort" || n.Fehler != "VNC-Passwort falsch" {
		t.Fatalf("gespeichertes falsch: %+v", n)
	}
	senden(t, ws, nachricht{T: "vnc-passwort", Passwort: "richtig", Speichern: true})
	if n, _ = vncBis(t, ws); n.T != "bereit" || !n.Gespeichert {
		t.Fatalf("ersetzt: %+v", n)
	}
	ws.Close(websocket.StatusNormalClosure, "")
	x, _ := s.st.Host(h.ID)
	if klar, _ := s.tr.Entschluesseln(x.VncPasswortEnc); string(klar) != "richtig" {
		t.Fatalf("gespeichert: %q", klar)
	}

	// Zu viele Fehlversuche.
	c.rufe("PUT", fmt.Sprintf("/api/hosts/%d", h.ID), map[string]any{"name": "panel", "adresse": "127.0.0.1", "port": 22, "vnc": true,
		"vncPort": tv.Port(), "vncWeg": "direkt", "vncPasswortEntfernen": true}, nil)
	ws = oeffnen("de")
	vncBis(t, ws)
	for {
		senden(t, ws, nachricht{T: "vnc-passwort", Passwort: "nein"})
		n, _ = vncBis(t, ws)
		if n.T != "vnc-passwort" {
			break
		}
	}
	ws.CloseNow()
	if n.T != "fehler" || n.Text != "Zu viele falsche VNC-Passwörter — Verbindung beendet" {
		t.Fatalf("zu viele: %+v", n)
	}
	if strings.Count(strings.Join(tv.Ereignisse(), "|"), "anmeldung falsch") < vncMaxFehlversuche {
		t.Fatalf("Ereignisse: %v", tv.Ereignisse())
	}
	keineVncGoroutinen(t)
}

// --- V2: Durchreich-Modus bei VeNCrypt ---

func TestVncDurchreichen(t *testing.T) {
	tv := testVNC(t, testvnc.Optionen{Typ: "vencrypt", Passwort: "plain-geheim", Name: "Mit-VeNCrypt"})
	ts, s, c, _ := angemeldeteApp(t, Optionen{Version: "test", Leerlauf: 1500 * time.Millisecond})
	h := vncHost(t, c, "", false, map[string]any{"name": "neu-vnc", "vncPort": tv.Port(), "vncWeg": "direkt"})
	ws := vncOeffnen(t, ts, c, fmt.Sprintf("host=%d", h.ID))
	n, alle := vncBis(t, ws)
	if n.T != "bereit" || n.Durchreichen == nil || !*n.Durchreichen {
		t.Fatalf("bereit: %+v", alle)
	}
	// Ab dem ersten Byte roh: Version, dann (nach der Version des Browsers) die Typenliste.
	l := &wsLeitung{ws: ws}
	kopf := make([]byte, 14)
	if _, err := io.ReadFull(l, kopf[:12]); err != nil || string(kopf[:12]) != "RFB 003.008\n" {
		t.Fatalf("Version: %q %v", kopf, err)
	}
	l.Write([]byte("RFB 003.008\n"))
	if _, err := io.ReadFull(l, kopf[12:]); err != nil || string(kopf[12:]) != "\x01\x13" {
		t.Fatalf("Typen: %q %v", kopf, err)
	}
	l.Write([]byte{19})
	v := make([]byte, 2)
	io.ReadFull(l, v)
	l.Write([]byte{0, 2})
	ack := make([]byte, 6)
	io.ReadFull(l, ack)
	l.Write([]byte{0, 0, 1, 0})
	pw := "plain-geheim"
	l.Write(binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, 2), uint32(len(pw))))
	l.Write([]byte("ui" + pw))
	ok := make([]byte, 4)
	if io.ReadFull(l, ok); !bytes.Equal(ok, []byte{0, 0, 0, 0}) {
		t.Fatalf("SecurityResult: %v", ok)
	}
	b := testvnc.NeuerBetrachter(l)
	if err := b.Init(); err != nil || b.Name != "Mit-VeNCrypt" {
		t.Fatalf("ServerInit: %v %+v", err, b)
	}
	if x, _ := s.st.Host(h.ID); len(x.VncPasswortEnc) > 0 {
		t.Fatal("Passwort beim Durchreichen gespeichert")
	}
	// Beim Durchreichen zaehlt jeder Block als Bedienung.
	bis := time.Now().Add(2500 * time.Millisecond)
	for time.Now().Before(bis) {
		b.BildAnfordern(true)
		time.Sleep(250 * time.Millisecond)
	}
	var st struct{ Angemeldet bool }
	if c.rufe("GET", "/api/status", nil, &st); !st.Angemeldet {
		t.Fatal("Durchreichen verlängert die Anmeldung nicht")
	}
	ws.Close(websocket.StatusNormalClosure, "")
	warteBis(t, 3*time.Second, "TCP zu", func() bool { return tv.Offen.Load() == 0 })
	keineVncGoroutinen(t)
}

// --- V2: kein SSRF ---

func TestVncKeinFremdesZiel(t *testing.T) {
	erlaubt := testVNC(t, testvnc.Optionen{Name: "Erlaubt"})
	fremd := testVNC(t, testvnc.Optionen{Name: "Fremd"})
	srv := sshTestServerMit(t, sshOptionen{benutzer: "pi", passwort: "himbeere"})
	ts, _, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	direkt := vncHost(t, c, "", false, map[string]any{"name": "direkt", "vncPort": erlaubt.Port(), "vncWeg": "direkt"})
	tunnel := vncHost(t, c, srv.addr, true, map[string]any{"name": "tunnel", "vncPort": erlaubt.Port()})
	aus := hostAnlegen(t, c, "127.0.0.1:22", false, map[string]any{"name": "ohne", "vncPort": erlaubt.Port(), "vncWeg": "direkt"})

	fp := fremd.Port()
	extra := fmt.Sprintf("&adresse=127.0.0.1&port=%d&vncPort=%d&vncWeg=direkt&weg=direkt&ziel=127.0.0.1:%d&host=%d", fp, fp, fp, aus.ID)
	for _, h := range []store.Host{direkt, tunnel} {
		// Der erste Parameter «host» gilt; alles andere wird nicht beachtet.
		ws := vncOeffnen(t, ts, c, fmt.Sprintf("host=%d%s", h.ID, extra))
		if n, alle := vncBis(t, ws); n.T != "bereit" || n.HostID != h.ID {
			t.Fatalf("%s: %+v", h.Name, alle)
		}
		b, _ := betrachten(t, ws)
		if b.Name != "Erlaubt" {
			t.Fatalf("%s: verbunden mit %q", h.Name, b.Name)
		}
		ws.Close(websocket.StatusNormalClosure, "")
	}
	if fremd.Verbindungen.Load() != 0 {
		t.Fatal("fremdes Ziel erreicht")
	}
	if z := srv.weiterleitungsZiele(); !slices.Equal(z, []string{fmt.Sprintf("127.0.0.1:%d", erlaubt.Port())}) {
		t.Fatalf("Tunnelziele: %v", z)
	}
	// VNC aus und unbekannter Host.
	for q, will := range map[string]string{
		fmt.Sprintf("host=%d&adresse=127.0.0.1&vncPort=%d", aus.ID, fp): "VNC isn’t turned on for “ohne” — turn it on in the host dialog",
		"host=999": "Host not found",
		"host=abc": "Host not found",
	} {
		ws := vncOeffnen(t, ts, c, q+"&sprache=en")
		if n := fehlerErwartet(t, ws); n.T != "fehler" || n.Text != will {
			t.Errorf("%s: %+v", q, n)
		}
	}
	if fremd.Verbindungen.Load() != 0 {
		t.Fatal("fremdes Ziel erreicht")
	}
	// Ohne Anmeldung: kein WebSocket.
	anon := neuerClient(t, ts.URL, ts.Client())
	u := "ws" + strings.TrimPrefix(ts.URL, "http") + fmt.Sprintf("/api/vnc?host=%d", direkt.ID)
	if _, res, err := websocket.Dial(context.Background(), u, &websocket.DialOptions{HTTPClient: anon.h}); err == nil || res == nil || res.StatusCode != 401 {
		t.Fatalf("ohne Anmeldung: %v %v", err, res)
	}
	// Fremde Origin: abgelehnt.
	if _, _, err := websocket.Dial(context.Background(), u, &websocket.DialOptions{HTTPClient: c.h,
		HTTPHeader: map[string][]string{"Origin": {"https://boese.example"}}}); err == nil {
		t.Fatal("fremde Origin angenommen")
	}
	keineVncGoroutinen(t)
}

// --- V2: Leerlauf: Eingaben sind Bedienung, Bildanforderungen nicht ---

func TestVncLeerlauf(t *testing.T) {
	tv := testVNC(t, testvnc.Optionen{})
	ts, _, c, _ := angemeldeteApp(t, Optionen{Version: "test", Leerlauf: 1500 * time.Millisecond})
	h := vncHost(t, c, "", false, map[string]any{"vncPort": tv.Port(), "vncWeg": "direkt"})
	ws := vncOeffnen(t, ts, c, fmt.Sprintf("host=%d", h.ID))
	if n, alle := vncBis(t, ws); n.T != "bereit" {
		t.Fatalf("%+v", alle)
	}
	b, l := betrachten(t, ws)
	b.Kodierungen(0)
	gelesen := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, l)
		gelesen <- err
	}()
	bis := time.Now().Add(2500 * time.Millisecond)
	for i := 0; time.Now().Before(bis); i++ {
		b.Zeiger(0, 10+i, 10)
		time.Sleep(250 * time.Millisecond)
	}
	select {
	case err := <-gelesen:
		t.Fatalf("trotz Mausbewegung geschlossen: %v", err)
	default:
	}
	// Nur noch Bildanforderungen (der Viewer laeuft, niemand bedient): Abmeldung.
	start := time.Now()
	for {
		select {
		case err := <-gelesen:
			if d := time.Since(start); d < time.Second {
				t.Fatalf("zu früh abgemeldet (%s): %v %+v", d, err, l.texte)
			}
			if l.schluss != wsAbgemeldet || !l.hatText("abgemeldet") {
				t.Fatalf("Schluss %d, Texte %+v", l.schluss, l.texte)
			}
			var st struct{ Angemeldet bool }
			if c.rufe("GET", "/api/status", nil, &st); st.Angemeldet {
				t.Fatal("noch angemeldet")
			}
			warteBis(t, 3*time.Second, "TCP zu", func() bool { return tv.Offen.Load() == 0 })
			keineVncGoroutinen(t)
			return
		case <-time.After(250 * time.Millisecond):
			b.BildAnfordern(true)
		}
		if time.Since(start) > 5*time.Second {
			t.Fatal("nach Leerlauf nicht geschlossen")
		}
	}
}

// --- Fehler in beiden Sprachen ---

func TestVncFehlerInDerSpracheDesBrowsers(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().(*net.TCPAddr)
	l.Close()
	ts, _, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	h := vncHost(t, c, "", false, map[string]any{"vncPort": addr.Port, "vncWeg": "direkt"})
	for sprache, will := range map[string][2]string{
		"de": {"Verbinde mit " + addr.String() + " (VNC) …", "Verbindung abgelehnt — läuft auf " + addr.String() + " ein VNC-Server?"},
		"en": {"Connecting to " + addr.String() + " (VNC)…", "Connection refused — is a VNC server running on " + addr.String() + "?"},
	} {
		ws := vncOeffnen(t, ts, c, fmt.Sprintf("host=%d&sprache=%s", h.ID, sprache))
		if n := naechste(t, ws); n.T != "status" || n.Text != will[0] {
			t.Errorf("%s Status: %+v", sprache, n)
		}
		if n := fehlerErwartet(t, ws); n.T != "fehler" || n.Text != will[1] {
			t.Errorf("%s Fehler: %+v", sprache, n)
		}
	}
	// Kein VNC-Dienst: ein SSH-Server auf dem VNC-Port.
	srv := sshTestServer(t, "pi", "himbeere")
	_, port, _ := net.SplitHostPort(srv)
	var p int
	fmt.Sscan(port, &p)
	h3 := vncHost(t, c, "", false, map[string]any{"name": "falscher-dienst", "vncPort": p, "vncWeg": "direkt"})
	ws := vncOeffnen(t, ts, c, fmt.Sprintf("host=%d&sprache=en", h3.ID))
	if n := fehlerErwartet(t, ws); n.T != "fehler" || n.Text != "127.0.0.1:"+port+" doesn’t speak VNC (RFB)" {
		t.Errorf("kein VNC: %+v", n)
	}
	keineVncGoroutinen(t)
}

// --- Lebenszeichen: Keyway pingt den Browser, auch waehrend das Bild still steht ---

func TestVncLebenszeichen(t *testing.T) {
	alt := vncLebenszeichen
	vncLebenszeichen = 50 * time.Millisecond
	t.Cleanup(func() { vncLebenszeichen = alt })
	tv := testVNC(t, testvnc.Optionen{})
	ts, _, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	h := vncHost(t, c, "", false, map[string]any{"vncPort": tv.Port(), "vncWeg": "direkt"})
	n := 0
	var mu sync.Mutex
	u := "ws" + strings.TrimPrefix(ts.URL, "http") + fmt.Sprintf("/api/vnc?host=%d", h.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPClient: c.h, OnPingReceived: func(context.Context, []byte) bool {
		mu.Lock()
		n++
		mu.Unlock()
		return true
	}})
	if err != nil {
		t.Fatal(err)
	}
	ws.SetReadLimit(16 << 20)
	t.Cleanup(func() { ws.CloseNow() })
	if m, alle := vncBis(t, ws); m.T != "bereit" {
		t.Fatalf("statt bereit: %+v", alle)
	}
	b, l := betrachten(t, ws)
	b.Kodierungen(0)
	go io.Copy(io.Discard, l) // liest weiter (nur dann kommen die Pings an)
	warteBis(t, 3*time.Second, "mindestens drei Pings nach «bereit»", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return n >= 3
	})
	ws.Close(websocket.StatusNormalClosure, "")
	warteBis(t, 3*time.Second, "TCP zum VNC-Server zu", func() bool { return tv.Offen.Load() == 0 })
	keineVncGoroutinen(t)
}
