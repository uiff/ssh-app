package web

// Waechter fuer die Zweisprachigkeit der Oberflaeche (Deutsch/Englisch).
//
// Die Kataloge js/sprache/<bereich>.js werden hier ohne Node gelesen: ein kleiner
// JavaScript-Zerleger liefert Zeichenketten (Escapes aufgeloest), Namen und Satzzeichen.
// Kommentare fallen weg, Regex-Literale werden uebersprungen (sonst stoeren Anfuehrungs-
// zeichen oder Backticks darin).

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

type jsToken struct {
	art   byte // 's' Zeichenkette, 'i' Name oder Zahl, 'p' Satzzeichen
	wert  string
	zeile int
}

// Nach diesen Namen beginnt mit «/» ein Regex-Literal, keine Division.
var vorRegex = map[string]bool{"return": true, "typeof": true, "case": true, "in": true, "of": true, "new": true,
	"delete": true, "void": true, "throw": true, "instanceof": true, "else": true, "do": true, "yield": true, "await": true}

func istNamenZeichen(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func jsZerlegen(src string) ([]jsToken, error) {
	var aus []jsToken
	zeile := 1
	i := 0
	regexErlaubt := func() bool {
		if len(aus) == 0 {
			return true
		}
		v := aus[len(aus)-1]
		switch v.art {
		case 'p':
			return v.wert != ")" && v.wert != "]" && v.wert != "}"
		case 'i':
			return vorRegex[v.wert]
		}
		return false
	}
	for i < len(src) {
		c := src[i]
		switch {
		case c == '\n':
			zeile++
			i++
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case strings.HasPrefix(src[i:], "//"):
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case strings.HasPrefix(src[i:], "/*"):
			ende := strings.Index(src[i+2:], "*/")
			if ende < 0 {
				return nil, fmt.Errorf("Zeile %d: Kommentar ohne Ende", zeile)
			}
			zeile += strings.Count(src[i:i+2+ende], "\n")
			i += ende + 4
		case c == '/' && regexErlaubt():
			start := zeile
			i++
			klasse := false
			for {
				if i >= len(src) || src[i] == '\n' {
					return nil, fmt.Errorf("Zeile %d: Regex ohne Ende", start)
				}
				d := src[i]
				if d == '\\' {
					i += 2
					continue
				}
				i++
				if d == '[' {
					klasse = true
				} else if d == ']' {
					klasse = false
				} else if d == '/' && !klasse {
					break
				}
			}
			for i < len(src) && istNamenZeichen(rune(src[i])) {
				i++ // Flags
			}
			aus = append(aus, jsToken{art: 'i', wert: "/regex/", zeile: start})
		case c == '\'' || c == '"' || c == '`':
			start := zeile
			var b strings.Builder
			i++
			for {
				if i >= len(src) {
					return nil, fmt.Errorf("Zeile %d: Zeichenkette ohne Ende", start)
				}
				d := src[i]
				if d == c {
					i++
					break
				}
				if d == '\n' {
					if c != '`' {
						return nil, fmt.Errorf("Zeile %d: Zeilenumbruch in Zeichenkette", start)
					}
					zeile++
				}
				if c == '`' && strings.HasPrefix(src[i:], "${") {
					// Ausdruck im Template: bis zur passenden Klammer ueberspringen.
					tiefe := 0
					for i < len(src) {
						if q := src[i]; q == '\'' || q == '"' {
							for i++; i < len(src) && src[i] != q; i++ {
								if src[i] == '\\' {
									i++
								}
							}
						} else if src[i] == '{' {
							tiefe++
						} else if src[i] == '}' {
							tiefe--
							if tiefe == 0 {
								i++
								break
							}
						} else if src[i] == '\n' {
							zeile++
						}
						i++
					}
					b.WriteString("${}")
					continue
				}
				if d != '\\' {
					b.WriteByte(d)
					i++
					continue
				}
				if i+1 >= len(src) {
					return nil, fmt.Errorf("Zeile %d: Escape am Ende", start)
				}
				e := src[i+1]
				i += 2
				switch e {
				case 'n':
					b.WriteByte('\n')
				case 't':
					b.WriteByte('\t')
				case 'r', 'b', 'f', 'v', '0':
				case '\n':
					zeile++ // Zeilenfortsetzung
				case 'x':
					if i+2 <= len(src) {
						if n, err := strconv.ParseUint(src[i:i+2], 16, 8); err == nil {
							b.WriteRune(rune(n))
							i += 2
						}
					}
				case 'u':
					hex := ""
					if i < len(src) && src[i] == '{' {
						ende := strings.IndexByte(src[i:], '}')
						if ende > 0 {
							hex = src[i+1 : i+ende]
							i += ende + 1
						}
					} else if i+4 <= len(src) {
						hex = src[i : i+4]
						i += 4
					}
					if n, err := strconv.ParseUint(hex, 16, 32); err == nil {
						b.WriteRune(rune(n))
					}
				default:
					b.WriteByte(e)
				}
			}
			aus = append(aus, jsToken{art: 's', wert: b.String(), zeile: start})
		default:
			r, n := utf8.DecodeRuneInString(src[i:])
			if istNamenZeichen(r) {
				j := i
				for j < len(src) {
					r2, n2 := utf8.DecodeRuneInString(src[j:])
					if !istNamenZeichen(r2) {
						break
					}
					j += n2
				}
				aus = append(aus, jsToken{art: 'i', wert: src[i:j], zeile: zeile})
				i = j
				continue
			}
			aus = append(aus, jsToken{art: 'p', wert: string(r), zeile: zeile})
			i += n
		}
	}
	return aus, nil
}

// --- Kataloge ---

type katalogEintrag struct {
	wert  string
	datei string
	zeile int
}

type katalog map[string]map[string]katalogEintrag // sprache -> schluessel -> eintrag

func (k katalog) hat(sprache, schluessel string) bool { _, ok := k[sprache][schluessel]; return ok }

// katalogDatei liest i18nErgaenzen({ de: {'k': 'v', …}, en: {…} }) aus einer Katalogdatei.
func katalogDatei(datei string, toks []jsToken) (map[string]map[string]katalogEintrag, error) {
	aus := map[string]map[string]katalogEintrag{}
	i := 0
	for i < len(toks) && !(toks[i].art == 'i' && toks[i].wert == "i18nErgaenzen") {
		i++
	}
	erwarte := func(art byte, wert string) error {
		if i >= len(toks) || toks[i].art != art || (wert != "" && toks[i].wert != wert) {
			z := 0
			if i < len(toks) {
				z = toks[i].zeile
			}
			return fmt.Errorf("%s:%d: %q erwartet", datei, z, wert)
		}
		i++
		return nil
	}
	if err := erwarte('i', "i18nErgaenzen"); err != nil {
		return nil, err
	}
	if err := erwarte('p', "("); err != nil {
		return nil, err
	}
	if err := erwarte('p', "{"); err != nil {
		return nil, err
	}
	for i < len(toks) && !(toks[i].art == 'p' && toks[i].wert == "}") {
		if i >= len(toks) || toks[i].art != 'i' || (toks[i].wert != "de" && toks[i].wert != "en") {
			return nil, fmt.Errorf("%s:%d: «de:» oder «en:» erwartet", datei, toks[i].zeile)
		}
		sprache := toks[i].wert
		i++
		if aus[sprache] != nil {
			return nil, fmt.Errorf("%s: Sprache %s doppelt", datei, sprache)
		}
		aus[sprache] = map[string]katalogEintrag{}
		if err := erwarte('p', ":"); err != nil {
			return nil, err
		}
		if err := erwarte('p', "{"); err != nil {
			return nil, err
		}
		for i < len(toks) && !(toks[i].art == 'p' && toks[i].wert == "}") {
			k := toks[i]
			if err := erwarte('s', ""); err != nil {
				return nil, err
			}
			if err := erwarte('p', ":"); err != nil {
				return nil, err
			}
			v := toks[i]
			if err := erwarte('s', ""); err != nil {
				return nil, err
			}
			wert := v.wert
			for i+1 < len(toks) && toks[i].art == 'p' && toks[i].wert == "+" && toks[i+1].art == 's' {
				wert += toks[i+1].wert
				i += 2
			}
			if _, doppelt := aus[sprache][k.wert]; doppelt {
				return nil, fmt.Errorf("%s:%d: Schluessel %s doppelt (%s)", datei, k.zeile, k.wert, sprache)
			}
			aus[sprache][k.wert] = katalogEintrag{wert: wert, datei: datei, zeile: k.zeile}
			if i < len(toks) && toks[i].art == 'p' && toks[i].wert == "," {
				i++
			}
		}
		if err := erwarte('p', "}"); err != nil {
			return nil, err
		}
		if i < len(toks) && toks[i].art == 'p' && toks[i].wert == "," {
			i++
		}
	}
	return aus, nil
}

func dateiZerlegen(t *testing.T, name string) []jsToken {
	t.Helper()
	b, err := fs.ReadFile(Static, name)
	if err != nil {
		t.Fatal(err)
	}
	toks, err := jsZerlegen(string(b))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return toks
}

func katalogeLesen(t *testing.T) katalog {
	t.Helper()
	dateien, err := fs.Glob(Static, "static/js/sprache/*.js")
	if err != nil || len(dateien) == 0 {
		t.Fatalf("keine Kataloge: %v", err)
	}
	k := katalog{"de": {}, "en": {}}
	for _, d := range dateien {
		teil, err := katalogDatei(d, dateiZerlegen(t, d))
		if err != nil {
			t.Fatal(err)
		}
		praefix := strings.TrimSuffix(path.Base(d), ".js") + "."
		for _, l := range []string{"de", "en"} {
			if teil[l] == nil {
				t.Errorf("%s: Sprache %s fehlt", d, l)
			}
			for s, e := range teil[l] {
				if !strings.HasPrefix(s, praefix) {
					t.Errorf("%s:%d: Schluessel %s ohne Praefix %s", d, e.zeile, s, praefix)
				}
				if alt, doppelt := k[l][s]; doppelt {
					t.Errorf("Schluessel %s doppelt: %s:%d und %s:%d", s, alt.datei, alt.zeile, d, e.zeile)
				}
				k[l][s] = e
			}
		}
	}
	return k
}

var (
	platzhalterRe = regexp.MustCompile(`\{(\w+)\}`)
	schluesselRe  = regexp.MustCompile(`^(basis|app|terminal|dateien|hilfe|hosts)\.[A-Za-z0-9_.]*$`)
	umlautRe      = regexp.MustCompile(`[äöüÄÖÜß]`)
)

func platzhalterVon(s string) string {
	var p []string
	for _, m := range platzhalterRe.FindAllStringSubmatch(s, -1) {
		p = append(p, m[1])
	}
	sort.Strings(p)
	return strings.Join(slicesKompakt(p), ",")
}

func slicesKompakt(s []string) []string {
	var aus []string
	for i, x := range s {
		if i == 0 || x != s[i-1] {
			aus = append(aus, x)
		}
	}
	return aus
}

// Jeder Schluessel steht in beiden Sprachen mit denselben {platzhaltern}; Deutsch (Schweiz)
// ohne «ß», Englisch ohne Umlaute und ohne Schweizer Anfuehrungszeichen.
func TestSprachkatalogeVollstaendig(t *testing.T) {
	k := katalogeLesen(t)
	if len(k["de"]) < 100 {
		t.Fatalf("nur %d Schluessel gelesen", len(k["de"]))
	}
	for s, de := range k["de"] {
		en, ok := k["en"][s]
		if !ok {
			t.Errorf("%s:%d: englischer Text fehlt: %s", de.datei, de.zeile, s)
			continue
		}
		if strings.TrimSpace(de.wert) == "" || strings.TrimSpace(en.wert) == "" {
			t.Errorf("%s: leerer Text", s)
		}
		if platzhalterVon(de.wert) != platzhalterVon(en.wert) {
			t.Errorf("%s: Platzhalter verschieden: %q / %q", s, de.wert, en.wert)
		}
		if strings.Contains(de.wert, "ß") {
			t.Errorf("%s: «ß» im deutschen Text (Schweiz: «ss»): %q", s, de.wert)
		}
		if umlautRe.MatchString(en.wert) || strings.ContainsAny(en.wert, "«»") {
			t.Errorf("%s: deutsche Zeichen im englischen Text: %q", s, en.wert)
		}
	}
	for s, en := range k["en"] {
		if !k.hat("de", s) {
			t.Errorf("%s:%d: deutscher Text fehlt: %s", en.datei, en.zeile, s)
		}
	}
	// index.html laedt jeden Katalog, und zwar nach i18n.js und vor dem ersten Programmteil.
	seite, err := fs.ReadFile(Static, "static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(seite)
	basis, erstesProgramm := strings.Index(html, `src="js/i18n.js"`), strings.Index(html, `src="js/thema.js"`)
	dateien, _ := fs.Glob(Static, "static/js/sprache/*.js")
	for _, d := range dateien {
		p := strings.Index(html, `src="js/sprache/`+path.Base(d)+`"`)
		if p < 0 || p < basis || p > erstesProgramm {
			t.Errorf("index.html laedt %s nicht (oder nicht zwischen i18n.js und thema.js)", path.Base(d))
		}
	}
}

// Jeder Schluessel, den der Browser-Code nennt (t('…'), tTeile('…'), auch in Bedingungen),
// steht in beiden Sprachen im Katalog; jeder Katalogschluessel wird benutzt.
func TestTextSchluesselImKatalog(t *testing.T) {
	k := katalogeLesen(t)
	dateien, err := fs.Glob(Static, "static/js/*.js")
	if err != nil || len(dateien) == 0 {
		t.Fatalf("keine Programmdateien: %v", err)
	}
	type fund struct {
		datei string
		zeile int
	}
	benutzt := map[string]fund{}
	praefixe := map[string]fund{}
	pruefen := func(datei string, toks []jsToken) {
		for _, tk := range toks {
			if tk.art != 's' || !schluesselRe.MatchString(tk.wert) {
				continue
			}
			if strings.HasSuffix(tk.wert, ".") {
				praefixe[tk.wert] = fund{datei, tk.zeile}
			} else {
				benutzt[tk.wert] = fund{datei, tk.zeile}
			}
		}
	}
	for _, d := range dateien {
		pruefen(d, dateiZerlegen(t, d))
	}
	for s, f := range benutzt {
		for _, l := range []string{"de", "en"} {
			if !k.hat(l, s) {
				t.Errorf("%s:%d: Schluessel %s fehlt im Katalog (%s)", f.datei, f.zeile, s, l)
			}
		}
	}
	for p, f := range praefixe {
		n := 0
		for s := range k["de"] {
			if strings.HasPrefix(s, p) {
				n++
			}
		}
		if n == 0 {
			t.Errorf("%s:%d: kein Katalogschluessel zum zusammengesetzten Praefix %s", f.datei, f.zeile, p)
		}
	}
	for s, e := range k["de"] {
		if _, ok := benutzt[s]; ok {
			continue
		}
		dynamisch := false
		for p := range praefixe {
			if strings.HasPrefix(s, p) {
				dynamisch = true
			}
		}
		if !dynamisch {
			t.Errorf("%s:%d: Schluessel %s wird nirgends benutzt", e.datei, e.zeile, s)
		}
	}
	// Der Klicktest klickt ueber Katalogschluessel (klickT): auch die muessen stimmen.
	if b, err := os.ReadFile("../tools/klicktest.js"); err == nil {
		toks, err := jsZerlegen(string(b))
		if err != nil {
			t.Fatalf("tools/klicktest.js: %v", err)
		}
		for _, tk := range toks {
			if tk.art == 's' && schluesselRe.MatchString(tk.wert) && !strings.HasSuffix(tk.wert, ".") && !k.hat("de", tk.wert) {
				t.Errorf("tools/klicktest.js:%d: Schluessel %s fehlt im Katalog", tk.zeile, tk.wert)
			}
		}
	}
}

// Woerter, die nur im Deutschen vorkommen. Grossgeschrieben: Nomen und Satzanfaenge
// (eine Zeichenkette mit kleinem Einzelwort wie 'ordner' ist ein Bezeichner, kein Text).
var deutscheWoerter = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`und oder nicht kein keine keinen ist sind wird werden wurde mit ohne für fuer
		auf aus bei nach vom zum zur der die das den dem des ein eine einen einem einer bitte noch schon jetzt
		hier dort alle alles erst über ueber zurück zurueck wieder gibt kann muss soll sich wenn dann weil auch
		nur mehr neu verbinden speichern löschen schliessen abbrechen hochladen herunterladen einfügen kopieren
		anmelden abmelden wählen`) {
		deutscheWoerter[w] = true
		deutscheWoerter[strings.ToUpper(w[:1])+w[1:]] = true
	}
	for _, w := range strings.Fields(`Sitzung Sitzungen Verbindung Fehler Datei Dateien Ordner Passwort Schlüssel
		Schluessel Tresor Zugang Zugänge Zugaenge Zugangsdaten Einstellungen Gerät Geraet Geräte Benutzer Befehl
		Befehle Befehlshilfe Suchen Sprache Strg Umschalt Pos1 Ende Bild Erweitert Akzentfarbe Fingerabdruck
		Sicherheitsschlüssel Hinzufügen Abbrechen Weiter Zurück Speichern Löschen Hilfe Verbinden Neu`) {
		deutscheWoerter[w] = true
	}
}

