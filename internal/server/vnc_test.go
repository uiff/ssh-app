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
	"net/url"
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
		{map[string]any{"vncWeg": "telnet"}, "Unbekannte VNC-Verbindung «telnet» (erlaubt: ssh, direkt)", "Unknown VNC connection type “telnet” (allowed: ssh, direkt)"},
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
		{h2.ID, "Der SSH-Server von gesperrt erlaubt keine Weiterleitung (AllowTcpForwarding) — im Host-Dialog unter «Verbindung» «Direkt» wählen oder die Weiterleitung freigeben",
			"The SSH server on gesperrt doesn’t allow forwarding (AllowTcpForwarding) — choose “Direct” under “Connection” in the host dialog or allow forwarding"},
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
	// Beim Durchreichen ist der Strom nicht lesbar: Binaerframes zaehlen nie als Bedienung,
	// nur {t:"bedient"} der Seite bei echter Eingabe.
	gelesen := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, l)
		gelesen <- err
	}()
	bis := time.Now().Add(2500 * time.Millisecond)
	for time.Now().Before(bis) {
		b.BildAnfordern(true)
		senden(t, ws, nachricht{T: "bedient"})
		time.Sleep(250 * time.Millisecond)
	}
	var st struct{ Angemeldet bool }
	if c.rufe("GET", "/api/status", nil, &st); !st.Angemeldet {
		t.Fatal("«bedient» verlängert die Anmeldung beim Durchreichen nicht")
	}
	// Nur noch Bildanforderungen und Zwischenablage (laufender Viewer, niemand bedient):
	// Leerlauf wie beim reinen Ansehen, die Anmeldung laeuft ab.
	start := time.Now()
	for {
		select {
		case err := <-gelesen:
			if d := time.Since(start); d < 250*time.Millisecond { // Drosselung wie in TestVncLeerlauf
				t.Fatalf("zu früh abgemeldet (%s): %v", d, err)
			}
			if l.schluss != wsAbgemeldet || !l.hatText("abgemeldet") {
				t.Fatalf("Schluss %d, Texte %+v", l.schluss, l.texte)
			}
			if c.rufe("GET", "/api/status", nil, &st); st.Angemeldet {
				t.Fatal("trotz Leerlauf noch angemeldet")
			}
			warteBis(t, 3*time.Second, "TCP zu", func() bool { return tv.Offen.Load() == 0 })
			keineVncGoroutinen(t)
			return
		case <-time.After(250 * time.Millisecond):
			b.BildAnfordern(true)
			b.AblageSenden("automatisch")
		}
		if time.Since(start) > 5*time.Second {
			t.Fatal("Binärframes verlängern die Anmeldung beim Durchreichen")
		}
	}
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

// --- V2: Leerlauf: Eingaben und «bedient» sind Bedienung; Bildanforderungen und die
// Zwischenablage (noVNC antwortet dort von selbst) nicht ---

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
	// Einfuegen meldet die Seite mit «bedient» (die Zwischenablage selbst zaehlt nicht).
	bis := time.Now().Add(2500 * time.Millisecond)
	for time.Now().Before(bis) {
		senden(t, ws, nachricht{T: "bedient"})
		b.AblageSenden("eingefügt")
		time.Sleep(250 * time.Millisecond)
	}
	select {
	case err := <-gelesen:
		t.Fatalf("trotz «bedient» geschlossen: %v", err)
	default:
	}
	bis = time.Now().Add(2500 * time.Millisecond)
	for i := 0; time.Now().Before(bis); i++ {
		b.Zeiger(0, 10+i, 10)
		time.Sleep(250 * time.Millisecond)
	}
	select {
	case err := <-gelesen:
		t.Fatalf("trotz Mausbewegung geschlossen: %v", err)
	default:
	}
	// Nur noch Bildanforderungen und automatische Antworten der Zwischenablage (der Viewer
	// laeuft, niemand bedient): Abmeldung.
	caps := []byte{6, 0, 0, 0, 0xff, 0xff, 0xff, 0xf8, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x10, 0x00}
	start := time.Now()
	for {
		select {
		case err := <-gelesen:
			// bedient() vermerkt hoechstens einmal je Sekunde: die letzte Bedienung liegt
			// bis 1,25 s zurueck, die Abmeldung kommt also fruehestens 0,25 s nach dem Wechsel.
			if d := time.Since(start); d < 250*time.Millisecond {
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
			l.Write(caps)
			b.AblageSenden("automatisch")
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

// --- Blockierende Gegenueber: Fristen, Speichergrenze, Abbau ---

// stummerVNC ist ein VNC-Server (RFB 3.8, Typ None), der nach ServerInit nichts mehr liest.
// Mit fluten schickt er ununterbrochen Daten (wie Bildaktualisierungen).
type stummerVNC struct {
	l      net.Listener
	fluten bool
	conns  chan net.Conn
}

func stummerVNCStarten(t *testing.T, fluten bool) *stummerVNC {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	sv := &stummerVNC{l: l, fluten: fluten, conns: make(chan net.Conn, 4)}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { c.Close() })
			go sv.begruessen(c)
		}
	}()
	return sv
}

