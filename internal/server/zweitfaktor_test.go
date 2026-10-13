package server

import (
	"bytes"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uiff/ssh-app/internal/totp"
)

// --- Hilfen ---

// testUhr ist eine von Hand gestellte Uhr fuer die TOTP-Pruefung des Servers.
type testUhr struct {
	mu sync.Mutex
	t  time.Time
}

func (u *testUhr) jetzt() time.Time {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.t
}

func (u *testUhr) weiter(d time.Duration) {
	u.mu.Lock()
	u.t = u.t.Add(d)
	u.mu.Unlock()
}

func uhrStellen(s *Server) *testUhr {
	u := &testUhr{t: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)}
	s.uhr = u.jetzt
	return u
}

type antwort struct {
	code   int
	daten  map[string]any
	roh    []byte
	cookie bool // die Antwort setzt ein Anmelde-Cookie
}

func (a antwort) text() string { s, _ := a.daten["fehler"].(string); return s }

// tfAnfrage ruft die API (JSON) und liefert die ganze Antwort.
func tfAnfrage(t *testing.T, c *client, methode, pfad string, rumpf any, koepfe map[string]string) antwort {
	t.Helper()
	var r io.Reader
	if rumpf != nil {
		b, _ := json.Marshal(rumpf)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(methode, c.base+pfad, r)
	req.Header.Set("X-SSH-App", "1")
	for k, v := range koepfe {
		req.Header.Set(k, v)
	}
	res, err := c.h.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	a := antwort{code: res.StatusCode}
	a.roh, _ = io.ReadAll(res.Body)
	json.Unmarshal(a.roh, &a.daten)
	for _, ck := range res.Cookies() {
		if ck.Name == cookieName && ck.Value != "" {
			a.cookie = true
		}
	}
	return a
}

func anmeldenMit(t *testing.T, c *client, passwort, code string) antwort {
	t.Helper()
	rumpf := map[string]string{"benutzer": "admin", "passwort": passwort}
	if code != "" {
		rumpf["code"] = code
	}
	return tfAnfrage(t, c, "POST", "/api/anmelden", rumpf, nil)
}

// falscherCode liefert einen Code, der zu keinem Zeitschritt in +-Suchweite passt (sonst
// kaeme der Hinweis auf die Uhr).
func falscherCode(geheim []byte, u *testUhr) string {
	nah := map[string]bool{}
	for d := int64(-totp.Suchweite - 1); d <= totp.Suchweite+1; d++ {
		nah[code(geheim, u, d)] = true
	}
	for c := 123456; ; c = (c + 111111) % 1000000 {
		if x := fmt.Sprintf("%06d", c); !nah[x] {
			return x
		}
	}
}

// code liefert den TOTP-Code zur Uhr des Servers, versetzt um schritte.
func code(geheim []byte, u *testUhr, schritte int64) string {
	return totp.CodeBei(geheim, totp.Schritt(u.jetzt())+schritte)
}

// zweitfaktorEin richtet die Zwei-Faktor-Anmeldung ueber die API ein und liefert Geheimnis
// und Ersatzcodes. Danach steht die Uhr einen Schritt weiter (der Einrichtungscode ist verbraucht).
func zweitfaktorEin(t *testing.T, c *client, u *testUhr) ([]byte, []string) {
	t.Helper()
	b := tfAnfrage(t, c, "POST", "/api/zweitfaktor/beginnen", map[string]any{}, nil)
	if b.code != 200 {
		t.Fatalf("beginnen: %d %s", b.code, b.roh)
	}
	geheim, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ReplaceAll(b.daten["schluessel"].(string), " ", ""))
	if err != nil || len(geheim) != 20 {
		t.Fatalf("Schluessel %q: %v", b.daten["schluessel"], err)
	}
	e := tfAnfrage(t, c, "POST", "/api/zweitfaktor/einschalten", map[string]string{"passwort": testPasswort, "code": code(geheim, u, 0)}, nil)
	if e.code != 200 {
		t.Fatalf("einschalten: %d %s", e.code, e.roh)
	}
	var ersatz []string
	for _, x := range e.daten["ersatzcodes"].([]any) {
		ersatz = append(ersatz, x.(string))
	}
	u.weiter(totp.Periode * time.Second)
	return geheim, ersatz
}

// token liefert das Anmelde-Cookie des Clients.
func (c *client) token(t *testing.T) string {
	t.Helper()
	u, _ := url.Parse(c.base)
	for _, ck := range c.h.Jar.Cookies(u) {
		if ck.Name == cookieName {
			return ck.Value
		}
	}
	t.Fatal("kein Anmelde-Cookie")
	return ""
}

func statusVon(t *testing.T, c *client) map[string]any {
	t.Helper()
	return tfAnfrage(t, c, "GET", "/api/status", nil, nil).daten
}

var ersatzMuster = regexp.MustCompile(`^[2-9A-HJ-NP-Z]{4}-[2-9A-HJ-NP-Z]{4}$`)

// --- Tests ---

