package vnc_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/uiff/ssh-app/internal/texte"
	"github.com/uiff/ssh-app/internal/vnc"
	"github.com/uiff/ssh-app/internal/vnc/testvnc"
)

// Die DES-Antwort stimmt mit der von noVNC 1.7 (core/crypto/des.js) ueberein; die Vektoren
// hat noVNC selbst berechnet. Laengere Passwoerter zaehlen nur mit 8 Zeichen.
func TestDESWieNoVNC(t *testing.T) {
	ch, _ := hex.DecodeString("031425364758697a8b9cadbecfe0f102")
	for pw, will := range map[string]string{
		"geheim":         "50c05925ef6fc5e839981390676385c7",
		"12345678":       "540ca97c45e6497120f21665e21b19cf",
		"langesPasswort": "8b9bd60b5ceaf883f2fae1cffc9eb455",
		"":               "562c25d76cc998b0abdb8424a0195286",
		"Geh31m-VNC":     "b0d376130accb37e1602108dec3df4c6",
		// Nicht-ASCII: noVNC nimmt je UTF-16-Einheit das untere Byte (charCodeAt & 0xFF),
		// nicht UTF-8. Werte aus web/static/vendor/novnc/core/crypto/des.js (RFB.genDES).
		"Grüezi€1": "f1391a7448244701a9640b6492076689",
		"äöü":      "37f4b8975c51e80df1346c632876ecc1",
		"Straße":   "5546372341d383a5f003ae8c9262a127",
		"€uro":     "b63bf39a638407c1ce059ed1368c9400",
		"pw😀x":     "8d454aadfa3c7cc25f48b82df17e26cf", // ausserhalb der BMP: zwei Einheiten
		"Ā":        "562c25d76cc998b0abdb8424a0195286", // U+0100: unteres Byte 0 wie leer
	} {
		if got := hex.EncodeToString(vnc.DESAntwort(pw, ch)); got != will {
			t.Errorf("%q: %s, noVNC: %s", pw, got, will)
		}
	}
	if !bytes.Equal(vnc.DESAntwort("langesPa", ch), vnc.DESAntwort("langesPasswort", ch)) {
		t.Error("nur die ersten 8 Zeichen zählen")
	}
	if !bytes.Equal(vnc.DESAntwort("Grüezi€1", ch), vnc.DESAntwort("Grüezi€1 und mehr", ch)) {
		t.Error("nur die ersten 8 Zeichen zählen (auch mit Umlauten)")
	}
}

