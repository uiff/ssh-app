package web

import (
	"io/fs"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

const lizenzOrdner = "static/vendor/licenses"

// Waechter: Die Lizenzseite verlinkt jeden Lizenztext, und jeder Link zeigt auf eine
// vorhandene Datei.
func TestLizenzseiteVerlinktAlleTexte(t *testing.T) {
	seite, err := fs.ReadFile(Static, lizenzOrdner+"/index.html")
	if err != nil {
		t.Fatal(err)
	}
	links := map[string]bool{}
	for _, m := range regexp.MustCompile(`href="([^"]+)"`).FindAllStringSubmatch(string(seite), -1) {
		if !strings.Contains(m[1], "/") && !strings.Contains(m[1], ":") {
			links[m[1]] = true
		}
	}
	dateien, err := fs.ReadDir(Static, lizenzOrdner)
	if err != nil {
		t.Fatal(err)
	}
	da := map[string]bool{}
	for _, d := range dateien {
		if d.Name() == "index.html" {
			continue
		}
		da[d.Name()] = true
		if !links[d.Name()] {
			t.Errorf("%s ist auf der Lizenzseite nicht verlinkt", d.Name())
		}
	}
	for l := range links {
		if !da[l] {
			t.Errorf("Lizenzseite verlinkt %s, die Datei fehlt", l)
		}
	}
}

// Waechter: Fuer jedes Go-Modul, das ins Programm uebersetzt wird, liegt ein Lizenztext bei
// (go-<modulpfad mit «-» statt «/»>.LICENSE). Eine neue Abhaengigkeit ohne Lizenztext faellt so auf.
func TestLizenztextFuerJedesGoModul(t *testing.T) {
	if testing.Short() {
		t.Skip("braucht «go list»")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go nicht im PATH")
	}
	out, err := exec.Command(goBin, "list", "-deps", "-f", "{{with .Module}}{{.Path}}{{end}}", "github.com/uiff/ssh-app/cmd/ssh-app").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	module := map[string]bool{}
	for _, m := range strings.Fields(string(out)) {
		if m != "github.com/uiff/ssh-app" {
			module[m] = true
		}
	}
	if len(module) == 0 {
		t.Fatal("go list lieferte keine Module")
	}
	for m := range module {
		name := lizenzOrdner + "/go-" + strings.ReplaceAll(m, "/", "-") + ".LICENSE"
		if _, err := fs.Stat(Static, name); err != nil {
			t.Errorf("Lizenztext fehlt für %s (%s)", m, name)
		}
	}
}
