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
	"unicode/utf8"

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

// deutlichBesser: So viel mehr Punkte (ein weiteres Suchwort im Beispiel) braucht die Seite
// der anderen Sprache, damit ihr Treffer statt dem der eigenen Seite zaehlt.
const deutlichBesser = 40

// kernBonus: Punkte je Suchwort, das einen der gaengigen Befehle meint (kernbefehle).
const kernBonus = 50

// namensVorrang: Vorsprung der eigenen Sprache, wenn nur der Name mit dem Suchwort beginnt.
const namensVorrang = 15

// Suchen findet Befehle nach Name oder nach Worten aus Beschreibung und Beispielen.
// Ein Suchwort passt am WORTANFANG («finden» trifft nicht «befinden»); Synonyme
// verbinden deutsche und englische Worte in beide Richtungen, Fuellwoerter («in», «the»)
// zaehlen nicht. Alltagsworte wie «löschen» oder «neu starten» heben die gaengigen Befehle
// (rm, systemctl) vor Seiten, die das Wort nur nebenbei enthalten.
//
// Je Befehl zaehlt die Seite in der Sprache der Oberflaeche. Passt sie nicht oder deutlich
// schlechter als die andere, gilt der Treffer der anderen: gezeigt wird dann trotzdem die
// eigene Seite mit dem entsprechenden Beispiel — oder, wenn sich das Beispiel nicht
// zuordnen laesst und die andere Seite die englische ist, die englische Seite selbst.
// Die Sprache entscheidet nur bei gleich guten Treffern.
func (ix *Index) Suchen(q, sprache string, max int) []Treffer {
	sprache = texte.Normal(sprache)
	q = wendungen(strings.TrimSpace(normal(q)))
	worte := ohneFuellwoerter(strings.Fields(q))
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
	bonus := kernFuer(worte)
	var out []Treffer
	for _, n := range ix.namen {
		erste, zweite := ix.seiten[n].reihenfolge(sprache)
		t1, ok1 := bewerten(n, erste, q, worte, alt)
		t2, ok2 := bewerten(n, zweite, q, worte, alt)
		var t Treffer
		switch {
		case ok1 && (!ok2 || t2.Punkte < t1.Punkte+deutlichBesser):
			t = t1
		case ok2:
			g, ganz := abbilden(t2, erste, zweite)
			switch {
			case ganz:
				t = g
			case zweite.Sprache == texte.EN:
				// Die englische Seite ist das Original; der Uebersetzung fehlt das passende Beispiel.
				t = t2
			case ok1:
				t = t1
			default:
				t = g
			}
		default:
			continue
		}
		t.Punkte += bonus[n]
		if len(worte) == 1 && n != q && strings.HasPrefix(n, q) && t.Sprache == sprache {
			// Namensanfang («git» → git-log, gitk): kein Inhaltstreffer, die Laenge sagt wenig —
			// hier geht die eigene Sprache vor.
			t.Punkte += namensVorrang
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Punkte != out[j].Punkte {
			return out[i].Punkte > out[j].Punkte
		}
		if ei, ej := out[i].Sprache == sprache, out[j].Sprache == sprache; ei != ej {
			return ei
		}
		return len(out[i].Name) < len(out[j].Name)
	})
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// abbilden: Treffer t stammt aus der Seite «gefunden», gezeigt werden soll «erste»
// (dieselbe Hilfe in der anderen Sprache). Das Beispiel geht mit, wenn die Uebersetzung es an
// derselben Stelle (oder eindeutig woanders) mit demselben Befehlsgeruest hat. ganz=false:
// Es gab ein passendes Beispiel, aber keine Entsprechung.
func abbilden(t Treffer, erste, gefunden *Seite) (g Treffer, ganz bool) {
	if erste == nil || erste == gefunden {
		return t, true
	}
	g = Treffer{Name: t.Name, Sprache: erste.Sprache, Beschreibung: erste.Beschreibung, Punkte: t.Punkte}
	if t.Beispiel == nil {
		return g, true
	}
	for i, b := range gefunden.Beispiele {
		if b == *t.Beispiel {
			if k := entsprechung(erste, gefunden, i); k >= 0 {
				e := erste.Beispiele[k]
				g.Beispiel = &e
				return g, true
			}
			break
		}
	}
	return g, false
}

var platzhalterRe = regexp.MustCompile(`\{\{.*?\}\}`)

// geruest: der Befehl ohne Platzhalter, Optionsplatzhalter {{[-C|--directory]}} als «-C|--directory».
// Die Uebersetzungen einer Seite haben dasselbe Geruest, nur die Platzhalter sind uebersetzt.
func geruest(befehl string) string {
	return strings.Join(strings.Fields(platzhalterRe.ReplaceAllString(optionRe.ReplaceAllString(befehl, "$1"), " ")), " ")
}

// entsprechung liefert zum Beispiel i der Seite von das Beispiel der Seite nach mit demselben
// Geruest: an derselben Stelle oder eindeutig an einer anderen; -1, wenn es keines gibt.
func entsprechung(nach, von *Seite, i int) int {
	if nach == nil || von == nil || i < 0 || i >= len(von.Beispiele) {
		return -1
	}
	g := geruest(von.Beispiele[i].Befehl)
	if i < len(nach.Beispiele) && geruest(nach.Beispiele[i].Befehl) == g {
		return i
	}
	k := -1
	for j, b := range nach.Beispiele {
		if geruest(b.Befehl) == g {
			if k >= 0 {
				return -1
			}
			k = j
		}
	}
	return k
}

// bewerten rechnet die Punkte einer Seite fuer die Suche; false = passt nicht.
func bewerten(n string, s *Seite, q string, worte []string, alt [][]suchform) (Treffer, bool) {
	if s == nil {
		return Treffer{}, false
	}
	p := 0
	switch {
	case n == q || strings.ReplaceAll(n, "-", " ") == q:
		p = 1000
	case len(worte) == 1 && strings.HasPrefix(n, q):
		p = 500 - len(n)
		if strings.HasPrefix(n, q+"-") {
			p += 10 // Unterbefehle («git-log») vor aehnlichen Namen («gita»)
		}
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
//
// Zusammengezogene Kurzschalter («tar -xzf», ohne Strich «tar xzf») werden einzeln
// erklaert: ueber die Merkhilfen der tldr-Seiten («E[x]tract», «g[z]ipped», «[f]ile»),
// die zu Beispielen mit diesem Buchstaben im Schalterbuendel gehoeren («tar czf»).
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
	var p *paar
	start := 1
	if len(worte) > 1 {
		if p = ix.seiten[seitenName(worte[0]+" "+worte[1])]; p != nil {
			start = 2
		}
	}
	if p == nil {
		if p = ix.seiten[seitenName(worte[0])]; p == nil {
			return Seite{}, nil, false
		}
	}
	s, andere := p.reihenfolge(sprache)
	teile := []Teil{{Wort: strings.Join(worte[:start], " "), Erklaerung: s.Beschreibung}}
	for i, w := range worte[start:] {
		var e string
		switch {
		case strings.HasPrefix(w, "-") && w != "-" && w != "--":
			e = schalterErklaeren(*s, w, sprache)
			switch {
			case e != "":
			case !strings.HasPrefix(w, "--") && kurzschalter(w[1:]):
				e = buendelErklaeren(s, andere, w[1:], "-", worte[0], sprache)
			case andere != nil:
				// Langer Schalter, den nur die Seite der anderen Sprache kennt («rm --force»).
				if x := schalterErklaeren(*andere, w, sprache); x != "" {
					e = vonSeite(sprache, andere, x)
				}
			}
			if e == "" {
				e = texte.T(sprache, "hilfe.schalter.unbekannt", worte[0])
			}
		case i == 0 && kurzschalter(w):
			// Erstes Wort ohne Strich: Schalterbuendel wie «tar xzf», «ps aux» oder ein Unterbefehl.
			if e = ohneStrichErklaeren(s, andere, w, worte[0], sprache); e == "" {
				continue
			}
		default:
			continue
		}
		teile = append(teile, Teil{Wort: w, Erklaerung: e})
	}
	return *s, teile, true
}

// kurzschalter: nur Buchstaben und Ziffern (Inhalt eines Buendels wie «xzf»).
func kurzschalter(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// ohneStrich liefert das Wort direkt nach dem Befehlsnamen, wenn es nur aus Buchstaben
// besteht («tar czf …» → «czf», «sudo apt install …» → «install»).
func ohneStrich(befehl, name string) string {
	f := strings.Fields(befehl)
	for len(f) > 1 && f[0] == "sudo" {
		f = f[1:]
	}
	j := 0
	switch {
	case len(f) > 1 && f[0] == name:
		j = 1
	case len(f) > 2 && f[0]+"-"+f[1] == name:
		j = 2
	}
	if j == 0 || j >= len(f) || !kurzschalter(f[j]) {
		return ""
	}
	return f[j]
}

// buendelIn: die Schalterbuendel eines Beispiels («-xzf», «-l», ohne Strich «czf»), ohne Strich.
func buendelIn(b Beispiel, name string) []string {
	var out []string
	for _, w := range befehlsWorte(b.Befehl) {
		if strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "--") && kurzschalter(w[1:]) {
			out = append(out, w[1:])
		}
	}
	if w := ohneStrich(b.Befehl, name); w != "" {
		out = append(out, w)
	}
	return out
}

// buendelSeite: Die Seite schreibt Schalter ohne Strich direkt nach dem Befehl und erklaert
// deren Buchstaben mit Merkhilfen (tar, 7z) — ein Wort wie «xzf» ist dort ein Buendel.
func buendelSeite(s *Seite) bool {
	if s == nil {
		return false
	}
	for _, b := range s.Beispiele {
		for _, c := range ohneStrich(b.Befehl, s.Name) {
			if merkwort(b.Text, c) != "" {
				return true
			}
		}
	}
	return false
}

func ohneStrichErklaeren(s, andere *Seite, w, man, sprache string) string {
	for _, b := range s.Beispiele {
		if ohneStrich(b.Befehl, s.Name) == w {
			return b.Text
		}
	}
	if !buendelSeite(s) && !buendelSeite(andere) {
		return ""
	}
	return buendelErklaeren(s, andere, w, "", man, sprache)
}

// buendelErklaeren: zuerst ein Beispiel mit genau diesen Buchstaben («-xvf» wie «tar xvf»),
// auch auf der Seite der anderen Sprache; sonst Buchstabe fuer Buchstabe (buchstabeErklaeren).
// Jeder Buchstabe bekommt einen Eintrag, einer ohne Erklaerung «siehe man <befehl>» — sonst
// hielte man das Buendel fuer vollstaendig erklaert. Ist kein Buchstabe bekannt: "".
// strich: «-» oder "" (Buendel ohne Strich wie «tar xzf»); man: der Befehl fuer «man».
func buendelErklaeren(s, andere *Seite, zeichen, strich, man, sprache string) string {
	if len(zeichen) > 1 {
		if x := genauesBeispiel(s, zeichen); x != "" {
			return x
		}
		if andere != nil {
			x := ""
			if strich != "" {
				x = schalterErklaeren(*andere, strich+zeichen, sprache)
			}
			if x == "" {
				x = genauesBeispiel(andere, zeichen)
			}
			if x != "" {
				return vonSeite(sprache, andere, x)
			}
		}
	}
	var einzeln []string
	gesehen := map[rune]bool{}
	bekannt := 0
	for _, c := range zeichen {
		if gesehen[c] {
			continue
		}
		gesehen[c] = true
		x := buchstabeErklaeren(s, andere, c, sprache)
		if x != "" && len(zeichen) == 1 {
			return x // ein einzelner Schalter braucht keine Aufzaehlung
		}
		if x != "" {
			bekannt++
		} else {
			x = texte.T(sprache, "hilfe.schalter.man", man)
		}
		einzeln = append(einzeln, strich+string(c)+": "+x)
	}
	// Ohne Strich kann das Wort auch ein Dateiname oder ein Unterbefehl sein («tar archiv»):
	// als Buendel nur, wenn mehr als die Haelfte der Buchstaben bekannt ist.
	if bekannt == 0 || strich == "" && 2*bekannt <= len(gesehen) {
		return ""
	}
	return strings.Join(einzeln, " · ")
}

// genauesBeispiel: der Text des Beispiels mit genau diesen Buchstaben im Buendel (Reihenfolge egal).
func genauesBeispiel(s *Seite, zeichen string) string {
	for _, b := range s.Beispiele {
		for _, x := range buendelIn(b, s.Name) {
			if gleicheBuchstaben(x, zeichen) {
				return b.Text
			}
		}
	}
	return ""
}

// buchstabeErklaeren erklaert einen Buchstaben eines Buendels: «-x» in einem Beispiel, die
// Langform an derselben Stelle («-lt|--listening --tcp»), die Merkhilfe; zuletzt dasselbe auf
// der Seite der anderen Sprache (mit Hinweis auf die Herkunft). "" = nicht in den Beispielen.
func buchstabeErklaeren(s, andere *Seite, c rune, sprache string) string {
	if x := schalterErklaeren(*s, "-"+string(c), sprache); x != "" {
		return x
	}
	if x := langformErklaeren(s, c, sprache); x != "" {
		return x
	}
	if x := merkhilfe(s, andere, c, sprache); x != "" {
		return x
	}
	if andere == nil {
		return ""
	}
	x := schalterErklaeren(*andere, "-"+string(c), sprache)
	if x == "" {
		x = langformErklaeren(andere, c, sprache)
	}
	if x == "" {
		return ""
	}
	return vonSeite(sprache, andere, x)
}

// langformErklaeren: Ein Optionsplatzhalter mit Buendel nennt die Langformen in derselben
// Reihenfolge («{{[-tulpn|--tcp --udp --listening --program --numeric]}}») — der Buchstabe an
// Stelle i ist die Langform an Stelle i. Nur bei gleicher Anzahl und einer echten Langform.
func langformErklaeren(s *Seite, c rune, sprache string) string {
	for _, b := range s.Beispiele {
		for _, m := range optionRe.FindAllStringSubmatch(b.Befehl, -1) {
			formen := strings.Split(m[1], "|")
			for _, kurz := range formen {
				if len(kurz) < 3 || kurz[0] != '-' || !kurzschalter(kurz[1:]) || strings.Count(kurz, string(c)) != 1 {
					continue
				}
				i := strings.IndexRune(kurz[1:], c)
				for _, lang := range formen {
					teile := strings.Fields(lang)
					if len(teile) != len(kurz)-1 || !alleSchalter(teile) {
						continue
					}
					if l := teile[i]; strings.HasPrefix(l, "--") {
						return texte.T(sprache, "hilfe.schalter.auch", l, b.Text)
					}
				}
			}
		}
	}
	return ""
}

func alleSchalter(worte []string) bool {
	for _, w := range worte {
		if len(w) < 2 || w[0] != '-' {
			return false
		}
	}
	return true
}

// vonSeite haengt die Herkunft an: der Text stammt von der Seite der anderen Sprache.
func vonSeite(sprache string, andere *Seite, text string) string {
	if andere.Sprache == texte.EN {
		return texte.T(sprache, "hilfe.seite.englisch", text)
	}
	return texte.T(sprache, "hilfe.seite.deutsch", text)
}

func gleicheBuchstaben(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	x, y := []rune(a), []rune(b)
	sort.Slice(x, func(i, j int) bool { return x[i] < x[j] })
	sort.Slice(y, func(i, j int) bool { return y[i] < y[j] })
	return string(x) == string(y)
}

// merkwort sucht im Text die tldr-Merkhilfe des Buchstabens und liefert das ganze Wort
// («E[x]tract a file» → «E[x]tract»). Ein alleinstehendes «[x]» ist keine Merkhilfe.
func merkwort(text string, c rune) string {
	muster := "[" + string(c) + "]"
	for ab := 0; ab < len(text); {
		i := strings.Index(text[ab:], muster)
		if i < 0 {
			return ""
		}
		i += ab
		a, e := i, i+len(muster)
		for a > 0 {
			r, n := utf8.DecodeLastRuneInString(text[:a])
			if !merkZeichen(r) {
				break
			}
			a -= n
		}
		for e < len(text) {
			r, n := utf8.DecodeRuneInString(text[e:])
			if !merkZeichen(r) {
				break
			}
			e += n
		}
		if e-a > len(muster) {
			return text[a:e]
		}
		ab = i + len(muster)
	}
	return ""
}

func merkZeichen(r rune) bool { return unicode.IsLetter(r) || r == '[' || r == ']' }

// merkwortIn: die erste Merkhilfe des Buchstabens in einem Beispiel, dessen Befehl den
// Buchstaben als Schalter hat (allein oder im Buendel).
func merkwortIn(s *Seite, c rune) string {
	if s == nil {
		return ""
	}
	for _, b := range s.Beispiele {
		if buendelMit(b, s.Name, c) {
			if w := merkwort(b.Text, c); w != "" {
				return w
			}
		}
	}
	return ""
}

// merkhilfe erklaert einen Buchstaben ueber die Merkhilfen: aus der gezeigten Seite; sonst aus
// der Seite der anderen Sprache — dann mit dem Wort der gezeigten Seite, das eindeutig dasselbe
// meint («E[x]tract» → «E[x]trahiere»), sonst die fremde Merkhilfe mit ihrer Herkunft
// («[f]ile (englische Seite)»).
func merkhilfe(s, andere *Seite, c rune, sprache string) string {
	if w := merkwortIn(s, c); w != "" {
		return w
	}
	w := merkwortIn(andere, c)
	if w == "" {
		return ""
	}
	if eigen := eigenesWort(s, andere, c, w); eigen != "" {
		if m := markieren(eigen, c); m != "" {
			return m
		}
		if andere.Sprache == texte.EN {
			return texte.T(sprache, "hilfe.merk.englisch", eigen, w)
		}
		return texte.T(sprache, "hilfe.merk.deutsch", eigen, w)
	}
	return vonSeite(sprache, andere, w)
}

// eigenesWort: das Wort der gezeigten Seite zur Merkhilfe w der anderen Seite, nur bei
// eindeutiger Zuordnung. Jedes Beispiel der anderen Seite mit dieser Merkhilfe (Buchstabe im
// Buendel) braucht ein Gegenstueck, und in jedem steht dasselbe passende Wort: Bei tar steht
// «[f]ile» fuer die Archivdatei, «Dateien» im deutschen Beispiel aber fuer die Eingabedateien —
// die anderen Gegenstuecke nennen keine Datei, also bleibt es bei der englischen Merkhilfe.
// Ist das Wort nur ueber die Synonyme verwandt (nicht ueber den Wortstamm), braucht es zwei
// Belege («Erstelle» zu «[c]reate» viermal); aus einem einzigen kaeme bei ls zu «[S]ize»
// etwa «groesster».
func eigenesWort(s, andere *Seite, c rune, w string) string {
	eigen, belege, stamm := "", 0, true
	for i, b := range andere.Beispiele {
		if merkwort(b.Text, c) != w || !buendelMit(b, andere.Name, c) {
			continue
		}
		k := entsprechung(s, andere, i)
		if k < 0 {
			return ""
		}
		x, gleicherStamm := passendesWort(s.Beispiele[k].Text, w)
		if x == "" || eigen != "" && normal(x) != normal(eigen) {
			return ""
		}
		eigen, stamm = x, stamm && gleicherStamm
		belege++
	}
	if !stamm && belege < 2 {
		return ""
	}
	return eigen
}

// buendelMit: Der Befehl des Beispiels hat den Buchstaben als Schalter (allein oder im Buendel).
func buendelMit(b Beispiel, name string, c rune) bool {
	for _, x := range buendelIn(b, name) {
		if strings.ContainsRune(x, c) {
			return true
		}
	}
	return false
}

// passendesWort sucht im Text das Wort, das dem Wort der anderen Sprache entspricht: mit
// gleichem Wortstamm («E[x]tract» → «Extrahiere», stamm = true) oder ueber die Synonyme
// («[c]reate» → «Erstelle», «g[z]ipped» → «gzip»). Ein Synonym muss das ganze Wort sein (hoechstens
// mit «e» dahinter): «date» passt nicht zu «Dateien», «ausfuhrlich» nicht zum gebeugten
// «ausführlichen».
func passendesWort(text, fremd string) (wort string, stamm bool) {
	ziel := strings.TrimSpace(suchText(fremd))
	if ziel == "" {
		return "", false
	}
	alt := erweitern(ziel)
	for _, roh := range strings.FieldsFunc(merkhilfen.Replace(text), func(r rune) bool { return !unicode.IsLetter(r) }) {
		w := strings.TrimSpace(normal(roh))
		if len(w) < 3 {
			continue
		}
		if verwandt(w, ziel) {
			return roh, true
		}
		for _, a := range alt {
			if len(a) >= 3 && (w == a || w == a+"e") {
				return roh, false
			}
		}
	}
	return "", false
}

// verwandt: gleicher Wortstamm ueber die Sprachen («archive»/«archiv», «extract»/«extrahiere»).
func verwandt(a, b string) bool {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n >= 5
}

// markieren setzt die Merkhilfe in ein Wort («Extrahiere», x → «E[x]trahiere»); "" ohne den Buchstaben.
func markieren(w string, c rune) string {
	i := strings.IndexRune(w, c)
	if i < 0 {
		return ""
	}
	return w[:i] + "[" + string(c) + "]" + w[i+utf8.RuneLen(c):]
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
