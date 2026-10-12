package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/crypto/ssh"

	"github.com/uiff/ssh-app/internal/sshx"
	"github.com/uiff/ssh-app/internal/store"
	"github.com/uiff/ssh-app/internal/term"
)

// anmeldungFuer entschluesselt den Zugang eines Hosts.
func (s *Server) anmeldungFuer(h store.Host) (sshx.Anmeldung, error) {
	a := sshx.Anmeldung{Benutzer: h.Benutzer}
	if h.ZugangID == 0 {
		return a, nil
	}
	z, err := s.st.Zugang(h.ZugangID)
	if err != nil {
		return a, fmt.Errorf("Zugang von %s: %w", h.Name, err)
	}
	if a.Benutzer == "" {
		a.Benutzer = z.Benutzer
	}
	pw, err := s.tr.Entschluesseln(z.PasswortEnc)
	if err != nil {
		return a, err
	}
	key, err := s.tr.Entschluesseln(z.SchluesselEnc)
	if err != nil {
		return a, err
	}
	pp, err := s.tr.Entschluesseln(z.PassphraseEnc)
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
	Abbrechen bool   `json:"abbrechen,omitempty"`
	// Server -> Browser
	Text   string `json:"text,omitempty"`
	Ziel   string `json:"ziel,omitempty"`
	Typ    string `json:"typ,omitempty"`
	FP     string `json:"fp,omitempty"`
	AltFP  string `json:"altFp,omitempty"`
	Status string `json:"status,omitempty"`
	ID     string `json:"id,omitempty"`
	Titel  string `json:"titel,omitempty"`
	HostID int64  `json:"hostId,omitempty"`
}

type wsVerbindung struct {
	c   *websocket.Conn
	ctx context.Context
	ein chan nachricht // Text-Nachrichten vom Browser
	bin chan []byte    // Tastendruecke
}

func (v *wsVerbindung) senden(n nachricht) error {
	b, _ := json.Marshal(n)
	ctx, cancel := context.WithTimeout(v.ctx, 10*time.Second)
	defer cancel()
	return v.c.Write(ctx, websocket.MessageText, b)
}

// warten liest die naechste Antwort eines bestimmten Typs.
func (v *wsVerbindung) warten(typ string, frist time.Duration) (nachricht, error) {
	t := time.NewTimer(frist)
	defer t.Stop()
	for {
		select {
		case n, ok := <-v.ein:
			if !ok {
				return n, errors.New("Browser getrennt")
			}
			if n.T == typ {
				return n, nil
			}
		case <-t.C:
			return nachricht{}, errors.New("Keine Antwort — abgebrochen")
		case <-v.ctx.Done():
			return nachricht{}, v.ctx.Err()
		}
	}
}

func (s *Server) ws(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil) // prueft Origin == Host
	if err != nil {
		return
	}
	c.SetReadLimit(1 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	v := &wsVerbindung{c: c, ctx: ctx, ein: make(chan nachricht, 8), bin: make(chan []byte, 64)}
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
	if id := q.Get("sitzung"); id != "" {
		var ok bool
		if sitzung, ok = s.tm.Hole(id); !ok {
			v.senden(nachricht{T: "ende", Text: "Sitzung existiert nicht mehr"})
			c.Close(websocket.StatusNormalClosure, "")
			return
		}
	} else {
		hostID, _ := strconv.ParseInt(q.Get("host"), 10, 64)
		spalten, _ := strconv.Atoi(q.Get("c"))
		zeilen, _ := strconv.Atoi(q.Get("r"))
		sitzung, err = s.sitzungAufbauen(v, hostID, spalten, zeilen)
		if err != nil {
			v.senden(nachricht{T: "fehler", Text: err.Error()})
			c.Close(websocket.StatusNormalClosure, "")
			return
		}
	}

	info := sitzung.Info()
	v.senden(nachricht{T: "bereit", ID: info.ID, Titel: info.Titel, HostID: info.HostID, FP: info.Fingerprint})

	// Ausgabe der Shell -> Browser
	puffer, beendet, grund, loesen := sitzung.Anhaengen(func(b []byte) bool {
		if b == nil {
			v.senden(nachricht{T: "ende", Text: "Sitzung beendet"})
			cancel()
			return false
		}
		wctx, wc := context.WithTimeout(ctx, 15*time.Second)
		defer wc()
		return c.Write(wctx, websocket.MessageBinary, b) == nil
	})
	defer loesen()
	if len(puffer) > 0 {
		c.Write(ctx, websocket.MessageBinary, puffer)
	}
	if beendet {
		v.senden(nachricht{T: "ende", Text: grund})
		c.Close(websocket.StatusNormalClosure, "")
		return
	}

	for {
		select {
		case <-ctx.Done():
			c.Close(websocket.StatusNormalClosure, "")
			return
		case b, ok := <-v.bin:
			if !ok {
				return
			}
			if err := sitzung.Eingabe(b); err != nil {
				return
			}
		case n, ok := <-v.ein:
			if !ok {
				return
			}
			switch n.T {
			case "groesse":
				sitzung.Groesse(n.C, n.R)
			case "ping":
				v.senden(nachricht{T: "pong"})
			}
		}
	}
}

