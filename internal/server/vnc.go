package server

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

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

// vncWerte: Fristen und Grenzen der laufenden Verbindung.
type vncWerte struct {
	// schreibFrist: so lange darf das Schreiben zum VNC-Server ohne Fortschritt haengen
	// (je Block von vncSchreibBlock), dann trennt Keyway. Ein Waechter statt
	// SetWriteDeadline, denn der SSH-Kanal des Tunnels kennt keine Fristen.
	schreibFrist time.Duration
	// browserFrist: so lange darf das Schreiben eines Bildblocks an den Browser dauern.
	browserFrist time.Duration
	// stauGrenze: so viele Bytes des Browsers duerfen auf den VNC-Server warten. Mehr
	// heisst: der Server nimmt nichts mehr an; Keyway trennt, statt Speicher anzuhaeufen.
	stauGrenze int
	// nachlauf: so lange schreibt Keyway nach dem Schliessen des Browsers noch, was dieser
	// davor geschickt hat (vncWeiterleiten, nachlaufen), dann schliesst es die Verbindung.
	nachlauf time.Duration
}

// vncGrenzen gilt fuer neue Verbindungen. Atomar, damit Tests sie aendern koennen,
// waehrend Verbindungen anderer Tests noch auslaufen.
var vncGrenzen atomic.Pointer[vncWerte]

func init() {
	vncGrenzen.Store(&vncWerte{schreibFrist: 30 * time.Second, browserFrist: 15 * time.Second, stauGrenze: 16 << 20,
		nachlauf: 2 * time.Second})
}

