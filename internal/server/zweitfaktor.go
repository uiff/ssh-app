package server

// Zwei-Faktor-Anmeldung (optional, Vorgabe aus): Code aus einer Authenticator-App
// (TOTP, RFC 6238) oder ein Ersatzcode. Laeuft ganz ohne Netz.
//
// Das Geheimnis und die Ersatzcodes liegen mit dem Datenschluessel versiegelt im
// Tresor-Kopf (vault.Kopf.Zweitfaktor) und reisen so mit jedem Backup. Hier leben nur
// Dinge im Speicher: das Geheimnis einer laufenden Einrichtung (je Anmeldung, 30 min),
// der zuletzt angenommene Zeitschritt (Schutz vor Wiederverwendung eines Codes), die durch
// den Uhrzeit-Hinweis verbrannten kuenftigen Zeitschritte und die kontoweite Bremse fuer
// falsche Codes samt der Meldung an den Besitzer.

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"time"

	"rsc.io/qr"

	"github.com/uiff/ssh-app/internal/texte"
	"github.com/uiff/ssh-app/internal/totp"
	"github.com/uiff/ssh-app/internal/vault"
)

const (
	// So lange gilt das Geheimnis einer begonnenen Einrichtung (wie die Leerlauf-Vorgabe);
	// jedes weitere «Einrichten» derselben Anmeldung verlaengert die Frist.
	einrichtenFrist = 30 * time.Minute
	// Name der App in der Authenticator-App (Aussteller).
	aussteller = "Keyway"
	// Kontoweite Bremse: Nach je so vielen falschen zweiten Faktoren in Folge (richtiges
	// Passwort, alle Adressen zusammen, seit dem letzten richtigen zweiten Faktor, egal wie
	// langsam) nimmt der Server keine Codes aus der App mehr an, auch richtige nicht: zuerst
	// sperreDauer lang, jede weitere Sperre ohne richtigen Code dazwischen doppelt so lang,
	// hoechstens maxCodeSperre. Ersatzcodes (2^40 Moeglichkeiten) gelten weiter.
	maxCodeFehler = 10
	maxCodeSperre = 24 * time.Hour
	// Ab so vielen falschen zweiten Faktoren in Folge warnt das Log: Das Passwort ist bekannt.
	codeFehlerWarnen = 5
	// Ab so vielen falschen zweiten Faktoren seit der letzten Anmeldung nennt die naechste
	// Anmeldung ihre Zahl (codeFehler); ein einzelner Tippfehler des Besitzers bleibt still.
	codeFehlerMelden = 3
)

// ausstehend ist eine begonnene Einrichtung (Geheimnis noch nicht bestaetigt).
type ausstehend struct {
	geheimnis []byte
	bis       time.Time
}

// codeFehler: der zweite Faktor stimmt nicht. Zaehlt als Fehlversuch der Adresse; der Text
// (eine texte.Meldung) geht an den Browser. Fuer die kontoweite Bremse zaehlt nur
// errCodeFalsch (siehe codePruefen).
type codeFehler struct{ m *texte.Meldung }

func (c codeFehler) Error() string { return c.m.Error() }
func (c codeFehler) Unwrap() error { return c.m }

func istCodeFehler(err error) bool {
	var c codeFehler
	return errors.As(err, &c)
}

var errCodeFalsch = codeFehler{texte.Neu("zweitfaktor.code.falsch")}

// codeSperre: Codes aus der App sind fuer das Konto gesperrt (zu viele falsche zweite
// Faktoren). Antwort 429; zaehlt nicht als Fehlversuch der Adresse.
type codeSperre struct{ m *texte.Meldung }

func (c codeSperre) Error() string { return c.m.Error() }
func (c codeSperre) Unwrap() error { return c.m }

func istCodeSperre(err error) bool {
	var c codeSperre
	return errors.As(err, &c)
}

// verbrannterSchritt: ein kuenftiger Zeitschritt eines Geheimnisses (kennung = SHA-256 des
// Geheimnisses), dessen Code der Uhrzeit-Hinweis bestaetigt hat.
type verbrannterSchritt struct {
	kennung [sha256.Size]byte
	schritt int64
}

// jetzt liefert die Uhrzeit des Servers (Tests setzen s.uhr).
func (s *Server) jetzt() time.Time {
	if s.uhr != nil {
		return s.uhr()
	}
	return time.Now()
}

