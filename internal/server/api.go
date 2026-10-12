package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/uiff/ssh-app/internal/sshx"
	"github.com/uiff/ssh-app/internal/store"
	"github.com/uiff/ssh-app/internal/vault"
)

func pfadID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

func speicherFehler(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNichtGefunden) {
		fehler(w, http.StatusNotFound, "Eintrag nicht gefunden")
		return
	}
	if errors.Is(err, vault.ErrGesperrt) {
		fehler(w, http.StatusUnauthorized, "Bitte anmelden")
		return
	}
	fehler(w, http.StatusInternalServerError, err.Error())
}

// --- Bestand ---

func (s *Server) daten(w http.ResponseWriter, r *http.Request) {
	ordner, err := s.st.Ordner()
	if err != nil {
		speicherFehler(w, err)
		return
	}
	hosts, err := s.st.Hosts()
	if err != nil {
		speicherFehler(w, err)
		return
	}
	zugaenge, err := s.st.Zugaenge()
	if err != nil {
		speicherFehler(w, err)
		return
	}
	nutzung, _ := s.st.ZugangNutzung()
	type zugangAus struct {
		store.Zugang
		Hosts int `json:"hosts"`
	}
	zs := make([]zugangAus, 0, len(zugaenge))
	for _, z := range zugaenge {
		zs = append(zs, zugangAus{Zugang: z, Hosts: nutzung[z.ID]})
	}
	snippets, err := s.st.Snippets()
	if err != nil {
		speicherFehler(w, err)
		return
	}
	bekannt, err := s.st.BekannteHosts()
	if err != nil {
		speicherFehler(w, err)
		return
	}
	jsonAntwort(w, http.StatusOK, map[string]any{
		"ordner": ordner, "hosts": hosts, "zugaenge": zs, "snippets": snippets, "bekannt": bekannt,
	})
}

func (s *Server) ordnerSpeichern(w http.ResponseWriter, r *http.Request) {
	var o store.Ordner
	if err := lesen(r, &o); err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	o.ID = pfadID(r)
	o.Name = strings.TrimSpace(o.Name)
	if o.Name == "" || len(o.Name) > 60 {
		fehler(w, http.StatusBadRequest, "Ordnername: 1 bis 60 Zeichen")
		return
	}
	if err := s.st.OrdnerSpeichern(&o); err != nil {
		speicherFehler(w, err)
		return
	}
	jsonAntwort(w, http.StatusOK, o)
}

