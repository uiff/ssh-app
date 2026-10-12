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
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const (
	pufferGroesse = 256 * 1024
	// Wie lange eine Sitzung ohne angehaengten Browser weiterlaeuft.
	LosgeloestMax = 12 * time.Hour
	// Wie lange eine beendete Sitzung (zum Nachlesen) stehen bleibt.
	BeendetMax = 10 * time.Minute
)

// pidMarke wird vom Startbefehl ausgegeben und aus dem Datenstrom entfernt.
var pidMarke = regexp.MustCompile(`\x1b\]7770;pid=(\d+)\x07`)

// osc7 meldet das Arbeitsverzeichnis, wenn die Shell es von sich aus sendet.
var osc7 = regexp.MustCompile(`\x1b\]7;file://[^/\x07\x1b]*(/[^\x07\x1b]*)(?:\x07|\x1b\\)`)

// StartBefehl gibt die PID der Shell aus und ersetzt sich dann durch die Login-Shell.
// So kann die Dateileiste /proc/<pid>/cwd lesen und dem Terminal folgen.
const StartBefehl = `printf '\033]7770;pid=%s\007' "$$"; exec "${SHELL:-/bin/sh}" -l`

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

type Sitzung struct {
	id, titel   string
	hostID      int64
	gestartet   time.Time
	fingerprint string

	client     *ssh.Client
	schliessen func()
	sess       *ssh.Session
	stdin      io.Writer

	mu         sync.Mutex
	ring       []byte
	empfaenger func([]byte) bool
	anhangNr   int
	losSeit    time.Time
	beendet    bool
	grund      string
	pid        int
	cwd        string
	rest       []byte

	sftpMu sync.Mutex
	sftpC  *sftp.Client
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
	for _, s := range alle {
		s.Beenden("Server fährt herunter")
	}
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
		jetzt := time.Now()
		m.mu.Lock()
		for id, s := range m.sitzung {
			s.mu.Lock()
			los := s.empfaenger == nil
			alt := jetzt.Sub(s.losSeit)
			beendet := s.beendet
			s.mu.Unlock()
			switch {
			case beendet && los && alt > BeendetMax:
				delete(m.sitzung, id)
			case !beendet && los && alt > LosgeloestMax:
				go s.Beenden("zu lange ohne Browser")
			}
		}
		m.mu.Unlock()
	}
}

// Starten oeffnet ein Terminal auf dem Client und registriert die Sitzung.
func (m *Manager) Starten(p Start) (*Sitzung, error) {
	sess, err := p.Client.NewSession()
	if err != nil {
		p.Schliessen()
		return nil, err
	}
	if p.Spalten <= 0 {
		p.Spalten = 80
	}
	if p.Zeilen <= 0 {
		p.Zeilen = 24
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 115200, ssh.TTY_OP_OSPEED: 115200}
	if err := sess.RequestPty("xterm-256color", p.Zeilen, p.Spalten, modes); err != nil {
		sess.Close()
		p.Schliessen()
		return nil, err
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		p.Schliessen()
		return nil, err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		p.Schliessen()
		return nil, err
	}
	sess.Stderr = nil
	if p.PfadFolgen {
		err = sess.Start(StartBefehl)
	} else {
		err = sess.Shell()
	}
	if err != nil {
		sess.Close()
		p.Schliessen()
		return nil, err
	}
	s := &Sitzung{
		id: zufallsID(), titel: p.Titel, hostID: p.HostID, gestartet: time.Now(), fingerprint: p.Fingerprint,
		client: p.Client, schliessen: p.Schliessen, sess: sess, stdin: stdin, losSeit: time.Now(),
	}
	m.mu.Lock()
	m.sitzung[s.id] = s
	m.mu.Unlock()
	go s.lesen(stdout)
	go func() {
		err := sess.Wait()
		grund := "Verbindung beendet"
		var ee *ssh.ExitError
		if errors.As(err, &ee) {
			grund = "Shell beendet (Code " + strconv.Itoa(ee.ExitStatus()) + ")"
		} else if err == nil {
			grund = "Abgemeldet"
		}
		s.Beenden(grund)
	}()
	return s, nil
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
		s.Beenden("geschlossen")
	}
}

