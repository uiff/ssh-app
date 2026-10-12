package term

import (
	"bytes"
	"strings"
	"testing"
)

// fuellen schreibt so viele Zeilen in die Sitzung, dass der Ring sicher ueberlaeuft.
func fuellen(s *Sitzung, zeile string) {
	stueck := []byte(strings.Repeat(zeile+"\r\n", 2000))
	for n := 0; n < 2*pufferGroesse; n += len(stueck) {
		s.verarbeiten(stueck)
	}
}

// BROWSER-CODE-14: Nach einem Ueberlauf fehlt der Anfang des Rings. Die Modi, die dort
// eingeschaltet wurden (htop: alternativer Bildschirm, Cursor-Tasten, Maus), stehen
// deshalb vor dem Puffer, den ein wieder angehaengter Browser bekommt.
func TestPufferStelltModiNachUeberlaufWiederHer(t *testing.T) {
	s, _ := testSitzung()
	s.verarbeiten([]byte("$ htop\r\n\x1b[?1049h\x1b[?1h\x1b=\x1b[?1000;1006h\x1b[?25l\x1b[?2004h"))
	// Gesetzt und wieder aufgehoben: gehoert nicht in den Vorspann.
	s.verarbeiten([]byte("\x1b[?1002h\x1b[?1002l"))
	fuellen(s, "\x1b[H CPU[|||   ] 12.5%")

	_, puffer, _, _ := s.Anhaengen(Empfaenger{Daten: func([]byte) bool { return true }})
	want := "\x1b[?1049h\x1b[?1h\x1b[?25l\x1b[?1000h\x1b[?1006h\x1b[?2004h\x1b="
	if !bytes.HasPrefix(puffer, []byte(want)) {
		t.Fatalf("Vorspann fehlt oder falsch: %q", puffer[:min(len(puffer), 80)])
	}
	if len(puffer) > pufferGroesse+len(want) {
		t.Fatalf("Puffer zu gross: %d", len(puffer))
	}
	// Der Ring beginnt an einem Zeilenanfang, nicht mitten in einer Sequenz.
	rest := puffer[len(want):]
	if !bytes.HasPrefix(rest, []byte("\x1b[H CPU")) {
		t.Fatalf("Ring beginnt mitten in einer Zeile: %q", rest[:40])
	}
}

// Ohne Ueberlauf enthaelt der Ring alle Sequenzen selbst: kein Vorspann.
func TestPufferOhneUeberlaufUnveraendert(t *testing.T) {
	s, _ := testSitzung()
	s.verarbeiten([]byte("\x1b[?1049hvi\x1b[?1h"))
	_, puffer, _, _ := s.Anhaengen(Empfaenger{Daten: func([]byte) bool { return true }})
	if string(puffer) != "\x1b[?1049hvi\x1b[?1h" {
		t.Fatalf("Puffer verändert: %q", puffer)
	}
}

// Wer den Vollbildmodus vor dem verlorenen Teil wieder verlassen hat, bekommt keinen Vorspann.
func TestPufferNachVerlassenOhneVorspann(t *testing.T) {
	s, _ := testSitzung()
	s.verarbeiten([]byte("\x1b[?1049h\x1b[?1hvi\x1b[?1l\x1b[?1049l$ "))
	fuellen(s, "ls -la")
	_, puffer, _, _ := s.Anhaengen(Empfaenger{Daten: func([]byte) bool { return true }})
	if bytes.HasPrefix(puffer, []byte("\x1b[?")) {
		t.Fatalf("unnötiger Vorspann: %q", puffer[:20])
	}
}

func TestModusStandLiestGeteilteSequenzen(t *testing.T) {
	var m modusStand
	m.lesen([]byte("text\x1b[?10"))
	m.lesen([]byte("49h\x1b"))
	m.lesen([]byte("[?2004h"))
	if got := string(m.folge()); got != "\x1b[?1049h\x1b[?2004h" {
		t.Fatalf("geteilte Sequenzen: %q", got)
	}
	m.lesen([]byte("\x1bc")) // Zuruecksetzen
	if got := m.folge(); got != nil {
		t.Fatalf("nach ESC c: %q", got)
	}
	m.lesen([]byte("\x1b[?7l\x1b[?1h\x1b[!p")) // weiches Zuruecksetzen hebt beide auf
	if got := m.folge(); got != nil {
		t.Fatalf("nach DECSTR: %q", got)
	}
}

func TestSchnittStelle(t *testing.T) {
	ring := []byte("abc\x1b[?1049hdef\nneu")
	if i := schnittStelle(ring, 2); string(ring[i:]) != "neu" {
		t.Fatalf("Zeilenende: %q", ring[i:])
	}
	ring = []byte("abc\x1b[?1049hdef")
	if i := schnittStelle(ring, 1); string(ring[i:]) != "\x1b[?1049hdef" {
		t.Fatalf("Sequenzanfang: %q", ring[i:])
	}
	ring = []byte("aäöü")
	if i := schnittStelle(ring, 2); string(ring[i:]) != "öü" {
		t.Fatalf("UTF-8: %q", ring[i:])
	}
}
