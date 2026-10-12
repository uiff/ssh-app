// Package term verwaltet laufende Terminal-Sitzungen. Eine Sitzung lebt auf
// dem Server weiter, wenn der Browser die Verbindung verliert (Funkloch,
// Neuladen), und laesst sich wieder anhaengen; die letzte Ausgabe wird dabei
// aus einem Ringpuffer nachgeliefert.
package term

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/uiff/ssh-app/internal/texte"
)

const (
	pufferGroesse = 256 * 1024
	// Wie lange eine Sitzung ohne angehaengten Browser weiterlaeuft.
	LosgeloestMax = 12 * time.Hour
	// Wie lange eine beendete Sitzung (zum Nachlesen) stehen bleibt.
	BeendetMax = 10 * time.Minute
	// Angefangene Steuersequenz («ESC ]») am Ende eines Stuecks: hoechstens so
	// viele Bytes und so lange zurueckhalten, dann trotzdem ausgeben.
	restMax   = 256
	restFrist = 150 * time.Millisecond
	// So lange wartet Starten auf die PID-Marke des Startbefehls.
	markeFrist = 3 * time.Second
	// Nach einem gescheiterten SFTP-Start wird so lange nicht neu versucht.
	sftpPause = time.Minute
)

// abbruchFrist: So lange wartet Beenden auf einen laufenden Schreibvorgang an den
// Browser, bevor es ihn abbricht (Empfaenger.Abbrechen). Variable fuer Tests.
var abbruchFrist = 2 * time.Second

// pidMarke wird vom Startbefehl ausgegeben und aus dem Datenstrom entfernt.
var pidMarke = regexp.MustCompile(`\x1b\]7770;pid=(\d+)\x07`)

// osc7 meldet das Arbeitsverzeichnis, wenn die Shell es von sich aus sendet.
var osc7 = regexp.MustCompile(`\x1b\]7;file://[^/\x07\x1b]*(/[^\x07\x1b]*)(?:\x07|\x1b\\)`)

// StartBefehl gibt die PID der Shell aus und ersetzt sich dann durch die Login-Shell.
// So kann die Dateileiste /proc/<pid>/cwd lesen und dem Terminal folgen.
//
// sshd fuehrt den Befehl mit der Login-Shell des Benutzers aus ($SHELL -c). Damit
// das auch mit fish, tcsh oder csh klappt, sieht die Login-Shell nur ein einfaches
// «exec /bin/sh -c '…'»: einfache Anfuehrungszeichen ohne ' und ! darin sind in
// allen diesen Shells woertlich. /bin/sh gibt die PID aus (exec behaelt sie) und
// ersetzt sich durch die Login-Shell. Versteht ein Geraet den Befehl trotzdem nicht,
// faellt Starten auf eine normale Shell zurueck.
const StartBefehl = `exec /bin/sh -c 'printf "\033]7770;pid=%s\007" "$$"; exec "${SHELL:-/bin/sh}" -l'`

// Fehler fuer den Anwender (texte.Meldung, Vergleich mit errors.Is).
var (
	// ErrAbgeloest: ein anderer Browser hat die Sitzung uebernommen.
	ErrAbgeloest error = texte.Neu("term.abgeloest")
	// ErrKeinSFTP: der Server bietet (gerade) kein SFTP an.
	ErrKeinSFTP error = texte.Neu("term.kein.sftp")
	// ErrBeendet: die Sitzung ist zu Ende.
	ErrBeendet error = texte.Neu("term.beendet")
)

// Gruende fuer das Ende einer Sitzung (Sitzung.Beenden).
var (
	GrundGeschlossen = texte.Neu("term.ende.geschlossen")
	GrundHerunter    = texte.Neu("term.ende.herunterfahren")
	GrundOhneBrowser = texte.Neu("term.ende.ohne.browser")
	GrundVerbindung  = texte.Neu("term.ende.verbindung")
	GrundAbgemeldet  = texte.Neu("term.ende.abgemeldet")
)

// GrundShell: die Shell endete mit einem Code.
func GrundShell(code int) *texte.Meldung { return texte.Neu("term.ende.shell", code) }

type Info struct {
	ID          string    `json:"id"`
	HostID      int64     `json:"hostId"`
	Titel       string    `json:"titel"`
	Gestartet   time.Time `json:"gestartet"`
	Angehaengt  bool      `json:"angehaengt"`
	Beendet     bool      `json:"beendet"`
	Grund       string    `json:"grund,omitempty"`
	Fingerprint string    `json:"fingerprint,omitempty"`
}