// codePruefen prueft eine Eingabe gegen einen Zweitfaktor: einen TOTP-Code (Fenster +-1
// Schritt, jeder Schritt nur einmal) oder, wenn ersatzErlaubt, einen Ersatzcode, der dabei
// aus z entfernt wird (verbraucht = true). neu: das Geheimnis wird gerade eingerichtet;
// sein erster Code ersetzt den Stand des Wiederverwendungs-Schutzes. Ohne neu zaehlt jeder
// falsche Code fuer die kontoweite Bremse (maxCodeFehler), und ein richtiger beendet die Folge.
func (s *Server) codePruefen(z *vault.Zweitfaktor, eingabe string, ersatzErlaubt, neu bool) (verbraucht bool, err error) {
	jetzt := s.jetzt()
	s.tfMu.Lock()
	defer s.tfMu.Unlock()
	verbraucht, err = s.codeVergleichen(z, eingabe, ersatzErlaubt, neu, jetzt)
	switch {
	case neu:
		// Einrichten: Anmeldung und Passwort sind bewiesen, das Geheimnis ist neu.
	case err == nil:
		// Eine laufende Sperre bleibt, die naechste beginnt wieder bei sperreDauer.
		s.tfFehlerFolge, s.tfSperren = 0, 0
	case errors.Is(err, errCodeFalsch):
		// Nur echte Fehlversuche: Ein Uhrzeit-Hinweis oder «schon verwendet» zeigt, dass der
		// Code zum Geheimnis passt (beim Raten etwa 8 zu 100'000), also meist den Besitzer.
		s.codeFehlerZaehlen(jetzt)
	}
	return verbraucht, err
}

// codeVergleichen ist die eigentliche Pruefung von codePruefen (tfMu ist gesperrt).
func (s *Server) codeVergleichen(z *vault.Zweitfaktor, eingabe string, ersatzErlaubt, neu bool, jetzt time.Time) (bool, error) {
	if code, ok := totp.NurZiffern(eingabe); ok {
		if !neu && jetzt.Before(s.tfSperreBis) {
			return false, codeSperre{texte.Neu("zweitfaktor.code.gesperrt", dauerText(s.tfSperreBis.Sub(jetzt)))}
		}
		kennung := sha256.Sum256(z.Geheimnis)
		if schritt, ok := totp.Pruefen(z.Geheimnis, code, jetzt, totp.Fenster); ok {
			if !neu && (schritt <= s.tfSchritt || s.verbrannt(kennung, schritt, jetzt)) {
				return false, codeFehler{texte.Neu("zweitfaktor.code.verwendet")}
			}
			s.tfSchritt = schritt
			return false, nil
		}
		if d, ok := totp.Abweichung(z.Geheimnis, code, jetzt, totp.Fenster, totp.Suchweite); ok {
			// Ein Code fuer eine spaetere Uhrzeit wuerde mit der Zeit gueltig: Wer das Passwort
			// kennt und raet, koennte einen so bestaetigten Code aufsparen. Darum gilt er nie mehr.
			// (Den Schritt direkt nach dem Fenster meldet Abweichung nicht: Sein Code gilt gleich
			// darauf von selbst und bleibt ohne Hinweis ein gewoehnlicher falscher Code.)
			if !neu && d > 0 {
				s.verbrennen(kennung, totp.Schritt(jetzt)+d, jetzt)
			}
			// Abstand in Minuten, gerundet (2 Schritte = 1 min). Beim Einrichten gibt es noch
			// keine Ersatzcodes: Text ohne diesen Rat.
			minuten := int((max(d, -d)*totp.Periode + 30) / 60)
			schluessel := "zweitfaktor.code.zeit."
			if !ersatzErlaubt {
				schluessel = "zweitfaktor.einrichten.zeit."
			}
			if d > 0 {
				return false, codeFehler{texte.Neu(schluessel+"spaeter", minuten)}
			}
			return false, codeFehler{texte.Neu(schluessel+"frueher", minuten)}
		}
		return false, errCodeFalsch
	}
	if ersatzErlaubt {
		if c := totp.ErsatzNormal(eingabe); totp.IstErsatzcode(c) {
			treffer := -1
			for i, e := range z.Ersatz {
				if subtle.ConstantTimeCompare([]byte(c), []byte(e)) == 1 {
					treffer = i
				}
			}
			if treffer >= 0 {
				z.Ersatz = slices.Delete(z.Ersatz, treffer, treffer+1)
				return true, nil
			}
		}
	}
	return false, errCodeFalsch
}

