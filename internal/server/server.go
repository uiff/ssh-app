// Package server stellt die HTTP-API, den WebSocket fuer Terminals und die
// eingebettete Oberflaeche bereit.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
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
	// Hoechstens so viele Anfragen warten auf einen Argon2-Platz (oder rechnen); weitere bekommen 503.
	maxArgonWartend = 16
	// Kopf automatischer Abfragen (Zeitgeber, Wiederverbinden): sie verlaengern die Anmeldung nicht.
	// Beim WebSocket (kein eigener Kopf moeglich) gilt der Parameter auto=1.
	autoKopf = "X-SSH-App-Auto"
)

type Optionen struct {
	Version        string
	Leerlauf       time.Duration // Abmeldung nach so langer Zeit ohne Bedienung
	SicheresCookie bool          // Secure-Flag (HTTPS)
	HSTS           bool          // Strict-Transport-Security senden (gilt fuer alle Ports des Namens)
	// Proxys sind vertrauenswuerdige Reverse-Proxys: nur von dort gilt X-Forwarded-For.
	Proxys []netip.Prefix
	// HostPruefung lehnt fremde Hostnamen ab (Schutz vor DNS-Rebinding im HTTP-Betrieb).
	// Erlaubt sind IP-Adressen, localhost, Namen ohne Punkt, lokale Endungen und Hosts.
	HostPruefung bool
	Hosts        []string // zusaetzlich erlaubte Namen, «*.beispiel.ch» fuer alle Unternamen
	Origins      []string // zusaetzlich erlaubte Origins fuer den WebSocket (Muster wie path.Match)
}

type Server struct {
	st    *store.Store
	tr    *vault.Tresor
	tm    *term.Manager
	hilfe *help.Index
	opt   Optionen

	mu        sync.Mutex
	anmeldung map[string]time.Time // Token -> letzte Bedienung
	fehler    map[string]*fehlversuch

	// kopfMu: Einrichten, Einspielen, Zuruecksetzen und Passwort aendern ersetzen den
	// Tresor-Kopf und laufen nacheinander.
	kopfMu sync.Mutex

	argon   chan struct{} // begrenzt gleichzeitige Argon2-Ableitungen (je 64 MiB)
	wartend atomic.Int32  // Anfragen, die auf einen Argon2-Platz warten oder rechnen
	stop    chan struct{}
	mux     *http.ServeMux
}

// fehlversuch zaehlt je Adresse (IPv6: je /64) die gescheiterten und die gerade
// laufenden Versuche.
type fehlversuch struct {
	n       int // gescheitert im aktuellen Fenster
	laufend int // reserviert, Ergebnis steht noch aus
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
	if s.opt.HSTS {
		h.Set("Strict-Transport-Security", "max-age=31536000")
	}
	if !s.hostErlaubt(r) {
		text := "Zugriff über den Namen «" + kurz(hostOhnePort(r.Host), 80) + "» ist nicht freigegeben (Schutz vor DNS-Rebinding). Den Namen in SSHAPP_HOSTS eintragen."
		if strings.HasPrefix(r.URL.Path, "/api/") {
			fehler(w, http.StatusMisdirectedRequest, text)
		} else {
			http.Error(w, text, http.StatusMisdirectedRequest)
		}
		return
	}
	s.mux.ServeHTTP(w, r)
}

func kurz(t string, n int) string {
	if r := []rune(t); len(r) > n {
		return string(r[:n]) + "…"
	}
	return t
}

func hostOhnePort(h string) string {
	if x, _, err := net.SplitHostPort(h); err == nil {
		h = x
	}
	return strings.ToLower(strings.TrimSuffix(strings.Trim(h, "[]"), "."))
}

// lokaleEndungen gehoeren zu Namen im eigenen Netz, die niemand Fremdes auf eine
// eigene Adresse zeigen lassen kann.
var lokaleEndungen = []string{".localhost", ".local", ".lan", ".home", ".home.arpa", ".internal", ".intranet", ".localdomain", ".fritz.box", ".ts.net"}

