package vnc

import "encoding/binary"

// Eingaben erkennt im Datenstrom des Browsers zum VNC-Server (ab ClientInit) die
// Nachrichten, die Bedienung sind: Tasten, Zeiger, Zwischenablage, Bildschirmgroesse.
// Automatische Nachrichten des Viewers (Bildanforderungen, Kodierungen, Fences …)
// zaehlen nicht, damit ein offenes Fenster die Anmeldung nicht ewig verlaengert.
// Versteht Eingaben eine Nachricht nicht, zaehlt ab dann jeder Datenblock als Bedienung.
type Eingaben struct {
	clientInit bool   // ClientInit (1 Byte) schon gesehen
	kopf       []byte // gesammelter Kopf der laufenden Nachricht
	rest       int    // noch zu ueberspringende Bytes der laufenden Nachricht
	unbekannt  bool
}

// Nachrichtentypen Browser -> Server (RFC 6143 und Erweiterungen, wie noVNC sie sendet).
const (
	msgPixelFormat       = 0
	msgKodierungen       = 2
	msgBildAnfordern     = 3
	msgTaste             = 4
	msgZeiger            = 5
	msgZwischenablage    = 6
	msgFortlaufend       = 150
	msgFence             = 248
	msgXvp               = 250
	msgBildschirmGroesse = 251
	msgQemu              = 255
)

// Pruefen verarbeitet den naechsten Datenblock und meldet, ob er Bedienung enthaelt.
func (e *Eingaben) Pruefen(p []byte) (bedient bool) {
	for len(p) > 0 {
		if e.unbekannt {
			return true
		}
		if !e.clientInit {
			e.clientInit = true
			p = p[1:]
			continue
		}
		if e.rest > 0 {
			n := min(e.rest, len(p))
			e.rest -= n
			p = p[n:]
			continue
		}
		// Kopf sammeln, bis die Laenge der Nachricht feststeht.
		e.kopf = append(e.kopf, p[0])
		p = p[1:]
		laenge, eingabe, ok := nachrichtLaenge(e.kopf)
		if !ok {
			e.unbekannt = true
			return true
		}
		if laenge < 0 {
			continue // Kopf noch unvollstaendig
		}
		if eingabe {
			bedient = true
		}
		e.rest = laenge - len(e.kopf)
		e.kopf = e.kopf[:0]
	}
	return bedient
}

// nachrichtLaenge bestimmt aus dem Kopf die Gesamtlaenge einer Nachricht (-1: Kopf
// noch zu kurz) und ob sie Bedienung ist. ok=false: unbekannte Nachricht.
func nachrichtLaenge(k []byte) (laenge int, eingabe, ok bool) {
	switch k[0] {
	case msgPixelFormat:
		return 20, false, true
	case msgKodierungen:
		if len(k) < 4 {
			return -1, false, true
		}
		return 4 + 4*int(binary.BigEndian.Uint16(k[2:4])), false, true
	case msgBildAnfordern:
		return 10, false, true
	case msgTaste:
		return 8, true, true
	case msgZeiger:
		if len(k) < 2 {
			return -1, true, true
		}
		if k[1]&0x80 != 0 { // erweiterter Zeiger mit zweitem Tastenbyte
			return 7, true, true
		}
		return 6, true, true
	case msgZwischenablage:
		if len(k) < 8 {
			return -1, true, true
		}
		n := int32(binary.BigEndian.Uint32(k[4:8]))
		if n < 0 { // erweiterte Zwischenablage: Laenge negativ
			n = -n
		}
		if n < 0 {
			return 0, false, false
		}
		return 8 + int(n), true, true
	case msgFortlaufend:
		return 10, false, true
	case msgFence:
		if len(k) < 9 {
			return -1, false, true
		}
		return 9 + int(k[8]), false, true
	case msgXvp:
		return 4, true, true
	case msgBildschirmGroesse:
		if len(k) < 8 {
			return -1, true, true
		}
		return 8 + 16*int(k[6]), true, true
	case msgQemu:
		if len(k) < 2 {
			return -1, false, true
		}
		switch k[1] {
		case 0: // erweitertes Tastenereignis
			return 12, true, true
		case 1: // Audio
			if len(k) < 4 {
				return -1, false, true
			}
			if binary.BigEndian.Uint16(k[2:4]) == 2 {
				return 10, false, true
			}
			return 4, false, true
		}
	}
	return 0, false, false
}
