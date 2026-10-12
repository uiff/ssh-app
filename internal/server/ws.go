package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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
		return a, fmt.Errorf("Der Zugang von «%s» fehlt — im Host-Dialog einen anderen wählen", h.Name)
	}
	if err != nil {
		log.Printf("Zugang %d lesen: %v", h.ZugangID, err)
		return a, fmt.Errorf("Der Zugang von «%s» lässt sich nicht lesen", h.Name)
	}
	if a.Benutzer == "" {
		a.Benutzer = z.Benutzer
	}
	// Nur deutsche Saetze zum Browser (V6): kaputte Daten melden sich sonst als «cipher: …».
	entschluesseln := func(b []byte) ([]byte, error) {
		k, err := s.tr.Entschluesseln(b)
		if err != nil && !errors.Is(err, vault.ErrGesperrt) {
			log.Printf("Zugang %d entschlüsseln: %v", z.ID, err)
			return nil, fmt.Errorf("Die gespeicherten Zugangsdaten von «%s» lassen sich nicht entschlüsseln", h.Name)
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

// zielFuer baut Ziel und optionalen Jump-Host. extra ergaenzt fehlende Angaben des Ziels.
func (s *Server) zielFuer(h store.Host, extra *sshx.Anmeldung) (sshx.Ziel, *sshx.Ziel, error) {
	a, err := s.anmeldungFuer(h)
	if err != nil {
		return sshx.Ziel{}, nil, err
	}
	if extra != nil {
		if a.Benutzer == "" {
			a.Benutzer = extra.Benutzer
		}
		if !hatGeheimnis(a) {
			a.Passwort = extra.Passwort
		}
	}
	ziel := sshx.Ziel{Adresse: h.Adresse, Port: h.Port, Anmeldung: a}
	if h.JumpID == 0 {
		return ziel, nil, nil
	}
	jh, err := s.st.Host(h.JumpID)
	if err != nil {
		return ziel, nil, fmt.Errorf("Jump-Host nicht gefunden")
	}
	ja, err := s.anmeldungFuer(jh)
	if err != nil {
		return ziel, nil, err
	}
	if !hatGeheimnis(ja) || ja.Benutzer == "" {
		return ziel, nil, fmt.Errorf("Der Jump-Host «%s» braucht einen Zugang mit Benutzer", jh.Name)
	}
	return ziel, &sshx.Ziel{Adresse: jh.Adresse, Port: jh.Port, Anmeldung: ja}, nil
}

// nurBekannte akzeptiert ausschliesslich schon bestaetigte Host-Schluessel (fuer Hintergrund-Aktionen).
func (s *Server) nurBekannte(ziel string, key ssh.PublicKey) error {
	b, err := s.st.Bekannt(ziel)
	if err != nil {
		return fmt.Errorf("Host-Schlüssel von %s ist noch unbekannt — zuerst einmal im Terminal verbinden und bestätigen", ziel)
	}
	if b.Schluessel != base64.StdEncoding.EncodeToString(key.Marshal()) {
		return fmt.Errorf("Host-Schlüssel von %s hat sich geändert — Verbindung abgebrochen", ziel)
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
var errAbgemeldet = errors.New("Abgemeldet")

type wsVerbindung struct {
	s     *Server
	c     *websocket.Conn
	ctx   context.Context
	token string
	ein   chan nachricht // Text-Nachrichten vom Browser
	bin   chan []byte    // Tastendruecke

	zuletztBedient time.Time
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
				return n, errors.New("Browser getrennt")
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
			return nachricht{}, errors.New("Keine Antwort — abgebrochen")
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
func fehlerText(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "Zeitüberschreitung beim Verbindungsaufbau"
	case errors.Is(err, context.Canceled):
		return "Abgebrochen"
	}
	return err.Error()
}

func (s *Server) ws(w http.ResponseWriter, r *http.Request) {
	tok, err := r.Cookie(cookieName) // von angemeldet() geprueft
	if err != nil {
		fehler(w, http.StatusUnauthorized, "Bitte anmelden")
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
	v := &wsVerbindung{s: s, c: c, ctx: ctx, token: tok.Value, ein: make(chan nachricht, 8), bin: make(chan []byte, 64)}
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
			v.senden(nachricht{T: "ende", Text: "Sitzung existiert nicht mehr"})
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
			v.senden(nachricht{T: "fehler", Text: fehlerText(err)})
			c.Close(websocket.StatusNormalClosure, "")
			return
		}
	}

	// Ausgabe der Shell -> Browser. Reihenfolge: «bereit», dann der Puffer, erst
	// danach Live-Ausgabe (Anhang.Freigeben).
	ende := make(chan string, 1)
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
		Ende: func(grund string) {
			select {
			case ende <- grund:
			default:
			}
		},
		Abgeloest: func() { abgeloestEinmal.Do(func() { close(abgeloest) }) },
	})
	defer anhang.Loesen()
	info := sitzung.Info()
	v.senden(nachricht{T: "bereit", ID: info.ID, Titel: info.Titel, HostID: info.HostID, FP: info.Fingerprint,
		Wieder: &wieder, Gespeichert: gespeichert})
	if len(puffer) > 0 {
		wctx, wc := context.WithTimeout(ctx, 15*time.Second)
		c.Write(wctx, websocket.MessageBinary, puffer)
		wc()
	}
	anhang.Freigeben()
	if beendet {
		v.senden(nachricht{T: "ende", Text: grund})
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
			v.senden(nachricht{T: "ende", Text: g})
			c.Close(websocket.StatusNormalClosure, "")
			return
		case <-abgeloest:
			// Ein haengender Schreibvorgang an diesen Browser darf den neuen nicht aufhalten.
			time.AfterFunc(2*time.Second, liveStop)
			v.beenden(nachricht{T: "abgeloest", Text: "In einem anderen Fenster geöffnet"}, wsAbgeloest, "abgeloest")
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
		return nil, false, errors.New("Host nicht gefunden")
	}
	ziel, jump, err := s.zielFuer(h, nil)
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
			return nil, false, errors.New("Abgebrochen")
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
			return nil, false, errors.New("Kein Benutzername angegeben")
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
			return errors.New("Host-Schlüssel nicht bestätigt — Verbindung abgebrochen")
		}
		if status == "geaendert" {
			log.Printf("Host-Schlüssel von %s ersetzt (%s -> %s)", zielHP, b.Fingerprint, neuFP)
		}
		if err := s.st.BekanntSetzen(store.BekannterHost{Ziel: zielHP, Typ: key.Type(), Schluessel: neuB64, Fingerprint: neuFP}); err != nil {
			log.Printf("Host-Schlüssel von %s speichern: %v", zielHP, err)
			return errors.New("Host-Schlüssel konnte nicht gespeichert werden")
		}
		return nil
	}

	v.senden(nachricht{T: "status", Text: "Verbinde mit " + ziel.HostPort() + " …"})
	ctx, cancel := context.WithTimeout(v.ctx, 6*time.Minute) // Rueckfragen inklusive
	defer cancel()
	client, schliessen, err := sshx.Verbinden(ctx, ziel, jump, pruefer)
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

// zugangsdatenMerken speichert beim Verbinden erfragte Angaben: mit Passwort als
// neuen Zugang des Hosts (Name = Hostname), sonst nur den Benutzer am Host.
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
	if err := s.st.HostZugangSetzen(h.ID, &z); err != nil {
		log.Printf("Zugangsdaten für «%s» speichern: %v", name, err)
		return false
	}
	log.Printf("Zugangsdaten für «%s» gespeichert", name)
	return true
}

func (s *Server) sitzungen(w http.ResponseWriter, r *http.Request) {
	jsonAntwort(w, http.StatusOK, s.tm.Liste())
}

func (s *Server) sitzungBeenden(w http.ResponseWriter, r *http.Request) {
	s.tm.Entfernen(r.PathValue("id"))
	jsonAntwort(w, http.StatusOK, map[string]bool{"ok": true})
}