func TestZweitfaktorEinrichtenUndAnmelden(t *testing.T) {
	ts, s, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	u := uhrStellen(s)

	// Status: angemeldet mit «zweitfaktor», ohne Anmeldung nichts Neues.
	st := statusVon(t, c)
	if st["zweitfaktor"] != false || st["ersatzcodesUebrig"] != nil {
		t.Fatalf("Status vorher: %v", st)
	}
	fremd := neuerClient(t, ts.URL, ts.Client())
	if st := statusVon(t, fremd); st["angemeldet"] != false || st["zweitfaktor"] != nil || st["ersatzcodesUebrig"] != nil {
		t.Fatalf("Status ohne Anmeldung: %v", st)
	}
	if a := tfAnfrage(t, fremd, "POST", "/api/zweitfaktor/beginnen", map[string]any{}, nil); a.code != 401 {
		t.Fatalf("beginnen ohne Anmeldung: %d", a.code)
	}

	// Einschalten ohne begonnene Einrichtung.
	if a := tfAnfrage(t, c, "POST", "/api/zweitfaktor/einschalten", map[string]string{"passwort": testPasswort, "code": "123456"}, nil); a.code != 409 || !strings.HasPrefix(a.text(), "Die Einrichtung ist abgelaufen — bitte neu beginnen.") {
		t.Fatalf("ohne Einrichtung: %d %q", a.code, a.text())
	}

	b := tfAnfrage(t, c, "POST", "/api/zweitfaktor/beginnen", map[string]any{}, nil)
	if b.code != 200 {
		t.Fatalf("beginnen: %d %s", b.code, b.roh)
	}
	sch, _ := b.daten["schluessel"].(string)
	if !regexp.MustCompile(`^([A-Z2-7]{4} ){7}[A-Z2-7]{4}$`).MatchString(sch) {
		t.Fatalf("Schluessel: %q", sch)
	}
	roh := strings.ReplaceAll(sch, " ", "")
	if uri := b.daten["uri"]; uri != "otpauth://totp/Keyway:admin@127.0.0.1?secret="+roh+"&issuer=Keyway&algorithm=SHA1&digits=6&period=30" {
		t.Fatalf("URI: %v", uri)
	}
	bild, _ := b.daten["qr"].(string)
	if !strings.HasPrefix(bild, "data:image/svg+xml;base64,") {
		t.Fatalf("QR: %.60s", bild)
	}
	svg, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(bild, "data:image/svg+xml;base64,"))
	if err != nil || !bytes.HasPrefix(svg, []byte("<svg ")) || !bytes.Contains(svg, []byte(`shape-rendering="crispEdges"`)) || !bytes.Contains(svg, []byte(`fill="#fff"`)) {
		t.Fatalf("QR-SVG: %v %.200s", err, svg)
	}
	if b.daten["serverzeit"] != float64(u.jetzt().UnixMilli()) {
		t.Fatalf("Serverzeit: %v", b.daten["serverzeit"])
	}
	geheim, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(roh)

	// Pflichtfelder fehlen: 400, zaehlt nicht.
	ein := func(pw, cd string) antwort {
		return tfAnfrage(t, c, "POST", "/api/zweitfaktor/einschalten", map[string]string{"passwort": pw, "code": cd}, nil)
	}
	if a := ein(testPasswort, " "); a.code != 400 || a.text() != "Bitte den 6-stelligen Code aus der Authenticator-App eingeben" {
		t.Fatalf("ohne Code: %d %q", a.code, a.text())
	}
	if a := ein("", "123456"); a.code != 400 || a.text() != "Bitte das aktuelle Passwort eingeben" {
		t.Fatalf("ohne Passwort: %d %q", a.code, a.text())
	}
	// Falsches Passwort, falscher Code, Code mit falsch gehender Uhr: 400 (nicht 401: die
	// Oberflaeche wuerde abmelden), die Anmeldung bleibt. «feld» nennt das falsche Feld.
	if a := ein("falsch-falsch", code(geheim, u, 0)); a.code != 400 || a.text() != "Das Passwort ist falsch" || a.daten["feld"] != "passwort" {
		t.Fatalf("falsches Passwort: %d %s", a.code, a.roh)
	}
	falsch := "000000"
	if falsch == code(geheim, u, 0) || falsch == code(geheim, u, 1) || falsch == code(geheim, u, -1) {
		falsch = "111111"
	}
	if a := ein(testPasswort, falsch); a.code != 400 || a.text() != "Der Code stimmt nicht" || a.daten["feld"] != "code" {
		t.Fatalf("falscher Code: %d %s", a.code, a.roh)
	}
	// Beim Einrichten gibt es noch keine Ersatzcodes: Der Hinweis auf die Uhr raet nicht dazu.
	if a := ein(testPasswort, code(geheim, u, 20)); a.code != 400 || a.text() != "Der Code gehört zu einer Uhrzeit, die etwa 10 min später ist als die Uhr dieses Servers. Bitte die Uhrzeit von Server und Telefon prüfen." {
		t.Fatalf("Uhr geht vor: %d %q", a.code, a.text())
	}
	if a := tfAnfrage(t, c, "POST", "/api/zweitfaktor/einschalten", map[string]string{"passwort": testPasswort, "code": code(geheim, u, -6)}, englisch); a.code != 400 ||
		a.text() != "The code belongs to a time about 3 min earlier than this server’s clock. Check the time on the server and on your phone." {
		t.Fatalf("Uhr geht nach, englisch: %d %q", a.code, a.text())
	}
	if statusVon(t, c)["zweitfaktor"] != false || s.tr.ZweitfaktorAktiv() {
		t.Fatal("gescheitertes Einschalten hat eingeschaltet")
	}
	// Fehlversuche beim Einrichten (neues Geheimnis, Anmeldung und Passwort bewiesen) zaehlen
	// nicht fuer die kontoweite Bremse und verbrennen keine Zeitschritte.
	s.tfMu.Lock()
	nFehl, nVerbrannt := s.tfFehlerFolge+s.tfFehlerMelden, len(s.tfVerbrannt)
	s.tfMu.Unlock()
	if nFehl != 0 || nVerbrannt != 0 {
		t.Fatalf("Einrichten: %d kontoweite Fehler, %d verbrannte Schritte", nFehl, nVerbrannt)
	}

	// Richtig (Leerzeichen im Code erlaubt): zehn Ersatzcodes.
	cd := code(geheim, u, 0)
	e := ein(testPasswort, cd[:3]+" "+cd[3:])
	if e.code != 200 {
		t.Fatalf("einschalten: %d %s", e.code, e.roh)
	}
	liste, _ := e.daten["ersatzcodes"].([]any)
	gesehen := map[string]bool{}
	var ersatz []string
	for _, x := range liste {
		c, _ := x.(string)
		if !ersatzMuster.MatchString(c) || gesehen[c] {
			t.Fatalf("Ersatzcode %q", c)
		}
		gesehen[c] = true
		ersatz = append(ersatz, c)
	}
	if len(ersatz) != 10 {
		t.Fatalf("%d Ersatzcodes", len(ersatz))
	}
	if st := statusVon(t, c); st["zweitfaktor"] != true || st["ersatzcodesUebrig"] != float64(10) {
		t.Fatalf("Status eingeschaltet: %v", st)
	}
	meta, _ := s.st.Meta(MetaTresor)
	if !bytes.Equal(meta, s.tr.KopfJSON()) || !bytes.Contains(meta, []byte(`"tf"`)) {
		t.Fatal("Kopf auf der Platte passt nicht")
	}
	if a := tfAnfrage(t, c, "POST", "/api/zweitfaktor/beginnen", map[string]any{}, nil); a.code != 409 {
		t.Fatalf("beginnen bei eingeschaltetem Zweitfaktor: %d", a.code)
	}
	if a := ein(testPasswort, cd); a.code != 409 {
		t.Fatalf("zweimal einschalten: %d", a.code)
	}

	// Anmelden: ohne Code kein Cookie, Tresor bleibt gesperrt, Frage nach dem Code.
	tfAnfrage(t, c, "POST", "/api/abmelden", nil, nil)
	if s.tr.Offen() {
		t.Fatal("nach Abmelden offen")
	}
	a := anmeldenMit(t, c, testPasswort, "")
	if a.code != 401 || a.daten["zweitfaktor"] != true || a.text() != "Bitte den 6-stelligen Code aus der Authenticator-App eingeben" || a.cookie || s.tr.Offen() {
		t.Fatalf("ohne Code: %d %s cookie=%v offen=%v", a.code, a.roh, a.cookie, s.tr.Offen())
	}
	if code := c.rufe("GET", "/api/daten", nil, nil); code != 401 {
		t.Fatalf("Daten ohne zweiten Faktor: %d", code)
	}
	// Derselbe Code wie beim Einschalten (gleicher Zeitschritt): verbraucht.
	if a := anmeldenMit(t, c, testPasswort, cd); a.code != 401 || a.daten["zweitfaktor"] != true || a.text() != "Dieser Code wurde schon verwendet — bitte auf den nächsten Code warten" || a.cookie || s.tr.Offen() {
		t.Fatalf("Wiederverwendung: %d %s", a.code, a.roh)
	}
	u.weiter(30 * time.Second)
	cd = code(geheim, u, 0)
	if a := anmeldenMit(t, c, testPasswort, cd); a.code != 200 || !a.cookie || a.daten["ersatzcodesUebrig"] != nil {
		t.Fatalf("mit Code: %d %s", a.code, a.roh)
	}
	if code := c.rufe("GET", "/api/daten", nil, nil); code != 200 {
		t.Fatalf("Daten nach Anmeldung: %d", code)
	}
	tfAnfrage(t, c, "POST", "/api/abmelden", nil, nil)
	if a := anmeldenMit(t, c, testPasswort, cd); a.code != 401 || !strings.Contains(a.text(), "schon verwendet") || a.cookie {
		t.Fatalf("Wiederverwendung nach Anmeldung: %d %s", a.code, a.roh)
	}
	// Ein Code des naechsten Schritts gilt (Fenster +-1), danach ist auch er verbraucht.
	if a := anmeldenMit(t, c, testPasswort, code(geheim, u, 1)); a.code != 200 {
		t.Fatalf("Code +1 Schritt: %d %s", a.code, a.roh)
	}
	tfAnfrage(t, c, "POST", "/api/abmelden", nil, nil)

	// Ersatzcode (klein, mit Leerzeichen getippt): gilt einmal, der Zaehler sinkt.
	getippt := strings.ToLower(strings.Replace(ersatz[3], "-", " ", 1))
	a = anmeldenMit(t, c, testPasswort, getippt)
	if a.code != 200 || !a.cookie || a.daten["ersatzcodesUebrig"] != float64(9) {
		t.Fatalf("Ersatzcode: %d %s", a.code, a.roh)
	}
	if st := statusVon(t, c); st["ersatzcodesUebrig"] != float64(9) {
		t.Fatalf("Status nach Ersatzcode: %v", st)
	}
	meta, _ = s.st.Meta(MetaTresor)
	if !bytes.Equal(meta, s.tr.KopfJSON()) {
		t.Fatal("verbrauchter Ersatzcode nicht gespeichert")
	}
	tfAnfrage(t, c, "POST", "/api/abmelden", nil, nil)
	if a := anmeldenMit(t, c, testPasswort, ersatz[3]); a.code != 401 || a.text() != "Der Code stimmt nicht" || a.cookie {
		t.Fatalf("Ersatzcode zweimal: %d %s", a.code, a.roh)
	}

	// Falsch gehende Uhr bei der Anmeldung: Hinweis in beide Richtungen, auch englisch.
	if a := anmeldenMit(t, c, testPasswort, code(geheim, u, 3)); a.code != 401 || !strings.Contains(a.text(), "etwa 2 min später") {
		t.Fatalf("Uhr +90 s: %d %q", a.code, a.text())
	}
	en := tfAnfrage(t, c, "POST", "/api/anmelden", map[string]string{"benutzer": "admin", "passwort": testPasswort, "code": code(geheim, u, -20)}, englisch)
	if en.code != 401 || en.daten["zweitfaktor"] != true || !strings.HasPrefix(en.text(), "The code belongs to a time about 10 min earlier than this server’s clock.") {
		t.Fatalf("Uhr -10 min englisch: %d %q", en.code, en.text())
	}
	if s.tr.Offen() {
		t.Fatal("gescheiterte Anmeldungen haben den Tresor geoeffnet")
	}
}

