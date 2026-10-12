// Package help liefert die Befehlshilfe aus den tldr-pages (CC BY 4.0,
// https://github.com/tldr-pages/tldr). Die Seiten werden offline aus den
// Release-Archiven gelesen: Deutsch zuerst, Englisch als Rueckfall.
package help

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Plattformen, die fuer SSH-Ziele zaehlen (Reihenfolge = Vorrang).
var plattformen = []string{"linux", "common", "osx", "freebsd", "openbsd", "netbsd", "cisco-ios"}

type Beispiel struct {
	Text   string `json:"text"`
	Befehl string `json:"befehl"` // mit {{Platzhaltern}}
}

type Seite struct {
	Name         string     `json:"name"`
	Plattform    string     `json:"plattform"`
	Sprache      string     `json:"sprache"`
	Beschreibung string     `json:"beschreibung"`
	Mehr         string     `json:"mehr,omitempty"`
	Beispiele    []Beispiel `json:"beispiele"`
	such         string
}

type Index struct {
	seiten map[string]*Seite // Name -> beste Seite
	namen  []string
	Quelle string
}

// Laden liest tldr-pages.de.zip und tldr-pages.en.zip aus verz. Fehlt beides, ist der Index leer.
func Laden(verz string) (*Index, error) {
	ix := &Index{seiten: map[string]*Seite{}}
	var gelesen []string
	for _, sprache := range []string{"de", "en"} {
		pfad := filepath.Join(verz, "tldr-pages."+sprache+".zip")
		if _, err := os.Stat(pfad); err != nil {
			continue
		}
		if err := ix.zipLesen(pfad, sprache); err != nil {
			return ix, fmt.Errorf("%s: %w", pfad, err)
		}
		gelesen = append(gelesen, sprache)
	}
	for n := range ix.seiten {
		ix.namen = append(ix.namen, n)
	}
	sort.Strings(ix.namen)
	ix.Quelle = strings.Join(gelesen, "+")
	return ix, nil
}

func rang(p string) int {
	for i, x := range plattformen {
		if x == p {
			return i
		}
	}
	return 99
}