// Empfaenger ist ein angehaengter Browser. Die Rueckrufe duerfen nicht lange blockieren
// (Daten hoechstens bis zu einer Schreibfrist).
type Empfaenger struct {
	// Daten liefert neue Ausgabe; false = Browser nicht mehr erreichbar.
	Daten func([]byte) bool
	// Ende meldet, dass die Sitzung zu Ende ist (nach der letzten Ausgabe).
	Ende func(grund *texte.Meldung)
	// Abgeloest meldet, dass ein anderer Browser die Sitzung uebernommen hat.
	Abgeloest func()
	// Abbrechen (optional) bricht einen haengenden Daten-Aufruf sofort ab. Beenden ruft
	// es, wenn ein Schreibvorgang laenger als abbruchFrist dauert (Browser liest nicht).
	Abbrechen func()
}

// Anhang ist die Verbindung eines Browsers mit einer Sitzung. Live-Ausgabe fliesst
// erst nach Freigeben, damit der Puffer sicher vor ihr beim Browser ankommt.
type Anhang struct {
	s      *Sitzung
	e      Empfaenger
	frei   chan struct{}
	einmal sync.Once
}

type Sitzung struct {
	id, titel   string
	hostID      int64
	gestartet   time.Time
	fingerprint string

	client     *ssh.Client
	schliessen func()
	sess       *ssh.Session
	stdin      io.Writer

	mu        sync.Mutex
	ring      []byte
	startModi modusStand // Terminal-Modi am Anfang des Rings (fuer den Puffer beim Wiederanhaengen)
	anhang    *Anhang    // angehaengter Browser (nil = losgeloest)
	losSeit   time.Time
	endeSeit  time.Time
	beendet   bool
	grund     *texte.Meldung
	pid       int
	cwd       string
	rest      []byte
	restTimer *time.Timer

	// ausgabeMu haelt die Reihenfolge der Ausgabe (Leser, Rest-Frist, Beenden).
	ausgabeMu sync.Mutex

	sftpMu         sync.Mutex
	sftpC          *sftp.Client
	sftpSess       *ssh.Session
	sftpFehlerSeit time.Time
}

// Parameter fuer eine neue Sitzung.
type Start struct {
	HostID      int64
	Titel       string
	Fingerprint string
	Client      *ssh.Client
	Schliessen  func()
	Spalten     int
	Zeilen      int
	PfadFolgen  bool
}

type Manager struct {
	mu      sync.Mutex
	sitzung map[string]*Sitzung
	stop    chan struct{}
	fertig  sync.WaitGroup
}

func NeuerManager() *Manager {
	m := &Manager{sitzung: map[string]*Sitzung{}, stop: make(chan struct{})}
	m.fertig.Add(1)
	go m.aufraeumen()
	return m
}

func (m *Manager) Close() {
	close(m.stop)
	m.fertig.Wait()
	m.mu.Lock()
	alle := make([]*Sitzung, 0, len(m.sitzung))
	for _, s := range m.sitzung {
		alle = append(alle, s)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, s := range alle {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Beenden(GrundHerunter)
		}()
	}
	wg.Wait()
}

func (m *Manager) aufraeumen() {
	defer m.fertig.Done()
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-t.C:
		}
		m.raeumen(time.Now())
	}
}

// raeumen entfernt beendete Sitzungen nach BeendetMax und beendet Sitzungen,
// die zu lange ohne Browser waren.
func (m *Manager) raeumen(jetzt time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sitzung {
		s.mu.Lock()
		los := s.anhang == nil
		beendet := s.beendet
		// Bezug ist das spaetere von Loesen und Ende: eine Sitzung, die lange
		// losgeloest lief und eben erst endet, bleibt trotzdem BeendetMax stehen.
		bezug := s.losSeit
		if beendet && s.endeSeit.After(bezug) {
			bezug = s.endeSeit
		}
		s.mu.Unlock()
		alt := jetzt.Sub(bezug)
		switch {
		case beendet && los && alt > BeendetMax:
			delete(m.sitzung, id)
		case !beendet && los && alt > LosgeloestMax:
			go s.Beenden(GrundOhneBrowser)
		}
	}
}

// kanal ist ein geoeffnetes Terminal: Sitzung mit PTY, Eingabe und gelesener Ausgabe.
type kanal struct {
	sess  *ssh.Session
	stdin io.Writer
	daten chan []byte // Ausgabe der Shell, wird bei Ende geschlossen
}