func (s *Server) ordnerLoeschen(w http.ResponseWriter, r *http.Request) {
	if err := s.st.OrdnerLoeschen(pfadID(r)); err != nil {
		speicherFehler(w, err)
		return
	}
	jsonAntwort(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) hostSpeichern(w http.ResponseWriter, r *http.Request) {
	var h store.Host
	if err := lesen(r, &h); err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	h.ID = pfadID(r)
	h.Name, h.Adresse, h.Benutzer = strings.TrimSpace(h.Name), strings.TrimSpace(h.Adresse), strings.TrimSpace(h.Benutzer)
	if h.Adresse == "" || strings.ContainsAny(h.Adresse, " /@") {
		fehler(w, http.StatusBadRequest, "Adresse fehlt oder enthält Leerzeichen, / oder @")
		return
	}
	if h.Name == "" {
		h.Name = h.Adresse
	}
	if h.Port < 0 || h.Port > 65535 {
		fehler(w, http.StatusBadRequest, "Port muss zwischen 1 und 65535 liegen")
		return
	}
	if h.ID != 0 && h.JumpID == h.ID {
		fehler(w, http.StatusBadRequest, "Ein Host kann nicht sein eigener Jump-Host sein")
		return
	}
	if err := s.st.HostSpeichern(&h); err != nil {
		speicherFehler(w, err)
		return
	}
	jsonAntwort(w, http.StatusOK, h)
}

func (s *Server) hostLoeschen(w http.ResponseWriter, r *http.Request) {
	if err := s.st.HostLoeschen(pfadID(r)); err != nil {
		speicherFehler(w, err)
		return
	}
	jsonAntwort(w, http.StatusOK, map[string]bool{"ok": true})
}

// zugangEingabe: leere Geheimnis-Felder bedeuten «unveraendert», loeschen nur mit Flag.
type zugangEingabe struct {
	Name                string `json:"name"`
	Benutzer            string `json:"benutzer"`
	Passwort            string `json:"passwort"`
	PasswortEntfernen   bool   `json:"passwortEntfernen"`
	Schluessel          string `json:"schluessel"`
	SchluesselEntfernen bool   `json:"schluesselEntfernen"`
	Passphrase          string `json:"passphrase"`
}

func (s *Server) zugangSpeichern(w http.ResponseWriter, r *http.Request) {
	var e zugangEingabe
	if err := lesen(r, &e); err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	z := store.Zugang{}
	if id := pfadID(r); id != 0 {
		var err error
		if z, err = s.st.Zugang(id); err != nil {
			speicherFehler(w, err)
			return
		}
	}
	z.Name, z.Benutzer = strings.TrimSpace(e.Name), strings.TrimSpace(e.Benutzer)
	if z.Name == "" {
		z.Name = z.Benutzer
	}
	if z.Name == "" {
		fehler(w, http.StatusBadRequest, "Name oder Benutzer angeben")
		return
	}
	var err error
	if e.PasswortEntfernen {
		z.PasswortEnc = nil
	}
	if e.Passwort != "" {
		if z.PasswortEnc, err = s.tr.Verschluesseln([]byte(e.Passwort)); err != nil {
			speicherFehler(w, err)
			return
		}
	}
	if e.SchluesselEntfernen {
		z.SchluesselEnc, z.PassphraseEnc, z.Oeffentlich, z.Fingerprint, z.SchluesselTyp = nil, nil, "", "", ""
	}
	if k := strings.TrimSpace(e.Schluessel); k != "" {
		typ, pub, fp, err := sshx.SchluesselInfo([]byte(k+"\n"), e.Passphrase, z.Name)
		if err != nil {
			fehler(w, http.StatusBadRequest, err.Error())
			return
		}
		if z.SchluesselEnc, err = s.tr.Verschluesseln([]byte(k + "\n")); err != nil {
			speicherFehler(w, err)
			return
		}
		z.PassphraseEnc = nil
		if e.Passphrase != "" {
			if z.PassphraseEnc, err = s.tr.Verschluesseln([]byte(e.Passphrase)); err != nil {
				speicherFehler(w, err)
				return
			}
		}
		z.SchluesselTyp, z.Oeffentlich, z.Fingerprint = typ, pub, fp
	}
	z.Art = "passwort"
	if len(z.SchluesselEnc) > 0 {
		z.Art = "schluessel"
	}
	if err := s.st.ZugangSpeichern(&z); err != nil {
		speicherFehler(w, err)
		return
	}
	jsonAntwort(w, http.StatusOK, z)
}

func (s *Server) zugangErzeugen(w http.ResponseWriter, r *http.Request) {
	var e zugangEingabe
	if err := lesen(r, &e); err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	z := store.Zugang{Name: strings.TrimSpace(e.Name), Benutzer: strings.TrimSpace(e.Benutzer), Art: "schluessel"}
	if z.Name == "" {
		fehler(w, http.StatusBadRequest, "Name angeben")
		return
	}
	priv, err := sshx.NeuerSchluessel(z.Name)
	if err != nil {
		speicherFehler(w, err)
		return
	}
	if z.SchluesselTyp, z.Oeffentlich, z.Fingerprint, err = sshx.SchluesselInfo(priv, "", z.Name); err != nil {
		speicherFehler(w, err)
		return
	}
	if z.SchluesselEnc, err = s.tr.Verschluesseln(priv); err != nil {
		speicherFehler(w, err)
		return
	}
	if err := s.st.ZugangSpeichern(&z); err != nil {
		speicherFehler(w, err)
		return
	}
	jsonAntwort(w, http.StatusOK, z)
}

// zugangPrivat liefert den privaten Schluessel als Datei (Export).
func (s *Server) zugangPrivat(w http.ResponseWriter, r *http.Request) {
	z, err := s.st.Zugang(pfadID(r))
	if err != nil {
		speicherFehler(w, err)
		return
	}
	klar, err := s.tr.Entschluesseln(z.SchluesselEnc)
	if err != nil || len(klar) == 0 {
		fehler(w, http.StatusNotFound, "Kein privater Schlüssel hinterlegt")
		return
	}
	log.Printf("Privater Schlüssel «%s» exportiert", z.Name)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(dateiname(z.Name)))
	w.Write(klar)
}