func starten(t *testing.T, o testvnc.Optionen) *testvnc.Server {
	t.Helper()
	s, err := testvnc.Starten(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func verbinden(t *testing.T, s *testvnc.Server) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", s.Adresse())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func zeiger(s string) *string { return &s }

// Keyway meldet sich mit RFB 3.3, 3.7 und 3.8 an: ohne Passwort, mit richtigem, mit
// falschem und ohne bekanntes Passwort.
func TestAnmeldenVersionenUndTypen(t *testing.T) {
	for _, version := range []string{"3.3", "3.7", "3.8"} {
		for _, typ := range []string{"none", "vnc"} {
			t.Run(version+"/"+typ, func(t *testing.T) {
				o := testvnc.Optionen{Version: version, Typ: typ, Name: "Probe " + version}
				if typ == "vnc" {
					o.Passwort = "geheim"
				}
				s := starten(t, o)
				ctx := context.Background()
				c := verbinden(t, s)
				erg, err := vnc.Anmelden(ctx, c, "probe", zeiger("geheim"))
				if err != nil {
					t.Fatalf("Anmelden: %v", err)
				}
				if erg.Version != "003.00"+version[2:] || erg.Durchreichen || (typ == "vnc") != (erg.Typ == vnc.TypVNC) {
					t.Fatalf("Ergebnis: %+v", erg)
				}
				bt := testvnc.NeuerBetrachter(c)
				c.SetDeadline(time.Now().Add(5 * time.Second))
				if err := bt.Init(); err != nil || bt.Name != "Probe "+version || bt.Breite != 1024 {
					t.Fatalf("ServerInit: %v %+v", err, bt)
				}
				if typ == "none" {
					return
				}
				// Falsches Passwort.
				_, err = vnc.Anmelden(ctx, verbinden(t, s), "probe", zeiger("falsch"))
				if !errors.Is(err, vnc.ErrPasswortFalsch) {
					t.Fatalf("falsches Passwort: %v", err)
				}
				// Kein Passwort bekannt: Rueckfrage noetig.
				_, err = vnc.Anmelden(ctx, verbinden(t, s), "probe", nil)
				if !errors.Is(err, vnc.ErrPasswortNoetig) {
					t.Fatalf("ohne Passwort: %v", err)
				}
				if ev := strings.Join(s.Ereignisse(), "|"); !strings.Contains(ev, "anmeldung ok") || !strings.Contains(ev, "anmeldung falsch") {
					t.Fatalf("Ereignisse: %s", ev)
				}
			})
		}
	}
}

// Bietet der Server nur VeNCrypt an, reicht Keyway durch: der Vorspann ist genau das,
// was der Server geschickt hat, und der Browser kann sich danach selbst anmelden.
func TestDurchreichenBeiVeNCrypt(t *testing.T) {
	s := starten(t, testvnc.Optionen{Typ: "vencrypt", Passwort: "langes-geheimnis"})
	c := verbinden(t, s)
	erg, err := vnc.Anmelden(context.Background(), c, "probe", zeiger("egal"))
	if err != nil {
		t.Fatal(err)
	}
	if !erg.Durchreichen || erg.Typ != 0 || string(erg.Vorspann) != "RFB 003.008\n\x01\x13" {
		t.Fatalf("Ergebnis: %+v %q", erg, erg.Vorspann)
	}
	// Browser-Seite ueber eine Leitung: Vorspann lesen, eigene Version schicken.
	keyway, browser := net.Pipe()
	defer keyway.Close()
	defer browser.Close()
	fertig := make(chan error, 1)
	go func() { fertig <- vnc.BrowserDurchreichen(keyway, erg) }()
	// Wie noVNC: erst die Version lesen und beantworten, dann die Typenliste.
	vorspann := make([]byte, len(erg.Vorspann))
	if _, err := io.ReadFull(browser, vorspann[:12]); err != nil || string(vorspann[:12]) != "RFB 003.008\n" {
		t.Fatalf("Version: %q %v", vorspann[:12], err)
	}
	browser.Write([]byte("RFB 003.008\n"))
	if _, err := io.ReadFull(browser, vorspann[12:]); err != nil || !bytes.Equal(vorspann, erg.Vorspann) {
		t.Fatalf("Vorspann: %q %v", vorspann, err)
	}
	if err := <-fertig; err != nil {
		t.Fatalf("BrowserDurchreichen: %v", err)
	}
	// Ab hier spricht der «Browser» direkt mit dem Server: VeNCrypt Plain.
	c.SetDeadline(time.Now().Add(5 * time.Second))
	c.Write([]byte{19})
	v := make([]byte, 2)
	io.ReadFull(c, v)
	c.Write([]byte{0, 2})
	ack := make([]byte, 6)
	io.ReadFull(c, ack)
	if !bytes.Equal(ack, []byte{0, 1, 0, 0, 1, 0}) {
		t.Fatalf("VeNCrypt: %v", ack)
	}
	c.Write([]byte{0, 0, 1, 0})
	pw := "langes-geheimnis"
	c.Write(append([]byte{0, 0, 0, 2, 0, 0, 0, byte(len(pw)), 'u', 'i'}, pw...))
	ok := make([]byte, 4)
	io.ReadFull(c, ok)
	if !bytes.Equal(ok, []byte{0, 0, 0, 0}) {
		t.Fatalf("SecurityResult: %v", ok)
	}
	bt := testvnc.NeuerBetrachter(c)
	if err := bt.Init(); err != nil || bt.Name != "Keyway Testbild" {
		t.Fatalf("ServerInit: %v %+v", err, bt)
	}

	// Waehlt der Browser eine andere Version, bricht Keyway ab.
	keyway2, browser2 := net.Pipe()
	defer keyway2.Close()
	defer browser2.Close()
	go func() { fertig <- vnc.BrowserDurchreichen(keyway2, erg) }()
	io.ReadFull(browser2, vorspann[:12])
	browser2.Write([]byte("RFB 003.007\n"))
	if err := <-fertig; err == nil || texte.Uebersetzen(texte.EN, err) != "The VNC viewer in the browser chose a different VNC version — please reconnect" {
		t.Fatalf("andere Version: %v", err)
	}
}

// Gegenueber dem Browser spielt Keyway einen RFB-3.8-Server ohne Passwort (auch 3.7 und 3.3).
func TestBrowserBegruessen(t *testing.T) {
	for _, f := range []struct {
		version string
		will    []byte // Antwort nach der Version des Browsers
		typ     bool   // Browser waehlt einen Typ
	}{
		{"RFB 003.008\n", []byte{1, 1, 0, 0, 0, 0}, true},
		{"RFB 003.007\n", []byte{1, 1}, true},
		{"RFB 003.003\n", []byte{0, 0, 0, 1}, false},
	} {
		keyway, browser := net.Pipe()
		fertig := make(chan error, 1)
		go func() { fertig <- vnc.BrowserBegruessen(keyway) }()
		v := make([]byte, 12)
		io.ReadFull(browser, v)
		if string(v) != "RFB 003.008\n" {
			t.Fatalf("Version an den Browser: %q", v)
		}
		browser.Write([]byte(f.version))
		got := make([]byte, len(f.will))
		if f.typ {
			io.ReadFull(browser, got[:2])
			browser.Write([]byte{1})
			io.ReadFull(browser, got[2:])
		} else {
			io.ReadFull(browser, got)
		}
		if err := <-fertig; err != nil || !bytes.Equal(got, f.will) {
			t.Fatalf("%q: %v %v", f.version, got, err)
		}
		keyway.Close()
		browser.Close()
	}
	// Unbekannte Version und falscher Typ.
	for _, f := range []struct {
		version string
		typ     byte
		will    string
	}{
		{"RFB 004.000\n", 0, "The VNC viewer in the browser speaks an unknown VNC version"},
		{"RFB 003.008\n", 2, "The VNC viewer in the browser chose an unexpected authentication method"},
	} {
		keyway, browser := net.Pipe()
		fertig := make(chan error, 1)
		go func() { fertig <- vnc.BrowserBegruessen(keyway) }()
		io.ReadFull(browser, make([]byte, 12))
		browser.Write([]byte(f.version))
		if f.typ != 0 {
			io.ReadFull(browser, make([]byte, 2))
			browser.Write([]byte{f.typ})
		}
		if err := <-fertig; texte.Uebersetzen(texte.EN, err) != f.will {
			t.Fatalf("%q: %v", f.version, err)
		}
		keyway.Close()
		browser.Close()
	}
}

// fremderServer lauscht und antwortet jedem Client mit antwort (dann schliessen oder schweigen).
func fremderServer(t *testing.T, antwort []byte, schweigen bool) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				c.Write(antwort)
				if schweigen {
					io.Copy(io.Discard, c)
				}
				c.Close()
			}()
		}
	}()
	return l.Addr().String()
}

