// Package help liefert die Befehlshilfe aus den tldr-pages (CC BY 4.0,
// https://github.com/tldr-pages/tldr). Die Seiten werden offline aus den
// Release-Archiven gelesen. Der Index haelt je Befehl die deutsche und die
// englische Seite; jede Abfrage bekommt die Sprache der Oberflaeche und nimmt
// die Seite dieser Sprache, die andere Sprache ist der Rueckfall.
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

	"github.com/uiff/ssh-app/internal/texte"
)

// Plattformen, die fuer SSH-Ziele zaehlen (Reihenfolge = Vorrang).
var plattformen = []string{"linux", "common", "osx", "freebsd", "openbsd", "netbsd", "cisco-ios"}

// Sprachen der Archive tldr-pages.<sprache>.zip.
var sprachen = []string{texte.DE, texte.EN}

type Beispiel struct {
	Text   string `json:"text"`   // tldr-Merkhilfen wie «E[x]tract» bleiben drin, die Oberflaeche hebt sie hervor
	Befehl string `json:"befehl"` // mit {{Platzhaltern}}
}

type Seite struct {
	Name         string     `json:"name"`
	Plattform    string     `json:"plattform"`
	Sprache      string     `json:"sprache"`
	Beschreibung string     `json:"beschreibung"`
	Mehr         string     `json:"mehr,omitempty"`
	Beispiele    []Beispiel `json:"beispiele"`

	// Suchtexte, beim Laden einmal normalisiert.
	such         string   // Name, Beschreibung und alle Beispieltexte
	kopf         string   // Name und Beschreibung
	beispielSuch []string // je Beispiel
}

// paar haelt zu einem Befehl die beste Seite je Sprache (Plattform mit Vorrang).
type paar struct{ de, en *Seite }

func (p *paar) in(sprache string) *Seite {
	if sprache == texte.EN {
		return p.en
	}
	return p.de
}

func (p *paar) setzen(sprache string, s *Seite) {
	if sprache == texte.EN {
		p.en = s
	} else {
		p.de = s
	}
}

// waehlen liefert die Seite in der gewuenschten Sprache, sonst die der anderen.
// Ausnahme: Beschreibt die eigene Sprache nur macOS/BSD, die andere aber Linux
// oder alle Systeme, gewinnt die andere — auf SSH-Zielen zaehlt die Linux-Fassung.
func (p *paar) waehlen(sprache string) *Seite {
	eigen, fremd := p.in(sprache), p.in(andere(sprache))
	if eigen == nil {
		return fremd
	}
	if fremd != nil && rang(eigen.Plattform) > rang("common") && rang(fremd.Plattform) <= rang("common") {
		return fremd
	}
	return eigen
}

// reihenfolge: zuerst die gewaehlte Seite, dann die der anderen Sprache (kann nil sein).
func (p *paar) reihenfolge(sprache string) (erste, zweite *Seite) {
	erste = p.waehlen(sprache)
	if erste == p.de {
		return erste, p.en
	}
	return erste, p.de
}

func andere(sprache string) string {
	if sprache == texte.EN {
		return texte.DE
	}
	return texte.EN
}

type Index struct {
	seiten map[string]*paar // Name -> Seiten je Sprache
	namen  []string
	Quelle string
}