// verbrennen merkt einen kuenftigen Zeitschritt, dessen Code der Uhrzeit-Hinweis bestaetigt
// hat. Er bleibt gesperrt, bis er das Fenster verlassen hat (tfMu ist gesperrt). Der Hinweis
// hilft trotzdem: Nach dem Stellen der Uhr kommen andere Codes.
func (s *Server) verbrennen(kennung [sha256.Size]byte, schritt int64, jetzt time.Time) {
	s.verbranntAufraeumen(jetzt)
	s.tfVerbrannt[verbrannterSchritt{kennung, schritt}] = time.Unix((schritt+totp.Fenster+1)*totp.Periode, 0)
}

// verbrannt meldet, ob ein Zeitschritt verbrannt ist (tfMu ist gesperrt).
func (s *Server) verbrannt(kennung [sha256.Size]byte, schritt int64, jetzt time.Time) bool {
	bis, ok := s.tfVerbrannt[verbrannterSchritt{kennung, schritt}]
	return ok && jetzt.Before(bis)
}

// verbranntAufraeumen entfernt Schritte, die nicht mehr gelten koennen (tfMu ist gesperrt).
// Es sind nie viele: hoechstens totp.Suchweite je Geheimnis, und die Bremse unten begrenzt
// die Hinweise ohnehin.
func (s *Server) verbranntAufraeumen(jetzt time.Time) {
	for k, bis := range s.tfVerbrannt {
		if !jetzt.Before(bis) {
			delete(s.tfVerbrannt, k)
		}
	}
}

// codeFehlerZaehlen zaehlt einen falschen zweiten Faktor fuer das Konto, ueber alle Adressen
// (tfMu ist gesperrt). Die Sperre je Adresse allein reicht fuer einen 6-stelligen Code nicht:
// Wer das Passwort kennt, verteilt die Versuche sonst auf viele Adressen (RFC 4226 7.3). Die
// Folge zaehlt seit dem letzten richtigen zweiten Faktor und nicht in einem Zeitfenster, wie
// NIST SP 800-63B 5.2.2 es verlangt: Auch wer langsam raet (etwa 9 Versuche je Viertelstunde),
// stoesst an die Grenze, und jede weitere Sperre dauert doppelt so lang.
func (s *Server) codeFehlerZaehlen(jetzt time.Time) {
	s.tfFehlerFolge++
	s.tfFehlerMelden++
	n := s.tfFehlerFolge
	if n%maxCodeFehler != 0 {
		if n == codeFehlerWarnen {
			log.Printf("WARNUNG: %d falsche Codes der Zwei-Faktor-Anmeldung in Folge, jedes Mal mit richtigem Passwort. "+
				"Das Passwort ist offenbar bekannt — bitte ändern.", n)
		}
		return
	}
	dauer := codeSperreDauer(s.tfSperren)
	s.tfSperren++
	if bis := jetzt.Add(dauer); bis.After(s.tfSperreBis) {
		s.tfSperreBis = bis
	}
	s.tfAlarm = true
	log.Printf("WARNUNG: %d falsche Codes der Zwei-Faktor-Anmeldung in Folge, jedes Mal mit richtigem Passwort. "+
		"Codes aus der App sind %s gesperrt, Ersatzcodes gelten weiter. Das Passwort ist offenbar bekannt — bitte ändern.",
		n, dauer)
}

// codeSperreDauer liefert die Dauer der n-ten kontoweiten Sperre (ab 0) ohne richtigen zweiten
// Faktor dazwischen: sperreDauer, dann jeweils doppelt so lang, hoechstens maxCodeSperre.
func codeSperreDauer(n int) time.Duration {
	d := sperreDauer
	for ; n > 0 && d < maxCodeSperre; n-- {
		d *= 2
	}
	return min(d, maxCodeSperre)
}

