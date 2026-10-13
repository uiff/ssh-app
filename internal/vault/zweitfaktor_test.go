package vault

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"testing"
)

const testPW = "richtig-langes-pw"

var (
	testGeheimnis = []byte("12345678901234567890")
	testErsatz    = []string{"AAAAAAAA", "BBBBBBBB", "CCCCCCCC"}
	errTestCode   = errors.New("Code falsch")
)

// annehmen prueft das Geheimnis und verbraucht, wenn ersatz gesetzt ist, diesen Ersatzcode.
func annehmen(t *testing.T, ersatz string) Pruefer {
	return func(z *Zweitfaktor) (bool, error) {
		if !bytes.Equal(z.Geheimnis, testGeheimnis) {
			t.Errorf("falsches Geheimnis im Pruefer: %q", z.Geheimnis)
			return false, errTestCode
		}
		if ersatz == "" {
			return false, nil
		}
		i := slices.Index(z.Ersatz, ersatz)
		if i < 0 {
			return false, errTestCode
		}
		z.Ersatz = slices.Delete(z.Ersatz, i, i+1)
		return true, nil
	}
}

func ablehnen(*Zweitfaktor) (bool, error) { return false, errTestCode }

// ablage merkt sich den zuletzt gespeicherten Kopf (oder scheitert absichtlich).
type ablage struct {
	kopf      []byte
	n         int
	scheitern bool
}

func (a *ablage) speichern(k []byte) error {
	if a.scheitern {
		return errors.New("Platte voll")
	}
	a.kopf, a.n = k, a.n+1
	return nil
}

// mitZweitfaktor: eingerichteter, offener Tresor mit eingeschalteter Zwei-Faktor-Anmeldung.
func mitZweitfaktor(t *testing.T) (*Tresor, []byte, string) {
	t.Helper()
	tr, _, sk := eingerichtet(t)
	var ab ablage
	z := Zweitfaktor{Geheimnis: slices.Clone(testGeheimnis), Ersatz: slices.Clone(testErsatz)}
	if err := tr.ZweitfaktorEinschalten(testPW, z, annehmen(t, ""), ab.speichern); err != nil {
		t.Fatal(err)
	}
	return tr, ab.kopf, sk
}

func TestZweitfaktorEinschalten(t *testing.T) {
	tr, vorher, _ := eingerichtet(t)
	if tr.ZweitfaktorAktiv() || tr.ErsatzcodesUebrig() != -1 {
		t.Fatal("frisch eingerichtet schon mit Zweitfaktor")
	}
	if bytes.Contains(vorher, []byte(`"tf"`)) {
		t.Fatalf("Kopf ohne Zweitfaktor enthaelt tf: %s", vorher)
	}
	z := func() Zweitfaktor {
		return Zweitfaktor{Geheimnis: slices.Clone(testGeheimnis), Ersatz: slices.Clone(testErsatz)}
	}
	var ab ablage
	// Falsches Passwort, falscher Code, Speichern scheitert: nichts aendert sich.
	if err := tr.ZweitfaktorEinschalten("falsches-passwort", z(), annehmen(t, ""), ab.speichern); !errors.Is(err, ErrPasswortFalsch) {
		t.Fatalf("falsches Passwort: %v", err)
	}
	if err := tr.ZweitfaktorEinschalten(testPW, z(), ablehnen, ab.speichern); !errors.Is(err, errTestCode) {
		t.Fatalf("falscher Code: %v", err)
	}
	if err := tr.ZweitfaktorEinschalten(testPW, z(), annehmen(t, ""), (&ablage{scheitern: true}).speichern); err == nil {
		t.Fatal("Speicherfehler verschluckt")
	}
	if tr.ZweitfaktorAktiv() || ab.n != 0 || !bytes.Equal(tr.KopfJSON(), vorher) {
		t.Fatal("gescheitertes Einschalten hat den Kopf veraendert")
	}

	if err := tr.ZweitfaktorEinschalten(testPW, z(), annehmen(t, ""), ab.speichern); err != nil {
		t.Fatal(err)
	}
	if !tr.ZweitfaktorAktiv() || tr.ErsatzcodesUebrig() != 3 || ab.n != 1 || !bytes.Equal(ab.kopf, tr.KopfJSON()) {
		t.Fatalf("eingeschaltet: aktiv=%v uebrig=%d gespeichert=%d", tr.ZweitfaktorAktiv(), tr.ErsatzcodesUebrig(), ab.n)
	}
	// Version bleibt 1, Geheimnis und Codes stehen nur versiegelt im Kopf.
	var roh map[string]any
	json.Unmarshal(ab.kopf, &roh)
	if roh["v"] != float64(1) || roh["tf"] == nil {
		t.Fatalf("Kopf: %s", ab.kopf)
	}
	for _, klar := range [][]byte{testGeheimnis, []byte(base64.StdEncoding.EncodeToString(testGeheimnis)), []byte("AAAAAAAA")} {
		if bytes.Contains(ab.kopf, klar) {
			t.Fatalf("Klartext %q im Kopf", klar)
		}
	}
	if _, err := KopfLesen(ab.kopf); err != nil {
		t.Fatal(err)
	}
	if err := tr.ZweitfaktorEinschalten(testPW, z(), annehmen(t, ""), ab.speichern); !errors.Is(err, ErrZweitfaktorAktiv) {
		t.Fatalf("zweimal einschalten: %v", err)
	}
}

