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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uiff/ssh-app/internal/help"
	"github.com/uiff/ssh-app/internal/store"
	"github.com/uiff/ssh-app/internal/term"
	"github.com/uiff/ssh-app/internal/texte"
	"github.com/uiff/ssh-app/internal/vault"
	"github.com/uiff/ssh-app/web"
)

const (
	cookieName = "sshapp"
	// Schluessel des Tresor-Kopfs in der meta-Tabelle.
	MetaTresor = "tresor"
	// Fehlversuche je Adresse, danach Sperre.
	maxFehlversuche = 5
	// Fehlversuche je fremdem IPv6-Netz (/64, ein Anschluss hat viele Adressen).
	maxFehlversucheNetz = 30
	sperreDauer         = 15 * time.Minute
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

	// Eigene IPv6-Netze (/64 der Schnittstellen), kurz zwischengespeichert. schnittstellen
	// liefert sie (Tests ersetzen die Funktion).
	netzMu         sync.Mutex
	netze          []netip.Prefix
	netzeStand     time.Time
	schnittstellen func() []netip.Prefix
}

// fehlversuch zaehlt je Sperrstufe (Adresse oder fremdes IPv6-Netz) die gescheiterten
// und die gerade laufenden Versuche.
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
		argon: make(chan struct{}, 2), stop: make(chan struct{}), mux: http.NewServeMux(),
		schnittstellen: schnittstellenNetze}
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
		text := texte.T(texte.AusAnfrage(r), "server.host.gesperrt", kurz(hostOhnePort(r.Host), 80))
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

// fehler antwortet mit einem fertigen Text. Neue Aufrufer nehmen fehlerT oder fehlerAus.
func fehler(w http.ResponseWriter, code int, text string) {
	jsonAntwort(w, code, map[string]string{"fehler": text})
}

// fehlerT antwortet mit dem Katalogtext schluessel (internal/texte) in der Sprache
// der Anfrage; werte fuellen die fmt-Verben des Textes.
func fehlerT(w http.ResponseWriter, r *http.Request, code int, schluessel string, werte ...any) {
	fehler(w, code, texte.T(texte.AusAnfrage(r), schluessel, werte...))
}

// fehlerAus antwortet mit einem Fehler: eine texte.Meldung (auch eingewickelt) kommt
// in der Sprache der Anfrage, jeder andere Fehler mit err.Error().
func fehlerAus(w http.ResponseWriter, r *http.Request, code int, err error) {
	fehler(w, code, texte.Uebersetzen(texte.AusAnfrage(r), err))
}

func lesen(r *http.Request, v any) error {
	d := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err := d.Decode(v); err != nil {
		return texte.Neu("server.json.ungueltig")
	}
	return nil
}

// --- Schutz ---

// csrf verlangt einen eigenen Kopf: Fremdseiten koennen ihn ohne CORS-Freigabe nicht setzen.
func (s *Server) csrf(f http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-SSH-App") != "1" {
			fehlerT(w, r, http.StatusForbidden, "server.csrf")
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
			fehlerT(w, r, http.StatusUnauthorized, "server.anmelden")
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

// sperrStufe ist ein Zaehler fuer Fehlversuche: die Adresse selbst oder ihr IPv6-Netz.
type sperrStufe struct {
	schl   string
	grenze int
}

// sperrStufen: Jede Adresse hat ihren eigenen Zaehler. IPv6-Adressen aus fremden Netzen
// zaehlen zusaetzlich gemeinsam je /64 mit einer lockereren Grenze, denn ein Anschluss
// hat dort beliebig viele Adressen. Im eigenen Netz (link-lokal, ULA, ein /64 der eigenen
// Schnittstellen) gilt nur die Adresse: sonst koennte jedes Geraet im LAN mit wenigen
// Fehlversuchen alle anderen IPv6-Geraete aussperren.
func (s *Server) sperrStufen(ip string) []sperrStufe {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return []sperrStufe{{ip, maxFehlversuche}}
	}
	a = a.Unmap().WithZone("")
	stufen := []sperrStufe{{a.String(), maxFehlversuche}}
	if a.Is6() && !s.eigenesNetz(a) {
		if p, err := a.Prefix(64); err == nil {
			stufen = append(stufen, sperrStufe{p.String(), maxFehlversucheNetz})
		}
	}
	return stufen
}

