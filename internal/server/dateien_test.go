package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/pkg/sftp"

	"github.com/uiff/ssh-app/internal/store"
)

// dateiSitzung richtet eine App ein, verbindet mit dem eingebetteten Test-SSH-Server
// (SFTP = lokales Dateisystem) und liefert die Sitzungs-ID. Die Sitzung laeuft ohne
// WebSocket weiter.
func dateiSitzung(t *testing.T) (*httptest.Server, *client, string, *Server) {
	t.Helper()
	sshAddr := sshTestServer(t, "pi", "himbeere")
	host, port, _ := net.SplitHostPort(sshAddr)
	ts, s := testApp(t)
	c := neuerClient(t, ts.URL, ts.Client())
	if code := c.rufe("POST", "/api/einrichten", map[string]string{"benutzer": "admin", "passwort": "geheim-geheim"}, nil); code != 200 {
		t.Fatalf("einrichten: %d", code)
	}
	var z store.Zugang
	c.rufe("POST", "/api/zugaenge", map[string]string{"name": "pi", "benutzer": "pi", "passwort": "himbeere"}, &z)
	p := 0
	fmt.Sscan(port, &p)
	var h store.Host
	if code := c.rufe("POST", "/api/hosts", map[string]any{"name": "testpi", "adresse": host, "port": p, "zugangId": z.ID}, &h); code != 200 {
		t.Fatalf("Host anlegen: %d", code)
	}
	ws := wsOeffnen(t, ts, c, fmt.Sprintf("host=%d&c=80&r=24", h.ID))
	liesNachricht(t, ws, "hostkey")
	b, _ := json.Marshal(nachricht{T: "hostkey", OK: true})
	ws.Write(context.Background(), websocket.MessageText, b)
	bereit := liesNachricht(t, ws, "bereit")
	ws.Close(websocket.StatusNormalClosure, "")
	return ts, c, bereit.ID, s
}

type hochAntwort struct {
	Fehler      string   `json:"fehler"`
	Vorhanden   []string `json:"vorhanden"`
	Gesperrt    []string `json:"gesperrt"`
	Hochgeladen []string `json:"hochgeladen"`
}

// hochladen schickt die Dateien (Name, Inhalt im Wechsel) in einem Auftrag.
func hochladen(t *testing.T, ts *httptest.Server, c *client, id, ziel string, ersetzen []string, namenInhalte ...string) (int, hochAntwort) {
	t.Helper()
	var mp bytes.Buffer
	mw := multipart.NewWriter(&mp)
	for i := 0; i+1 < len(namenInhalte); i += 2 {
		fw, _ := mw.CreateFormFile("datei", namenInhalte[i])
		fw.Write([]byte(namenInhalte[i+1]))
	}
	mw.Close()
	q := url.Values{"pfad": {ziel}}
	for _, n := range ersetzen {
		q.Add("ersetzen", n)
	}
	req, _ := http.NewRequest("POST", ts.URL+"/api/sitzungen/"+id+"/hochladen?"+q.Encode(), &mp)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-SSH-App", "1")
	res, err := c.h.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var a hochAntwort
	json.NewDecoder(res.Body).Decode(&a)
	return res.StatusCode, a
}

func inhalt(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		return "<" + err.Error() + ">"
	}
	return string(b)
}

func tmpReste(t *testing.T, verz string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(verz, ".*.sshapp-upload"))
	return m
}

func bisSo(t *testing.T, was string, f func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if f() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("Zeitueberschreitung: %s", was)
}

// abgebrochenHochladen sendet Kopf und 64 KiB einer angekuendigten 10-MB-Datei und
// schliesst dann die Verbindung (Funkloch, Tab zu), wie ein Browser unter HTTP/1.1.
func abgebrochenHochladen(t *testing.T, ts *httptest.Server, c *client, id, verz, name string, ersetzen bool) {
	t.Helper()
	u, _ := url.Parse(ts.URL)
	var cookies []string
	for _, k := range c.h.Jar.Cookies(u) {
		cookies = append(cookies, k.Name+"="+k.Value)
	}
	q := url.Values{"pfad": {verz}}
	if ersetzen {
		q.Set("ersetzen", name)
	}
	nc, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	const grenze = "grenze-1234"
	fmt.Fprintf(nc, "POST /api/sitzungen/%s/hochladen?%s HTTP/1.1\r\nHost: %s\r\nCookie: %s\r\nX-SSH-App: 1\r\n"+
		"Content-Type: multipart/form-data; boundary=%s\r\nContent-Length: %d\r\n\r\n",
		id, q.Encode(), u.Host, strings.Join(cookies, "; "), grenze, 10<<20)
	fmt.Fprintf(nc, "--%s\r\nContent-Disposition: form-data; name=\"datei\"; filename=%q\r\nContent-Type: application/octet-stream\r\n\r\n", grenze, name)
	nc.Write(bytes.Repeat([]byte("N"), 64<<10))
	// Erst trennen, wenn der Server schreibt (die temporaere Datei existiert).
	bisSo(t, "temporaere Datei angelegt", func() bool { return len(tmpReste(t, verz)) > 0 })
	nc.Close()
	bisSo(t, "temporaere Datei nach Abbruch entfernt", func() bool { return len(tmpReste(t, verz)) == 0 })
}

