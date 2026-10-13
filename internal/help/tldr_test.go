package help

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const tarDE = "# tar\n\n> Archivierungstool.\n> Weitere Informationen: <https://www.gnu.org/software/tar/manual/tar.html>.\n\n" +
	"- Erstelle ein mit gzip komprimiertes Archiv:\n\n`tar czf {{ziel.tar.gz}} {{pfad/zu/datei1}}`\n\n" +
	"- Extrahiere ein Archiv in ein Verzeichnis:\n\n`tar xf {{pfad/zu/quelle.tar}} {{[-C|--directory]}} {{pfad/zu/verzeichnis}}`\n\n" +
	"- Liste den Inhalt auf:\n\n`tar tvf {{quelle.tar}}`\n"

const tarEN = "# tar\n\n> Archiving utility.\n> More information: <https://www.gnu.org/software/tar/manual/tar.html>.\n\n" +
	"- [c]reate a g[z]ipped archive and write it to a [f]ile:\n\n`tar czf {{path/to/target.tar.gz}} {{path/to/file1}}`\n\n" +
	"- E[x]tract a (compressed) archive [f]ile into the target directory:\n\n`tar xf {{path/to/source.tar}} {{[-C|--directory]}} {{path/to/directory}}`\n\n" +
	"- Lis[t] the contents of a tar [f]ile [v]erbosely:\n\n`tar tvf {{path/to/source.tar}}`\n"

const findEN = "# find\n\n> Find files or directories.\n\n" +
	"- Find files larger than a size:\n\n`find {{path/to/dir}} -type f -size +{{100M}}`\n"