// vncSchreibBlock: so viel schreibt Keyway am Stueck zum VNC-Server (Fortschritt fuer die Frist).
const vncSchreibBlock = 64 << 10

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
	// WebSocket zu => Verbindung zum VNC-Server (und SSH) zu: vncWeiterleiten schreibt
	// vorher noch, was der Browser davor geschickt hat (hoechstens vncWerte.nachlauf lang).
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
//
// Bedienung (verlaengert die Anmeldung): im eigenen Modus Tasten, Zeiger, Bildschirmgroesse
// und XVP im Datenstrom (vnc.Eingaben), dazu in beiden Modi die Text-Nachricht
// {t:"bedient"} der Seite bei echter Eingabe (Einfuegen der Zwischenablage, Tippen im
// Anmeldedialog des Viewers …). Beim Durchreichen ist der Strom (Anmeldung, evtl.
// verschluesselt) nicht lesbar: dort zaehlen Binaerframes nie, Leerlauf gilt wie beim
// reinen Ansehen.
//
// Die Hauptschleife blockiert nie: das Schreiben zum VNC-Server laeuft in eigener
// Goroutine mit Frist und Speichergrenze (vncWerte), das zum Browser ebenfalls mit Frist.
// So greifen Abmeldung und Abbruch des Browsers immer, und am Ende laeuft keine
// Goroutine weiter. Schliesst der Browser, geht vorher noch zum VNC-Server, was er davor
// geschickt hat (nachlaufen), etwa das SetDesktopSize, mit dem die Seite beim Trennen
// die Aufloesung des Geraets zuruecksetzt.
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
	var eingaben *vnc.Eingaben
	if !durch {
		eingaben = &vnc.Eingaben{}
	}

	g := *vncGrenzen.Load()
	var wg sync.WaitGroup
	defer wg.Wait() // zuletzt: erst schliessen, dann auf die Goroutinen warten

	// Browser -> VNC-Server
	sch := &vncSchreiber{vb: vb, g: g, wecken: make(chan struct{}, 1)}
	schFertig := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(schFertig)
		sch.laufen()
	}()
	defer sch.zu()
	defer vb.schliessen()
	weiter := func(b []byte) {
		if eingaben != nil && eingaben.Pruefen(b) {
			v.bedient()
		}
		if !sch.einreihen(b) && !sch.haengt.Swap(true) {
			log.Printf("VNC %s: der Server nimmt keine Daten an (%d MB warten) — getrennt", vb.titel, g.stauGrenze>>20)
			vb.schliessen() // der Lesende meldet das Ende
		}
	}
	if len(strom.rest) > 0 {
		weiter(strom.rest)
		strom.rest = nil
	}
	// Der Browser ist weg (oder die Anfrage endet): Was er davor noch geschickt hat, liegt in
	// v.bin oder in der Schlange und geht noch zum VNC-Server, hoechstens g.nachlauf lang.
	// Danach schliessen die defers die Verbindung. Das zaehlt nicht mehr als Bedienung.
	nachlaufen := func() {
		frist := time.NewTimer(g.nachlauf)
		defer frist.Stop()
		// Die Lese-Goroutine schliesst v.bin und v.ein, sobald der Browser weg ist.
		for bin, ein := v.bin, v.ein; bin != nil || ein != nil; {
			select {
			case b, ok := <-bin:
				if !ok {
					bin = nil
				} else if !sch.einreihen(b) {
					return // Stau: der Server nimmt ohnehin nichts an
				}
			case _, ok := <-ein:
				if !ok {
					ein = nil
				}
			case <-frist.C:
				return
			}
		}
		sch.zu()
		select {
		case <-schFertig:
		case <-frist.C:
		}
	}

	// VNC-Server -> Browser
	ende := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 64<<10)
		for {
			n, err := vb.conn.Read(buf)
			if n > 0 {
				wctx, wc := context.WithTimeout(ctx, g.browserFrist)
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

	pruef := time.NewTicker(s.pruefAbstand())
	defer pruef.Stop()
	for {
		select {
		case <-ctx.Done():
			nachlaufen()
			c.Close(websocket.StatusNormalClosure, "")
			return
		case err := <-ende:
			if errors.Is(err, errBrowserWeg) || ctx.Err() != nil {
				// Der Browser ist weg oder liest nicht mehr (das Schreiben an ihn scheiterte
				// womoeglich vor dem Schliessen der WebSocket).
				nachlaufen()
				c.Close(websocket.StatusGoingAway, "")
				return
			}
			grund := "vnc.ende.server"
			if sch.haengt.Load() {
				grund = "vnc.ende.haengt"
			} else if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				log.Printf("VNC %s: %v", vb.titel, err)
			}
			v.senden(nachricht{T: "ende", Text: v.t(grund)})
			c.Close(websocket.StatusNormalClosure, "")
			return
		case <-pruef.C:
			if !s.tokenGueltig(v.token, false) {
				v.beenden(nachricht{T: "abgemeldet"}, wsAbgemeldet, "abgemeldet")
				return
			}
		case b, ok := <-v.bin:
			if !ok {
				nachlaufen()
				return
			}
			weiter(b)
		case n, ok := <-v.ein:
			if !ok {
				nachlaufen()
				return
			}
			vncText(v, n)
		}
	}
}

// vncText beantwortet die Text-Nachrichten der Seite nach «bereit»: ping und bedient.
func vncText(v *wsVerbindung, n nachricht) {
	switch n.T {
	case "ping":
		v.senden(nachricht{T: "pong"})
	case "bedient":
		v.bedient()
	}
}

// vncSchreiber schreibt die Daten des Browsers in eigener Goroutine zum VNC-Server, damit
// ein Server, der nichts mehr annimmt, die Hauptschleife nicht aufhaelt. Was wartet, ist
// auf g.stauGrenze begrenzt; jeder Block muss innert g.schreibFrist weiterkommen.
type vncSchreiber struct {
	vb     *vncVerbindung
	g      vncWerte
	wecken chan struct{} // Platz 1: neue Daten oder Ende
	haengt atomic.Bool   // Keyway hat getrennt, weil der Server nichts annahm

	mu       sync.Mutex
	schlange [][]byte
	stau     int  // Bytes in der Schlange und im laufenden Schreiben (mit Zuschlag je Block)
	fertig   bool // zu(): nach dem Leeren der Schlange aufhoeren
	kaputt   bool // Schreiben ging nicht mehr: alles Weitere verwerfen
}