// codeWarnungAbholen meldet einmal, was seit der letzten Anmeldung geschah: ob die kontoweite
// Sperre gegriffen hat und wie viele falsche zweite Faktoren es gab (fuer einen Hinweis an den
// Besitzer). Jede erfolgreiche Anmeldung holt es ab, auch ohne Zwei-Faktor-Anmeldung (sie kann
// inzwischen ausgeschaltet sein).
func (s *Server) codeWarnungAbholen() (alarm bool, fehler int) {
	s.tfMu.Lock()
	defer s.tfMu.Unlock()
	alarm, fehler = s.tfAlarm, s.tfFehlerMelden
	s.tfAlarm, s.tfFehlerMelden = false, 0
	return alarm, fehler
}

// codeBremseLeeren hebt die kontoweite Bremse auf: Sperre, Folge falscher Codes und Stufe. Nach
// einem neuen Geheimnis (Einschalten) galt sie dem alten. Nach einem neuen Passwort
// (Zuruecksetzen, Passwort aendern) kommt niemand mehr bis zum Code, der das alte kannte; dann
// entfaellt auch die Meldung an den Besitzer (passwortNeu), denn sie raet zu einem neuen
// Passwort. Den Schutz vor Wiederverwendung (tfSchritt) setzt der erste Code eines neuen
// Geheimnisses ohnehin neu, ein bleibendes Geheimnis behaelt ihn.
func (s *Server) codeBremseLeeren(passwortNeu bool) {
	s.tfMu.Lock()
	defer s.tfMu.Unlock()
	s.tfSperreBis, s.tfFehlerFolge, s.tfSperren = time.Time{}, 0, 0
	if passwortNeu {
		s.tfAlarm, s.tfFehlerMelden = false, 0
	}
}

// pruefer liefert den vault.Pruefer fuer eine Eingabe; ohne Eingabe nil (der Tresor
// meldet dann vault.ErrZweitfaktorFehlt). verbraucht meldet einen verwendeten Ersatzcode.
func (s *Server) pruefer(eingabe string, verbraucht *bool) vault.Pruefer {
	if strings.TrimSpace(eingabe) == "" {
		return nil
	}
	return func(z *vault.Zweitfaktor) (bool, error) {
		v, err := s.codePruefen(z, eingabe, true, false)
		if verbraucht != nil {
			*verbraucht = v
		}
		return v, err
	}
}

// kopfSpeichern legt einen geaenderten Tresor-Kopf ab (vault.Speicher).
func (s *Server) kopfSpeichern(kopf []byte) error {
	if err := s.st.SetMeta(MetaTresor, kopf); err != nil {
		log.Printf("Tresor-Kopf speichern: %v", err)
		return texte.Neu("server.speichern.fehlgeschlagen").MitUrsache(err)
	}
	return nil
}

// zweitfaktorAntwort antwortet auf einen gescheiterten zweiten Faktor (Anmeldung): 401,
// bei der kontoweiten Sperre 429.
func zweitfaktorAntwort(w http.ResponseWriter, r *http.Request, status int, err error) {
	jsonAntwort(w, status, map[string]any{
		"fehler":      texte.Uebersetzen(texte.AusAnfrage(r), err),
		"zweitfaktor": true,
	})
}

// --- Einrichtung im Speicher ---

func sitzungsToken(r *http.Request) string {
	if c, err := r.Cookie(cookieName); err == nil {
		return c.Value
	}
	return ""
}

// ausstehendBeginnen liefert das Geheimnis der laufenden Einrichtung dieser Anmeldung und
// verlaengert ihre Frist; gibt es keine (oder ist sie abgelaufen), legt es ein neues an. Ein
// zweites «Einrichten» (Dialog geschlossen, Seite neu geladen, Doppelklick) zeigt so
// denselben QR-Code, und ein schon gescannter Eintrag in der Authenticator-App bleibt gueltig.
// Ergebnis: eine Kopie.
func (s *Server) ausstehendBeginnen(token string) []byte {
	s.tfMu.Lock()
	defer s.tfMu.Unlock()
	jetzt := s.jetzt()
	a, ok := s.tfAusstehend[token]
	if !ok || !jetzt.Before(a.bis) {
		if ok {
			clear(a.geheimnis)
		}
		a = ausstehend{geheimnis: totp.NeuesGeheimnis()}
	}
	a.bis = jetzt.Add(einrichtenFrist)
	s.tfAusstehend[token] = a
	return slices.Clone(a.geheimnis)
}

