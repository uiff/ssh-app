// Package vault schuetzt alle Geheimnisse (Passwoerter, private Schluessel)
// mit einem zufaelligen Datenschluessel (DEK). Der DEK liegt nur verpackt auf
// der Platte, und zwar zweimal:
//
//   - verpackt mit dem Anmeldepasswort (Argon2id) — fuer den Alltag,
//   - verpackt mit dem Sicherheitsschluessel (Notfallschluessel) — um das
//     Passwort zurueckzusetzen oder ein Backup auf einem anderen System
//     einzuspielen.
//
// Backups werden mit einem aus dem Sicherheitsschluessel abgeleiteten
// Schluessel verschluesselt; den haelt der Server ebenfalls nur verpackt.
//
// Waehrend eine SSH-Verbindung aufgebaut wird, muss der Server das Geheimnis
// im Speicher entschluesseln — eine Ende-zu-Ende-Verschluesselung bis zum
// Browser ist fuer ein SSH-Gateway nicht moeglich.
package vault

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/json"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/crypto/argon2"

	"github.com/uiff/ssh-app/internal/texte"
)

// MinPasswortLaenge ist die Mindestlaenge des Anmeldepassworts.
const MinPasswortLaenge = 10

// Fehler fuer den Anwender (texte.Meldung, Vergleich mit errors.Is).
var (
	ErrGesperrt         error = texte.Neu("vault.gesperrt")
	ErrFalsch           error = texte.Neu("vault.anmeldung.falsch")
	ErrSchluesselFalsch error = texte.Neu("vault.schluessel.falsch")
	ErrSchonAngelegt    error = texte.Neu("vault.schon.eingerichtet")
	ErrNichtAngelegt    error = texte.Neu("vault.nicht.eingerichtet")
	ErrZuKurz           error = texte.Neu("vault.passwort.kurz", MinPasswortLaenge)
	ErrBenutzer         error = texte.Neu("vault.benutzer.ungueltig")
	ErrAltFalsch        error = texte.Neu("vault.passwort.alt.falsch")
	ErrSchluesselFormat error = texte.Neu("vault.schluessel.format")

	// Zwei-Faktor-Anmeldung
	ErrPasswortFalsch    error = texte.Neu("vault.passwort.falsch")
	ErrZweitfaktorFehlt  error = texte.Neu("vault.zweitfaktor.fehlt")
	ErrZweitfaktorAktiv  error = texte.Neu("vault.zweitfaktor.aktiv")
	ErrZweitfaktorAus    error = texte.Neu("vault.zweitfaktor.aus")
	ErrZweitfaktorKaputt error = texte.Neu("vault.zweitfaktor.kaputt")
	ErrKopfGeaendert     error = texte.Neu("vault.kopf.geaendert")
)

// Kopf ist der dauerhaft gespeicherte Teil. Er enthaelt keine Klartext-Geheimnisse.
type Kopf struct {
	Version    int    `json:"v"`
	Benutzer   string `json:"user"`
	Salz       []byte `json:"salt"`
	Zeit       uint32 `json:"t"`
	SpeicherKB uint32 `json:"m"`
	Threads    uint8  `json:"p"`
	// DEK verpackt mit dem Passwort-Schluessel.
	DEKPasswort []byte `json:"dekPw"`
	// DEK verpackt mit dem Sicherheitsschluessel.
	DEKNotfall []byte `json:"dekRk"`
	// Backup-Schluessel verpackt mit dem DEK.
	BackupVerpackt []byte `json:"bk"`
	// Zweitfaktor (optional): TOTP-Geheimnis und Ersatzcodes, versiegelt mit dem DEK
	// (JSON von Zweitfaktor). Fehlt das Feld, ist die Zwei-Faktor-Anmeldung aus. Es reist
	// mit dem Kopf in jedes Backup; aeltere Versionen ohne das Feld lesen den Kopf weiter.
	Zweitfaktor []byte `json:"tf,omitempty"`
}