func (m *Manager) Liste() []Info {
	m.mu.Lock()
	out := make([]Info, 0, len(m.sitzung))
	for _, s := range m.sitzung {
		out = append(out, s.Info())
	}
	m.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Gestartet.Before(out[j].Gestartet) })
	return out
}

func (s *Sitzung) ID() string          { return s.id }
func (s *Sitzung) Client() *ssh.Client { return s.client }

func (s *Sitzung) Info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Info{ID: s.id, HostID: s.hostID, Titel: s.titel, Gestartet: s.gestartet, Angehaengt: s.empfaenger != nil,
		Beendet: s.beendet, Grund: s.grund, Fingerprint: s.fingerprint}
}

// lesen verteilt die Ausgabe der Shell an Ringpuffer und angehaengten Browser.
func (s *Sitzung) lesen(r io.Reader) {
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			s.verarbeiten(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func (s *Sitzung) verarbeiten(chunk []byte) {
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
	// Angefangene Steuersequenz am Ende zurueckhalten, bis sie vollstaendig ist.
	if i := bytes.LastIndex(daten, []byte("\x1b]")); i >= 0 && len(daten)-i < 512 &&
		!bytes.ContainsAny(daten[i:], "\x07") && !bytes.Contains(daten[i:], []byte("\x1b\\")) {
		s.rest = append([]byte(nil), daten[i:]...)
		daten = daten[:i]
	}
	if len(daten) == 0 {
		s.mu.Unlock()
		return
	}
	s.ring = append(s.ring, daten...)
	if over := len(s.ring) - pufferGroesse; over > 0 {
		s.ring = append([]byte(nil), s.ring[over:]...)
	}
	f := s.empfaenger
	s.mu.Unlock()
	if f != nil {
		kopie := append([]byte(nil), daten...)
		if !f(kopie) {
			s.mu.Lock()
			if s.empfaenger != nil {
				s.empfaenger = nil
				s.losSeit = time.Now()
			}
			s.mu.Unlock()
		}
	}
}

// Anhaengen setzt den Empfaenger (ein vorheriger Browser wird abgeloest) und
// liefert den bisherigen Puffer zum Nachzeichnen. Die Rueckgabe-Funktion loest.
func (s *Sitzung) Anhaengen(f func([]byte) bool) (puffer []byte, beendet bool, grund string, loesen func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.anhangNr++
	nr := s.anhangNr
	s.empfaenger = f
	puffer = append([]byte(nil), s.ring...)
	return puffer, s.beendet, s.grund, func() {
		s.mu.Lock()
		if s.anhangNr == nr {
			s.empfaenger = nil
			s.losSeit = time.Now()
		}
		s.mu.Unlock()
	}
}

// Eingabe schreibt Tastendruecke in die Shell.
func (s *Sitzung) Eingabe(b []byte) error {
	s.mu.Lock()
	beendet := s.beendet
	s.mu.Unlock()
	if beendet {
		return errors.New("Sitzung ist beendet")
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
func (s *Sitzung) Beendet() (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.beendet, s.grund
}

func (s *Sitzung) Beenden(grund string) {
	s.mu.Lock()
	if s.beendet {
		s.mu.Unlock()
		return
	}
	s.beendet, s.grund = true, grund
	f := s.empfaenger
	s.mu.Unlock()
	s.sftpMu.Lock()
	if s.sftpC != nil {
		s.sftpC.Close()
		s.sftpC = nil
	}
	s.sftpMu.Unlock()
	s.sess.Close()
	s.schliessen()
	if f != nil {
		f(nil) // nil = Ende-Signal an den Browser
	}
}

// SFTP liefert (und oeffnet bei Bedarf) den SFTP-Kanal der Sitzung.
func (s *Sitzung) SFTP() (*sftp.Client, error) {
	s.sftpMu.Lock()
	defer s.sftpMu.Unlock()
	if b, _ := s.Beendet(); b {
		return nil, errors.New("Sitzung ist beendet")
	}
	if s.sftpC != nil {
		return s.sftpC, nil
	}
	c, err := sftp.NewClient(s.client)
	if err != nil {
		return nil, errors.New("Der Server bietet kein SFTP an")
	}
	s.sftpC = c
	return c, nil
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
