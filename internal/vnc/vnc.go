// Package vnc meldet Keyway als Client an einem VNC-Server an (RFB 3.3, 3.7 und 3.8,
// Sicherheitstypen None und VNC-Authentication) und spielt gegenueber dem VNC-Viewer im
// Browser die Begruessung eines RFB-3.8-Servers ohne Passwort. Ab ClientInit laufen die
// Daten unveraendert zwischen Browser und VNC-Server. Bietet der Server keinen der
// beiden Typen an (VeNCrypt, RA2, Apple …), reicht Keyway alles roh durch, und der
// Viewer im Browser meldet sich selbst an.
//
// Fehler fuer den Anwender kommen als texte.Meldung (Deutsch und Englisch).
package vnc

import (
	"bytes"
	"context"
	"crypto/des"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"math/bits"
	"net"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf16"

	"github.com/uiff/ssh-app/internal/texte"
)

// Sicherheitstypen, die Keyway selbst beherrscht.
const (
	TypNone = 1
	TypVNC  = 2
)

// Fehlerarten (Vergleich mit errors.Is).
var (
	// ErrPasswortNoetig: der Server verlangt VNC-Authentication, es ist kein Passwort bekannt.
	ErrPasswortNoetig error = texte.Neu("vnc.passwort.noetig")
	// ErrPasswortFalsch: der Server hat das Passwort abgelehnt.
	ErrPasswortFalsch error = texte.Neu("vnc.passwort.falsch")
	// ErrAbgebrochen: der Aufbau wurde abgebrochen (Browser weg).
	ErrAbgebrochen error = texte.Neu("vnc.abgebrochen")
)

// Fristen; Variablen, damit Tests sie kuerzen koennen.
var (
	// VerbindenFrist begrenzt den TCP-Aufbau bei direkter Verbindung.
	VerbindenFrist = 15 * time.Second
	// AnmeldeFrist begrenzt Versions- und Sicherheitsphase mit dem VNC-Server.
	AnmeldeFrist = 15 * time.Second
)

// Ergebnis der Anmeldung am VNC-Server.
type Ergebnis struct {
	// Version, die Keyway dem Server geschickt hat: "003.003", "003.007" oder "003.008".
	Version string
	// ServerVersion, wie der Server sie gemeldet hat (z. B. "005.000").
	ServerVersion string
	// Typ ist der gewaehlte Sicherheitstyp (TypNone, TypVNC); 0 beim Durchreichen.
	Typ int
	// Typen, die der Server angeboten hat (fuer das Log).
	Typen []byte
	// Durchreichen: Keyway meldet sich nicht selbst an, der Viewer im Browser tut es.
	Durchreichen bool
	// Vorspann: beim Durchreichen die schon gelesenen Bytes des Servers (ProtocolVersion
	// und Liste der Sicherheitstypen), die zuerst an den Browser gehen.
	Vorspann []byte
}

// versionen bildet die Versionen der Server auf die Version ab, die Keyway spricht
// (dieselbe Tabelle wie noVNC, damit der Viewer beim Durchreichen dieselbe waehlt).
var versionen = map[string]string{
	"003.003": "003.003",
	"003.006": "003.003", // UltraVNC
	"003.007": "003.007",
	"003.008": "003.008",
	"003.889": "003.008", // Apple Remote Desktop
	"004.000": "003.008", // Intel AMT KVM
	"004.001": "003.008", // RealVNC 4.6
	"005.000": "003.008", // RealVNC 5.3
}

// Anmelden fuehrt mit dem VNC-Server hinter conn die Versions- und Sicherheitsphase als
// Client durch. ziel benennt den Server in Meldungen. passwort ist nil, wenn keines bekannt
// ist; verlangt der Server eines, kommt ErrPasswortNoetig. Danach erwartet der Server
// ClientInit. Bietet er weder None noch VNC-Authentication an, kommt ein Ergebnis mit
// Durchreichen. Bei einem Fehler ist conn nicht mehr zu gebrauchen.
func Anmelden(ctx context.Context, conn net.Conn, ziel string, passwort *string) (erg Ergebnis, err error) {
	fctx, cancel := context.WithTimeout(ctx, AnmeldeFrist)
	defer cancel()
	stop := context.AfterFunc(fctx, func() { conn.Close() })
	defer func() {
		if !stop() { // die Frist lief ab oder der Browser ist weg: conn ist zu
			switch {
			case ctx.Err() != nil:
				err = ErrAbgebrochen
			default:
				err = texte.Neu("vnc.keine.antwort", ziel, sekunden(AnmeldeFrist))
			}
		}
	}()
	a := &anmeldung{conn: conn, ziel: ziel}
	return a.ablauf(passwort)
}