func (k *Kopf) argonParameter() {
	k.Salz = zufall(16)
	// RFC 9106, zweite Empfehlung (64 MiB) — laeuft auch auf einem Raspberry Pi.
	k.Zeit, k.SpeicherKB, k.Threads = 3, 64*1024, 4
}

func (k Kopf) passwortKEK(passwort string) []byte {
	return argon2.IDKey([]byte(passwort), k.Salz, k.Zeit, k.SpeicherKB, k.Threads, 32)
}

// Der Sicherheitsschluessel hat 256 Bit Zufall; eine schnelle Ableitung genuegt.
func notfallKEK(roh []byte) []byte       { return ableiten(roh, "ssh-app notfall dek") }
func backupSchluessel(roh []byte) []byte { return ableiten(roh, "ssh-app backup") }

func ableiten(roh []byte, zweck string) []byte {
	k, err := hkdf.Key(sha256.New, roh, nil, zweck, 32)
	if err != nil {
		panic(err)
	}
	return k
}

// Tresor haelt den Datenschluessel, solange er entsperrt ist.
//
// Abgeleitete Schluessel (KEKs, Backup-Schluessel) werden nach Gebrauch geleert,
// der DEK beim Ersetzen und beim Sperren. Kopien, die Go selbst anlegt (Passwort-
// Strings, interne Puffer von Argon2 und AES), lassen sich nicht garantiert loeschen.
type Tresor struct {
	mu   sync.RWMutex
	kopf *Kopf
	dek  []byte
	aead cipher.AEAD // fuer den DEK, einmal erzeugt
}

// dekSetzen ersetzt den DEK (t.mu muss gesperrt sein) und leert den bisherigen.
func (t *Tresor) dekSetzen(dek []byte) error {
	aead, err := gcmFuer(dek)
	if err != nil {
		clear(dek)
		return err
	}
	if t.dek != nil && (len(t.dek) == 0 || len(dek) == 0 || &t.dek[0] != &dek[0]) {
		clear(t.dek)
	}
	t.dek, t.aead = dek, aead
	return nil
}

// Neu laedt einen Tresor aus einem gespeicherten Kopf (leer = noch nicht eingerichtet).
func Neu(kopfJSON []byte) (*Tresor, error) {
	t := &Tresor{}
	if len(kopfJSON) == 0 {
		return t, nil
	}
	k, err := KopfLesen(kopfJSON)
	if err != nil {
		return nil, err
	}
	t.kopf = &k
	return t, nil
}

// KopfLesen prueft einen gespeicherten Kopf.
func KopfLesen(kopfJSON []byte) (Kopf, error) {
	var k Kopf
	if err := json.Unmarshal(kopfJSON, &k); err != nil {
		return k, texte.Neu("vault.kopf.unlesbar").MitUrsache(err)
	}
	if k.Version != 1 || len(k.Salz) < 16 || len(k.DEKPasswort) == 0 || len(k.DEKNotfall) == 0 || k.Benutzer == "" {
		return k, texte.Neu("vault.kopf.unvollstaendig")
	}
	return k, nil
}

func (t *Tresor) Angelegt() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.kopf != nil
}

func (t *Tresor) Offen() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.dek != nil
}

func (t *Tresor) Benutzer() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.kopf == nil {
		return ""
	}
	return t.kopf.Benutzer
}

// KopfJSON liefert den aktuellen Kopf zum Speichern.
func (t *Tresor) KopfJSON() []byte {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.kopf == nil {
		return nil
	}
	b, _ := json.Marshal(t.kopf)
	return b
}

func pruefeBenutzer(b string) error {
	if b == "" || len([]rune(b)) > 40 || strings.IndexFunc(b, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return ErrBenutzer
	}
	return nil
}

func pruefePasswort(p string) error {
	if len([]rune(p)) < MinPasswortLaenge {
		return ErrZuKurz
	}
	return nil
}

