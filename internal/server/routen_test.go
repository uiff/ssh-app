package server

import (
	"os"
	"regexp"
	"testing"
)

// Routen sind Bezeichner, keine Anzeigetexte: nur ASCII (Umlaut-Umstellungen
// duerfen sie nicht erfassen, sonst antwortet der Server mit 405/404).
func TestRoutenNurASCII(t *testing.T) {
	quelle, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	routen := regexp.MustCompile(`"(?:GET|POST|PUT|DELETE) (/[^"]*)"`).FindAllSubmatch(quelle, -1)
	if len(routen) < 30 {
		t.Fatalf("nur %d Routen gefunden — Muster veraltet?", len(routen))
	}
	for _, r := range routen {
		for _, b := range r[1] {
			if b > 127 {
				t.Errorf("Route mit Nicht-ASCII-Zeichen: %s", r[1])
				break
			}
		}
	}
}