func (sv *stummerVNC) port() int { return sv.l.Addr().(*net.TCPAddr).Port }

func (sv *stummerVNC) begruessen(c net.Conn) {
	lies := func(n int) bool { _, err := io.ReadFull(c, make([]byte, n)); return err == nil }
	if _, err := c.Write([]byte("RFB 003.008\n")); err != nil || !lies(12) {
		return
	}
	if _, err := c.Write([]byte{1, 1}); err != nil || !lies(1) {
		return
	}
	if _, err := c.Write([]byte{0, 0, 0, 0}); err != nil || !lies(1) { // SecurityResult, ClientInit
		return
	}
	init := []byte{4, 0, 3, 0, 32, 24, 0, 1, 0, 255, 0, 255, 0, 255, 16, 8, 0, 0, 0, 0}
	init = binary.BigEndian.AppendUint32(init, 5)
	if _, err := c.Write(append(init, "Stumm"...)); err != nil {
		return
	}
	sv.conns <- c
	if sv.fluten {
		block := make([]byte, 64<<10)
		for {
			if _, err := c.Write(block); err != nil {
				return
			}
		}
	}
}

// zu wartet, bis Keyway die Verbindung geschlossen hat (liest dazu, was noch ansteht).
func (sv *stummerVNC) zu(t *testing.T, c net.Conn, frist time.Duration) {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(frist))
	if _, err := io.Copy(io.Discard, c); err != nil {
		t.Fatalf("Keyway hält die Verbindung zum VNC-Server offen: %v", err)
	}
}

// stummVerbinden oeffnet den Bildschirm eines stummen Servers bis nach ServerInit.
func stummVerbinden(t *testing.T, ts *httptest.Server, c *client, sv *stummerVNC, sprache string) (*websocket.Conn, *wsLeitung, net.Conn) {
	t.Helper()
	h := vncHost(t, c, "", false, map[string]any{"name": "stumm", "vncPort": sv.port(), "vncWeg": "direkt"})
	ws := vncOeffnen(t, ts, c, fmt.Sprintf("host=%d&sprache=%s", h.ID, sprache))
	if n, alle := vncBis(t, ws); n.T != "bereit" {
		t.Fatalf("statt bereit: %+v", alle)
	}
	b, l := betrachten(t, ws)
	if b.Name != "Stumm" {
		t.Fatalf("ServerInit: %+v", b)
	}
	select {
	case conn := <-sv.conns:
		return ws, l, conn
	case <-time.After(3 * time.Second):
		t.Fatal("stummer Server nicht verbunden")
	}
	return nil, nil, nil
}

// ablage baut ein ClientCutText mit n Bytes (wie ein grosses Einfuegen).
func ablage(n int) []byte {
	m := binary.BigEndian.AppendUint32([]byte{6, 0, 0, 0}, uint32(n))
	return append(m, bytes.Repeat([]byte{'x'}, n)...)
}

// fluten schickt Zwischenablage-Bloecke, bis Keyway nichts mehr annimmt oder max erreicht ist.
func fluten(ws *websocket.Conn, block []byte, max int) (gesendet int) {
	for ; gesendet < max; gesendet++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := ws.Write(ctx, websocket.MessageBinary, block)
		cancel()
		if err != nil {
			return gesendet
		}
	}
	return gesendet
}