// Die Sicherheitsregel: Der Tresor wird erst entsperrt, wenn Passwort UND zweiter Faktor stimmen.
func TestZweitfaktorAnmeldungEntsperrtErstMitCode(t *testing.T) {
	_, kopf, _ := mitZweitfaktor(t)
	tr, _ := Neu(kopf)
	if !tr.ZweitfaktorAktiv() || tr.Offen() {
		t.Fatal("nach Neustart")
	}
	if err := tr.Anmelden("admin", testPW); !errors.Is(err, ErrZweitfaktorFehlt) || tr.Offen() {
		t.Fatalf("Anmelden ohne Code: %v offen=%v", err, tr.Offen())
	}
	// Falsches Passwort: dieselbe Antwort wie ohne Zweitfaktor.
	if _, err := tr.AnmeldungBeginnen("admin", "falsches-passwort"); !errors.Is(err, ErrFalsch) {
		t.Fatalf("falsches Passwort: %v", err)
	}
	if _, err := tr.AnmeldungBeginnen("chef", testPW); !errors.Is(err, ErrFalsch) {
		t.Fatalf("falscher Benutzer: %v", err)
	}

	a, err := tr.AnmeldungBeginnen("admin", testPW)
	if err != nil || !a.ZweitfaktorAktiv() {
		t.Fatal(err)
	}
	defer a.Verwerfen()
	if tr.Offen() {
		t.Fatal("Passwort allein hat entsperrt")
	}
	if _, err := a.Abschliessen(nil, nil); !errors.Is(err, ErrZweitfaktorFehlt) || tr.Offen() {
		t.Fatalf("ohne Code: %v offen=%v", err, tr.Offen())
	}
	if _, err := a.Abschliessen(ablehnen, nil); !errors.Is(err, errTestCode) || tr.Offen() {
		t.Fatalf("falscher Code: %v offen=%v", err, tr.Offen())
	}
	ab, err := a.Abschliessen(annehmen(t, ""), nil)
	if err != nil || !tr.Offen() || !ab.Zweitfaktor || ab.Geaendert || ab.ErsatzUebrig != 3 {
		t.Fatalf("richtiger Code: %v offen=%v %+v", err, tr.Offen(), ab)
	}
	// Der DEK gehoert jetzt dem Tresor: Verwerfen leert ihn nicht.
	a.Verwerfen()
	geheim, err := tr.Verschluesseln([]byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if klar, err := tr.Entschluesseln(geheim); err != nil || string(klar) != "x" {
		t.Fatalf("nach Verwerfen: %q %v", klar, err)
	}
	if _, err := a.Abschliessen(annehmen(t, ""), nil); !errors.Is(err, ErrFalsch) {
		t.Fatalf("zweimal abgeschlossen: %v", err)
	}
}

// Eine andere Sitzung hat den Tresor offen: ein gescheiterter zweiter Faktor sperrt ihn nicht.
func TestZweitfaktorFalschLaesstOffenenTresorOffen(t *testing.T) {
	tr, _, _ := mitZweitfaktor(t)
	if !tr.Offen() {
		t.Fatal("nach dem Einschalten gesperrt")
	}
	dek := tr.dek
	a, err := tr.AnmeldungBeginnen("admin", testPW)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Abschliessen(ablehnen, nil); !errors.Is(err, errTestCode) {
		t.Fatal(err)
	}
	if _, err := a.Abschliessen(nil, nil); !errors.Is(err, ErrZweitfaktorFehlt) {
		t.Fatal(err)
	}
	neuDEK := a.dek
	a.Verwerfen()
	if !tr.Offen() || &tr.dek[0] != &dek[0] || nullen(dek) {
		t.Fatal("gescheiterter zweiter Faktor hat den offenen Tresor veraendert")
	}
	if !nullen(neuDEK) {
		t.Fatal("verworfene Anmeldung hat ihren DEK nicht geleert")
	}
	// Erfolgreiche zweite Anmeldung: der vorhandene DEK bleibt.
	a, _ = tr.AnmeldungBeginnen("admin", testPW)
	defer a.Verwerfen()
	if _, err := a.Abschliessen(annehmen(t, ""), nil); err != nil || &tr.dek[0] != &dek[0] {
		t.Fatalf("zweite Anmeldung: %v", err)
	}
}

func TestErsatzcodeWirdVerbrauchtUndGespeichert(t *testing.T) {
	_, kopf, _ := mitZweitfaktor(t)
	tr, _ := Neu(kopf)
	a, err := tr.AnmeldungBeginnen("admin", testPW)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Verwerfen()
	// Speichern scheitert: nicht angemeldet, der Code bleibt gueltig.
	if _, err := a.Abschliessen(annehmen(t, "BBBBBBBB"), (&ablage{scheitern: true}).speichern); err == nil || tr.Offen() {
		t.Fatalf("Speicherfehler: %v offen=%v", err, tr.Offen())
	}
	if !bytes.Equal(tr.KopfJSON(), kopf) {
		t.Fatal("Kopf trotz Speicherfehler geaendert")
	}
	var ab ablage
	erg, err := a.Abschliessen(annehmen(t, "BBBBBBBB"), ab.speichern)
	if err != nil || !erg.Geaendert || erg.ErsatzUebrig != 2 || !tr.Offen() {
		t.Fatalf("Ersatzcode: %v %+v", err, erg)
	}
	if ab.n != 1 || !bytes.Equal(ab.kopf, tr.KopfJSON()) || bytes.Equal(ab.kopf, kopf) || tr.ErsatzcodesUebrig() != 2 {
		t.Fatalf("gespeichert %d, uebrig %d", ab.n, tr.ErsatzcodesUebrig())
	}
	// Nach Neustart mit dem gespeicherten Kopf: derselbe Ersatzcode gilt nicht mehr, die anderen schon.
	tr2, _ := Neu(ab.kopf)
	a2, _ := tr2.AnmeldungBeginnen("admin", testPW)
	defer a2.Verwerfen()
	if _, err := a2.Abschliessen(annehmen(t, "BBBBBBBB"), nil); !errors.Is(err, errTestCode) || tr2.Offen() {
		t.Fatalf("verbrauchter Ersatzcode: %v", err)
	}
	if erg, err := a2.Abschliessen(annehmen(t, "CCCCCCCC"), nil); err != nil || erg.ErsatzUebrig != 1 {
		t.Fatalf("weiterer Ersatzcode: %v %+v", err, erg)
	}
}

func TestZweitfaktorBleibtBeimPasswortwechsel(t *testing.T) {
	tr, _, _ := mitZweitfaktor(t)
	kopf, err := tr.PasswortAendern(testPW, "zweites-passwort")
	if err != nil {
		t.Fatal(err)
	}
	if !tr.ZweitfaktorAktiv() || tr.ErsatzcodesUebrig() != 3 {
		t.Fatal("Passwortwechsel hat den Zweitfaktor entfernt")
	}
	tr2, _ := Neu(kopf)
	if err := tr2.Anmelden("admin", "zweites-passwort"); !errors.Is(err, ErrZweitfaktorFehlt) {
		t.Fatalf("nach Passwortwechsel ohne Code: %v", err)
	}
	a, err := tr2.AnmeldungBeginnen("admin", "zweites-passwort")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Verwerfen()
	if _, err := a.Abschliessen(annehmen(t, ""), nil); err != nil {
		t.Fatal(err)
	}
}

func TestZuruecksetzenEntferntZweitfaktor(t *testing.T) {
	tr, kopf, sk := mitZweitfaktor(t)
	geheim, _ := tr.Verschluesseln([]byte("host-passwort"))
	tr2, _ := Neu(kopf)
	if _, _, err := tr2.Zuruecksetzen(SchluesselFormatieren(make([]byte, 32)), "admin", "neues-passwort-1"); !errors.Is(err, ErrSchluesselFalsch) || !tr2.ZweitfaktorAktiv() {
		t.Fatalf("falscher Schluessel: %v", err)
	}
	neuKopf, aus, err := tr2.Zuruecksetzen(sk, "admin", "neues-passwort-1")
	if err != nil || !aus || tr2.ZweitfaktorAktiv() {
		t.Fatalf("zuruecksetzen: %v aus=%v", err, aus)
	}
	if bytes.Contains(neuKopf, []byte(`"tf"`)) {
		t.Fatal("Zweitfaktor steht noch im Kopf")
	}
	tr3, _ := Neu(neuKopf)
	if err := tr3.Anmelden("admin", "neues-passwort-1"); err != nil {
		t.Fatalf("Anmelden ohne Code nach Zuruecksetzen: %v", err)
	}
	if klar, err := tr3.Entschluesseln(geheim); err != nil || string(klar) != "host-passwort" {
		t.Fatalf("Geheimnis: %q %v", klar, err)
	}
	// Ohne Zweitfaktor meldet Zuruecksetzen nichts Entferntes.
	if _, aus, err := tr3.Zuruecksetzen(sk, "admin", "drittes-passwort"); err != nil || aus {
		t.Fatalf("zweites Zuruecksetzen: %v aus=%v", err, aus)
	}
}

func TestZweitfaktorReistImBackup(t *testing.T) {
	tr, kopf, sk := mitZweitfaktor(t)
	if err := KopfPruefen(kopf, sk); err != nil {
		t.Fatal(err)
	}
	neu, _ := Neu(nil)
	if err := neu.Uebernehmen(kopf, sk); err != nil {
		t.Fatal(err)
	}
	if !neu.ZweitfaktorAktiv() || neu.ErsatzcodesUebrig() != 3 {
		t.Fatal("Zweitfaktor nicht mit dem Backup gekommen")
	}
	neu.Sperren()
	if err := neu.Anmelden("admin", testPW); !errors.Is(err, ErrZweitfaktorFehlt) || neu.Offen() {
		t.Fatalf("nach Einspielen ohne Code: %v", err)
	}

	// Ein Backup ohne Zweitfaktor schaltet ihn beim Einspielen aus (es gilt der Stand des Backups).
	var k Kopf
	json.Unmarshal(kopf, &k)
	k.Zweitfaktor = nil
	ohne, _ := json.Marshal(k)
	if err := neu.Uebernehmen(ohne, sk); err != nil || neu.ZweitfaktorAktiv() {
		t.Fatalf("Backup ohne Zweitfaktor: %v", err)
	}

	// Ein beschaedigter Zweitfaktor wird vor dem Ersetzen abgelehnt.
	json.Unmarshal(kopf, &k)
	k.Zweitfaktor[len(k.Zweitfaktor)-1] ^= 1
	kaputt, _ := json.Marshal(k)
	if err := KopfPruefen(kaputt, sk); !errors.Is(err, ErrZweitfaktorKaputt) {
		t.Fatalf("KopfPruefen kaputt: %v", err)
	}
	vorher := neu.KopfJSON()
	if err := neu.Uebernehmen(kaputt, sk); !errors.Is(err, ErrZweitfaktorKaputt) || !bytes.Equal(neu.KopfJSON(), vorher) {
		t.Fatalf("Uebernehmen kaputt: %v", err)
	}
	_ = tr
}

func TestZweitfaktorAusschaltenUndErsatzcodesErneuern(t *testing.T) {
	tr, _, _ := mitZweitfaktor(t)
	var ab ablage
	neue := []string{"DDDDDDDD", "EEEEEEEE"}
	if err := tr.ErsatzcodesErneuern("falsches-passwort", neue, annehmen(t, ""), ab.speichern); !errors.Is(err, ErrPasswortFalsch) {
		t.Fatalf("Erneuern falsches Passwort: %v", err)
	}
	if err := tr.ErsatzcodesErneuern(testPW, neue, ablehnen, ab.speichern); !errors.Is(err, errTestCode) {
		t.Fatalf("Erneuern falscher Code: %v", err)
	}
	if err := tr.ErsatzcodesErneuern(testPW, neue, nil, ab.speichern); !errors.Is(err, ErrZweitfaktorFehlt) {
		t.Fatalf("Erneuern ohne Code: %v", err)
	}
	if ab.n != 0 || tr.ErsatzcodesUebrig() != 3 {
		t.Fatal("gescheitertes Erneuern hat etwas geaendert")
	}
	// Mit einem Ersatzcode bestaetigt: alle alten gelten danach nicht mehr.
	if err := tr.ErsatzcodesErneuern(testPW, neue, annehmen(t, "AAAAAAAA"), ab.speichern); err != nil {
		t.Fatal(err)
	}
	if ab.n != 1 || tr.ErsatzcodesUebrig() != 2 {
		t.Fatalf("erneuert: %d %d", ab.n, tr.ErsatzcodesUebrig())
	}
	tr2, _ := Neu(ab.kopf)
	a, _ := tr2.AnmeldungBeginnen("admin", testPW)
	defer a.Verwerfen()
	if _, err := a.Abschliessen(annehmen(t, "BBBBBBBB"), nil); !errors.Is(err, errTestCode) {
		t.Fatalf("alter Ersatzcode nach Erneuern: %v", err)
	}
	if _, err := a.Abschliessen(annehmen(t, "DDDDDDDD"), nil); err != nil {
		t.Fatalf("neuer Ersatzcode: %v", err)
	}

	if err := tr.ZweitfaktorAusschalten("falsches-passwort", annehmen(t, ""), ab.speichern); !errors.Is(err, ErrPasswortFalsch) {
		t.Fatalf("Ausschalten falsches Passwort: %v", err)
	}
	if err := tr.ZweitfaktorAusschalten(testPW, ablehnen, ab.speichern); !errors.Is(err, errTestCode) || !tr.ZweitfaktorAktiv() {
		t.Fatalf("Ausschalten falscher Code: %v", err)
	}
	if err := tr.ZweitfaktorAusschalten(testPW, annehmen(t, ""), ab.speichern); err != nil || tr.ZweitfaktorAktiv() {
		t.Fatalf("Ausschalten: %v", err)
	}
	if bytes.Contains(ab.kopf, []byte(`"tf"`)) || tr.ErsatzcodesUebrig() != -1 {
		t.Fatalf("ausgeschalteter Kopf: %s", ab.kopf)
	}
	if err := tr.ZweitfaktorAusschalten(testPW, annehmen(t, ""), ab.speichern); !errors.Is(err, ErrZweitfaktorAus) {
		t.Fatalf("zweimal ausschalten: %v", err)
	}
	if err := tr.ErsatzcodesErneuern(testPW, neue, annehmen(t, ""), ab.speichern); !errors.Is(err, ErrZweitfaktorAus) {
		t.Fatalf("Erneuern ohne Zweitfaktor: %v", err)
	}
	tr3, _ := Neu(ab.kopf)
	if err := tr3.Anmelden("admin", testPW); err != nil {
		t.Fatalf("Anmelden nach Ausschalten: %v", err)
	}
}

// Zwischen Passwortpruefung und Abschluss aendert sich der Kopf: bei gleicher Ableitung
// (Zwei-Faktor-Einstellung) gilt die Anmeldung weiter, nach einem Passwortwechsel nicht.
func TestAnmeldungNachKopfwechsel(t *testing.T) {
	_, kopf, _ := mitZweitfaktor(t)
	tr, _ := Neu(kopf)
	a, err := tr.AnmeldungBeginnen("admin", testPW)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Verwerfen()
	if err := tr.ErsatzcodesErneuern(testPW, []string{"FFFFFFFF"}, annehmen(t, ""), nil); err != nil {
		t.Fatal(err)
	}
	erg, err := a.Abschliessen(annehmen(t, "FFFFFFFF"), nil)
	if err != nil || erg.ErsatzUebrig != 0 || !tr.Offen() {
		t.Fatalf("nach Erneuern: %v %+v", err, erg)
	}

	tr2, _ := Neu(kopf)
	a2, err := tr2.AnmeldungBeginnen("admin", testPW)
	if err != nil {
		t.Fatal(err)
	}
	defer a2.Verwerfen()
	if _, err := tr2.PasswortAendern(testPW, "ganz-anderes-pw"); err != nil {
		t.Fatal(err)
	}
	if _, err := a2.Abschliessen(annehmen(t, ""), nil); !errors.Is(err, ErrFalsch) {
		t.Fatalf("nach Passwortwechsel: %v", err)
	}
}

func TestAnmeldungVerwerfenLeert(t *testing.T) {
	_, kopf, _ := mitZweitfaktor(t)
	tr, _ := Neu(kopf)
	a, err := tr.AnmeldungBeginnen("admin", testPW)
	if err != nil {
		t.Fatal(err)
	}
	kek, dek := a.kek, a.dek
	a.Verwerfen()
	a.Verwerfen() // zweimal schadet nicht
	if !nullen(kek) || !nullen(dek) {
		t.Fatal("Verwerfen hat die Schluessel nicht geleert")
	}
	if _, err := a.Abschliessen(annehmen(t, ""), nil); !errors.Is(err, ErrFalsch) || tr.Offen() {
		t.Fatalf("verworfene Anmeldung abgeschlossen: %v", err)
	}
	var nichts *Anmeldung
	nichts.Verwerfen()
}
