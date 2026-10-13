package server

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/uiff/ssh-app/internal/store"
	"github.com/uiff/ssh-app/internal/texte"
	"github.com/uiff/ssh-app/internal/vault"
	"github.com/uiff/ssh-app/internal/vnc"
)

// VNC-Viewer (V2): GET /api/vnc?host=<id>&sprache=<de|en>
//
// Aufbauphase nur mit Text-JSON: Rueckfragen wie beim Terminal (hostkey, anmeldung beim
// Weg ssh) und vnc-passwort; status und fehler. Ziel ist immer die gespeicherte
// Host-Adresse: beim Weg ssh 127.0.0.1:<vncPort> auf dem Host (durch den SSH-Tunnel),
// beim Weg direkt <adresse>:<vncPort>. Keyway meldet sich selbst am VNC-Server an (das
// Passwort bleibt auf dem Server), schickt «bereit» und spielt dann gegenueber dem
// Browser einen RFB-3.8-Server ohne Passwort; ab ClientInit laufen die Binaerframes
// unveraendert durch. Kennt Keyway das Anmeldeverfahren nicht, reicht es ab dem ersten
// Byte durch (durchreichen:true), und der Viewer im Browser meldet sich selbst an.

const (
	// vncLeseGrenze: groesste Nachricht des Browsers (Zwischenablage).
	vncLeseGrenze = 8 << 20
	// vncMaxFehlversuche: so viele falsche VNC-Passwoerter je Verbindung, dann Schluss.
	vncMaxFehlversuche = 5
	// vncBrowserFrist: so lange wartet die Begruessung auf den Viewer im Browser.
	vncBrowserFrist = 30 * time.Second
)

// vncLebenszeichen: so oft schickt Keyway dem Browser einen WebSocket-Ping. Die Seite selbst
// schickt nach «bereit» keinen JSON-ping wie das Terminal; ein Bildschirm, dessen Bild sich
// nicht aendert, bliebe sonst still, und Proxys mit 60 s Lesefrist (nginx-Vorgabe) trennten
// ihn. Variable fuer die Tests.
var vncLebenszeichen = 25 * time.Second

// vncVerbindung ist die angemeldete Verbindung zum VNC-Server eines Hosts.
type vncVerbindung struct {
	host        store.Host
	titel       string
	weg         string
	conn        net.Conn
	erg         vnc.Ergebnis
	ssh         *sshVerbindung // nur beim Weg ssh
	gespeichert bool           // erfragte Angaben (SSH-Zugang oder VNC-Passwort) gespeichert
	einmal      sync.Once
}

func (vb *vncVerbindung) schliessen() {
	vb.einmal.Do(func() {
		if vb.conn != nil {
			vb.conn.Close()
		}
		if vb.ssh != nil {
			vb.ssh.schliessen()
		}
	})
}

func (s *Server) vncWS(w http.ResponseWriter, r *http.Request) {
	v, cancel, ok := s.wsAnnehmen(w, r, vncLeseGrenze)
	if !ok {
		return
	}
	defer cancel()
	defer v.lebenszeichen(vncLebenszeichen)()
	hostID, _ := strconv.ParseInt(r.URL.Query().Get("host"), 10, 64)
	vb, err := s.vncAufbauen(v, hostID)
	if errors.Is(err, errAbgemeldet) {
		v.beenden(nachricht{T: "abgemeldet"}, wsAbgemeldet, "abgemeldet")
		return
	}
	if err != nil {
		v.senden(nachricht{T: "fehler", Text: fehlerText(v.sprache, err)})
		v.c.Close(websocket.StatusNormalClosure, "")
		return
	}
	// WebSocket zu => Verbindung zum VNC-Server (und SSH) zu, auch mitten im Schreiben.
	stop := context.AfterFunc(v.ctx, vb.schliessen)
	defer stop()
	defer vb.schliessen()
	s.vncWeiterleiten(v, vb)
}

// vncAufbauen verbindet mit dem VNC-Server eines Hosts und meldet Keyway dort an.
func (s *Server) vncAufbauen(v *wsVerbindung, hostID int64) (*vncVerbindung, error) {
	h, err := s.st.Host(hostID)
	if err != nil {
		return nil, texte.Neu("server.host.unbekannt")
	}
	vb := &vncVerbindung{host: h, titel: h.Name, weg: h.VncWeg}
	if vb.titel == "" {
		vb.titel = h.Adresse
	}
	if !h.VNC {
		return nil, texte.Neu("vnc.aus", vb.titel)
	}
	port := h.VncPort
	if port < 1 || port > 65535 {
		port = store.VncPortVorgabe
	}
	// Ziel nur aus dem gespeicherten Host: keine Angabe des Browsers lenkt die Verbindung um.
	var waehlen func(ctx context.Context) (net.Conn, error)
	ziel := vnc.HostPort(h.Adresse, port)
	if vb.weg == store.VncWegDirekt {
		v.senden(nachricht{T: "status", Text: v.t("vnc.verbinde", ziel)})
		waehlen = func(ctx context.Context) (net.Conn, error) { return vnc.Waehlen(ctx, h.Adresse, port) }
	} else {
		vb.weg = store.VncWegSSH
		sv, err := s.sshAufbauen(v, h)
		if err != nil {
			return nil, err
		}
		vb.ssh = &sv
		vb.gespeichert = sv.gespeichert
		v.senden(nachricht{T: "status", Text: v.t("vnc.tunnel.oeffnen", port)})
		waehlen = func(ctx context.Context) (net.Conn, error) { return vnc.Tunnel(ctx, sv.client, vb.titel, port) }
	}
	if err := s.vncAnmelden(v, vb, waehlen, ziel); err != nil {
		vb.schliessen()
		return nil, err
	}
	return vb, nil
}