// Falsche Codes zaehlen wie falsche Passwoerter: nach maxFehlversuche ist die Adresse gesperrt.
func TestZweitfaktorFalscheCodesSperren(t *testing.T) {
	_, s, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	u := uhrStellen(s)
	geheim, _ := zweitfaktorEin(t, c, u)
	tfAnfrage(t, c, "POST", "/api/abmelden", nil, nil)
	richtig := code(geheim, u, 0)
	falsch := "000000"
	for falsch == richtig || falsch == code(geheim, u, 1) || falsch == code(geheim, u, -1) {
		falsch = "99999" + falsch[:1]
	}
	// Ohne Code ist ein Versuch neutral.
	for i := 0; i < maxFehlversuche+2; i++ {
		if a := anmeldenMit(t, c, testPasswort, ""); a.code != 401 || a.daten["zweitfaktor"] != true {
			t.Fatalf("ohne Code %d: %d", i, a.code)
		}
	}
	for i := 0; i < maxFehlversuche; i++ {
		if a := anmeldenMit(t, c, testPasswort, falsch); a.code != 401 || a.daten["zweitfaktor"] != true {
			t.Fatalf("falscher Code %d: %d %s", i, a.code, a.roh)
		}
	}
	if a := anmeldenMit(t, c, testPasswort, richtig); a.code != 429 || a.cookie || s.tr.Offen() {
		t.Fatalf("Sperre greift nicht: %d", a.code)
	}
}

// Ein falsches Passwort verraet nicht, ob die Zwei-Faktor-Anmeldung eingeschaltet ist.
func TestZweitfaktorFalschesPasswortGleicheAntwort(t *testing.T) {
	_, s, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	u := uhrStellen(s)
	andere := neuerClient(t, c.base, c.h)
	vorher := anmeldenMit(t, andere, "falsch-falsch", "")
	vorherMitCode := anmeldenMit(t, andere, "falsch-falsch", "123456")
	zweitfaktorEin(t, c, u)
	nachher := anmeldenMit(t, andere, "falsch-falsch", "")
	nachherMitCode := anmeldenMit(t, andere, "falsch-falsch", code(nil, u, 0))
	for _, a := range []antwort{vorher, vorherMitCode, nachher, nachherMitCode} {
		if a.code != 401 || !bytes.Equal(a.roh, vorher.roh) || a.cookie || a.daten["zweitfaktor"] != nil {
			t.Fatalf("falsches Passwort: %d %s (vorher %s)", a.code, a.roh, vorher.roh)
		}
	}
}

// Ist der Tresor fuer eine andere Sitzung offen, bleibt er es; die neue Sitzung bekommt nichts.
func TestZweitfaktorFalschLaesstAndereSitzungOffen(t *testing.T) {
	ts, s, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	u := uhrStellen(s)
	zweitfaktorEin(t, c, u)
	b := neuerClient(t, ts.URL, ts.Client())
	for _, cd := range []string{"", "000000", "ABCD-EFGH"} {
		if a := anmeldenMit(t, b, testPasswort, cd); a.code != 401 || a.cookie {
			t.Fatalf("B mit %q: %d %s", cd, a.code, a.roh)
		}
	}
	if code := b.rufe("GET", "/api/daten", nil, nil); code != 401 {
		t.Fatalf("B ohne zweiten Faktor: %d", code)
	}
	if code := c.rufe("GET", "/api/daten", nil, nil); code != 200 || !s.tr.Offen() {
		t.Fatalf("A nach gescheiterter Anmeldung von B: %d offen=%v", code, s.tr.Offen())
	}
}

