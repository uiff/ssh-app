package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/crypto/ssh"

	"github.com/uiff/ssh-app/internal/sshx"
	"github.com/uiff/ssh-app/internal/store"
	"github.com/uiff/ssh-app/internal/term"
	"github.com/uiff/ssh-app/internal/texte"
	"github.com/uiff/ssh-app/internal/vault"
)

// anmeldungFuer entschluesselt den Zugang eines Hosts.
func (s *Server) anmeldungFuer(h store.Host) (sshx.Anmeldung, error) {
	a := sshx.Anmeldung{Benutzer: h.Benutzer}
	if h.ZugangID == 0 {
		return a, nil
	}
	z, err := s.st.Zugang(h.ZugangID)
	if errors.Is(err, store.ErrNichtGefunden) {
		return a, texte.Neu("server.zugang.fehlt", h.Name)
	}
	if err != nil {
		log.Printf("Zugang %d lesen: %v", h.ZugangID, err)
		return a, texte.Neu("server.zugang.unlesbar", h.Name).MitUrsache(err)
	}
	if a.Benutzer == "" {
		a.Benutzer = z.Benutzer
	}
	// Nur verstaendliche Saetze zum Browser (V6): kaputte Daten melden sich sonst als «cipher: …».
	entschluesseln := func(b []byte) ([]byte, error) {
		k, err := s.tr.Entschluesseln(b)
		if err != nil && !errors.Is(err, vault.ErrGesperrt) {
			log.Printf("Zugang %d entschlüsseln: %v", z.ID, err)
			return nil, texte.Neu("server.zugang.entschluesseln", h.Name).MitUrsache(err)
		}
		return k, err
	}
	pw, err := entschluesseln(z.PasswortEnc)
	if err != nil {
		return a, err
	}
	key, err := entschluesseln(z.SchluesselEnc)
	if err != nil {
		return a, err
	}
	pp, err := entschluesseln(z.PassphraseEnc)
	if err != nil {
		return a, err
	}
	a.Passwort, a.Schluessel, a.Passphrase = string(pw), key, string(pp)
	return a, nil
}

func hatGeheimnis(a sshx.Anmeldung) bool { return a.Passwort != "" || len(a.Schluessel) > 0 }

// zielFuer baut das Ziel eines Hosts. extra ergaenzt fehlende Angaben.
func (s *Server) zielFuer(h store.Host, extra *sshx.Anmeldung) (sshx.Ziel, error) {
	a, err := s.anmeldungFuer(h)
	if err != nil {
		return sshx.Ziel{}, err
	}
	if extra != nil {
		if a.Benutzer == "" {
			a.Benutzer = extra.Benutzer
		}
		if !hatGeheimnis(a) {
			a.Passwort = extra.Passwort
		}
	}
	return sshx.Ziel{Adresse: h.Adresse, Port: h.Port, Anmeldung: a}, nil
}

// nurBekannte akzeptiert ausschliesslich schon bestaetigte Host-Schluessel (fuer Hintergrund-Aktionen).
func (s *Server) nurBekannte(ziel string, key ssh.PublicKey) error {
	b, err := s.st.Bekannt(ziel)
	if err != nil {
		return texte.Neu("server.hostkey.unbekannt", ziel)
	}
	if b.Schluessel != base64.StdEncoding.EncodeToString(key.Marshal()) {
		return texte.Neu("server.hostkey.geaendert", ziel)
	}
	return nil
}

// --- Nachrichten ---

type nachricht struct {
	T string `json:"t"`
	// Terminal
	C int `json:"c,omitempty"`
	R int `json:"r,omitempty"`
	// Rueckfragen
	OK        bool   `json:"ok,omitempty"`
	Benutzer  string `json:"benutzer,omitempty"`
	Passwort  string `json:"passwort,omitempty"`
	Speichern bool   `json:"speichern,omitempty"`
	Abbrechen bool   `json:"abbrechen,omitempty"`
	// Server -> Browser
	Text        string `json:"text,omitempty"`
	Ziel        string `json:"ziel,omitempty"`
	Typ         string `json:"typ,omitempty"`
	FP          string `json:"fp,omitempty"`
	AltFP       string `json:"altFp,omitempty"`
	Status      string `json:"status,omitempty"`
	ID          string `json:"id,omitempty"`
	Titel       string `json:"titel,omitempty"`
	HostID      int64  `json:"hostId,omitempty"`
	Wieder      *bool  `json:"wieder,omitempty"`      // bereit: an eine laufende Sitzung angehaengt
	Gespeichert bool   `json:"gespeichert,omitempty"` // bereit: erfragte Zugangsdaten gespeichert
}

