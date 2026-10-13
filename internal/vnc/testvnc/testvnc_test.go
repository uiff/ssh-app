package testvnc

import (
	"image"
	"image/color"
	"net"
	"slices"
	"strings"
	"testing"
	"time"
)

func betrachter(t *testing.T, s *Server, passwort string) (*Betrachter, net.Conn) {
	t.Helper()
	c, err := net.Dial("tcp", s.Adresse())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.SetDeadline(time.Now().Add(10 * time.Second))
	b, err := Begruessen(c, passwort)
	if err != nil {
		t.Fatalf("Begrüssung: %v", err)
	}
	if err := b.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return b, c
}

func warteAuf(t *testing.T, s *Server, ereignis string) {
	t.Helper()
	bis := time.Now().Add(3 * time.Second)
	for time.Now().Before(bis) {
		if slices.ContainsFunc(s.Ereignisse(), func(e string) bool { return strings.HasPrefix(e, ereignis) }) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Ereignis %q fehlt: %v", ereignis, s.Ereignisse())
}

func TestNurLoopback(t *testing.T) {
	for _, a := range []string{"0.0.0.0:0", "192.168.10.113:5900", ":0", "[::]:0"} {
		if s, err := Starten(Optionen{Adresse: a}); err == nil {
			s.Close()
			t.Fatalf("%s angenommen", a)
		}
	}
	s, err := Starten(Optionen{Adresse: "localhost:0"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.Adresse(), "127.0.0.1:") {
		t.Fatalf("Adresse %s", s.Adresse())
	}
	s.Close()
}

// Bild, Klick, Taste, Zwischenablage, Rechtsklick, Fernaufloesung, Cursor.
func TestTestServerBedienung(t *testing.T) {
	s, err := Starten(Optionen{Passwort: "geheim", Breite: 640, Hoehe: 480})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b, c := betrachter(t, s, "geheim")
	if b.Breite != 640 || b.Hoehe != 480 || b.Name != "Keyway Testbild" {
		t.Fatalf("ServerInit: %+v", b)
	}
	b.Kodierungen(kodRaw, kodCursor, kodExtendedDesktopSize, kodDesktopSize)
	b.BildAnfordern(false)
	r, err := b.Aktualisierung()
	if err != nil {
		t.Fatal(err)
	}
	var arten []int32
	var bild Rechteck
	for _, x := range r {
		arten = append(arten, x.Kodierung)
		if x.Kodierung == kodRaw {
			bild = x
		}
	}
	if !slices.Contains(arten, kodExtendedDesktopSize) || !slices.Contains(arten, kodCursor) || bild.B != 640 || bild.H != 480 {
		t.Fatalf("erste Aktualisierung: %v %dx%d", arten, bild.B, bild.H)
	}
	if p := bild.Pixel(1, 300); p != 0xFF8800 {
		t.Fatalf("Rahmen: %06x", p)
	}
	if p := bild.Pixel(500, 200); p != 0x0000FF {
		t.Fatalf("blauer Balken: %06x", p)
	}

	// Linksklick malt einen roten Punkt.
	b.Zeiger(1, 300, 400)
	b.Zeiger(0, 300, 400)
	warteAuf(t, s, "maus links 300,400")
	b.BildAnfordern(true)
	rot := false
	for !rot {
		r, err := b.Aktualisierung()
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range r {
			if x.Kodierung == kodRaw && x.X <= 300 && x.Y <= 400 && x.X+x.B > 300 && x.Y+x.H > 400 {
				if p := x.Pixel(300, 400); p != 0xFF2020 {
					t.Fatalf("Punkt: %06x", p)
				}
				rot = true
			}
		}
		if !rot {
			b.BildAnfordern(true)
		}
	}

	// Strg+Alt+Entf, Zwischenablage.
	for _, k := range []uint32{0xffe3, 0xffe9, 0xffff} {
		b.Taste(true, k)
	}
	for _, k := range []uint32{0xffff, 0xffe9, 0xffe3} {
		b.Taste(false, k)
	}
	warteAuf(t, s, "taste ffff STRG+ALT+ENTF")
	b.AblageSenden("hallo welt")
	warteAuf(t, s, "ablage hallo welt")

	// Rechtsklick legt Text in die Zwischenablage des Viewers.
	b.Zeiger(4, 10, 20)
	b.Zeiger(0, 10, 20)
	b.BildAnfordern(true)
	for len(b.Ablage) == 0 {
		if _, err := b.Aktualisierung(); err != nil {
			t.Fatal(err)
		}
		b.BildAnfordern(true)
	}
	if !strings.Contains(b.Ablage[0], "Klick 10,20") {
		t.Fatalf("Ablage: %q", b.Ablage)
	}

	// Fernaufloesung.
	b.GroesseSetzen(800, 600)
	warteAuf(t, s, "groesse 800x600")
	b.BildAnfordern(true)
	for b.Breite != 800 {
		if _, err := b.Aktualisierung(); err != nil {
			t.Fatal(err)
		}
		b.BildAnfordern(true)
	}
	if w, h := s.Groesse(); w != 800 || h != 600 || b.Hoehe != 600 {
		t.Fatalf("Grösse: %dx%d / %dx%d", w, h, b.Breite, b.Hoehe)
	}
	c.Close()
	bis := time.Now().Add(2 * time.Second)
	for s.Offen.Load() != 0 && time.Now().Before(bis) {
		time.Sleep(10 * time.Millisecond)
	}
	if s.Offen.Load() != 0 {
		t.Fatal("Verbindung bleibt offen")
	}
}

func TestTestServerPixelformatUndBild(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, color.RGBA{R: 0x12, G: 0x34, B: 0x56, A: 255})
		}
	}
	s, err := Starten(Optionen{Bild: img, Version: "3.3"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b, c := betrachter(t, s, "")
	if b.Breite != 100 || b.Hoehe != 80 || b.Version != "003.003" {
		t.Fatalf("ServerInit: %+v", b)
	}
	// noVNC: 32 bpp, Tiefe 24, Rot ab Bit 0.
	pf := pixelFormat{bpp: 32, tiefe: 24, echtfarbe: true, rMax: 255, gMax: 255, bMax: 255, rS: 0, gS: 8, bS: 16}
	c.Write(append([]byte{0, 0, 0, 0}, pf.bytes()...))
	b.BildAnfordern(false)
	r, err := b.Aktualisierung()
	if err != nil || len(r) != 1 {
		t.Fatalf("%v %v", r, err)
	}
	// Ganz unten rechts liegt kein Text: dort zeigt sich das Bild.
	if p := r[0].Pixel(99, 79); p != 0x563412 {
		t.Fatalf("Pixel im Format des Viewers: %06x", p)
	}
}

func TestTestServerVeNCrypt(t *testing.T) {
	if _, err := Starten(Optionen{Typ: "vencrypt", Version: "3.3"}); err == nil {
		t.Fatal("VeNCrypt mit 3.3 angenommen")
	}
	if _, err := Starten(Optionen{Typ: "rsa"}); err == nil {
		t.Fatal("unbekannter Typ angenommen")
	}
}
