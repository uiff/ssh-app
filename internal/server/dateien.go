package server

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/uiff/ssh-app/internal/term"
	"github.com/uiff/ssh-app/internal/texte"
)

type eintrag struct {
	Name   string `json:"name"`
	Ordner bool   `json:"ordner"`
	Link   bool   `json:"link,omitempty"`
	// Art: datei | ordner | geraet | pipe | socket | andere (Verknuepfung ohne Ziel).
	// Bei einer Verknuepfung die Art des Ziels. Nur «datei» laesst sich herunterladen.
	Art       string    `json:"art"`
	Groesse   int64     `json:"groesse"`
	Rechte    string    `json:"rechte"`
	Geaendert time.Time `json:"geaendert"`
}

// posixRename ersetzt ein vorhandenes Ziel in einem Schritt (OpenSSH-Erweiterung).
const posixRename = "posix-rename@openssh.com"

// dateiFehler ist ein Fehler mit HTTP-Code und uebersetzbarem Satz fuer die Oberflaeche.
// gesperrt: Beim Hochladen ist der Name durch einen Ordner oder eine Spezialdatei belegt
// (wird wie ein Konflikt gesammelt, der Auftrag laeuft weiter).
type dateiFehler struct {
	code     int
	m        *texte.Meldung
	gesperrt bool
}

func (f *dateiFehler) Error() string { return f.m.Error() }
func (f *dateiFehler) Unwrap() error { return f.m }

func meldung(code int, schluessel string, werte ...any) *dateiFehler {
	return &dateiFehler{code: code, m: texte.Neu(schluessel, werte...)}
}

// mit haengt den technischen Grund an (nur fuers Log und errors.Is).
func (f *dateiFehler) mit(err error) *dateiFehler {
	k := *f
	k.m = f.m.MitUrsache(err)
	return &k
}

// errDateiVorhanden: Der Name ist am Ziel schon belegt und darf nicht ersetzt werden.
var errDateiVorhanden = errors.New("gibt es schon")

// errUebertragungAbgebrochen ersetzt io.ErrUnexpectedEOF des Browsers, damit pkg/sftp
// (das ErrUnexpectedEOF als regulaeres Ende wertet) den Abbruch nicht verschluckt.
var errUebertragungAbgebrochen = errors.New("übertragung vom Browser abgebrochen")

func (s *Server) sftpFuer(w http.ResponseWriter, r *http.Request) (*term.Sitzung, *sftp.Client, bool) {
	sz, ok := s.tm.Hole(r.PathValue("id"))
	if !ok {
		fehlerT(w, r, http.StatusNotFound, "dateien.sitzungFehlt")
		return nil, nil, false
	}
	c, err := sz.SFTP()
	if err != nil {
		var m *texte.Meldung
		switch {
		case errors.As(err, &m): // z. B. term.ErrKeinSFTP
			fehlerAus(w, r, http.StatusBadGateway, err)
		default:
			log.Printf("SFTP: %v", err)
			fehlerT(w, r, http.StatusBadGateway, "dateien.sitzungBeendet")
		}
		return nil, nil, false
	}
	return sz, c, true
}

func sauberPfad(p string) (string, error) {
	if p == "" || !strings.HasPrefix(p, "/") || strings.ContainsRune(p, 0) {
		return "", meldung(http.StatusBadRequest, "dateien.pfadAbsolut")
	}
	return path.Clean(p), nil
}

// sftpText uebersetzt einen SFTP-Fehler in einen verstaendlichen Satz in der Sprache der
// Anfrage. Der rohe Text (z. B. «sftp: "Failure" (SSH_FX_FAILURE)») geht nur ins Server-Log.
func sftpText(sprache string, err error) (int, string) {
	var f *dateiFehler
	var m *texte.Meldung
	var se *sftp.StatusError
	t := func(code int, schluessel string) (int, string) { return code, texte.T(sprache, schluessel) }
	switch {
	case errors.As(err, &f):
		return f.code, f.m.Text(sprache)
	case errors.As(err, &m):
		return http.StatusBadRequest, m.Text(sprache)
	case errors.Is(err, os.ErrNotExist):
		return t(http.StatusNotFound, "dateien.sftp.fehlt")
	case errors.Is(err, os.ErrPermission):
		return t(http.StatusForbidden, "dateien.sftp.keinRecht")
	case errors.Is(err, os.ErrExist):
		return t(http.StatusConflict, "dateien.sftp.vorhanden")
	case errors.Is(err, sftp.ErrSSHFxConnectionLost), errors.Is(err, sftp.ErrSSHFxNoConnection),
		errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		log.Printf("SFTP: %v", err)
		return t(http.StatusBadGateway, "dateien.sftp.verbindung")
	case errors.As(err, &se):
		log.Printf("SFTP: %v", err)
		switch se.FxCode() {
		case sftp.ErrSSHFxOpUnsupported:
			return t(http.StatusBadGateway, "dateien.sftp.nichtUnterstuetzt")
		case sftp.ErrSSHFxBadMessage:
			return t(http.StatusBadGateway, "dateien.sftp.nichtVerstanden")
		case sftp.ErrSSHFxConnectionLost, sftp.ErrSSHFxNoConnection:
			return t(http.StatusBadGateway, "dateien.sftp.verbindung")
		}
		return t(http.StatusBadGateway, "dateien.sftp.abgelehnt")
	}
	log.Printf("SFTP: %v", err)
	return t(http.StatusBadGateway, "dateien.sftp.fehler")
}

func sftpFehler(w http.ResponseWriter, r *http.Request, err error) {
	code, text := sftpText(texte.AusAnfrage(r), err)
	fehler(w, code, text)
}

// zitatListe setzt Namen in die Anfuehrungszeichen der Sprache: «a», «b» bzw. “a”, “b”.
func zitatListe(sprache string, namen []string) string {
	teile := make([]string, len(namen))
	for i, n := range namen {
		teile[i] = texte.T(sprache, "dateien.zitat", n)
	}
	return strings.Join(teile, ", ")
}