// Schliesscodes des Terminal-WebSockets: der Browser verbindet danach nicht von selbst neu.
const (
	wsAbgemeldet websocket.StatusCode = 4001 // Anmeldung abgelaufen
	wsAbgeloest  websocket.StatusCode = 4002 // ein anderer Browser hat die Sitzung uebernommen
)

// errAbgemeldet: die Anmeldung des WebSockets ist abgelaufen.
var errAbgemeldet error = texte.Neu("server.ws.abgemeldet")

type wsVerbindung struct {
	s       *Server
	c       *websocket.Conn
	ctx     context.Context
	token   string
	sprache string         // Sprache der Meldungen (Parameter «sprache» beim Aufbau)
	ein     chan nachricht // Text-Nachrichten vom Browser
	bin     chan []byte    // Tastendruecke

	zuletztBedient time.Time
}

// t liefert einen Katalogtext in der Sprache dieses Browsers.
func (v *wsVerbindung) t(schluessel string, werte ...any) string {
	return texte.T(v.sprache, schluessel, werte...)
}

func (v *wsVerbindung) senden(n nachricht) error { return v.sendenFrist(n, 10*time.Second) }

func (v *wsVerbindung) sendenFrist(n nachricht, frist time.Duration) error {
	b, _ := json.Marshal(n)
	ctx, cancel := context.WithTimeout(v.ctx, frist)
	defer cancel()
	return v.c.Write(ctx, websocket.MessageText, b)
}

// bedient vermerkt Bedienung (Eingabe, Groesse, Antwort) bei der Anmeldung, damit
// sie nicht im Leerlauf ablaeuft. Hoechstens einmal je Sekunde.
func (v *wsVerbindung) bedient() {
	if time.Since(v.zuletztBedient) < time.Second {
		return
	}
	v.zuletztBedient = time.Now()
	v.s.tokenGueltig(v.token, true)
}

// beenden schickt eine letzte Nachricht und schliesst mit code.
func (v *wsVerbindung) beenden(n nachricht, code websocket.StatusCode, grund string) {
	v.sendenFrist(n, 2*time.Second)
	v.c.Close(code, grund)
}

// warten liest die naechste Antwort eines bestimmten Typs.
func (v *wsVerbindung) warten(typ string, frist time.Duration) (nachricht, error) {
	t := time.NewTimer(frist)
	defer t.Stop()
	pruef := time.NewTicker(v.s.pruefAbstand())
	defer pruef.Stop()
	for {
		select {
		case n, ok := <-v.ein:
			if !ok {
				return n, texte.Neu("server.ws.getrennt")
			}
			if n.T == typ {
				v.bedient()
				return n, nil
			}
		case <-pruef.C:
			if !v.s.tokenGueltig(v.token, false) {
				return nachricht{}, errAbgemeldet
			}
		case <-t.C:
			return nachricht{}, texte.Neu("server.ws.keine.antwort")
		case <-v.ctx.Done():
			return nachricht{}, v.ctx.Err()
		}
	}
}

// originMuster: zusaetzlich erlaubte Origins fuer den WebSocket (SSHAPP_ORIGINS und,
// hinter einem vertrauenswuerdigen Proxy, der weitergereichte Host).
func (s *Server) originMuster(r *http.Request) []string {
	m := s.opt.Origins
	if s.vonProxy(r) {
		fh := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0])
		if fh != "" && !strings.ContainsAny(fh, "*?[]\\/") {
			m = append(slices.Clone(m), fh)
		}
	}
	return m
}

