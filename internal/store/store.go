// Package store haelt Ordner, Hosts, Zugaenge, bekannte Host-Schluessel und
// Snippets in einer SQLite-Datei. Geheimnisse kommen bereits verschluesselt an.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/uiff/ssh-app/internal/texte"
)

// ErrNichtGefunden: der Eintrag existiert nicht (Vergleich mit errors.Is).
var ErrNichtGefunden error = texte.Neu("store.nicht.gefunden")

type Ordner struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Rang int    `json:"rang"`
}

type Host struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Adresse    string   `json:"adresse"`
	Port       int      `json:"port"`
	Benutzer   string   `json:"benutzer"` // ueberschreibt den Benutzer des Zugangs, wenn gesetzt
	OrdnerID   int64    `json:"ordnerId"`
	ZugangID   int64    `json:"zugangId"`
	Farbe      string   `json:"farbe"`
	Tags       []string `json:"tags"`
	PfadFolgen bool     `json:"pfadFolgen"`
	Notiz      string   `json:"notiz"`
	Geraet     string   `json:"geraet"` // Geraetetyp (ids aus web/static/js/geraete.js, '' = Allgemein)
}

// Zugang = Benutzername plus Passwort und/oder privater Schluessel.
type Zugang struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Benutzer      string `json:"benutzer"`
	Art           string `json:"art"` // "passwort" | "schluessel"
	Oeffentlich   string `json:"oeffentlich,omitempty"`
	Fingerprint   string `json:"fingerprint,omitempty"`
	SchluesselTyp string `json:"schluesselTyp,omitempty"`
	HatPasswort   bool   `json:"hatPasswort"`
	HatPassphrase bool   `json:"hatPassphrase"`
	// Verschluesselt, nie in JSON.
	PasswortEnc   []byte `json:"-"`
	SchluesselEnc []byte `json:"-"`
	PassphraseEnc []byte `json:"-"`
}

type BekannterHost struct {
	Ziel        string    `json:"ziel"` // host:port
	Typ         string    `json:"typ"`
	Schluessel  string    `json:"schluessel"` // base64 wire format
	Fingerprint string    `json:"fingerprint"`
	Seit        time.Time `json:"seit"`
}