// Einrichten legt Benutzer und Passwort fest, entsperrt den Tresor und liefert
// den Sicherheitsschluessel. Er wird NICHT gespeichert und nur dieses eine Mal gezeigt.
func (t *Tresor) Einrichten(benutzer, passwort string) (kopf []byte, sicherheitsschluessel string, err error) {
	if err := pruefeBenutzer(benutzer); err != nil {
		return nil, "", err
	}
	if err := pruefePasswort(passwort); err != nil {
		return nil, "", err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.kopf != nil {
		return nil, "", ErrSchonAngelegt
	}
	dek := zufall(32)
	roh := zufall(32)
	defer clear(roh)
	k := Kopf{Version: 1, Benutzer: benutzer}
	k.argonParameter()
	kek, nk, bs := k.passwortKEK(passwort), notfallKEK(roh), backupSchluessel(roh)
	defer clear(kek)
	defer clear(nk)
	defer clear(bs)
	if k.DEKPasswort, err = versiegeln(kek, dek); err != nil {
		return nil, "", err
	}
	if k.DEKNotfall, err = versiegeln(nk, dek); err != nil {
		return nil, "", err
	}
	if k.BackupVerpackt, err = versiegeln(dek, bs); err != nil {
		return nil, "", err
	}
	if err := t.dekSetzen(dek); err != nil {
		return nil, "", err
	}
	t.kopf = &k
	kopf, err = json.Marshal(k)
	return kopf, SchluesselFormatieren(roh), err
}

// Anmelden prueft Benutzer und Passwort und entsperrt den Tresor. Ist die
// Zwei-Faktor-Anmeldung eingeschaltet, scheitert es mit ErrZweitfaktorFehlt
// (dann AnmeldungBeginnen und Abschliessen mit Pruefer).
func (t *Tresor) Anmelden(benutzer, passwort string) error {
	a, err := t.AnmeldungBeginnen(benutzer, passwort)
	if err != nil {
		return err
	}
	defer a.Verwerfen()
	_, err = a.Abschliessen(nil, nil)
	return err
}

// Anmeldung ist eine begonnene Anmeldung: Benutzer und Passwort stimmen, der DEK ist
// entpackt, aber noch nicht eingesetzt. Erst Abschliessen entsperrt den Tresor —
// mit eingeschalteter Zwei-Faktor-Anmeldung nur, wenn auch der zweite Faktor stimmt.
// Verwerfen leert die Schluessel (immer aufrufen, auch nach Abschliessen).
type Anmeldung struct {
	t        *Tresor
	kopf     *Kopf
	benutzer string
	kek, dek []byte
}

// Abschluss meldet, wie eine Anmeldung abgeschlossen wurde.
type Abschluss struct {
	Zweitfaktor  bool // der zweite Faktor war verlangt und stimmte
	Geaendert    bool // der Pruefer hat den Zweitfaktor geaendert (Ersatzcode verbraucht)
	ErsatzUebrig int  // verbleibende Ersatzcodes (mit Zweitfaktor)
}

// Zweitfaktor ist der entschluesselte Inhalt von Kopf.Zweitfaktor. Er liegt nur
// waehrend einer Pruefung im Speicher.
type Zweitfaktor struct {
	Geheimnis []byte    `json:"geheimnis"` // TOTP-Geheimnis (20 Byte)
	Ersatz    []string  `json:"ersatz"`    // unverbrauchte Ersatzcodes (bereinigt, ohne Bindestrich)
	Seit      time.Time `json:"seit"`      // eingeschaltet am
}

func (z *Zweitfaktor) leeren() { clear(z.Geheimnis) }

// Pruefer entscheidet ueber den zweiten Faktor (nil: kein Code angegeben). Darf er z
// aendern und liefert geaendert = true (etwa ein verbrauchter Ersatzcode), versiegelt
// der Tresor z neu. Er laeuft unter der Sperre des Tresors: keine Tresor-Methoden aufrufen.
type Pruefer func(z *Zweitfaktor) (geaendert bool, err error)

// Speicher legt einen geaenderten Kopf dauerhaft ab. Scheitert er, bleibt der Tresor
// unveraendert. nil: nur im Speicher aendern (Tests).
type Speicher func(kopf []byte) error

// AnmeldungBeginnen prueft Benutzer und Passwort (Argon2id, teuer). Der Tresor bleibt
// dabei unveraendert.
func (t *Tresor) AnmeldungBeginnen(benutzer, passwort string) (*Anmeldung, error) {
	t.mu.RLock()
	k := t.kopf
	t.mu.RUnlock()
	if k == nil {
		return nil, ErrNichtAngelegt
	}
	kek := k.passwortKEK(passwort)
	dek, err := oeffnen(kek, k.DEKPasswort)
	if err != nil {
		clear(kek)
		return nil, ErrFalsch
	}
	if subtle.ConstantTimeCompare([]byte(benutzer), []byte(k.Benutzer)) != 1 {
		clear(kek)
		clear(dek)
		return nil, ErrFalsch
	}
	return &Anmeldung{t: t, kopf: k, benutzer: benutzer, kek: kek, dek: dek}, nil
}

// ZweitfaktorAktiv meldet, ob die Anmeldung beim Pruefen des Passworts einen zweiten
// Faktor verlangte.
func (a *Anmeldung) ZweitfaktorAktiv() bool { return len(a.kopf.Zweitfaktor) > 0 }

// Abschliessen prueft den zweiten Faktor (falls eingeschaltet) und entsperrt den Tresor.
// Ist er schon offen (weitere Anmeldung), bleibt der vorhandene DEK. Scheitert der
// zweite Faktor, bleibt der Tresor, wie er war: gesperrt oder offen fuer andere Sitzungen.
func (a *Anmeldung) Abschliessen(pruefen Pruefer, speichern Speicher) (Abschluss, error) {
	var ab Abschluss
	if a.dek == nil {
		return ab, ErrFalsch // schon abgeschlossen oder verworfen
	}
	t := a.t
	t.mu.Lock()
	defer t.mu.Unlock()
	k := t.kopf
	if k == nil {
		return ab, ErrNichtAngelegt
	}
	if k != a.kopf {
		// Der Kopf wurde seit der Passwortpruefung ersetzt. Bei gleicher Ableitung (Ersatzcode
		// verbraucht, Zwei-Faktor geaendert) gilt das Passwort weiter; sonst (neues Passwort,
		// Backup eingespielt) nicht mehr.
		if a.kek == nil || !gleicheAbleitung(*k, *a.kopf) {
			return ab, ErrFalsch
		}
		dek, err := oeffnen(a.kek, k.DEKPasswort)
		if err != nil || subtle.ConstantTimeCompare([]byte(a.benutzer), []byte(k.Benutzer)) != 1 {
			clear(dek)
			return ab, ErrFalsch
		}
		clear(a.dek)
		a.dek, a.kopf = dek, k
	}
	if len(k.Zweitfaktor) > 0 {
		if pruefen == nil {
			return ab, ErrZweitfaktorFehlt
		}
		z, err := zweitfaktorOeffnen(a.dek, k.Zweitfaktor)
		if err != nil {
			return ab, err
		}
		defer z.leeren()
		geaendert, err := pruefen(&z)
		if err != nil {
			return ab, err
		}
		ab = Abschluss{Zweitfaktor: true, Geaendert: geaendert, ErsatzUebrig: len(z.Ersatz)}
		if geaendert {
			nk := *k
			if nk.Zweitfaktor, err = zweitfaktorVersiegeln(a.dek, z); err != nil {
				return Abschluss{}, err
			}
			if err := kopfSpeichern(speichern, nk); err != nil {
				return Abschluss{}, err
			}
			t.kopf = &nk
			a.kopf = &nk
		}
	}
	if t.dek != nil {
		// Schon offen (weitere Anmeldung): den vorhandenen DEK behalten.
		return ab, nil
	}
	dek := a.dek
	a.dek = nil // gehoert jetzt dem Tresor (dekSetzen leert ihn im Fehlerfall selbst)
	if err := t.dekSetzen(dek); err != nil {
		return Abschluss{}, err
	}
	return ab, nil
}

// Verwerfen leert die Schluessel einer Anmeldung, die nicht (mehr) gebraucht wird.
func (a *Anmeldung) Verwerfen() {
	if a == nil {
		return
	}
	clear(a.kek)
	clear(a.dek)
	a.kek, a.dek = nil, nil
}

// gleicheAbleitung: beide Koepfe leiten den Passwort-Schluessel gleich ab.
func gleicheAbleitung(a, b Kopf) bool {
	return bytes.Equal(a.Salz, b.Salz) && a.Zeit == b.Zeit && a.SpeicherKB == b.SpeicherKB && a.Threads == b.Threads
}

func kopfSpeichern(speichern Speicher, k Kopf) error {
	if speichern == nil {
		return nil
	}
	b, err := json.Marshal(k)
	if err != nil {
		return err
	}
	return speichern(b)
}

// --- Zwei-Faktor-Anmeldung ---

// ZweitfaktorAktiv meldet, ob die Zwei-Faktor-Anmeldung eingeschaltet ist.
func (t *Tresor) ZweitfaktorAktiv() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.kopf != nil && len(t.kopf.Zweitfaktor) > 0
}