// ausstehendHolen liefert eine Kopie des Geheimnisses einer laufenden Einrichtung (nil:
// keine oder abgelaufen).
func (s *Server) ausstehendHolen(token string) []byte {
	s.tfMu.Lock()
	defer s.tfMu.Unlock()
	a, ok := s.tfAusstehend[token]
	if !ok {
		return nil
	}
	if !s.jetzt().Before(a.bis) {
		clear(a.geheimnis)
		delete(s.tfAusstehend, token)
		return nil
	}
	return slices.Clone(a.geheimnis)
}

// ausstehendVergessen entfernt die Einrichtung einer Anmeldung ("" = alle).
func (s *Server) ausstehendVergessen(token string) {
	s.tfMu.Lock()
	defer s.tfMu.Unlock()
	for t, a := range s.tfAusstehend {
		if token == "" || t == token {
			clear(a.geheimnis)
			delete(s.tfAusstehend, t)
		}
	}
}

// ausstehendAufraeumen entfernt abgelaufene Einrichtungen und verbrannte Zeitschritte, die
// nicht mehr gelten koennen (Leerlaufwache).
func (s *Server) ausstehendAufraeumen() {
	s.tfMu.Lock()
	defer s.tfMu.Unlock()
	jetzt := s.jetzt()
	for t, a := range s.tfAusstehend {
		if !jetzt.Before(a.bis) {
			clear(a.geheimnis)
			delete(s.tfAusstehend, t)
		}
	}
	s.verbranntAufraeumen(jetzt)
}

// --- Handler ---

// zweitfaktorBeginnen liefert das Geheimnis einer Einrichtung fuer die Authenticator-App
// (Schluessel zum Abtippen, otpauth-Link, QR-Code) samt Uhrzeit des Servers. Ruft dieselbe
// Anmeldung es noch einmal auf, kommt dasselbe Geheimnis (ausstehendBeginnen).
func (s *Server) zweitfaktorBeginnen(w http.ResponseWriter, r *http.Request) {
	if s.tr.ZweitfaktorAktiv() {
		fehlerAus(w, r, http.StatusConflict, vault.ErrZweitfaktorAktiv)
		return
	}
	geheimnis := s.ausstehendBeginnen(sitzungsToken(r))
	defer clear(geheimnis)
	konto := s.tr.Benutzer()
	if h := kontoHost(r.Host); h != "" {
		konto += "@" + h
	}
	uri := totp.URI(aussteller, konto, geheimnis)
	bild, err := qrDatenURI(uri)
	if err != nil {
		log.Printf("QR-Code erzeugen: %v", err)
		fehlerT(w, r, http.StatusInternalServerError, "zweitfaktor.qr")
		return
	}
	schluessel := totp.Gruppiert(totp.Base32(geheimnis))
	jsonAntwort(w, http.StatusOK, map[string]any{
		"schluessel": schluessel,
		"uri":        uri,
		"qr":         bild,
		"serverzeit": s.jetzt().UnixMilli(),
	})
}

// codeAnfrage: aktuelles Passwort und Code (Einschalten, Ausschalten, Ersatzcodes).
type codeAnfrage struct {
	Passwort string `json:"passwort"`
	Code     string `json:"code"`
}

// codeAnfrageLesen liest und prueft die Pflichtfelder. Fehlt etwas, ist die Antwort
// geschrieben und das Ergebnis false (zaehlt nicht als Fehlversuch).
func codeAnfrageLesen(w http.ResponseWriter, r *http.Request) (codeAnfrage, bool) {
	var a codeAnfrage
	if err := lesen(r, &a); err != nil {
		fehlerAus(w, r, http.StatusBadRequest, err)
		return a, false
	}
	if a.Passwort == "" {
		fehlerT(w, r, http.StatusBadRequest, "zweitfaktor.passwort.fehlt")
		return a, false
	}
	if strings.TrimSpace(a.Code) == "" {
		fehlerAus(w, r, http.StatusBadRequest, vault.ErrZweitfaktorFehlt)
		return a, false
	}
	return a, true
}