// vncAnmelden oeffnet die Verbindung zum VNC-Server und meldet Keyway an. Fehlt das
// Passwort oder ist es falsch, fragt es den Browser (vnc-passwort) und verbindet neu.
// Ein erfragtes Passwort speichert es nur nach erfolgreicher Anmeldung mit «speichern».
func (s *Server) vncAnmelden(v *wsVerbindung, vb *vncVerbindung, waehlen func(context.Context) (net.Conn, error), ziel string) error {
	var passwort *string
	if len(vb.host.VncPasswortEnc) > 0 {
		klar, err := s.tr.Entschluesseln(vb.host.VncPasswortEnc)
		if err != nil {
			if !errors.Is(err, vault.ErrGesperrt) {
				log.Printf("VNC-Passwort von «%s» entschlüsseln: %v", vb.titel, err)
			}
			return texte.Neu("vnc.passwort.unlesbar", vb.titel).MitUrsache(err)
		}
		p := string(klar)
		passwort = &p
	}
	gefragt, speichern, fehlversuche := false, false, 0
	for {
		conn, err := waehlen(v.ctx)
		if err != nil {
			return err
		}
		erg, err := vnc.Anmelden(v.ctx, conn, ziel, passwort)
		if err == nil {
			vb.conn, vb.erg = conn, erg
			break
		}
		conn.Close()
		falsch := errors.Is(err, vnc.ErrPasswortFalsch)
		if !falsch && !errors.Is(err, vnc.ErrPasswortNoetig) {
			return err
		}
		frage := nachricht{T: "vnc-passwort", Ziel: vb.titel}
		if falsch {
			if fehlversuche++; fehlversuche >= vncMaxFehlversuche {
				return texte.Neu("vnc.passwort.zu.oft")
			}
			frage.Fehler = v.t("vnc.passwort.falsch")
		}
		v.senden(frage)
		n, err := v.warten("vnc-passwort", 5*time.Minute)
		if err != nil {
			return err
		}
		if n.Abbrechen {
			return texte.Neu("vnc.passwort.abgebrochen")
		}
		p := n.Passwort
		passwort, gefragt, speichern = &p, true, n.Speichern
	}
	log.Printf("VNC %s: RFB %s (Server %s), Typ %d, angeboten %v, durchreichen %v", ziel, vb.erg.Version, vb.erg.ServerVersion,
		vb.erg.Typ, vb.erg.Typen, vb.erg.Durchreichen)
	if gefragt && speichern && vb.erg.Typ == vnc.TypVNC && s.vncPasswortMerken(vb.host, *passwort) {
		vb.gespeichert = true
	}
	return nil
}

// vncPasswortMerken speichert ein erfragtes VNC-Passwort verschluesselt am Host.
func (s *Server) vncPasswortMerken(h store.Host, passwort string) bool {
	if passwort == "" {
		return false
	}
	enc, err := s.tr.Verschluesseln([]byte(passwort))
	if err != nil {
		log.Printf("VNC-Passwort für «%s» verschlüsseln: %v", h.Name, err)
		return false
	}
	if err := s.st.HostVncPasswortSetzen(h.ID, enc); err != nil {
		log.Printf("VNC-Passwort für «%s» speichern: %v", h.Name, err)
		return false
	}
	log.Printf("VNC-Passwort für «%s» gespeichert", h.Name)
	return true
}

// errBrowserWeg: Schreiben an den Browser ging nicht mehr.
var errBrowserWeg = errors.New("Browser antwortet nicht")