func TestZweitfaktorErsatzcodesUndAusschalten(t *testing.T) {
	_, s, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	u := uhrStellen(s)
	geheim, alt := zweitfaktorEin(t, c, u)

	neu := func(pw, cd string) antwort {
		return tfAnfrage(t, c, "POST", "/api/zweitfaktor/ersatzcodes", map[string]string{"passwort": pw, "code": cd}, nil)
	}
	if a := neu(testPasswort, ""); a.code != 400 {
		t.Fatalf("ohne Code: %d", a.code)
	}
	if a := neu("falsch-falsch", code(geheim, u, 0)); a.code != 400 || a.text() != "Das Passwort ist falsch" || a.daten["feld"] != "passwort" {
		t.Fatalf("falsches Passwort: %d %s", a.code, a.roh)
	}
	if a := neu(testPasswort, "ZZZZ-ZZZZ"); a.code != 400 || a.text() != "Der Code stimmt nicht" || a.daten["feld"] != "code" {
		t.Fatalf("falscher Code: %d %s", a.code, a.roh)
	}
	// Ausserhalb des Einrichtens gibt es Ersatzcodes: Der Hinweis auf die Uhr nennt sie.
	if a := neu(testPasswort, code(geheim, u, -10)); a.code != 400 || !strings.Contains(a.text(), "etwa 5 min früher") || !strings.HasSuffix(a.text(), "oder einen Ersatzcode verwenden.") {
		t.Fatalf("Uhr geht nach: %d %q", a.code, a.text())
	}
	a := neu(testPasswort, code(geheim, u, 0))
	liste, _ := a.daten["ersatzcodes"].([]any)
	if a.code != 200 || len(liste) != 10 {
		t.Fatalf("erneuern: %d %s", a.code, a.roh)
	}
	frisch := liste[0].(string)
	if !ersatzMuster.MatchString(frisch) {
		t.Fatalf("neuer Ersatzcode %q", frisch)
	}
	if st := statusVon(t, c); st["ersatzcodesUebrig"] != float64(10) {
		t.Fatalf("Status nach Erneuern: %v", st)
	}
	tfAnfrage(t, c, "POST", "/api/abmelden", nil, nil)
	if a := anmeldenMit(t, c, testPasswort, alt[0]); a.code != 401 {
		t.Fatalf("alter Ersatzcode nach Erneuern: %d", a.code)
	}
	if a := anmeldenMit(t, c, testPasswort, frisch); a.code != 200 || a.daten["ersatzcodesUebrig"] != float64(9) {
		t.Fatalf("neuer Ersatzcode: %d %s", a.code, a.roh)
	}

	// Ausschalten mit einem Ersatzcode (Telefon weg, Passwort bekannt).
	aus := func(pw, cd string) antwort {
		return tfAnfrage(t, c, "POST", "/api/zweitfaktor/ausschalten", map[string]string{"passwort": pw, "code": cd}, nil)
	}
	if a := aus("falsch-falsch", liste[1].(string)); a.code != 400 || a.daten["feld"] != "passwort" || !s.tr.ZweitfaktorAktiv() {
		t.Fatalf("ausschalten falsches Passwort: %d %s", a.code, a.roh)
	}
	if a := aus(testPasswort, liste[1].(string)); a.code != 200 || a.daten["ok"] != true {
		t.Fatalf("ausschalten: %d %s", a.code, a.roh)
	}
	if st := statusVon(t, c); st["zweitfaktor"] != false || st["ersatzcodesUebrig"] != nil {
		t.Fatalf("Status nach Ausschalten: %v", st)
	}
	if a := aus(testPasswort, code(geheim, u, 0)); a.code != 409 {
		t.Fatalf("zweimal ausschalten: %d", a.code)
	}
	if a := neu(testPasswort, code(geheim, u, 0)); a.code != 409 {
		t.Fatalf("Ersatzcodes ohne Zweitfaktor: %d", a.code)
	}
	tfAnfrage(t, c, "POST", "/api/abmelden", nil, nil)
	if a := anmeldenMit(t, c, testPasswort, ""); a.code != 200 || !a.cookie {
		t.Fatalf("Anmelden nach Ausschalten: %d %s", a.code, a.roh)
	}
	meta, _ := s.st.Meta(MetaTresor)
	if bytes.Contains(meta, []byte(`"tf"`)) {
		t.Fatal("Zweitfaktor steht noch auf der Platte")
	}
}

// Sicherheitsschluessel = Generalschluessel: Zuruecksetzen entfernt die Zwei-Faktor-Anmeldung.
func TestZuruecksetzenEntferntZweitfaktor(t *testing.T) {
	_, s, c, sk := angemeldeteApp(t, Optionen{Version: "test"})
	u := uhrStellen(s)
	zweitfaktorEin(t, c, u)
	z := map[string]string{"schluessel": sk, "benutzer": "admin", "passwort": "neues-passwort-1"}
	a := tfAnfrage(t, c, "POST", "/api/zuruecksetzen", z, nil)
	if a.code != 200 || a.daten["ok"] != true || a.daten["zweitfaktorAus"] != true {
		t.Fatalf("zuruecksetzen: %d %s", a.code, a.roh)
	}
	if s.tr.ZweitfaktorAktiv() || s.tr.Offen() {
		t.Fatal("nach Zuruecksetzen")
	}
	if a := anmeldenMit(t, c, "neues-passwort-1", ""); a.code != 200 {
		t.Fatalf("Anmelden ohne Code: %d %s", a.code, a.roh)
	}
	z["passwort"] = "drittes-passwort"
	if a := tfAnfrage(t, c, "POST", "/api/zuruecksetzen", z, nil); a.code != 200 || a.daten["zweitfaktorAus"] != false {
		t.Fatalf("zuruecksetzen ohne Zweitfaktor: %d %s", a.code, a.roh)
	}
}

// Der Zweitfaktor reist im Tresor-Kopf mit dem Backup und gilt nach dem Einspielen.
func TestZweitfaktorReistImBackup(t *testing.T) {
	_, quelle, c, sk := angemeldeteApp(t, Optionen{Version: "test"})
	u := uhrStellen(quelle)
	geheim, ersatz := zweitfaktorEin(t, c, u)
	backup := backupLaden(t, c)

	ts2, ziel := testApp(t)
	ziel.uhr = u.jetzt
	c2 := neuerClient(t, ts2.URL, ts2.Client())
	if code, text := einspielenAn(c2, "/api/einspielen", backup, sk); code != 200 {
		t.Fatalf("einspielen: %d %s", code, text)
	}
	if a := anmeldenMit(t, c2, testPasswort, ""); a.code != 401 || a.daten["zweitfaktor"] != true || a.cookie {
		t.Fatalf("nach Einspielen ohne Code: %d %s", a.code, a.roh)
	}
	if a := anmeldenMit(t, c2, testPasswort, code(geheim, u, 0)); a.code != 200 || !a.cookie {
		t.Fatalf("nach Einspielen mit Code: %d %s", a.code, a.roh)
	}
	if st := statusVon(t, c2); st["zweitfaktor"] != true || st["ersatzcodesUebrig"] != float64(len(ersatz)) {
		t.Fatalf("Status nach Einspielen: %v", st)
	}
}

// Eine begonnene Einrichtung gilt einrichtenFrist lang und nur fuer die eigene Anmeldung.
func TestZweitfaktorEinrichtungLaeuftAb(t *testing.T) {
	ts, s, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	u := uhrStellen(s)
	b := tfAnfrage(t, c, "POST", "/api/zweitfaktor/beginnen", map[string]any{}, nil)
	geheim, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ReplaceAll(b.daten["schluessel"].(string), " ", ""))

	// Eine zweite Anmeldung (anderer Browser) sieht die Einrichtung nicht.
	zweiter := neuerClient(t, ts.URL, ts.Client())
	if a := anmeldenMit(t, zweiter, testPasswort, ""); a.code != 200 {
		t.Fatalf("zweite Anmeldung: %d", a.code)
	}
	if a := tfAnfrage(t, zweiter, "POST", "/api/zweitfaktor/einschalten", map[string]string{"passwort": testPasswort, "code": code(geheim, u, 0)}, nil); a.code != 409 {
		t.Fatalf("fremde Einrichtung: %d %s", a.code, a.roh)
	}
	if b2 := tfAnfrage(t, zweiter, "POST", "/api/zweitfaktor/beginnen", map[string]any{}, nil); b2.code != 200 || b2.daten["schluessel"] == b.daten["schluessel"] {
		t.Fatalf("zweite Anmeldung bekommt das Geheimnis der ersten: %d %v", b2.code, b2.daten["schluessel"])
	}

	u.weiter(einrichtenFrist + time.Second)
	if a := tfAnfrage(t, c, "POST", "/api/zweitfaktor/einschalten", map[string]string{"passwort": testPasswort, "code": code(geheim, u, 0)}, nil); a.code != 409 ||
		a.text() != "Die Einrichtung ist abgelaufen — bitte neu beginnen. Einen schon angelegten Eintrag «Keyway» in der Authenticator-App bitte löschen, er gilt nicht mehr." {
		t.Fatalf("abgelaufen: %d %q", a.code, a.text())
	}
	s.tfMu.Lock()
	_, da := s.tfAusstehend[c.token(t)]
	s.tfMu.Unlock()
	if da {
		t.Fatal("abgelaufene Einrichtung liegt noch im Speicher")
	}
	// Nach Ablauf: ein neues Geheimnis. Abmelden vergisst die Einrichtung.
	if a := tfAnfrage(t, c, "POST", "/api/zweitfaktor/beginnen", map[string]any{}, nil); a.code != 200 || a.daten["schluessel"] == b.daten["schluessel"] {
		t.Fatalf("nach Ablauf dasselbe Geheimnis: %d %v", a.code, a.daten["schluessel"])
	}
	token := c.token(t)
	tfAnfrage(t, c, "POST", "/api/abmelden", nil, nil)
	s.tfMu.Lock()
	_, da = s.tfAusstehend[token]
	s.tfMu.Unlock()
	if da {
		t.Fatal("Einrichtung nach Abmelden noch im Speicher")
	}
}

