package vault

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func eingerichtet(t *testing.T) (*Tresor, []byte, string) {
	t.Helper()
	tr, _ := Neu(nil)
	kopf, sk, err := tr.Einrichten("admin", "richtig-langes-pw")
	if err != nil {
		t.Fatal(err)
	}
	return tr, kopf, sk
}

func TestEinrichtenAnmeldenSperren(t *testing.T) {
	tr, _ := Neu(nil)
	if _, _, err := tr.Einrichten("admin", "kurz"); !errors.Is(err, ErrZuKurz) {
		t.Fatalf("kurzes Passwort: %v", err)
	}
	if _, _, err := tr.Einrichten("mit leer", "richtig-langes-pw"); !errors.Is(err, ErrBenutzer) {
		t.Fatalf("Benutzer mit Leerzeichen: %v", err)
	}
	tr, kopf, sk := eingerichtet(t)
	if strings.Count(sk, "-") < 10 {
		t.Fatalf("Schluesselformat: %s", sk)
	}
	if bytes.Contains(kopf, []byte(strings.ReplaceAll(sk, "-", ""))) {
		t.Fatal("Sicherheitsschluessel steht im Kopf")
	}
	geheim, err := tr.Verschluesseln([]byte("ssh-geheimnis"))
	if err != nil {
		t.Fatal(err)
	}

	tr2, err := Neu(kopf)
	if err != nil {
		t.Fatal(err)
	}
	if tr2.Offen() {
		t.Fatal("nach Neustart muss der Tresor gesperrt sein")
	}
	if _, err := tr2.Entschluesseln(geheim); !errors.Is(err, ErrGesperrt) {
		t.Fatalf("gesperrt entschluesseln: %v", err)
	}
	if err := tr2.Anmelden("admin", "falsches-passwort"); !errors.Is(err, ErrFalsch) {
		t.Fatalf("falsches Passwort: %v", err)
	}
	if err := tr2.Anmelden("anderer", "richtig-langes-pw"); !errors.Is(err, ErrFalsch) {
		t.Fatalf("falscher Benutzer: %v", err)
	}
	if err := tr2.Anmelden("admin", "richtig-langes-pw"); err != nil {
		t.Fatal(err)
	}
	if klar, err := tr2.Entschluesseln(geheim); err != nil || string(klar) != "ssh-geheimnis" {
		t.Fatalf("entschluesseln: %q %v", klar, err)
	}
	tr2.Sperren()
	if tr2.Offen() {
		t.Fatal("Sperren wirkt nicht")
	}
}

func TestZuruecksetzenMitSicherheitsschluessel(t *testing.T) {
	tr, kopf, sk := eingerichtet(t)
	geheim, _ := tr.Verschluesseln([]byte("x"))
	tr2, _ := Neu(kopf)
	if _, err := tr2.Zuruecksetzen(SchluesselFormatieren(make([]byte, 32)), "admin", "neues-passwort-1"); !errors.Is(err, ErrSchluesselFalsch) {
		t.Fatalf("falscher Schluessel: %v", err)
	}
	// Kleinschreibung und Leerzeichen beim Abtippen sind erlaubt.
	locker := strings.ToLower(strings.ReplaceAll(sk, "-", " "))
	neuKopf, err := tr2.Zuruecksetzen(locker, "chef", "neues-passwort-1")
	if err != nil {
		t.Fatal(err)
	}
	tr3, _ := Neu(neuKopf)
	if err := tr3.Anmelden("admin", "richtig-langes-pw"); err == nil {
		t.Fatal("alte Anmeldung geht noch")
	}
	if err := tr3.Anmelden("chef", "neues-passwort-1"); err != nil {
		t.Fatal(err)
	}
	if klar, err := tr3.Entschluesseln(geheim); err != nil || string(klar) != "x" {
		t.Fatalf("Geheimnis nach Zuruecksetzen: %q %v", klar, err)
	}
	// Der Sicherheitsschluessel bleibt gueltig.
	if _, err := tr3.Zuruecksetzen(sk, "chef", "drittes-passwort"); err != nil {
		t.Fatal(err)
	}
}

func TestPasswortAendern(t *testing.T) {
	tr, _, _ := eingerichtet(t)
	if _, err := tr.PasswortAendern("falsch-falsch", "zweites-passwort"); err == nil {
		t.Fatal("falsches altes Passwort angenommen")
	}
	kopf, err := tr.PasswortAendern("richtig-langes-pw", "zweites-passwort")
	if err != nil {
		t.Fatal(err)
	}
	tr2, _ := Neu(kopf)
	if err := tr2.Anmelden("admin", "zweites-passwort"); err != nil {
		t.Fatal(err)
	}
}

func TestBackupNurMitSicherheitsschluessel(t *testing.T) {
	tr, kopf, sk := eingerichtet(t)
	geheimHost, _ := tr.Verschluesseln([]byte("host-passwort"))
	paket, err := tr.BackupVersiegeln([]byte(`{"hosts":["web"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(paket, []byte("web")) {
		t.Fatal("Backup ist nicht verschluesselt")
	}
	if _, err := BackupOeffnen(SchluesselFormatieren(make([]byte, 32)), paket); !errors.Is(err, ErrSchluesselFalsch) {
		t.Fatalf("falscher Schluessel: %v", err)
	}
	klar, err := BackupOeffnen(sk, paket)
	if err != nil || string(klar) != `{"hosts":["web"]}` {
		t.Fatalf("%s %v", klar, err)
	}
	// Neues System: Kopf aus dem Backup uebernehmen, Geheimnisse lesbar, alte Anmeldung gilt.
	neu, _ := Neu(nil)
	if err := neu.Uebernehmen(kopf, sk); err != nil {
		t.Fatal(err)
	}
	if klar, err := neu.Entschluesseln(geheimHost); err != nil || string(klar) != "host-passwort" {
		t.Fatalf("%q %v", klar, err)
	}
	neu.Sperren()
	if err := neu.Anmelden("admin", "richtig-langes-pw"); err != nil {
		t.Fatal(err)
	}
}
