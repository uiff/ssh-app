package help

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// echterIndex baut den Index aus testdata/tldr: unveraenderte Seiten aus tldr-pages
// (CC BY 4.0, © tldr-pages team and contributors) im Format der Release-Archive.
func echterIndex(t *testing.T) *Index {
	t.Helper()
	verz := t.TempDir()
	for _, sprache := range []string{"de", "en"} {
		wurzel := filepath.Join("testdata", "tldr", sprache)
		seiten := map[string]string{}
		err := filepath.WalkDir(wurzel, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(wurzel, p)
			seiten[filepath.ToSlash(rel)] = string(b)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		testZip(t, verz, sprache, seiten)
	}
	ix, err := Laden(verz)
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

func erklaerung(t *testing.T, ix *Index, zeile, sprache string) []Teil {
	t.Helper()
	_, teile, ok := ix.Erklaeren(zeile, sprache)
	if !ok {
		t.Fatalf("%s/%s: keine Seite", zeile, sprache)
	}
	return teile[1:]
}

// «tar -xzf» und «tar xzf»: Die tldr-Seiten schreiben tar-Schalter ohne Strich («tar czf»)
// und erklaeren die Buchstaben mit Merkhilfen («E[x]tract», «g[z]ipped», «[f]ile»).
func TestKurzschalterEchteSeiten(t *testing.T) {
	ix := echterIndex(t)
	for _, f := range []struct{ zeile, sprache, wort, erklaerung string }{
		{"tar -xzf a.tar.gz", "en", "-xzf", "-x: E[x]tract · -z: g[z]ipped · -f: [f]ile"},
		{"tar xzf a.tar.gz", "en", "xzf", "x: E[x]tract · z: g[z]ipped · f: [f]ile"},
		{"sudo tar -czvf a.tgz b", "en", "-czvf", "-c: [c]reate · -z: g[z]ipped · -v: [v]erbosely · -f: [f]ile"},
		{"tar -tf a.tar", "en", "-tf", "-t: Lis[t] · -f: [f]ile"},
		{"tar -x", "en", "-x", "E[x]tract"},
		// Deutsche Seite ohne Merkhilfen: das deutsche Wort, wenn es eindeutig dasselbe meint (in
		// jedem Gegenstueck der englischen Beispiele mit dieser Merkhilfe), mit dem Buchstaben
		// markiert — sonst die englische Merkhilfe mit ihrer Herkunft. «Dateien» meint bei tar die
		// Eingabedateien, nicht die Archivdatei «[f]ile»; «ausführlichen» steht nur in einem der
		// zwei Gegenstuecke von «[v]erbosely».
		{"tar -xzf a.tar.gz", "de", "-xzf", "-x: E[x]trahiere · -z: g[z]ip · -f: [f]ile (englische Seite)"},
		{"tar xzf a.tar.gz", "de", "xzf", "x: E[x]trahiere · z: g[z]ip · f: [f]ile (englische Seite)"},
		{"tar -czvf a.tgz b", "de", "-czvf", "-c: Erstelle (englisch: [c]reate) · -z: g[z]ip · -v: [v]erbosely (englische Seite) · -f: [f]ile (englische Seite)"},
		// Kein entsprechendes deutsches Wort («Führe … auf»): englische Merkhilfe mit Herkunft.
		{"tar -tf a.tar", "de", "-tf", "-t: Lis[t] (englische Seite) · -f: [f]ile (englische Seite)"},
		// Jeder Buchstabe bekommt einen Eintrag, einer ohne Erklaerung «siehe man».
		{"tar -cjf a.tbz b", "en", "-cjf", "-c: [c]reate · -j: see man tar · -f: [f]ile"},
		{"tar -cjf a.tbz b", "de", "-cjf", "-c: Erstelle (englisch: [c]reate) · -j: siehe man tar · -f: [f]ile (englische Seite)"},
		{"tar xjf a.tbz", "en", "xjf", "x: E[x]tract · j: see man tar · f: [f]ile"},
		// ls: Ein einziges Beispiel reicht nicht fuer ein deutsches Wort ohne den Buchstaben
		// («größter» zu «[S]ize»); die Langform an derselben Stelle erklaert «-h».
		{"ls -lS", "de", "-lS", "-l: [l]ong (englische Seite) · -S: [S]ize (englische Seite)"},
		{"ls -lah", "en", "-lah", "-l: [l]ong · -a: --all — example: List all files, including hidden files · -h: --human-readable — example: List files in [l]ong format with size displayed using human-readable units (KiB, MiB, GiB)"},
		{"ls -lah", "de", "-lah", "-l: [l]ong (englische Seite) · -a: --all — Beispiel: Liste alle Dateien inklusive versteckter Dateien auf · -h: --human-readable — Beispiel: Liste alle Dateien mit Dateigröße in für Menschen lesbaren Einheiten (KiB, MiB, GiB)"},
		{"ls -ltr", "de", "-ltr", "-lt --reverse — Beispiel: Liste alle Dateien sortiert nach dem Änderungsdatum mit ältester beginnend auf"},
		// rm: Was die deutsche Seite nicht kennt, kommt von der englischen, mit Herkunft.
		{"rm -rf x", "en", "-rf", "-r: --recursive — example: Remove specific files and directories recursively · -f: --force — example: Remove specific files ignoring nonexistent ones"},
		{"rm -rf x", "de", "-rf", "-r: Lösche ein Verzeichnis und alle seine Unterverzeichnisse rekursiv · -f: --force — Beispiel: Remove specific files ignoring nonexistent ones (englische Seite)"},
		{"rm --force x", "de", "--force", "-f — Beispiel: Remove specific files ignoring nonexistent ones (englische Seite)"},
		// ss/netstat: Langformen an derselben Stelle wie der Buchstabe («-lt|--listening --tcp»).
		{"ss -tulpn", "en", "-tulpn", "-t: --tcp — example: Show all TCP sockets connected to the local HTTPS port (443) · -u: --udp — example: Show all UDP sockets connected on specific source and destination ports · -l: --listening — example: Show all TCP sockets listening on the local 8080 port · -p: --processes — example: Show all TCP sockets along with processes connected to a remote SSH port · -n: see man ss"},
		{"netstat -tulpn", "en", "-tulpn", "-t: --tcp — example: List listening TCP ports · -u: --udp — example: List listening TCP and UDP ports (+ user and process if you're root) · -l: --listening — example: List all listening ports · -p: --program — example: Display PID and program names · -n: --numeric — example: List routes and do not resolve IP addresses to hostnames"},
		// Genau die Buchstaben eines Beispiels (Reihenfolge egal): dessen Text.
		{"tar -xvf a.tar", "en", "-xvf", "E[x]tract a (compressed) archive [f]ile into the current directory [v]erbosely"},
		{"tar -fvx a.tar", "de", "-fvx", "Extrahiere ein (komprimiertes) Archiv in das derzeitige Verzeichnis im ausführlichen Modus"},
		// Optionsplatzhalter bleiben wie bisher.
		{"tar xf a.tar -C /tmp", "en", "-C", "--directory — example: [c]reate a g[z]ipped (compressed) archive from a directory using relative paths"},
		// 7z: Befehlsbuchstabe ohne Strich; die deutsche Seite hat eigene Merkhilfen.
		{"7z x a.7z", "de", "x", "E[x]trahiere ein Archiv und behalte die originale Verzeichnisstruktur bei"},
		{"7z l a.7z", "en", "l", "[l]ist the contents of an archive"},
		// ps: Buendel ohne Strich, ohne Merkhilfen — nur das genau passende Beispiel.
		{"ps aux", "en", "aux", "List all running processes"},
		{"ps aux", "de", "aux", "List all running processes"},
		{"ps -aux", "en", "-aux", "List all running processes"},
		// ps mit Strich: unbekannte Buchstaben stehen da, statt still wegzufallen.
		{"ps -eo pid,cmd", "en", "-eo", "-e: see man ps · -o: --format — example: Get the parent PID of a process"},
		{"ps -eo pid,cmd", "de", "-eo", "-e: siehe man ps · -o: --format — Beispiel: Get the parent PID of a process"},
		// Kein Buchstabe bekannt: ein Hinweis fuer das ganze Buendel.
		{"ps -ef", "en", "-ef", "not in the examples — see man ps"},
	} {
		teile := erklaerung(t, ix, f.zeile, f.sprache)
		var gefunden *Teil
		for i := range teile {
			if teile[i].Wort == f.wort {
				gefunden = &teile[i]
			}
		}
		if gefunden == nil || gefunden.Erklaerung != f.erklaerung {
			t.Errorf("%s (%s): %+v\n  erwartet %s: %s", f.zeile, f.sprache, teile, f.wort, f.erklaerung)
		}
	}
	// Ohne Strich nur, wo die Seite Buendel kennt: «ps auxf» passt zu keinem Beispiel,
	// ein Wort nach einem Befehl ohne Buendel-Beispiele ist kein Schalter.
	// Ein Wort ohne Strich, dessen Buchstaben grossteils unbekannt sind, ist kein Buendel («tar archiv»).
	for _, f := range []struct{ zeile, sprache string }{{"ps auxf", "en"}, {"ps notizen", "de"}, {"7z a.7z", "de"}, {"tar archiv", "de"}, {"tar list", "en"}} {
		if teile := erklaerung(t, ix, f.zeile, f.sprache); len(teile) != 0 {
			t.Errorf("%s: nichts zu erklaeren erwartet: %+v", f.zeile, teile)
		}
	}
	if teile := erklaerung(t, ix, "tar -j a", "de"); len(teile) != 1 || teile[0].Erklaerung != "nicht in den Beispielen — siehe man tar" {
		t.Errorf("unbekannter Kurzschalter: %+v", teile)
	}
}

func TestMerkhilfeEigeneSpracheZuerst(t *testing.T) {
	ix := echterIndex(t)
	p := ix.seiten["7z"]
	// Die deutsche 7z-Seite hat eigene Merkhilfen («[a]rchiviere»).
	if m := merkhilfe(p.de, p.en, 'a', "de"); m != "[a]rchiviere" {
		t.Fatalf("de: %q", m)
	}
	if m := merkhilfe(p.en, p.de, 'a', "en"); m != "[a]dd" {
		t.Fatalf("en: %q", m)
	}
	if merkwort("Erstelle [C] eine Datei", 'C') != "" || merkwort("in a [f]ile:", 'f') != "[f]ile" || merkwort("E[x]tract", 'z') != "" {
		t.Fatal("merkwort")
	}
	if markieren("Extrahiere", 'x') != "E[x]trahiere" || markieren("Dateien", 'f') != "" {
		t.Fatal("markieren")
	}
	if !verwandt("extract", "extrahiere") || !verwandt("archiv", "archive") || verwandt("datei", "date") || verwandt("file", "filter") {
		t.Fatal("verwandt")
	}
	if g := geruest("tar xf {{pfad/zu/quelle.tar}} {{[-C|--directory]}} {{pfad/zu/verzeichnis}}"); g != "tar xf -C|--directory" {
		t.Fatalf("geruest: %q", g)
	}
	// Eindeutige Zuordnung: «Dateien» (Eingabedateien) ist nicht «[f]ile» (Archivdatei),
	// «ausführlichen» steht nur in einem Gegenstueck von «[v]erbosely».
	tar, ls := ix.seiten["tar"], ix.seiten["ls"]
	for _, f := range []struct {
		p    *paar
		c    rune
		wort string
	}{{tar, 'x', "Extrahiere"}, {tar, 'c', "Erstelle"}, {tar, 'z', "gzip"}, {tar, 'f', ""}, {tar, 'v', ""}, {ls, 'S', ""}, {ls, 't', ""}} {
		if w := eigenesWort(f.p.de, f.p.en, f.c, merkwortIn(f.p.en, f.c)); w != f.wort {
			t.Errorf("eigenesWort %s %c: %q, erwartet %q", f.p.de.Name, f.c, w, f.wort)
		}
	}
	// Synonyme nur als ganzes Wort (hoechstens mit «e»): «date» (zu «[t]ime») passt nicht zu
	// «Dateien», «ausfuhrlich» nicht zum gebeugten «ausführlichen».
	for _, f := range []struct{ text, fremd, wort string }{
		{"Liste alle Dateien sortiert nach dem Änderungsdatum", "[t]ime", ""},
		{"im ausführlichen Modus", "[v]erbosely", ""},
		{"Erstelle ein Archiv", "[c]reate", "Erstelle"},
		{"Extrahiere ein Archiv", "E[x]tract", "Extrahiere"},
	} {
		if w, _ := passendesWort(f.text, f.fremd); w != f.wort {
			t.Errorf("passendesWort(%q, %q) = %q, erwartet %q", f.text, f.fremd, w, f.wort)
		}
	}
	if !strings.Contains(ix.seiten["tar"].de.Beispiele[0].Text, "Dateien") {
		t.Fatal("testdata veraendert")
	}
}