// eigenesNetz: die IPv6-Adresse liegt im lokalen Netz des Servers.
func (s *Server) eigenesNetz(a netip.Addr) bool {
	if a.IsLoopback() || a.IsLinkLocalUnicast() || a.IsPrivate() {
		return true
	}
	s.netzMu.Lock()
	if s.netze == nil || time.Since(s.netzeStand) > time.Minute {
		if s.schnittstellen != nil {
			s.netze = s.schnittstellen()
		}
		if s.netze == nil {
			s.netze = []netip.Prefix{}
		}
		s.netzeStand = time.Now()
	}
	netze := s.netze
	s.netzMu.Unlock()
	for _, p := range netze {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// schnittstellenNetze liefert die /64-Netze der eigenen IPv6-Adressen (ohne link-lokale).
func schnittstellenNetze() []netip.Prefix {
	adressen, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []netip.Prefix
	for _, ad := range adressen {
		n, ok := ad.(*net.IPNet)
		if !ok {
			continue
		}
		a, ok := netip.AddrFromSlice(n.IP)
		if !ok || !a.Is6() || a.Is4In6() || a.IsLinkLocalUnicast() || a.IsLoopback() {
			continue
		}
		if p, err := a.Prefix(64); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// versuch ist ein reservierter Anmelde- oder Schluesselversuch einer Adresse.
type versuch struct {
	s      *Server
	f      []*fehlversuch // je Sperrstufe, die eigene Adresse zuerst
	stufen []sperrStufe
	fertig bool
}

const (
	versuchOffen = iota
	versuchFalsch
	versuchRichtig
)

// dauerText: Restdauer einer Sperre, aufgerundet («15 min», «40 s»).
func dauerText(d time.Duration) string {
	if d > time.Minute {
		return strconv.Itoa(int((d+time.Minute-1)/time.Minute)) + " min"
	}
	return strconv.Itoa(max(1, int((d+time.Second-1)/time.Second))) + " s"
}

// versuchBeginnen reserviert einen Versuch, bevor gerechnet wird. Gleichzeitige
// Anfragen zaehlen so schon gegen die Grenze; mehr als die Grenze sind je Sperrfenster
// nicht moeglich. Ohne Reservierung kommt der Grund der Ablehnung (texte.Meldung) zurueck.
func (s *Server) versuchBeginnen(r *http.Request) (*versuch, error) {
	stufen := s.sperrStufen(s.clientIP(r))
	s.mu.Lock()
	defer s.mu.Unlock()
	jetzt := time.Now()
	fs := make([]*fehlversuch, len(stufen))
	for i, st := range stufen {
		f := s.fehler[st.schl]
		if f == nil {
			f = &fehlversuch{}
			s.fehler[st.schl] = f
		}
		f.zuletzt = jetzt
		fs[i] = f
	}
	for _, f := range fs {
		if jetzt.Before(f.bis) {
			return nil, texte.Neu("server.sperre.aktiv", dauerText(f.bis.Sub(jetzt)))
		}
	}
	for i, f := range fs {
		if f.n+f.laufend >= stufen[i].grenze {
			return nil, texte.Neu("server.sperre.gleichzeitig")
		}
	}
	for _, f := range fs {
		f.laufend++
	}
	return &versuch{s: s, f: fs, stufen: stufen}, nil
}

func (v *versuch) beenden(ergebnis int) {
	v.s.mu.Lock()
	defer v.s.mu.Unlock()
	if v.fertig {
		return
	}
	v.fertig = true
	jetzt := time.Now()
	for i, f := range v.f {
		f.laufend--
		f.zuletzt = jetzt
		switch ergebnis {
		case versuchFalsch:
			f.n++
			if g := v.stufen[i].grenze; f.n >= g {
				f.bis = jetzt.Add(sperreDauer)
				f.n = 0
				log.Printf("Anmeldung für %s nach %d Fehlversuchen für %s gesperrt", v.stufen[i].schl, g, sperreDauer)
			}
		case versuchRichtig:
			// Nur den Zaehler der eigenen Adresse zuruecksetzen; eine laufende Sperre bleibt
			// bestehen, und der Zaehler eines fremden Netzes laeuft weiter.
			if i == 0 {
				f.n = 0
			}
		}
	}
}

func (v *versuch) falsch()    { v.beenden(versuchFalsch) }
func (v *versuch) richtig()   { v.beenden(versuchRichtig) }
func (v *versuch) abbrechen() { v.beenden(versuchOffen) }

var errBelegt error = texte.Neu("server.argon.belegt")

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
func argonFehler(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errBelegt) {
		fehlerAus(w, r, http.StatusServiceUnavailable, err)
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
		fehlerAus(w, r, http.StatusBadRequest, err)
		return
	}
	if s.tr.Angelegt() { // schnelle Antwort ohne Warten auf andere Vorgaenge
		fehlerAus(w, r, http.StatusConflict, vault.ErrSchonAngelegt)
		return
	}
	s.kopfMu.Lock()
	defer s.kopfMu.Unlock()
	if s.tr.Angelegt() {
		fehlerAus(w, r, http.StatusConflict, vault.ErrSchonAngelegt)
		return
	}
	var kopf []byte
	var sk string
	var err error
	if e := s.mitArgon(r.Context(), func() { kopf, sk, err = s.tr.Einrichten(strings.TrimSpace(z.Benutzer), z.Passwort) }); e != nil {
		argonFehler(w, r, e)
		return
	}
	if errors.Is(err, vault.ErrSchonAngelegt) {
		fehlerAus(w, r, http.StatusConflict, err)
		return
	}
	if err != nil {
		fehlerAus(w, r, http.StatusBadRequest, err)
		return
	}
	if err := s.st.SetMeta(MetaTresor, kopf); err != nil {
		log.Printf("Einrichtung speichern: %v", err)
		fehlerT(w, r, http.StatusInternalServerError, "server.speichern.fehlgeschlagen")
		return
	}
	log.Printf("Einrichtung abgeschlossen (Benutzer %s, von %s)", s.tr.Benutzer(), s.clientIP(r))
	s.anmeldungStarten(w)
	jsonAntwort(w, http.StatusOK, map[string]string{"sicherheitsschluessel": sk})
}

func (s *Server) anmelden(w http.ResponseWriter, r *http.Request) {
	v, grund := s.versuchBeginnen(r)
	if v == nil {
		fehlerAus(w, r, http.StatusTooManyRequests, grund)
		return
	}
	defer v.abbrechen()
	var z zugangsdaten
	if err := lesen(r, &z); err != nil {
		fehlerAus(w, r, http.StatusBadRequest, err)
		return
	}
	var err error
	if e := s.mitArgon(r.Context(), func() { err = s.tr.Anmelden(strings.TrimSpace(z.Benutzer), z.Passwort) }); e != nil {
		argonFehler(w, r, e)
		return
	}
	if errors.Is(err, vault.ErrNichtAngelegt) {
		fehlerAus(w, r, http.StatusConflict, err)
		return
	}
	if err != nil {
		v.falsch()
		log.Printf("Fehlgeschlagene Anmeldung von %s", s.clientIP(r))
		fehlerAus(w, r, http.StatusUnauthorized, err)
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
		fehlerAus(w, r, http.StatusTooManyRequests, grund)
		return
	}
	defer v.abbrechen()
	var z zugangsdaten
	if err := lesen(r, &z); err != nil {
		fehlerAus(w, r, http.StatusBadRequest, err)
		return
	}
	s.kopfMu.Lock()
	defer s.kopfMu.Unlock()
	var kopf []byte
	var err error
	if e := s.mitArgon(r.Context(), func() { kopf, err = s.tr.Zuruecksetzen(z.Schluessel, strings.TrimSpace(z.Benutzer), z.Passwort) }); e != nil {
		argonFehler(w, r, e)
		return
	}
	if errors.Is(err, vault.ErrSchluesselFalsch) {
		v.falsch()
		log.Printf("Zurücksetzen mit falschem Sicherheitsschlüssel von %s", s.clientIP(r))
		fehlerAus(w, r, http.StatusUnauthorized, err)
		return
	}
	if err != nil {
		fehlerAus(w, r, http.StatusBadRequest, err)
		return
	}
	if err := s.st.SetMeta(MetaTresor, kopf); err != nil {
		log.Printf("Zurücksetzen speichern: %v", err)
		fehlerT(w, r, http.StatusInternalServerError, "server.speichern.fehlgeschlagen")
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
		fehlerAus(w, r, http.StatusTooManyRequests, grund)
		return
	}
	defer v.abbrechen()
	var p struct{ Alt, Neu string }
	if err := lesen(r, &p); err != nil {
		fehlerAus(w, r, http.StatusBadRequest, err)
		return
	}
	s.kopfMu.Lock()
	defer s.kopfMu.Unlock()
	var kopf []byte
	var err error
	if e := s.mitArgon(r.Context(), func() { kopf, err = s.tr.PasswortAendern(p.Alt, p.Neu) }); e != nil {
		argonFehler(w, r, e)
		return
	}
	if errors.Is(err, vault.ErrAltFalsch) {
		v.falsch()
		log.Printf("Passwort ändern mit falschem bisherigem Passwort von %s", s.clientIP(r))
	}
	if err != nil {
		fehlerAus(w, r, http.StatusBadRequest, err)
		return
	}
	if err := s.st.SetMeta(MetaTresor, kopf); err != nil {
		log.Printf("Passwort ändern speichern: %v", err)
		fehlerT(w, r, http.StatusInternalServerError, "server.speichern.fehlgeschlagen")
		return
	}
	v.richtig()
	jsonAntwort(w, http.StatusOK, map[string]bool{"ok": true})
}