// Ein zweites «Einrichten» derselben Anmeldung (Dialog geschlossen, Seite neu geladen,
// Doppelklick) liefert dasselbe Geheimnis und verlaengert die Frist: Ein schon gescannter
// Eintrag in der Authenticator-App bleibt gueltig.
func TestZweitfaktorBeginnenZweimalGleich(t *testing.T) {
	_, s, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	u := uhrStellen(s)
	b1 := tfAnfrage(t, c, "POST", "/api/zweitfaktor/beginnen", map[string]any{}, nil)
	u.weiter(einrichtenFrist - time.Minute)
	b2 := tfAnfrage(t, c, "POST", "/api/zweitfaktor/beginnen", map[string]any{}, nil)
	if b1.code != 200 || b2.code != 200 || b1.daten["schluessel"] != b2.daten["schluessel"] || b1.daten["uri"] != b2.daten["uri"] || b1.daten["qr"] != b2.daten["qr"] {
		t.Fatalf("zweites Beginnen: %d %d %v / %v", b1.code, b2.code, b1.daten["schluessel"], b2.daten["schluessel"])
	}
	// Die Frist laeuft ab dem zweiten Beginnen: nach weiteren 2 Minuten (insgesamt ueber der
	// Frist) gilt der Code des zuerst gescannten Schluessels.
	u.weiter(2 * time.Minute)
	geheim, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ReplaceAll(b1.daten["schluessel"].(string), " ", ""))
	if a := tfAnfrage(t, c, "POST", "/api/zweitfaktor/einschalten", map[string]string{"passwort": testPasswort, "code": code(geheim, u, 0)}, nil); a.code != 200 {
		t.Fatalf("einschalten mit dem zuerst gescannten Schluessel: %d %s", a.code, a.roh)
	}
	// Nach dem Einschalten ist die Einrichtung vorbei; nach dem Ausschalten kommt ein neues Geheimnis.
	u.weiter(totp.Periode * time.Second)
	if a := tfAnfrage(t, c, "POST", "/api/zweitfaktor/ausschalten", map[string]string{"passwort": testPasswort, "code": code(geheim, u, 0)}, nil); a.code != 200 {
		t.Fatalf("ausschalten: %d %s", a.code, a.roh)
	}
	if b3 := tfAnfrage(t, c, "POST", "/api/zweitfaktor/beginnen", map[string]any{}, nil); b3.code != 200 || b3.daten["schluessel"] == b1.daten["schluessel"] {
		t.Fatalf("nach dem Ausschalten dasselbe Geheimnis: %d %v", b3.code, b3.daten["schluessel"])
	}
}

// Der Hinweis auf eine falsch gehende Uhr bestaetigt bei einem Code fuer eine spaetere Uhrzeit,
// dass er stimmt. Wer das Passwort kennt und raet, darf ihn danach nicht aufsparen und spaeter
// einloesen: Der Zeitschritt ist verbrannt.
func TestZweitfaktorHinweisVerbrenntKuenftigenCode(t *testing.T) {
	ts, s, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	u := uhrStellen(s)
	geheim, _ := zweitfaktorEin(t, c, u)
	tfAnfrage(t, c, "POST", "/api/abmelden", nil, nil)
	angreifer := neuerClient(t, ts.URL, ts.Client())

	spaeter := code(geheim, u, 30)
	if a := anmeldenMit(t, angreifer, testPasswort, spaeter); a.code != 401 || !strings.Contains(a.text(), "etwa 15 min später") || a.cookie {
		t.Fatalf("Hinweis: %d %q", a.code, a.text())
	}
	// Ein Code fuer eine fruehere Uhrzeit laesst sich nicht aufsparen: Hinweis, nichts verbrannt.
	if a := anmeldenMit(t, angreifer, testPasswort, code(geheim, u, -30)); a.code != 401 || !strings.Contains(a.text(), "etwa 15 min früher") {
		t.Fatalf("Hinweis frueher: %d %q", a.code, a.text())
	}
	u.weiter(15 * time.Minute)
	if code(geheim, u, 0) != spaeter {
		t.Fatal("Testaufbau: der Code gehoert nicht zum jetzigen Zeitschritt")
	}
	if a := anmeldenMit(t, angreifer, testPasswort, spaeter); a.code != 401 || a.cookie || s.tr.Offen() ||
		a.text() != "Dieser Code wurde schon verwendet — bitte auf den nächsten Code warten" {
		t.Fatalf("aufgesparter Code: %d %s cookie=%v offen=%v", a.code, a.roh, a.cookie, s.tr.Offen())
	}
	// Der Besitzer meldet sich mit dem naechsten Code an.
	if a := anmeldenMit(t, c, testPasswort, code(geheim, u, 1)); a.code != 200 || !a.cookie {
		t.Fatalf("naechster Code des Besitzers: %d %s", a.code, a.roh)
	}
	// Auch in den Einstellungen (Ausschalten) verbrennt der Hinweis den Schritt.
	u.weiter(totp.Periode * time.Second)
	nochmal := code(geheim, u, 30)
	if a := tfAnfrage(t, c, "POST", "/api/zweitfaktor/ausschalten", map[string]string{"passwort": testPasswort, "code": nochmal}, nil); a.code != 400 || !strings.Contains(a.text(), "etwa 15 min später") {
		t.Fatalf("Hinweis beim Ausschalten: %d %s", a.code, a.roh)
	}
	u.weiter(15 * time.Minute)
	if a := tfAnfrage(t, c, "POST", "/api/zweitfaktor/ausschalten", map[string]string{"passwort": testPasswort, "code": nochmal}, nil); a.code != 400 || !s.tr.ZweitfaktorAktiv() {
		t.Fatalf("aufgesparter Code beim Ausschalten: %d %s", a.code, a.roh)
	}
	// Abgelaufene verbrannte Schritte raeumt die Leerlaufwache weg.
	u.weiter(time.Hour)
	s.ausstehendAufraeumen()
	s.tfMu.Lock()
	n := len(s.tfVerbrannt)
	s.tfMu.Unlock()
	if n != 0 {
		t.Fatalf("%d verbrannte Schritte nach einer Stunde", n)
	}
}