// dateienPfad liefert das Arbeitsverzeichnis der Shell (fuer «folgt Terminal»).
func (s *Server) dateienPfad(w http.ResponseWriter, r *http.Request) {
	sz, ok := s.tm.Hole(r.PathValue("id"))
	if !ok {
		fehlerT(w, r, http.StatusNotFound, "dateien.sitzungFehlt")
		return
	}
	p, bekannt := sz.Arbeitsverzeichnis()
	jsonAntwort(w, http.StatusOK, map[string]any{"pfad": p, "bekannt": bekannt})
}

// dateiArt ordnet eine Dateiart fuer die Liste ein (siehe eintrag.Art).
func dateiArt(m os.FileMode) string {
	switch {
	case m.IsDir():
		return "ordner"
	case m.IsRegular():
		return "datei"
	case m&os.ModeDevice != 0:
		return "geraet"
	case m&os.ModeNamedPipe != 0:
		return "pipe"
	case m&os.ModeSocket != 0:
		return "socket"
	}
	return "andere"
}

func (s *Server) dateienListe(w http.ResponseWriter, r *http.Request) {
	_, c, ok := s.sftpFuer(w, r)
	if !ok {
		return
	}
	p := r.URL.Query().Get("pfad")
	if p == "" {
		wd, err := c.Getwd()
		if err != nil {
			sftpFehler(w, r, err)
			return
		}
		p = wd
	}
	p, err := sauberPfad(p)
	if err != nil {
		sftpFehler(w, r, err)
		return
	}
	infos, err := c.ReadDir(p)
	if err != nil {
		if fi, e := c.Stat(p); e == nil && !fi.IsDir() {
			fehlerT(w, r, http.StatusBadRequest, "dateien.keinOrdner", p)
			return
		}
		sftpFehler(w, r, err)
		return
	}
	out := make([]eintrag, 0, len(infos))
	for _, fi := range infos {
		e := eintrag{Name: fi.Name(), Ordner: fi.IsDir(), Art: dateiArt(fi.Mode()), Groesse: fi.Size(), Rechte: fi.Mode().Perm().String(), Geaendert: fi.ModTime()}
		if fi.Mode()&os.ModeSymlink != 0 {
			e.Link = true
			e.Art = "andere" // Ziel fehlt oder ist nicht lesbar
			if ziel, err := c.Stat(path.Join(p, fi.Name())); err == nil {
				e.Ordner, e.Art = ziel.IsDir(), dateiArt(ziel.Mode())
				if ziel.Mode().IsRegular() {
					e.Groesse = ziel.Size()
				}
			}
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ordner != out[j].Ordner {
			return out[i].Ordner
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	jsonAntwort(w, http.StatusOK, map[string]any{"pfad": p, "eintraege": out})
}

// pseudoDatei: /proc und /sys melden per stat keine verlaessliche Groesse (0, unter lxcfs zu viel).
func pseudoDatei(p string) bool {
	for _, v := range []string{"/proc", "/sys"} {
		if p == v || strings.HasPrefix(p, v+"/") {
			return true
		}
	}
	return false
}

// echterPfad loest Verknuepfungen auf: RealPath (OpenSSH loest dabei alle Stufen auf) und
// danach die letzte Stufe von Hand (fuer SFTP-Dienste, deren RealPath nur saeubert).
// So erkennt dateiLaden auch eine Verknuepfung nach /proc als Pseudodatei.
func echterPfad(c *sftp.Client, p string) string {
	if rp, err := c.RealPath(p); err == nil && strings.HasPrefix(rp, "/") {
		p = path.Clean(rp)
	}
	if fi, err := c.Lstat(p); err == nil {
		if z, _, err := linkAufloesen(c, p, fi); err == nil {
			p = z
		}
	}
	return p
}

// dateiLaden schickt eine Datei als Download. Mit «pruefen=1» nur die Pruefung (gibt es
// die Datei, ist sie gewoehnlich und lesbar?) als JSON: Ein Download per Link zeigt eine
// Fehlerantwort nicht an, deshalb fragt der Browser vorher so nach.
func (s *Server) dateiLaden(w http.ResponseWriter, r *http.Request) {
	_, c, ok := s.sftpFuer(w, r)
	if !ok {
		return
	}
	p, err := sauberPfad(r.URL.Query().Get("pfad"))
	if err != nil {
		sftpFehler(w, r, err)
		return
	}
	// Erst pruefen, dann oeffnen: Eine Pipe (FIFO) wuerde beim Oeffnen haengen bleiben.
	fi, err := c.Stat(p)
	if err != nil {
		sftpFehler(w, r, err)
		return
	}
	if fi.IsDir() {
		fehlerT(w, r, http.StatusBadRequest, "dateien.laden.ordner")
		return
	}
	if !fi.Mode().IsRegular() {
		fehlerT(w, r, http.StatusBadRequest, "dateien.laden.keineDatei")
		return
	}
	f, err := c.Open(p)
	if err != nil {
		sftpFehler(w, r, err)
		return
	}
	defer f.Close()
	if r.URL.Query().Get("pruefen") == "1" {
		jsonAntwort(w, http.StatusOK, map[string]any{"ok": true, "groesse": fi.Size()})
		return
	}
	name := path.Base(p)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	// Laenge nur angeben, wenn sie stimmt: Sonst kaeme eine leere (Groesse 0) oder eine
	// abgebrochene Datei (zu grosse Angabe) an. Ohne Angabe streamt net/http «chunked».
	if fi.Size() > 0 && !pseudoDatei(p) && !pseudoDatei(echterPfad(c, p)) {
		w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
	}
	if _, err := io.Copy(w, f); err != nil {
		log.Printf("Download %s abgebrochen: %v", p, err)
	}
}

// dateiHochladen streamt jede Datei per SFTP in eine temporaere Datei neben dem Ziel. Ein
// Abbruch (Funkloch, Tab zu) hinterlaesst so weder ein Bruchstueck unter dem richtigen
// Namen noch ein zerstoertes Original. Vorhandene Dateien werden nur ersetzt, wenn ihr Name
// als «ersetzen=<name>» mitkommt (je Datei bestaetigt). Andere Namenskonflikte werden
// uebersprungen und gesammelt gemeldet: 409 mit «vorhanden» (ersetzbar nach Rueckfrage),
// «gesperrt» (Ordner oder Spezialdatei, nicht ersetzbar) und «hochgeladen» (was geklappt hat).
func (s *Server) dateiHochladen(w http.ResponseWriter, r *http.Request) {
	sz, c, ok := s.sftpFuer(w, r)
	if !ok {
		return
	}
	hilfe := ersetzHilfe{anmelden: func() func() { return s.einsetzenAnmelden(sz.ID()) }}
	if _, kopie := c.HasExtension(kopieDaten); kopie && sz.Client() != nil {
		hilfe.kanal = func() (io.ReadWriteCloser, error) { return sftpKanalOeffnen(sz.Client()) }
	}
	sprache := texte.AusAnfrage(r)
	q := r.URL.Query()
	ziel, err := sauberPfad(q.Get("pfad"))
	if err != nil {
		sftpFehler(w, r, err)
		return
	}
	ersetzen := map[string]bool{}
	for _, n := range q["ersetzen"] {
		ersetzen[n] = true
	}
	mr, err := r.MultipartReader()
	if err != nil {
		fehlerT(w, r, http.StatusBadRequest, "dateien.hochladen.multipart")
		return
	}
	fertig, vorhanden, gesperrt := []string{}, []string{}, []string{}
	var gesperrtText string
	abbruch := func(code int, text string) {
		jsonAntwort(w, code, map[string]any{"fehler": text, "hochgeladen": fertig})
	}
	for {
		teil, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Hochladen nach %s abgebrochen: %v", ziel, err)
			abbruch(http.StatusBadRequest, texte.T(sprache, "dateien.hochladen.abgebrochen"))
			return
		}
		name := path.Base(strings.ReplaceAll(teil.FileName(), "\\", "/"))
		if teil.FormName() != "datei" || name == "" || name == "." || name == ".." || name == "/" || strings.ContainsRune(name, 0) {
			teil.Close()
			continue
		}
		err = hochladenEine(c, ziel, name, ersetzen[name], teil, hilfe)
		teil.Close()
		var df *dateiFehler
		switch {
		case err == nil:
			fertig = append(fertig, name)
		case errors.Is(err, errDateiVorhanden):
			vorhanden = append(vorhanden, name)
		case errors.As(err, &df) && df.gesperrt:
			gesperrt = append(gesperrt, name)
			gesperrtText = df.m.Text(sprache)
		case errors.As(err, &df):
			// Eigene Saetze nennen den Namen schon.
			abbruch(df.code, df.m.Text(sprache))
			return
		case errors.Is(err, errUebertragungAbgebrochen):
			abbruch(http.StatusBadRequest, texte.T(sprache, "dateien.hochladen.nichtGespeichert", name))
			return
		default:
			code, text := sftpText(sprache, err)
			abbruch(code, texte.T(sprache, "dateien.hochladen.fehlerBei", name, text))
			return
		}
	}
	if len(vorhanden)+len(gesperrt) > 0 {
		var saetze []string
		switch {
		case len(vorhanden) == 1:
			saetze = append(saetze, texte.T(sprache, "dateien.vorhanden", vorhanden[0]))
		case len(vorhanden) > 1:
			saetze = append(saetze, texte.T(sprache, "dateien.hochladen.vorhandenMehrere", zitatListe(sprache, vorhanden)))
		}
		switch {
		case len(gesperrt) == 1:
			saetze = append(saetze, gesperrtText)
		case len(gesperrt) > 1:
			saetze = append(saetze, texte.T(sprache, "dateien.hochladen.gesperrtMehrere", zitatListe(sprache, gesperrt)))
		}
		jsonAntwort(w, http.StatusConflict, map[string]any{"fehler": strings.Join(saetze, ". "),
			"vorhanden": vorhanden, "gesperrt": gesperrt, "hochgeladen": fertig})
		return
	}
	jsonAntwort(w, http.StatusOK, map[string]any{"hochgeladen": fertig})
}

// leseWaechter merkt sich Lesefehler des Browsers und macht aus io.ErrUnexpectedEOF
// einen eigenen Fehler (siehe errUebertragungAbgebrochen).
type leseWaechter struct {
	r   io.Reader
	err error
}

func (l *leseWaechter) Read(p []byte) (int, error) {
	n, err := l.r.Read(p)
	if err != nil && err != io.EOF {
		l.err = err
		if errors.Is(err, io.ErrUnexpectedEOF) {
			err = errUebertragungAbgebrochen
		}
	}
	return n, err
}

// linkAufloesen folgt symbolischen Verknuepfungen (hoechstens 16 Stufen) und liefert den
// Pfad des eigentlichen Ziels. So ersetzt ein Hochladen den Inhalt hinter der Verknuepfung
// und nicht die Verknuepfung selbst. Fehlt das Ziel, kommt sein Pfad mit fi == nil (wie
// «cp» legt das Hochladen es dann an). Bleibt nach 16 Stufen eine Verknuepfung, kommt sie
// selbst zurueck (keine gewoehnliche Datei).
func linkAufloesen(c *sftp.Client, p string, fi os.FileInfo) (string, os.FileInfo, error) {
	for i := 0; i < 16 && fi.Mode()&os.ModeSymlink != 0; i++ {
		z, err := c.ReadLink(p)
		if err != nil {
			return p, fi, err
		}
		if !strings.HasPrefix(z, "/") {
			z = path.Join(path.Dir(p), z)
		}
		z = path.Clean(z)
		zfi, err := c.Lstat(z)
		if errors.Is(err, os.ErrNotExist) {
			return z, nil, nil
		}
		if err != nil {
			return p, fi, err
		}
		p, fi = z, zfi
	}
	return p, fi, nil
}

// kuerzen schneidet s auf hoechstens n Bytes, ohne ein UTF-8-Zeichen zu zerteilen
// (Dateinamen duerfen meist nur 255 Bytes lang sein, die Endung braucht Platz).
func kuerzen(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func zufallsEndung() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// nebenName liefert einen freien Namen neben p: «.<name>.<zufall>.sshapp-<art>».
func nebenName(p, art string) string {
	return path.Join(path.Dir(p), "."+kuerzen(path.Base(p), 180)+"."+zufallsEndung()+".sshapp-"+art)
}

// entfernen raeumt eine Hilfsdatei weg; scheitert das, steht es im Log (false: sie liegt noch da).
func entfernen(c *sftp.Client, p string) bool {
	if err := c.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("Hilfsdatei %s bleibt liegen: %v", p, err)
		return false
	}
	return true
}

// beantwortet: Der Host hat die Anfrage beantwortet (und abgelehnt). Bei anderen Fehlern
// (Verbindung weg) ist offen, ob er sie noch ausgefuehrt hat.
func beantwortet(err error) bool {
	var se *sftp.StatusError
	return errors.As(err, &se) || errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist)
}

// uebertragen schreibt den Browser-Strom in f und schliesst f.
func uebertragen(f *sftp.File, quelle io.Reader, ziel string) error {
	leser := &leseWaechter{r: quelle}
	// Gleichzeitige Schreibauftraege sind hier unbedenklich: Bei einem Fehler wird die
	// Datei ohnehin verworfen, Luecken darin spielen keine Rolle.
	_, err := f.ReadFromWithConcurrency(leser, 0)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if leser.err != nil {
		log.Printf("Hochladen %s abgebrochen: %v", ziel, leser.err)
		return errUebertragungAbgebrochen
	}
	return err
}

// ersetzHilfe: was hochladenEine zum Ersetzen ausser dem SFTP-Client braucht (aus der
// Sitzung; in Tests von Hand).
type ersetzHilfe struct {
	// kanal oeffnet einen eigenen SFTP-Kanal zum Host fuer hostKopieren (nil: der Host
	// kann nicht selbst kopieren).
	kanal func() (io.ReadWriteCloser, error)
	// anmelden meldet die Phase an, in der die vorhandene Datei schon ueberschrieben und danach
	// die Zwischendatei entfernt wird; die zurueckgegebene Funktion meldet sie wieder ab (siehe
	// EinsetzenAbwarten).
	anmelden func() (fertig func())
}

// hochladenEine schreibt eine Datei nach ordner/name. Eine neue Datei entsteht erst als
// «.name.<zufall>.sshapp-upload» und bekommt nach vollstaendiger Uebertragung ihren Namen.
// Ersetzen (nur mit Erlaubnis) siehe ersetzenMit.
func hochladenEine(c *sftp.Client, ordner, name string, ersetzen bool, quelle io.Reader, hilfe ersetzHilfe) error {
	dateiPfad := path.Join(ordner, name)
	fi, err := c.Lstat(dateiPfad)
	if errors.Is(err, os.ErrNotExist) {
		return neuAnlegen(c, dateiPfad, quelle)
	}
	if err != nil {
		return err
	}
	// Ordner und Spezialdateien lassen sich nie ersetzen: gar nicht erst nachfragen lassen.
	ziel, zfi, err := linkAufloesen(c, dateiPfad, fi)
	switch {
	case err != nil:
		return err
	case zfi != nil && zfi.IsDir():
		f := meldung(http.StatusConflict, "dateien.hochladen.istOrdner", name)
		f.gesperrt = true
		return f
	case zfi != nil && !zfi.Mode().IsRegular():
		f := meldung(http.StatusConflict, "dateien.hochladen.keineDatei", name)
		f.gesperrt = true
		return f
	case !ersetzen:
		return errDateiVorhanden
	case zfi == nil:
		return neuAnlegen(c, ziel, quelle) // Verknuepfung ohne Ziel: Ziel anlegen (wie cp)
	}
	return ersetzenMit(c, ziel, zfi, name, quelle, hilfe)
}

// neuAnlegen: temporaere Datei schreiben, dann umbenennen. SSH_FXP_RENAME ersetzt bei
// OpenSSH nie ein vorhandenes Ziel.
func neuAnlegen(c *sftp.Client, ziel string, quelle io.Reader) error {
	tmp := nebenName(ziel, "upload")
	f, err := c.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return err
	}
	if err := uebertragen(f, quelle, ziel); err != nil {
		entfernen(c, tmp)
		return err
	}
	if _, e := c.Lstat(ziel); e == nil {
		// Waehrend der Uebertragung angelegt: nicht still ueberschreiben.
		entfernen(c, tmp)
		return errDateiVorhanden
	}
	if err := c.Rename(tmp, ziel); err != nil {
		entfernen(c, tmp)
		return err
	}
	return nil
}

