// Package texte liefert Meldungen fuer Anwender in Deutsch und Englisch.
//
// Jeder Bereich traegt seine Texte in einer eigenen Datei dieses Pakets ein
// (init-Funktion mit Eintragen). Platzhalter sind fmt-Verben (%s, %d …) in
// derselben Reihenfolge in beiden Sprachen. Fehlt ein Text in der gewuenschten
// Sprache, kommt der deutsche; fehlt der Schluessel ganz, der Schluessel selbst
// (der Waechter-Test findet das).
package texte

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// Sprachen, die die App kennt. Die erste ist die Vorgabe.
const (
	DE = "de"
	EN = "en"
)

var (
	mu      sync.RWMutex
	katalog = map[string]map[string]string{DE: {}, EN: {}}
)

// Eintragen fuegt Texte hinzu. Ein Schluessel darf nur einmal vorkommen.
func Eintragen(de, en map[string]string) {
	mu.Lock()
	defer mu.Unlock()
	for k, v := range de {
		if _, gibt := katalog[DE][k]; gibt {
			panic("texte: Schluessel doppelt: " + k)
		}
		katalog[DE][k] = v
	}
	for k, v := range en {
		katalog[EN][k] = v
	}
}

// T liefert den Text zum Schluessel in der Sprache, mit eingesetzten Werten.
func T(sprache, schluessel string, werte ...any) string {
	mu.RLock()
	s, ok := katalog[Normal(sprache)][schluessel]
	if !ok {
		s, ok = katalog[DE][schluessel]
	}
	mu.RUnlock()
	if !ok {
		s = schluessel
	}
	if len(werte) == 0 {
		return s
	}
	return fmt.Sprintf(s, werte...)
}

// Normal bildet beliebige Sprachangaben («en-US», «DE») auf DE oder EN ab.
func Normal(sprache string) string {
	s := strings.ToLower(strings.TrimSpace(sprache))
	if strings.HasPrefix(s, "en") {
		return EN
	}
	return DE
}

// AusAnfrage bestimmt die Sprache einer Anfrage: Kopf «X-SSH-App-Sprache»,
// sonst Parameter «sprache» (WebSocket), sonst Accept-Language, sonst Deutsch.
func AusAnfrage(r *http.Request) string {
	if v := r.Header.Get("X-SSH-App-Sprache"); v != "" {
		return Normal(v)
	}
	if v := r.URL.Query().Get("sprache"); v != "" {
		return Normal(v)
	}
	if al := r.Header.Get("Accept-Language"); al != "" {
		erste := strings.SplitN(al, ",", 2)[0]
		return Normal(erste)
	}
	return DE
}

// Schluessel listet alle Schluessel einer Sprache (fuer Waechter-Tests).
func Schluessel(sprache string) map[string]string {
	mu.RLock()
	defer mu.RUnlock()
	kopie := make(map[string]string, len(katalog[sprache]))
	for k, v := range katalog[sprache] {
		kopie[k] = v
	}
	return kopie
}

// Meldung ist ein Fehler mit uebersetzbarem Text. Error() liefert Deutsch
// (fuer Logs und bestehende Aufrufer); Text(sprache) die gewuenschte Sprache.
type Meldung struct {
	Schluessel string
	Werte      []any
	Ursache    error // optional, nur fuers Log / errors.Is
}

func Neu(schluessel string, werte ...any) *Meldung {
	return &Meldung{Schluessel: schluessel, Werte: werte}
}

func (m *Meldung) Error() string              { return T(DE, m.Schluessel, m.Werte...) }
func (m *Meldung) Text(sprache string) string { return T(sprache, m.Schluessel, m.Werte...) }
func (m *Meldung) Unwrap() error              { return m.Ursache }

// MitUrsache haengt den technischen Grund an (erscheint nie beim Anwender).
func (m *Meldung) MitUrsache(err error) *Meldung {
	k := *m
	k.Ursache = err
	return &k
}

// Uebersetzen liefert fuer einen beliebigen Fehler den Anwendertext: eine
// Meldung (auch eingewickelt) in der gewuenschten Sprache, sonst err.Error().
func Uebersetzen(sprache string, err error) string {
	if err == nil {
		return ""
	}
	var m *Meldung
	for e := err; e != nil; {
		if mm, ok := e.(*Meldung); ok {
			m = mm
			break
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			break
		}
		e = u.Unwrap()
	}
	if m != nil {
		return m.Text(sprache)
	}
	return err.Error()
}
