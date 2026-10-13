package testvnc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/uiff/ssh-app/internal/vnc"
)

// Betrachter ist ein minimaler RFB-Client fuer Tests (er spielt den Viewer im Browser):
// Begruessung mit None oder VNC-Passwort, ClientInit/ServerInit, Bildanforderungen und
// Eingaben. Bilder kommen im Format des Servers (32 bpp), Kodierungen: Raw und die
// Pseudo-Kodierungen Cursor, DesktopSize, ExtendedDesktopSize.
type Betrachter struct {
	rw      io.ReadWriter
	Version string // ausgehandelte Version, z. B. "003.008"
	Typen   []byte // angebotene Sicherheitstypen (3.3: der vom Server bestimmte)
	Breite  int
	Hoehe   int
	Name    string
	// Ablage sammelt empfangene ServerCutText-Texte.
	Ablage []string
}

// Rechteck einer Bildaktualisierung.
type Rechteck struct {
	X, Y, B, H int
	Kodierung  int32
	Daten      []byte
}

// ErrAbgelehnt: der Server hat die Anmeldung abgelehnt.
var ErrAbgelehnt = errors.New("Anmeldung abgelehnt")

// NeuerBetrachter: ein Betrachter fuer eine Verbindung, deren Begruessung schon
// erledigt ist (als Naechstes kommt ClientInit).
func NeuerBetrachter(rw io.ReadWriter) *Betrachter { return &Betrachter{rw: rw} }

// Begruessen fuehrt Version und Sicherheit als Client durch (hoechstens RFB 3.8).
// passwort wird nur bei VNC-Authentication gebraucht.
func Begruessen(rw io.ReadWriter, passwort string) (*Betrachter, error) {
	b := &Betrachter{rw: rw}
	sv := make([]byte, 12)
	if _, err := io.ReadFull(rw, sv); err != nil {
		return nil, err
	}
	switch string(sv[4:11]) {
	case "003.003":
		b.Version = "003.003"
	case "003.007":
		b.Version = "003.007"
	default:
		b.Version = "003.008"
	}
	if _, err := io.WriteString(rw, "RFB "+b.Version+"\n"); err != nil {
		return nil, err
	}
	var typ byte
	if b.Version == "003.003" {
		var t uint32
		if err := zahlLesen(rw, &t); err != nil {
			return nil, err
		}
		b.Typen = []byte{byte(t)}
		typ = byte(t)
	} else {
		n := make([]byte, 1)
		if _, err := io.ReadFull(rw, n); err != nil {
			return nil, err
		}
		b.Typen = make([]byte, n[0])
		if _, err := io.ReadFull(rw, b.Typen); err != nil {
			return nil, err
		}
		for _, t := range b.Typen {
			if t == 1 || t == 2 {
				typ = t
				break
			}
		}
		if typ == 0 {
			return b, fmt.Errorf("keine bekannten Sicherheitstypen: %v", b.Typen)
		}
		if _, err := rw.Write([]byte{typ}); err != nil {
			return nil, err
		}
	}
	mitErgebnis := b.Version == "003.008"
	if typ == 2 {
		ch := make([]byte, 16)
		if _, err := io.ReadFull(rw, ch); err != nil {
			return nil, err
		}
		if _, err := rw.Write(vnc.DESAntwort(passwort, ch)); err != nil {
			return nil, err
		}
		mitErgebnis = true
	}
	if mitErgebnis {
		var status uint32
		if err := zahlLesen(rw, &status); err != nil {
			return nil, err
		}
		if status != 0 {
			return b, ErrAbgelehnt
		}
	}
	return b, nil
}

// Init schickt ClientInit (geteilt) und liest ServerInit.
func (b *Betrachter) Init() error {
	if _, err := b.rw.Write([]byte{1}); err != nil {
		return err
	}
	return b.ServerInitLesen()
}

// ServerInitLesen liest ServerInit (nach eigenem ClientInit).
func (b *Betrachter) ServerInitLesen() error {
	si := make([]byte, 24)
	if _, err := io.ReadFull(b.rw, si); err != nil {
		return err
	}
	b.Breite, b.Hoehe = int(binary.BigEndian.Uint16(si[0:2])), int(binary.BigEndian.Uint16(si[2:4]))
	n := binary.BigEndian.Uint32(si[20:24])
	if n > 1<<16 {
		return fmt.Errorf("Name zu lang: %d", n)
	}
	name := make([]byte, n)
	if _, err := io.ReadFull(b.rw, name); err != nil {
		return err
	}
	b.Name = string(name)
	return nil
}

// Kodierungen meldet die Kodierungen des Betrachters (SetEncodings).
func (b *Betrachter) Kodierungen(k ...int32) error {
	m := []byte{2, 0}
	m = binary.BigEndian.AppendUint16(m, uint16(len(k)))
	for _, x := range k {
		m = binary.BigEndian.AppendUint32(m, uint32(x))
	}
	_, err := b.rw.Write(m)
	return err
}