// zweitfaktorFehler beantwortet einen Fehler von Einschalten, Ausschalten oder Ersatzcodes.
// Falsches Passwort und falscher Code zaehlen als Fehlversuch. Antwort 400, nicht 401:
// Die Oberflaeche deutet 401 auf angemeldeten Routen als abgelaufene Anmeldung. «feld»
// sagt der Oberflaeche, welches Feld falsch ist («passwort» oder «code»).
func (s *Server) zweitfaktorFehler(w http.ResponseWriter, r *http.Request, v *versuch, err error, was string) {
	switch {
	case errors.Is(err, vault.ErrPasswortFalsch) || istCodeFehler(err):
		v.falsch()
		feld, wort := "code", "Code"
		if errors.Is(err, vault.ErrPasswortFalsch) {
			feld, wort = "passwort", "Passwort"
		}
		log.Printf("%s mit falschem %s von %s", was, wort, s.clientIP(r))
		jsonAntwort(w, http.StatusBadRequest, map[string]any{"fehler": texte.Uebersetzen(texte.AusAnfrage(r), err), "feld": feld})
	case istCodeSperre(err):
		fehlerAus(w, r, http.StatusTooManyRequests, err)
	case errors.Is(err, vault.ErrZweitfaktorAktiv), errors.Is(err, vault.ErrZweitfaktorAus), errors.Is(err, vault.ErrKopfGeaendert):
		fehlerAus(w, r, http.StatusConflict, err)
	case errors.Is(err, vault.ErrZweitfaktorKaputt), errors.Is(err, vault.ErrNichtAngelegt):
		fehlerAus(w, r, http.StatusConflict, err)
	default:
		var m *texte.Meldung
		if !errors.As(err, &m) {
			log.Printf("%s: %v", was, err)
			err = texte.Neu("server.speichern.fehlgeschlagen")
		}
		fehlerAus(w, r, http.StatusInternalServerError, err)
	}
}

// zweitfaktorEinschalten prueft Passwort und den ersten Code der Authenticator-App und
// schaltet die Zwei-Faktor-Anmeldung ein. Antwort: die Ersatzcodes (nur dieses eine Mal).
func (s *Server) zweitfaktorEinschalten(w http.ResponseWriter, r *http.Request) {
	v, grund := s.versuchBeginnen(r)
	if v == nil {
		fehlerAus(w, r, http.StatusTooManyRequests, grund)
		return
	}
	defer v.abbrechen()
	a, ok := codeAnfrageLesen(w, r)
	if !ok {
		return
	}
	s.kopfMu.Lock()
	defer s.kopfMu.Unlock()
	if s.tr.ZweitfaktorAktiv() {
		fehlerAus(w, r, http.StatusConflict, vault.ErrZweitfaktorAktiv)
		return
	}
	token := sitzungsToken(r)
	geheimnis := s.ausstehendHolen(token)
	if geheimnis == nil {
		fehlerT(w, r, http.StatusConflict, "zweitfaktor.einrichten.abgelaufen")
		return
	}
	defer clear(geheimnis)
	ersatz := totp.NeueErsatzcodes(totp.ErsatzAnzahl)
	z := vault.Zweitfaktor{Geheimnis: geheimnis, Ersatz: ersatz, Seit: s.jetzt().UTC()}
	pruefen := func(z *vault.Zweitfaktor) (bool, error) {
		return s.codePruefen(z, a.Code, false, true)
	}
	var err error
	if e := s.mitArgon(r.Context(), func() { err = s.tr.ZweitfaktorEinschalten(a.Passwort, z, pruefen, s.kopfSpeichern) }); e != nil {
		argonFehler(w, r, e)
		return
	}
	if err != nil {
		s.zweitfaktorFehler(w, r, v, err, "Zwei-Faktor-Anmeldung einschalten")
		return
	}
	v.richtig()
	s.ausstehendVergessen(token)
	s.codeBremseLeeren(false) // eine Sperre galt dem alten Geheimnis
	log.Printf("Zwei-Faktor-Anmeldung eingeschaltet (von %s)", s.clientIP(r))
	jsonAntwort(w, http.StatusOK, map[string]any{"ersatzcodes": ersatzAnzeige(ersatz)})
}