// vncWeiterleiten meldet «bereit», begruesst den Viewer im Browser und reicht dann die
// Daten in beide Richtungen durch, bis eine Seite schliesst.
func (s *Server) vncWeiterleiten(v *wsVerbindung, vb *vncVerbindung) {
	c, ctx := v.c, v.ctx
	durch := vb.erg.Durchreichen
	v.senden(nachricht{T: "bereit", Titel: vb.titel, HostID: vb.host.ID, Weg: vb.weg, Durchreichen: &durch,
		FP: vb.sshFP(), Gespeichert: vb.gespeichert})

	strom := &wsStrom{v: v, frist: vncBrowserFrist}
	var err error
	if durch {
		err = vnc.BrowserDurchreichen(strom, vb.erg)
	} else {
		err = vnc.BrowserBegruessen(strom)
	}
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("VNC %s: Begrüssung des Browsers: %v", vb.titel, err)
			var m *texte.Meldung
			if !errors.As(err, &m) {
				err = texte.Neu("vnc.browser.keine.antwort")
			}
			v.senden(nachricht{T: "fehler", Text: fehlerText(v.sprache, err)})
			c.Close(websocket.StatusNormalClosure, "")
		}
		return
	}
	// Bedienung: im eigenen Modus nur Tasten, Zeiger und Co.; beim Durchreichen ist der
	// Datenstrom (Anmeldung, evtl. verschluesselt) nicht lesbar, dort zaehlt jeder Block.
	var eingaben *vnc.Eingaben
	if !durch {
		eingaben = &vnc.Eingaben{}
	}
	weiter := func(b []byte) error {
		if eingaben == nil || eingaben.Pruefen(b) {
			v.bedient()
		}
		_, err := vb.conn.Write(b)
		return err
	}
	if len(strom.rest) > 0 {
		if err := weiter(strom.rest); err != nil {
			v.senden(nachricht{T: "ende", Text: v.t("vnc.ende.server")})
			c.Close(websocket.StatusNormalClosure, "")
			return
		}
		strom.rest = nil
	}

	// VNC-Server -> Browser
	ende := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 64<<10)
		for {
			n, err := vb.conn.Read(buf)
			if n > 0 {
				wctx, wc := context.WithTimeout(ctx, 15*time.Second)
				werr := c.Write(wctx, websocket.MessageBinary, buf[:n])
				wc()
				if werr != nil {
					err = errBrowserWeg
				}
			}
			if err != nil {
				ende <- err
				return
			}
		}
	}()
	defer wg.Wait()
	defer vb.schliessen()

	pruef := time.NewTicker(s.pruefAbstand())
	defer pruef.Stop()
	for {
		select {
		case <-ctx.Done():
			c.Close(websocket.StatusNormalClosure, "")
			return
		case err := <-ende:
			if errors.Is(err, errBrowserWeg) || ctx.Err() != nil {
				c.Close(websocket.StatusGoingAway, "")
				return
			}
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				log.Printf("VNC %s: %v", vb.titel, err)
			}
			v.senden(nachricht{T: "ende", Text: v.t("vnc.ende.server")})
			c.Close(websocket.StatusNormalClosure, "")
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
			if err := weiter(b); err != nil {
				vb.schliessen() // der Lesende meldet das Ende
			}
		case n, ok := <-v.ein:
			if !ok {
				return
			}
			if n.T == "ping" {
				v.senden(nachricht{T: "pong"})
			}
		}
	}
}

// lebenszeichen pingt den Browser in festem Abstand (WebSocket-Ping; der Browser antwortet
// selbst, die Seite merkt nichts davon). Die Pongs liest die Lese-Goroutine von wsAnnehmen.
// Ein ausbleibender Pong beendet nichts: Das Lebenszeichen haelt nur Proxys wach. Die
// zurueckgegebene Funktion haelt es an und wartet, bis es beendet ist.
func (v *wsVerbindung) lebenszeichen(abstand time.Duration) (stop func()) {
	ctx, cancel := context.WithCancel(v.ctx)
	fertig := make(chan struct{})
	go func() {
		defer close(fertig)
		tick := time.NewTicker(abstand)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				pctx, pcancel := context.WithTimeout(ctx, abstand)
				v.c.Ping(pctx)
				pcancel()
			}
		}
	}()
	return func() {
		cancel()
		<-fertig
	}
}

func (vb *vncVerbindung) sshFP() string {
	if vb.ssh == nil {
		return ""
	}
	return vb.ssh.fp
}

// wsStrom liest die Binaerframes des Browsers als Datenstrom und schreibt Binaerframes
// (fuer die Begruessung des Viewers). Was ueber die Begruessung hinaus gelesen wurde,
// bleibt in rest.
type wsStrom struct {
	v     *wsVerbindung
	rest  []byte
	frist time.Duration
}

func (st *wsStrom) Read(p []byte) (int, error) {
	if len(st.rest) == 0 {
		t := time.NewTimer(st.frist)
		defer t.Stop()
		for len(st.rest) == 0 {
			select {
			case b, ok := <-st.v.bin:
				if !ok {
					return 0, io.EOF
				}
				st.rest = b
			case n, ok := <-st.v.ein:
				if !ok {
					return 0, io.EOF
				}
				if n.T == "ping" {
					st.v.senden(nachricht{T: "pong"})
				}
			case <-t.C:
				return 0, texte.Neu("vnc.browser.keine.antwort")
			case <-st.v.ctx.Done():
				return 0, st.v.ctx.Err()
			}
		}
	}
	n := copy(p, st.rest)
	st.rest = st.rest[n:]
	return n, nil
}

func (st *wsStrom) Write(p []byte) (int, error) {
	ctx, cancel := context.WithTimeout(st.v.ctx, 15*time.Second)
	defer cancel()
	if err := st.v.c.Write(ctx, websocket.MessageBinary, p); err != nil {
		return 0, err
	}
	return len(p), nil
}
