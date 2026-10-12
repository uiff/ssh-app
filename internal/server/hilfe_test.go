package server

import (
	"archive/zip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uiff/ssh-app/internal/help"
)

// hilfeTestServer: nur der Index, die Hilfe-Handler brauchen nichts anderes.
func hilfeTestServer(t *testing.T) *Server {
	t.Helper()
	verz := t.TempDir()
	for sprache, seiten := range map[string]map[string]string{
		"de": {"common/tar.md": "# tar\n\n> Archivierungstool.\n\n- Extrahiere ein Archiv in ein Verzeichnis:\n\n`tar xf {{quelle.tar}} {{[-C|--directory]}} {{verzeichnis}}`\n"},
		"en": {
			"common/tar.md":  "# tar\n\n> Archiving utility.\n\n- E[x]tract an archive into a directory:\n\n`tar xf {{source.tar}} {{[-C|--directory]}} {{directory}}`\n",
			"common/find.md": "# find\n\n> Find files or directories.\n\n- Find files by extension:\n\n`find {{path}} -name '*.{{ext}}'`\n",
		},
	} {
		f, err := os.Create(filepath.Join(verz, "tldr-pages."+sprache+".zip"))
		if err != nil {
			t.Fatal(err)
		}
		w := zip.NewWriter(f)
		for name, inhalt := range seiten {
			x, _ := w.Create(name)
			x.Write([]byte(inhalt))
		}
		w.Close()
		f.Close()
	}
	ix, err := help.Laden(verz)
	if err != nil {
		t.Fatal(err)
	}
	return &Server{hilfe: ix}
}

func hilfeAnfrage(t *testing.T, h http.HandlerFunc, ziel, sprache, name string, v any) int {
	t.Helper()
	r := httptest.NewRequest("GET", ziel, nil)
	if sprache != "" {
		r.Header.Set("X-SSH-App-Sprache", sprache)
	}
	if name != "" {
		r.SetPathValue("name", name)
	}
	w := httptest.NewRecorder()
	h(w, r)
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("%s: %v (%s)", ziel, err, w.Body.String())
	}
	return w.Code
}

func TestHilfeSprache(t *testing.T) {
	s := hilfeTestServer(t)

	var seite help.Seite
	if hilfeAnfrage(t, s.hilfeSeite, "/api/hilfe/seite/tar", "de", "tar", &seite); seite.Sprache != "de" {
		t.Fatalf("deutsche Seite erwartet: %+v", seite)
	}
	if hilfeAnfrage(t, s.hilfeSeite, "/api/hilfe/seite/tar", "en", "tar", &seite); seite.Sprache != "en" {
		t.Fatalf("englische Seite erwartet: %+v", seite)
	}
	// Ohne Kopf zaehlt Accept-Language.
	r := httptest.NewRequest("GET", "/api/hilfe/seite/tar", nil)
	r.Header.Set("Accept-Language", "en-US,en;q=0.9")
	r.SetPathValue("name", "tar")
	w := httptest.NewRecorder()
	s.hilfeSeite(w, r)
	if !strings.Contains(w.Body.String(), "Archiving utility") {
		t.Fatalf("Accept-Language: %s", w.Body.String())
	}
	// Ein Suchtreffer aus der anderen Sprache holt genau diese Seite.
	if hilfeAnfrage(t, s.hilfeSeite, "/api/hilfe/seite/tar?seitensprache=en", "de", "tar", &seite); seite.Sprache != "en" {
		t.Fatalf("seitensprache: %+v", seite)
	}
	if hilfeAnfrage(t, s.hilfeSeite, "/api/hilfe/seite/find?seitensprache=de", "de", "find", &seite); seite.Sprache != "en" {
		t.Fatalf("seitensprache ohne Seite: Rueckfall erwartet: %+v", seite)
	}

	var f map[string]string
	if code := hilfeAnfrage(t, s.hilfeSeite, "/api/hilfe/seite/gibtsnicht", "de", "gibtsnicht", &f); code != 404 || f["fehler"] != "Keine Hilfe zu «gibtsnicht»" {
		t.Fatalf("%d %v", code, f)
	}
	if code := hilfeAnfrage(t, s.hilfeSeite, "/api/hilfe/seite/gibtsnicht", "en", "gibtsnicht", &f); code != 404 || f["fehler"] != "No help for “gibtsnicht”" {
		t.Fatalf("%d %v", code, f)
	}

	var tr []help.Treffer
	hilfeAnfrage(t, s.hilfeSuche, "/api/hilfe/suche?q=archiv+entpacken", "en", "", &tr)
	if len(tr) == 0 || tr[0].Name != "tar" || tr[0].Sprache != "en" || tr[0].Beispiel == nil || !strings.HasPrefix(tr[0].Beispiel.Text, "E[x]tract") {
		t.Fatalf("Suche en: %+v", tr)
	}
	hilfeAnfrage(t, s.hilfeSuche, "/api/hilfe/suche?q=extract+archive", "de", "", &tr)
	if len(tr) == 0 || tr[0].Name != "tar" || tr[0].Sprache != "de" {
		t.Fatalf("Suche de: %+v", tr)
	}

	var e struct {
		Gefunden bool        `json:"gefunden"`
		Seite    help.Seite  `json:"seite"`
		Teile    []help.Teil `json:"teile"`
	}
	hilfeAnfrage(t, s.hilfeErklaeren, "/api/hilfe/erklaeren?zeile="+"tar+-C+%2Ftmp+-q", "en", "", &e)
	if !e.Gefunden || e.Seite.Sprache != "en" || len(e.Teile) != 3 ||
		!strings.HasPrefix(e.Teile[1].Erklaerung, "--directory — example:") || e.Teile[2].Erklaerung != "not in the examples — see man tar" {
		t.Fatalf("Erklaeren en: %+v", e)
	}
	hilfeAnfrage(t, s.hilfeErklaeren, "/api/hilfe/erklaeren?zeile="+"tar+-C+%2Ftmp+-q", "de", "", &e)
	if e.Seite.Sprache != "de" || !strings.HasPrefix(e.Teile[1].Erklaerung, "--directory — Beispiel:") || e.Teile[2].Erklaerung != "nicht in den Beispielen — man tar" {
		t.Fatalf("Erklaeren de: %+v", e)
	}
}