// vncBlockZuschlag: so viel zaehlt jeder Block zusaetzlich zur Laenge (viele kleine Bloecke).
const vncBlockZuschlag = 64

// einreihen gibt einen Block zum Schreiben weiter. false: Stau, der Server nimmt nichts an.
func (sw *vncSchreiber) einreihen(b []byte) bool {
	sw.mu.Lock()
	switch {
	case sw.kaputt:
		sw.mu.Unlock()
		return true // die Verbindung ist schon zu; der Lesende meldet das Ende
	case sw.stau+len(b)+vncBlockZuschlag > sw.g.stauGrenze:
		sw.mu.Unlock()
		return false
	}
	sw.stau += len(b) + vncBlockZuschlag
	sw.schlange = append(sw.schlange, b)
	sw.mu.Unlock()
	sw.wach()
	return true
}

func (sw *vncSchreiber) wach() {
	select {
	case sw.wecken <- struct{}{}:
	default:
	}
}

// zu beendet den Schreiber, sobald die Schlange leer ist (oder das Schreiben scheitert).
func (sw *vncSchreiber) zu() {
	sw.mu.Lock()
	sw.fertig = true
	sw.mu.Unlock()
	sw.wach()
}

func (sw *vncSchreiber) laufen() {
	for {
		sw.mu.Lock()
		arbeit, fertig := sw.schlange, sw.fertig
		sw.schlange = nil
		sw.mu.Unlock()
		if len(arbeit) == 0 {
			if fertig {
				return
			}
			<-sw.wecken
			continue
		}
		for i, b := range arbeit {
			err := sw.schreiben(b)
			arbeit[i] = nil
			sw.mu.Lock()
			sw.stau -= len(b) + vncBlockZuschlag
			if err != nil {
				sw.kaputt, sw.schlange, sw.stau = true, nil, 0
			}
			sw.mu.Unlock()
			if err != nil {
				sw.vb.schliessen() // der Lesende meldet das Ende
				return
			}
		}
	}
}

