package help

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func kleinerIndex(t *testing.T, de, en map[string]string) *Index {
	t.Helper()
	verz := t.TempDir()
	testZip(t, verz, "de", de)
	testZip(t, verz, "en", en)
	ix, err := Laden(verz)
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

// Die Sprache entscheidet nur bei gleich guten Treffern: eine englische Seite, die besser
// passt, steht vor einer deutschen, die das Wort nur nebenbei enthaelt.
func TestSucheSpracheNurBeiGleichstand(t *testing.T) {
	ix := kleinerIndex(t,
		map[string]string{"common/nebenbei.md": "# nebenbei\n\n> Ein Werkzeug.\n\n- Kopiere etwas nebenbei:\n\n`nebenbei`\n"},
		map[string]string{"common/dupl.md": "# dupl\n\n> Copy files.\n\n- Copy a file:\n\n`dupl {{a}} {{b}}`\n"})
	tr := ix.Suchen("kopieren", "de", 5)
	if len(tr) != 2 || tr[0].Name != "dupl" || tr[0].Sprache != "en" {
		t.Fatalf("besser passende englische Seite zuerst: %+v", tr)
	}
}

// Passt die deutsche Seite eines Befehls schlechter (oder gar nicht) und laesst sich das passende
// Beispiel nicht auf sie abbilden, zeigt die deutsche Oberflaeche die englische Seite.
// Laesst es sich abbilden, bleibt es bei der deutschen Seite mit dem entsprechenden Beispiel.
func TestSucheRueckfallAufEnglisch(t *testing.T) {
	ix := kleinerIndex(t,
		map[string]string{
			"linux/dienstx.md": "# dienstx\n\n> Steuert Dienste.\n\n- Zeige den Status eines Dienstes:\n\n`dienstx status {{einheit}}`\n",
			"common/katze.md":  "# katze\n\n> Verkette Dateien.\n\n- Gib den Inhalt einer Datei aus:\n\n`katze {{pfad/zu/datei}}`\n\n- Verkette Dateien:\n\n`katze {{datei1}} {{datei2}}`\n",
		},
		map[string]string{
			"linux/dienstx.md": "# dienstx\n\n> Control services.\n\n- Show the status of a service:\n\n`dienstx status {{unit}}`\n\n- Restart a service:\n\n`dienstx restart {{unit}}`\n",
			"common/katze.md":  "# katze\n\n> Print and concatenate files.\n\n- Print the contents of a file:\n\n`katze {{path/to/file}}`\n",
		})
	tr := ix.Suchen("dienst neu starten", "de", 5)
	if len(tr) == 0 || tr[0].Name != "dienstx" || tr[0].Sprache != "en" || tr[0].Beispiel == nil || tr[0].Beispiel.Text != "Restart a service" {
		t.Fatalf("Rueckfall auf die englische Seite: %+v", tr)
	}
	tr = ix.Suchen("datei anzeigen", "de", 5)
	if len(tr) == 0 || tr[0].Name != "katze" || tr[0].Sprache != "de" || tr[0].Beispiel == nil || tr[0].Beispiel.Text != "Gib den Inhalt einer Datei aus" {
		t.Fatalf("Beispiel auf die deutsche Seite abgebildet (Geruest «katze», andere Anzahl Beispiele): %+v", tr)
	}
	// Englische Oberflaeche: die englische Seite bleibt, auch wenn nur die deutsche passt.
	tr = ix.Suchen("status dienst", "en", 5)
	if len(tr) == 0 || tr[0].Name != "dienstx" || tr[0].Sprache != "en" {
		t.Fatalf("en: %+v", tr)
	}
}

func TestSucheWendungenUndFuellwoerter(t *testing.T) {
	if q := wendungen("dienst neu starten"); q != "dienst neustart" {
		t.Fatal(q)
	}
	if w := ohneFuellwoerter(strings.Fields("text in der datei suchen")); strings.Join(w, " ") != "text datei suchen" {
		t.Fatal(w)
	}
	if w := ohneFuellwoerter([]string{"in"}); len(w) != 1 {
		t.Fatal("nur Fuellwoerter: bleiben stehen")
	}
	b := kernFuer([]string{"loschen", "ordner"})
	if b["rm"] != kernBonus || b["rmdir"] != kernBonus-10 || b["cp"] != 0 {
		t.Fatalf("%v", b)
	}
	// «neu» allein ist kein Synonym mehr (traf jede deutsche Seite mit «neue …»).
	for _, a := range erweitern("neustart") {
		if a == "neu" {
			t.Fatal("neu")
		}
	}
}

// Alltagsfragen mit den echten tldr-Archiven (SSHAPP_HILFE, sonst /app/hilfe wie im Image);
// ohne Archive wird der Test uebersprungen. Erwartet ist der Spitzenbefehl (oder einer davon).
func TestSucheAlltagsfragenEchteArchive(t *testing.T) {
	verz := os.Getenv("SSHAPP_HILFE")
	if verz == "" {
		verz = "/app/hilfe"
	}
	if _, err := os.Stat(filepath.Join(verz, "tldr-pages.de.zip")); err != nil {
		t.Skip("keine tldr-Archive in " + verz)
	}
	if _, err := os.Stat(filepath.Join(verz, "tldr-pages.en.zip")); err != nil {
		t.Skip("keine tldr-Archive in " + verz)
	}
	ix, err := Laden(verz)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []struct {
		q, sprache string
		spitze     []string
	}{
		{"dienst neu starten", "de", []string{"systemctl"}},
		{"neu starten", "de", []string{"reboot", "systemctl"}},
		{"herunterfahren", "de", []string{"shutdown", "poweroff"}},
		{"archiv entpacken", "de", []string{"tar"}},
		{"entpacken", "de", []string{"tar", "unzip"}},
		{"datei löschen", "de", []string{"rm"}},
		{"löschen", "de", []string{"rm"}},
		{"datei kopieren", "de", []string{"cp"}},
		{"kopieren", "de", []string{"cp"}},
		{"datei anzeigen", "de", []string{"cat"}},
		{"inhalt anzeigen", "de", []string{"cat", "ls"}},
		{"ordner anlegen", "de", []string{"mkdir"}},
		{"verzeichnis erstellen", "de", []string{"mkdir"}},
		{"benutzer anlegen", "de", []string{"useradd"}},
		{"log ansehen", "de", []string{"journalctl", "tail"}},
		{"festplatte voll", "de", []string{"df", "du"}},
		{"prozess beenden", "de", []string{"kill", "pkill"}},
		{"datei verschieben", "de", []string{"mv"}},
		{"rechte ändern", "de", []string{"chmod"}},
		{"paket installieren", "de", []string{"apt", "apt-get", "dnf"}},
		{"geplante aufgabe", "de", []string{"crontab"}},
		{"text in datei suchen", "de", []string{"grep"}},
		{"ip adresse anzeigen", "de", []string{"ip"}},
		{"datei herunterladen", "de", []string{"wget", "curl"}},

		{"restart service", "en", []string{"systemctl"}},
		{"extract archive", "en", []string{"tar"}},
		{"delete file", "en", []string{"rm"}},
		{"copy file", "en", []string{"cp"}},
		{"show file contents", "en", []string{"cat"}},
		{"create directory", "en", []string{"mkdir"}},
		{"add user", "en", []string{"useradd"}},
		{"shut down", "en", []string{"shutdown"}},
		{"view logs", "en", []string{"journalctl"}},
		{"disk space", "en", []string{"df", "du"}},
		{"kill process", "en", []string{"kill", "pkill"}},
		{"change permissions", "en", []string{"chmod"}},
		{"install package", "en", []string{"apt", "apt-get", "dnf"}},
		{"search text in files", "en", []string{"grep"}},
		{"download file", "en", []string{"wget", "curl"}},
		{"scheduled task", "en", []string{"crontab"}},
	} {
		tr := ix.Suchen(f.q, f.sprache, 5)
		ok := false
		for _, s := range f.spitze {
			ok = ok || len(tr) > 0 && tr[0].Name == s
		}
		if !ok {
			var namen []string
			for _, x := range tr {
				namen = append(namen, x.Name+":"+x.Sprache)
			}
			t.Errorf("%s (%s): Spitze %v erwartet, bekommen %v", f.q, f.sprache, f.spitze, namen)
		}
	}
	// Deutsche Oberflaeche, gaengiger Befehl mit deutscher Seite: die deutsche Seite samt Beispiel.
	if tr := ix.Suchen("datei anzeigen", "de", 1); len(tr) == 0 || tr[0].Sprache != "de" || tr[0].Beispiel == nil {
		t.Errorf("cat deutsch mit Beispiel: %+v", tr)
	}
}