func TestHochladenAbbruchLaesstOriginalStehen(t *testing.T) {
	ts, c, id, _ := dateiSitzung(t)
	verz := t.TempDir()
	original := filepath.Join(verz, "wichtig.conf")
	os.WriteFile(original, []byte("ORIGINAL-INHALT\n"), 0o640)

	// Ueberschreiben bestaetigt, Verbindung bricht mitten in der Uebertragung ab.
	abgebrochenHochladen(t, ts, c, id, verz, "wichtig.conf", true)
	if got := inhalt(t, original); got != "ORIGINAL-INHALT\n" {
		t.Fatalf("Original nach Abbruch zerstoert: %d Bytes %.20q", len(got), got)
	}

	// Neue Datei: Kein Bruchstueck unter dem vollen Namen.
	abgebrochenHochladen(t, ts, c, id, verz, "neu.bin", false)
	if _, err := os.Lstat(filepath.Join(verz, "neu.bin")); !os.IsNotExist(err) {
		t.Fatalf("Bruchstueck nach Abbruch liegt unter vollem Namen: %v", err)
	}

	// Danach funktioniert ein vollstaendiges Hochladen normal.
	if code, a := hochladen(t, ts, c, id, verz, []string{"wichtig.conf"}, "wichtig.conf", "NEU"); code != 200 || len(a.Hochgeladen) != 1 {
		t.Fatalf("Hochladen nach Abbruch: %d %+v", code, a)
	}
	if got := inhalt(t, original); got != "NEU" {
		t.Fatalf("Inhalt: %q", got)
	}
	if fi, _ := os.Stat(original); fi.Mode().Perm() != 0o640 {
		t.Fatalf("Rechte des Originals nicht uebernommen: %v", fi.Mode().Perm())
	}
}

// Mit HTTPS (Vorgabe) laeuft HTTP/2: Dort meldet der Body beim Abbruch einen anderen Fehler,
// frueher wurde dann das Ziel selbst geloescht.
func TestHochladenAbbruchHTTP2(t *testing.T) {
	_, c, id, s := dateiSitzung(t)
	h2 := httptest.NewUnstartedServer(s)
	h2.EnableHTTP2 = true
	h2.StartTLS()
	t.Cleanup(h2.Close)
	hc := h2.Client()
	hc.Jar = c.h.Jar
	if res, err := hc.Get(h2.URL + "/api/status"); err != nil || res.ProtoMajor != 2 {
		t.Fatalf("kein HTTP/2: %v %v", err, res)
	} else {
		res.Body.Close()
	}

	verz := t.TempDir()
	original := filepath.Join(verz, "wichtig.conf")
	os.WriteFile(original, []byte("ORIGINAL-INHALT\n"), 0o644)
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	ctx, abbrechen := context.WithCancel(context.Background())
	q := url.Values{"pfad": {verz}, "ersetzen": {"wichtig.conf"}}
	req, _ := http.NewRequestWithContext(ctx, "POST", h2.URL+"/api/sitzungen/"+id+"/hochladen?"+q.Encode(), pr)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-SSH-App", "1")
	fertig := make(chan struct{})
	go func() {
		if res, err := hc.Do(req); err == nil {
			res.Body.Close()
		}
		close(fertig)
	}()
	go func() {
		fw, _ := mw.CreateFormFile("datei", "wichtig.conf")
		fw.Write(bytes.Repeat([]byte("N"), 64<<10))
	}()
	bisSo(t, "temporaere Datei angelegt", func() bool { return len(tmpReste(t, verz)) > 0 })
	abbrechen()
	<-fertig
	pr.CloseWithError(io.ErrClosedPipe)
	bisSo(t, "temporaere Datei nach Abbruch entfernt", func() bool { return len(tmpReste(t, verz)) == 0 })
	if got := inhalt(t, original); got != "ORIGINAL-INHALT\n" {
		t.Fatalf("Original nach Abbruch unter HTTP/2: %d Bytes %.20q", len(got), got)
	}
}

func TestHochladenFragtJeDatei(t *testing.T) {
	ts, c, id, _ := dateiSitzung(t)
	verz := t.TempDir()
	os.WriteFile(filepath.Join(verz, "a.txt"), []byte("alt-a"), 0o644)
	os.WriteFile(filepath.Join(verz, "b.txt"), []byte("alt-b"), 0o644)

	// Ohne Erlaubnis: alle Konflikte gesammelt, das Neue trotzdem hochgeladen.
	code, a := hochladen(t, ts, c, id, verz, nil, "a.txt", "neu-a", "b.txt", "neu-b", "c.txt", "neu-c")
	if code != 409 || strings.Join(a.Vorhanden, ",") != "a.txt,b.txt" || strings.Join(a.Hochgeladen, ",") != "c.txt" {
		t.Fatalf("Konflikte: %d %+v", code, a)
	}
	if !strings.Contains(a.Fehler, "a.txt") || !strings.Contains(a.Fehler, "b.txt") {
		t.Fatalf("Meldung nennt nicht alle Namen: %q", a.Fehler)
	}
	if inhalt(t, filepath.Join(verz, "a.txt")) != "alt-a" || inhalt(t, filepath.Join(verz, "b.txt")) != "alt-b" || inhalt(t, filepath.Join(verz, "c.txt")) != "neu-c" {
		t.Fatal("Inhalte nach dem ersten Auftrag falsch")
	}

	// Erlaubnis nur fuer a.txt: b.txt bleibt unberuehrt.
	code, a = hochladen(t, ts, c, id, verz, []string{"a.txt"}, "a.txt", "neu-a", "b.txt", "neu-b")
	if code != 409 || strings.Join(a.Vorhanden, ",") != "b.txt" || strings.Join(a.Hochgeladen, ",") != "a.txt" {
		t.Fatalf("Erlaubnis je Datei: %d %+v", code, a)
	}
	if inhalt(t, filepath.Join(verz, "a.txt")) != "neu-a" || inhalt(t, filepath.Join(verz, "b.txt")) != "alt-b" {
		t.Fatal("b.txt ohne Erlaubnis ersetzt")
	}
	if code, a = hochladen(t, ts, c, id, verz, []string{"b.txt"}, "b.txt", "neu-b"); code != 200 || inhalt(t, filepath.Join(verz, "b.txt")) != "neu-b" {
		t.Fatalf("b.txt ersetzen: %d %+v", code, a)
	}

	// Ausfuehrbares Skript bleibt ausfuehrbar.
	skript := filepath.Join(verz, "start.sh")
	os.WriteFile(skript, []byte("#!/bin/sh\n"), 0o755)
	hochladen(t, ts, c, id, verz, []string{"start.sh"}, "start.sh", "#!/bin/sh\necho neu\n")
	if fi, _ := os.Stat(skript); fi.Mode().Perm() != 0o755 || inhalt(t, skript) != "#!/bin/sh\necho neu\n" {
		t.Fatalf("Skript: %v %q", fi.Mode().Perm(), inhalt(t, skript))
	}

	// Verknuepfung: Der Inhalt dahinter wird ersetzt, die Verknuepfung bleibt.
	os.WriteFile(filepath.Join(verz, "ziel.txt"), []byte("alt"), 0o644)
	os.Symlink("ziel.txt", filepath.Join(verz, "link.txt"))
	if code, a = hochladen(t, ts, c, id, verz, []string{"link.txt"}, "link.txt", "durch-link"); code != 200 {
		t.Fatalf("Verknuepfung: %d %+v", code, a)
	}
	if fi, _ := os.Lstat(filepath.Join(verz, "link.txt")); fi.Mode()&os.ModeSymlink == 0 || inhalt(t, filepath.Join(verz, "ziel.txt")) != "durch-link" {
		t.Fatal("Verknuepfung ersetzt statt Inhalt dahinter")
	}

	// Ordner gleichen Namens laesst sich nicht durch eine Datei ersetzen.
	os.Mkdir(filepath.Join(verz, "ordner"), 0o755)
	if code, a = hochladen(t, ts, c, id, verz, []string{"ordner"}, "ordner", "x"); code != 409 || !strings.Contains(a.Fehler, "Ordner") || len(a.Vorhanden) != 0 {
		t.Fatalf("Ordner ersetzen: %d %+v", code, a)
	}
	if fi, err := os.Stat(filepath.Join(verz, "ordner")); err != nil || !fi.IsDir() {
		t.Fatal("Ordner beschaedigt")
	}
	if r := tmpReste(t, verz); len(r) > 0 {
		t.Fatalf("temporaere Dateien liegen herum: %v", r)
	}
}