// zweitfaktorAusschalten schaltet die Zwei-Faktor-Anmeldung mit Passwort und Code aus.
func (s *Server) zweitfaktorAusschalten(w http.ResponseWriter, r *http.Request) {
	v, grund := s.versuchBeginnen(r)
	if v == nil {
		fehlerAus(w, r, http.StatusTooManyRequests, grund)
		return
	}
	defer v.abbrechen()
	a, ok := codeAnfrageLesen(w, r)
	if !ok {
		return
	}
	s.kopfMu.Lock()
	defer s.kopfMu.Unlock()
	if !s.tr.ZweitfaktorAktiv() {
		fehlerAus(w, r, http.StatusConflict, vault.ErrZweitfaktorAus)
		return
	}
	var err error
	if e := s.mitArgon(r.Context(), func() { err = s.tr.ZweitfaktorAusschalten(a.Passwort, s.pruefer(a.Code, nil), s.kopfSpeichern) }); e != nil {
		argonFehler(w, r, e)
		return
	}
	if err != nil {
		s.zweitfaktorFehler(w, r, v, err, "Zwei-Faktor-Anmeldung ausschalten")
		return
	}
	v.richtig()
	log.Printf("Zwei-Faktor-Anmeldung ausgeschaltet (von %s)", s.clientIP(r))
	jsonAntwort(w, http.StatusOK, map[string]bool{"ok": true})
}

// ersatzcodesErneuern ersetzt alle Ersatzcodes (Passwort und Code noetig).
func (s *Server) ersatzcodesErneuern(w http.ResponseWriter, r *http.Request) {
	v, grund := s.versuchBeginnen(r)
	if v == nil {
		fehlerAus(w, r, http.StatusTooManyRequests, grund)
		return
	}
	defer v.abbrechen()
	a, ok := codeAnfrageLesen(w, r)
	if !ok {
		return
	}
	s.kopfMu.Lock()
	defer s.kopfMu.Unlock()
	if !s.tr.ZweitfaktorAktiv() {
		fehlerAus(w, r, http.StatusConflict, vault.ErrZweitfaktorAus)
		return
	}
	ersatz := totp.NeueErsatzcodes(totp.ErsatzAnzahl)
	var err error
	if e := s.mitArgon(r.Context(), func() { err = s.tr.ErsatzcodesErneuern(a.Passwort, ersatz, s.pruefer(a.Code, nil), s.kopfSpeichern) }); e != nil {
		argonFehler(w, r, e)
		return
	}
	if err != nil {
		s.zweitfaktorFehler(w, r, v, err, "Ersatzcodes erneuern")
		return
	}
	v.richtig()
	log.Printf("Neue Ersatzcodes für die Zwei-Faktor-Anmeldung erzeugt (von %s)", s.clientIP(r))
	jsonAntwort(w, http.StatusOK, map[string]any{"ersatzcodes": ersatzAnzeige(ersatz)})
}

func ersatzAnzeige(codes []string) []string {
	out := make([]string, len(codes))
	for i, c := range codes {
		out[i] = totp.ErsatzAnzeige(c)
	}
	return out
}

// kontoHost liefert den Rechnernamen fuer die Bezeichnung in der Authenticator-App
// (ohne Port, nur harmlose Zeichen, IPv6 in Klammern).
func kontoHost(host string) string {
	h := hostOhnePort(host)
	h = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == ':' {
			return r
		}
		return -1
	}, h)
	if len(h) > 64 {
		h = h[:64]
	}
	if strings.Contains(h, ":") {
		if a, err := netip.ParseAddr(h); err == nil && a.Is6() {
			return "[" + h + "]"
		}
		return strings.ReplaceAll(h, ":", "")
	}
	return h
}

// --- QR-Code ---

// qrRand: Ruhezone um den Code in Modulen (vom QR-Standard verlangt).
const qrRand = 4

// qrDatenURI kodiert text als QR-Code (Fehlerkorrektur M) und liefert ein SVG als
// data:-URI (die CSP erlaubt img-src data:). Schwarz auf Weiss, auch im dunklen Thema.
func qrDatenURI(text string) (string, error) {
	c, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	n := c.Size + 2*qrRand
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="200" height="200" shape-rendering="crispEdges">`, n, n)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, n, n)
	for y := 0; y < c.Size; y++ {
		for x := 0; x < c.Size; {
			if !c.Black(x, y) {
				x++
				continue
			}
			anfang := x
			for x < c.Size && c.Black(x, y) {
				x++
			}
			fmt.Fprintf(&b, "M%d %dh%dv1h-%dz", anfang+qrRand, y+qrRand, x-anfang, x-anfang)
		}
	}
	b.WriteString(`"/></svg>`)
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(b.String())), nil
}
