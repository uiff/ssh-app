// Package server stellt die HTTP-API, den WebSocket fuer Terminals und die
// eingebettete Oberflaeche bereit.
package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/uiff/ssh-app/internal/help"
	"github.com/uiff/ssh-app/internal/store"
	"github.com/uiff/ssh-app/internal/term"
	"github.com/uiff/ssh-app/internal/vault"
	"github.com/uiff/ssh-app/web"
)

const (
	cookieName = "sshapp"
	// Schluessel des Tresor-Kopfs in der meta-Tabelle.
	MetaTresor = "tresor"
	// Fehlversuche je Adresse, danach Sperre.
	maxFehlversuche = 5
	sperreDauer     = 15 * time.Minute
)

type Optionen struct {
	Version        string
	Leerlauf       time.Duration // Abmeldung nach Inaktivitaet
	SicheresCookie bool          // Secure-Flag (HTTPS)
}

type Server struct {
	st    *store.Store
	tr    *vault.Tresor
	tm    *term.Manager
	hilfe *help.Index
	opt   Optionen

	mu        sync.Mutex
	anmeldung map[string]time.Time // Token -> letzte Aktivitaet
	fehler    map[string]*fehlversuch

	argon chan struct{} // begrenzt gleichzeitige Argon2-Ableitungen (je 64 MiB)
	stop  chan struct{}
	mux   *http.ServeMux
}

type fehlversuch struct {
	n       int
	bis     time.Time
	zuletzt time.Time
}

func Neu(st *store.Store, tr *vault.Tresor, tm *term.Manager, hilfe *help.Index, opt Optionen) *Server {
	if opt.Leerlauf <= 0 {
		opt.Leerlauf = 30 * time.Minute
	}
	s := &Server{st: st, tr: tr, tm: tm, hilfe: hilfe, opt: opt,
		anmeldung: map[string]time.Time{}, fehler: map[string]*fehlversuch{},
		argon: make(chan struct{}, 2), stop: make(chan struct{}), mux: http.NewServeMux()}
	s.routen()
	go s.leerlaufWache()
	return s
}

func (s *Server) Close() { close(s.stop) }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	if s.opt.SicheresCookie {
		h.Set("Strict-Transport-Security", "max-age=31536000")
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routen() {
	statisch, _ := fs.Sub(web.Static, "static")
	dateien := http.FileServer(http.FS(statisch))
	s.mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Cache-Control", "no-store")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		dateien.ServeHTTP(w, r)
	}))

	// Ohne Anmeldung
	s.mux.HandleFunc("GET /api/status", s.status)
	s.mux.HandleFunc("POST /api/einrichten", s.csrf(s.einrichten))
	s.mux.HandleFunc("POST /api/anmelden", s.csrf(s.anmelden))
	s.mux.HandleFunc("POST /api/zuruecksetzen", s.csrf(s.zuruecksetzen))
	s.mux.HandleFunc("POST /api/einspielen", s.csrf(s.einspielenOffen))

	// Mit Anmeldung
	a := func(muster string, f http.HandlerFunc) {
		if !strings.HasPrefix(muster, "GET ") {
			f = s.csrf(f)
		}
		s.mux.HandleFunc(muster, s.angemeldet(f))
	}
	a("POST /api/abmelden", s.abmelden)
	a("POST /api/passwort", s.passwortAendern)
	a("GET /api/sicherung", s.sicherungLaden)
	a("POST /api/sicherung", s.einspielenAngemeldet)

	a("GET /api/daten", s.daten)
	a("POST /api/ordner", s.ordnerSpeichern)
	a("PUT /api/ordner/{id}", s.ordnerSpeichern)
	a("DELETE /api/ordner/{id}", s.ordnerLoeschen)
	a("POST /api/hosts", s.hostSpeichern)
	a("PUT /api/hosts/{id}", s.hostSpeichern)
	a("DELETE /api/hosts/{id}", s.hostLoeschen)
	a("POST /api/zugaenge", s.zugangSpeichern)
	a("PUT /api/zugaenge/{id}", s.zugangSpeichern)
	a("DELETE /api/zugaenge/{id}", s.zugangLoeschen)
	a("POST /api/zugaenge/erzeugen", s.zugangErzeugen)
	a("GET /api/zugaenge/{id}/privat", s.zugangPrivat)
	a("POST /api/zugaenge/{id}/installieren", s.zugangInstallieren)
	a("POST /api/snippets", s.snippetSpeichern)
	a("PUT /api/snippets/{id}", s.snippetSpeichern)
	a("DELETE /api/snippets/{id}", s.snippetLoeschen)
	a("DELETE /api/bekannt/{ziel}", s.bekanntLoeschen)

	a("GET /api/sitzungen", s.sitzungen)
	a("DELETE /api/sitzungen/{id}", s.sitzungBeenden)
	a("GET /api/ws", s.ws)
	a("GET /api/sitzungen/{id}/dateien", s.dateienListe)
	a("GET /api/sitzungen/{id}/pfad", s.dateienPfad)
	a("GET /api/sitzungen/{id}/laden", s.dateiLaden)
	a("POST /api/sitzungen/{id}/hochladen", s.dateiHochladen)
	a("POST /api/sitzungen/{id}/aktion", s.dateiAktion)

	a("GET /api/hilfe/suche", s.hilfeSuche)
	a("GET /api/hilfe/seite/{name}", s.hilfeSeite)
	a("GET /api/hilfe/erklaeren", s.hilfeErklaeren)
}

