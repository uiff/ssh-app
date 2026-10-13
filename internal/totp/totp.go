// Package totp erzeugt und prueft Einmal-Codes fuer die Zwei-Faktor-Anmeldung:
// TOTP nach RFC 6238 (HMAC-SHA1, 6 Stellen, 30 Sekunden) und Ersatzcodes fuer den
// Fall, dass das Telefon fehlt.
//
// Alles sind reine Funktionen ohne Netz und ohne eigene Uhr: Die Zeit kommt immer
// vom Aufrufer (Tests rechnen so mit festen Zeitpunkten).
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	// Stellen eines Codes.
	Stellen = 6
	// Periode: so viele Sekunden gilt ein Code (ein Zeitschritt).
	Periode = 30
	// GeheimnisLaenge in Byte (160 Bit, wie von RFC 4226 empfohlen).
	GeheimnisLaenge = 20
	// Fenster: so viele Zeitschritte vor und nach der eigenen Uhr gilt ein Code noch.
	Fenster = 1
	// Suchweite fuer den Hinweis auf eine falsch gehende Uhr (40 Schritte = 20 Minuten).
	Suchweite = 40

	// ErsatzAnzahl: so viele Ersatzcodes gibt es auf einmal.
	ErsatzAnzahl = 10
	// ErsatzLaenge: Zeichen je Ersatzcode (ohne Bindestrich).
	ErsatzLaenge = 8
	// ErsatzZeichen: ohne 0/O, 1/I/L, damit sich nichts verwechseln laesst. 32 Zeichen:
	// fuenf Zufallsbits je Zeichen, ohne Verzerrung.
	ErsatzZeichen = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NeuesGeheimnis liefert ein zufaelliges Geheimnis (20 Byte).
func NeuesGeheimnis() []byte {
	return zufall(GeheimnisLaenge)
}

// Base32 kodiert ein Geheimnis nach RFC 4648 ohne Auffuellzeichen (wie Authenticator-Apps es erwarten).
func Base32(geheimnis []byte) string {
	return b32.EncodeToString(geheimnis)
}

// Gruppiert teilt einen Schluessel zum Abtippen in Vierergruppen: «ABCD EFGH IJ».
func Gruppiert(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && i%4 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Schritt liefert den Zeitschritt (Zaehler) zu einem Zeitpunkt.
func Schritt(t time.Time) int64 {
	s := t.Unix()
	if s < 0 { // abrunden, auch vor 1970
		return (s - Periode + 1) / Periode
	}
	return s / Periode
}

// CodeBei liefert den Code eines Zeitschritts.
func CodeBei(geheimnis []byte, schritt int64) string {
	return hotp(geheimnis, uint64(schritt), Stellen)
}

// Code liefert den Code zu einem Zeitpunkt.
func Code(geheimnis []byte, t time.Time) string {
	return CodeBei(geheimnis, Schritt(t))
}

// hotp nach RFC 4226: HMAC-SHA1 ueber den Zaehler, dynamisch gekuerzt.
func hotp(geheimnis []byte, zaehler uint64, stellen int) string {
	var z [8]byte
	binary.BigEndian.PutUint64(z[:], zaehler)
	m := hmac.New(sha1.New, geheimnis)
	m.Write(z[:])
	h := m.Sum(nil)
	o := h[len(h)-1] & 0x0f
	wert := binary.BigEndian.Uint32(h[o:o+4]) & 0x7fffffff
	mod := uint32(1)
	for range stellen {
		mod *= 10
	}
	s := strconv.FormatUint(uint64(wert%mod), 10)
	return strings.Repeat("0", stellen-len(s)) + s
}

// NurZiffern bereinigt eine Eingabe (Leerzeichen erlaubt) und meldet, ob sie ein
// Code mit genau Stellen Ziffern ist.
func NurZiffern(eingabe string) (string, bool) {
	s := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, eingabe)
	if len(s) != Stellen {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return "", false
		}
	}
	return s, true
}

// Pruefen vergleicht einen Code mit den Codes der Zeitschritte jetzt-fenster bis
// jetzt+fenster und liefert den passenden Schritt (bei mehreren den spaetesten).
// Der Vergleich laeuft in konstanter Zeit ueber alle Schritte.
func Pruefen(geheimnis []byte, code string, jetzt time.Time, fenster int) (schritt int64, ok bool) {
	if len(code) != Stellen {
		return 0, false
	}
	basis := Schritt(jetzt)
	for d := -fenster; d <= fenster; d++ {
		s := basis + int64(d)
		if subtle.ConstantTimeCompare([]byte(CodeBei(geheimnis, s)), []byte(code)) == 1 {
			schritt, ok = s, true
		}
	}
	return schritt, ok
}