// Laden liest tldr-pages.de.zip und tldr-pages.en.zip aus verz. Fehlt beides, ist der Index leer.
func Laden(verz string) (*Index, error) {
	ix := &Index{seiten: map[string]*paar{}}
	var gelesen []string
	for _, sprache := range sprachen {
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
		p := ix.seiten[name]
		// Innerhalb derselben Sprache gewinnt die Plattform mit Vorrang.
		if p != nil {
			if alt := p.in(sprache); alt != nil && rang(alt.Plattform) <= rang(teile[0]) {
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
		s.kopf = suchText(name + " " + s.Beschreibung)
		s.such = suchText(name + " " + s.Beschreibung + " " + beispielTexte(s))
		for _, e := range s.Beispiele {
			s.beispielSuch = append(s.beispielSuch, suchText(e.Text))
		}
		if p == nil {
			p = &paar{}
			ix.seiten[name] = p
		}
		p.setzen(sprache, &s)
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

// Anzahl zaehlt die Befehle (eine oder zwei Sprachen je Befehl).
func (ix *Index) Anzahl() int { return len(ix.seiten) }

// AnzahlIn zaehlt die Befehle mit einer Seite in der Sprache.
func (ix *Index) AnzahlIn(sprache string) int {
	sprache = texte.Normal(sprache)
	n := 0
	for _, p := range ix.seiten {
		if p.in(sprache) != nil {
			n++
		}
	}
	return n
}

func seitenName(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), " ", "-")
}

// Seite liefert die Hilfe zu einem Befehl (auch zweiteilig wie "git commit"),
// in der gewuenschten Sprache oder — wenn es sie dort nicht gibt — in der anderen.
func (ix *Index) Seite(name, sprache string) (Seite, bool) {
	if p, ok := ix.seiten[seitenName(name)]; ok {
		if s := p.waehlen(texte.Normal(sprache)); s != nil {
			return *s, true
		}
	}
	return Seite{}, false
}

// SeiteGenau liefert die Seite genau in dieser Sprache (fuer einen Suchtreffer,
// der aus der anderen Sprache stammt).
func (ix *Index) SeiteGenau(name, sprache string) (Seite, bool) {
	if p, ok := ix.seiten[seitenName(name)]; ok {
		if s := p.in(texte.Normal(sprache)); s != nil {
			return *s, true
		}
	}
	return Seite{}, false
}

type Treffer struct {
	Name         string    `json:"name"`
	Sprache      string    `json:"sprache"` // Sprache der Seite, aus der Beschreibung und Beispiel stammen
	Beschreibung string    `json:"beschreibung"`
	Beispiel     *Beispiel `json:"beispiel,omitempty"`
	Punkte       int       `json:"-"`
}

// Seiten in der Sprache der Oberflaeche stehen vorne: bei gleicher Relevanz immer, und vor
// einer Seite der anderen Sprache, die nur um ein Beschreibungswort besser passt.
const sprachVorrang = 15

// Suchen findet Befehle nach Name oder nach Worten aus Beschreibung und Beispielen.
// Ein Suchwort passt am WORTANFANG («finden» trifft nicht «befinden»); Synonyme
// verbinden deutsche und englische Worte in beide Richtungen. Je Befehl zaehlt die
// Seite in der Sprache der Oberflaeche; die andere nur, wenn es sie nicht gibt oder
// sie nicht passt.
func (ix *Index) Suchen(q, sprache string, max int) []Treffer {
	sprache = texte.Normal(sprache)
	q = strings.TrimSpace(normal(q))
	worte := strings.Fields(q)
	if len(worte) == 0 {
		return nil
	}
	alt := make([][]suchform, len(worte))
	for i, w := range worte {
		if len(w) >= 2 {
			for _, a := range erweitern(w) {
				alt[i] = append(alt[i], suchform{a, " " + a, "-" + a})
			}
		}
	}
	var out []Treffer
	for _, n := range ix.namen {
		erste, zweite := ix.seiten[n].reihenfolge(sprache)
		t, ok := bewerten(n, erste, q, worte, alt, sprache)
		if !ok {
			// Passt nur die Seite der anderen Sprache (Synonyme, Uebersetzung), wird trotzdem
			// die gewaehlte Seite gezeigt: die andere Sprache ist nur Rueckfall, wenn es keine gibt.
			if t2, ok2 := bewerten(n, zweite, q, worte, alt, sprache); ok2 {
				t, ok = gezeigtAls(t2, erste, zweite, sprache), true
			}
		}
		if ok {
			out = append(out, t)
		}
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

// gezeigtAls: Treffer t wurde ueber die Seite gefunden, gezeigt wird aber erste (die Seite,
// die auch Seite() liefert). Punkte ohne/mit Sprachvorrang umrechnen; das Beispiel nur
// uebernehmen, wenn die Uebersetzung dieselben Beispiele in derselben Reihenfolge hat.
func gezeigtAls(t Treffer, erste, gefunden *Seite, sprache string) Treffer {
	if erste == nil || erste == gefunden {
		return t
	}
	p := t.Punkte
	if gefunden.Sprache == sprache {
		p -= sprachVorrang
	}
	if erste.Sprache == sprache {
		p += sprachVorrang
	}
	g := Treffer{Name: t.Name, Sprache: erste.Sprache, Beschreibung: erste.Beschreibung, Punkte: p}
	if t.Beispiel != nil && len(erste.Beispiele) == len(gefunden.Beispiele) {
		for i, b := range gefunden.Beispiele {
			if b == *t.Beispiel && erstesWort(erste.Beispiele[i].Befehl) == erstesWort(b.Befehl) {
				e := erste.Beispiele[i]
				g.Beispiel = &e
				break
			}
		}
	}
	return g
}

func erstesWort(befehl string) string {
	if f := strings.Fields(befehl); len(f) > 0 {
		return f[0]
	}
	return ""
}

// bewerten rechnet die Punkte einer Seite fuer die Suche; false = passt nicht.
func bewerten(n string, s *Seite, q string, worte []string, alt [][]suchform, sprache string) (Treffer, bool) {
	if s == nil {
		return Treffer{}, false
	}
	p := 0
	switch {
	case n == q || strings.ReplaceAll(n, "-", " ") == q:
		p = 1000
	case len(worte) == 1 && strings.HasPrefix(n, q):
		p = 500 - len(n)
	}
	// Ein Wort darf auch nur im Namen/der Beschreibung stehen, die anderen im Beispiel.
	gesamt := wortTreffer(s.such, alt)
	if p == 0 {
		noetig := len(worte)
		if noetig >= 3 {
			noetig--
		}
		if gesamt < noetig {
			return Treffer{}, false
		}
	}
	kopf := wortTreffer(s.kopf, alt)
	b, bn := bestesBeispiel(s, alt)
	p += bn*40 + kopf*15 + gesamt*5
	if gaengig[n] {
		p += 60
	}
	if s.Sprache == sprache {
		p += sprachVorrang
	}
	t := Treffer{Name: n, Sprache: s.Sprache, Beschreibung: s.Beschreibung, Punkte: p}
	if p < 500 && b != nil {
		t.Beispiel = b
	}
	return t, true
}

// suchform: ein Suchwort (oder Synonym) mit vorbereiteten Wortanfaengen.
type suchform struct{ wort, nachLeer, nachStrich string }

// wortTreffer zaehlt die Suchworte, die (oder deren Synonyme) in text an einem Wortanfang stehen.
// alt enthaelt je Suchwort die Alternativen aus erweitern (nil = zu kurz, zaehlt nicht).
func wortTreffer(text string, alt [][]suchform) int {
	n := 0
	for _, a := range alt {
		for _, x := range a {
			if strings.HasPrefix(text, x.wort) || strings.Contains(text, x.nachLeer) || strings.Contains(text, x.nachStrich) {
				n++
				break
			}
		}
	}
	return n
}

func bestesBeispiel(s *Seite, alt [][]suchform) (*Beispiel, int) {
	best, bestN := -1, 0
	for i, t := range s.beispielSuch {
		if n := wortTreffer(t, alt); n > bestN {
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
// Seite und Beispiele kommen in der gewuenschten Sprache (Rueckfall: die andere),
// die eigenen Hinweise immer in der Sprache der Oberflaeche.
func (ix *Index) Erklaeren(zeile, sprache string) (Seite, []Teil, bool) {
	sprache = texte.Normal(sprache)
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
		if s, ok = ix.Seite(worte[0]+" "+worte[1], sprache); ok {
			start = 2
		}
	}
	if !ok {
		if s, ok = ix.Seite(worte[0], sprache); !ok {
			return Seite{}, nil, false
		}
	}
	teile := []Teil{{Wort: strings.Join(worte[:start], " "), Erklaerung: s.Beschreibung}}
	for _, w := range worte[start:] {
		if !strings.HasPrefix(w, "-") || w == "-" || w == "--" {
			continue
		}
		e := schalterErklaeren(s, w, sprache)
		// Zusammengezogene Kurzschalter (-xzf) einzeln versuchen.
		if e == "" && !strings.HasPrefix(w, "--") && len(w) > 2 {
			var einzeln []string
			for _, r := range w[1:] {
				if x := schalterErklaeren(s, "-"+string(r), sprache); x != "" {
					einzeln = append(einzeln, "-"+string(r)+": "+x)
				}
			}
			e = strings.Join(einzeln, " · ")
		}
		if e == "" {
			e = texte.T(sprache, "hilfe.schalter.unbekannt", worte[0])
		}
		teile = append(teile, Teil{Wort: w, Erklaerung: e})
	}
	return s, teile, true
}

var optionRe = regexp.MustCompile(`\{\{\[([^\]]+)\]\}\}`)

func schalterErklaeren(s Seite, schalter, sprache string) string {
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
					return texte.T(sprache, "hilfe.schalter.auch", strings.Join(andere, ", "), b.Text)
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

var (
	umlaute = strings.NewReplacer("ß", "ss", "ä", "a", "ö", "o", "ü", "u", "ae", "a", "oe", "o", "ue", "u")
	// tldr-Merkhilfen («E[x]tract», «[c]reate») stuenden sonst der Wortsuche im Weg.
	merkhilfen = strings.NewReplacer("[", "", "]", "")
)

// normal: Kleinschreibung, Umlaute vereinheitlicht (ss/ß, ä/ae ...), Satzzeichen weg.
func normal(s string) string {
	s = umlaute.Replace(strings.ToLower(s))
	return strings.Map(func(c rune) rune {
		if unicode.IsLetter(c) || unicode.IsDigit(c) || c == '-' || c == '.' || c == '+' {
			return c
		}
		return ' '
	}, s)
}

// suchText bereitet einen Seitentext fuer die Wortsuche auf.
func suchText(s string) string { return normal(merkhilfen.Replace(s)) }