// BildAnfordern schickt FramebufferUpdateRequest fuer das ganze Bild.
func (b *Betrachter) BildAnfordern(inkrementell bool) error {
	m := []byte{3, 0, 0, 0, 0, 0}
	if inkrementell {
		m[1] = 1
	}
	m = binary.BigEndian.AppendUint16(m, uint16(b.Breite))
	m = binary.BigEndian.AppendUint16(m, uint16(b.Hoehe))
	_, err := b.rw.Write(m)
	return err
}

// Zeiger schickt ein PointerEvent.
func (b *Betrachter) Zeiger(maske uint8, x, y int) error {
	m := []byte{5, maske}
	m = binary.BigEndian.AppendUint16(m, uint16(x))
	m = binary.BigEndian.AppendUint16(m, uint16(y))
	_, err := b.rw.Write(m)
	return err
}

// Taste schickt ein KeyEvent.
func (b *Betrachter) Taste(unten bool, keysym uint32) error {
	m := []byte{4, 0, 0, 0}
	if unten {
		m[1] = 1
	}
	m = binary.BigEndian.AppendUint32(m, keysym)
	_, err := b.rw.Write(m)
	return err
}

// AblageSenden schickt ClientCutText (Latin-1).
func (b *Betrachter) AblageSenden(text string) error {
	m := []byte{6, 0, 0, 0}
	m = binary.BigEndian.AppendUint32(m, uint32(len(text)))
	_, err := b.rw.Write(append(m, text...))
	return err
}

// GroesseSetzen schickt SetDesktopSize mit einem Bildschirm.
func (b *Betrachter) GroesseSetzen(breite, hoehe int) error {
	m := []byte{251, 0}
	m = binary.BigEndian.AppendUint16(m, uint16(breite))
	m = binary.BigEndian.AppendUint16(m, uint16(hoehe))
	m = append(m, 1, 0)
	m = binary.BigEndian.AppendUint32(m, 0)
	m = append(m, 0, 0, 0, 0)
	m = binary.BigEndian.AppendUint16(m, uint16(breite))
	m = binary.BigEndian.AppendUint16(m, uint16(hoehe))
	m = binary.BigEndian.AppendUint32(m, 0)
	_, err := b.rw.Write(m)
	return err
}

// Aktualisierung liest die naechste Bildaktualisierung; ServerCutText dazwischen landet in
// Ablage. Groessenaenderungen passen Breite und Hoehe an.
func (b *Betrachter) Aktualisierung() ([]Rechteck, error) {
	for {
		typ := make([]byte, 1)
		if _, err := io.ReadFull(b.rw, typ); err != nil {
			return nil, err
		}
		switch typ[0] {
		case 0:
		case 2: // Bell
			continue
		case 3:
			k := make([]byte, 7)
			if _, err := io.ReadFull(b.rw, k); err != nil {
				return nil, err
			}
			text := make([]byte, binary.BigEndian.Uint32(k[3:7]))
			if _, err := io.ReadFull(b.rw, text); err != nil {
				return nil, err
			}
			b.Ablage = append(b.Ablage, string(text))
			continue
		default:
			return nil, fmt.Errorf("unbekannte Server-Nachricht %d", typ[0])
		}
		k := make([]byte, 3)
		if _, err := io.ReadFull(b.rw, k); err != nil {
			return nil, err
		}
		n := int(binary.BigEndian.Uint16(k[1:3]))
		var aus []Rechteck
		for i := 0; i < n; i++ {
			kopf := make([]byte, 12)
			if _, err := io.ReadFull(b.rw, kopf); err != nil {
				return nil, err
			}
			r := Rechteck{X: int(binary.BigEndian.Uint16(kopf[0:2])), Y: int(binary.BigEndian.Uint16(kopf[2:4])),
				B: int(binary.BigEndian.Uint16(kopf[4:6])), H: int(binary.BigEndian.Uint16(kopf[6:8])),
				Kodierung: int32(binary.BigEndian.Uint32(kopf[8:12]))}
			var laenge int
			switch r.Kodierung {
			case kodRaw:
				laenge = r.B * r.H * 4
			case kodCursor:
				laenge = r.B*r.H*4 + (r.B+7)/8*r.H
			case kodDesktopSize:
				b.Breite, b.Hoehe = r.B, r.H
			case kodExtendedDesktopSize:
				anz := make([]byte, 4)
				if _, err := io.ReadFull(b.rw, anz); err != nil {
					return nil, err
				}
				laenge = 16 * int(anz[0])
				if r.Y == 0 {
					b.Breite, b.Hoehe = r.B, r.H
				}
			default:
				return nil, fmt.Errorf("unbekannte Kodierung %d", r.Kodierung)
			}
			r.Daten = make([]byte, laenge)
			if _, err := io.ReadFull(b.rw, r.Daten); err != nil {
				return nil, err
			}
			aus = append(aus, r)
		}
		return aus, nil
	}
}

// Pixel liefert die Farbe 0xRRGGBB eines Raw-Rechtecks (Format des Servers).
func (r Rechteck) Pixel(x, y int) uint32 {
	i := ((y-r.Y)*r.B + (x - r.X)) * 4
	return binary.LittleEndian.Uint32(r.Daten[i:]) & 0xFFFFFF
}