// fehlerText macht aus einem Fehler beim Aufbau einen Satz fuer den Browser.
func fehlerText(sprache string, err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return texte.T(sprache, "server.ws.zeitueberschreitung")
	case errors.Is(err, context.Canceled):
		return texte.Uebersetzen(sprache, sshx.ErrAbgebrochen)
	}
	return texte.Uebersetzen(sprache, err)
}

func (s *Server) ws(w http.ResponseWriter, r *http.Request) {
	tok, err := r.Cookie(cookieName) // von angemeldet() geprueft
	if err != nil {
		fehlerT(w, r, http.StatusUnauthorized, "server.anmelden")
		return
	}
	// Origin muss zum Host passen (oder zu SSHAPP_ORIGINS).
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: s.originMuster(r)})
	if err != nil {
		return
	}
	c.SetReadLimit(1 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	v := &wsVerbindung{s: s, c: c, ctx: ctx, token: tok.Value, sprache: texte.AusAnfrage(r),
		ein: make(chan nachricht, 8), bin: make(chan []byte, 64)}
	go func() {
		defer cancel()
		defer close(v.ein)
		defer close(v.bin)
		for {
			typ, b, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageBinary {
				select {
				case v.bin <- b:
				case <-ctx.Done():
					return
				}
				continue
			}
			var n nachricht
			if json.Unmarshal(b, &n) == nil {
				select {
				case v.ein <- n:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	q := r.URL.Query()
	var sitzung *term.Sitzung
	wieder, gespeichert := false, false
	if id := q.Get("sitzung"); id != "" {
		var ok bool
		if sitzung, ok = s.tm.Hole(id); !ok {
			v.senden(nachricht{T: "ende", Text: v.t("server.sitzung.weg")})
			c.Close(websocket.StatusNormalClosure, "")
			return
		}
		wieder = true
	} else {
		hostID, _ := strconv.ParseInt(q.Get("host"), 10, 64)
		spalten, _ := strconv.Atoi(q.Get("c"))
		zeilen, _ := strconv.Atoi(q.Get("r"))
		sitzung, gespeichert, err = s.sitzungAufbauen(v, hostID, spalten, zeilen)
		if errors.Is(err, errAbgemeldet) {
			v.beenden(nachricht{T: "abgemeldet"}, wsAbgemeldet, "abgemeldet")
			return
		}
		if err != nil {
			v.senden(nachricht{T: "fehler", Text: fehlerText(v.sprache, err)})
			c.Close(websocket.StatusNormalClosure, "")
			return
		}
	}

	// Ausgabe der Shell -> Browser. Reihenfolge: «bereit», dann der Puffer, erst
	// danach Live-Ausgabe (Anhang.Freigeben).
	ende := make(chan *texte.Meldung, 1)
	abgeloest := make(chan struct{})
	var abgeloestEinmal sync.Once
	liveCtx, liveStop := context.WithCancel(ctx)
	defer liveStop()
	anhang, puffer, beendet, grund := sitzung.Anhaengen(term.Empfaenger{
		Daten: func(b []byte) bool {
			wctx, wc := context.WithTimeout(liveCtx, 15*time.Second)
			defer wc()
			return c.Write(wctx, websocket.MessageBinary, b) == nil
		},
		Ende: func(grund *texte.Meldung) {
			select {
			case ende <- grund:
			default:
			}
		},
		Abgeloest: func() { abgeloestEinmal.Do(func() { close(abgeloest) }) },
		// Die Sitzung endet, und dieser Browser liest nicht mehr: nicht bis zur Frist warten.
		Abbrechen: liveStop,
	})
	defer anhang.Loesen()
	info := sitzung.Info(v.sprache)
	v.senden(nachricht{T: "bereit", ID: info.ID, Titel: info.Titel, HostID: info.HostID, FP: info.Fingerprint,
		Wieder: &wieder, Gespeichert: gespeichert})
	if len(puffer) > 0 {
		wctx, wc := context.WithTimeout(liveCtx, 15*time.Second)
		c.Write(wctx, websocket.MessageBinary, puffer)
		wc()
	}
	anhang.Freigeben()
	if beendet {
		v.senden(nachricht{T: "ende", Text: grundText(v.sprache, grund)})
		c.Close(websocket.StatusNormalClosure, "")
		return
	}

	pruef := time.NewTicker(s.pruefAbstand())
	defer pruef.Stop()
	for {
		select {
		case <-ctx.Done():
			c.Close(websocket.StatusNormalClosure, "")
			return
		case g := <-ende:
			v.senden(nachricht{T: "ende", Text: grundText(v.sprache, g)})
			c.Close(websocket.StatusNormalClosure, "")
			return
		case <-abgeloest:
			// Ein haengender Schreibvorgang an diesen Browser darf den neuen nicht aufhalten.
			time.AfterFunc(2*time.Second, liveStop)
			v.beenden(nachricht{T: "abgeloest", Text: v.t("server.ws.abgeloest")}, wsAbgeloest, "abgeloest")
			return
		case <-pruef.C:
			if !s.tokenGueltig(v.token, false) {
				v.beenden(nachricht{T: "abgemeldet"}, wsAbgemeldet, "abgemeldet")
				return
			}
		case b, ok := <-v.bin:
			if !ok {
				return
			}
			v.bedient()
			if err := anhang.Eingabe(b); err != nil {
				if errors.Is(err, term.ErrAbgeloest) {
					continue // die Abloesung kommt gleich ueber «abgeloest»
				}
				return
			}
		case n, ok := <-v.ein:
			if !ok {
				return
			}
			switch n.T {
			case "groesse":
				v.bedient()
				anhang.Groesse(n.C, n.R)
			case "ping":
				v.senden(nachricht{T: "pong"})
			}
		}
	}
}

// sitzungAufbauen verbindet mit einem Host und oeffnet das Terminal. gespeichert:
// erfragte Zugangsdaten wurden nach erfolgreicher Anmeldung gespeichert.
func (s *Server) sitzungAufbauen(v *wsVerbindung, hostID int64, spalten, zeilen int) (*term.Sitzung, bool, error) {
	h, err := s.st.Host(hostID)
	if err != nil {
		return nil, false, texte.Neu("server.host.unbekannt")
	}
	ziel, err := s.zielFuer(h, nil)
	if err != nil {
		return nil, false, err
	}
	// Fehlende Angaben im Browser erfragen (Benutzer und/oder Passwort).
	var speichern, benutzerGefragt, passwortGefragt bool
	var gefragtesPasswort string
	if ziel.Anmeldung.Benutzer == "" || !hatGeheimnis(ziel.Anmeldung) {
		v.senden(nachricht{T: "anmeldung", Benutzer: ziel.Anmeldung.Benutzer, Ziel: h.Name})
		n, err := v.warten("anmeldung", 5*time.Minute)
		if err != nil {
			return nil, false, err
		}
		if n.Abbrechen {
			return nil, false, texte.Neu("server.ws.anmeldung.abgebrochen")
		}
		speichern = n.Speichern
		if ziel.Anmeldung.Benutzer == "" {
			ziel.Anmeldung.Benutzer = strings.TrimSpace(n.Benutzer)
			benutzerGefragt = true
		}
		if !hatGeheimnis(ziel.Anmeldung) {
			ziel.Anmeldung.Passwort = n.Passwort
			gefragtesPasswort = n.Passwort
			passwortGefragt = true
		}
		if ziel.Anmeldung.Benutzer == "" {
			return nil, false, texte.Neu("server.ws.kein.benutzer")
		}
	}

	var fp string
	pruefer := func(zielHP string, key ssh.PublicKey) error {
		neuB64 := base64.StdEncoding.EncodeToString(key.Marshal())
		neuFP := ssh.FingerprintSHA256(key)
		if zielHP == ziel.HostPort() {
			fp = neuFP
		}
		b, err := s.st.Bekannt(zielHP)
		status := "neu"
		if err == nil {
			if b.Schluessel == neuB64 {
				return nil
			}
			status = "geaendert"
		}
		v.senden(nachricht{T: "hostkey", Ziel: zielHP, Typ: key.Type(), FP: neuFP, AltFP: b.Fingerprint, Status: status})
		antwort, err := v.warten("hostkey", 5*time.Minute)
		if err != nil {
			return err
		}
		if !antwort.OK {
			return texte.Neu("server.hostkey.abgelehnt")
		}
		if status == "geaendert" {
			log.Printf("Host-Schlüssel von %s ersetzt (%s -> %s)", zielHP, b.Fingerprint, neuFP)
		}
		if err := s.st.BekanntSetzen(store.BekannterHost{Ziel: zielHP, Typ: key.Type(), Schluessel: neuB64, Fingerprint: neuFP}); err != nil {
			log.Printf("Host-Schlüssel von %s speichern: %v", zielHP, err)
			return texte.Neu("server.hostkey.speichern")
		}
		return nil
	}

	v.senden(nachricht{T: "status", Text: v.t("server.ws.verbinde", ziel.HostPort())})
	ctx, cancel := context.WithTimeout(v.ctx, 6*time.Minute) // Rueckfragen inklusive
	defer cancel()
	client, schliessen, err := sshx.Verbinden(ctx, ziel, pruefer)
	if err != nil {
		return nil, false, err
	}
	// Erst nach erfolgreicher Anmeldung speichern, was der Browser erfragt hat.
	gespeichert := false
	if speichern && (benutzerGefragt || passwortGefragt) {
		gespeichert = s.zugangsdatenMerken(h, ziel.Anmeldung.Benutzer, gefragtesPasswort, passwortGefragt)
	}
	titel := h.Name
	if titel == "" {
		titel = h.Adresse
	}
	sitzung, err := s.tm.Starten(term.Start{HostID: h.ID, Titel: titel, Fingerprint: fp, Client: client, Schliessen: schliessen,
		Spalten: spalten, Zeilen: zeilen, PfadFolgen: h.PfadFolgen})
	if err != nil {
		return nil, gespeichert, err
	}
	log.Printf("Sitzung %s: %s@%s", sitzung.ID()[:6], ziel.Anmeldung.Benutzer, ziel.HostPort())
	return sitzung, gespeichert, nil
}

// zugangsdatenMerken speichert beim Verbinden erfragte Angaben: mit Passwort im
// eigenen Passwort-Zugang des Hosts oder als neuen Zugang (Name = Hostname), sonst
// nur den Benutzer am Host.
func (s *Server) zugangsdatenMerken(h store.Host, benutzer, passwort string, mitPasswort bool) bool {
	if !mitPasswort {
		if err := s.st.HostBenutzerSetzen(h.ID, benutzer); err != nil {
			log.Printf("Benutzer für «%s» speichern: %v", h.Name, err)
			return false
		}
		return true
	}
	if passwort == "" {
		return false
	}
	enc, err := s.tr.Verschluesseln([]byte(passwort))
	if err != nil {
		log.Printf("Zugangsdaten für «%s» verschlüsseln: %v", h.Name, err)
		return false
	}
	name := h.Name
	if name == "" {
		name = h.Adresse
	}
	z := store.Zugang{Name: name, Benutzer: benutzer, Art: "passwort", PasswortEnc: enc}
	aktualisiert, err := s.st.HostZugangSetzen(h.ID, &z)
	if err != nil {
		log.Printf("Zugangsdaten für «%s» speichern: %v", name, err)
		return false
	}
	if aktualisiert {
		log.Printf("Zugangsdaten für «%s» im Zugang «%s» ergänzt", name, z.Name)
	} else {
		log.Printf("Zugangsdaten für «%s» gespeichert", name)
	}
	return true
}

func (s *Server) sitzungen(w http.ResponseWriter, r *http.Request) {
	jsonAntwort(w, http.StatusOK, s.tm.Liste(texte.AusAnfrage(r)))
}

func (s *Server) sitzungBeenden(w http.ResponseWriter, r *http.Request) {
	s.tm.Entfernen(r.PathValue("id"))
	jsonAntwort(w, http.StatusOK, map[string]bool{"ok": true})
}

// grundText: der Grund des Sitzungsendes fuer den Browser.
func grundText(sprache string, grund *texte.Meldung) string {
	if grund == nil {
		return texte.T(sprache, "term.ende.verbindung")
	}
	return grund.Text(sprache)
}