var wortTrenner = regexp.MustCompile(`[^\p{L}\p{N}-]+`)

// deutscherText meldet, ob eine Zeichenkette sichtbarer deutscher Text ist.
func deutscherText(s string) string {
	buchstaben := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			buchstaben++
		}
	}
	if buchstaben < 2 {
		return "" // Satzzeichen, Prompt-Zeichen wie '$#%❯»'
	}
	if m := umlautRe.FindString(s); m != "" {
		return "Umlaut «" + m + "»"
	}
	if strings.ContainsAny(s, "«»") {
		return "Schweizer Anfuehrungszeichen"
	}
	// Nur prosaaehnliche Zeichenketten: mit Leerzeichen oder gross beginnend.
	erstes, _ := utf8.DecodeRuneInString(s)
	if !strings.ContainsAny(strings.TrimSpace(s), "  ") && !unicode.IsUpper(erstes) {
		return ""
	}
	for _, w := range wortTrenner.Split(s, -1) {
		if deutscheWoerter[w] {
			return "Wort «" + w + "»"
		}
	}
	return ""
}

// Kein sichtbarer deutscher Rohtext im Browser-Code: alles kommt aus den Katalogen.
// Ausnahmen: Werte von «de:» in zweisprachigen Tabellen (geraete.js, thema.js), Kommentare.
func TestKeineRohtexteImBrowserCode(t *testing.T) {
	dateien, err := fs.Glob(Static, "static/js/*.js")
	if err != nil || len(dateien) == 0 {
		t.Fatalf("keine Programmdateien: %v", err)
	}
	// Selbsttest der Erkennung
	for _, s := range []string{"Neu verbinden", "Datei wählen", "«x» fehlt", "Keine Sitzung", "Fehler"} {
		if deutscherText(s) == "" {
			t.Fatalf("Erkennung verfehlt %q", s)
		}
	}
	for _, s := range []string{"ordner", "btn klein primaer", "$#%❯»", "Reconnect", "SSH-App", "IoTUeli — iotueli.ch", "data-mod"} {
		if g := deutscherText(s); g != "" {
			t.Fatalf("Fehlalarm bei %q: %s", s, g)
		}
	}
	for _, d := range dateien {
		toks := dateiZerlegen(t, d)
		for i, tk := range toks {
			if tk.art != 's' {
				continue
			}
			if i >= 2 && toks[i-1].art == 'p' && toks[i-1].wert == ":" && toks[i-2].art == 'i' && toks[i-2].wert == "de" {
				continue // zweisprachige Tabelle { de: '…', en: '…' }
			}
			if grund := deutscherText(tk.wert); grund != "" {
				t.Errorf("%s:%d: deutscher Text ausserhalb des Katalogs (%s): %q", d, tk.zeile, grund, tk.wert)
			}
		}
	}
}

