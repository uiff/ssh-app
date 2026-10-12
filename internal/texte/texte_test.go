package texte

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"regexp"
	"testing"
)

func TestGrundfunktionen(t *testing.T) {
	Eintragen(map[string]string{"test.hallo": "Hallo %s"}, map[string]string{"test.hallo": "Hello %s"})
	if got := T("en-US", "test.hallo", "Ueli"); got != "Hello Ueli" {
		t.Fatal(got)
	}
	if got := T("fr", "test.hallo", "Ueli"); got != "Hallo Ueli" {
		t.Fatal(got)
	}
	m := Neu("test.hallo", "x").MitUrsache(errors.New("roh"))
	if Uebersetzen(EN, fmt.Errorf("aussen: %w", m)) != "Hello x" || m.Error() != "Hallo x" {
		t.Fatal("Meldung")
	}
	r := httptest.NewRequest("GET", "/?sprache=en", nil)
	if AusAnfrage(r) != EN {
		t.Fatal("Parameter")
	}
	r.Header.Set("X-SSH-App-Sprache", "de")
	if AusAnfrage(r) != DE {
		t.Fatal("Kopf hat Vorrang")
	}
}

var verben = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z%]`)

// Jeder Schluessel braucht beide Sprachen mit denselben Platzhaltern.
func TestKatalogVollstaendig(t *testing.T) {
	de, en := Schluessel(DE), Schluessel(EN)
	for k, v := range de {
		e, ok := en[k]
		if !ok || e == "" {
			t.Errorf("englischer Text fehlt: %s", k)
			continue
		}
		if fmt.Sprint(verben.FindAllString(v, -1)) != fmt.Sprint(verben.FindAllString(e, -1)) {
			t.Errorf("Platzhalter verschieden in %s: %q / %q", k, v, e)
		}
	}
	for k := range en {
		if _, ok := de[k]; !ok {
			t.Errorf("deutscher Text fehlt: %s", k)
		}
	}
}
