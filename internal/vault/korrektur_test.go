package vault

import (
	"encoding/json"
	"errors"
	"testing"
)

// SICHERHEIT-05: Ein Kopf wird vor dem Einspielen ohne Zustandsaenderung geprueft.
func TestKopfPruefen(t *testing.T) {
	_, kopf, sk := eingerichtet(t)
	if err := KopfPruefen(kopf, sk); err != nil {
		t.Fatalf("gültiger Kopf: %v", err)
	}
	if err := KopfPruefen(kopf, SchluesselFormatieren(make([]byte, 32))); !errors.Is(err, ErrSchluesselFalsch) {
		t.Fatalf("falscher Schlüssel: %v", err)
	}
	var k map[string]any
	json.Unmarshal(kopf, &k)
	k["v"] = 2
	neuer, _ := json.Marshal(k)
	if err := KopfPruefen(neuer, sk); err == nil {
		t.Fatal("Kopf einer neueren Version angenommen")
	}
}

// SICHERHEIT-09: Ein ersetzter Datenschluessel wird geleert, nicht nur losgelassen.
func TestDEKWirdBeimErsetzenGeleert(t *testing.T) {
	tr, kopf, sk := eingerichtet(t)
	alt := tr.dek
	if err := tr.Uebernehmen(kopf, sk); err != nil {
		t.Fatal(err)
	}
	if !nullen(alt) {
		t.Fatal("alter DEK nach Uebernehmen nicht geleert")
	}
	alt = tr.dek
	if _, err := tr.PasswortAendern("richtig-langes-pw", "noch-ein-langes-pw"); err != nil {
		t.Fatal(err)
	}
	if !nullen(alt) {
		t.Fatal("alter DEK nach Passwortwechsel nicht geleert")
	}
	// Zweite Anmeldung: der vorhandene DEK bleibt, die neue Kopie wird verworfen.
	alt = tr.dek
	if err := tr.Anmelden("admin", "noch-ein-langes-pw"); err != nil {
		t.Fatal(err)
	}
	if &tr.dek[0] != &alt[0] || nullen(alt) {
		t.Fatal("zweite Anmeldung hat den DEK ersetzt")
	}
	geheim, err := tr.Verschluesseln([]byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	tr.Sperren()
	if !nullen(alt) || tr.aead != nil {
		t.Fatal("Sperren hat den DEK nicht geleert")
	}
	if _, err := tr.Entschluesseln(geheim); !errors.Is(err, ErrGesperrt) {
		t.Fatalf("gesperrt: %v", err)
	}
}

func nullen(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return len(b) > 0
}