// schreiben schreibt einen Block in Stuecken; haengt ein Stueck laenger als
// g.schreibFrist, schliesst der Waechter die Verbindung (auch den SSH-Kanal).
func (sw *vncSchreiber) schreiben(b []byte) error {
	frist := sw.g.schreibFrist
	for len(b) > 0 {
		n := min(len(b), vncSchreibBlock)
		waechter := time.AfterFunc(frist, func() {
			log.Printf("VNC %s: der Server nimmt seit %s keine Daten an — getrennt", sw.vb.titel, frist)
			sw.haengt.Store(true)
			sw.vb.schliessen()
		})
		_, err := sw.vb.conn.Write(b[:n])
		waechter.Stop()
		if err != nil {
			return err
		}
		b = b[n:]
	}
	return nil
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
				vncText(st.v, n)
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

// --- Schluessel des VNC-Servers beim Durchreichen (RA2, Trust on First Use) ---
//
// Meldet sich der Viewer im Browser selbst an (Durchreichen, z. B. RealVNC mit RA2), zeigt
// der Server ihm seinen oeffentlichen Schluessel. Die Seite bildet den Fingerabdruck und
// merkt ihn hier wie einen SSH-Host-Schluessel: in «bekannt» unter "vnc:<adresse>:<port>"
// (im Backup enthalten, unter «Bekannte Hosts» loeschbar). Ein anderer Schluessel beim
// naechsten Mal ist erkennbar (status «geaendert») und wird nur mit «ersetzen» gespeichert.
//
//	GET    /api/hosts/{id}/vnc-schluessel[?typ=…&fingerprint=…]
//	POST   /api/hosts/{id}/vnc-schluessel  {typ, fingerprint, ersetzen}
//	DELETE /api/hosts/{id}/vnc-schluessel

// vncSchluesselAntwort: der gemerkte Schluessel und, wenn ein Fingerabdruck mitkam, der Vergleich.
type vncSchluesselAntwort struct {
	Ziel        string     `json:"ziel"`
	Bekannt     bool       `json:"bekannt"`
	Typ         string     `json:"typ,omitempty"`
	Fingerprint string     `json:"fingerprint,omitempty"`
	Seit        *time.Time `json:"seit,omitempty"`
	// Status: "neu" (nichts gemerkt), "gleich" oder "geaendert" (AltTyp/AltFingerprint:
	// der bisher gemerkte). Bei GET nur mit fingerprint in der Anfrage.
	Status         string `json:"status,omitempty"`
	AltTyp         string `json:"altTyp,omitempty"`
	AltFingerprint string `json:"altFingerprint,omitempty"`
	Fehler         string `json:"fehler,omitempty"` // nur bei 409
}

// vncSchluesselEingabe: was die Seite vom Schluessel des VNC-Servers meldet.
type vncSchluesselEingabe struct {
	Typ         string `json:"typ"`         // z. B. "RSA"
	Fingerprint string `json:"fingerprint"` // so, wie die Seite ihn zeigt
	Ersetzen    bool   `json:"ersetzen"`    // einen anderen gemerkten Schluessel ersetzen
}

// vncSchluesselMu: Vergleichen und Merken eines VNC-Schluessels am Stueck.
var vncSchluesselMu sync.Mutex

// vncSchluesselZiel: das Ziel in «bekannt» fuer den VNC-Server eines Hosts.
func vncSchluesselZiel(h store.Host) string {
	port := h.VncPort
	if port < 1 || port > 65535 {
		port = store.VncPortVorgabe
	}
	return store.BekanntVncPraefix + vnc.HostPort(h.Adresse, port)
}

// vncSchluesselAngabe prueft Typ und Fingerabdruck: nicht leer, kurz, ohne Steuerzeichen.
func vncSchluesselAngabe(typ, fp string) (string, string, bool) {
	typ, fp = strings.TrimSpace(typ), strings.TrimSpace(fp)
	gut := func(s string, n int) bool {
		return s != "" && len(s) <= n && utf8.ValidString(s) &&
			!strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) })
	}
	return typ, fp, gut(typ, 40) && gut(fp, 256)
}

// vncSchluesselHost liest den Host der Anfrage (404, wenn es ihn nicht gibt).
func (s *Server) vncSchluesselHost(w http.ResponseWriter, r *http.Request) (store.Host, bool) {
	h, err := s.st.Host(pfadID(r))
	if err != nil {
		speicherFehler(w, r, err)
		return h, false
	}
	return h, true
}

// vncSchluesselVergleich fuellt Status und den bisher gemerkten Schluessel.
func vncSchluesselVergleich(a *vncSchluesselAntwort, alt store.BekannterHost, gemerkt bool, typ, fp string) {
	switch {
	case !gemerkt:
		a.Status = "neu"
	case alt.Typ == typ && alt.Fingerprint == fp:
		a.Status = "gleich"
	default:
		a.Status, a.AltTyp, a.AltFingerprint = "geaendert", alt.Typ, alt.Fingerprint
	}
}