// verwerfen schliesst einen nicht mehr gebrauchten Kanal und leert seine Ausgabe.
func (k *kanal) verwerfen() {
	k.sess.Close()
	go func() {
		for range k.daten {
		}
	}()
}

// terminalOeffnen oeffnet eine Sitzung mit PTY und startet befehl (leer = Login-Shell).
func terminalOeffnen(c *ssh.Client, spalten, zeilen int, befehl string) (*kanal, error) {
	sess, err := c.NewSession()
	if err != nil {
		log.Printf("Terminal: Sitzung öffnen: %v", err)
		return nil, texte.Neu("term.kanal.voll").MitUrsache(err)
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 115200, ssh.TTY_OP_OSPEED: 115200}
	if err := sess.RequestPty("xterm-256color", zeilen, spalten, modes); err != nil {
		sess.Close()
		log.Printf("Terminal: PTY anfordern: %v", err)
		return nil, texte.Neu("term.kein.pty").MitUrsache(err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		log.Printf("Terminal: Eingabe öffnen: %v", err)
		return nil, texte.Neu("term.kanal.fehler").MitUrsache(err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		log.Printf("Terminal: Ausgabe öffnen: %v", err)
		return nil, texte.Neu("term.kanal.fehler").MitUrsache(err)
	}
	sess.Stderr = nil
	if befehl != "" {
		err = sess.Start(befehl)
	} else {
		err = sess.Shell()
	}
	if err != nil {
		sess.Close()
		log.Printf("Terminal: Start: %v", err)
		if befehl != "" {
			return nil, texte.Neu("term.kein.startbefehl").MitUrsache(err)
		}
		return nil, texte.Neu("term.keine.shell").MitUrsache(err)
	}
	k := &kanal{sess: sess, stdin: stdin, daten: make(chan []byte, 16)}
	go func() {
		defer close(k.daten)
		buf := make([]byte, 32*1024)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				k.daten <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				return
			}
		}
	}()
	return k, nil
}

// markeAbwarten liest die erste Ausgabe des Startbefehls. ok = false, wenn der Befehl
// ohne PID-Marke endete (die Login-Shell hat ihn nicht verstanden).
func markeAbwarten(k *kanal, frist time.Duration) (erste []byte, ok bool) {
	t := time.NewTimer(frist)
	defer t.Stop()
	for {
		select {
		case b, offen := <-k.daten:
			if !offen {
				return erste, false
			}
			erste = append(erste, b...)
			if pidMarke.Match(erste) || len(erste) > 64*1024 {
				return erste, true
			}
		case <-t.C:
			// Laeuft, aber (noch) ohne Marke: weiter ohne Rueckfall.
			return erste, true
		}
	}
}

// Starten oeffnet ein Terminal auf dem Client und registriert die Sitzung.
func (m *Manager) Starten(p Start) (*Sitzung, error) {
	if p.Spalten <= 0 {
		p.Spalten = 80
	}
	if p.Zeilen <= 0 {
		p.Zeilen = 24
	}
	var k *kanal
	var erste []byte
	if p.PfadFolgen {
		var err error
		k, err = terminalOeffnen(p.Client, p.Spalten, p.Zeilen, StartBefehl)
		if err == nil {
			var ok bool
			if erste, ok = markeAbwarten(k, markeFrist); !ok {
				log.Printf("Startbefehl auf «%s» ohne Ergebnis beendet (Ausgabe %q) — normale Shell, die Dateileiste folgt dem Terminal hier nicht", p.Titel, kuerzen(erste, 120))
				k.verwerfen()
				k, erste = nil, nil
			}
		} else {
			log.Printf("Startbefehl auf «%s» abgelehnt — normale Shell, die Dateileiste folgt dem Terminal hier nicht", p.Titel)
		}
	}
	if k == nil {
		var err error
		if k, err = terminalOeffnen(p.Client, p.Spalten, p.Zeilen, ""); err != nil {
			p.Schliessen()
			return nil, err
		}
	}
	s := &Sitzung{
		id: zufallsID(), titel: p.Titel, hostID: p.HostID, gestartet: time.Now(), fingerprint: p.Fingerprint,
		client: p.Client, schliessen: p.Schliessen, sess: k.sess, stdin: k.stdin, losSeit: time.Now(),
	}
	if len(erste) > 0 {
		s.verarbeiten(erste)
	}
	m.mu.Lock()
	m.sitzung[s.id] = s
	m.mu.Unlock()
	gelesen := make(chan struct{})
	go func() {
		defer close(gelesen)
		for b := range k.daten {
			s.verarbeiten(b)
		}
	}()
	go func() {
		err := k.sess.Wait()
		// Die letzte Ausgabe soll vor der Ende-Meldung beim Browser sein.
		select {
		case <-gelesen:
		case <-time.After(time.Second):
		}
		grund := GrundVerbindung
		var ee *ssh.ExitError
		if errors.As(err, &ee) {
			grund = GrundShell(ee.ExitStatus())
		} else if err == nil {
			grund = GrundAbgemeldet
		}
		s.Beenden(grund)
	}()
	return s, nil
}