// ErsatzcodesUebrig liefert die Zahl der unverbrauchten Ersatzcodes; -1, wenn die
// Zwei-Faktor-Anmeldung aus oder der Tresor gesperrt ist.
func (t *Tresor) ErsatzcodesUebrig() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.kopf == nil || len(t.kopf.Zweitfaktor) == 0 || t.aead == nil {
		return -1
	}
	klar, err := oeffnenMit(t.aead, t.kopf.Zweitfaktor)
	if err != nil {
		return -1
	}
	defer clear(klar)
	var z Zweitfaktor
	if err := json.Unmarshal(klar, &z); err != nil {
		return -1
	}
	z.leeren()
	return len(z.Ersatz)
}

// ZweitfaktorEinschalten prueft das Passwort, laesst pruefen den ersten Code gegen das
// neue Geheimnis pruefen und versiegelt z (Geheimnis, Ersatzcodes) im Kopf.
func (t *Tresor) ZweitfaktorEinschalten(passwort string, z Zweitfaktor, pruefen Pruefer, speichern Speicher) error {
	return t.kopfAendern(passwort, speichern, func(k *Kopf, dek []byte) error {
		if len(k.Zweitfaktor) > 0 {
			return ErrZweitfaktorAktiv
		}
		if z.Seit.IsZero() {
			z.Seit = time.Now().UTC()
		}
		if pruefen != nil {
			if _, err := pruefen(&z); err != nil {
				return err
			}
		}
		var err error
		k.Zweitfaktor, err = zweitfaktorVersiegeln(dek, z)
		return err
	})
}