// ersetzenMit ersetzt den Inhalt der gewoehnlichen Datei ziel (vorlage = ihr Lstat).
//
// Die Datei selbst bleibt erhalten: Besitzer, Gruppe, Rechte, ACLs, harte Verknuepfungen
// und per Docker eingebundene Einzeldateien sehen den neuen Inhalt. Dazu kommt der Strom
// des Browsers erst vollstaendig in eine temporaere Datei neben ziel (ein Abbruch bis
// dahin laesst das Original stehen). Danach wird sie in die Datei eingesetzt
// (fassungEinsetzen): vom Host selbst, wenn sein SFTP-Dienst «copy-data» kennt (OpenSSH
// ab 9.0), sonst liest die App sie zurueck und schreibt sie in die Datei.
//
//   - Darf der Benutzer die Datei nicht schreiben: abgelehnt (wie frueher «permission denied»),
//     bevor etwas uebertragen wird.
//   - Laesst sie sich nicht zum Schreiben oeffnen, ohne dass Rechte fehlen (z. B. «Text file
//     busy» bei einem laufenden Programm): temporaere Datei mit Rechten und Besitzer der
//     Vorlage, dann ersetzend umbenennen.
//   - Fehlt im Ordner das Schreibrecht fuer die temporaere Datei: wie frueher direkt in die
//     Datei schreiben (ein Abbruch hinterlaesst sie dann unvollstaendig).
func ersetzenMit(c *sftp.Client, ziel string, vorlage os.FileInfo, name string, quelle io.Reader, hilfe ersetzHilfe) error {
	// Oeffnen ohne O_TRUNC aendert nichts an der Datei.
	vorOrt := true
	probe, err := c.OpenFile(ziel, os.O_WRONLY)
	switch {
	case err == nil:
		probe.Close()
	case errors.Is(err, os.ErrPermission):
		return meldung(http.StatusForbidden, "dateien.hochladen.keinSchreibrecht", name).mit(err)
	case errors.Is(err, os.ErrNotExist):
		return err
	default:
		log.Printf("Hochladen: %s nicht beschreibbar (%v), ersetze per Umbenennen", ziel, err)
		vorOrt = false
	}

	tmp := nebenName(ziel, "upload")
	f, err := c.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		if vorOrt && errors.Is(err, os.ErrPermission) {
			return direktSchreiben(c, ziel, name, quelle)
		}
		return err
	}
	if err := uebertragen(f, quelle, ziel); err != nil {
		entfernen(c, tmp)
		return err
	}
	if vorOrt {
		// Die Phase bleibt angemeldet, bis auch tmp wieder weg ist: Sonst schlossen «Sitzung
		// beenden» und das Herunterfahren die Verbindung mitten im Aufraeumen, und eine
		// versteckte Vollkopie bliebe liegen (siehe EinsetzenAbwarten).
		if hilfe.anmelden != nil {
			defer hilfe.anmelden()()
		}
		angefangen, err := fassungEinsetzen(c, tmp, ziel, hilfe)
		switch {
		case err == nil:
			entfernen(c, tmp)
			return nil
		case angefangen:
			// Das Original ist angeschrieben: die vollstaendige Fassung nicht wegwerfen.
			log.Printf("Hochladen: %s unvollstaendig, vollstaendig in %s: %v", ziel, tmp, err)
			code, _ := sftpText(texte.DE, err)
			return meldung(code, "dateien.hochladen.einsetzenFehler", name, path.Base(tmp)).mit(err)
		case !entfernen(c, tmp):
			// Abbruch (meist: Verbindung weg), bevor das Original beruehrt war: Es ist unveraendert,
			// und die vollstaendige Fassung bleibt liegen (sie liess sich nicht mehr entfernen).
			code, _ := sftpText(texte.DE, err)
			return meldung(code, "dateien.hochladen.einsetzenNichtBegonnen", name, path.Base(tmp)).mit(err)
		}
		return err
	}
	besitzUebernehmen(c, tmp, vorlage)
	if err := ersetzendUmbenennen(c, tmp, ziel); err != nil {
		entfernen(c, tmp)
		return err
	}
	return nil
}

