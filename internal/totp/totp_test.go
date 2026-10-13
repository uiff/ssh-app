package totp

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// Geheimnis der Testvektoren aus RFC 4226 und RFC 6238 (SHA1).
var rfcGeheimnis = []byte("12345678901234567890")

// RFC 4226, Anhang D: HOTP-Werte fuer die Zaehler 0 bis 9.
func TestHOTPVektoren(t *testing.T) {
	want := []string{"755224", "287082", "359152", "969429", "338314", "254676", "287922", "162583", "399871", "520489"}
	for i, w := range want {
		if got := hotp(rfcGeheimnis, uint64(i), 6); got != w {
			t.Errorf("Zaehler %d: %s, erwartet %s", i, got, w)
		}
	}
}

// RFC 6238, Anhang B (SHA1): 8 Stellen genau, 6 Stellen = die letzten sechs.
func TestTOTPVektoren(t *testing.T) {
	for _, f := range []struct {
		zeit int64
		acht string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
		{20000000000, "65353130"},
	} {
		jetzt := time.Unix(f.zeit, 0)
		if got := hotp(rfcGeheimnis, uint64(Schritt(jetzt)), 8); got != f.acht {
			t.Errorf("T=%d: %s, erwartet %s", f.zeit, got, f.acht)
		}
		if got := Code(rfcGeheimnis, jetzt); got != f.acht[2:] {
			t.Errorf("T=%d (6 Stellen): %s, erwartet %s", f.zeit, got, f.acht[2:])
		}
	}
}

func TestSchritt(t *testing.T) {
	for _, f := range []struct {
		s    int64
		want int64
	}{{0, 0}, {29, 0}, {30, 1}, {59, 1}, {60, 2}, {-1, -1}, {-30, -1}, {-31, -2}} {
		if got := Schritt(time.Unix(f.s, 0)); got != f.want {
			t.Errorf("Schritt(%d) = %d, erwartet %d", f.s, got, f.want)
		}
	}
}

func TestPruefenFenster(t *testing.T) {
	jetzt := time.Unix(1111111111, 0)
	basis := Schritt(jetzt)
	for d := int64(-1); d <= 1; d++ {
		code := CodeBei(rfcGeheimnis, basis+d)
		s, ok := Pruefen(rfcGeheimnis, code, jetzt, Fenster)
		if !ok || s != basis+d {
			t.Errorf("Abstand %d: ok=%v Schritt %d", d, ok, s)
		}
	}
	for _, d := range []int64{-2, 2, 10} {
		if _, ok := Pruefen(rfcGeheimnis, CodeBei(rfcGeheimnis, basis+d), jetzt, Fenster); ok {
			t.Errorf("Abstand %d ausserhalb des Fensters angenommen", d)
		}
	}
	for _, falsch := range []string{"", "12345", "1234567", "abcdef"} {
		if _, ok := Pruefen(rfcGeheimnis, falsch, jetzt, Fenster); ok {
			t.Errorf("%q angenommen", falsch)
		}
	}
	// Anderes Geheimnis: der Code gilt nicht.
	if _, ok := Pruefen([]byte("anderes-geheimnis-20"), Code(rfcGeheimnis, jetzt), jetzt, Fenster); ok {
		t.Error("Code eines anderen Geheimnisses angenommen")
	}
}

func TestAbweichung(t *testing.T) {
	jetzt := time.Unix(2000000000, 0)
	basis := Schritt(jetzt)
	for _, d := range []int64{3, -2, 20, -20, 40, -40} {
		code := CodeBei(rfcGeheimnis, basis+d)
		got, ok := Abweichung(rfcGeheimnis, code, jetzt, Fenster, Suchweite)
		if !ok || got != d {
			t.Errorf("Abweichung %d: %d %v", d, got, ok)
		}
	}
	// Der Schritt direkt nach dem Fenster gilt gleich darauf von selbst: keine Abweichung.
	if d, ok := Abweichung(rfcGeheimnis, CodeBei(rfcGeheimnis, basis+Fenster+1), jetzt, Fenster, Suchweite); ok {
		t.Errorf("Schritt direkt nach dem Fenster als Abweichung %d gemeldet", d)
	}
	// Im Fenster ist keine Abweichung (dort gilt der Code), jenseits der Suchweite nicht gefunden.
	if _, ok := Abweichung(rfcGeheimnis, CodeBei(rfcGeheimnis, basis+1), jetzt, Fenster, Suchweite); ok {
		// Nur wenn der Code zufaellig auch weiter weg passt — bei diesem Zeitpunkt nicht.
		t.Error("Code im Fenster als Abweichung gemeldet")
	}
	if _, ok := Abweichung(rfcGeheimnis, CodeBei(rfcGeheimnis, basis+41), jetzt, Fenster, Suchweite); ok {
		t.Error("Abweichung jenseits der Suchweite gefunden")
	}
}

