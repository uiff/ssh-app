package server

import (
	"net/http"
	"net/url"

	"github.com/uiff/ssh-app/internal/help"
	"github.com/uiff/ssh-app/internal/texte"
)

// --- Hilfe ---
//
// Die Sprache der Oberflaeche (texte.AusAnfrage) bestimmt, welche tldr-Seite
// kommt: die in dieser Sprache, sonst die der anderen.

func (s *Server) hilfeSuche(w http.ResponseWriter, r *http.Request) {
	jsonAntwort(w, http.StatusOK, s.hilfe.Suchen(r.URL.Query().Get("q"), texte.AusAnfrage(r), 25))
}

// hilfeSeite: Parameter «seitensprache» verlangt die Seite in genau dieser
// Sprache (ein Suchtreffer kann aus der anderen Sprache stammen).
func (s *Server) hilfeSeite(w http.ResponseWriter, r *http.Request) {
	sprache := texte.AusAnfrage(r)
	name, _ := url.PathUnescape(r.PathValue("name"))
	var seite help.Seite
	ok := false
	if ss := r.URL.Query().Get("seitensprache"); ss != "" {
		seite, ok = s.hilfe.SeiteGenau(name, ss)
	}
	if !ok {
		seite, ok = s.hilfe.Seite(name, sprache)
	}
	if !ok {
		fehlerT(w, r, http.StatusNotFound, "hilfe.keine_seite", name)
		return
	}
	jsonAntwort(w, http.StatusOK, seite)
}

func (s *Server) hilfeErklaeren(w http.ResponseWriter, r *http.Request) {
	seite, teile, ok := s.hilfe.Erklaeren(r.URL.Query().Get("zeile"), texte.AusAnfrage(r))
	if !ok {
		jsonAntwort(w, http.StatusOK, map[string]any{"gefunden": false})
		return
	}
	jsonAntwort(w, http.StatusOK, map[string]any{"gefunden": true, "seite": seite, "teile": teile})
}