// Zweisprachige Tabellen (Geraetetypen, Akzentfarben): jeder Eintrag hat de und en.
func TestZweisprachigeTabellen(t *testing.T) {
	for _, d := range []string{"static/js/geraete.js", "static/js/thema.js"} {
		toks := dateiZerlegen(t, d)
		de, en := 0, 0
		for i := 0; i+2 < len(toks); i++ {
			if toks[i].art == 'i' && toks[i+1].wert == ":" && toks[i+2].art == 's' {
				switch toks[i].wert {
				case "de":
					de++
					if strings.Contains(toks[i+2].wert, "ß") {
						t.Errorf("%s:%d: «ß» (Schweiz: «ss»)", d, toks[i].zeile)
					}
				case "en":
					en++
					if umlautRe.MatchString(toks[i+2].wert) {
						t.Errorf("%s:%d: Umlaut im englischen Namen %q", d, toks[i].zeile, toks[i+2].wert)
					}
				}
			}
		}
		if de == 0 || de != en {
			t.Errorf("%s: %d deutsche, %d englische Namen", d, de, en)
		}
	}
}

// Keine Emoji in der Oberflaeche (nur Strich-Symbole als SVG).
func TestKeineEmoji(t *testing.T) {
	err := fs.WalkDir(Static, "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(p, "static/vendor/") {
			return err
		}
		switch path.Ext(p) {
		case ".js", ".css", ".html", ".webmanifest", ".svg":
		default:
			return nil
		}
		b, err := fs.ReadFile(Static, p)
		if err != nil {
			return err
		}
		for n, z := range strings.Split(string(b), "\n") {
			for _, r := range z {
				if (r >= 0x1F000 && r <= 0x1FAFF) || (r >= 0x2600 && r <= 0x26FF) || (r >= 0x2700 && r <= 0x2767) || r == 0xFE0F {
					t.Errorf("%s:%d: Emoji %U", p, n+1, r)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Der Zerleger selbst: Anfuehrungszeichen in Regex-Literalen und Kommentaren stoeren nicht.
func TestJsZerlegen(t *testing.T) {
	toks, err := jsZerlegen("a = s.split('`'); b = x.replace(/`/g, '') // 'nein'\n/* \"auch nicht\" */ c = n / 2 / 3; d = \"it's\" + 'Zeile\\n'; e = `t ${x + '}'} u`;")
	if err != nil {
		t.Fatal(err)
	}
	var s []string
	for _, tk := range toks {
		if tk.art == 's' {
			s = append(s, tk.wert)
		}
	}
	if fmt.Sprint(s) != fmt.Sprint([]string{"`", "", "it's", "Zeile\n", "t ${} u"}) {
		t.Fatalf("Zeichenketten: %q", s)
	}
}
