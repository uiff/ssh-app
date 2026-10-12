package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pkg/sftp"

	"github.com/uiff/ssh-app/internal/term"
)

type eintrag struct {
	Name      string    `json:"name"`
	Ordner    bool      `json:"ordner"`
	Link      bool      `json:"link,omitempty"`
	Groesse   int64     `json:"groesse"`
	Rechte    string    `json:"rechte"`
	Geaendert time.Time `json:"geaendert"`
}

// posixRename ersetzt ein vorhandenes Ziel in einem Schritt (OpenSSH-Erweiterung).
const posixRename = "posix-rename@openssh.com"

// sftpMeldung ist ein Fehler mit fertigem Satz fuer die Oberflaeche.
type sftpMeldung struct {
	code int
	text string
}

func (m *sftpMeldung) Error() string { return m.text }

// errDateiVorhanden: Der Name ist am Ziel schon belegt und darf nicht ersetzt werden.
var errDateiVorhanden = errors.New("gibt es schon")

// errUebertragungAbgebrochen ersetzt io.ErrUnexpectedEOF des Browsers, damit pkg/sftp
// (das ErrUnexpectedEOF als regulaeres Ende wertet) den Abbruch nicht verschluckt.
var errUebertragungAbgebrochen = errors.New("Übertragung vom Browser abgebrochen")

func (s *Server) sftpFuer(w http.ResponseWriter, r *http.Request) (*term.Sitzung, *sftp.Client, bool) {
	sz, ok := s.tm.Hole(r.PathValue("id"))
	if !ok {
		fehler(w, http.StatusNotFound, "Sitzung existiert nicht mehr")
		return nil, nil, false
	}
	c, err := sz.SFTP()
	if err != nil {
		fehler(w, http.StatusBadGateway, err.Error())
		return nil, nil, false
	}
	return sz, c, true
}

func sauberPfad(p string) (string, error) {
	if p == "" || !strings.HasPrefix(p, "/") || strings.ContainsRune(p, 0) {
		return "", errors.New("Pfad muss absolut sein")
	}
	return path.Clean(p), nil
}

// sftpText uebersetzt einen SFTP-Fehler in einen verstaendlichen Satz. Der rohe Text
// (z.B. «sftp: "Failure" (SSH_FX_FAILURE)») geht nur ins Server-Log.
func sftpText(err error) (int, string) {
	var m *sftpMeldung
	var se *sftp.StatusError
	switch {
	case errors.As(err, &m):
		return m.code, m.text
	case errors.Is(err, os.ErrNotExist):
		return http.StatusNotFound, "Datei oder Ordner gibt es dort nicht (mehr)"
	case errors.Is(err, os.ErrPermission):
		return http.StatusForbidden, "Keine Berechtigung — der angemeldete Benutzer darf das dort nicht"
	case errors.Is(err, os.ErrExist):
		return http.StatusConflict, "Gibt es dort schon"
	case errors.Is(err, sftp.ErrSSHFxConnectionLost), errors.Is(err, sftp.ErrSSHFxNoConnection),
		errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		log.Printf("SFTP: %v", err)
		return http.StatusBadGateway, "Verbindung zum Host unterbrochen — bitte neu verbinden"
	case errors.As(err, &se):
		log.Printf("SFTP: %v", err)
		switch se.FxCode() {
		case sftp.ErrSSHFxOpUnsupported:
			return http.StatusBadGateway, "Das unterstützt der SFTP-Dienst dieses Hosts nicht"
		case sftp.ErrSSHFxBadMessage:
			return http.StatusBadGateway, "Der SFTP-Dienst des Hosts hat die Anfrage nicht verstanden"
		case sftp.ErrSSHFxConnectionLost, sftp.ErrSSHFxNoConnection:
			return http.StatusBadGateway, "Verbindung zum Host unterbrochen — bitte neu verbinden"
		}
		return http.StatusBadGateway, "Der Host hat das abgelehnt, ohne einen Grund zu nennen (Datenträger voll, Datei in Benutzung oder schon vorhanden?)"
	}
	log.Printf("SFTP: %v", err)
	return http.StatusBadGateway, "Dateizugriff auf dem Host fehlgeschlagen"
}