func vncVariablen(t *testing.T, schreibFrist, browserFrist time.Duration, stau int) {
	alt := vncGrenzen.Swap(&vncWerte{schreibFrist: schreibFrist, browserFrist: browserFrist, stauGrenze: stau, nachlauf: 2 * time.Second})
	t.Cleanup(func() { vncGrenzen.Store(alt) })
}

// Schliesst der Browser, geht vorher noch zum VNC-Server, was er davor geschickt hat (etwa
// das SetDesktopSize, mit dem die Seite beim Trennen die Aufloesung zuruecksetzt).
func TestVncNachlaufBeimSchliessen(t *testing.T) {
	tv := testVNC(t, testvnc.Optionen{})
	ts, _, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	h := vncHost(t, c, "", false, map[string]any{"name": "hmi", "vncPort": tv.Port(), "vncWeg": "direkt"})
	for runde := range 3 {
		ws := vncOeffnen(t, ts, c, fmt.Sprintf("host=%d&sprache=de", h.ID))
		if n, alle := vncBis(t, ws); n.T != "bereit" {
			t.Fatalf("statt bereit: %+v", alle)
		}
		b, _ := betrachten(t, ws)
		b.Kodierungen(0, -308)
		const tasten = 300
		for i := 0; i < tasten; i++ {
			b.Taste(true, 'a')
			b.Taste(false, 'a')
		}
		breite := 640 + 16*runde
		b.GroesseSetzen(breite, 480)
		ws.Close(websocket.StatusNormalClosure, "")
		gross := fmt.Sprintf("groesse %dx480", breite)
		warteBis(t, 5*time.Second, "alle Eingaben vor dem Schliessen beim VNC-Server", func() bool {
			ev := tv.Ereignisse()
			return slices.Contains(ev, gross) && len(slices.DeleteFunc(ev, func(e string) bool { return e != "taste 61 a" })) == tasten*(runde+1)
		})
		warteBis(t, 3*time.Second, "TCP zum VNC-Server zu", func() bool { return tv.Offen.Load() == 0 })
		keineVncGoroutinen(t)
	}
}

// Ein VNC-Server, der nichts mehr liest: Abmeldung und Schliessen des Browsers greifen
// trotzdem sofort, das Schreiben laeuft in eigener Goroutine.
func TestVncStummerServerAbmeldenUndAbbruch(t *testing.T) {
	vncVariablen(t, time.Minute, 15*time.Second, 64<<20)
	sv := stummerVNCStarten(t, false)
	ts, _, c, _ := angemeldeteApp(t, Optionen{Version: "test"})

	// Abmelden, waehrend das Schreiben zum Server haengt: 4001 innert Sekunden.
	ws, l, conn := stummVerbinden(t, ts, c, sv, "de")
	gelesen := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, l)
		gelesen <- err
	}()
	block := ablage(1 << 20)
	if n := fluten(ws, block, 40); n < 40 {
		t.Fatalf("Keyway nimmt nach %d MB nichts mehr an", n)
	}
	time.Sleep(200 * time.Millisecond) // das Schreiben zum Server haengt jetzt
	start := time.Now()
	if code := c.rufe("POST", "/api/abmelden", nil, nil); code != 200 {
		t.Fatalf("abmelden: %d", code)
	}
	select {
	case <-gelesen:
		if l.schluss != wsAbgemeldet || !l.hatText("abgemeldet") {
			t.Fatalf("Schluss %d, Texte %+v", l.schluss, l.texte)
		}
		if d := time.Since(start); d > 8*time.Second {
			t.Fatalf("Abmeldung erst nach %s", d)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("nach der Abmeldung bleibt der Bildschirm offen")
	}
	sv.zu(t, conn, 5*time.Second)
	keineVncGoroutinen(t)

	// Browser weg, waehrend das Schreiben haengt: alles baut ab.
	c.rufe("POST", "/api/anmelden", map[string]string{"benutzer": "admin", "passwort": testPasswort}, nil)
	ws, l, conn = stummVerbinden(t, ts, c, sv, "de")
	go io.Copy(io.Discard, l)
	fluten(ws, block, 40)
	time.Sleep(200 * time.Millisecond)
	ws.CloseNow()
	sv.zu(t, conn, 5*time.Second)
	keineVncGoroutinen(t)
}