// --- Antworten ---

func jsonAntwort(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func fehler(w http.ResponseWriter, code int, text string) {
	jsonAntwort(w, code, map[string]string{"fehler": text})
}

func lesen(r *http.Request, v any) error {
	d := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err := d.Decode(v); err != nil {
		return errors.New("Anfrage ist kein gültiges JSON")
	}
	return nil
}

// --- Schutz ---

// csrf verlangt einen eigenen Kopf: Fremdseiten koennen ihn ohne CORS-Freigabe nicht setzen.
func (s *Server) csrf(f http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-SSH-App") != "1" {
			fehler(w, http.StatusForbidden, "Anfrage ohne App-Kennung abgelehnt")
			return
		}
		f(w, r)
	}
}

func (s *Server) angemeldet(f http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil || !s.tokenGueltig(c.Value) {
			fehler(w, http.StatusUnauthorized, "Bitte anmelden")
			return
		}
		f(w, r)
	}
}

func (s *Server) tokenGueltig(t string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	zuletzt, ok := s.anmeldung[t]
	if !ok || time.Since(zuletzt) > s.opt.Leerlauf || !s.tr.Offen() {
		delete(s.anmeldung, t)
		return false
	}
	s.anmeldung[t] = time.Now()
	return true
}

func (s *Server) anmeldungStarten(w http.ResponseWriter) {
	b := make([]byte, 32)
	rand.Read(b)
	t := hex.EncodeToString(b)
	s.mu.Lock()
	s.anmeldung[t] = time.Now()
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: t, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: s.opt.SicheresCookie})
}

// alleAbmelden beendet alle Anmeldungen und sperrt den Tresor.
func (s *Server) alleAbmelden() {
	s.mu.Lock()
	s.anmeldung = map[string]time.Time{}
	s.mu.Unlock()
	s.tr.Sperren()
}

// leerlaufWache sperrt den Tresor, sobald keine Anmeldung mehr aktiv ist.
// Laufende Terminals bleiben dabei bestehen.
func (s *Server) leerlaufWache() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
		}
		s.mu.Lock()
		for tok, zuletzt := range s.anmeldung {
			if time.Since(zuletzt) > s.opt.Leerlauf {
				delete(s.anmeldung, tok)
			}
		}
		leer := len(s.anmeldung) == 0
		for ip, f := range s.fehler {
			if time.Since(f.zuletzt) > sperreDauer && time.Now().After(f.bis) {
				delete(s.fehler, ip)
			}
		}
		s.mu.Unlock()
		if leer && s.tr.Offen() {
			s.tr.Sperren()
			log.Printf("Tresor nach Leerlauf gesperrt")
		}
	}
}

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

// gesperrt meldet, ob die Adresse wegen zu vieler Fehlversuche warten muss.
func (s *Server) gesperrt(r *http.Request) (bool, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fehler[clientIP(r)]
	if f != nil && time.Now().Before(f.bis) {
		return true, time.Until(f.bis).Round(time.Second)
	}
	return false, 0
}

func (s *Server) fehlversuch(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ip := clientIP(r)
	f := s.fehler[ip]
	if f == nil {
		f = &fehlversuch{}
		s.fehler[ip] = f
	}
	f.n++
	f.zuletzt = time.Now()
	if f.n >= maxFehlversuche {
		f.bis = time.Now().Add(sperreDauer)
		f.n = 0
		log.Printf("Anmeldung für %s nach %d Fehlversuchen für %s gesperrt", ip, maxFehlversuche, sperreDauer)
	}
}

func (s *Server) erfolg(r *http.Request) {
	s.mu.Lock()
	delete(s.fehler, clientIP(r))
	s.mu.Unlock()
}

func (s *Server) mitArgon(f func()) {
	s.argon <- struct{}{}
	defer func() { <-s.argon }()
	f()
}