// ZweitfaktorAusschalten prueft Passwort und Code (pruefen) und entfernt den Zweitfaktor.
func (t *Tresor) ZweitfaktorAusschalten(passwort string, pruefen Pruefer, speichern Speicher) error {
	return t.kopfAendern(passwort, speichern, func(k *Kopf, dek []byte) error {
		if err := zweitfaktorPruefen(k, dek, pruefen); err != nil {
			return err
		}
		k.Zweitfaktor = nil
		return nil
	})
}

// ErsatzcodesErneuern prueft Passwort und Code (pruefen) und ersetzt alle Ersatzcodes.
func (t *Tresor) ErsatzcodesErneuern(passwort string, ersatz []string, pruefen Pruefer, speichern Speicher) error {
	return t.kopfAendern(passwort, speichern, func(k *Kopf, dek []byte) error {
		if len(k.Zweitfaktor) == 0 {
			return ErrZweitfaktorAus
		}
		z, err := zweitfaktorOeffnen(dek, k.Zweitfaktor)
		if err != nil {
			return err
		}
		defer z.leeren()
		if pruefen == nil {
			return ErrZweitfaktorFehlt
		}
		if _, err := pruefen(&z); err != nil {
			return err
		}
		z.Ersatz = append([]string(nil), ersatz...)
		k.Zweitfaktor, err = zweitfaktorVersiegeln(dek, z)
		return err
	})
}