func kuerzen(b []byte, n int) string {
	if len(b) > n {
		b = b[:n]
	}
	return string(b)
}

func (m *Manager) Hole(id string) (*Sitzung, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sitzung[id]
	return s, ok
}

func (m *Manager) Entfernen(id string) {
	m.mu.Lock()
	s := m.sitzung[id]
	delete(m.sitzung, id)
	m.mu.Unlock()
	if s != nil {
		s.Beenden(GrundGeschlossen)
	}
}

// Liste liefert alle Sitzungen; Gruende in der Sprache sprache.
func (m *Manager) Liste(sprache string) []Info {
	m.mu.Lock()
	out := make([]Info, 0, len(m.sitzung))
	for _, s := range m.sitzung {
		out = append(out, s.Info(sprache))
	}
	m.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Gestartet.Before(out[j].Gestartet) })
	return out
}

func (s *Sitzung) ID() string          { return s.id }
func (s *Sitzung) Client() *ssh.Client { return s.client }

// Info beschreibt die Sitzung; den Grund des Endes in der Sprache sprache.
func (s *Sitzung) Info(sprache string) Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := Info{ID: s.id, HostID: s.hostID, Titel: s.titel, Gestartet: s.gestartet, Angehaengt: s.anhang != nil,
		Beendet: s.beendet, Fingerprint: s.fingerprint}
	if s.grund != nil {
		i.Grund = s.grund.Text(sprache)
	}
	return i
}

// offeneOSC liefert den Beginn einer angefangenen OSC-Sequenz («ESC ]») am Ende, die
// zurueckgehalten werden soll, bis sie vollstaendig ist (sonst -1). Zurueckgehalten
// wird nur, solange der Rest wie eine laufende OSC-Nutzlast aussieht: Ein ESC (ausser
// einem einzelnen am Ende, das zu «ESC \» gehoeren kann), CR, LF, CAN oder SUB beendet
// sie, wie in xterm.js.
func offeneOSC(daten []byte) int {
	i := bytes.LastIndex(daten, []byte("\x1b]"))
	if i < 0 || len(daten)-i >= restMax {
		return -1
	}
	rest := daten[i+2:]
	if bytes.ContainsAny(rest, "\x07\r\n\x18\x1a") {
		return -1
	}
	if esc := bytes.IndexByte(rest, 0x1b); esc >= 0 && esc != len(rest)-1 {
		return -1
	}
	return i
}

// verarbeiten nimmt ein Stueck Ausgabe der Shell entgegen.
func (s *Sitzung) verarbeiten(chunk []byte) { s.ausgeben(chunk, false) }