// Speichergrenze: nimmt der Server nichts an, haeuft Keyway hoechstens stauGrenze an
// und trennt dann mit verstaendlicher Meldung.
func TestVncStummerServerStau(t *testing.T) {
	vncVariablen(t, time.Minute, 15*time.Second, 6<<20)
	sv := stummerVNCStarten(t, false)
	ts, _, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	ws, l, conn := stummVerbinden(t, ts, c, sv, "en")
	gelesen := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, l)
		gelesen <- err
	}()
	start := time.Now()
	gesendet := fluten(ws, ablage(1<<20), 200)
	select {
	case <-gelesen:
	case <-time.After(10 * time.Second):
		t.Fatalf("nach %d MB nicht getrennt", gesendet)
	}
	if gesendet >= 200 {
		t.Fatal("Keyway hat 200 MB angenommen, ohne zu trennen")
	}
	if e := l.text("ende"); e.Text != "The VNC server stopped accepting data — disconnected" {
		t.Fatalf("Ende: %+v (%+v)", e, l.texte)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("erst nach %s getrennt", d)
	}
	sv.zu(t, conn, 5*time.Second)
	keineVncGoroutinen(t)
}

// Schreibfrist: kommt ein Block zum Server nicht weiter, trennt der Waechter (auch ohne Stau).
func TestVncStummerServerFrist(t *testing.T) {
	vncVariablen(t, 400*time.Millisecond, 15*time.Second, 256<<20)
	sv := stummerVNCStarten(t, false)
	ts, _, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	ws, l, conn := stummVerbinden(t, ts, c, sv, "de")
	gelesen := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, l)
		gelesen <- err
	}()
	gesendet := fluten(ws, ablage(1<<20), 64)
	select {
	case <-gelesen:
	case <-time.After(10 * time.Second):
		t.Fatalf("nach %d MB nicht getrennt", gesendet)
	}
	if e := l.text("ende"); e.Text != "Der VNC-Server nimmt keine Daten mehr an — Verbindung getrennt" {
		t.Fatalf("Ende: %+v (%+v)", e, l.texte)
	}
	if l.schluss != websocket.StatusNormalClosure {
		t.Fatalf("Schliesscode %d", l.schluss)
	}
	sv.zu(t, conn, 5*time.Second)
	keineVncGoroutinen(t)
}

// Ein Browser, der nichts mehr liest: nach browserFrist baut Keyway ab, die
// Verbindung zum VNC-Server inklusive.
func TestVncBrowserLiestNicht(t *testing.T) {
	vncVariablen(t, time.Minute, 300*time.Millisecond, 16<<20)
	sv := stummerVNCStarten(t, true)
	ts, _, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	ws, _, conn := stummVerbinden(t, ts, c, sv, "de")
	// Der Browser liest ab jetzt nicht mehr; der Server flutet.
	sv.zu(t, conn, 10*time.Second)
	keineVncGoroutinen(t)
	ws.CloseNow()
}

// --- Schluessel des VNC-Servers beim Durchreichen (RA2, TOFU) ---