func (ix *Index) zipLesen(pfad, sprache string) error {
	z, err := zip.OpenReader(pfad)
	if err != nil {
		return err
	}
	defer z.Close()
	for _, f := range z.File {
		teile := strings.Split(f.Name, "/")
		if len(teile) != 2 || !strings.HasSuffix(teile[1], ".md") || rang(teile[0]) == 99 {
			continue
		}
		name := strings.TrimSuffix(teile[1], ".md")
		if alt, ok := ix.seiten[name]; ok {
			// Deutsch schlaegt Englisch; innerhalb derselben Sprache gewinnt die Plattform mit Vorrang.
			if alt.Sprache == "de" && sprache != "de" {
				continue
			}
			if alt.Sprache == sprache && rang(alt.Plattform) <= rang(teile[0]) {
				continue
			}
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		b, err := io.ReadAll(io.LimitReader(r, 256*1024))
		r.Close()
		if err != nil {
			return err
		}
		s := Parsen(string(b))
		s.Name, s.Plattform, s.Sprache = name, teile[0], sprache
		s.such = normal(name + " " + s.Beschreibung + " " + beispielTexte(s))
		ix.seiten[name] = &s
	}
	return nil
}

func beispielTexte(s Seite) string {
	var b strings.Builder
	for _, e := range s.Beispiele {
		b.WriteString(e.Text)
		b.WriteByte(' ')
	}
	return b.String()
}

var mehrRe = regexp.MustCompile(`<(https?://[^>]+)>`)

// Parsen liest eine tldr-Seite im Markdown-Format.
func Parsen(md string) Seite {
	var s Seite
	var text string
	var beschr []string
	for _, zeile := range strings.Split(md, "\n") {
		zeile = strings.TrimSpace(zeile)
		switch {
		case strings.HasPrefix(zeile, "> "):
			z := strings.TrimPrefix(zeile, "> ")
			if m := mehrRe.FindStringSubmatch(z); m != nil {
				s.Mehr = m[1]
				continue
			}
			beschr = append(beschr, z)
		case strings.HasPrefix(zeile, "- "):
			text = strings.TrimSuffix(strings.TrimPrefix(zeile, "- "), ":")
		case strings.HasPrefix(zeile, "`") && strings.HasSuffix(zeile, "`") && len(zeile) > 1:
			s.Beispiele = append(s.Beispiele, Beispiel{Text: text, Befehl: zeile[1 : len(zeile)-1]})
		}
	}
	s.Beschreibung = strings.Join(beschr, " ")
	return s
}

func (ix *Index) Anzahl() int { return len(ix.seiten) }

// Seite liefert die Hilfe zu einem Befehl (auch zweiteilig wie "git commit").
func (ix *Index) Seite(name string) (Seite, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if s, ok := ix.seiten[strings.ReplaceAll(name, " ", "-")]; ok {
		return *s, true
	}
	return Seite{}, false
}

type Treffer struct {
	Name         string    `json:"name"`
	Beschreibung string    `json:"beschreibung"`
	Beispiel     *Beispiel `json:"beispiel,omitempty"`
	Punkte       int       `json:"-"`
}

// Suchen findet Befehle nach Name oder nach Worten aus Beschreibung und Beispielen.
// Ein Suchwort passt am WORTANFANG («finden» trifft nicht «befinden»).
func (ix *Index) Suchen(q string, max int) []Treffer {
	q = strings.TrimSpace(normal(q))
	worte := strings.Fields(q)
	if len(worte) == 0 {
		return nil
	}
	var out []Treffer
	for _, n := range ix.namen {
		s := ix.seiten[n]
		p := 0
		switch {
		case n == q || strings.ReplaceAll(n, "-", " ") == q:
			p = 1000
		case len(worte) == 1 && strings.HasPrefix(n, q):
			p = 500 - len(n)
		}
		kopf := wortTreffer(normal(n+" "+s.Beschreibung), worte)
		b, bn := bestesBeispiel(s, worte)
		// Ein Wort darf auch nur im Namen/der Beschreibung stehen, die anderen im Beispiel.
		gesamt := wortTreffer(s.such, worte)
		if p == 0 {
			noetig := len(worte)
			if noetig >= 3 {
				noetig--
			}
			if gesamt < noetig {
				continue
			}
		}
		p += bn*40 + kopf*15 + gesamt*5
		if gaengig[n] {
			p += 60
		}
		if s.Sprache == "de" {
			p += 5
		}
		t := Treffer{Name: n, Beschreibung: s.Beschreibung, Punkte: p}
		if p < 500 && b != nil {
			t.Beispiel = b
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Punkte != out[j].Punkte {
			return out[i].Punkte > out[j].Punkte
		}
		return len(out[i].Name) < len(out[j].Name)
	})
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// wortTreffer zaehlt die Suchworte, die (oder deren Synonyme) in text an einem Wortanfang stehen.
func wortTreffer(text string, worte []string) int {
	n := 0
	for _, w := range worte {
		if len(w) < 2 {
			continue
		}
		for _, a := range erweitern(w) {
			if strings.HasPrefix(text, a) || strings.Contains(text, " "+a) || strings.Contains(text, "-"+a) {
				n++
				break
			}
		}
	}
	return n
}

func bestesBeispiel(s *Seite, worte []string) (*Beispiel, int) {
	best, bestN := -1, 0
	for i, b := range s.Beispiele {
		if n := wortTreffer(normal(b.Text), worte); n > bestN {
			best, bestN = i, n
		}
	}
	if best < 0 {
		return nil, 0
	}
	b := s.Beispiele[best]
	return &b, bestN
}

// Teil ist ein Stueck der eingegebenen Befehlszeile mit Erklaerung.
type Teil struct {
	Wort       string `json:"wort"`
	Erklaerung string `json:"erklaerung"`
}

// Erklaeren ordnet die Schalter einer Befehlszeile den tldr-Beispielen zu, in
// denen sie vorkommen. Das ist eine Zuordnung, keine vollstaendige man-Seite.
func (ix *Index) Erklaeren(zeile string) (Seite, []Teil, bool) {
	// Bei Ketten (a && b | c; d) zaehlt der Befehl, an dem gerade getippt wird: der letzte.
	if i := strings.LastIndexAny(zeile, "|;&"); i >= 0 {
		zeile = zeile[i+1:]
	}
	worte := strings.Fields(zeile)
	if len(worte) == 0 {
		return Seite{}, nil, false
	}
	// Vorgestelltes sudo/env ueberspringen.
	for len(worte) > 1 && (worte[0] == "sudo" || worte[0] == "env" || worte[0] == "time") {
		worte = worte[1:]
	}
	s, ok := Seite{}, false
	start := 1
	if len(worte) > 1 {
		if s, ok = ix.Seite(worte[0] + " " + worte[1]); ok {
			start = 2
		}
	}
	if !ok {
		if s, ok = ix.Seite(worte[0]); !ok {
			return Seite{}, nil, false
		}
	}
	teile := []Teil{{Wort: strings.Join(worte[:start], " "), Erklaerung: s.Beschreibung}}
	for _, w := range worte[start:] {
		if !strings.HasPrefix(w, "-") || w == "-" || w == "--" {
			continue
		}
		e := schalterErklaeren(s, w)
		// Zusammengezogene Kurzschalter (-xzf) einzeln versuchen.
		if e == "" && !strings.HasPrefix(w, "--") && len(w) > 2 {
			var einzeln []string
			for _, r := range w[1:] {
				if x := schalterErklaeren(s, "-"+string(r)); x != "" {
					einzeln = append(einzeln, "-"+string(r)+": "+x)
				}
			}
			e = strings.Join(einzeln, " · ")
		}
		if e == "" {
			e = "nicht in den Beispielen — man " + worte[0]
		}
		teile = append(teile, Teil{Wort: w, Erklaerung: e})
	}
	return s, teile, true
}

var optionRe = regexp.MustCompile(`\{\{\[([^\]]+)\]\}\}`)

func schalterErklaeren(s Seite, schalter string) string {
	name := schalter
	if i := strings.IndexByte(name, '='); i > 0 {
		name = name[:i]
	}
	// Optionsplatzhalter {{[-C|--directory]}} nennen die andere Schreibweise — die sagt oft am meisten.
	for _, b := range s.Beispiele {
		for _, m := range optionRe.FindAllStringSubmatch(b.Befehl, -1) {
			formen := strings.Split(m[1], "|")
			for _, f := range formen {
				if f != name {
					continue
				}
				var andere []string
				for _, x := range formen {
					if x != name {
						andere = append(andere, x)
					}
				}
				if len(andere) > 0 {
					return strings.Join(andere, ", ") + " — Beispiel: " + b.Text
				}
				return b.Text
			}
		}
	}
	for _, b := range s.Beispiele {
		for _, tok := range befehlsWorte(b.Befehl) {
			if tok == name {
				return b.Text
			}
		}
	}
	return ""
}

// befehlsWorte zerlegt einen Beispielbefehl; Optionsplatzhalter {{[-C|--directory]}} liefern beide Formen.
func befehlsWorte(befehl string) []string {
	var out []string
	for _, m := range optionRe.FindAllStringSubmatch(befehl, -1) {
		out = append(out, strings.Split(m[1], "|")...)
	}
	rest := optionRe.ReplaceAllString(befehl, " ")
	for _, w := range strings.Fields(rest) {
		if i := strings.IndexByte(w, '='); i > 0 {
			w = w[:i]
		}
		out = append(out, w)
	}
	return out
}

// normal: Kleinschreibung, Umlaute vereinheitlicht (ss/ß, ä/ae ...), Satzzeichen weg.
func normal(s string) string {
	s = strings.ToLower(s)
	r := strings.NewReplacer("ß", "ss", "ä", "a", "ö", "o", "ü", "u", "ae", "a", "oe", "o", "ue", "u")
	s = r.Replace(s)
	return strings.Map(func(c rune) rune {
		if unicode.IsLetter(c) || unicode.IsDigit(c) || c == '-' || c == '.' || c == '+' {
			return c
		}
		return ' '
	}, s)
}