// Abweichung sucht einen Code, der nicht im Fenster passt, weiter weg (bis weite
// Schritte vor und nach jetzt) und liefert den naechstgelegenen Abstand in Schritten:
// positiv = der Code gehoert zu einer spaeteren Uhrzeit als jetzt. Dient nur dem
// Hinweis auf eine falsch gehende Uhr; ein solcher Code gilt nie.
//
// Den Schritt direkt nach dem Fenster (fenster+1) meldet sie nicht: Sein Code gilt
// spaetestens einen Schritt spaeter von selbst (Telefon 30-60 s vor). Ein Hinweis
// wuerde dort verwirren und einen Code bestaetigen, der gleich darauf gilt.
func Abweichung(geheimnis []byte, code string, jetzt time.Time, fenster, weite int) (schritte int64, ok bool) {
	if len(code) != Stellen {
		return 0, false
	}
	basis := Schritt(jetzt)
	for d := fenster + 1; d <= weite; d++ {
		for _, v := range []int64{int64(d), -int64(d)} {
			if v == int64(fenster)+1 {
				continue
			}
			if CodeBei(geheimnis, basis+v) == code {
				return v, true
			}
		}
	}
	return 0, false
}

// URI baut den otpauth-Link fuer Authenticator-Apps (Format «Key Uri» von Google
// Authenticator): otpauth://totp/<aussteller>:<konto>?secret=…&issuer=…&algorithm=SHA1&digits=6&period=30
func URI(aussteller, konto string, geheimnis []byte) string {
	return "otpauth://totp/" + escape(aussteller) + ":" + escape(konto) +
		"?secret=" + Base32(geheimnis) +
		"&issuer=" + escape(aussteller) +
		"&algorithm=SHA1&digits=" + strconv.Itoa(Stellen) +
		"&period=" + strconv.Itoa(Periode)
}

// escape kodiert alles ausser den unreservierten Zeichen (RFC 3986) und «@» als %XX.
// Ein «:» im Konto wird so zu %3A und trennt nicht faelschlich Aussteller und Konto.
func escape(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || strings.IndexByte("-._~@", c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	return b.String()
}

// --- Ersatzcodes ---

// NeueErsatzcodes liefert n verschiedene Ersatzcodes in der gespeicherten Form
// (ErsatzLaenge Zeichen, ohne Bindestrich). Zum Anzeigen: ErsatzAnzeige.
func NeueErsatzcodes(n int) []string {
	codes := make([]string, 0, n)
	gibt := map[string]bool{}
	for len(codes) < n {
		roh := zufall(ErsatzLaenge)
		b := make([]byte, ErsatzLaenge)
		for i, x := range roh {
			b[i] = ErsatzZeichen[x&31]
		}
		clear(roh)
		if c := string(b); !gibt[c] {
			gibt[c] = true
			codes = append(codes, c)
		}
	}
	return codes
}

// ErsatzNormal bereinigt eine Eingabe: Grossbuchstaben, ohne Leerzeichen und Bindestriche.
func ErsatzNormal(eingabe string) string {
	return strings.ToUpper(strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Pd, r) || unicode.IsSpace(r) { // Pd: auch Bindestriche der Telefon-Tastatur
			return -1
		}
		return r
	}, eingabe))
}

// IstErsatzcode meldet, ob eine bereinigte Eingabe die Form eines Ersatzcodes hat.
func IstErsatzcode(c string) bool {
	if len(c) != ErsatzLaenge {
		return false
	}
	for i := 0; i < len(c); i++ {
		if strings.IndexByte(ErsatzZeichen, c[i]) < 0 {
			return false
		}
	}
	return true
}

// ErsatzAnzeige formatiert einen Ersatzcode zum Ablesen: «XXXX-XXXX».
func ErsatzAnzeige(c string) string {
	if len(c) != ErsatzLaenge {
		return c
	}
	return c[:ErsatzLaenge/2] + "-" + c[ErsatzLaenge/2:]
}

func zufall(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("Zufallsquelle: " + err.Error())
	}
	return b
}
