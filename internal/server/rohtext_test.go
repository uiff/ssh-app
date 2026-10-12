package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/uiff/ssh-app/internal/texte"
)

// Waechter: Antworten an den Browser enthalten keine Texte direkt aus dem Code, alles kommt
// aus dem Katalog (internal/texte) in der Sprache der Anfrage. Geprueft werden
//   - fehler(w, code, TEXT) und http.Error(w, TEXT, code),
//   - JSON-Felder «fehler», «grund», «hinweis» und «text» in Map-Literalen,
//   - nachricht{Text: …} fuer den Terminal-WebSocket.
//
// Ein Text ist ein Zeichenketten-Literal mit Buchstaben (auch in einer Verkettung).
func TestKeineRohtexteInAntworten(t *testing.T) {
	dateien, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	geprueft := 0
	for _, d := range dateien {
		if strings.HasSuffix(d, "_test.go") {
			continue
		}
		quelle, err := os.ReadFile(d)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, d, quelle, 0)
		if err != nil {
			t.Fatal(err)
		}
		melde := func(e ast.Expr, wo string) {
			geprueft++
			if lit := prosa(e); lit != "" {
				t.Errorf("%s: %s mit Text aus dem Code: %s — Katalog (internal/texte) und fehlerT/fehlerAus benutzen", fset.Position(e.Pos()), wo, lit)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				switch fn := x.Fun.(type) {
				case *ast.Ident:
					if fn.Name == "fehler" && len(x.Args) == 3 {
						melde(x.Args[2], "fehler()")
					}
				case *ast.SelectorExpr:
					if p, ok := fn.X.(*ast.Ident); ok && p.Name == "http" && fn.Sel.Name == "Error" && len(x.Args) == 3 {
						melde(x.Args[1], "http.Error()")
					}
				}
			case *ast.KeyValueExpr:
				if k, ok := x.Key.(*ast.BasicLit); ok && k.Kind == token.STRING {
					switch s, _ := strconv.Unquote(k.Value); s {
					case "fehler", "grund", "hinweis", "text":
						melde(x.Value, "JSON-Feld «"+s+"»")
					}
				}
			case *ast.CompositeLit:
				if typ, ok := x.Type.(*ast.Ident); ok && typ.Name == "nachricht" {
					for _, el := range x.Elts {
						if kv, ok := el.(*ast.KeyValueExpr); ok {
							if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Text" {
								melde(kv.Value, "nachricht.Text")
							}
						}
					}
				}
			}
			return true
		})
	}
	if geprueft < 10 {
		t.Fatalf("nur %d Antwortstellen gefunden — Pruefung greift nicht", geprueft)
	}
}

var fmtVerb = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z]`)

// Waechter: Jeder Katalogschluessel, den der Go-Code nennt (texte.Neu, texte.T, fehlerT,
// v.t, meldung), steht im Katalog — mit so vielen Werten, wie der Text Platzhalter hat.
func TestKatalogschluesselImServerCode(t *testing.T) {
	de := texte.Schluessel(texte.DE)
	dateien, _ := filepath.Glob("../*/*.go")
	extra, _ := filepath.Glob("../../cmd/*/*.go")
	dateien = append(dateien, extra...)
	fset := token.NewFileSet()
	n := 0
	for _, d := range dateien {
		if strings.HasSuffix(d, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, d, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(x ast.Node) bool {
			c, ok := x.(*ast.CallExpr)
			if !ok {
				return true
			}
			pos := -1
			switch fn := c.Fun.(type) {
			case *ast.Ident:
				switch fn.Name {
				case "fehlerT":
					pos = 3
				case "meldung":
					pos = 1
				}
			case *ast.SelectorExpr:
				p, _ := fn.X.(*ast.Ident)
				switch {
				case p != nil && p.Name == "texte" && fn.Sel.Name == "Neu":
					pos = 0
				case p != nil && p.Name == "texte" && fn.Sel.Name == "T":
					pos = 1
				case fn.Sel.Name == "t" && (p == nil || p.Name != "texte"):
					pos = 0
				}
			}
			if pos < 0 || len(c.Args) <= pos {
				return true
			}
			lit, ok := c.Args[pos].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true // Schluessel aus einer Variablen: zur Laufzeit (texte_test)
			}
			k, _ := strconv.Unquote(lit.Value)
			n++
			text, gibt := de[k]
			if !gibt {
				t.Errorf("%s: Schluessel %q fehlt im Katalog (internal/texte)", fset.Position(lit.Pos()), k)
				return true
			}
			if c.Ellipsis == token.NoPos {
				verben := 0
				for _, v := range fmtVerb.FindAllString(text, -1) {
					if v != "%%" {
						verben++
					}
				}
				if werte := len(c.Args) - pos - 1; werte != verben {
					t.Errorf("%s: %q braucht %d Werte, bekommt %d", fset.Position(lit.Pos()), k, verben, werte)
				}
			}
			return true
		})
	}
	if n < 50 {
		t.Fatalf("nur %d Schluessel im Code gefunden — Pruefung greift nicht", n)
	}
}

// prosa liefert das erste Zeichenketten-Literal mit Buchstaben in e (auch in a + b), sonst "".
func prosa(e ast.Expr) string {
	gefunden := ""
	ast.Inspect(e, func(n ast.Node) bool {
		if gefunden != "" {
			return false
		}
		switch x := n.(type) {
		case *ast.CallExpr:
			// Argumente von Aufrufen (z.B. texte.T(sprache, "schluessel")) sind Schluessel, kein Text.
			return false
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				s, _ := strconv.Unquote(x.Value)
				if strings.IndexFunc(s, unicode.IsLetter) >= 0 {
					gefunden = x.Value
				}
			}
		}
		return true
	})
	return gefunden
}
