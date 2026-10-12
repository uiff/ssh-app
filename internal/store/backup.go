package store

import (
	"errors"
	"time"
)

// SicherungVersion steigt, wenn sich das Format der Sicherung aendert.
const SicherungVersion = 1

// ZugangRoh traegt einen Zugang samt verschluesselter Felder (base64 in JSON).
type ZugangRoh struct {
	Zugang
	PasswortEnc   []byte `json:"passwortEnc,omitempty"`
	SchluesselEnc []byte `json:"schluesselEnc,omitempty"`
	PassphraseEnc []byte `json:"passphraseEnc,omitempty"`
}

// Sicherung ist der komplette Bestand. Geheimnisse bleiben verschluesselt;
// der Tresor-Kopf reist mit, damit das Hauptpasswort sie wieder oeffnet.
type Sicherung struct {
	Format     string          `json:"format"`
	Version    int             `json:"version"`
	Erstellt   time.Time       `json:"erstellt"`
	App        string          `json:"app"`
	TresorKopf []byte          `json:"tresorKopf"`
	Ordner     []Ordner        `json:"ordner"`
	Hosts      []Host          `json:"hosts"`
	Zugaenge   []ZugangRoh     `json:"zugaenge"`
	Bekannt    []BekannterHost `json:"bekannt"`
	Snippets   []Snippet       `json:"snippets"`
}

const SicherungFormat = "ssh-app-sicherung"

// Exportieren liest den ganzen Bestand.
func (s *Store) Exportieren(tresorKopf []byte, app string) (Sicherung, error) {
	sg := Sicherung{Format: SicherungFormat, Version: SicherungVersion, Erstellt: time.Now().UTC(), App: app, TresorKopf: tresorKopf}
	var err error
	if sg.Ordner, err = s.Ordner(); err != nil {
		return sg, err
	}
	if sg.Hosts, err = s.Hosts(); err != nil {
		return sg, err
	}
	zs, err := s.Zugaenge()
	if err != nil {
		return sg, err
	}
	for _, z := range zs {
		sg.Zugaenge = append(sg.Zugaenge, ZugangRoh{Zugang: z, PasswortEnc: z.PasswortEnc, SchluesselEnc: z.SchluesselEnc, PassphraseEnc: z.PassphraseEnc})
	}
	if sg.Bekannt, err = s.BekannteHosts(); err != nil {
		return sg, err
	}
	if sg.Snippets, err = s.Snippets(); err != nil {
		return sg, err
	}
	return sg, nil
}

// Pruefen lehnt Dateien ab, die keine Sicherung dieser App sind.
func (sg Sicherung) Pruefen() error {
	if sg.Format != SicherungFormat {
		return errors.New("Die Datei ist keine Sicherung dieser App")
	}
	if sg.Version < 1 || sg.Version > SicherungVersion {
		return errors.New("Die Sicherung stammt aus einer neueren Version — erst die App aktualisieren")
	}
	if len(sg.TresorKopf) == 0 {
		return errors.New("Der Sicherung fehlt der Tresor-Kopf")
	}
	return nil
}

// Einspielen ersetzt den ganzen Bestand in einer Transaktion (alles oder nichts).
// IDs bleiben erhalten, damit Verweise (Ordner, Zugang, Jump-Host) stimmen.
func (s *Store) Einspielen(sg Sicherung, kopfSchluessel string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range []string{"ordner", "host", "zugang", "bekannt", "snippet"} {
		if _, err := tx.Exec(`DELETE FROM ` + t); err != nil {
			return err
		}
	}
	for _, o := range sg.Ordner {
		if _, err := tx.Exec(`INSERT INTO ordner(id,name,rang) VALUES(?,?,?)`, o.ID, o.Name, o.Rang); err != nil {
			return err
		}
	}
	for _, z := range sg.Zugaenge {
		if _, err := tx.Exec(`INSERT INTO zugang(id,name,benutzer,art,oeffentlich,fingerprint,schluessel_typ,passwort_enc,schluessel_enc,passphrase_enc) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			z.ID, z.Name, z.Benutzer, z.Art, z.Oeffentlich, z.Fingerprint, z.SchluesselTyp, z.PasswortEnc, z.SchluesselEnc, z.PassphraseEnc); err != nil {
			return err
		}
	}
	for _, h := range sg.Hosts {
		if h.Port == 0 {
			h.Port = 22
		}
		if _, err := tx.Exec(`INSERT INTO host(id,name,adresse,port,benutzer,ordner_id,zugang_id,jump_id,farbe,tags,pfad_folgen,notiz,geraet) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			h.ID, h.Name, h.Adresse, h.Port, h.Benutzer, h.OrdnerID, h.ZugangID, h.JumpID, h.Farbe, joinTags(h.Tags), b2i(h.PfadFolgen), h.Notiz, h.Geraet); err != nil {
			return err
		}
	}
	for _, b := range sg.Bekannt {
		if _, err := tx.Exec(`INSERT INTO bekannt(ziel,typ,schluessel,fingerprint,seit) VALUES(?,?,?,?,?)`, b.Ziel, b.Typ, b.Schluessel, b.Fingerprint, b.Seit.Unix()); err != nil {
			return err
		}
	}
	for _, sn := range sg.Snippets {
		if _, err := tx.Exec(`INSERT INTO snippet(id,name,befehl,beschreibung) VALUES(?,?,?,?)`, sn.ID, sn.Name, sn.Befehl, sn.Beschreibung); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO meta(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, kopfSchluessel, sg.TresorKopf); err != nil {
		return err
	}
	return tx.Commit()
}