func TestNurZiffern(t *testing.T) {
	for in, want := range map[string]string{"123456": "123456", "123 456": "123456", " 12 34 56 ": "123456", "\t123456\n": "123456"} {
		if got, ok := NurZiffern(in); !ok || got != want {
			t.Errorf("%q: %q %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "12345", "1234567", "12a456", "ABCD-EFGH", "１２３４５６"} {
		if _, ok := NurZiffern(in); ok {
			t.Errorf("%q als Code angenommen", in)
		}
	}
}

func TestGeheimnisUndSchluessel(t *testing.T) {
	g := NeuesGeheimnis()
	if len(g) != GeheimnisLaenge {
		t.Fatalf("Laenge %d", len(g))
	}
	s := Base32(g)
	if len(s) != 32 || strings.ContainsAny(s, "=") {
		t.Fatalf("Base32: %q", s)
	}
	gr := Gruppiert(s)
	if len(strings.Fields(gr)) != 8 || strings.ReplaceAll(gr, " ", "") != s {
		t.Fatalf("Gruppiert: %q", gr)
	}
	if Gruppiert("ABCDEFGHIJ") != "ABCD EFGH IJ" {
		t.Fatal(Gruppiert("ABCDEFGHIJ"))
	}
	if Base32(rfcGeheimnis) != "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" {
		t.Fatal(Base32(rfcGeheimnis))
	}
}

func TestURI(t *testing.T) {
	u := URI("Keyway", "admin@keyway.local", rfcGeheimnis)
	want := "otpauth://totp/Keyway:admin@keyway.local?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&issuer=Keyway&algorithm=SHA1&digits=6&period=30"
	if u != want {
		t.Fatalf("URI:\n%s\n%s", u, want)
	}
	// Sonderzeichen im Konto: kodiert, ein «:» trennt nicht.
	u = URI("Keyway", "ü:x y/z@[::1]", rfcGeheimnis)
	p, err := url.Parse(u)
	if err != nil {
		t.Fatal(err)
	}
	pfad, _ := url.PathUnescape(strings.TrimPrefix(p.EscapedPath(), "/"))
	if p.Scheme != "otpauth" || p.Host != "totp" || pfad != "Keyway:ü:x y/z@[::1]" {
		t.Fatalf("Sonderzeichen: %s -> %q %q %q", u, p.Scheme, p.Host, pfad)
	}
	if strings.Count(strings.SplitN(u, "?", 2)[0], ":") != 2 { // «otpauth:» und der Trenner
		t.Fatalf("ungeschuetzter Doppelpunkt: %s", u)
	}
	if q := p.Query(); q.Get("secret") != Base32(rfcGeheimnis) || q.Get("issuer") != "Keyway" || q.Get("digits") != "6" || q.Get("period") != "30" || q.Get("algorithm") != "SHA1" {
		t.Fatalf("Parameter: %v", q)
	}
}

func TestErsatzcodes(t *testing.T) {
	codes := NeueErsatzcodes(ErsatzAnzahl)
	if len(codes) != ErsatzAnzahl {
		t.Fatalf("Anzahl %d", len(codes))
	}
	gesehen := map[string]bool{}
	for _, c := range codes {
		if !IstErsatzcode(c) || gesehen[c] {
			t.Fatalf("Code %q (doppelt: %v)", c, gesehen[c])
		}
		gesehen[c] = true
		a := ErsatzAnzeige(c)
		if len(a) != 9 || a[4] != '-' || ErsatzNormal(a) != c {
			t.Fatalf("Anzeige %q", a)
		}
		// Abgetippt: klein, mit Leerzeichen oder typografischem Bindestrich.
		for _, eingabe := range []string{strings.ToLower(a), " " + a[:4] + " " + a[5:] + " ", a[:4] + "‐" + a[5:], a[:4] + "–" + a[5:]} {
			if ErsatzNormal(eingabe) != c {
				t.Fatalf("%q -> %q, erwartet %q", eingabe, ErsatzNormal(eingabe), c)
			}
		}
	}
	for _, c := range []string{"", "ABCD", "ABCDEFGHJ", "ABCDEFG0", "ABCDEFGI", "ABCDEFG1"} {
		if IstErsatzcode(c) {
			t.Errorf("%q als Ersatzcode angenommen", c)
		}
	}
	// Alle Zeichen kommen vor (Verteilung grob gleichmaessig).
	zaehl := map[rune]int{}
	for _, c := range NeueErsatzcodes(400) {
		for _, r := range c {
			zaehl[r]++
		}
	}
	if len(zaehl) != len(ErsatzZeichen) {
		t.Fatalf("nur %d verschiedene Zeichen", len(zaehl))
	}
}
