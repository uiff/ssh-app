package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pkg/sftp"

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
var errUebertragungAbgebrochen = errors.New("Übertragung vom Browser abgebrochen")

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
		case errors.As(err, &m): // z.B. term.ErrKeinSFTP
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
// Anfrage. Der rohe Text (z.B. «sftp: "Failure" (SSH_FX_FAILURE)») geht nur ins Server-Log.
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
	_, c, ok := s.sftpFuer(w, r)
	if !ok {
		return
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
		err = hochladenEine(c, ziel, name, ersetzen[name], teil)
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

// entfernen raeumt eine Hilfsdatei weg; scheitert das, steht es im Log.
func entfernen(c *sftp.Client, p string) {
	if err := c.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("Hilfsdatei %s bleibt liegen: %v", p, err)
	}
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

// hochladenEine schreibt eine Datei nach ordner/name. Eine neue Datei entsteht erst als
// «.name.<zufall>.sshapp-upload» und bekommt nach vollstaendiger Uebertragung ihren Namen.
// Ersetzen (nur mit Erlaubnis) siehe ersetzenMit.
func hochladenEine(c *sftp.Client, ordner, name string, ersetzen bool, quelle io.Reader) error {
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
	return ersetzenMit(c, ziel, zfi, name, quelle)
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
// erst vollstaendig in eine temporaere Datei (ein Abbruch laesst das Original stehen) und
// wird danach auf dem Host in die Datei kopiert.
//
//   - Darf der Benutzer die Datei nicht schreiben: abgelehnt (wie frueher «permission denied»),
//     bevor etwas uebertragen wird.
//   - Laesst sie sich nicht zum Schreiben oeffnen, ohne dass Rechte fehlen (z.B. «Text file
//     busy» bei einem laufenden Programm): temporaere Datei mit Rechten und Besitzer der
//     Vorlage, dann ersetzend umbenennen.
//   - Fehlt im Ordner das Schreibrecht fuer die temporaere Datei: wie frueher direkt in die
//     Datei schreiben (ein Abbruch hinterlaesst sie dann unvollstaendig).
func ersetzenMit(c *sftp.Client, ziel string, vorlage os.FileInfo, name string, quelle io.Reader) error {
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
		angefangen, err := inhaltEinsetzen(c, tmp, ziel)
		if err != nil && angefangen {
			// Das Original ist angeschrieben: die vollstaendige Fassung nicht wegwerfen.
			log.Printf("Hochladen: %s unvollstaendig, vollstaendig in %s: %v", ziel, tmp, err)
			code, _ := sftpText(texte.DE, err)
			return meldung(code, "dateien.hochladen.einsetzenFehler", name, path.Base(tmp)).mit(err)
		}
		entfernen(c, tmp)
		return err
	}
	besitzUebernehmen(c, tmp, vorlage)
	if err := ersetzendUmbenennen(c, tmp, ziel); err != nil {
		entfernen(c, tmp)
		return err
	}
	return nil
}

// inhaltEinsetzen kopiert von in die vorhandene Datei nach (auf dem Host, die Datei behaelt
// Inode, Besitzer und Rechte). angefangen meldet, ob nach schon gekuerzt wurde.
func inhaltEinsetzen(c *sftp.Client, von, nach string) (angefangen bool, err error) {
	src, err := c.Open(von)
	if err != nil {
		return false, err
	}
	defer src.Close()
	dst, err := c.OpenFile(nach, os.O_WRONLY|os.O_TRUNC)
	if err != nil {
		return false, err
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