type Snippet struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Befehl       string `json:"befehl"`
	Beschreibung string `json:"beschreibung"`
}

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS meta (k TEXT PRIMARY KEY, v BLOB);
CREATE TABLE IF NOT EXISTS ordner (id INTEGER PRIMARY KEY, name TEXT NOT NULL, rang INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS zugang (
  id INTEGER PRIMARY KEY, name TEXT NOT NULL, benutzer TEXT NOT NULL DEFAULT '', art TEXT NOT NULL,
  oeffentlich TEXT NOT NULL DEFAULT '', fingerprint TEXT NOT NULL DEFAULT '', schluessel_typ TEXT NOT NULL DEFAULT '',
  passwort_enc BLOB, schluessel_enc BLOB, passphrase_enc BLOB);
CREATE TABLE IF NOT EXISTS host (
  id INTEGER PRIMARY KEY, name TEXT NOT NULL, adresse TEXT NOT NULL, port INTEGER NOT NULL DEFAULT 22,
  benutzer TEXT NOT NULL DEFAULT '', ordner_id INTEGER NOT NULL DEFAULT 0, zugang_id INTEGER NOT NULL DEFAULT 0,
  farbe TEXT NOT NULL DEFAULT '', tags TEXT NOT NULL DEFAULT '',
  pfad_folgen INTEGER NOT NULL DEFAULT 1, notiz TEXT NOT NULL DEFAULT '', geraet TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS bekannt (ziel TEXT PRIMARY KEY, typ TEXT NOT NULL, schluessel TEXT NOT NULL, fingerprint TEXT NOT NULL, seit INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS snippet (id INTEGER PRIMARY KEY, name TEXT NOT NULL, befehl TEXT NOT NULL, beschreibung TEXT NOT NULL DEFAULT '');
`

// Spalten, die spaeter dazukamen: bei aelteren Datenbanken nachruesten. Aeltere
// Datenbanken haben in host noch die Spalte jump_id; sie bleibt unbenutzt stehen.
var nachruesten = []struct{ tabelle, spalte, def string }{
	{"host", "geraet", `TEXT NOT NULL DEFAULT ''`},
}

func Oeffnen(pfad string) (*Store, error) {
	if err := dateiSchuetzen(pfad); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+pfad+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("Schema anlegen: %w", err)
	}
	for _, n := range nachruesten {
		if err := spalteNachruesten(db, n.tabelle, n.spalte, n.def); err != nil {
			db.Close()
			return nil, fmt.Errorf("Datenbank nachrüsten (%s.%s): %w", n.tabelle, n.spalte, err)
		}
	}
	return &Store{db: db}, nil
}

// dateiSchuetzen legt die Datenbank nur fuer den eigenen Benutzer lesbar an (0600)
// und zieht die Rechte vorhandener Dateien nach. SQLite uebernimmt die Rechte der
// Datenbank fuer -wal und -shm.
func dateiSchuetzen(pfad string) error {
	f, err := os.OpenFile(pfad, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("Datenbankdatei %s: %w", pfad, err)
	}
	f.Close()
	for _, endung := range []string{"", "-wal", "-shm"} {
		if fi, err := os.Stat(pfad + endung); err == nil && fi.Mode().Perm()&0o077 != 0 {
			if err := os.Chmod(pfad+endung, 0o600); err != nil {
				return fmt.Errorf("Rechte von %s: %w", pfad+endung, err)
			}
		}
	}
	return nil
}

func spalteNachruesten(db *sql.DB, tabelle, spalte, def string) error {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, tabelle)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		if name == spalte {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	_, err = db.Exec(`ALTER TABLE ` + tabelle + ` ADD COLUMN ` + spalte + ` ` + def)
	return err
}

func (s *Store) Close() error { return s.db.Close() }

// --- meta ---

func (s *Store) Meta(k string) ([]byte, error) {
	var v []byte
	err := s.db.QueryRow(`SELECT v FROM meta WHERE k=?`, k).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (s *Store) SetMeta(k string, v []byte) error {
	_, err := s.db.Exec(`INSERT INTO meta(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, k, v)
	return err
}

// --- Ordner ---

func (s *Store) Ordner() ([]Ordner, error) {
	rows, err := s.db.Query(`SELECT id,name,rang FROM ordner ORDER BY rang,name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Ordner{}
	for rows.Next() {
		var o Ordner
		if err := rows.Scan(&o.ID, &o.Name, &o.Rang); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) OrdnerSpeichern(o *Ordner) error {
	if o.ID == 0 {
		r, err := s.db.Exec(`INSERT INTO ordner(name,rang) VALUES(?,?)`, o.Name, o.Rang)
		if err != nil {
			return err
		}
		o.ID, _ = r.LastInsertId()
		return nil
	}
	return mussTreffen(s.db.Exec(`UPDATE ordner SET name=?,rang=? WHERE id=?`, o.Name, o.Rang, o.ID))
}

// OrdnerLoeschen haengt die Hosts des Ordners aus (sie landen in «Ohne Ordner»).
func (s *Store) OrdnerLoeschen(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE host SET ordner_id=0 WHERE ordner_id=?`, id); err != nil {
		return err
	}
	if err := mussTreffen(tx.Exec(`DELETE FROM ordner WHERE id=?`, id)); err != nil {
		return err
	}
	return tx.Commit()
}

// --- Hosts ---

const hostSpalten = `id,name,adresse,port,benutzer,ordner_id,zugang_id,farbe,tags,pfad_folgen,notiz,geraet`

func scanHost(sc interface{ Scan(...any) error }) (Host, error) {
	var h Host
	var tags string
	var folgen int
	err := sc.Scan(&h.ID, &h.Name, &h.Adresse, &h.Port, &h.Benutzer, &h.OrdnerID, &h.ZugangID, &h.Farbe, &tags, &folgen, &h.Notiz, &h.Geraet)
	h.Tags = splitTags(tags)
	h.PfadFolgen = folgen != 0
	return h, err
}

func (s *Store) Hosts() ([]Host, error) {
	rows, err := s.db.Query(`SELECT ` + hostSpalten + ` FROM host ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Host{}
	for rows.Next() {
		h, err := scanHost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *Store) Host(id int64) (Host, error) {
	h, err := scanHost(s.db.QueryRow(`SELECT `+hostSpalten+` FROM host WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return h, ErrNichtGefunden
	}
	return h, err
}

func (s *Store) HostSpeichern(h *Host) error {
	if h.Port == 0 {
		h.Port = 22
	}
	args := []any{h.Name, h.Adresse, h.Port, h.Benutzer, h.OrdnerID, h.ZugangID, h.Farbe, joinTags(h.Tags), b2i(h.PfadFolgen), h.Notiz, h.Geraet}
	if h.ID == 0 {
		r, err := s.db.Exec(`INSERT INTO host(name,adresse,port,benutzer,ordner_id,zugang_id,farbe,tags,pfad_folgen,notiz,geraet) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, args...)
		if err != nil {
			return err
		}
		h.ID, _ = r.LastInsertId()
		return nil
	}
	return mussTreffen(s.db.Exec(`UPDATE host SET name=?,adresse=?,port=?,benutzer=?,ordner_id=?,zugang_id=?,farbe=?,tags=?,pfad_folgen=?,notiz=?,geraet=? WHERE id=?`, append(args, h.ID)...))
}

// HostZugangSetzen speichert beim Verbinden erfragte Zugangsdaten am Host (eine
// Transaktion). Hat der Host schon einen eigenen Passwort-Zugang (nur er benutzt ihn),
// bekommt dieser Benutzer und Passwort: wiederholtes Speichern legt keinen Doppelgaenger
// an. Sonst entsteht ein neuer Zugang, der an den Host gehaengt wird. Ein Benutzer im
// bestehenden Zugang bleibt; z.Benutzer gilt dort nur, wenn er leer war. aktualisiert
// meldet, ob ein vorhandener Zugang ergaenzt wurde; z traegt danach dessen Stand.
func (s *Store) HostZugangSetzen(hostID int64, z *Zugang) (aktualisiert bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var eigen int64
	err = tx.QueryRow(`SELECT z.id FROM host h JOIN zugang z ON z.id=h.zugang_id
WHERE h.id=? AND z.art='passwort' AND (SELECT COUNT(*) FROM host WHERE zugang_id=z.id)=1`, hostID).Scan(&eigen)
	switch {
	case err == nil:
		if err := mussTreffen(tx.Exec(`UPDATE zugang SET benutzer=CASE WHEN benutzer='' THEN ? ELSE benutzer END, passwort_enc=? WHERE id=?`,
			z.Benutzer, z.PasswortEnc, eigen)); err != nil {
			return false, err
		}
		neu, err := scanZugang(tx.QueryRow(`SELECT `+zugangSpalten+` FROM zugang WHERE id=?`, eigen))
		if err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		*z = neu
		return true, nil
	case !errors.Is(err, sql.ErrNoRows):
		return false, err
	}
	r, err := tx.Exec(`INSERT INTO zugang(name,benutzer,art,oeffentlich,fingerprint,schluessel_typ,passwort_enc,schluessel_enc,passphrase_enc) VALUES(?,?,?,?,?,?,?,?,?)`,
		z.Name, z.Benutzer, z.Art, z.Oeffentlich, z.Fingerprint, z.SchluesselTyp, z.PasswortEnc, z.SchluesselEnc, z.PassphraseEnc)
	if err != nil {
		return false, err
	}
	id, _ := r.LastInsertId()
	if err := mussTreffen(tx.Exec(`UPDATE host SET zugang_id=? WHERE id=?`, id, hostID)); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	z.ID = id
	z.HatPasswort, z.HatPassphrase = len(z.PasswortEnc) > 0, len(z.PassphraseEnc) > 0
	return false, nil
}

// HostBenutzerSetzen traegt den Benutzer ein, wenn der Host noch keinen hat.
func (s *Store) HostBenutzerSetzen(hostID int64, benutzer string) error {
	_, err := s.db.Exec(`UPDATE host SET benutzer=? WHERE id=? AND benutzer=''`, benutzer, hostID)
	return err
}

func (s *Store) HostLoeschen(id int64) error {
	return mussTreffen(s.db.Exec(`DELETE FROM host WHERE id=?`, id))
}

// --- Zugaenge ---

const zugangSpalten = `id,name,benutzer,art,oeffentlich,fingerprint,schluessel_typ,passwort_enc,schluessel_enc,passphrase_enc`

func scanZugang(sc interface{ Scan(...any) error }) (Zugang, error) {
	var z Zugang
	err := sc.Scan(&z.ID, &z.Name, &z.Benutzer, &z.Art, &z.Oeffentlich, &z.Fingerprint, &z.SchluesselTyp, &z.PasswortEnc, &z.SchluesselEnc, &z.PassphraseEnc)
	z.HatPasswort = len(z.PasswortEnc) > 0
	z.HatPassphrase = len(z.PassphraseEnc) > 0
	return z, err
}

func (s *Store) Zugaenge() ([]Zugang, error) {
	rows, err := s.db.Query(`SELECT ` + zugangSpalten + ` FROM zugang ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Zugang{}
	for rows.Next() {
		z, err := scanZugang(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, z)
	}
	return out, rows.Err()
}

func (s *Store) Zugang(id int64) (Zugang, error) {
	z, err := scanZugang(s.db.QueryRow(`SELECT `+zugangSpalten+` FROM zugang WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return z, ErrNichtGefunden
	}
	return z, err
}

func (s *Store) ZugangSpeichern(z *Zugang) error {
	z.HatPasswort, z.HatPassphrase = len(z.PasswortEnc) > 0, len(z.PassphraseEnc) > 0
	args := []any{z.Name, z.Benutzer, z.Art, z.Oeffentlich, z.Fingerprint, z.SchluesselTyp, z.PasswortEnc, z.SchluesselEnc, z.PassphraseEnc}
	if z.ID == 0 {
		r, err := s.db.Exec(`INSERT INTO zugang(name,benutzer,art,oeffentlich,fingerprint,schluessel_typ,passwort_enc,schluessel_enc,passphrase_enc) VALUES(?,?,?,?,?,?,?,?,?)`, args...)
		if err != nil {
			return err
		}
		z.ID, _ = r.LastInsertId()
		return nil
	}
	return mussTreffen(s.db.Exec(`UPDATE zugang SET name=?,benutzer=?,art=?,oeffentlich=?,fingerprint=?,schluessel_typ=?,passwort_enc=?,schluessel_enc=?,passphrase_enc=? WHERE id=?`, append(args, z.ID)...))
}

func (s *Store) ZugangLoeschen(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE host SET zugang_id=0 WHERE zugang_id=?`, id); err != nil {
		return err
	}
	if err := mussTreffen(tx.Exec(`DELETE FROM zugang WHERE id=?`, id)); err != nil {
		return err
	}
	return tx.Commit()
}

// ZugangNutzung zaehlt die Hosts je Zugang.
func (s *Store) ZugangNutzung() (map[int64]int, error) {
	rows, err := s.db.Query(`SELECT zugang_id, COUNT(*) FROM host WHERE zugang_id<>0 GROUP BY zugang_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// --- bekannte Host-Schluessel ---

func (s *Store) Bekannt(ziel string) (BekannterHost, error) {
	var b BekannterHost
	var seit int64
	err := s.db.QueryRow(`SELECT ziel,typ,schluessel,fingerprint,seit FROM bekannt WHERE ziel=?`, ziel).Scan(&b.Ziel, &b.Typ, &b.Schluessel, &b.Fingerprint, &seit)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNichtGefunden
	}
	b.Seit = time.Unix(seit, 0)
	return b, err
}

func (s *Store) BekanntSetzen(b BekannterHost) error {
	_, err := s.db.Exec(`INSERT INTO bekannt(ziel,typ,schluessel,fingerprint,seit) VALUES(?,?,?,?,?)
ON CONFLICT(ziel) DO UPDATE SET typ=excluded.typ,schluessel=excluded.schluessel,fingerprint=excluded.fingerprint,seit=excluded.seit`,
		b.Ziel, b.Typ, b.Schluessel, b.Fingerprint, time.Now().Unix())
	return err
}

func (s *Store) BekannteHosts() ([]BekannterHost, error) {
	rows, err := s.db.Query(`SELECT ziel,typ,schluessel,fingerprint,seit FROM bekannt ORDER BY ziel`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BekannterHost{}
	for rows.Next() {
		var b BekannterHost
		var seit int64
		if err := rows.Scan(&b.Ziel, &b.Typ, &b.Schluessel, &b.Fingerprint, &seit); err != nil {
			return nil, err
		}
		b.Seit = time.Unix(seit, 0)
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) BekanntLoeschen(ziel string) error {
	return mussTreffen(s.db.Exec(`DELETE FROM bekannt WHERE ziel=?`, ziel))
}

// --- Snippets ---

func (s *Store) Snippets() ([]Snippet, error) {
	rows, err := s.db.Query(`SELECT id,name,befehl,beschreibung FROM snippet ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Snippet{}
	for rows.Next() {
		var sn Snippet
		if err := rows.Scan(&sn.ID, &sn.Name, &sn.Befehl, &sn.Beschreibung); err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	return out, rows.Err()
}

func (s *Store) SnippetSpeichern(sn *Snippet) error {
	if sn.ID == 0 {
		r, err := s.db.Exec(`INSERT INTO snippet(name,befehl,beschreibung) VALUES(?,?,?)`, sn.Name, sn.Befehl, sn.Beschreibung)
		if err != nil {
			return err
		}
		sn.ID, _ = r.LastInsertId()
		return nil
	}
	return mussTreffen(s.db.Exec(`UPDATE snippet SET name=?,befehl=?,beschreibung=? WHERE id=?`, sn.Name, sn.Befehl, sn.Beschreibung, sn.ID))
}

func (s *Store) SnippetLoeschen(id int64) error {
	return mussTreffen(s.db.Exec(`DELETE FROM snippet WHERE id=?`, id))
}

// --- Hilfen ---

func mussTreffen(r sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNichtGefunden
	}
	return nil
}

func splitTags(s string) []string {
	out := []string{}
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func joinTags(t []string) string {
	clean := make([]string, 0, len(t))
	for _, x := range t {
		x = strings.TrimSpace(strings.ReplaceAll(x, ",", " "))
		if x != "" {
			clean = append(clean, x)
		}
	}
	return strings.Join(clean, ",")
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