func TestUmbenennenUeberschreibtNichtOhneErlaubnis(t *testing.T) {
	_, c, id, _ := dateiSitzung(t)
	verz := t.TempDir()
	a, b := filepath.Join(verz, "a.txt"), filepath.Join(verz, "b.txt")
	os.WriteFile(a, []byte("A"), 0o644)
	os.WriteFile(b, []byte("B - wichtige Daten"), 0o644)
	aktion := func(art, pfad, nach string, ersetzen bool) (int, string) {
		var aus struct{ Fehler string }
		code := c.rufe("POST", "/api/sitzungen/"+id+"/aktion", map[string]any{"art": art, "pfad": pfad, "nach": nach, "ersetzen": ersetzen}, &aus)
		return code, aus.Fehler
	}

	if code, text := aktion("umbenennen", a, b, false); code != 409 || !strings.Contains(text, "b.txt") {
		t.Fatalf("Umbenennen auf vorhandenen Namen: %d %q", code, text)
	}
	if inhalt(t, b) != "B - wichtige Daten" || inhalt(t, a) != "A" {
		t.Fatal("Datei still ueberschrieben")
	}
	if code, _ := aktion("umbenennen", a, b, true); code != 200 || inhalt(t, b) != "A" {
		t.Fatalf("Ersetzen nach Rueckfrage: %d %q", code, inhalt(t, b))
	}
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Fatal("Quelle nach Umbenennen noch da")
	}

	// Nur Gross-/Kleinschreibung aendern geht, eine andere Datei mit diesem Namen bleibt geschuetzt.
	k := filepath.Join(verz, "klein.txt")
	os.WriteFile(k, []byte("k"), 0o644)
	if code, text := aktion("umbenennen", k, filepath.Join(verz, "Klein.txt"), false); code != 200 || inhalt(t, filepath.Join(verz, "Klein.txt")) != "k" {
		t.Fatalf("Gross/klein: %d %q", code, text)
	}
	os.WriteFile(filepath.Join(verz, "x.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(verz, "X.txt"), []byte("X"), 0o644)
	if code, _ := aktion("umbenennen", filepath.Join(verz, "x.txt"), filepath.Join(verz, "X.txt"), false); code != 409 || inhalt(t, filepath.Join(verz, "X.txt")) != "X" {
		t.Fatalf("Gross/klein mit vorhandenem Namen: %d", code)
	}

	// Ordner gegen Ordner und Ordner in sich selbst.
	os.Mkdir(filepath.Join(verz, "o1"), 0o755)
	os.Mkdir(filepath.Join(verz, "o2"), 0o755)
	if code, _ := aktion("umbenennen", filepath.Join(verz, "o1"), filepath.Join(verz, "o2"), true); code != 409 {
		t.Fatalf("Ordner ersetzen: %d", code)
	}
	if code, text := aktion("umbenennen", filepath.Join(verz, "o1"), filepath.Join(verz, "o1", "innen"), false); code != 400 || text == "" {
		t.Fatalf("Ordner in sich selbst: %d %q", code, text)
	}
	if code, text := aktion("ordner", filepath.Join(verz, "o1"), "", false); code != 409 || !strings.Contains(text, "o1") {
		t.Fatalf("Ordner doppelt anlegen: %d %q", code, text)
	}
}

func TestHerunterladenLaenge(t *testing.T) {
	ts, c, id, _ := dateiSitzung(t)
	verz := t.TempDir()
	laden := func(p string) (*http.Response, string) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/sitzungen/"+id+"/laden?pfad="+url.QueryEscape(p), nil)
		res, err := c.h.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res, string(b)
	}

	gross := strings.Repeat("0123456789", 20000) // 200 kB: ueber Puffer und SFTP-Paketgroesse
	os.WriteFile(filepath.Join(verz, "gross.bin"), []byte(gross), 0o644)
	if res, body := laden(filepath.Join(verz, "gross.bin")); res.StatusCode != 200 || res.ContentLength != int64(len(gross)) || body != gross {
		t.Fatalf("gewoehnliche Datei: %d Laenge %d, %d Bytes", res.StatusCode, res.ContentLength, len(body))
	}
	os.WriteFile(filepath.Join(verz, "leer"), nil, 0o644)
	if res, body := laden(filepath.Join(verz, "leer")); res.StatusCode != 200 || body != "" {
		t.Fatalf("leere Datei: %d %q", res.StatusCode, body)
	}

	// /proc meldet Groesse 0, hat aber Inhalt: keine falsche Laengenangabe.
	if want, err := os.ReadFile("/proc/version"); err == nil && len(want) > 0 {
		res, body := laden("/proc/version")
		if res.StatusCode != 200 || body == "" || body != string(want) {
			t.Fatalf("/proc/version: %d, %d Bytes (lokal %d)", res.StatusCode, len(body), len(want))
		}
		if res.ContentLength != -1 && res.ContentLength != int64(len(body)) {
			t.Fatalf("/proc/version: Content-Length %d, erhalten %d", res.ContentLength, len(body))
		}
	}

	// Ordner und Geraete lassen sich nicht herunterladen, Fehler sind deutsche Saetze.
	for _, p := range []string{verz, "/dev/null"} {
		res, body := laden(p)
		if res.StatusCode != 400 || strings.Contains(body, "sftp") {
			t.Fatalf("%s: %d %s", p, res.StatusCode, body)
		}
	}
	res, body := laden(filepath.Join(verz, "gibts-nicht"))
	if res.StatusCode != 404 || strings.Contains(body, "sftp") || strings.Contains(body, "SSH_FX") {
		t.Fatalf("fehlende Datei: %d %s", res.StatusCode, body)
	}

	// Liste: Datei statt Ordner und fehlender Ordner ergeben verstaendliche Saetze.
	var aus struct{ Fehler string }
	if code := c.rufe("GET", "/api/sitzungen/"+id+"/dateien?pfad="+url.QueryEscape(filepath.Join(verz, "leer")), nil, &aus); code != 400 || !strings.Contains(aus.Fehler, "kein Ordner") {
		t.Fatalf("Liste auf Datei: %d %q", code, aus.Fehler)
	}
}