func sftpFehler(w http.ResponseWriter, err error) {
	code, text := sftpText(err)
	fehler(w, code, text)
}

// dateienPfad liefert das Arbeitsverzeichnis der Shell (fuer «folgt Terminal»).
func (s *Server) dateienPfad(w http.ResponseWriter, r *http.Request) {
	sz, ok := s.tm.Hole(r.PathValue("id"))
	if !ok {
		fehler(w, http.StatusNotFound, "Sitzung existiert nicht mehr")
		return
	}
	p, bekannt := sz.Arbeitsverzeichnis()
	jsonAntwort(w, http.StatusOK, map[string]any{"pfad": p, "bekannt": bekannt})
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
			sftpFehler(w, err)
			return
		}
		p = wd
	}
	p, err := sauberPfad(p)
	if err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	infos, err := c.ReadDir(p)
	if err != nil {
		if fi, e := c.Stat(p); e == nil && !fi.IsDir() {
			fehler(w, http.StatusBadRequest, "«"+p+"» ist kein Ordner")
			return
		}
		sftpFehler(w, err)
		return
	}
	out := make([]eintrag, 0, len(infos))
	for _, fi := range infos {
		e := eintrag{Name: fi.Name(), Ordner: fi.IsDir(), Groesse: fi.Size(), Rechte: fi.Mode().Perm().String(), Geaendert: fi.ModTime()}
		if fi.Mode()&os.ModeSymlink != 0 {
			e.Link = true
			if ziel, err := c.Stat(path.Join(p, fi.Name())); err == nil && ziel.IsDir() {
				e.Ordner = true
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

func (s *Server) dateiLaden(w http.ResponseWriter, r *http.Request) {
	_, c, ok := s.sftpFuer(w, r)
	if !ok {
		return
	}
	p, err := sauberPfad(r.URL.Query().Get("pfad"))
	if err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	// Erst pruefen, dann oeffnen: Eine Pipe (FIFO) wuerde beim Oeffnen haengen bleiben.
	fi, err := c.Stat(p)
	if err != nil {
		sftpFehler(w, err)
		return
	}
	if fi.IsDir() {
		fehler(w, http.StatusBadRequest, "Ordner lassen sich nicht herunterladen")
		return
	}
	if !fi.Mode().IsRegular() {
		fehler(w, http.StatusBadRequest, "Nur gewöhnliche Dateien lassen sich herunterladen (das ist ein Gerät, eine Pipe oder ein Socket)")
		return
	}
	f, err := c.Open(p)
	if err != nil {
		sftpFehler(w, err)
		return
	}
	defer f.Close()
	name := path.Base(p)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	// Laenge nur angeben, wenn sie stimmt: Sonst kaeme eine leere (Groesse 0) oder eine
	// abgebrochene Datei (zu grosse Angabe) an. Ohne Angabe streamt net/http «chunked».
	if fi.Size() > 0 && !pseudoDatei(p) {
		w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
	}
	if _, err := io.Copy(w, f); err != nil {
		log.Printf("Download %s abgebrochen: %v", p, err)
	}
}

// dateiHochladen streamt jede Datei per SFTP in eine temporaere Datei neben dem Ziel und
// benennt sie erst nach vollstaendiger Uebertragung um. Ein Abbruch (Funkloch, Tab zu)
// hinterlaesst so weder ein Bruchstueck unter dem richtigen Namen noch ein zerstoertes
// Original. Vorhandene Dateien werden nur ersetzt, wenn ihr Name als «ersetzen=<name>»
// mitkommt (je Datei bestaetigt). Andere Namenskonflikte werden uebersprungen und gesammelt
// gemeldet: 409 mit «vorhanden» (alle Konflikte) und «hochgeladen» (was geklappt hat).
func (s *Server) dateiHochladen(w http.ResponseWriter, r *http.Request) {
	_, c, ok := s.sftpFuer(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	ziel, err := sauberPfad(q.Get("pfad"))
	if err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	ersetzen := map[string]bool{}
	for _, n := range q["ersetzen"] {
		ersetzen[n] = true
	}
	mr, err := r.MultipartReader()
	if err != nil {
		fehler(w, http.StatusBadRequest, "Erwarte multipart/form-data")
		return
	}
	fertig, vorhanden := []string{}, []string{}
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
			abbruch(http.StatusBadRequest, "Übertragung abgebrochen")
			return
		}
		name := path.Base(strings.ReplaceAll(teil.FileName(), "\\", "/"))
		if teil.FormName() != "datei" || name == "" || name == "." || name == ".." || name == "/" || strings.ContainsRune(name, 0) {
			teil.Close()
			continue
		}
		err = hochladenEine(c, ziel, name, ersetzen[name], teil)
		teil.Close()
		switch {
		case err == nil:
			fertig = append(fertig, name)
		case errors.Is(err, errDateiVorhanden):
			vorhanden = append(vorhanden, name)
		case errors.Is(err, errUebertragungAbgebrochen):
			abbruch(http.StatusBadRequest, "Übertragung abgebrochen — «"+name+"» wurde nicht gespeichert")
			return
		default:
			code, text := sftpText(err)
			abbruch(code, "«"+name+"»: "+text)
			return
		}
	}
	if len(vorhanden) > 0 {
		text := "«" + vorhanden[0] + "» gibt es dort schon"
		if len(vorhanden) > 1 {
			text = "Schon vorhanden: «" + strings.Join(vorhanden, "», «") + "»"
		}
		jsonAntwort(w, http.StatusConflict, map[string]any{"fehler": text, "vorhanden": vorhanden, "hochgeladen": fertig})
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
// Pfad des eigentlichen Ziels. So ersetzt ein Hochladen wie frueher den Inhalt hinter der
// Verknuepfung und nicht die Verknuepfung selbst. Fehlt das Ziel, bleibt der Pfad stehen.
func linkAufloesen(c *sftp.Client, p string, fi os.FileInfo) (string, os.FileInfo) {
	for i := 0; i < 16 && fi.Mode()&os.ModeSymlink != 0; i++ {
		z, err := c.ReadLink(p)
		if err != nil {
			break
		}
		if !strings.HasPrefix(z, "/") {
			z = path.Join(path.Dir(p), z)
		}
		zfi, err := c.Lstat(z)
		if err != nil {
			break
		}
		p, fi = path.Clean(z), zfi
	}
	return p, fi
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

// hochladenEine schreibt eine Datei nach ordner/name: erst in «.name.<zufall>.sshapp-upload»,
// nach vollstaendiger Uebertragung umbenennen. Bei jedem Fehler wird die temporaere Datei
// entfernt, ein vorhandenes Original bleibt unberuehrt.
func hochladenEine(c *sftp.Client, ordner, name string, ersetzen bool, quelle io.Reader) error {
	dateiPfad := path.Join(ordner, name)
	ersetzt := false        // ein vorhandenes Ziel wird (mit Erlaubnis) ersetzt
	var vorlage os.FileInfo // Rechte und Besitzer, die die neue Datei uebernimmt
	fi, err := c.Lstat(dateiPfad)
	switch {
	case err == nil:
		if !ersetzen {
			return errDateiVorhanden
		}
		dateiPfad, fi = linkAufloesen(c, dateiPfad, fi)
		if fi.IsDir() {
			return &sftpMeldung{http.StatusConflict, "«" + name + "» ist dort ein Ordner und lässt sich nicht durch eine Datei ersetzen"}
		}
		ersetzt = true
		if fi.Mode().IsRegular() {
			vorlage = fi
		}
	case !errors.Is(err, os.ErrNotExist):
		return err
	}

	tmp := path.Join(path.Dir(dateiPfad), "."+kuerzen(path.Base(dateiPfad), 180)+"."+zufallsEndung()+".sshapp-upload")
	f, err := c.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return err
	}
	leser := &leseWaechter{r: quelle}
	// Gleichzeitige Schreibauftraege sind hier unbedenklich: Bei einem Fehler wird die
	// temporaere Datei ohnehin verworfen, Luecken darin spielen keine Rolle.
	_, err = f.ReadFromWithConcurrency(leser, 0)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if leser.err != nil {
		log.Printf("Hochladen %s abgebrochen: %v", dateiPfad, leser.err)
		err = errUebertragungAbgebrochen
	}
	if err == nil && vorlage != nil {
		// Rechte und (soweit erlaubt) Besitzer des Originals uebernehmen.
		if st, ok := vorlage.Sys().(*sftp.FileStat); ok {
			_ = c.Chown(tmp, int(st.UID), int(st.GID))
		}
		_ = c.Chmod(tmp, vorlage.Mode().Perm())
	}
	if err == nil {
		if ersetzt {
			err = ersetzendUmbenennen(c, tmp, dateiPfad)
		} else if _, e := c.Lstat(dateiPfad); e == nil {
			// Waehrend der Uebertragung angelegt: nicht still ueberschreiben.
			err = errDateiVorhanden
		} else {
			// SSH_FXP_RENAME ersetzt bei OpenSSH nie ein vorhandenes Ziel.
			err = c.Rename(tmp, dateiPfad)
		}
	}
	if err != nil {
		if rerr := c.Remove(tmp); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
			log.Printf("Temporäre Datei %s bleibt liegen: %v", tmp, rerr)
		}
		return err
	}
	return nil
}

// ersetzendUmbenennen ersetzt nach durch von: in einem Schritt per posix-rename; kennt der
// Host die Erweiterung nicht, erst das alte Ziel loeschen und dann umbenennen.
func ersetzendUmbenennen(c *sftp.Client, von, nach string) error {
	if _, ok := c.HasExtension(posixRename); ok {
		return c.PosixRename(von, nach)
	}
	if err := c.Remove(nach); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return c.Rename(von, nach)
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
		return &sftpMeldung{http.StatusBadRequest, "Ein Ordner lässt sich nicht in sich selbst verschieben"}
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
		return &sftpMeldung{http.StatusConflict, "«" + path.Base(nach) + "» gibt es dort schon"}
	}
	if ziel.IsDir() {
		return &sftpMeldung{http.StatusConflict, "«" + path.Base(nach) + "» ist ein Ordner und lässt sich nicht ersetzen"}
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
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := sauberPfad(a.Pfad)
	if err != nil || p == "/" {
		fehler(w, http.StatusBadRequest, "Ungültiger Pfad")
		return
	}
	switch a.Art {
	case "ordner":
		if _, e := c.Lstat(p); e == nil {
			fehler(w, http.StatusConflict, "«"+path.Base(p)+"» gibt es dort schon")
			return
		}
		err = c.Mkdir(p)
	case "umbenennen":
		nach, e := sauberPfad(a.Nach)
		if e != nil || nach == "/" {
			fehler(w, http.StatusBadRequest, "Ungültiger neuer Name")
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
		fehler(w, http.StatusBadRequest, "Unbekannte Aktion")
		return
	}
	if err != nil {
		sftpFehler(w, err)
		return
	}
	jsonAntwort(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Hilfe ---

func (s *Server) hilfeSuche(w http.ResponseWriter, r *http.Request) {
	jsonAntwort(w, http.StatusOK, s.hilfe.Suchen(r.URL.Query().Get("q"), 25))
}

func (s *Server) hilfeSeite(w http.ResponseWriter, r *http.Request) {
	name, _ := url.PathUnescape(r.PathValue("name"))
	seite, ok := s.hilfe.Seite(name)
	if !ok {
		fehler(w, http.StatusNotFound, "Keine Hilfe zu «"+name+"»")
		return
	}
	jsonAntwort(w, http.StatusOK, seite)
}

func (s *Server) hilfeErklaeren(w http.ResponseWriter, r *http.Request) {
	seite, teile, ok := s.hilfe.Erklaeren(r.URL.Query().Get("zeile"))
	if !ok {
		jsonAntwort(w, http.StatusOK, map[string]any{"gefunden": false})
		return
	}
	jsonAntwort(w, http.StatusOK, map[string]any{"gefunden": true, "seite": seite, "teile": teile})
}