func (s *Server) sitzungAufbauen(v *wsVerbindung, hostID int64, spalten, zeilen int) (*term.Sitzung, error) {
	h, err := s.st.Host(hostID)
	if err != nil {
		return nil, errors.New("Host nicht gefunden")
	}
	ziel, jump, err := s.zielFuer(h, nil)
	if err != nil {
		return nil, err
	}
	// Fehlende Angaben im Browser erfragen (Benutzer und/oder Passwort).
	if ziel.Anmeldung.Benutzer == "" || !hatGeheimnis(ziel.Anmeldung) {
		v.senden(nachricht{T: "anmeldung", Benutzer: ziel.Anmeldung.Benutzer, Ziel: h.Name})
		n, err := v.warten("anmeldung", 5*time.Minute)
		if err != nil {
			return nil, err
		}
		if n.Abbrechen {
			return nil, errors.New("Abgebrochen")
		}
		if ziel.Anmeldung.Benutzer == "" {
			ziel.Anmeldung.Benutzer = n.Benutzer
		}
		if !hatGeheimnis(ziel.Anmeldung) {
			ziel.Anmeldung.Passwort = n.Passwort
		}
		if ziel.Anmeldung.Benutzer == "" {
			return nil, errors.New("Kein Benutzername angegeben")
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
		return s.st.BekanntSetzen(store.BekannterHost{Ziel: zielHP, Typ: key.Type(), Schluessel: neuB64, Fingerprint: neuFP})
	}

	v.senden(nachricht{T: "status", Text: "Verbinde mit " + ziel.HostPort() + " …"})
	ctx, cancel := context.WithTimeout(v.ctx, 6*time.Minute) // Rueckfragen inklusive
	defer cancel()
	client, schliessen, err := sshx.Verbinden(ctx, ziel, jump, pruefer)
	if err != nil {
		return nil, err
	}
	titel := h.Name
	if titel == "" {
		titel = h.Adresse
	}
	sitzung, err := s.tm.Starten(term.Start{HostID: h.ID, Titel: titel, Fingerprint: fp, Client: client, Schliessen: schliessen,
		Spalten: spalten, Zeilen: zeilen, PfadFolgen: h.PfadFolgen})
	if err != nil {
		return nil, fmt.Errorf("Terminal öffnen: %w", err)
	}
	log.Printf("Sitzung %s: %s@%s", sitzung.ID()[:6], ziel.Anmeldung.Benutzer, ziel.HostPort())
	return sitzung, nil
}

func (s *Server) sitzungen(w http.ResponseWriter, r *http.Request) {
	jsonAntwort(w, http.StatusOK, s.tm.Liste())
}

func (s *Server) sitzungBeenden(w http.ResponseWriter, r *http.Request) {
	s.tm.Entfernen(r.PathValue("id"))
	jsonAntwort(w, http.StatusOK, map[string]bool{"ok": true})
}