// grund: RFB 3.8 ohne Sicherheitstypen, mit Grund.
func grund(text string) []byte {
	b := append([]byte("RFB 003.008\n"), 0)
	b = binary.BigEndian.AppendUint32(b, uint32(len(text)))
	return append(b, text...)
}

// Verstaendliche Fehler in Deutsch und Englisch.
func TestFehlerVerstaendlich(t *testing.T) {
	alt := vnc.AnmeldeFrist
	vnc.AnmeldeFrist = 600 * time.Millisecond
	defer func() { vnc.AnmeldeFrist = alt }()
	ctx := context.Background()
	anmelden := func(addr string) error {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		_, err = vnc.Anmelden(ctx, c, "pi", nil)
		return err
	}
	for _, f := range []struct {
		addr   string
		de, en string
	}{
		{fremderServer(t, []byte("SSH-2.0-OpenSSH_9.6\r\n"), true), "pi spricht kein VNC (RFB)", "pi doesn’t speak VNC (RFB)"},
		{fremderServer(t, []byte("RFB 009.000\n"), true), "pi meldet die unbekannte VNC-Version «009.000»", "pi reports the unknown VNC version “009.000”"},
		{fremderServer(t, nil, false), "pi hat die Verbindung beendet — läuft dort ein VNC-Server, und lässt er diese Adresse zu?", "pi closed the connection — is a VNC server running there, and does it allow this address?"},
		{fremderServer(t, nil, true), "Der VNC-Server pi antwortet nicht (Zeitüberschreitung nach 1 s)", "The VNC server pi isn’t responding (timed out after 1 s)"},
		{fremderServer(t, grund("Too many security failures"), true), "Der VNC-Server lehnt die Verbindung ab: Too many security failures", "The VNC server rejected the connection: Too many security failures"},
		{fremderServer(t, []byte("RFB 003.003\n\x00\x00\x00\x05"), true), "pi verlangt ein unbekanntes Anmeldeverfahren (Typ 5)", "pi requires an unknown authentication method (type 5)"},
	} {
		err := anmelden(f.addr)
		if de, en := texte.Uebersetzen(texte.DE, err), texte.Uebersetzen(texte.EN, err); de != f.de || en != f.en {
			t.Errorf("%s:\n  %q\n  %q", f.addr, de, en)
		}
	}

	// Direkte Verbindung abgelehnt.
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().(*net.TCPAddr)
	l.Close()
	_, err := vnc.Waehlen(ctx, "127.0.0.1", addr.Port)
	hp := addr.String()
	if de, en := texte.Uebersetzen(texte.DE, err), texte.Uebersetzen(texte.EN, err); de != "Verbindung abgelehnt — läuft auf "+hp+" ein VNC-Server?" || en != "Connection refused — is a VNC server running on "+hp+"?" {
		t.Fatalf("abgelehnt: %q / %q", de, en)
	}
	// Abbruch durch den Browser.
	c, err := net.Dial("tcp", fremderServer(t, nil, true))
	if err != nil {
		t.Fatal(err)
	}
	abbruch, cancel := context.WithCancel(ctx)
	time.AfterFunc(50*time.Millisecond, cancel)
	if _, err := vnc.Anmelden(abbruch, c, "pi", nil); !errors.Is(err, vnc.ErrAbgebrochen) {
		t.Fatalf("Abbruch: %v", err)
	}
}