type anmeldung struct {
	conn net.Conn
	ziel string
}

func (a *anmeldung) ablauf(passwort *string) (Ergebnis, error) {
	var erg Ergebnis
	kopf := make([]byte, 12)
	if err := a.lesen(kopf); err != nil {
		return erg, err
	}
	if !bytes.HasPrefix(kopf, []byte("RFB ")) || kopf[11] != '\n' {
		log.Printf("VNC %s: keine RFB-Begrüssung: %q", a.ziel, kopf)
		return erg, texte.Neu("vnc.kein.vnc", a.ziel)
	}
	erg.ServerVersion = string(kopf[4:11])
	v, ok := versionen[erg.ServerVersion]
	if !ok {
		return erg, texte.Neu("vnc.version", a.ziel, sauber(erg.ServerVersion, 20))
	}
	erg.Version = v
	if err := a.schreiben([]byte("RFB " + v + "\n")); err != nil {
		return erg, err
	}

	if v == "003.003" {
		// Der Server bestimmt den Typ.
		var typ uint32
		if err := a.lesenZahl(&typ); err != nil {
			return erg, err
		}
		erg.Typen = []byte{byte(typ)}
		switch typ {
		case 0:
			return erg, a.abgelehnt(true)
		case TypNone:
			erg.Typ = TypNone
			return erg, nil
		case TypVNC:
			erg.Typ = TypVNC
			return erg, a.vncAuth(passwort, false)
		}
		return erg, texte.Neu("vnc.typ.unbekannt", a.ziel, int(typ))
	}

	// 3.7 und 3.8: der Server bietet an, der Client waehlt.
	anzahl := make([]byte, 1)
	if err := a.lesen(anzahl); err != nil {
		return erg, err
	}
	if anzahl[0] == 0 {
		return erg, a.abgelehnt(true)
	}
	typen := make([]byte, anzahl[0])
	if err := a.lesen(typen); err != nil {
		return erg, err
	}
	erg.Typen = typen
	mit38 := v == "003.008"
	switch {
	case slices.Contains(typen, TypNone):
		erg.Typ = TypNone
		if err := a.schreiben([]byte{TypNone}); err != nil {
			return erg, err
		}
		if mit38 {
			if err := a.ergebnis(mit38, false); err != nil {
				return erg, err
			}
		}
		return erg, nil
	case slices.Contains(typen, TypVNC):
		erg.Typ = TypVNC
		if passwort == nil {
			return erg, ErrPasswortNoetig
		}
		if err := a.schreiben([]byte{TypVNC}); err != nil {
			return erg, err
		}
		return erg, a.vncAuth(passwort, mit38)
	}
	// Weder None noch VNC-Authentication: der Viewer im Browser meldet sich an.
	erg.Durchreichen = true
	erg.Vorspann = slices.Concat(kopf, anzahl, typen)
	return erg, nil
}

// vncAuth beantwortet die DES-Challenge und liest das Ergebnis.
func (a *anmeldung) vncAuth(passwort *string, mit38 bool) error {
	if passwort == nil {
		return ErrPasswortNoetig
	}
	challenge := make([]byte, 16)
	if err := a.lesen(challenge); err != nil {
		return err
	}
	if err := a.schreiben(DESAntwort(*passwort, challenge)); err != nil {
		return err
	}
	return a.ergebnis(mit38, true)
}

// ergebnis liest SecurityResult. Bei RFB 3.8 folgt auf eine Ablehnung ein Grund.
func (a *anmeldung) ergebnis(mit38, mitPasswort bool) error {
	var status uint32
	if err := a.lesenZahl(&status); err != nil {
		if mitPasswort && errors.Is(err, errBeendet) {
			// Manche Server trennen bei falschem Passwort ohne Ergebnis.
			return ErrPasswortFalsch
		}
		return err
	}
	if status == 0 {
		return nil
	}
	grund := ""
	if mit38 {
		grund = a.grundLesen()
	}
	if mitPasswort {
		log.Printf("VNC %s: Passwort abgelehnt (%q)", a.ziel, grund)
		return ErrPasswortFalsch
	}
	return a.mitGrund(grund)
}