// ausgeben verteilt Ausgabe an Ringpuffer und angehaengten Browser. alles = auch einen
// zurueckgehaltenen Rest ausgeben (Frist abgelaufen oder Sitzungsende).
func (s *Sitzung) ausgeben(chunk []byte, alles bool) {
	s.ausgabeMu.Lock()
	defer s.ausgabeMu.Unlock()
	s.mu.Lock()
	daten := append(s.rest, chunk...)
	s.rest = nil
	// PID-Marke herausfiltern (nur die erste zaehlt).
	if m := pidMarke.FindSubmatchIndex(daten); m != nil {
		if s.pid == 0 {
			s.pid, _ = strconv.Atoi(string(daten[m[2]:m[3]]))
		}
		daten = append(daten[:m[0]:m[0]], daten[m[1]:]...)
	}
	for _, m := range osc7.FindAllSubmatch(daten, -1) {
		if p, err := url.PathUnescape(string(m[1])); err == nil {
			s.cwd = p
		}
	}
	// Angefangene Steuersequenz am Ende zurueckhalten, bis sie vollstaendig ist
	// (hoechstens restFrist lang).
	if !alles {
		if i := offeneOSC(daten); i >= 0 {
			s.rest = append([]byte(nil), daten[i:]...)
			daten = daten[:i]
			if s.restTimer == nil {
				s.restTimer = time.AfterFunc(restFrist, func() { s.ausgeben(nil, true) })
			} else {
				s.restTimer.Reset(restFrist)
			}
		}
	}
	if len(daten) == 0 {
		s.mu.Unlock()
		return
	}
	s.ring = append(s.ring, daten...)
	if over := len(s.ring) - pufferGroesse; over > 0 {
		schnitt := schnittStelle(s.ring, over)
		s.startModi.lesen(s.ring[:schnitt])
		s.ring = append([]byte(nil), s.ring[schnitt:]...)
	}
	a := s.anhang
	s.mu.Unlock()
	if a == nil {
		return
	}
	// Erst wenn der Browser seinen Puffer hat, kommt Live-Ausgabe.
	<-a.frei
	if !a.Aktuell() {
		// Inzwischen abgeloest oder geloest: ein neuer Browser hat diese Daten
		// schon in seinem Puffer (der Ring wurde vor dem Anhaengen ergaenzt).
		return
	}
	if !a.e.Daten(append([]byte(nil), daten...)) {
		s.mu.Lock()
		// Nur diesen Browser loesen; ein inzwischen angehaengter bleibt.
		if s.anhang == a {
			s.anhang = nil
			s.losSeit = time.Now()
		}
		s.mu.Unlock()
	}
}

// Anhaengen setzt den Empfaenger und liefert den bisherigen Puffer zum Nachzeichnen.
// Ein vorher angehaengter Browser wird abgeloest (Abgeloest-Rueckruf). Live-Ausgabe
// fliesst erst nach Anhang.Freigeben; Anhang.Loesen haengt wieder ab.
func (s *Sitzung) Anhaengen(e Empfaenger) (a *Anhang, puffer []byte, beendet bool, grund *texte.Meldung) {
	a = &Anhang{s: s, e: e, frei: make(chan struct{})}
	s.mu.Lock()
	alt := s.anhang
	s.anhang = a
	// Ist der Anfang des Rings verloren, stellen die Modi von dort den Bildschirm richtig ein.
	puffer = append(s.startModi.folge(), s.ring...)
	beendet, grund = s.beendet, s.grund
	s.mu.Unlock()
	if alt != nil {
		// Ein Schreibvorgang an den alten Browser soll nicht am Tor warten.
		alt.Freigeben()
		if alt.e.Abgeloest != nil {
			alt.e.Abgeloest()
		}
	}
	return a, puffer, beendet, grund
}

// Freigeben laesst die Live-Ausgabe zum Browser fliessen (nach dem Puffer).
func (a *Anhang) Freigeben() { a.einmal.Do(func() { close(a.frei) }) }

// Loesen haengt den Browser ab (die Sitzung laeuft weiter).
func (a *Anhang) Loesen() {
	a.Freigeben()
	s := a.s
	s.mu.Lock()
	if s.anhang == a {
		s.anhang = nil
		s.losSeit = time.Now()
	}
	s.mu.Unlock()
}

// Aktuell meldet, ob dieser Browser noch angehaengt ist.
func (a *Anhang) Aktuell() bool {
	a.s.mu.Lock()
	defer a.s.mu.Unlock()
	return a.s.anhang == a
}

// Eingabe schreibt Tastendruecke in die Shell, solange dieser Browser angehaengt ist.
func (a *Anhang) Eingabe(b []byte) error {
	if !a.Aktuell() {
		return ErrAbgeloest
	}
	return a.s.Eingabe(b)
}

// Groesse aendert die Terminalgroesse, solange dieser Browser angehaengt ist.
func (a *Anhang) Groesse(spalten, zeilen int) error {
	if !a.Aktuell() {
		return ErrAbgeloest
	}
	return a.s.Groesse(spalten, zeilen)
}

// Eingabe schreibt Tastendruecke in die Shell.
func (s *Sitzung) Eingabe(b []byte) error {
	s.mu.Lock()
	beendet := s.beendet
	s.mu.Unlock()
	if beendet {
		return ErrBeendet
	}
	_, err := s.stdin.Write(b)
	return err
}

func (s *Sitzung) Groesse(spalten, zeilen int) error {
	if spalten < 2 || zeilen < 2 || spalten > 1000 || zeilen > 500 {
		return nil
	}
	return s.sess.WindowChange(zeilen, spalten)
}

// Beendet meldet, ob und warum die Sitzung zu Ende ist.
func (s *Sitzung) Beendet() (bool, *texte.Meldung) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.beendet, s.grund
}