// Kontoweite Bremse: Falsche Codes von vielen Adressen (je eine unter der Sperre je Adresse)
// sperren die Codes aus der App fuer alle, auch den richtigen. Ersatzcodes gelten weiter, und
// der Besitzer erfaehrt es bei der naechsten Anmeldung.
func TestZweitfaktorKontoweiteSperre(t *testing.T) {
	ts, s, c, _ := angemeldeteApp(t, Optionen{Version: "test", Proxys: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}})
	u := uhrStellen(s)
	geheim, ersatz := zweitfaktorEin(t, c, u)
	tfAnfrage(t, c, "POST", "/api/abmelden", nil, nil)
	nr := 0
	anmeldenVon := func(cd string, koepfe map[string]string) antwort {
		nr++
		k := map[string]string{"X-Forwarded-For": fmt.Sprintf("198.51.100.%d", nr)}
		maps.Copy(k, koepfe)
		return tfAnfrage(t, neuerClient(t, ts.URL, ts.Client()), "POST", "/api/anmelden", map[string]string{"benutzer": "admin", "passwort": testPasswort, "code": cd}, k)
	}
	falsch := falscherCode(geheim, u)
	for i := 0; i < maxCodeFehler; i++ {
		if a := anmeldenVon(falsch, nil); a.code != 401 || a.daten["zweitfaktor"] != true {
			t.Fatalf("falscher Code %d: %d %s", i, a.code, a.roh)
		}
	}
	// Jetzt gilt auch der richtige Code nicht, von keiner Adresse.
	a := anmeldenVon(code(geheim, u, 0), nil)
	if a.code != 429 || a.cookie || s.tr.Offen() || a.text() != "Zu viele falsche Codes — Codes aus der App gelten noch 15 min nicht. Ein Ersatzcode funktioniert weiterhin." {
		t.Fatalf("richtiger Code waehrend der Sperre: %d %s", a.code, a.roh)
	}
	if en := anmeldenVon(code(geheim, u, 0), englisch); en.code != 429 || !strings.HasPrefix(en.text(), "Too many wrong codes — codes from the app won’t be accepted for another ") {
		t.Fatalf("englisch: %d %q", en.code, en.text())
	}
	// Ein Ersatzcode geht, und die Antwort meldet die Sperre samt Zahl der falschen Codes (einmal).
	a = anmeldenVon(ersatz[0], nil)
	if a.code != 200 || !a.cookie || a.daten["codeAlarm"] != true || a.daten["codeFehler"] != float64(maxCodeFehler) {
		t.Fatalf("Ersatzcode waehrend der Sperre: %d %s", a.code, a.roh)
	}
	if a := anmeldenVon(ersatz[1], nil); a.code != 200 || a.daten["codeAlarm"] != nil || a.daten["codeFehler"] != nil {
		t.Fatalf("zweite Anmeldung meldet die Sperre noch einmal: %d %s", a.code, a.roh)
	}
	// Auch die Einstellungen nehmen waehrend der Sperre keinen Code aus der App (429, die
	// Anmeldung bleibt), wohl aber einen Ersatzcode.
	if a := anmeldenMit(t, c, testPasswort, ersatz[2]); a.code != 200 {
		t.Fatalf("Anmelden mit Ersatzcode: %d %s", a.code, a.roh)
	}
	if a := tfAnfrage(t, c, "POST", "/api/zweitfaktor/ersatzcodes", map[string]string{"passwort": testPasswort, "code": code(geheim, u, 0)}, nil); a.code != 429 || !strings.HasPrefix(a.text(), "Zu viele falsche Codes") {
		t.Fatalf("Einstellungen waehrend der Sperre: %d %s", a.code, a.roh)
	}
	if a := tfAnfrage(t, c, "POST", "/api/zweitfaktor/ersatzcodes", map[string]string{"passwort": testPasswort, "code": ersatz[3]}, nil); a.code != 200 {
		t.Fatalf("Einstellungen mit Ersatzcode waehrend der Sperre: %d %s", a.code, a.roh)
	}
	// Nach Ablauf gilt der Code aus der App wieder.
	u.weiter(sperreDauer)
	if a := anmeldenVon(code(geheim, u, 0), nil); a.code != 200 || !a.cookie {
		t.Fatalf("nach der Sperre: %d %s", a.code, a.roh)
	}
}

// logMitschnitt faengt das Log des Servers (Paket log) fuer einen Test ab.
type logMitschnitt struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *logMitschnitt) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logMitschnitt) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func logMitschneiden(t *testing.T) *logMitschnitt {
	l := &logMitschnitt{}
	alt := log.Writer()
	log.SetOutput(l)
	t.Cleanup(func() { log.SetOutput(alt) })
	return l
}

// angreiferVon meldet sich jedes Mal von einer neuen Adresse an (X-Forwarded-For hinter dem
// Proxy 127.0.0.1): Die Sperre je Adresse greift so nie, nur die kontoweite Bremse.
func angreiferVon(t *testing.T, ts *httptest.Server) func(cd string) antwort {
	nr := 0
	return func(cd string) antwort {
		nr++
		k := map[string]string{"X-Forwarded-For": fmt.Sprintf("203.0.%d.%d", nr/250, nr%250+1)}
		c := neuerClient(t, ts.URL, ts.Client())
		return tfAnfrage(t, c, "POST", "/api/anmelden", map[string]string{"benutzer": "admin", "passwort": testPasswort, "code": cd}, k)
	}
}

var mitProxy = Optionen{Version: "test", Proxys: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}}

// Kontoweite Bremse gegen langsames Raten (N1): 9 falsche Codes je Viertelstunde, also nie 10
// in einem 15-Minuten-Fenster, von immer neuen Adressen. Gezaehlt wird die Folge seit dem
// letzten richtigen zweiten Faktor: ab 5 eine WARNUNG im Log, bei 10 Sperre und Alarm, jede
// weitere Sperre doppelt so lang. Ein richtiger zweiter Faktor beginnt die Folge neu.
func TestZweitfaktorLangsamerAngreifer(t *testing.T) {
	ts, s, c, _ := angemeldeteApp(t, mitProxy)
	u := uhrStellen(s)
	geheim, ersatz := zweitfaktorEin(t, c, u)
	tfAnfrage(t, c, "POST", "/api/abmelden", nil, nil)
	logbuch := logMitschneiden(t)
	von := angreiferVon(t, ts)
	takt := sperreDauer / (maxCodeFehler - 1)
	falsche := func(n int) {
		t.Helper()
		for i := 0; i < n; i++ {
			if i > 0 {
				u.weiter(takt)
			}
			if a := von(falscherCode(geheim, u)); a.code != 401 || a.text() != "Der Code stimmt nicht" {
				t.Fatalf("falscher Code %d: %d %s", i, a.code, a.roh)
			}
		}
	}
	gesperrt := func(rest string) {
		t.Helper()
		if a := von(code(geheim, u, 0)); a.code != 429 || a.cookie ||
			a.text() != "Zu viele falsche Codes — Codes aus der App gelten noch "+rest+" nicht. Ein Ersatzcode funktioniert weiterhin." {
			t.Fatalf("richtiger Code, Sperre %s erwartet: %d %s", rest, a.code, a.roh)
		}
	}
	frei := func(meldung any) {
		t.Helper()
		a := von(code(geheim, u, 0))
		if a.code != 200 || !a.cookie || a.daten["codeFehler"] != meldung || a.daten["codeAlarm"] != nil {
			t.Fatalf("richtiger Code, frei erwartet (codeFehler %v): %d %s", meldung, a.code, a.roh)
		}
	}

	// 9 in einer Viertelstunde: noch keine Sperre, aber ab dem fuenften die Warnung im Log.
	falsche(maxCodeFehler - 1)
	if l := logbuch.String(); !strings.Contains(l, "WARNUNG: 5 falsche Codes der Zwei-Faktor-Anmeldung in Folge") || strings.Contains(l, "gesperrt") {
		t.Fatalf("Log nach 9 falschen Codes:\n%s", l)
	}
	// Der zehnte eine Viertelstunde nach dem ersten (das alte Fenster hielt nur 9): Sperre.
	u.weiter(takt)
	falsche(1)
	gesperrt("15 min")
	if l := logbuch.String(); !strings.Contains(l, "WARNUNG: 10 falsche Codes der Zwei-Faktor-Anmeldung in Folge") || !strings.Contains(l, "Codes aus der App sind 15m0s gesperrt") {
		t.Fatalf("Log nach 10 falschen Codes:\n%s", l)
	}
	// Nach der Sperre im selben Takt weiter: Die naechste Sperre dauert doppelt so lang.
	u.weiter(sperreDauer)
	falsche(maxCodeFehler)
	gesperrt("30 min")
	// Der Besitzer (Ersatzcode) erfaehrt Sperre und Zahl, einmal.
	if a := von(ersatz[0]); a.code != 200 || a.daten["codeAlarm"] != true || a.daten["codeFehler"] != float64(2*maxCodeFehler) {
		t.Fatalf("Besitzer nach 20 falschen Codes: %d %s", a.code, a.roh)
	}
	if a := von(ersatz[1]); a.code != 200 || a.daten["codeAlarm"] != nil || a.daten["codeFehler"] != nil {
		t.Fatalf("zweite Anmeldung des Besitzers: %d %s", a.code, a.roh)
	}
	// Ein richtiger zweiter Faktor beginnt die Folge neu: 9 falsche, richtig, 9 falsche sperren nicht.
	u.weiter(30 * time.Minute)
	falsche(maxCodeFehler - 1)
	u.weiter(takt)
	frei(float64(maxCodeFehler - 1))
	u.weiter(takt)
	falsche(maxCodeFehler - 1)
	u.weiter(takt)
	frei(float64(maxCodeFehler - 1))
	// ... und die Sperre beginnt nach einem richtigen wieder bei 15 Minuten.
	u.weiter(takt)
	falsche(maxCodeFehler)
	gesperrt("15 min")
}