// abgelehnt: der Server lehnt die Verbindung schon vor der Anmeldung ab (mit Grund).
func (a *anmeldung) abgelehnt(mitGrund bool) error {
	grund := ""
	if mitGrund {
		grund = a.grundLesen()
	}
	return a.mitGrund(grund)
}

func (a *anmeldung) mitGrund(grund string) error {
	log.Printf("VNC %s: Verbindung abgelehnt (%q)", a.ziel, grund)
	if grund == "" {
		return texte.Neu("vnc.abgelehnt")
	}
	return texte.Neu("vnc.abgelehnt.grund", grund)
}

// grundLesen liest einen Grund (Laenge + Text); Fehler ergeben einen leeren Grund.
func (a *anmeldung) grundLesen() string {
	var n uint32
	if a.lesenZahl(&n) != nil || n == 0 {
		return ""
	}
	b := make([]byte, min(n, 4096))
	if a.lesen(b) != nil {
		return ""
	}
	return sauber(string(b), 200)
}

var errBeendet = errors.New("beendet")

func (a *anmeldung) lesen(b []byte) error {
	if _, err := io.ReadFull(a.conn, b); err != nil {
		log.Printf("VNC %s lesen: %v", a.ziel, err)
		if errors.Is(err, syscall.ECONNRESET) {
			return texte.Neu("vnc.netz.zurueckgesetzt", a.ziel).MitUrsache(errBeendet)
		}
		return texte.Neu("vnc.beendet", a.ziel).MitUrsache(errBeendet)
	}
	return nil
}

func (a *anmeldung) lesenZahl(z *uint32) error {
	b := make([]byte, 4)
	if err := a.lesen(b); err != nil {
		return err
	}
	*z = binary.BigEndian.Uint32(b)
	return nil
}

func (a *anmeldung) schreiben(b []byte) error {
	if _, err := a.conn.Write(b); err != nil {
		log.Printf("VNC %s schreiben: %v", a.ziel, err)
		return texte.Neu("vnc.beendet", a.ziel).MitUrsache(errBeendet)
	}
	return nil
}

// DESAntwort verschluesselt die Challenge der VNC-Authentication. Der Schluessel entsteht
// wie bei noVNC (RFB.genDES, charCodeAt): die ersten 8 UTF-16-Einheiten des Passworts, von
// jeder nur das untere Byte (bis U+00FF also Latin-1, «ü» = 0xFC; nicht UTF-8), mit Nullen
// aufgefuellt, jedes Byte bitgespiegelt. So gilt ein Passwort mit Umlauten bei Keyway
// gleich wie beim Viewer im Browser und bei Servern, die Latin-1 erwarten.
func DESAntwort(passwort string, challenge []byte) []byte {
	schluessel := make([]byte, 0, 8)
	for _, r := range passwort {
		for _, u := range utf16.AppendRune(nil, r) {
			if len(schluessel) < 8 {
				schluessel = append(schluessel, bits.Reverse8(byte(u)))
			}
		}
		if len(schluessel) == 8 {
			break
		}
	}
	schluessel = append(schluessel, make([]byte, 8-len(schluessel))...)
	c, _ := des.NewCipher(schluessel) // 8 Bytes: kein Fehler moeglich
	aus := make([]byte, len(challenge))
	for i := 0; i+8 <= len(challenge); i += 8 {
		c.Encrypt(aus[i:i+8], challenge[i:i+8])
	}
	return aus
}

