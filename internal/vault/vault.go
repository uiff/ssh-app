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
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/crypto/argon2"
)

// MinPasswortLaenge ist die Mindestlaenge des Anmeldepassworts.
const MinPasswortLaenge = 10

var (
	ErrGesperrt         = errors.New("Tresor ist gesperrt")
	ErrFalsch           = errors.New("Benutzername oder Passwort ist falsch")
	ErrSchluesselFalsch = errors.New("Der Sicherheitsschlüssel passt nicht")
	ErrSchonAngelegt    = errors.New("Die App ist schon eingerichtet")
	ErrNichtAngelegt    = errors.New("Die App ist noch nicht eingerichtet")
	ErrZuKurz           = fmt.Errorf("Das Passwort braucht mindestens %d Zeichen", MinPasswortLaenge)
	ErrBenutzer         = errors.New("Benutzername: 1 bis 40 Zeichen, keine Leerzeichen")
	ErrAltFalsch        = errors.New("Das bisherige Passwort ist falsch")
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
		return k, fmt.Errorf("Tresor-Kopf lesen: %w", err)
	}
	if k.Version != 1 || len(k.Salz) < 16 || len(k.DEKPasswort) == 0 || len(k.DEKNotfall) == 0 || k.Benutzer == "" {
		return k, errors.New("Tresor-Kopf ist unvollständig")
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

// Anmelden prueft Benutzer und Passwort und entsperrt den Tresor.
func (t *Tresor) Anmelden(benutzer, passwort string) error {
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
		return ErrFalsch
	}
	if subtle.ConstantTimeCompare([]byte(benutzer), []byte(k.Benutzer)) != 1 {
		clear(dek)
		return ErrFalsch
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dek != nil && t.kopf == k {
		// Schon offen (weitere Anmeldung): den vorhandenen DEK behalten.
		clear(dek)
		return nil
	}
	return t.dekSetzen(dek)
}

// PasswortAendern verpackt den DEK neu. Geheimnisse und Sicherheitsschluessel bleiben.
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

// Zuruecksetzen setzt Benutzer und Passwort mit dem Sicherheitsschluessel neu.
func (t *Tresor) Zuruecksetzen(sicherheitsschluessel, benutzer, neu string) ([]byte, error) {
	if err := pruefeBenutzer(benutzer); err != nil {
		return nil, err
	}
	if err := pruefePasswort(neu); err != nil {
		return nil, err
	}
	roh, err := SchluesselLesen(sicherheitsschluessel)
	if err != nil {
		return nil, err
	}
	defer clear(roh)
	t.mu.RLock()
	k := t.kopf
	t.mu.RUnlock()
	if k == nil {
		return nil, ErrNichtAngelegt
	}
	nk := notfallKEK(roh)
	dek, err := oeffnen(nk, k.DEKNotfall)
	clear(nk)
	if err != nil {
		return nil, ErrSchluesselFalsch
	}
	return t.neuVerpacken(*k, dek, benutzer, neu)
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
	clear(dek)
	return nil
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
func (t *Tresor) Uebernehmen(kopfJSON []byte, sicherheitsschluessel string) error {
	k, err := KopfLesen(kopfJSON)
	if err != nil {
		return err
	}
	dek, err := notfallOeffnen(k, sicherheitsschluessel)
	if err != nil {
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
		return nil, errors.New("Das ist kein gültiger Sicherheitsschlüssel")
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
		return nil, errors.New("Geheimnis zu kurz")
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