// fassungEinsetzen bringt die vollstaendig hochgeladene Fassung tmp in die vorhandene Datei
// ziel. angefangen meldet, ob ziel dabei schon angeschrieben sein kann.
//
// Kann der Host selbst kopieren (hostKopieren), gehen die Daten nicht noch einmal ueber die
// SSH-Verbindung. Sonst (aeltere Hosts, andere SFTP-Dienste) liest die App tmp zurueck und
// schreibt es in ziel: Die Daten gehen dann zweimal mehr ueber die SSH-Verbindung (zuerst
// in tmp, dann gelesen, dann geschrieben). Das ist der Preis dafuer, dass die Datei selbst
// erhalten bleibt; SFTP v3 kennt sonst keine Kopie auf dem Host. Der Browser zeigt diese
// Phase als «Ersetze …».
//
// Waehrend des Einsetzens ist die Phase angemeldet (ersetzenMit, bis tmp aufgeraeumt ist):
// Endet die Sitzung mittendrin, bliebe ziel unvollstaendig (die vollstaendige Fassung laege
// noch in tmp). «Sitzung beenden» und das Herunterfahren warten darum mit EinsetzenAbwarten.
func fassungEinsetzen(c *sftp.Client, tmp, ziel string, hilfe ersetzHilfe) (angefangen bool, err error) {
	if hilfe.kanal != nil {
		beruehrt, err := aufDemHostEinsetzen(c, tmp, ziel, hilfe.kanal)
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, errKeineHostKopie) {
			log.Printf("Hochladen: Kopie auf dem Host nach %s gescheitert, kopiere über die App: %v", ziel, err)
		}
		angefangen = beruehrt
	}
	a, err := inhaltEinsetzen(c, tmp, ziel)
	return angefangen || a, err
}