// ---------- Eingebetteter SFTP-Server mit Stoerungen ----------

// SFTP-Pakete und -Codes (draft-ietf-secsh-filexfer-02), soweit die Stoerregeln sie brauchen.
const (
	fxpInit     = 1
	fxpVersion  = 2
	fxpOpen     = 3
	fxpClose    = 4
	fxpRead     = 5
	fxpWrite    = 6
	fxpFstat    = 8
	fxpFsetstat = 10
	fxpRename   = 18
	fxpStatus   = 101
	fxpHandle   = 102
	fxpExtended = 200

	fxfWrite = 0x02
	fxfCreat = 0x08
	fxfTrunc = 0x10

	fxPermissionDenied = 3
	fxFailure          = 4
)

// anfrage beschreibt eine SFTP-Anfrage fuer eine Stoerregel. Bei Anfragen auf einen
// Handle (WRITE, CLOSE …) stehen pfad und pflags des zugehoerigen OPEN.
type anfrage struct {
	typ        byte
	pfad, nach string // nach: RENAME und posix-rename
	pflags     uint32
	ext        string
}

// stoerung beantwortet eine Anfrage mit einem SFTP-Statuscode, statt sie weiterzugeben (0: weitergeben).
type stoerung func(a anfrage) uint32

func paketLesen(r io.Reader) ([]byte, error) {
	var l [4]byte
	if _, err := io.ReadFull(r, l[:]); err != nil {
		return nil, err
	}
	b := make([]byte, binary.BigEndian.Uint32(l[:]))
	_, err := io.ReadFull(r, b)
	return b, err
}

func paketSchreiben(w io.Writer, b []byte) error {
	_, err := w.Write(append(binary.BigEndian.AppendUint32(nil, uint32(len(b))), b...))
	return err
}

func zeichenkette(b []byte) (string, []byte) {
	if len(b) < 4 || uint32(len(b)-4) < binary.BigEndian.Uint32(b) {
		return "", nil
	}
	n := binary.BigEndian.Uint32(b)
	return string(b[4 : 4+n]), b[4+n:]
}

// stoerClient verbindet einen SFTP-Client ueber einen Zwischenhaendler mit einem eingebetteten
// SFTP-Server (lokales Dateisystem). ohnePosix verschweigt dem Client die Erweiterungen (wie
// ein Dienst ohne posix-rename), regel beantwortet ausgewaehlte Anfragen mit einem Fehler.
func stoerClient(t *testing.T, ohnePosix bool, regel stoerung) *sftp.Client {
	t.Helper()
	appSeite, zuApp := net.Pipe()
	zuServer, serverSeite := net.Pipe()
	srv, err := sftp.NewServer(serverSeite)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	var schreibMu, mu sync.Mutex
	anApp := func(b []byte) error {
		schreibMu.Lock()
		defer schreibMu.Unlock()
		return paketSchreiben(zuApp, b)
	}
	offen := map[uint32]anfrage{}   // OPEN nach Anfrage-ID, bis der Handle kommt
	handles := map[string]anfrage{} // OPEN nach Handle
	// App -> Server: Anfragen pruefen, gestoerte selbst beantworten.
	go func() {
		defer zuServer.Close()
		for {
			b, err := paketLesen(zuApp)
			if err != nil || len(b) == 0 {
				return
			}
			if b[0] == fxpInit || len(b) < 5 {
				if paketSchreiben(zuServer, b) != nil {
					return
				}
				continue
			}
			a := anfrage{typ: b[0]}
			id, rest := binary.BigEndian.Uint32(b[1:5]), b[5:]
			switch b[0] {
			case fxpOpen:
				a.pfad, rest = zeichenkette(rest)
				if len(rest) >= 4 {
					a.pflags = binary.BigEndian.Uint32(rest)
				}
				mu.Lock()
				offen[id] = a
				mu.Unlock()
			case fxpRename:
				a.pfad, rest = zeichenkette(rest)
				a.nach, _ = zeichenkette(rest)
			case fxpExtended:
				a.ext, rest = zeichenkette(rest)
				a.pfad, rest = zeichenkette(rest)
				a.nach, _ = zeichenkette(rest)
			case fxpClose, fxpRead, fxpWrite, fxpFstat, fxpFsetstat:
				h, _ := zeichenkette(rest)
				mu.Lock()
				if o, ok := handles[h]; ok {
					a.pfad, a.pflags = o.pfad, o.pflags
				}
				mu.Unlock()
			default:
				a.pfad, _ = zeichenkette(rest)
			}
			if regel != nil {
				if code := regel(a); code != 0 {
					mu.Lock()
					delete(offen, id)
					mu.Unlock()
					antwort := binary.BigEndian.AppendUint32([]byte{fxpStatus}, id)
					antwort = binary.BigEndian.AppendUint32(antwort, code)
					antwort = binary.BigEndian.AppendUint32(antwort, 0) // Text
					antwort = binary.BigEndian.AppendUint32(antwort, 0) // Sprache
					if anApp(antwort) != nil {
						return
					}
					continue
				}
			}
			if paketSchreiben(zuServer, b) != nil {
				return
			}
		}
	}()
	// Server -> App: Erweiterungen verschweigen, Handles den OPEN-Anfragen zuordnen.
	go func() {
		defer zuApp.Close()
		for {
			b, err := paketLesen(zuServer)
			if err != nil || len(b) == 0 {
				return
			}
			switch {
			case b[0] == fxpVersion && ohnePosix && len(b) >= 5:
				b = b[:5] // nur die Version, keine Erweiterungen
			case b[0] == fxpHandle && len(b) >= 5:
				id := binary.BigEndian.Uint32(b[1:5])
				h, _ := zeichenkette(b[5:])
				mu.Lock()
				if o, ok := offen[id]; ok {
					handles[h] = o
					delete(offen, id)
				}
				mu.Unlock()
			}
			if anApp(b) != nil {
				return
			}
		}
	}()
	c, err := sftp.NewClientPipe(appSeite, appSeite)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close(); appSeite.Close(); serverSeite.Close() })
	return c
}