// hostErlaubt prueft den Host-Kopf gegen DNS-Rebinding: Eine fremde Seite kann ihren
// eigenen Namen auf die Adresse der App zeigen lassen und waere dann «gleiche Herkunft».
func (s *Server) hostErlaubt(r *http.Request) bool {
	if !s.opt.HostPruefung || s.vonProxy(r) {
		return true
	}
	host := hostOhnePort(r.Host)
	if host == "" || host == "localhost" {
		return true
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	for _, n := range s.opt.Hosts {
		n = strings.ToLower(n)
		if host == n || (strings.HasPrefix(n, "*.") && strings.HasSuffix(host, n[1:])) {
			return true
		}
	}
	if !strings.Contains(host, ".") {
		return true // Rechnername im eigenen Netz
	}
	for _, e := range lokaleEndungen {
		if strings.HasSuffix(host, e) {
			return true
		}
	}
	return false
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

// istAuto: automatische Abfrage (Zeitgeber, Wiederverbinden), keine Bedienung.
func istAuto(r *http.Request) bool {
	return r.Header.Get(autoKopf) == "1" || r.URL.Query().Get("auto") == "1"
}

func (s *Server) angemeldet(f http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil || !s.tokenGueltig(c.Value, !istAuto(r)) {
			fehler(w, http.StatusUnauthorized, "Bitte anmelden")
			return
		}
		f(w, r)
	}
}

// tokenGueltig prueft eine Anmeldung. auffrischen = es war Bedienung: die
// Leerlaufzeit beginnt neu. Abgelaufene Anmeldungen werden entfernt.
func (s *Server) tokenGueltig(t string, auffrischen bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	zuletzt, ok := s.anmeldung[t]
	if !ok || time.Since(zuletzt) > s.opt.Leerlauf || !s.tr.Offen() {
		delete(s.anmeldung, t)
		return false
	}
	if auffrischen {
		s.anmeldung[t] = time.Now()
	}
	return true
}

// pruefAbstand: so oft prueft ein offener WebSocket, ob seine Anmeldung noch gilt.
func (s *Server) pruefAbstand() time.Duration {
	return min(5*time.Second, max(s.opt.Leerlauf/10, 50*time.Millisecond))
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
			if f.laufend == 0 && time.Since(f.zuletzt) > sperreDauer && time.Now().After(f.bis) {
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

// vertraut meldet, ob eine Adresse zu einem vertrauenswuerdigen Proxy gehoert.
func (s *Server) vertraut(a string) bool {
	ip, err := netip.ParseAddr(strings.Trim(a, "[]"))
	if err != nil {
		return false
	}
	ip = ip.Unmap()
	for _, p := range s.opt.Proxys {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

func entferntHost(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

// vonProxy: die Anfrage kommt direkt von einem vertrauenswuerdigen Proxy.
func (s *Server) vonProxy(r *http.Request) bool {
	return len(s.opt.Proxys) > 0 && s.vertraut(entferntHost(r))
}

// clientIP liefert die Adresse des Browsers. Hinter einem vertrauenswuerdigen Proxy
// (SSHAPP_PROXY) gilt der letzte fremde Eintrag in X-Forwarded-For, sonst die
// Gegenstelle der Verbindung.
func (s *Server) clientIP(r *http.Request) string {
	h := entferntHost(r)
	if !s.vonProxy(r) {
		return h
	}
	teile := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	if len(teile) == 1 && strings.TrimSpace(teile[0]) == "" {
		teile = []string{r.Header.Get("X-Real-IP")}
	}
	for i := len(teile) - 1; i >= 0; i-- {
		a := strings.TrimSpace(teile[i])
		if a == "" {
			continue
		}
		if x, _, err := net.SplitHostPort(a); err == nil {
			a = x
		}
		ip, err := netip.ParseAddr(strings.Trim(a, "[]"))
		if err != nil {
			break // ungueltig: weiter links nichts mehr glauben
		}
		h = ip.Unmap().String()
		if !s.vertraut(h) {
			break
		}
	}
	return h
}

// sperrSchluessel fasst IPv6-Adressen zum /64 zusammen (ein Anschluss hat viele).
func sperrSchluessel(ip string) string {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	a = a.Unmap()
	if a.Is6() {
		if p, err := a.Prefix(64); err == nil {
			return p.String()
		}
	}
	return a.String()
}

// versuch ist ein reservierter Anmelde- oder Schluesselversuch einer Adresse.
type versuch struct {
	s      *Server
	f      *fehlversuch
	schl   string
	fertig bool
}

const (
	versuchOffen = iota
	versuchFalsch
	versuchRichtig
)

// versuchBeginnen reserviert einen Versuch, bevor gerechnet wird. Gleichzeitige
// Anfragen zaehlen so schon gegen die Grenze; mehr als maxFehlversuche sind je
// Sperrfenster nicht moeglich. Ohne Reservierung kommt der Grund der Ablehnung zurueck.
func (s *Server) versuchBeginnen(r *http.Request) (*versuch, string) {
	schl := sperrSchluessel(s.clientIP(r))
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fehler[schl]
	if f == nil {
		f = &fehlversuch{}
		s.fehler[schl] = f
	}
	jetzt := time.Now()
	f.zuletzt = jetzt
	if jetzt.Before(f.bis) {
		return nil, "Zu viele Fehlversuche — noch " + f.bis.Sub(jetzt).Round(time.Second).String() + " gesperrt"
	}
	if f.n+f.laufend >= maxFehlversuche {
		return nil, "Zu viele Anmeldeversuche gleichzeitig — bitte kurz warten"
	}
	f.laufend++
	return &versuch{s: s, f: f, schl: schl}, ""
}

func (v *versuch) beenden(ergebnis int) {
	v.s.mu.Lock()
	defer v.s.mu.Unlock()
	if v.fertig {
		return
	}
	v.fertig = true
	f := v.f
	f.laufend--
	f.zuletzt = time.Now()
	switch ergebnis {
	case versuchFalsch:
		f.n++
		if f.n >= maxFehlversuche {
			f.bis = time.Now().Add(sperreDauer)
			f.n = 0
			log.Printf("Anmeldung für %s nach %d Fehlversuchen für %s gesperrt", v.schl, maxFehlversuche, sperreDauer)
		}
	case versuchRichtig:
		// Nur den Zaehler zuruecksetzen; eine laufende Sperre bleibt bestehen.
		f.n = 0
	}
}

func (v *versuch) falsch()    { v.beenden(versuchFalsch) }
func (v *versuch) richtig()   { v.beenden(versuchRichtig) }
func (v *versuch) abbrechen() { v.beenden(versuchOffen) }

var errBelegt = errors.New("Gerade zu viele Anmeldeversuche — bitte kurz warten")

// mitArgon fuehrt f aus, sobald einer der Argon2-Plaetze frei ist. Die Warteschlange
// ist begrenzt (errBelegt), und Anfragen, deren Browser schon weg ist, rechnen nicht.
func (s *Server) mitArgon(ctx context.Context, f func()) error {
	if s.wartend.Add(1) > maxArgonWartend {
		s.wartend.Add(-1)
		return errBelegt
	}
	defer s.wartend.Add(-1)
	select {
	case s.argon <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-s.argon }()
	if err := ctx.Err(); err != nil {
		return err
	}
	f()
	return nil
}

// argonFehler antwortet auf einen Fehler von mitArgon (ist der Browser weg, nicht mehr).
func argonFehler(w http.ResponseWriter, err error) {
	if errors.Is(err, errBelegt) {
		fehler(w, http.StatusServiceUnavailable, err.Error())
	}
}

// --- Einrichtung und Anmeldung ---

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	angemeldet := false
	if c, err := r.Cookie(cookieName); err == nil {
		angemeldet = s.tokenGueltig(c.Value, !istAuto(r))
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
	if s.tr.Angelegt() { // schnelle Antwort ohne Warten auf andere Vorgaenge
		fehler(w, http.StatusConflict, vault.ErrSchonAngelegt.Error())
		return
	}
	s.kopfMu.Lock()
	defer s.kopfMu.Unlock()
	if s.tr.Angelegt() {
		fehler(w, http.StatusConflict, vault.ErrSchonAngelegt.Error())
		return
	}
	var kopf []byte
	var sk string
	var err error
	if e := s.mitArgon(r.Context(), func() { kopf, sk, err = s.tr.Einrichten(strings.TrimSpace(z.Benutzer), z.Passwort) }); e != nil {
		argonFehler(w, e)
		return
	}
	if errors.Is(err, vault.ErrSchonAngelegt) {
		fehler(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.st.SetMeta(MetaTresor, kopf); err != nil {
		log.Printf("Einrichtung speichern: %v", err)
		fehler(w, http.StatusInternalServerError, "Speichern fehlgeschlagen")
		return
	}
	log.Printf("Einrichtung abgeschlossen (Benutzer %s, von %s)", s.tr.Benutzer(), s.clientIP(r))
	s.anmeldungStarten(w)
	jsonAntwort(w, http.StatusOK, map[string]string{"sicherheitsschluessel": sk})
}

func (s *Server) anmelden(w http.ResponseWriter, r *http.Request) {
	v, grund := s.versuchBeginnen(r)
	if v == nil {
		fehler(w, http.StatusTooManyRequests, grund)
		return
	}
	defer v.abbrechen()
	var z zugangsdaten
	if err := lesen(r, &z); err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	var err error
	if e := s.mitArgon(r.Context(), func() { err = s.tr.Anmelden(strings.TrimSpace(z.Benutzer), z.Passwort) }); e != nil {
		argonFehler(w, e)
		return
	}
	if errors.Is(err, vault.ErrNichtAngelegt) {
		fehler(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		v.falsch()
		log.Printf("Fehlgeschlagene Anmeldung von %s", s.clientIP(r))
		fehler(w, http.StatusUnauthorized, err.Error())
		return
	}
	v.richtig()
	log.Printf("Anmeldung von %s", s.clientIP(r))
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
	v, grund := s.versuchBeginnen(r)
	if v == nil {
		fehler(w, http.StatusTooManyRequests, grund)
		return
	}
	defer v.abbrechen()
	var z zugangsdaten
	if err := lesen(r, &z); err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	s.kopfMu.Lock()
	defer s.kopfMu.Unlock()
	var kopf []byte
	var err error
	if e := s.mitArgon(r.Context(), func() { kopf, err = s.tr.Zuruecksetzen(z.Schluessel, strings.TrimSpace(z.Benutzer), z.Passwort) }); e != nil {
		argonFehler(w, e)
		return
	}
	if errors.Is(err, vault.ErrSchluesselFalsch) {
		v.falsch()
		log.Printf("Zurücksetzen mit falschem Sicherheitsschlüssel von %s", s.clientIP(r))
		fehler(w, http.StatusUnauthorized, err.Error())
		return
	}
	if err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.st.SetMeta(MetaTresor, kopf); err != nil {
		log.Printf("Zurücksetzen speichern: %v", err)
		fehler(w, http.StatusInternalServerError, "Speichern fehlgeschlagen")
		return
	}
	v.richtig()
	log.Printf("Passwort mit Sicherheitsschlüssel zurückgesetzt (von %s)", s.clientIP(r))
	s.alleAbmelden()
	jsonAntwort(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) passwortAendern(w http.ResponseWriter, r *http.Request) {
	v, grund := s.versuchBeginnen(r)
	if v == nil {
		fehler(w, http.StatusTooManyRequests, grund)
		return
	}
	defer v.abbrechen()
	var p struct{ Alt, Neu string }
	if err := lesen(r, &p); err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	s.kopfMu.Lock()
	defer s.kopfMu.Unlock()
	var kopf []byte
	var err error
	if e := s.mitArgon(r.Context(), func() { kopf, err = s.tr.PasswortAendern(p.Alt, p.Neu) }); e != nil {
		argonFehler(w, e)
		return
	}
	if errors.Is(err, vault.ErrAltFalsch) {
		v.falsch()
		log.Printf("Passwort ändern mit falschem bisherigem Passwort von %s", s.clientIP(r))
	}
	if err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.st.SetMeta(MetaTresor, kopf); err != nil {
		log.Printf("Passwort ändern speichern: %v", err)
		fehler(w, http.StatusInternalServerError, "Speichern fehlgeschlagen")
		return
	}
	v.richtig()
	jsonAntwort(w, http.StatusOK, map[string]bool{"ok": true})
}