func testZip(t *testing.T, verz, sprache string, seiten map[string]string) {
	t.Helper()
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

func testIndex(t *testing.T) *Index {
	t.Helper()
	verz := t.TempDir()
	testZip(t, verz, "de", map[string]string{
		"common/tar.md": tarDE,
		// nur deutsch / nur englisch, sonst gleich gut fuer «datei»
		"common/lsx.md": "# lsx\n\n> Listet Dateien.\n",
		// macOS-Seite auf Deutsch, Linux-Seite nur auf Englisch
		"osx/open.md": "# open\n\n> Öffnet Dateien unter macOS.\n",
		// beide Sprachen, aber nur die deutsche passt zu «copy» (Synonym «kopier»)
		"common/cpx.md": "# cpx\n\n> Kopiert Dateien.\n\n- Kopiere eine Datei:\n\n`cpx {{quelle}} {{ziel}}`\n",
	})
	testZip(t, verz, "en", map[string]string{
		"common/tar.md":  tarEN,
		"common/find.md": findEN,
		"common/lsy.md":  "# lsy\n\n> Lists files.\n",
		"common/open.md": "# open\n\n> Open files with the default app.\n",
		"windows/dir.md": "# dir\n",
		"common/cpx.md":  "# cpx\n\n> Duplicates things.\n\n- Duplicate one thing:\n\n`cpx {{source}} {{target}}`\n",
	})
	ix, err := Laden(verz)
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

func TestLadenSeiteJeSprache(t *testing.T) {
	ix := testIndex(t)
	if ix.Anzahl() != 6 || ix.AnzahlIn("de") != 4 || ix.AnzahlIn("en") != 5 {
		t.Fatalf("Windows-Seiten muessen fehlen, je Befehl beide Sprachen: %d de=%d en=%d", ix.Anzahl(), ix.AnzahlIn("de"), ix.AnzahlIn("en"))
	}
	if ix.Quelle != "de+en" {
		t.Fatal(ix.Quelle)
	}
	s, ok := ix.Seite("tar", "de")
	if !ok || s.Sprache != "de" || len(s.Beispiele) != 3 || s.Mehr == "" {
		t.Fatalf("deutsche Oberflaeche: deutsche Seite: %+v", s)
	}
	s, ok = ix.Seite("TAR", "en-US")
	if !ok || s.Sprache != "en" || !strings.Contains(s.Beschreibung, "Archiving") {
		t.Fatalf("englische Oberflaeche: englische Seite: %+v", s)
	}
	if s, ok = ix.Seite("find", "de"); !ok || s.Sprache != "en" {
		t.Fatalf("Rueckfall auf Englisch: %+v", s)
	}
	if s, ok = ix.Seite("lsx", "en"); !ok || s.Sprache != "de" {
		t.Fatalf("Rueckfall auf Deutsch: %+v", s)
	}
	if s, ok = ix.Seite("open", "de"); !ok || s.Sprache != "en" || s.Plattform != "common" {
		t.Fatalf("Linux/allgemein vor macOS, auch ueber die Sprache hinweg: %+v", s)
	}
	if _, ok = ix.SeiteGenau("find", "de"); ok {
		t.Fatal("SeiteGenau darf nicht zurueckfallen")
	}
	if s, ok = ix.SeiteGenau("open", "de"); !ok || s.Plattform != "osx" {
		t.Fatalf("SeiteGenau: %+v", s)
	}
}

func TestSuchenZweisprachig(t *testing.T) {
	ix := testIndex(t)
	// Deutsche Worte, deutsche Oberflaeche.
	tr := ix.Suchen("archiv extrahiere", "de", 5)
	if len(tr) == 0 || tr[0].Name != "tar" || tr[0].Sprache != "de" || tr[0].Beispiel == nil || !strings.Contains(tr[0].Beispiel.Text, "Extrahiere") {
		t.Fatalf("de/de: %+v", tr)
	}
	// Deutsche Worte, englische Oberflaeche: Synonyme fuehren zur englischen Seite.
	tr = ix.Suchen("archiv entpacken", "en", 5)
	if len(tr) == 0 || tr[0].Name != "tar" || tr[0].Sprache != "en" || tr[0].Beispiel == nil || !strings.Contains(tr[0].Beispiel.Text, "E[x]tract") {
		t.Fatalf("de-Worte/en: %+v", tr)
	}
	// Englische Worte, deutsche Oberflaeche: deutsche Seite.
	tr = ix.Suchen("extract archive", "de", 5)
	if len(tr) == 0 || tr[0].Name != "tar" || tr[0].Sprache != "de" || tr[0].Beispiel == nil || !strings.Contains(tr[0].Beispiel.Text, "Extrahiere") {
		t.Fatalf("en-Worte/de: %+v", tr)
	}
	// Merkhilfen «[c]reate» stehen der Suche nicht im Weg.
	tr = ix.Suchen("create gzipped", "en", 5)
	if len(tr) == 0 || tr[0].Name != "tar" || tr[0].Beispiel == nil || !strings.HasPrefix(tr[0].Beispiel.Text, "[c]reate") {
		t.Fatalf("Merkhilfen: %+v", tr)
	}
	// Gleiche Relevanz: die Sprache der Oberflaeche zuerst.
	for _, f := range []struct{ sprache, erst, dann string }{{"de", "lsx", "lsy"}, {"en", "lsy", "lsx"}} {
		tr = ix.Suchen("datei", f.sprache, 10)
		var namen []string
		for _, x := range tr {
			if x.Name == "lsx" || x.Name == "lsy" {
				namen = append(namen, x.Name)
			}
		}
		if len(namen) != 2 || namen[0] != f.erst || namen[1] != f.dann {
			t.Fatalf("%s: %v (%+v)", f.sprache, namen, tr)
		}
	}
	tr = ix.Suchen("files", "de", 10)
	if len(tr) < 2 {
		t.Fatalf("englisches Wort findet deutsche Seite: %+v", tr)
	}
	if tr := ix.Suchen("find", "de", 5); len(tr) == 0 || tr[0].Name != "find" || tr[0].Sprache != "en" {
		t.Fatalf("Namenssuche mit Rueckfall: %+v", tr)
	}
	if tr := ix.Suchen("tar", "en", 5); len(tr) == 0 || tr[0].Name != "tar" || tr[0].Sprache != "en" || tr[0].Beispiel != nil {
		t.Fatalf("Namenssuche englisch: %+v", tr)
	}
	// Nur die deutsche Seite passt, die englische gibt es: gezeigt wird die englische
	// (samt dem Beispiel an derselben Stelle), die andere Sprache ist nur Rueckfall.
	tr = ix.Suchen("copy", "en", 5)
	if len(tr) == 0 || tr[0].Name != "cpx" || tr[0].Sprache != "en" || tr[0].Beschreibung != "Duplicates things." ||
		tr[0].Beispiel == nil || tr[0].Beispiel.Text != "Duplicate one thing" {
		t.Fatalf("Treffer ueber die andere Sprache, gezeigt in der eigenen: %+v", tr)
	}
	if tr = ix.Suchen("copy", "de", 5); len(tr) == 0 || tr[0].Name != "cpx" || tr[0].Sprache != "de" {
		t.Fatalf("copy/de: %+v", tr)
	}
}

func TestErklaerenZweisprachig(t *testing.T) {
	ix := testIndex(t)
	s, teile, ok := ix.Erklaeren("sudo tar -xzf a.tgz -C /tmp", "de")
	if !ok || s.Sprache != "de" || len(teile) != 3 {
		t.Fatalf("Erklaeren: %+v", teile)
	}
	if !strings.Contains(teile[2].Erklaerung, "--directory — Beispiel: Extrahiere") {
		t.Fatalf("-C aus Optionsplatzhalter: %+v", teile[2])
	}
	// Gebuendelte Kurzschalter ueber die Merkhilfen: die deutsche Seite hat keine, also das
	// deutsche Wort im selben Beispiel, wenn es eindeutig ist (gleicher Wortstamm, oder ein
	// Synonym in mindestens zwei Beispielen); sonst die englische Merkhilfe mit Herkunft.
	// Hier belegt nur ein Beispiel «gzip» zu «g[z]ipped» (die echte Seite hat zwei).
	if teile[1].Erklaerung != "-x: E[x]trahiere · -z: g[z]ipped (englische Seite) · -f: [f]ile (englische Seite)" {
		t.Fatalf("Buendel deutsch: %+v", teile[1])
	}
	if _, teile, _ = ix.Erklaeren("tar -q", "de"); teile[1].Erklaerung != "nicht in den Beispielen — siehe man tar" {
		t.Fatalf("unbekannter Schalter muss ehrlich sein: %+v", teile[1])
	}
	s, teile, ok = ix.Erklaeren("ls | tar -xzf a.tgz -C /tmp", "en")
	if !ok || s.Sprache != "en" || len(teile) != 3 || teile[0].Erklaerung != "Archiving utility." {
		t.Fatalf("Erklaeren englisch: %+v %+v", s, teile)
	}
	if !strings.Contains(teile[2].Erklaerung, "--directory — example: E[x]tract") {
		t.Fatalf("-C englisch: %+v", teile[2])
	}
	if teile[1].Erklaerung != "-x: E[x]tract · -z: g[z]ipped · -f: [f]ile" {
		t.Fatalf("Buendel englisch: %+v", teile[1])
	}
	if _, teile, _ = ix.Erklaeren("tar -xzqf", "en"); teile[1].Erklaerung != "-x: E[x]tract · -z: g[z]ipped · -q: see man tar · -f: [f]ile" {
		t.Fatalf("unbekannter Buchstabe im Buendel faellt nicht weg: %+v", teile[1])
	}
	if _, teile, _ = ix.Erklaeren("tar -q", "en"); teile[1].Erklaerung != "not in the examples — see man tar" {
		t.Fatalf("unbekannter Schalter englisch: %+v", teile[1])
	}
	// Seite nur auf Englisch, Oberflaeche deutsch: Beispieltext englisch, eigener Hinweis deutsch.
	s, teile, _ = ix.Erklaeren("find . -size +5M -xdev", "de")
	if s.Sprache != "en" || teile[1].Erklaerung != "Find files larger than a size" || teile[2].Erklaerung != "nicht in den Beispielen — siehe man find" {
		t.Fatalf("%+v", teile)
	}
	if _, _, ok := ix.Erklaeren("unbekannt -x", "en"); ok {
		t.Fatal("unbekannter Befehl")
	}
}

func TestOhneArchiveLeer(t *testing.T) {
	ix, err := Laden(t.TempDir())
	if err != nil || ix.Anzahl() != 0 {
		t.Fatal(err)
	}
	if len(ix.Suchen("tar", "de", 5)) != 0 || len(ix.Suchen("tar", "en", 5)) != 0 {
		t.Fatal("leerer Index liefert Treffer")
	}
	if _, ok := ix.Seite("tar", "de"); ok {
		t.Fatal("leerer Index liefert Seite")
	}
}