func TestCodeSperreDauer(t *testing.T) {
	for n, want := range []time.Duration{15 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour, 4 * time.Hour,
		8 * time.Hour, 16 * time.Hour, 24 * time.Hour, 24 * time.Hour} {
		if got := codeSperreDauer(n); got != want {
			t.Errorf("codeSperreDauer(%d) = %v, erwartet %v", n, got, want)
		}
	}
	if got := codeSperreDauer(1000); got != maxCodeSperre {
		t.Errorf("codeSperreDauer(1000) = %v", got)
	}
	for d, want := range map[time.Duration]string{
		40 * time.Second: "40 s", 15 * time.Minute: "15 min", 2 * time.Hour: "120 min",
		2*time.Hour + time.Second: "2 h 1 min", 23*time.Hour + 59*time.Minute + 30*time.Second: "24 h", 24 * time.Hour: "24 h",
	} {
		if got := dauerText(d); got != want {
			t.Errorf("dauerText(%v) = %q, erwartet %q", d, got, want)
		}
	}
}

// Telefon 40 s vor (N2): Direkt nach dem Erscheinen gehoert sein Code zum Schritt gleich nach
// dem Fenster. Kein Uhrzeit-Hinweis und nichts verbrannt: «stimmt nicht», und derselbe Code gilt
// 10 s spaeter. (Weiter weg bleibt es beim Hinweis, siehe TestZweitfaktorHinweisVerbrenntKuenftigenCode.)
func TestZweitfaktorTelefonKnappVor(t *testing.T) {
	_, s, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	u := uhrStellen(s)
	geheim, _ := zweitfaktorEin(t, c, u)
	tfAnfrage(t, c, "POST", "/api/abmelden", nil, nil)
	u.weiter(20 * time.Second)
	telefon := func() time.Time { return u.jetzt().Add(40 * time.Second) }
	if totp.Schritt(telefon()) != totp.Schritt(u.jetzt())+totp.Fenster+1 || telefon().Unix()%totp.Periode != 0 {
		t.Fatal("Testaufbau: das Telefon zeigt nicht gerade den Code des Schritts nach dem Fenster")
	}
	cd := totp.Code(geheim, telefon())
	if a := anmeldenMit(t, c, testPasswort, cd); a.code != 401 || a.text() != "Der Code stimmt nicht" || a.cookie {
		t.Fatalf("sofort abgeschickt: %d %s", a.code, a.roh)
	}
	s.tfMu.Lock()
	nVerbrannt := len(s.tfVerbrannt)
	s.tfMu.Unlock()
	if nVerbrannt != 0 {
		t.Fatalf("%d Schritte verbrannt", nVerbrannt)
	}
	u.weiter(10 * time.Second)
	if totp.Code(geheim, telefon()) != cd {
		t.Fatal("Testaufbau: das Telefon zeigt schon einen anderen Code")
	}
	if a := anmeldenMit(t, c, testPasswort, cd); a.code != 200 || !a.cookie || a.daten["codeFehler"] != nil {
		t.Fatalf("derselbe Code 10 s spaeter: %d %s", a.code, a.roh)
	}
}

// Uhrzeit-Hinweise und «schon verwendet» zeigen, dass der Code zum Geheimnis passt (N3): Sie
// zaehlen nur fuer die Sperre je Adresse, nicht fuer die kontoweite Bremse. Serveruhr 5 min
// nach, der Besitzer versucht es von zwei Adressen bis zu deren Sperre.
func TestZweitfaktorUhrHinweiseZaehlenNicht(t *testing.T) {
	ts, s, c, _ := angemeldeteApp(t, mitProxy)
	u := uhrStellen(s)
	geheim, _ := zweitfaktorEin(t, c, u)
	tfAnfrage(t, c, "POST", "/api/abmelden", nil, nil)
	logbuch := logMitschneiden(t)
	anmeldenVon := func(adresse, cd string) antwort {
		return tfAnfrage(t, neuerClient(t, ts.URL, ts.Client()), "POST", "/api/anmelden",
			map[string]string{"benutzer": "admin", "passwort": testPasswort, "code": cd}, map[string]string{"X-Forwarded-For": adresse})
	}
	// Der schon beim Einschalten verwendete Code: «verwendet», dreimal.
	for i := 0; i < 3; i++ {
		if a := anmeldenVon("192.168.1.19", code(geheim, u, -1)); a.code != 401 || !strings.Contains(a.text(), "schon verwendet") {
			t.Fatalf("verwendet %d: %d %s", i, a.code, a.roh)
		}
	}
	for _, adresse := range []string{"192.168.1.20", "192.168.1.21"} {
		for i := 0; i < maxFehlversuche; i++ {
			if a := anmeldenVon(adresse, code(geheim, u, 10)); a.code != 401 || !strings.Contains(a.text(), "etwa 5 min später") {
				t.Fatalf("%s Versuch %d: %d %s", adresse, i, a.code, a.roh)
			}
			u.weiter(35 * time.Second)
		}
		// Die Sperre je Adresse bleibt.
		if a := anmeldenVon(adresse, code(geheim, u, 10)); a.code != 429 {
			t.Fatalf("%s: Sperre je Adresse fehlt: %d %s", adresse, a.code, a.roh)
		}
	}
	s.tfMu.Lock()
	folge, melden, alarm, bis := s.tfFehlerFolge, s.tfFehlerMelden, s.tfAlarm, s.tfSperreBis
	s.tfMu.Unlock()
	if folge != 0 || melden != 0 || alarm || !bis.IsZero() || strings.Contains(logbuch.String(), "WARNUNG") {
		t.Fatalf("kontoweit gezaehlt: Folge %d, Meldung %d, Alarm %v, Sperre bis %v\n%s", folge, melden, alarm, bis, logbuch.String())
	}
	// Uhr gestellt: Der aktuelle Code gilt, ohne Alarm.
	u.weiter(5 * time.Minute)
	if a := anmeldenVon("192.168.1.22", code(geheim, u, 0)); a.code != 200 || a.daten["codeAlarm"] != nil || a.daten["codeFehler"] != nil {
		t.Fatalf("nach dem Stellen der Uhr: %d %s", a.code, a.roh)
	}
}