// zweitfaktorPruefen oeffnet den Zweitfaktor eines Kopfs und laesst pruefen entscheiden.
func zweitfaktorPruefen(k *Kopf, dek []byte, pruefen Pruefer) error {
	if len(k.Zweitfaktor) == 0 {
		return ErrZweitfaktorAus
	}
	z, err := zweitfaktorOeffnen(dek, k.Zweitfaktor)
	if err != nil {
		return err
	}
	defer z.leeren()
	if pruefen == nil {
		return ErrZweitfaktorFehlt
	}
	_, err = pruefen(&z)
	return err
}

// kopfAendern prueft das Passwort (Argon2id, ausserhalb der Sperre) und wendet aendern
// auf eine Kopie des Kopfs an. Erst wenn speichern gelingt, gilt der neue Kopf.
func (t *Tresor) kopfAendern(passwort string, speichern Speicher, aendern func(k *Kopf, dek []byte) error) error {
	t.mu.RLock()
	k := t.kopf
	t.mu.RUnlock()
	if k == nil {
		return ErrNichtAngelegt
	}
	kek := k.passwortKEK(passwort)
	dek, err := oeffnen(kek, k.DEKPasswort)
	clear(kek)
	if err != nil {
		return ErrPasswortFalsch
	}
	defer clear(dek)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.kopf != k {
		return ErrKopfGeaendert
	}
	nk := *k
	if err := aendern(&nk, dek); err != nil {
		return err
	}
	if err := kopfSpeichern(speichern, nk); err != nil {
		return err
	}
	t.kopf = &nk
	return nil
}

func zweitfaktorOeffnen(dek, geheim []byte) (Zweitfaktor, error) {
	var z Zweitfaktor
	klar, err := oeffnen(dek, geheim)
	if err != nil {
		return z, ErrZweitfaktorKaputt
	}
	defer clear(klar)
	if err := json.Unmarshal(klar, &z); err != nil || len(z.Geheimnis) == 0 {
		z.leeren()
		return Zweitfaktor{}, ErrZweitfaktorKaputt
	}
	return z, nil
}

func zweitfaktorVersiegeln(dek []byte, z Zweitfaktor) ([]byte, error) {
	klar, err := json.Marshal(z)
	if err != nil {
		return nil, err
	}
	defer clear(klar)
	return versiegeln(dek, klar)
}

// PasswortAendern verpackt den DEK neu. Geheimnisse, Sicherheitsschluessel und eine
// eingeschaltete Zwei-Faktor-Anmeldung (mit dem DEK versiegelt) bleiben.
func (t *Tresor) PasswortAendern(alt, neu string) ([]byte, error) {
	if err := pruefePasswort(neu); err != nil {
		return nil, err
	}
	t.mu.RLock()
	k := t.kopf
	t.mu.RUnlock()
	if k == nil {
		return nil, ErrNichtAngelegt
	}
	kek := k.passwortKEK(alt)
	dek, err := oeffnen(kek, k.DEKPasswort)
	clear(kek)
	if err != nil {
		return nil, ErrAltFalsch
	}
	return t.neuVerpacken(*k, dek, k.Benutzer, neu)
}

