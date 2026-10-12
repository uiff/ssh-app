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

func TestLadenSuchenErklaeren(t *testing.T) {
	verz := t.TempDir()
	testZip(t, verz, "de", map[string]string{"common/tar.md": tarDE})
	testZip(t, verz, "en", map[string]string{"common/tar.md": "# tar\n\n> english\n", "common/find.md": findEN, "windows/dir.md": "# dir\n"})
	ix, err := Laden(verz)
	if err != nil {
		t.Fatal(err)
	}
	if ix.Anzahl() != 2 {
		t.Fatalf("Windows-Seiten muessen fehlen: %d", ix.Anzahl())
	}
	s, ok := ix.Seite("tar")
	if !ok || s.Sprache != "de" || len(s.Beispiele) != 3 || s.Mehr == "" {
		t.Fatalf("Deutsch muss gewinnen: %+v", s)
	}
	tr := ix.Suchen("archiv extrahiere", 5)
	if len(tr) == 0 || tr[0].Name != "tar" || tr[0].Beispiel == nil || !strings.Contains(tr[0].Beispiel.Text, "Extrahiere") {
		t.Fatalf("Suche: %+v", tr)
	}
	if tr := ix.Suchen("find", 5); len(tr) == 0 || tr[0].Name != "find" {
		t.Fatalf("Namenssuche: %+v", tr)
	}
	_, teile, ok := ix.Erklaeren("sudo tar -xzf a.tgz -C /tmp")
	if !ok || len(teile) != 3 {
		t.Fatalf("Erklaeren: %+v", teile)
	}
	if !strings.Contains(teile[2].Erklaerung, "Verzeichnis") {
		t.Fatalf("-C aus Optionsplatzhalter: %+v", teile[2])
	}
	if !strings.Contains(teile[1].Erklaerung, "man tar") {
		t.Fatalf("unbekannter Schalter muss ehrlich sein: %+v", teile[1])
	}
	_, teile, _ = ix.Erklaeren("find . -size +5M")
	if teile[1].Erklaerung != "Find files larger than a size" {
		t.Fatalf("%+v", teile)
	}
}

func TestOhneArchiveLeer(t *testing.T) {
	ix, err := Laden(t.TempDir())
	if err != nil || ix.Anzahl() != 0 {
		t.Fatal(err)
	}
	if len(ix.Suchen("tar", 5)) != 0 {
		t.Fatal("leerer Index liefert Treffer")
	}
}
