package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

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