// aufDemHostEinsetzen oeffnet einen eigenen SFTP-Kanal und laesst den Host tmp nach ziel kopieren.
func aufDemHostEinsetzen(c *sftp.Client, tmp, ziel string, kanal func() (io.ReadWriteCloser, error)) (beruehrt bool, err error) {
	fi, err := c.Lstat(tmp)
	if err != nil {
		return false, err
	}
	k, err := kanal()
	if err != nil {
		return false, err
	}
	defer k.Close()
	return hostKopieren(k, tmp, ziel, fi.Size())
}

// inhaltEinsetzen kopiert von in die vorhandene Datei nach, ueber die App: Die Daten werden
// gelesen und wieder geschrieben (die Datei behaelt Inode, Besitzer und Rechte). angefangen
// meldet, ob nach schon gekuerzt sein kann.
func inhaltEinsetzen(c *sftp.Client, von, nach string) (angefangen bool, err error) {
	src, err := c.Open(von)
	if err != nil {
		return false, err
	}
	defer src.Close()
	dst, err := c.OpenFile(nach, os.O_WRONLY|os.O_TRUNC)
	if err != nil {
		// Ohne Antwort des Hosts ist offen, ob er schon gekuerzt hat.
		return !beantwortet(err), err
	}
	_, err = io.Copy(dst, src) // src.WriteTo: liest mit mehreren Anfragen gleichzeitig
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	return true, err
}

// direktSchreiben schreibt den Strom wie frueher direkt in die Datei (Ordner ohne Schreibrecht).
func direktSchreiben(c *sftp.Client, ziel, name string, quelle io.Reader) error {
	f, err := c.OpenFile(ziel, os.O_WRONLY|os.O_TRUNC)
	if err != nil {
		return err
	}
	err = uebertragen(f, quelle, ziel)
	if errors.Is(err, errUebertragungAbgebrochen) {
		return meldung(http.StatusBadRequest, "dateien.hochladen.abbruchDirekt", name).mit(err)
	}
	return err
}