func inode(t *testing.T, p string) (ino uint64, uid, gid uint32, mode os.FileMode) {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	st := fi.Sys().(*syscall.Stat_t)
	return st.Ino, st.Uid, st.Gid, fi.Mode()
}

// meldungVon liefert den Katalogschluessel eines dateiFehlers (sonst "").
func meldungVon(err error) (*dateiFehler, string) {
	var df *dateiFehler
	if errors.As(err, &df) {
		return df, df.m.Schluessel
	}
	return nil, ""
}

// abbrechenderLeser liefert ein Stueck und bricht dann ab wie ein Browser im Funkloch.
type abbrechenderLeser struct{ gesendet bool }

func (l *abbrechenderLeser) Read(p []byte) (int, error) {
	if l.gesendet {
		return 0, io.ErrUnexpectedEOF
	}
	l.gesendet = true
	return copy(p, "BRUCHST"), nil
}

// Ersetzen behaelt die Datei selbst: Besitzer, Gruppe, Rechte, Inode und harte Verknuepfungen.
func TestHochladenErsetzenBehaeltDatei(t *testing.T) {
	ts, c, id, _ := dateiSitzung(t)
	verz := t.TempDir()
	p := filepath.Join(verz, "geteilt.conf")
	os.WriteFile(p, []byte("alt"), 0o664)
	if err := os.Chown(p, 65534, 100); err != nil {
		t.Skipf("chown braucht root: %v", err)
	}
	os.Chmod(p, 0o664)
	os.Link(p, filepath.Join(verz, "zweiter-name.conf"))
	ino, _, _, _ := inode(t, p)

	if code, a := hochladen(t, ts, c, id, verz, []string{"geteilt.conf"}, "geteilt.conf", "neuer Inhalt"); code != 200 {
		t.Fatalf("Ersetzen: %d %+v", code, a)
	}
	ino2, uid, gid, mode := inode(t, p)
	if ino2 != ino || uid != 65534 || gid != 100 || mode.Perm() != 0o664 {
		t.Fatalf("Datei nicht erhalten: Inode %d->%d, %d:%d, %v", ino, ino2, uid, gid, mode)
	}
	if inhalt(t, p) != "neuer Inhalt" || inhalt(t, filepath.Join(verz, "zweiter-name.conf")) != "neuer Inhalt" {
		t.Fatal("Inhalt bzw. harte Verknuepfung nicht ersetzt")
	}
	if r := tmpReste(t, verz); len(r) > 0 {
		t.Fatalf("temporaere Dateien liegen herum: %v", r)
	}
}

// Ordner ohne Schreibrecht, Datei beschreibbar: wie frueher direkt hineinschreiben.
func TestHochladenOrdnerOhneSchreibrecht(t *testing.T) {
	verz := t.TempDir()
	p := filepath.Join(verz, "datei.txt")
	os.WriteFile(p, []byte("alt"), 0o644)
	ino, _, _, _ := inode(t, p)
	c := stoerClient(t, false, func(a anfrage) uint32 {
		if a.typ == fxpOpen && a.pflags&fxfCreat != 0 && strings.HasSuffix(a.pfad, ".sshapp-upload") {
			return fxPermissionDenied
		}
		return 0
	})
	if err := hochladenEine(c, verz, "datei.txt", true, strings.NewReader("direkt")); err != nil {
		t.Fatalf("direkt schreiben: %v", err)
	}
	if ino2, _, _, _ := inode(t, p); inhalt(t, p) != "direkt" || ino2 != ino {
		t.Fatalf("Inhalt %q, Inode %d->%d", inhalt(t, p), ino, ino2)
	}
	// Abbruch in diesem Fall: eigene Meldung (die Datei ist jetzt unvollstaendig).
	err := hochladenEine(c, verz, "datei.txt", true, &abbrechenderLeser{})
	if _, k := meldungVon(err); k != "dateien.hochladen.abbruchDirekt" || !errors.Is(err, errUebertragungAbgebrochen) {
		t.Fatalf("Abbruch beim direkten Schreiben: %v (%s)", err, k)
	}
	// Eine neue Datei braucht das Schreibrecht im Ordner: keine Hintertuer.
	if err := hochladenEine(c, verz, "neu.txt", false, strings.NewReader("x")); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("neue Datei ohne Ordnerrecht: %v", err)
	}
}

