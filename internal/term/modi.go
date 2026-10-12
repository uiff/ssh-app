package term

import (
	"bytes"
	"regexp"
	"slices"
	"strconv"
)

// Terminal-Modi am Anfang des Ringpuffers.
//
// Beim Wiederanhaengen setzt der Browser sein Terminal zurueck und zeichnet den Puffer neu.
// Ist der Puffer uebergelaufen, fehlt sein Anfang und damit oft die Sequenz, die z.B. den
// alternativen Bildschirm (top, htop, vi), die Cursor-Tasten oder die Maus eingeschaltet hat.
// Deshalb fuehrt die Sitzung mit, welche Modi am Anfang des Rings gelten, und stellt sie dem
// Puffer voran. So stimmen Vollbildprogramme auch nach einem langen Funkloch wieder.

// verfolgteModi sind DEC-Privatmodi mit ihrem Grundzustand nach einem Zuruecksetzen.
var verfolgteModi = map[int]bool{
	1:    false, // Cursor-Tasten im Anwendungsmodus
	7:    true,  // automatischer Zeilenumbruch
	25:   true,  // Cursor sichtbar
	47:   false, // alternativer Bildschirm (alt)
	1047: false, // alternativer Bildschirm
	1049: false, // alternativer Bildschirm mit gemerktem Cursor
	1000: false, // Maus: Klicks
	1002: false, // Maus: Ziehen
	1003: false, // Maus: jede Bewegung
	1005: false, // Maus: UTF-8-Kodierung
	1006: false, // Maus: SGR-Kodierung
	1015: false, // Maus: urxvt-Kodierung
	2004: false, // Bracketed Paste
}

// Zuerst der Bildschirm, die anderen Modi gelten danach fuer ihn.
var bildschirmModi = []int{47, 1047, 1049}

// modusSequenz: DECSET/DECRST («ESC [ ? n ; m h|l»), Tastenblock («ESC =», «ESC >»),
// Zuruecksetzen («ESC c») und weiches Zuruecksetzen («ESC [ ! p»).
var modusSequenz = regexp.MustCompile(`\x1b\[\?([0-9;]*)([hl])|\x1b([=>c])|\x1b\[!p`)

// offeneModusSequenz: angefangene Sequenz am Ende eines Stuecks.
var offeneModusSequenz = regexp.MustCompile(`\x1b(?:\[(?:\?[0-9;]*|!)?)?$`)

// modusStand haelt die Modi, die vom Grundzustand abweichen.
type modusStand struct {
	wert   map[int]bool
	keypad bool   // Tastenblock im Anwendungsmodus («ESC =»)
	rest   []byte // angefangene Sequenz am Ende des zuletzt gelesenen Stuecks
}

// lesen fuehrt den Stand ueber ein Stueck Ausgabe nach. Eine Sequenz, die ueber das Ende
// hinausgeht, wird mit dem naechsten Stueck zusammengesetzt.
func (m *modusStand) lesen(b []byte) {
	if len(m.rest) > 0 {
		b = append(m.rest, b...)
		m.rest = nil
	}
	for _, t := range modusSequenz.FindAllSubmatch(b, -1) {
		switch {
		case t[2] != nil:
			an := t[2][0] == 'h'
			for _, f := range bytes.Split(t[1], []byte(";")) {
				n, err := strconv.Atoi(string(f))
				grund, ok := verfolgteModi[n]
				if err != nil || !ok {
					continue
				}
				m.setzen(n, an, grund)
			}
		case t[3] != nil:
			switch t[3][0] {
			case '=':
				m.keypad = true
			case '>':
				m.keypad = false
			case 'c':
				m.wert, m.keypad = nil, false
			}
		default: // weiches Zuruecksetzen (DECSTR): Tasten, Umbruch und Cursor wie am Anfang
			for _, n := range []int{1, 7, 25} {
				delete(m.wert, n)
			}
			m.keypad = false
		}
	}
	if i := offeneModusSequenz.FindIndex(b); i != nil && len(b)-i[0] <= 32 {
		m.rest = append([]byte(nil), b[i[0]:]...)
	}
}

func (m *modusStand) setzen(n int, an, grund bool) {
	if an == grund {
		delete(m.wert, n)
		return
	}
	if m.wert == nil {
		m.wert = map[int]bool{}
	}
	m.wert[n] = an
}

// folge liefert die Sequenzen, die ein frisch zurueckgesetztes Terminal in diesen Stand
// bringen (leer, wenn alles im Grundzustand ist).
func (m *modusStand) folge() []byte {
	if len(m.wert) == 0 && !m.keypad {
		return nil
	}
	nummern := make([]int, 0, len(m.wert))
	for n := range m.wert {
		if !slices.Contains(bildschirmModi, n) {
			nummern = append(nummern, n)
		}
	}
	slices.Sort(nummern)
	var b bytes.Buffer
	for _, n := range append(slices.Clone(bildschirmModi), nummern...) {
		an, ok := m.wert[n]
		if !ok {
			continue
		}
		b.WriteString("\x1b[?" + strconv.Itoa(n))
		if an {
			b.WriteByte('h')
		} else {
			b.WriteByte('l')
		}
	}
	if m.keypad {
		b.WriteString("\x1b=")
	}
	return b.Bytes()
}

// schnittStelle sucht, wo der Ring gekuerzt wird: ab over muss weg. Bevorzugt nach einem
// Zeilenende, sonst vor einer Steuersequenz, sonst nicht mitten in einem UTF-8-Zeichen —
// so beginnt der Puffer nicht mit dem Bruchstueck einer Sequenz oder eines Zeichens.
func schnittStelle(ring []byte, over int) int {
	ende := min(len(ring), over+4096)
	if i := bytes.IndexByte(ring[over:ende], '\n'); i >= 0 {
		return over + i + 1
	}
	if i := bytes.IndexByte(ring[over:ende], 0x1b); i >= 0 {
		return over + i
	}
	for over < len(ring) && ring[over]&0xC0 == 0x80 {
		over++
	}
	return over
}