// besitzUebernehmen gibt der temporaeren Datei Besitzer, Gruppe und Rechte der Vorlage.
// Ohne root scheitert chown auf einen fremden Besitzer; dann wenigstens die Gruppe
// behalten (erlaubt, wenn der Benutzer Mitglied ist). Die Rechte kommen danach, weil
// chown Set-UID/Set-GID loeschen kann.
func besitzUebernehmen(c *sftp.Client, tmp string, vorlage os.FileInfo) {
	if st, ok := vorlage.Sys().(*sftp.FileStat); ok {
		if err := c.Chown(tmp, int(st.UID), int(st.GID)); err != nil {
			if eigen, e := c.Lstat(tmp); e == nil {
				if est, ok := eigen.Sys().(*sftp.FileStat); ok && est.GID != st.GID {
					if err := c.Chown(tmp, int(est.UID), int(st.GID)); err != nil {
						log.Printf("Hochladen: Gruppe von %s nicht uebernommen: %v", tmp, err)
					}
				}
			}
		}
	}
	_ = c.Chmod(tmp, vorlage.Mode()&(os.ModePerm|os.ModeSetuid|os.ModeSetgid|os.ModeSticky))
}

// ersetzendUmbenennen ersetzt nach durch von: in einem Schritt per posix-rename. Kennt der
// Host die Erweiterung nicht, wird das alte Ziel erst beiseite gelegt und nur bei Erfolg
// geloescht; scheitert das Umbenennen, kommt es zurueck. So gehen nie beide verloren.
func ersetzendUmbenennen(c *sftp.Client, von, nach string) error {
	if _, ok := c.HasExtension(posixRename); ok {
		return c.PosixRename(von, nach)
	}
	alt := nebenName(nach, "alt")
	if err := c.Rename(nach, alt); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return c.Rename(von, nach)
		}
		return err
	}
	if err := c.Rename(von, nach); err != nil {
		if rerr := c.Rename(alt, nach); rerr != nil {
			log.Printf("Umbenennen: %s liegt als %s beiseite: %v / %v", nach, alt, err, rerr)
			code, _ := sftpText(texte.DE, err)
			return meldung(code, "dateien.umbenennen.originalGesichert", path.Base(nach), path.Base(alt)).mit(err)
		}
		return err
	}
	entfernen(c, alt)
	return nil
}