// --- Einrichtung und Anmeldung ---

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	angemeldet := false
	if c, err := r.Cookie(cookieName); err == nil {
		angemeldet = s.tokenGueltig(c.Value)
	}
	antwort := map[string]any{
		"eingerichtet": s.tr.Angelegt(),
		"angemeldet":   angemeldet,
		"version":      s.opt.Version,
		"hilfeSeiten":  s.hilfe.Anzahl(),
		"minPasswort":  vault.MinPasswortLaenge,
	}
	if angemeldet {
		antwort["benutzer"] = s.tr.Benutzer()
		antwort["leerlaufMin"] = int(s.opt.Leerlauf.Minutes())
	}
	jsonAntwort(w, http.StatusOK, antwort)
}

type zugangsdaten struct {
	Benutzer   string `json:"benutzer"`
	Passwort   string `json:"passwort"`
	Schluessel string `json:"schluessel"`
}

func (s *Server) einrichten(w http.ResponseWriter, r *http.Request) {
	var z zugangsdaten
	if err := lesen(r, &z); err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	var kopf []byte
	var sk string
	var err error
	s.mitArgon(func() { kopf, sk, err = s.tr.Einrichten(strings.TrimSpace(z.Benutzer), z.Passwort) })
	if errors.Is(err, vault.ErrSchonAngelegt) {
		fehler(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.st.SetMeta(MetaTresor, kopf); err != nil {
		fehler(w, http.StatusInternalServerError, "Speichern fehlgeschlagen: "+err.Error())
		return
	}
	log.Printf("Einrichtung abgeschlossen (Benutzer %s)", s.tr.Benutzer())
	s.anmeldungStarten(w)
	jsonAntwort(w, http.StatusOK, map[string]string{"sicherheitsschluessel": sk})
}

func (s *Server) anmelden(w http.ResponseWriter, r *http.Request) {
	if g, rest := s.gesperrt(r); g {
		fehler(w, http.StatusTooManyRequests, "Zu viele Fehlversuche — noch "+rest.String()+" gesperrt")
		return
	}
	var z zugangsdaten
	if err := lesen(r, &z); err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	var err error
	s.mitArgon(func() { err = s.tr.Anmelden(strings.TrimSpace(z.Benutzer), z.Passwort) })
	if errors.Is(err, vault.ErrNichtAngelegt) {
		fehler(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		s.fehlversuch(r)
		log.Printf("Fehlgeschlagene Anmeldung von %s", clientIP(r))
		fehler(w, http.StatusUnauthorized, err.Error())
		return
	}
	s.erfolg(r)
	log.Printf("Anmeldung von %s", clientIP(r))
	s.anmeldungStarten(w)
	jsonAntwort(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) abmelden(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.mu.Lock()
		delete(s.anmeldung, c.Value)
		leer := len(s.anmeldung) == 0
		s.mu.Unlock()
		if leer {
			s.tr.Sperren()
		}
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: s.opt.SicheresCookie})
	jsonAntwort(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) zuruecksetzen(w http.ResponseWriter, r *http.Request) {
	if g, rest := s.gesperrt(r); g {
		fehler(w, http.StatusTooManyRequests, "Zu viele Fehlversuche — noch "+rest.String()+" gesperrt")
		return
	}
	var z zugangsdaten
	if err := lesen(r, &z); err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	var kopf []byte
	var err error
	s.mitArgon(func() { kopf, err = s.tr.Zuruecksetzen(z.Schluessel, strings.TrimSpace(z.Benutzer), z.Passwort) })
	if errors.Is(err, vault.ErrSchluesselFalsch) {
		s.fehlversuch(r)
		fehler(w, http.StatusUnauthorized, err.Error())
		return
	}
	if err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.st.SetMeta(MetaTresor, kopf); err != nil {
		fehler(w, http.StatusInternalServerError, "Speichern fehlgeschlagen: "+err.Error())
		return
	}
	s.erfolg(r)
	log.Printf("Passwort mit Sicherheitsschlüssel zurückgesetzt (von %s)", clientIP(r))
	s.alleAbmelden()
	jsonAntwort(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) passwortAendern(w http.ResponseWriter, r *http.Request) {
	var p struct{ Alt, Neu string }
	if err := lesen(r, &p); err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	var kopf []byte
	var err error
	s.mitArgon(func() { kopf, err = s.tr.PasswortAendern(p.Alt, p.Neu) })
	if err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.st.SetMeta(MetaTresor, kopf); err != nil {
		fehler(w, http.StatusInternalServerError, "Speichern fehlgeschlagen: "+err.Error())
		return
	}
	jsonAntwort(w, http.StatusOK, map[string]bool{"ok": true})
}
