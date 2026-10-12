package server

import (
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

func sftpFehler(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, os.ErrNotExist):
		fehler(w, http.StatusNotFound, "Gibt es nicht")
	case errors.Is(err, os.ErrPermission):
		fehler(w, http.StatusForbidden, "Keine Berechtigung")
	default:
		fehler(w, http.StatusBadGateway, err.Error())
	}
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
	f, err := c.Open(p)
	if err != nil {
		sftpFehler(w, err)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		sftpFehler(w, err)
		return
	}
	if fi.IsDir() {
		fehler(w, http.StatusBadRequest, "Ordner lassen sich nicht herunterladen")
		return
	}
	name := path.Base(p)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
	if _, err := io.Copy(w, f); err != nil {
		log.Printf("Download %s abgebrochen: %v", p, err)
	}
}

// dateiHochladen streamt die Datei direkt per SFTP, ohne Zwischenspeicher auf dem Server.
func (s *Server) dateiHochladen(w http.ResponseWriter, r *http.Request) {
	_, c, ok := s.sftpFuer(w, r)
	if !ok {
		return
	}
	ziel, err := sauberPfad(r.URL.Query().Get("pfad"))
	if err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	mr, err := r.MultipartReader()
	if err != nil {
		fehler(w, http.StatusBadRequest, "Erwarte multipart/form-data")
		return
	}
	ueberschreiben := r.URL.Query().Get("ueberschreiben") == "1"
	var fertig []string
	for {
		teil, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			fehler(w, http.StatusBadRequest, err.Error())
			return
		}
		name := path.Base(strings.ReplaceAll(teil.FileName(), "\\", "/"))
		if teil.FormName() != "datei" || name == "" || name == "." || name == "/" {
			teil.Close()
			continue
		}
		dateiPfad := path.Join(ziel, name)
		flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		if !ueberschreiben {
			flags |= os.O_EXCL
		}
		f, err := c.OpenFile(dateiPfad, flags)
		if err != nil {
			teil.Close()
			if errors.Is(err, os.ErrExist) || strings.Contains(err.Error(), "exist") {
				fehler(w, http.StatusConflict, "«"+name+"» gibt es dort schon")
				return
			}
			sftpFehler(w, err)
			return
		}
		_, err = f.ReadFrom(teil)
		f.Close()
		teil.Close()
		if err != nil {
			c.Remove(dateiPfad)
			sftpFehler(w, err)
			return
		}
		fertig = append(fertig, name)
	}
	jsonAntwort(w, http.StatusOK, map[string]any{"hochgeladen": fertig})
}

func (s *Server) dateiAktion(w http.ResponseWriter, r *http.Request) {
	_, c, ok := s.sftpFuer(w, r)
	if !ok {
		return
	}
	var a struct {
		Art  string `json:"art"` // ordner | umbenennen | loeschen
		Pfad string `json:"pfad"`
		Nach string `json:"nach"`
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
		err = c.Mkdir(p)
	case "umbenennen":
		var nach string
		if nach, err = sauberPfad(a.Nach); err == nil {
			err = c.PosixRename(p, nach)
			if err != nil {
				err = c.Rename(p, nach)
			}
		}
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
