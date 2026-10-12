package term

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// schreiber ist eine Shell-Eingabe zum Mitlesen.
type schreiber struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *schreiber) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(b)
}

func (w *schreiber) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// testSitzung ist eine Sitzung ohne SSH: Ausgabe kommt ueber verarbeiten.
func testSitzung() (*Sitzung, *schreiber) {
	w := &schreiber{}
	return &Sitzung{id: "test", titel: "test", gestartet: time.Now(), losSeit: time.Now(), stdin: w}, w
}

// empfang sammelt, was ein Browser bekommt.
type empfang struct {
	mu        sync.Mutex
	daten     bytes.Buffer
	ende      string
	abgeloest int
	antwort   func([]byte) bool // optional: eigenes Verhalten beim Schreiben
}

func (e *empfang) empfaenger() Empfaenger {
	return Empfaenger{
		Daten: func(b []byte) bool {
			if e.antwort != nil && !e.antwort(b) {
				return false
			}
			e.mu.Lock()
			defer e.mu.Unlock()
			e.daten.Write(b)
			return true
		},
		Ende: func(g string) {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.ende = g
		},
		Abgeloest: func() {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.abgeloest++
		},
	}
}

func (e *empfang) text() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.daten.String()
}

func warteBis(t *testing.T, was string, f func() bool) {
	t.Helper()
	ende := time.Now().Add(3 * time.Second)
	for time.Now().Before(ende) {
		if f() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("Zeitüberschreitung: %s", was)
}

// Der Startbefehl muss in jeder Login-Shell laufen, die sshd mit «-c» startet, und die
// PID der spaeteren Shell melden (exec behaelt sie). fish/tcsh/csh scheiterten frueher
// schon beim Parsen ($$, ${SHELL:-…}).
func TestStartBefehlInShells(t *testing.T) {
	type shell struct {
		name string
		args []string
	}
	var shells []shell
	for _, n := range []string{"sh", "dash", "bash", "zsh", "ksh", "mksh", "fish", "tcsh", "csh"} {
		if p, err := exec.LookPath(n); err == nil {
			shells = append(shells, shell{n, []string{p, "-c", StartBefehl}})
		}
	}
	if p, err := exec.LookPath("busybox"); err == nil {
		shells = append(shells, shell{"busybox sh", []string{p, "sh", "-c", StartBefehl}})
	}
	if len(shells) == 0 {
		t.Skip("keine Shell gefunden")
	}
	for _, sh := range shells {
		t.Run(sh.name, func(t *testing.T) {
			cmd := exec.Command(sh.args[0], sh.args[1:]...)
			// Die «Login-Shell» ist hier /bin/true: sie endet sofort mit Code 0.
			cmd.Env = append(os.Environ(), "SHELL=/bin/true")
			var out bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &out
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			pid := cmd.Process.Pid
			if err := cmd.Wait(); err != nil {
				t.Fatalf("Startbefehl in %s: %v, Ausgabe %q", sh.name, err, out.String())
			}
			m := pidMarke.FindSubmatch(out.Bytes())
			if m == nil {
				t.Fatalf("keine PID-Marke von %s: %q", sh.name, out.String())
			}
			if string(m[1]) != strconv.Itoa(pid) {
				t.Fatalf("%s meldet PID %s, die Shell hat %d", sh.name, m[1], pid)
			}
		})
	}
	// Im Befehl darf nichts stehen, was fish oder csh in einfachen Anfuehrungszeichen deuten.
	innen := StartBefehl[strings.Index(StartBefehl, "'")+1 : strings.LastIndex(StartBefehl, "'")]
	if strings.ContainsAny(innen, "'!") || !strings.HasPrefix(StartBefehl, "exec /bin/sh -c '") {
		t.Fatalf("Startbefehl nicht shell-neutral: %s", StartBefehl)
	}
}

// BACKEND-04: Ein unabgeschlossenes «ESC ]» haelt die folgende Ausgabe nicht mehr fest.
func TestOffeneOSCHaeltNichtFest(t *testing.T) {
	s, _ := testSitzung()
	e := &empfang{}
	a, _, _, _ := s.Anhaengen(e.empfaenger())
	a.Freigeben()

	s.verarbeiten([]byte("vorher\r\n\x1b]"))
	s.verarbeiten([]byte("\x1b[32mpi@host\x1b[0m:~$ "))
	if !strings.Contains(e.text(), "pi@host") {
		t.Fatalf("Prompt nach streunendem ESC ] zurückgehalten: %q", e.text())
	}

	// Eine echte, geteilte OSC-7-Sequenz wird zusammengesetzt und erkannt.
	s.verarbeiten([]byte("\x1b]7;file://host/home/p"))
	if strings.Contains(e.text(), "]7;") {
		t.Fatalf("halbe OSC-7-Sequenz ausgegeben: %q", e.text())
	}
	s.verarbeiten([]byte("i\x07$ "))
	if p, ok := s.Arbeitsverzeichnis(); !ok || p != "/home/pi" {
		t.Fatalf("OSC 7: %q %v", p, ok)
	}
	// Geteiltes ST («ESC \\»): ein einzelnes ESC am Ende bleibt zurueck.
	s.verarbeiten([]byte("\x1b]0;Titel\x1b"))
	s.verarbeiten([]byte("\\weiter"))
	if !strings.Contains(e.text(), "\x1b]0;Titel\x1b\\weiter") {
		t.Fatalf("ST über Stückgrenze: %q", e.text())
	}

	// Ohne Abschluss und ohne weitere Ausgabe: nach kurzer Frist trotzdem ausgeben.
	s.verarbeiten([]byte("\x1b]kaputt"))
	warteBis(t, "Rest nach Frist", func() bool { return strings.Contains(e.text(), "kaputt") })

	// Ein Rest beim Ende geht in den Puffer und an den Browser.
	s.verarbeiten([]byte("\x1b]ende"))
	s.Beenden("Test")
	if !strings.Contains(string(s.ring), "\x1b]ende") || !strings.Contains(e.text(), "\x1b]ende") {
		t.Fatalf("Rest bei Ende verloren: Ring %q", s.ring)
	}
	if e.ende != "Test" {
		t.Fatalf("Ende-Meldung: %q", e.ende)
	}
}

// BACKEND-01: Scheitert das Schreiben an einen alten Browser, nachdem ein neuer
// angehaengt hat, bleibt der neue angehaengt und bekommt weiter Ausgabe.
func TestAlterEmpfaengerLoestNeuenNicht(t *testing.T) {
	s, _ := testSitzung()
	imSchreiben, weiter := make(chan struct{}), make(chan struct{})
	alt := &empfang{antwort: func([]byte) bool {
		close(imSchreiben)
		<-weiter // haengt wie ein Schreiben an eine tote Verbindung
		return false
	}}
	a, _, _, _ := s.Anhaengen(alt.empfaenger())
	a.Freigeben()
	fertig := make(chan struct{})
	go func() {
		s.verarbeiten([]byte("eins "))
		close(fertig)
	}()
	<-imSchreiben

	neu := &empfang{}
	b, puffer, _, _ := s.Anhaengen(neu.empfaenger())
	if string(puffer) != "eins " {
		t.Fatalf("Puffer des neuen Browsers: %q", puffer)
	}
	b.Freigeben()
	close(weiter)
	<-fertig

	if !s.Info().Angehaengt || !b.Aktuell() {
		t.Fatal("der neue Browser wurde vom alten abgehängt")
	}
	s.verarbeiten([]byte("zwei"))
	if neu.text() != "zwei" {
		t.Fatalf("neuer Browser bekommt keine Ausgabe: %q", neu.text())
	}
	if alt.abgeloest != 1 {
		t.Fatalf("alter Browser nicht abgelöst: %d", alt.abgeloest)
	}
}

// BACKEND-09/V5: Live-Ausgabe kommt erst nach Freigeben (also nach dem Puffer).
func TestLiveErstNachFreigabe(t *testing.T) {
	s, _ := testSitzung()
	s.verarbeiten([]byte("alt "))
	e := &empfang{}
	a, puffer, _, _ := s.Anhaengen(e.empfaenger())
	fertig := make(chan struct{})
	go func() {
		s.verarbeiten([]byte("neu"))
		close(fertig)
	}()
	time.Sleep(50 * time.Millisecond)
	if e.text() != "" {
		t.Fatalf("Live-Ausgabe vor dem Puffer: %q", e.text())
	}
	if string(puffer) != "alt " {
		t.Fatalf("Puffer: %q", puffer)
	}
	a.Freigeben()
	<-fertig
	if e.text() != "neu" {
		t.Fatalf("nach Freigabe: %q", e.text())
	}
	// Loesen gibt ein wartendes Schreiben ebenfalls frei (kein Haenger).
	f := &empfang{}
	b, _, _, _ := s.Anhaengen(f.empfaenger())
	done := make(chan struct{})
	go func() {
		s.verarbeiten([]byte("x"))
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	b.Loesen()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("verarbeiten hängt nach Loesen")
	}
	if f.text() != "" || s.Info().Angehaengt {
		t.Fatalf("nach Loesen noch Ausgabe/angehängt: %q", f.text())
	}
}

// BACKEND-08/V4: Der abgeloeste Browser erfaehrt es und kann weder tippen noch die Groesse aendern.
func TestAbloesenSperrtAltenBrowser(t *testing.T) {
	s, stdin := testSitzung()
	alt, neu := &empfang{}, &empfang{}
	a, _, _, _ := s.Anhaengen(alt.empfaenger())
	a.Freigeben()
	if err := a.Eingabe([]byte("ls\r")); err != nil {
		t.Fatal(err)
	}
	b, _, _, _ := s.Anhaengen(neu.empfaenger())
	b.Freigeben()
	if alt.abgeloest != 1 || neu.abgeloest != 0 {
		t.Fatalf("Abgelöst-Meldungen: alt %d, neu %d", alt.abgeloest, neu.abgeloest)
	}
	if err := a.Eingabe([]byte("rm -rf x\r")); err != ErrAbgeloest {
		t.Fatalf("alte Eingabe: %v", err)
	}
	if err := a.Groesse(100, 40); err != ErrAbgeloest {
		t.Fatalf("alte Grösse: %v", err)
	}
	if err := b.Eingabe([]byte("pwd\r")); err != nil {
		t.Fatal(err)
	}
	if stdin.String() != "ls\rpwd\r" {
		t.Fatalf("Eingaben in der Shell: %q", stdin.String())
	}
	a.Loesen() // darf den neuen nicht loesen
	if !s.Info().Angehaengt {
		t.Fatal("Loesen des alten hat den neuen abgehängt")
	}
}

// BACKEND-10: Eine lange losgeloeste Sitzung, die eben endet, bleibt BeendetMax stehen.
func TestBeendetBleibtZumNachlesen(t *testing.T) {
	m := &Manager{sitzung: map[string]*Sitzung{}, stop: make(chan struct{})}
	s, _ := testSitzung()
	m.sitzung[s.id] = s
	jetzt := time.Now()
	s.losSeit = jetzt.Add(-30 * time.Minute)
	s.Beenden("Shell beendet (Code 0)")
	m.raeumen(jetzt.Add(time.Minute))
	if _, ok := m.Hole(s.id); !ok {
		t.Fatal("eben beendete Sitzung sofort entfernt")
	}
	m.raeumen(jetzt.Add(BeendetMax + time.Minute))
	if _, ok := m.Hole(s.id); ok {
		t.Fatal("beendete Sitzung nach BeendetMax nicht entfernt")
	}
}

func TestPidMarkeWirdEntfernt(t *testing.T) {
	s, _ := testSitzung()
	s.verarbeiten([]byte("a\x1b]7770;pid=42"))
	s.verarbeiten([]byte("42\x07b"))
	if string(s.ring) != "ab" || s.pid != 4242 {
		t.Fatalf("Ring %q, PID %d", s.ring, s.pid)
	}
}