func TestVncSchluessel(t *testing.T) {
	ts, s, c, sk := angemeldeteApp(t, Optionen{Version: "test"})
	h := vncHost(t, c, "", false, map[string]any{"name": "realvnc", "adresse": "10.0.0.9", "vncPort": 5901, "vncWeg": "direkt"})
	pfad := fmt.Sprintf("/api/hosts/%d/vnc-schluessel", h.ID)
	const ziel = "vnc:10.0.0.9:5901"
	fp1 := "c4:1f:9e:00:5a:7b:22:13"
	fp2 := "de:ad:be:ef:00:11:22:33"

	// Unbekannt: nichts gemerkt; mit Fingerabdruck «neu».
	var a vncSchluesselAntwort
	if code := c.rufe("GET", pfad, nil, &a); code != 200 || a.Ziel != ziel || a.Bekannt || a.Status != "" {
		t.Fatalf("leer: %d %+v", code, a)
	}
	a = vncSchluesselAntwort{}
	if code := c.rufe("GET", pfad+"?typ=RSA&fingerprint="+fp1, nil, &a); code != 200 || a.Status != "neu" || a.Bekannt {
		t.Fatalf("Vergleich neu: %d %+v", code, a)
	}

	// Ohne CSRF-Kopf und ohne Anmeldung: abgelehnt, nichts gespeichert.
	rumpf := `{"typ":"RSA","fingerprint":"` + fp1 + `"}`
	if code, _ := rufeMit(t, c, "POST", pfad, rumpf, map[string]string{"X-SSH-App": ""}); code != 403 {
		t.Fatalf("ohne CSRF: %d", code)
	}
	anon := neuerClient(t, ts.URL, ts.Client())
	if code := anon.rufe("POST", pfad, map[string]string{"typ": "RSA", "fingerprint": fp1}, nil); code != 401 {
		t.Fatalf("ohne Anmeldung POST: %d", code)
	}
	if code := anon.rufe("GET", pfad, nil, nil); code != 401 {
		t.Fatalf("ohne Anmeldung GET: %d", code)
	}
	if _, err := s.st.Bekannt(ziel); err == nil {
		t.Fatal("ohne CSRF/Anmeldung gespeichert")
	}

	// Ungueltige Angaben, beide Sprachen; unbekannter Host.
	for _, r := range []string{`{"typ":"","fingerprint":"x"}`, `{"typ":"RSA","fingerprint":"  "}`, `{"typ":"RSA","fingerprint":"a\nb"}`,
		`{"typ":"RSA","fingerprint":"` + strings.Repeat("a", 300) + `"}`, `kein json`} {
		code, de := rufeMit(t, c, "POST", pfad, r, nil)
		_, en := rufeMit(t, c, "POST", pfad, r, englisch)
		if code != 400 || de == "" || en == "" || de == en {
			t.Errorf("%s: %d %q %q", r, code, de, en)
		}
	}
	if code, text := rufeMit(t, c, "POST", pfad, `{"typ":"RSA"}`, englisch); text != "The VNC key’s type and fingerprint are missing or invalid" {
		t.Errorf("englisch: %d %q", code, text)
	}
	if code, _ := rufeMit(t, c, "POST", "/api/hosts/999/vnc-schluessel", rumpf, nil); code != 404 {
		t.Errorf("unbekannter Host: %d", code)
	}

	// Merken (neu), dann gleich (bleibt mit altem Datum).
	a = vncSchluesselAntwort{}
	if code := c.rufe("POST", pfad, map[string]string{"typ": "RSA", "fingerprint": " " + fp1 + " "}, &a); code != 200 || a.Status != "neu" || !a.Bekannt || a.Fingerprint != fp1 || a.Typ != "RSA" || a.Seit == nil {
		t.Fatalf("merken: %d %+v", code, a)
	}
	b, err := s.st.Bekannt(ziel)
	if err != nil || b.Fingerprint != fp1 || b.Typ != "RSA" || b.Schluessel != fp1 {
		t.Fatalf("gespeichert: %+v %v", b, err)
	}
	a = vncSchluesselAntwort{}
	if code := c.rufe("POST", pfad, map[string]string{"typ": "RSA", "fingerprint": fp1}, &a); code != 200 || a.Status != "gleich" || !a.Seit.Equal(b.Seit) {
		t.Fatalf("gleich: %d %+v", code, a)
	}
	a = vncSchluesselAntwort{}
	if c.rufe("GET", pfad+"?typ=RSA&fingerprint="+fp1, nil, &a); a.Status != "gleich" || !a.Bekannt || a.Fingerprint != fp1 {
		t.Fatalf("Vergleich gleich: %+v", a)
	}

	// Geaendert: erkennbar; ohne «ersetzen» 409 und unveraendert, mit «ersetzen» ersetzt.
	a = vncSchluesselAntwort{}
	if c.rufe("GET", pfad+"?typ=RSA&fingerprint="+fp2, nil, &a); a.Status != "geaendert" || a.AltFingerprint != fp1 || a.AltTyp != "RSA" || a.Fingerprint != fp1 {
		t.Fatalf("Vergleich geändert: %+v", a)
	}
	a = vncSchluesselAntwort{}
	if code := c.rufe("POST", pfad, map[string]string{"typ": "RSA", "fingerprint": fp2}, &a); code != 409 || a.Status != "geaendert" || a.AltFingerprint != fp1 ||
		a.Fehler != "Der Schlüssel des VNC-Servers von «realvnc» hat sich geändert — nur ersetzen, wenn der Fingerabdruck am Gerät stimmt" {
		t.Fatalf("ohne ersetzen: %d %+v", code, a)
	}
	if code, text := rufeMit(t, c, "POST", pfad, `{"typ":"RSA","fingerprint":"`+fp2+`"}`, englisch); code != 409 ||
		text != "The VNC server key for “realvnc” has changed — replace it only if the fingerprint matches the one on the device" {
		t.Fatalf("englisch geändert: %d %q", code, text)
	}
	if b, _ := s.st.Bekannt(ziel); b.Fingerprint != fp1 {
		t.Fatalf("ohne ersetzen überschrieben: %+v", b)
	}
	a = vncSchluesselAntwort{}
	if code := c.rufe("POST", pfad, map[string]any{"typ": "RSA", "fingerprint": fp2, "ersetzen": true}, &a); code != 200 || a.Status != "geaendert" || a.AltFingerprint != fp1 || a.Fingerprint != fp2 {
		t.Fatalf("ersetzen: %d %+v", code, a)
	}
	// Auch ein anderer Typ gilt als Aenderung.
	a = vncSchluesselAntwort{}
	if c.rufe("GET", pfad+"?typ=ECDSA&fingerprint="+fp2, nil, &a); a.Status != "geaendert" || a.AltTyp != "RSA" {
		t.Fatalf("anderer Typ: %+v", a)
	}

	// In /api/daten (Bekannte Hosts) und im Backup; Rundlauf.
	var d struct{ Bekannt []store.BekannterHost }
	c.rufe("GET", "/api/daten", nil, &d)
	if !slices.ContainsFunc(d.Bekannt, func(x store.BekannterHost) bool { return x.Ziel == ziel && x.Fingerprint == fp2 }) {
		t.Fatalf("Daten: %+v", d.Bekannt)
	}
	backup := backupLaden(t, c)
	ts2, s2 := testApp(t)
	c2 := neuerClient(t, ts2.URL, ts2.Client())
	if code, text := einspielenAn(c2, "/api/einspielen", backup, sk); code != 200 {
		t.Fatalf("Einspielen: %d %s", code, text)
	}
	if b, err := s2.st.Bekannt(ziel); err != nil || b.Fingerprint != fp2 || b.Typ != "RSA" {
		t.Fatalf("nach Rundlauf: %+v %v", b, err)
	}

	// Vergessen: ueber den Host und ueber «Bekannte Hosts».
	if code := c.rufe("DELETE", pfad, nil, nil); code != 200 {
		t.Fatalf("vergessen: %d", code)
	}
	if code := c.rufe("DELETE", pfad, nil, nil); code != 404 {
		t.Fatalf("zweimal vergessen: %d", code)
	}
	c.rufe("POST", pfad, map[string]string{"typ": "RSA", "fingerprint": fp1}, nil)
	if code := c.rufe("DELETE", "/api/bekannt/"+url.PathEscape(ziel), nil, nil); code != 200 {
		t.Fatalf("über bekannt löschen: %d", code)
	}
	a = vncSchluesselAntwort{}
	if c.rufe("GET", pfad, nil, &a); a.Bekannt {
		t.Fatalf("nach dem Löschen: %+v", a)
	}

	// IPv6 in Klammern, Port-Vorgabe; der SSH-Schluessel desselben Hosts bleibt getrennt.
	h6 := vncHost(t, c, "", false, map[string]any{"name": "v6", "adresse": "fd00::5", "vncWeg": "direkt"})
	a = vncSchluesselAntwort{}
	if c.rufe("POST", fmt.Sprintf("/api/hosts/%d/vnc-schluessel", h6.ID), map[string]string{"typ": "RSA", "fingerprint": fp1}, &a); a.Ziel != "vnc:[fd00::5]:5900" {
		t.Fatalf("IPv6: %+v", a)
	}
	if _, err := s.st.Bekannt("[fd00::5]:5900"); err == nil {
		t.Fatal("VNC-Schlüssel unter einem SSH-Ziel")
	}
}