// Eine Datei, die der Benutzer nicht schreiben darf, wird nicht still ersetzt (frueher «permission denied»).
func TestHochladenFremdeDateiAbgelehnt(t *testing.T) {
	verz := t.TempDir()
	p := filepath.Join(verz, "fremd.conf")
	os.WriteFile(p, []byte("gehoert root"), 0o644)
	c := stoerClient(t, false, func(a anfrage) uint32 {
		if a.typ == fxpOpen && a.pfad == p && a.pflags&fxfWrite != 0 {
			return fxPermissionDenied
		}
		return 0
	})
	err := hochladenEine(c, verz, "fremd.conf", true, strings.NewReader("neu"))
	if df, k := meldungVon(err); k != "dateien.hochladen.keinSchreibrecht" || df.code != http.StatusForbidden {
		t.Fatalf("fremde Datei: %v", err)
	}
	if inhalt(t, p) != "gehoert root" || len(tmpReste(t, verz)) > 0 {
		t.Fatal("fremde Datei veraendert oder Reste")
	}
	if code, text := sftpText("en", err); code != 403 || !strings.Contains(text, "“fremd.conf”") {
		t.Fatalf("englische Meldung: %d %q", code, text)
	}
}

// Laesst sich die Datei nicht zum Schreiben oeffnen (z.B. «Text file busy»), ersetzt das
// Hochladen sie per Umbenennen und uebernimmt Rechte und Besitzer.
func TestHochladenBelegteDateiPerUmbenennen(t *testing.T) {
	for _, ohnePosix := range []bool{false, true} {
		verz := t.TempDir()
		p := filepath.Join(verz, "dienst")
		os.WriteFile(p, []byte("v1"), 0o750)
		os.Chmod(p, 0o750)
		root := os.Chown(p, 65534, 100) == nil
		c := stoerClient(t, ohnePosix, func(a anfrage) uint32 {
			if a.typ == fxpOpen && a.pfad == p && a.pflags&fxfWrite != 0 && a.pflags&fxfCreat == 0 {
				return fxFailure
			}
			return 0
		})
		if err := hochladenEine(c, verz, "dienst", true, strings.NewReader("v2")); err != nil {
			t.Fatalf("ohnePosix=%v: %v", ohnePosix, err)
		}
		_, uid, gid, mode := inode(t, p)
		if inhalt(t, p) != "v2" || mode.Perm() != 0o750 || (root && (uid != 65534 || gid != 100)) {
			t.Fatalf("ohnePosix=%v: %q %v %d:%d", ohnePosix, inhalt(t, p), mode, uid, gid)
		}
		if r, _ := filepath.Glob(filepath.Join(verz, ".*.sshapp-*")); len(r) > 0 {
			t.Fatalf("ohnePosix=%v: Reste %v", ohnePosix, r)
		}
	}
}

// Echtes «Text file busy»: ein laufendes Programm laesst sich trotzdem ersetzen.
func TestHochladenLaufendesProgramm(t *testing.T) {
	quelle, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("kein sleep")
	}
	verz := t.TempDir()
	prog := filepath.Join(verz, "prog")
	b, _ := os.ReadFile(quelle)
	os.WriteFile(prog, b, 0o755)
	cmd := exec.Command(prog, "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("Programm laesst sich nicht starten: %v", err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	if f, err := os.OpenFile(prog, os.O_WRONLY, 0); err == nil {
		f.Close()
		t.Skip("kein ETXTBSY auf diesem System")
	}
	ts, c, id, _ := dateiSitzung(t)
	if code, a := hochladen(t, ts, c, id, verz, []string{"prog"}, "prog", "#!/bin/sh\n"); code != 200 {
		t.Fatalf("laufendes Programm ersetzen: %d %+v", code, a)
	}
	if _, _, _, mode := inode(t, prog); inhalt(t, prog) != "#!/bin/sh\n" || mode.Perm() != 0o755 {
		t.Fatalf("Programm: %q %v", inhalt(t, prog), mode)
	}
}

// Scheitert das Schreiben in die Datei mittendrin, bleibt die vollstaendige Fassung liegen.
func TestHochladenEinsetzenScheitert(t *testing.T) {
	verz := t.TempDir()
	p := filepath.Join(verz, "gross.bin")
	os.WriteFile(p, []byte("ALT-INHALT"), 0o644)
	c := stoerClient(t, false, func(a anfrage) uint32 {
		if a.typ == fxpWrite && a.pfad == p && a.pflags&fxfTrunc != 0 {
			return fxFailure
		}
		return 0
	})
	err := hochladenEine(c, verz, "gross.bin", true, strings.NewReader("NEU-VOLLSTAENDIG"))
	df, k := meldungVon(err)
	reste := tmpReste(t, verz)
	if k != "dateien.hochladen.einsetzenFehler" || len(reste) != 1 {
		t.Fatalf("Fehler %v (%s), Reste %v", err, k, reste)
	}
	if inhalt(t, reste[0]) != "NEU-VOLLSTAENDIG" || df.m.Werte[1] != filepath.Base(reste[0]) {
		t.Fatalf("vollstaendige Fassung: %q, Meldung nennt %v", inhalt(t, reste[0]), df.m.Werte)
	}
}