func (s *Server) vncSchluesselLesen(w http.ResponseWriter, r *http.Request) {
	h, ok := s.vncSchluesselHost(w, r)
	if !ok {
		return
	}
	a := vncSchluesselAntwort{Ziel: vncSchluesselZiel(h)}
	b, err := s.st.Bekannt(a.Ziel)
	gemerkt := err == nil
	if err != nil && !errors.Is(err, store.ErrNichtGefunden) {
		speicherFehler(w, r, err)
		return
	}
	if gemerkt {
		a.Bekannt, a.Typ, a.Fingerprint, a.Seit = true, b.Typ, b.Fingerprint, &b.Seit
	}
	if q := r.URL.Query(); q.Has("fingerprint") {
		typ, fp, gut := vncSchluesselAngabe(q.Get("typ"), q.Get("fingerprint"))
		if !gut {
			fehlerT(w, r, http.StatusBadRequest, "vnc.schluessel.ungueltig")
			return
		}
		vncSchluesselVergleich(&a, b, gemerkt, typ, fp)
	}
	jsonAntwort(w, http.StatusOK, a)
}

func (s *Server) vncSchluesselMerken(w http.ResponseWriter, r *http.Request) {
	var e vncSchluesselEingabe
	if err := lesen(r, &e); err != nil {
		fehlerAus(w, r, http.StatusBadRequest, err)
		return
	}
	typ, fp, gut := vncSchluesselAngabe(e.Typ, e.Fingerprint)
	if !gut {
		fehlerT(w, r, http.StatusBadRequest, "vnc.schluessel.ungueltig")
		return
	}
	h, ok := s.vncSchluesselHost(w, r)
	if !ok {
		return
	}
	a := vncSchluesselAntwort{Ziel: vncSchluesselZiel(h)}
	// Lesen und Schreiben unter einer Sperre: zwei Tabs koennen nicht gegenseitig ersetzen.
	vncSchluesselMu.Lock()
	defer vncSchluesselMu.Unlock()
	alt, err := s.st.Bekannt(a.Ziel)
	gemerkt := err == nil
	if err != nil && !errors.Is(err, store.ErrNichtGefunden) {
		speicherFehler(w, r, err)
		return
	}
	vncSchluesselVergleich(&a, alt, gemerkt, typ, fp)
	switch a.Status {
	case "gleich":
		a.Bekannt, a.Typ, a.Fingerprint, a.Seit = true, alt.Typ, alt.Fingerprint, &alt.Seit
		jsonAntwort(w, http.StatusOK, a)
		return
	case "geaendert":
		if !e.Ersetzen {
			a.Bekannt, a.Typ, a.Fingerprint, a.Seit = true, alt.Typ, alt.Fingerprint, &alt.Seit
			a.Fehler = texte.T(texte.AusAnfrage(r), "vnc.schluessel.geaendert", kurz(h.Name, 60))
			jsonAntwort(w, http.StatusConflict, a)
			return
		}
	}
	if err := s.st.BekanntSetzen(store.BekannterHost{Ziel: a.Ziel, Typ: typ, Schluessel: fp, Fingerprint: fp}); err != nil {
		speicherFehler(w, r, err)
		return
	}
	if a.Status == "geaendert" {
		log.Printf("VNC-Schlüssel von %s ersetzt (%s %s -> %s %s)", a.Ziel, alt.Typ, alt.Fingerprint, typ, fp)
	} else {
		log.Printf("VNC-Schlüssel von %s gemerkt (%s %s)", a.Ziel, typ, fp)
	}
	neu, err := s.st.Bekannt(a.Ziel)
	if err != nil {
		speicherFehler(w, r, err)
		return
	}
	a.Bekannt, a.Typ, a.Fingerprint, a.Seit = true, neu.Typ, neu.Fingerprint, &neu.Seit
	jsonAntwort(w, http.StatusOK, a)
}

func (s *Server) vncSchluesselVergessen(w http.ResponseWriter, r *http.Request) {
	h, ok := s.vncSchluesselHost(w, r)
	if !ok {
		return
	}
	ziel := vncSchluesselZiel(h)
	if err := s.st.BekanntLoeschen(ziel); err != nil {
		speicherFehler(w, r, err)
		return
	}
	log.Printf("VNC-Schlüssel von %s vergessen", ziel)
	jsonAntwort(w, http.StatusOK, map[string]bool{"ok": true})
}