// Zuruecksetzen setzt Benutzer und Passwort mit dem Sicherheitsschluessel neu. Eine
// eingeschaltete Zwei-Faktor-Anmeldung wird dabei entfernt (zweitfaktorAus): Der
// Sicherheitsschluessel ist der Generalschluessel, auch wenn Telefon und Passwort fehlen.
func (t *Tresor) Zuruecksetzen(sicherheitsschluessel, benutzer, neu string) (kopf []byte, zweitfaktorAus bool, err error) {
	if err := pruefeBenutzer(benutzer); err != nil {
		return nil, false, err
	}
	if err := pruefePasswort(neu); err != nil {
		return nil, false, err
	}
	roh, err := SchluesselLesen(sicherheitsschluessel)
	if err != nil {
		return nil, false, err
	}
	defer clear(roh)
	t.mu.RLock()
	k := t.kopf
	t.mu.RUnlock()
	if k == nil {
		return nil, false, ErrNichtAngelegt
	}
	nk := notfallKEK(roh)
	dek, err := oeffnen(nk, k.DEKNotfall)
	clear(nk)
	if err != nil {
		return nil, false, ErrSchluesselFalsch
	}
	neuKopf := *k
	zweitfaktorAus = len(neuKopf.Zweitfaktor) > 0
	neuKopf.Zweitfaktor = nil
	kopf, err = t.neuVerpacken(neuKopf, dek, benutzer, neu)
	if err != nil {
		return nil, false, err
	}
	return kopf, zweitfaktorAus, nil
}

func (t *Tresor) neuVerpacken(k Kopf, dek []byte, benutzer, passwort string) ([]byte, error) {
	k.Benutzer = benutzer
	k.argonParameter()
	kek := k.passwortKEK(passwort)
	var err error
	k.DEKPasswort, err = versiegeln(kek, dek)
	clear(kek)
	if err != nil {
		clear(dek)
		return nil, err
	}
	t.mu.Lock()
	err = t.dekSetzen(dek)
	if err == nil {
		t.kopf = &k
	}
	t.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return json.Marshal(k)
}

// Sperren vergisst den Datenschluessel.
func (t *Tresor) Sperren() {
	t.mu.Lock()
	defer t.mu.Unlock()
	clear(t.dek)
	t.dek, t.aead = nil, nil
}