func dateiname(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, s)
	if s == "" {
		return "schluessel"
	}
	return s
}

func (s *Server) zugangLoeschen(w http.ResponseWriter, r *http.Request) {
	if err := s.st.ZugangLoeschen(pfadID(r)); err != nil {
		speicherFehler(w, err)
		return
	}
	jsonAntwort(w, http.StatusOK, map[string]bool{"ok": true})
}

// zugangInstallieren traegt den oeffentlichen Schluessel in authorized_keys eines Hosts ein.
// Angemeldet wird mit dem Zugang des Hosts; der Host-Schluessel muss schon bekannt sein.
func (s *Server) zugangInstallieren(w http.ResponseWriter, r *http.Request) {
	var p struct {
		HostID   int64  `json:"hostId"`
		Passwort string `json:"passwort"`
	}
	if err := lesen(r, &p); err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	z, err := s.st.Zugang(pfadID(r))
	if err != nil {
		speicherFehler(w, err)
		return
	}
	if z.Oeffentlich == "" {
		fehler(w, http.StatusBadRequest, "Dieser Zugang hat keinen Schlüssel")
		return
	}
	befehl, err := sshx.AuthorizedKeysBefehl(z.Oeffentlich)
	if err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	h, err := s.st.Host(p.HostID)
	if err != nil {
		speicherFehler(w, err)
		return
	}
	ziel, jump, err := s.zielFuer(h, &sshx.Anmeldung{Passwort: p.Passwort})
	if err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	client, schliessen, err := sshx.Verbinden(ctx, ziel, jump, s.nurBekannte)
	if err != nil {
		fehler(w, http.StatusBadGateway, err.Error())
		return
	}
	defer schliessen()
	out, err := sshx.Ausfuehren(client, befehl)
	if err != nil || !strings.Contains(out, "SSHAPP-OK") {
		fehler(w, http.StatusBadGateway, "Eintragen fehlgeschlagen: "+strings.TrimSpace(out))
		return
	}
	log.Printf("Schlüssel «%s» auf %s eingetragen", z.Name, h.Name)
	jsonAntwort(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) snippetSpeichern(w http.ResponseWriter, r *http.Request) {
	var sn store.Snippet
	if err := lesen(r, &sn); err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	sn.ID = pfadID(r)
	sn.Name = strings.TrimSpace(sn.Name)
	if sn.Name == "" || strings.TrimSpace(sn.Befehl) == "" {
		fehler(w, http.StatusBadRequest, "Name und Befehl angeben")
		return
	}
	if err := s.st.SnippetSpeichern(&sn); err != nil {
		speicherFehler(w, err)
		return
	}
	jsonAntwort(w, http.StatusOK, sn)
}

func (s *Server) snippetLoeschen(w http.ResponseWriter, r *http.Request) {
	if err := s.st.SnippetLoeschen(pfadID(r)); err != nil {
		speicherFehler(w, err)
		return
	}
	jsonAntwort(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) bekanntLoeschen(w http.ResponseWriter, r *http.Request) {
	if err := s.st.BekanntLoeschen(r.PathValue("ziel")); err != nil {
		speicherFehler(w, err)
		return
	}
	jsonAntwort(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Sicherung ---

type sicherungsDatei struct {
	Format   string    `json:"format"`
	Version  int       `json:"version"`
	Erstellt time.Time `json:"erstellt"`
	Hinweis  string    `json:"hinweis"`
	Daten    []byte    `json:"daten"`
}

const dateiFormat = "ssh-app-backup"

func (s *Server) sicherungLaden(w http.ResponseWriter, r *http.Request) {
	sg, err := s.st.Exportieren(s.tr.KopfJSON(), "ssh-app "+s.opt.Version)
	if err != nil {
		speicherFehler(w, err)
		return
	}
	klar, _ := json.Marshal(sg)
	geheim, err := s.tr.BackupVersiegeln(klar)
	if err != nil {
		speicherFehler(w, err)
		return
	}
	d := sicherungsDatei{Format: dateiFormat, Version: 1, Erstellt: sg.Erstellt,
		Hinweis: "Verschlüsselt. Einspielen nur mit dem Sicherheitsschlüssel.", Daten: geheim}
	name := "ssh-app-backup-" + time.Now().Format("2006-01-02-1504") + ".json"
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	json.NewEncoder(w).Encode(d)
}

// sicherungLesen entschluesselt eine hochgeladene Sicherung.
func sicherungLesen(r *http.Request) (store.Sicherung, string, error) {
	var sg store.Sicherung
	r.Body = http.MaxBytesReader(nil, r.Body, 64<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		return sg, "", errors.New("Datei fehlt oder ist zu gross")
	}
	schluessel := r.FormValue("schluessel")
	f, _, err := r.FormFile("datei")
	if err != nil {
		return sg, "", errors.New("Datei fehlt")
	}
	defer f.Close()
	roh, err := io.ReadAll(f)
	if err != nil {
		return sg, "", err
	}
	var d sicherungsDatei
	if err := json.Unmarshal(roh, &d); err != nil || d.Format != dateiFormat {
		return sg, "", errors.New("Die Datei ist kein Backup dieser App")
	}
	klar, err := vault.BackupOeffnen(schluessel, d.Daten)
	if err != nil {
		return sg, "", err
	}
	if err := json.NewDecoder(bytes.NewReader(klar)).Decode(&sg); err != nil {
		return sg, "", fmt.Errorf("Backup-Inhalt lesen: %w", err)
	}
	if err := sg.Pruefen(); err != nil {
		return sg, "", err
	}
	return sg, schluessel, nil
}

func (s *Server) einspielen(w http.ResponseWriter, r *http.Request) {
	if g, rest := s.gesperrt(r); g {
		fehler(w, http.StatusTooManyRequests, "Zu viele Fehlversuche — noch "+rest.String()+" gesperrt")
		return
	}
	sg, schluessel, err := sicherungLesen(r)
	if errors.Is(err, vault.ErrSchluesselFalsch) {
		s.fehlversuch(r)
	}
	if err != nil {
		fehler(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.st.Einspielen(sg, MetaTresor); err != nil {
		fehler(w, http.StatusInternalServerError, "Einspielen fehlgeschlagen: "+err.Error())
		return
	}
	if err := s.tr.Uebernehmen(sg.TresorKopf, schluessel); err != nil {
		fehler(w, http.StatusInternalServerError, "Tresor übernehmen: "+err.Error())
		return
	}
	s.erfolg(r)
	s.alleAbmelden()
	log.Printf("Backup vom %s eingespielt (%d Hosts)", sg.Erstellt.Format(time.DateTime), len(sg.Hosts))
	jsonAntwort(w, http.StatusOK, map[string]any{"ok": true, "hosts": len(sg.Hosts), "benutzer": s.tr.Benutzer()})
}

// einspielenOffen: nur auf einem frischen System ohne Einrichtung.
func (s *Server) einspielenOffen(w http.ResponseWriter, r *http.Request) {
	if s.tr.Angelegt() {
		fehler(w, http.StatusConflict, "Die App ist schon eingerichtet — Backup nach der Anmeldung unter Einstellungen einspielen")
		return
	}
	s.einspielen(w, r)
}

func (s *Server) einspielenAngemeldet(w http.ResponseWriter, r *http.Request) { s.einspielen(w, r) }