// Ohne posix-rename: erst das Ziel beiseite legen; scheitert das Umbenennen, kommt es zurueck.
func TestUmbenennenOhnePosixRename(t *testing.T) {
	verz := t.TempDir()
	a, b := filepath.Join(verz, "a.txt"), filepath.Join(verz, "b.txt")
	reste := func() []string { m, _ := filepath.Glob(filepath.Join(verz, ".*.sshapp-alt")); return m }

	// Ohne Stoerung: ersetzt, nichts bleibt liegen.
	os.WriteFile(a, []byte("A"), 0o644)
	os.WriteFile(b, []byte("B"), 0o644)
	if err := umbenennen(stoerClient(t, true, nil), a, b, true); err != nil || inhalt(t, b) != "A" || len(reste()) > 0 {
		t.Fatalf("ohne posix-rename: %v %q %v", err, inhalt(t, b), reste())
	}
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Fatal("Quelle noch da")
	}

	// Das eigentliche Umbenennen scheitert: beide Dateien bleiben.
	os.WriteFile(a, []byte("A"), 0o644)
	os.WriteFile(b, []byte("B"), 0o644)
	c := stoerClient(t, true, func(x anfrage) uint32 {
		if x.typ == fxpRename && x.pfad == a {
			return fxFailure
		}
		return 0
	})
	if err := umbenennen(c, a, b, true); err == nil || inhalt(t, a) != "A" || inhalt(t, b) != "B" || len(reste()) > 0 {
		t.Fatalf("Umbenennen scheitert: %v, a=%q b=%q %v", err, inhalt(t, a), inhalt(t, b), reste())
	}

	// Auch das Zurueckholen scheitert: Das Original liegt beiseite, die Meldung nennt es.
	c = stoerClient(t, true, func(x anfrage) uint32 {
		if x.typ == fxpRename && x.nach == b {
			return fxFailure
		}
		return 0
	})
	err := umbenennen(c, a, b, true)
	df, k := meldungVon(err)
	r := reste()
	if k != "dateien.umbenennen.originalGesichert" || len(r) != 1 || inhalt(t, r[0]) != "B" || inhalt(t, a) != "A" {
		t.Fatalf("Zurueckholen scheitert: %v (%s) %v", err, k, r)
	}
	if df.m.Werte[1] != filepath.Base(r[0]) {
		t.Fatalf("Meldung nennt %v statt %s", df.m.Werte, filepath.Base(r[0]))
	}
}

// Ordner und Spezialdateien werden beim Hochladen uebersprungen und gesammelt, der Rest kommt an.
func TestHochladenOrdnerKonfliktUeberspringt(t *testing.T) {
	ts, c, id, _ := dateiSitzung(t)
	verz := t.TempDir()
	os.Mkdir(filepath.Join(verz, "ordner"), 0o755)
	if err := syscall.Mkfifo(filepath.Join(verz, "rohr"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, a := hochladen(t, ts, c, id, verz, []string{"ordner"}, "ordner", "x", "c.txt", "neu-c")
	if code != 409 || len(a.Vorhanden) != 0 || strings.Join(a.Gesperrt, ",") != "ordner" || strings.Join(a.Hochgeladen, ",") != "c.txt" {
		t.Fatalf("Ordner-Konflikt: %d %+v", code, a)
	}
	if strings.Count(a.Fehler, "ordner") != 1 || inhalt(t, filepath.Join(verz, "c.txt")) != "neu-c" {
		t.Fatalf("Meldung %q", a.Fehler)
	}
	// Spezialdatei: auch ohne Erlaubnis gar nicht erst als «vorhanden» (ersetzbar) melden.
	code, a = hochladen(t, ts, c, id, verz, nil, "rohr", "x", "ordner", "y")
	if code != 409 || len(a.Vorhanden) != 0 || strings.Join(a.Gesperrt, ",") != "rohr,ordner" {
		t.Fatalf("Spezialdatei: %d %+v", code, a)
	}
	if fi, err := os.Lstat(filepath.Join(verz, "rohr")); err != nil || fi.Mode()&os.ModeNamedPipe == 0 {
		t.Fatal("Pipe veraendert")
	}
}

// Eine Verknuepfung ohne Ziel: Ersetzen legt das Ziel an (wie cp), die Verknuepfung bleibt.
func TestHochladenVerknuepfungOhneZiel(t *testing.T) {
	ts, c, id, _ := dateiSitzung(t)
	verz := t.TempDir()
	os.Symlink("ziel.txt", filepath.Join(verz, "link.txt"))
	if code, a := hochladen(t, ts, c, id, verz, nil, "link.txt", "x"); code != 409 || strings.Join(a.Vorhanden, ",") != "link.txt" {
		t.Fatalf("ohne Erlaubnis: %d %+v", code, a)
	}
	if code, a := hochladen(t, ts, c, id, verz, []string{"link.txt"}, "link.txt", "durch-link"); code != 200 {
		t.Fatalf("mit Erlaubnis: %d %+v", code, a)
	}
	if fi, _ := os.Lstat(filepath.Join(verz, "link.txt")); fi.Mode()&os.ModeSymlink == 0 || inhalt(t, filepath.Join(verz, "ziel.txt")) != "durch-link" {
		t.Fatal("Ziel nicht angelegt oder Verknuepfung ersetzt")
	}
}

// holen ruft eine Adresse mit Sprachkopf ab.
func holen(t *testing.T, ts *httptest.Server, c *client, pfad, sprache string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", ts.URL+pfad, nil)
	req.Header.Set("X-SSH-App", "1")
	if sprache != "" {
		req.Header.Set("X-SSH-App-Sprache", sprache)
	}
	res, err := c.h.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("%s: Lesefehler nach %d Bytes: %v", pfad, len(b), err)
	}
	return res, string(b)
}

// Die Liste nennt die Art; nur gewoehnliche Dateien (auch hinter Verknuepfungen) sind «datei».
func TestDateienListeArt(t *testing.T) {
	ts, c, id, _ := dateiSitzung(t)
	verz := t.TempDir()
	os.WriteFile(filepath.Join(verz, "f.txt"), []byte("12345"), 0o644)
	os.Mkdir(filepath.Join(verz, "o"), 0o755)
	syscall.Mkfifo(filepath.Join(verz, "rohr"), 0o644)
	l, err := net.Listen("unix", filepath.Join(verz, "sock"))
	if err == nil {
		defer l.Close()
	}
	os.Symlink("f.txt", filepath.Join(verz, "zu-f"))
	os.Symlink("o", filepath.Join(verz, "zu-o"))
	os.Symlink("gibts-nicht", filepath.Join(verz, "kaputt"))
	os.Symlink("/dev/null", filepath.Join(verz, "zu-null"))
	var aus struct{ Eintraege []eintrag }
	if code := c.rufe("GET", "/api/sitzungen/"+id+"/dateien?pfad="+url.QueryEscape(verz), nil, &aus); code != 200 {
		t.Fatalf("Liste: %d", code)
	}
	art := map[string]string{}
	for _, e := range aus.Eintraege {
		art[e.Name] = e.Art
		if e.Name == "zu-f" && (e.Groesse != 5 || !e.Link) {
			t.Fatalf("Verknuepfung auf Datei: %+v", e)
		}
	}
	want := map[string]string{"f.txt": "datei", "o": "ordner", "rohr": "pipe", "zu-f": "datei", "zu-o": "ordner", "kaputt": "andere", "zu-null": "geraet"}
	if err == nil {
		want["sock"] = "socket"
	}
	for n, a := range want {
		if art[n] != a {
			t.Errorf("%s: Art %q, erwartet %q", n, art[n], a)
		}
	}
	_ = ts
}

// Die Pruefung vor dem Download liefert den Grund in der Sprache der Oberflaeche.
func TestHerunterladenPruefen(t *testing.T) {
	ts, c, id, _ := dateiSitzung(t)
	verz := t.TempDir()
	os.WriteFile(filepath.Join(verz, "a.txt"), []byte("abc"), 0o644)
	adresse := func(p string) string {
		return "/api/sitzungen/" + id + "/laden?pruefen=1&pfad=" + url.QueryEscape(p)
	}
	if res, body := holen(t, ts, c, adresse(filepath.Join(verz, "a.txt")), ""); res.StatusCode != 200 || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("gewoehnliche Datei: %d %s", res.StatusCode, body)
	}
	faelle := []struct {
		pfad, sprache string
		code          int
		text          string
	}{
		{"/dev/null", "en", 400, "Only regular files"},
		{"/dev/null", "de", 400, "Nur gewöhnliche Dateien"},
		{verz, "en", 400, "Folders can’t be downloaded"},
		{filepath.Join(verz, "fehlt"), "en", 404, "doesn’t exist"},
		{filepath.Join(verz, "fehlt"), "de", 404, "gibt es dort nicht"},
		{"relativ", "en", 400, "must be absolute"},
	}
	for _, f := range faelle {
		res, body := holen(t, ts, c, adresse(f.pfad), f.sprache)
		if res.StatusCode != f.code || !strings.Contains(body, f.text) {
			t.Errorf("%s (%s): %d %s", f.pfad, f.sprache, res.StatusCode, body)
		}
	}
	// Ohne Kopf: Parameter «sprache» (Download per Link).
	if res, body := holen(t, ts, c, adresse("/dev/null")+"&sprache=en", ""); res.StatusCode != 400 || !strings.Contains(body, "Only regular files") {
		t.Fatalf("Parameter sprache: %d %s", res.StatusCode, body)
	}
}