// Sperre und Alarm gelten dem Passwort und dem Geheimnis, die es bei der Sperre gab (N4): Ein
// neues Passwort (Zuruecksetzen, Passwort aendern) hebt beides auf, ein neues Geheimnis die
// Sperre. Der Alarm erreicht den Besitzer auch bei einer Anmeldung ohne Zwei-Faktor-Anmeldung.
func TestZweitfaktorSperreNachNeuemPasswortUndGeheimnis(t *testing.T) {
	sperren := func(t *testing.T, ts *httptest.Server, geheim []byte, u *testUhr) {
		t.Helper()
		von := angreiferVon(t, ts)
		for i := 0; i < maxCodeFehler; i++ {
			if a := von(falscherCode(geheim, u)); a.code != 401 {
				t.Fatalf("falscher Code %d: %d %s", i, a.code, a.roh)
			}
		}
		if a := von(code(geheim, u, 0)); a.code != 429 {
			t.Fatalf("keine Sperre: %d %s", a.code, a.roh)
		}
	}
	neuEinrichten := func(t *testing.T, c *client, u *testUhr, passwort string) []byte {
		t.Helper()
		b := tfAnfrage(t, c, "POST", "/api/zweitfaktor/beginnen", map[string]any{}, nil)
		geheim, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ReplaceAll(fmt.Sprint(b.daten["schluessel"]), " ", ""))
		if b.code != 200 || err != nil {
			t.Fatalf("beginnen: %d %s", b.code, b.roh)
		}
		if a := tfAnfrage(t, c, "POST", "/api/zweitfaktor/einschalten", map[string]string{"passwort": passwort, "code": code(geheim, u, 0)}, nil); a.code != 200 {
			t.Fatalf("einschalten: %d %s", a.code, a.roh)
		}
		u.weiter(totp.Periode * time.Second)
		return geheim
	}

	t.Run("Zuruecksetzen", func(t *testing.T) {
		ts, s, c, sk := angemeldeteApp(t, mitProxy)
		u := uhrStellen(s)
		geheim, _ := zweitfaktorEin(t, c, u)
		tfAnfrage(t, c, "POST", "/api/abmelden", nil, nil)
		sperren(t, ts, geheim, u)
		z := map[string]string{"schluessel": sk, "benutzer": "admin", "passwort": "neues-passwort-1"}
		if a := tfAnfrage(t, c, "POST", "/api/zuruecksetzen", z, nil); a.code != 200 || a.daten["zweitfaktorAus"] != true {
			t.Fatalf("zuruecksetzen: %d %s", a.code, a.roh)
		}
		// Neues Passwort: kein Alarm mehr, er raete zu dem, was eben geschah.
		if a := anmeldenMit(t, c, "neues-passwort-1", ""); a.code != 200 || a.daten["codeAlarm"] != nil || a.daten["codeFehler"] != nil {
			t.Fatalf("Anmelden nach Zuruecksetzen: %d %s", a.code, a.roh)
		}
		neu := neuEinrichten(t, c, u, "neues-passwort-1")
		tfAnfrage(t, c, "POST", "/api/abmelden", nil, nil)
		if a := anmeldenMit(t, c, "neues-passwort-1", code(neu, u, 0)); a.code != 200 || a.daten["codeAlarm"] != nil {
			t.Fatalf("neuer Schluessel nach Zuruecksetzen: %d %s", a.code, a.roh)
		}
	})

	t.Run("Ausschalten und neu einrichten", func(t *testing.T) {
		ts, s, c, _ := angemeldeteApp(t, mitProxy)
		u := uhrStellen(s)
		geheim, ersatz := zweitfaktorEin(t, c, u) // c bleibt angemeldet
		sperren(t, ts, geheim, u)
		if a := tfAnfrage(t, c, "POST", "/api/zweitfaktor/ausschalten", map[string]string{"passwort": testPasswort, "code": ersatz[0]}, nil); a.code != 200 {
			t.Fatalf("ausschalten: %d %s", a.code, a.roh)
		}
		tfAnfrage(t, c, "POST", "/api/abmelden", nil, nil)
		// Ohne Zwei-Faktor-Anmeldung: Der Alarm kommt trotzdem, einmal.
		if a := anmeldenMit(t, c, testPasswort, ""); a.code != 200 || a.daten["codeAlarm"] != true || a.daten["codeFehler"] != float64(maxCodeFehler) {
			t.Fatalf("Anmelden ohne Zweitfaktor nach der Sperre: %d %s", a.code, a.roh)
		}
		// Neues Geheimnis: Die Sperre galt dem alten, der neue Code gilt sofort.
		neu := neuEinrichten(t, c, u, testPasswort)
		tfAnfrage(t, c, "POST", "/api/abmelden", nil, nil)
		if a := anmeldenMit(t, c, testPasswort, code(neu, u, 0)); a.code != 200 || a.daten["codeAlarm"] != nil || a.daten["codeFehler"] != nil {
			t.Fatalf("neuer Schluessel waehrend der alten Sperre: %d %s", a.code, a.roh)
		}
	})

	t.Run("Passwort aendern", func(t *testing.T) {
		ts, s, c, _ := angemeldeteApp(t, mitProxy)
		u := uhrStellen(s)
		geheim, ersatz := zweitfaktorEin(t, c, u)
		tfAnfrage(t, c, "POST", "/api/abmelden", nil, nil)
		sperren(t, ts, geheim, u)
		if a := anmeldenMit(t, c, testPasswort, ersatz[0]); a.code != 200 || a.daten["codeAlarm"] != true {
			t.Fatalf("Besitzer mit Ersatzcode: %d %s", a.code, a.roh)
		}
		if code := c.rufe("POST", "/api/passwort", map[string]string{"alt": testPasswort, "neu": "neues-passwort-1"}, nil); code != 200 {
			t.Fatalf("Passwort aendern: %d", code)
		}
		tfAnfrage(t, c, "POST", "/api/abmelden", nil, nil)
		if a := anmeldenMit(t, c, "neues-passwort-1", code(geheim, u, 0)); a.code != 200 {
			t.Fatalf("Code aus der App nach neuem Passwort: %d %s", a.code, a.roh)
		}
	})
}

// Der Passwortwechsel behaelt die Zwei-Faktor-Anmeldung.
func TestPasswortwechselBehaeltZweitfaktor(t *testing.T) {
	_, s, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	u := uhrStellen(s)
	geheim, _ := zweitfaktorEin(t, c, u)
	if code := c.rufe("POST", "/api/passwort", map[string]string{"alt": testPasswort, "neu": "neues-passwort-1"}, nil); code != 200 {
		t.Fatalf("Passwort aendern: %d", code)
	}
	tfAnfrage(t, c, "POST", "/api/abmelden", nil, nil)
	if a := anmeldenMit(t, c, "neues-passwort-1", ""); a.code != 401 || a.daten["zweitfaktor"] != true {
		t.Fatalf("nach Passwortwechsel ohne Code: %d %s", a.code, a.roh)
	}
	if a := anmeldenMit(t, c, "neues-passwort-1", code(geheim, u, 0)); a.code != 200 {
		t.Fatalf("nach Passwortwechsel mit Code: %d %s", a.code, a.roh)
	}
}

func TestKontoHost(t *testing.T) {
	for in, want := range map[string]string{
		"keyway.local:8445":      "keyway.local",
		"Keyway.Example.":        "keyway.example",
		"192.168.1.5:8445":       "192.168.1.5",
		"[2001:db8::1]:8445":     "[2001:db8::1]",
		"[::1]":                  "[::1]",
		"böse<host>&x":           "bsehostx",
		"a:b:c":                  "abc",
		"":                       "",
		strings.Repeat("a", 100): strings.Repeat("a", 64),
	} {
		if got := kontoHost(in); got != want {
			t.Errorf("kontoHost(%q) = %q, erwartet %q", in, got, want)
		}
	}
}