// zielBelegt prueft, ob nach schon vergeben ist. Aendert sich nur die Gross-/Kleinschreibung,
// zaehlt nur ein Eintrag mit genau diesem Namen: Auf Dateisystemen ohne Unterscheidung
// faende Lstat sonst die Quelle selbst.
func zielBelegt(c *sftp.Client, von, nach string) (os.FileInfo, bool, error) {
	if von != nach && strings.EqualFold(von, nach) {
		infos, err := c.ReadDir(path.Dir(nach))
		if err != nil {
			return nil, false, err
		}
		for _, fi := range infos {
			if fi.Name() == path.Base(nach) {
				return fi, true, nil
			}
		}
		return nil, false, nil
	}
	fi, err := c.Lstat(nach)
	if err == nil {
		return fi, true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return nil, false, err
}

// umbenennen ersetzt ein vorhandenes Ziel nur mit ausdruecklicher Erlaubnis (Rueckfrage im Browser).
func umbenennen(c *sftp.Client, von, nach string, ersetzen bool) error {
	if nach == von {
		return nil
	}
	if strings.HasPrefix(nach, von+"/") {
		return meldung(http.StatusBadRequest, "dateien.umbenennen.inSichSelbst")
	}
	ziel, belegt, err := zielBelegt(c, von, nach)
	if err != nil {
		return err
	}
	if !belegt {
		if strings.EqualFold(von, nach) {
			// Nur Gross-/Kleinschreibung: das einfache Rename scheitert dort, wo beide Namen dieselbe Datei sind.
			if _, ok := c.HasExtension(posixRename); ok {
				return c.PosixRename(von, nach)
			}
		}
		return c.Rename(von, nach)
	}
	if !ersetzen {
		return meldung(http.StatusConflict, "dateien.vorhanden", path.Base(nach))
	}
	if ziel.IsDir() {
		return meldung(http.StatusConflict, "dateien.umbenennen.istOrdner", path.Base(nach))
	}
	return ersetzendUmbenennen(c, von, nach)
}

func (s *Server) dateiAktion(w http.ResponseWriter, r *http.Request) {
	_, c, ok := s.sftpFuer(w, r)
	if !ok {
		return
	}
	var a struct {
		Art      string `json:"art"` // ordner | umbenennen | loeschen
		Pfad     string `json:"pfad"`
		Nach     string `json:"nach"`
		Ersetzen bool   `json:"ersetzen"` // umbenennen: vorhandenes Ziel ersetzen (nach Rueckfrage)
	}
	if err := lesen(r, &a); err != nil {
		fehlerAus(w, r, http.StatusBadRequest, err)
		return
	}
	p, err := sauberPfad(a.Pfad)
	if err != nil || p == "/" {
		fehlerT(w, r, http.StatusBadRequest, "dateien.pfadUngueltig")
		return
	}
	switch a.Art {
	case "ordner":
		if _, e := c.Lstat(p); e == nil {
			fehlerT(w, r, http.StatusConflict, "dateien.vorhanden", path.Base(p))
			return
		}
		err = c.Mkdir(p)
	case "umbenennen":
		nach, e := sauberPfad(a.Nach)
		if e != nil || nach == "/" {
			fehlerT(w, r, http.StatusBadRequest, "dateien.nameUngueltig")
			return
		}
		err = umbenennen(c, p, nach, a.Ersetzen)
	case "loeschen":
		var fi os.FileInfo
		if fi, err = c.Lstat(p); err == nil {
			if fi.IsDir() {
				err = c.RemoveAll(p)
			} else {
				err = c.Remove(p)
			}
		}
	default:
		fehlerT(w, r, http.StatusBadRequest, "dateien.aktionUnbekannt")
		return
	}
	if err != nil {
		sftpFehler(w, r, err)
		return
	}
	jsonAntwort(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------- Kopie auf dem Host (SFTP-Erweiterung «copy-data») ----------

// SFTP-Pakete und -Codes (draft-ietf-secsh-filexfer-02), soweit hostKopieren sie braucht.
const (
	fxpInit     = 1
	fxpVersion  = 2
	fxpOpen     = 3
	fxpClose    = 4
	fxpFsetstat = 10
	fxpStatus   = 101
	fxpHandle   = 102
	fxpExtended = 200

	fxfRead  = 0x01
	fxfWrite = 0x02

	fxOK               = 0
	fxNoSuchFile       = 2
	fxPermissionDenied = 3
	fxOpUnsupported    = 8

	attrGroesse = 0x01 // SSH_FILEXFER_ATTR_SIZE

	// sftpPaketMax begrenzt eine Antwort des Hosts (OpenSSH schickt hoechstens 256 KiB).
	sftpPaketMax = 1 << 20
)

// kopieDaten: Erweiterung des OpenSSH-SFTP-Dienstes (ab 9.0, draft-ietf-secsh-filexfer-extensions):
// Der Host kopiert zwischen zwei offenen Dateien, ohne dass die Daten ueber die Verbindung gehen.
const kopieDaten = "copy-data"

// errKeineHostKopie: Der SFTP-Dienst des Hosts kann nicht selbst kopieren.
var errKeineHostKopie = errors.New("SFTP-Dienst ohne copy-data")

// errSFTPAntwort: Die Antwort des Hosts passt nicht zur Anfrage.
var errSFTPAntwort = errors.New("unerwartete SFTP-Antwort")

// sftpStatus ist eine Fehlerantwort (SSH_FXP_STATUS) auf eine Anfrage von sftpRoh.
type sftpStatus struct {
	code uint32
	text string
}

func (e *sftpStatus) Error() string { return fmt.Sprintf("SFTP-Status %d: %s", e.code, e.text) }

// Is ordnet die Codes wie pkg/sftp ein (fuer sftpText und errors.Is).
func (e *sftpStatus) Is(ziel error) bool {
	switch e.code {
	case fxNoSuchFile:
		return ziel == os.ErrNotExist
	case fxPermissionDenied:
		return ziel == os.ErrPermission
	}
	return false
}

func paketLesen(r io.Reader) ([]byte, error) {
	var l [4]byte
	if _, err := io.ReadFull(r, l[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(l[:])
	if n > sftpPaketMax {
		return nil, errSFTPAntwort
	}
	b := make([]byte, n)
	_, err := io.ReadFull(r, b)
	return b, err
}

func paketSchreiben(w io.Writer, b []byte) error {
	_, err := w.Write(append(binary.BigEndian.AppendUint32(nil, uint32(len(b))), b...))
	return err
}

// zeichenkette liest eine SFTP-Zeichenkette; rest ist nil, wenn sie nicht vollstaendig ist.
func zeichenkette(b []byte) (string, []byte) {
	if len(b) < 4 || uint32(len(b)-4) < binary.BigEndian.Uint32(b) {
		return "", nil
	}
	n := binary.BigEndian.Uint32(b)
	return string(b[4 : 4+n]), b[4+n:]
}

// hatErweiterung sucht name in den Erweiterungen eines VERSION-Pakets (nach der Versionsnummer).
func hatErweiterung(b []byte, name string) bool {
	for len(b) > 0 {
		n, rest := zeichenkette(b)
		if rest == nil {
			return false
		}
		if _, rest = zeichenkette(rest); rest == nil {
			return false
		}
		if n == name {
			return true
		}
		b = rest
	}
	return false
}

// sftpFelder baut den Inhalt einer Anfrage (nach Typ und ID).
type sftpFelder []byte

func (f sftpFelder) text(s string) sftpFelder {
	return append(binary.BigEndian.AppendUint32(f, uint32(len(s))), s...)
}
func (f sftpFelder) u32(v uint32) sftpFelder { return binary.BigEndian.AppendUint32(f, v) }
func (f sftpFelder) u64(v uint64) sftpFelder { return binary.BigEndian.AppendUint64(f, v) }

// sftpRoh spricht SFTP v3 direkt auf einem eigenen Kanal, Anfrage um Anfrage:
// pkg/sftp kennt «copy-data» nicht.
type sftpRoh struct {
	rw io.ReadWriter
	id uint32
}

// anfrage schickt typ mit neuer ID und liefert Typ und Inhalt der Antwort (nach der ID).
// Eine STATUS-Antwort kommt als Fehler zurueck (nil bei SSH_FX_OK).
func (s *sftpRoh) anfrage(typ byte, felder sftpFelder) (byte, []byte, error) {
	s.id++
	if err := paketSchreiben(s.rw, append(binary.BigEndian.AppendUint32([]byte{typ}, s.id), felder...)); err != nil {
		return 0, nil, err
	}
	a, err := paketLesen(s.rw)
	if err != nil {
		return 0, nil, err
	}
	if len(a) < 5 || binary.BigEndian.Uint32(a[1:5]) != s.id {
		return 0, nil, errSFTPAntwort
	}
	if a[0] != fxpStatus {
		return a[0], a[5:], nil
	}
	if len(a) < 9 {
		return 0, nil, errSFTPAntwort
	}
	if code := binary.BigEndian.Uint32(a[5:9]); code != fxOK {
		text, _ := zeichenkette(a[9:])
		return fxpStatus, nil, &sftpStatus{code: code, text: text}
	}
	return fxpStatus, nil, nil
}

// status schickt eine Anfrage, auf die nur ein STATUS zurueckkommt.
func (s *sftpRoh) status(typ byte, felder sftpFelder) error {
	t, _, err := s.anfrage(typ, felder)
	if err == nil && t != fxpStatus {
		return errSFTPAntwort
	}
	return err
}

// oeffnen liefert einen Handle fuer pfad (pflags: SSH_FXF_*, ohne Attribute).
func (s *sftpRoh) oeffnen(pfad string, pflags uint32) (string, error) {
	t, rest, err := s.anfrage(fxpOpen, sftpFelder(nil).text(pfad).u32(pflags).u32(0))
	if err != nil {
		return "", err
	}
	h, rest := zeichenkette(rest)
	if t != fxpHandle || rest == nil || h == "" {
		return "", errSFTPAntwort
	}
	return h, nil
}

// hostKopieren laesst den Host von in die vorhandene Datei nach kopieren (rw: ein eigener,
// frischer SFTP-Kanal). Die Daten gehen dabei nicht ueber die Verbindung.
//
// nach wird ab dem Anfang ueberschrieben und erst danach auf laenge gekuerzt, nicht vorher:
// Bricht die Verbindung vor der Kopie ab, bleibt das Original unberuehrt. Die Kopie selbst
// ist eine einzige Anfrage; der sftp-server von OpenSSH arbeitet sie auch dann zu Ende ab,
// wenn die Verbindung waehrenddessen abreisst (er merkt das erst beim Antworten). Nur wenn
// die neue Fassung kuerzer ist, steht zwischen Kopie und Kuerzen (eine Antwortzeit lang)
// noch das Ende des Originals dahinter.
//
// beruehrt meldet, ob nach schon angeschrieben sein kann. errKeineHostKopie: Der Dienst
// kennt «copy-data» nicht (nach ist unberuehrt).
func hostKopieren(rw io.ReadWriter, von, nach string, laenge int64) (beruehrt bool, err error) {
	if err := paketSchreiben(rw, binary.BigEndian.AppendUint32([]byte{fxpInit}, 3)); err != nil {
		return false, err
	}
	v, err := paketLesen(rw)
	if err != nil {
		return false, err
	}
	if len(v) < 5 || v[0] != fxpVersion || !hatErweiterung(v[5:], kopieDaten) {
		return false, errKeineHostKopie
	}
	s := &sftpRoh{rw: rw}
	lesen, err := s.oeffnen(von, fxfRead)
	if err != nil {
		return false, err
	}
	defer s.status(fxpClose, sftpFelder(nil).text(lesen))
	schreiben, err := s.oeffnen(nach, fxfWrite)
	if err != nil {
		return false, err
	}
	// Lesen ab 0 bis zum Ende (Laenge 0), schreiben ab 0.
	err = s.status(fxpExtended, sftpFelder(nil).text(kopieDaten).text(lesen).u64(0).u64(0).text(schreiben).u64(0))
	var st *sftpStatus
	beruehrt = !(errors.As(err, &st) && st.code == fxOpUnsupported)
	if err == nil {
		err = s.status(fxpFsetstat, sftpFelder(nil).text(schreiben).u32(attrGroesse).u64(uint64(laenge)))
	}
	if cerr := s.status(fxpClose, sftpFelder(nil).text(schreiben)); err == nil {
		err = cerr
	}
	if !beruehrt {
		err = errKeineHostKopie
	}
	return beruehrt, err
}

// sftpKanal ist ein weiterer SFTP-Kanal auf der Verbindung einer Sitzung (fuer hostKopieren).
type sftpKanal struct {
	io.Reader
	io.Writer
	sess *ssh.Session
}

func (k *sftpKanal) Close() error { return k.sess.Close() }

func sftpKanalOeffnen(client *ssh.Client) (io.ReadWriteCloser, error) {
	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	w, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		return nil, err
	}
	r, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		return nil, err
	}
	if err := sess.RequestSubsystem("sftp"); err != nil {
		sess.Close()
		return nil, err
	}
	return &sftpKanal{Reader: r, Writer: w, sess: sess}, nil
}

// ---------- Einsetzen abwarten ----------

type einsetzSchluessel struct {
	s  *Server
	id string
}

// einsetzLaeufe zaehlt je Sitzung, wie viele hochgeladene Dateien gerade in vorhandene
// eingesetzt werden (fassungEinsetzen, samt Aufraeumen der Zwischendatei in ersetzenMit).
// geaendert wird bei jedem Ende geschlossen und erneuert.
var einsetzLaeufe = struct {
	sync.Mutex
	n         map[einsetzSchluessel]int
	geaendert chan struct{}
}{n: map[einsetzSchluessel]int{}, geaendert: make(chan struct{})}

// einsetzenAnmelden meldet ein Einsetzen in der Sitzung id an; fertig meldet es ab.
func (s *Server) einsetzenAnmelden(id string) (fertig func()) {
	k := einsetzSchluessel{s, id}
	einsetzLaeufe.Lock()
	einsetzLaeufe.n[k]++
	einsetzLaeufe.Unlock()
	var einmal sync.Once
	return func() {
		einmal.Do(func() {
			einsetzLaeufe.Lock()
			if einsetzLaeufe.n[k]--; einsetzLaeufe.n[k] <= 0 {
				delete(einsetzLaeufe.n, k)
			}
			close(einsetzLaeufe.geaendert)
			einsetzLaeufe.geaendert = make(chan struct{})
			einsetzLaeufe.Unlock()
		})
	}
}

// einsetzenLaufend: Anzahl laufender Einsetzungen in der Sitzung id (leer: in allen Sitzungen).
func (s *Server) einsetzenLaufend(id string) int {
	einsetzLaeufe.Lock()
	defer einsetzLaeufe.Unlock()
	return s.einsetzenZaehlen(id)
}

func (s *Server) einsetzenZaehlen(id string) int {
	n := 0
	for k, v := range einsetzLaeufe.n {
		if k.s == s && (id == "" || k.id == id) {
			n += v
		}
	}
	return n
}

// EinsetzenAbwarten wartet hoechstens frist, bis in der Sitzung id (leer: in keiner Sitzung
// dieses Servers) mehr eine hochgeladene Datei in eine vorhandene eingesetzt und ihre
// Zwischendatei aufgeraeumt wird. Endet die SFTP-Verbindung mitten im Einsetzen, bleibt die
// Datei unvollstaendig (beim Aufraeumen: eine versteckte Vollkopie liegen): Darum vor dem Beenden
// einer Sitzung und vor dem Schliessen aller Sitzungen beim Herunterfahren aufrufen.
// false: Die Frist ist abgelaufen.
func (s *Server) EinsetzenAbwarten(id string, frist time.Duration) bool {
	ende := time.NewTimer(frist)
	defer ende.Stop()
	gemeldet := false
	for {
		einsetzLaeufe.Lock()
		n := s.einsetzenZaehlen(id)
		warte := einsetzLaeufe.geaendert
		einsetzLaeufe.Unlock()
		if n == 0 {
			return true
		}
		if !gemeldet {
			log.Printf("Warte auf %d hochgeladene Datei(en), die gerade eingesetzt werden", n)
			gemeldet = true
		}
		select {
		case <-warte:
		case <-ende.C:
			log.Printf("Einsetzen nach %v nicht fertig — Sitzung wird trotzdem beendet", frist)
			return false
		}
	}
}