// sauber kuerzt einen Text des Servers und entfernt Steuerzeichen.
func sauber(s string, n int) string {
	s = strings.ToValidUTF8(s, "?")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func sekunden(d time.Duration) int { return int((d + time.Second/2) / time.Second) }

// --- Gegenueber dem Browser ---

// BrowserBegruessen spielt gegenueber dem Viewer im Browser die Versions- und
// Sicherheitsphase eines RFB-3.8-Servers mit Sicherheitstyp None (Keyway hat sich
// schon angemeldet). Danach folgen ClientInit des Browsers und ServerInit des Servers.
func BrowserBegruessen(b io.ReadWriter) error {
	if _, err := b.Write([]byte("RFB 003.008\n")); err != nil {
		return err
	}
	v := make([]byte, 12)
	if _, err := io.ReadFull(b, v); err != nil {
		return err
	}
	switch string(v) {
	case "RFB 003.007\n", "RFB 003.008\n":
		if _, err := b.Write([]byte{1, TypNone}); err != nil {
			return err
		}
		typ := make([]byte, 1)
		if _, err := io.ReadFull(b, typ); err != nil {
			return err
		}
		if typ[0] != TypNone {
			return texte.Neu("vnc.browser.typ")
		}
		if string(v) == "RFB 003.008\n" {
			_, err := b.Write([]byte{0, 0, 0, 0}) // SecurityResult OK
			return err
		}
		return nil
	case "RFB 003.003\n":
		_, err := b.Write([]byte{0, 0, 0, TypNone})
		return err
	}
	return texte.Neu("vnc.browser.version")
}

// BrowserDurchreichen schickt dem Viewer im Browser die schon gelesenen Bytes des
// Servers in der Reihenfolge des Protokolls: zuerst die ProtocolVersion, dann (nach der
// Version des Viewers, die Keyway dem Server schon geschickt hat und hier abnimmt) die
// Liste der Sicherheitstypen. noVNC liest die Liste erst nach seiner Antwort. Ab hier
// laeuft alles roh durch.
func BrowserDurchreichen(b io.ReadWriter, erg Ergebnis) error {
	if len(erg.Vorspann) < 12 {
		return texte.Neu("vnc.durchreichen.version")
	}
	if _, err := b.Write(erg.Vorspann[:12]); err != nil {
		return err
	}
	v := make([]byte, 12)
	if _, err := io.ReadFull(b, v); err != nil {
		return err
	}
	if string(v) != "RFB "+erg.Version+"\n" {
		log.Printf("VNC durchreichen: Browser wählt %q, Keyway schickte %s", v, erg.Version)
		return texte.Neu("vnc.durchreichen.version")
	}
	if rest := erg.Vorspann[12:]; len(rest) > 0 {
		if _, err := b.Write(rest); err != nil {
			return err
		}
	}
	return nil
}

// --- Verbindung ---

// Waehlen verbindet direkt per TCP mit adresse:port. Fehler kommen als verstaendlicher Satz.
func Waehlen(ctx context.Context, adresse string, port int) (net.Conn, error) {
	hp := HostPort(adresse, port)
	d := net.Dialer{Timeout: VerbindenFrist}
	c, err := d.DialContext(ctx, "tcp", hp)
	if err == nil {
		return c, nil
	}
	log.Printf("VNC verbinden mit %s: %v", hp, err)
	host := strings.TrimSpace(adresse)
	var dns *net.DNSError
	var ne net.Error
	switch {
	case ctx.Err() != nil:
		return nil, ErrAbgebrochen
	case errors.As(err, &dns):
		if dns.IsNotFound {
			return nil, texte.Neu("vnc.netz.name.unbekannt", dns.Name)
		}
		return nil, texte.Neu("vnc.netz.name.dns", dns.Name)
	case errors.Is(err, syscall.ECONNREFUSED):
		return nil, texte.Neu("vnc.netz.abgelehnt", hp)
	case errors.Is(err, syscall.EHOSTUNREACH), errors.Is(err, syscall.ENETUNREACH):
		return nil, texte.Neu("vnc.netz.kein.weg", host)
	case errors.Is(err, syscall.ECONNRESET):
		return nil, texte.Neu("vnc.netz.zurueckgesetzt", hp)
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return nil, texte.Neu("vnc.netz.keine.antwort", host, sekunden(VerbindenFrist))
	}
	return nil, texte.Neu("vnc.netz.fehlgeschlagen", hp)
}

// HostPort setzt Adresse und Port zusammen (IPv6 in Klammern).
func HostPort(adresse string, port int) string {
	return net.JoinHostPort(strings.TrimSpace(adresse), strconv.Itoa(port))
}