// Beenden schliesst die Sitzung und meldet dem angehaengten Browser grund. Haengt
// ein Schreibvorgang an einen Browser, der nicht mehr liest, wird er nach abbruchFrist
// abgebrochen: Beenden (und damit «Sitzung schliessen» oder das Herunterfahren) wartet
// nicht bis zu dessen Schreibfrist.
func (s *Sitzung) Beenden(grund *texte.Meldung) {
	s.mu.Lock()
	if s.beendet {
		s.mu.Unlock()
		return
	}
	s.beendet, s.grund, s.endeSeit = true, grund, time.Now()
	if s.restTimer != nil {
		s.restTimer.Stop()
	}
	s.mu.Unlock()
	s.sftpMu.Lock()
	if s.sftpC != nil {
		s.sftpC.Close()
		s.sftpC = nil
	}
	if s.sftpSess != nil {
		s.sftpSess.Close()
		s.sftpSess = nil
	}
	s.sftpMu.Unlock()
	if s.sess != nil {
		s.sess.Close()
	}
	if s.schliessen != nil {
		s.schliessen()
	}
	abbruch := time.AfterFunc(abbruchFrist, s.schreibenAbbrechen)
	defer abbruch.Stop()
	// Zurueckgehaltenen Rest noch ausgeben, dann das Ende melden.
	s.ausgeben(nil, true)
	s.mu.Lock()
	a := s.anhang
	s.mu.Unlock()
	if a != nil && a.e.Ende != nil {
		<-a.frei
		a.e.Ende(grund)
	}
}

// schreibenAbbrechen bricht einen haengenden Schreibvorgang an den angehaengten Browser ab.
func (s *Sitzung) schreibenAbbrechen() {
	s.mu.Lock()
	a := s.anhang
	s.mu.Unlock()
	if a != nil && a.e.Abbrechen != nil {
		log.Printf("Sitzung %s: Browser liest nicht mehr — Schreiben abgebrochen", kurzID(s.id))
		a.e.Abbrechen()
	}
}

func kurzID(id string) string {
	if len(id) > 6 {
		return id[:6]
	}
	return id
}

// SFTP liefert (und oeffnet bei Bedarf) den SFTP-Kanal der Sitzung. Bietet der
// Server kein SFTP an, wird eine Minute lang nicht neu versucht.
func (s *Sitzung) SFTP() (*sftp.Client, error) {
	s.sftpMu.Lock()
	defer s.sftpMu.Unlock()
	if b, _ := s.Beendet(); b {
		return nil, ErrBeendet
	}
	if s.sftpC != nil {
		return s.sftpC, nil
	}
	if !s.sftpFehlerSeit.IsZero() && time.Since(s.sftpFehlerSeit) < sftpPause {
		return nil, ErrKeinSFTP
	}
	c, sess, err := sftpOeffnen(s.client)
	if err != nil {
		s.sftpFehlerSeit = time.Now()
		log.Printf("SFTP für «%s»: %v", s.titel, err)
		return nil, ErrKeinSFTP
	}
	s.sftpC, s.sftpSess, s.sftpFehlerSeit = c, sess, time.Time{}
	return c, nil
}

// sftpOeffnen startet das SFTP-Subsystem; bei einem Fehler wird der Kanal wieder
// geschlossen (sftp.NewClient laesst ihn offen).
func sftpOeffnen(client *ssh.Client) (*sftp.Client, *ssh.Session, error) {
	sess, err := client.NewSession()
	if err != nil {
		return nil, nil, err
	}
	w, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		return nil, nil, err
	}
	r, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		return nil, nil, err
	}
	if err := sess.RequestSubsystem("sftp"); err != nil {
		sess.Close()
		return nil, nil, err
	}
	c, err := sftp.NewClientPipe(r, w)
	if err != nil {
		sess.Close()
		return nil, nil, err
	}
	return c, sess, nil
}

// Arbeitsverzeichnis der Shell: aus /proc (bei bekannter PID) oder OSC 7.
func (s *Sitzung) Arbeitsverzeichnis() (string, bool) {
	s.mu.Lock()
	pid, cwd := s.pid, s.cwd
	s.mu.Unlock()
	if pid > 0 {
		if c, err := s.SFTP(); err == nil {
			if p, err := c.ReadLink("/proc/" + strconv.Itoa(pid) + "/cwd"); err == nil && p != "" {
				return p, true
			}
		}
	}
	return cwd, cwd != ""
}

func zufallsID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}