func (t *Tresor) Verschluesseln(klar []byte) ([]byte, error) {
	if len(klar) == 0 {
		return nil, nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.aead == nil {
		return nil, ErrGesperrt
	}
	return versiegelnMit(t.aead, klar), nil
}

func (t *Tresor) Entschluesseln(geheim []byte) ([]byte, error) {
	if len(geheim) == 0 {
		return nil, nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.aead == nil {
		return nil, ErrGesperrt
	}
	return oeffnenMit(t.aead, geheim)
}

// BackupVersiegeln verschluesselt eine Sicherung mit dem Backup-Schluessel.
func (t *Tresor) BackupVersiegeln(klar []byte) ([]byte, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.dek == nil {
		return nil, ErrGesperrt
	}
	bk, err := oeffnenMit(t.aead, t.kopf.BackupVerpackt)
	if err != nil {
		return nil, err
	}
	defer clear(bk)
	return versiegeln(bk, klar)
}

// BackupOeffnen entschluesselt eine Sicherung mit dem Sicherheitsschluessel.
func BackupOeffnen(sicherheitsschluessel string, geheim []byte) ([]byte, error) {
	roh, err := SchluesselLesen(sicherheitsschluessel)
	if err != nil {
		return nil, err
	}
	bs := backupSchluessel(roh)
	clear(roh)
	klar, err := oeffnen(bs, geheim)
	clear(bs)
	if err != nil {
		return nil, ErrSchluesselFalsch
	}
	return klar, nil
}

// KopfPruefen prueft einen Tresor-Kopf (etwa aus einer Sicherung) samt
// Sicherheitsschluessel, ohne etwas zu veraendern.
func KopfPruefen(kopfJSON []byte, sicherheitsschluessel string) error {
	k, err := KopfLesen(kopfJSON)
	if err != nil {
		return err
	}
	dek, err := notfallOeffnen(k, sicherheitsschluessel)
	if err != nil {
		return err
	}
	defer clear(dek)
	return zweitfaktorLesbar(k, dek)
}

// zweitfaktorLesbar: ein vorhandener Zweitfaktor laesst sich mit dem DEK oeffnen (sonst
// waere nach dem Einspielen keine Anmeldung mehr moeglich).
func zweitfaktorLesbar(k Kopf, dek []byte) error {
	if len(k.Zweitfaktor) == 0 {
		return nil
	}
	z, err := zweitfaktorOeffnen(dek, k.Zweitfaktor)
	z.leeren()
	return err
}

// notfallOeffnen entpackt den DEK eines Kopfs mit dem Sicherheitsschluessel.
func notfallOeffnen(k Kopf, sicherheitsschluessel string) ([]byte, error) {
	roh, err := SchluesselLesen(sicherheitsschluessel)
	if err != nil {
		return nil, err
	}
	nk := notfallKEK(roh)
	clear(roh)
	dek, err := oeffnen(nk, k.DEKNotfall)
	clear(nk)
	if err != nil {
		return nil, ErrSchluesselFalsch
	}
	return dek, nil
}

// Uebernehmen ersetzt den Kopf (nach dem Einspielen eines Backups) und
// entsperrt mit dem Sicherheitsschluessel, damit sofort weitergearbeitet werden kann.
// Eine Zwei-Faktor-Anmeldung im Backup gilt danach so, wie sie im Backup steht.
func (t *Tresor) Uebernehmen(kopfJSON []byte, sicherheitsschluessel string) error {
	k, err := KopfLesen(kopfJSON)
	if err != nil {
		return err
	}
	dek, err := notfallOeffnen(k, sicherheitsschluessel)
	if err != nil {
		return err
	}
	if err := zweitfaktorLesbar(k, dek); err != nil {
		clear(dek)
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.dekSetzen(dek); err != nil {
		return err
	}
	t.kopf = &k
	return nil
}

// --- Sicherheitsschluessel: 32 Byte, base32 in Vierergruppen ---

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func SchluesselFormatieren(roh []byte) string {
	s := b32.EncodeToString(roh)
	var gruppen []string
	for len(s) > 4 {
		gruppen = append(gruppen, s[:4])
		s = s[4:]
	}
	return strings.Join(append(gruppen, s), "-")
}

func SchluesselLesen(s string) ([]byte, error) {
	s = strings.ToUpper(strings.Map(func(r rune) rune {
		if r == '-' || unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s))
	roh, err := b32.DecodeString(s)
	if err != nil || len(roh) != 32 {
		return nil, ErrSchluesselFormat
	}
	return roh, nil
}

// --- AES-GCM ---

func versiegeln(schluessel, klar []byte) ([]byte, error) {
	gcm, err := gcmFuer(schluessel)
	if err != nil {
		return nil, err
	}
	return versiegelnMit(gcm, klar), nil
}

func versiegelnMit(gcm cipher.AEAD, klar []byte) []byte {
	nonce := zufall(gcm.NonceSize())
	return gcm.Seal(nonce, nonce, klar, nil)
}

func oeffnen(schluessel, geheim []byte) ([]byte, error) {
	gcm, err := gcmFuer(schluessel)
	if err != nil {
		return nil, err
	}
	return oeffnenMit(gcm, geheim)
}

func oeffnenMit(gcm cipher.AEAD, geheim []byte) ([]byte, error) {
	n := gcm.NonceSize()
	if len(geheim) < n {
		return nil, texte.Neu("vault.geheimnis.kaputt")
	}
	return gcm.Open(nil, geheim[:n], geheim[n:], nil)
}

func gcmFuer(schluessel []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(schluessel)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func zufall(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("Zufallsquelle: " + err.Error())
	}
	return b
}