// Eine Verknuepfung nach /proc ist eine Pseudodatei: keine (falsche) Laengenangabe.
func TestHerunterladenProcVerknuepfung(t *testing.T) {
	ts, c, id, _ := dateiSitzung(t)
	verz := t.TempDir()
	geprueft := 0
	for _, ziel := range []string{"/proc/cpuinfo", "/proc/meminfo", "/proc/version"} {
		if _, err := os.Stat(ziel); err != nil {
			continue
		}
		link := filepath.Join(verz, filepath.Base(ziel))
		os.Symlink(ziel, link)
		res, body := holen(t, ts, c, "/api/sitzungen/"+id+"/laden?pfad="+url.QueryEscape(link), "")
		if res.StatusCode != 200 || body == "" || (res.ContentLength != -1 && res.ContentLength != int64(len(body))) {
			t.Fatalf("%s: %d, Laenge %d, erhalten %d", ziel, res.StatusCode, res.ContentLength, len(body))
		}
		geprueft++
	}
	if geprueft == 0 {
		t.Skip("kein /proc")
	}
}

// Meldungen beim Hochladen kommen in der Sprache der Anfrage.
func TestHochladenSprache(t *testing.T) {
	ts, c, id, _ := dateiSitzung(t)
	verz := t.TempDir()
	os.WriteFile(filepath.Join(verz, "a.txt"), []byte("alt"), 0o644)
	os.WriteFile(filepath.Join(verz, "b.txt"), []byte("alt"), 0o644)
	var mp bytes.Buffer
	mw := multipart.NewWriter(&mp)
	for _, n := range []string{"a.txt", "b.txt"} {
		fw, _ := mw.CreateFormFile("datei", n)
		fw.Write([]byte("neu"))
	}
	mw.Close()
	req, _ := http.NewRequest("POST", ts.URL+"/api/sitzungen/"+id+"/hochladen?pfad="+url.QueryEscape(verz), &mp)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-SSH-App", "1")
	req.Header.Set("X-SSH-App-Sprache", "en")
	res, err := c.h.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var a hochAntwort
	json.NewDecoder(res.Body).Decode(&a)
	if res.StatusCode != 409 || a.Fehler != "Already exist: “a.txt”, “b.txt”" {
		t.Fatalf("englisch: %d %q", res.StatusCode, a.Fehler)
	}
}

// TestSSHHalten startet nur auf Wunsch (SSHAPP_TESTSSH_HALTEN=<Sekunden>) den eingebetteten
// Test-SSH-Server (Benutzer pi, Passwort himbeere, SFTP = lokales Dateisystem) und haelt ihn
// offen: fuer Klicktests der Oberflaeche ohne echtes SSH-Ziel. Die Adresse steht in der
// Ausgabe und in der Datei SSHAPP_TESTSSH_DATEI (falls gesetzt).
func TestSSHHalten(t *testing.T) {
	sek, err := strconv.Atoi(os.Getenv("SSHAPP_TESTSSH_HALTEN"))
	if err != nil || sek <= 0 {
		t.Skip("nur fuer Klicktests (SSHAPP_TESTSSH_HALTEN=<Sekunden>)")
	}
	addr := sshTestServer(t, "pi", "himbeere")
	fmt.Println("TESTSSH", addr)
	if d := os.Getenv("SSHAPP_TESTSSH_DATEI"); d != "" {
		os.WriteFile(d, []byte(addr), 0o644)
	}
	time.Sleep(time.Duration(sek) * time.Second)
}