// Bedienung erkennen: Tasten, Zeiger, Bildschirmgroesse zaehlen; Bildanforderungen,
// Kodierungen, Pixelformat, Fences und die Zwischenablage nicht (noVNC beantwortet die
// erweiterte Zwischenablage des Servers von selbst, auch bei «Nur ansehen»).
func TestEingaben(t *testing.T) {
	var e vnc.Eingaben
	nicht := [][]byte{
		{1},                                    // ClientInit
		append([]byte{0}, make([]byte, 19)...), // SetPixelFormat
		{2, 0, 0, 2, 0, 0, 0, 0, 0xff, 0xff, 0xfe, 0xcc}, // SetEncodings (2)
		{3, 1, 0, 0, 0, 0, 4, 0, 3, 0},                   // FramebufferUpdateRequest
		{248, 0, 0, 0, 0, 0, 0, 0, 2, 'a', 'b'},          // Fence mit 2 Bytes
		{150, 1, 0, 0, 0, 0, 0, 10, 0, 10},               // EnableContinuousUpdates
		{6, 0, 0, 0, 0, 0, 0, 3, 'a', 'b', 'c'},          // ClientCutText (klassisch)
		// Automatische Antworten von noVNC 1.7 (core/rfb.js) auf die erweiterte
		// Zwischenablage des Servers: Caps (Laenge -8 mit Formaten und Groesse), Request
		// auf Notify, Notify auf Peek, Provide auf Request.
		{6, 0, 0, 0, 0xff, 0xff, 0xff, 0xf8, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x10, 0x00}, // Caps
		{6, 0, 0, 0, 0xff, 0xff, 0xff, 0xfc, 0x02, 0x00, 0x00, 0x01},                         // Request
		{6, 0, 0, 0, 0xff, 0xff, 0xff, 0xfc, 0x08, 0x00, 0x00, 0x01},                         // Notify
		{6, 0, 0, 0, 0xff, 0xff, 0xff, 0xf9, 0x10, 0x00, 0x00, 0x01, 0x78, 0x9c, 0x03},       // Provide (zlib)
	}
	for i, b := range nicht {
		if e.Pruefen(b) {
			t.Fatalf("%d: %v zählt als Bedienung", i, b)
		}
	}
	ja := [][]byte{
		{5, 1, 0, 10, 0, 20},        // Zeiger
		{5, 0x81, 0, 10, 0, 20, 0},  // erweiterter Zeiger
		{4, 1, 0, 0, 0, 0, 0, 0x61}, // Taste
		append([]byte{251, 0, 3, 0, 2, 0, 1, 0}, make([]byte, 16)...), // SetDesktopSize
		{255, 0, 0, 1, 0, 0, 0, 0x61, 0, 0, 0, 30},                    // QEMU-Taste
		{250, 0, 1, 2}, // XVP (Neustart …)
	}
	for i, b := range ja {
		if !e.Pruefen(b) {
			t.Fatalf("%d: %v zählt nicht als Bedienung", i, b)
		}
	}
	// Zerstueckelt: eine Bildanforderung ueber zwei Bloecke, ein Zeiger ueber zwei Bloecke.
	var f vnc.Eingaben
	if f.Pruefen([]byte{1, 3, 1, 0}) || f.Pruefen([]byte{0, 0, 0, 4, 0, 3, 0}) {
		t.Fatal("zerstückelte Bildanforderung zählt")
	}
	f.Pruefen([]byte{5})
	if !f.Pruefen([]byte{0, 0, 1, 0, 1}) {
		t.Fatal("Zeiger über zwei Blöcke zählt nicht")
	}
	if f.Pruefen([]byte{3, 1, 0, 0, 0, 0, 0, 1, 0, 1}) {
		t.Fatal("nach dem Zeiger aus dem Takt")
	}
	// Nach der Zwischenablage im Takt: der Zeiger dahinter zaehlt.
	var z vnc.Eingaben
	if !z.Pruefen([]byte{1, 6, 0, 0, 0, 0, 0, 0, 2, 'h', 'i', 5, 0, 0, 1, 0, 1}) {
		t.Fatal("Zeiger nach der Zwischenablage zählt nicht")
	}
	// Unbekannte Nachricht: der Strom ist nicht mehr lesbar, ab dann zaehlt nichts mehr
	// (wie beim Durchreichen; Bedienung meldet der Browser dann mit «bedient»).
	var g vnc.Eingaben
	if g.Pruefen([]byte{1, 77}) || g.Pruefen([]byte{3, 1, 0, 0, 0, 0, 0, 1, 0, 1}) || g.Pruefen([]byte{4, 1, 0, 0, 0, 0, 0, 0x61}) {
		t.Fatal("nach unbekannter Nachricht darf nichts mehr zählen")
	}
	if !g.Unlesbar() {
		t.Fatal("Unlesbar nach unbekannter Nachricht")
	}
}
