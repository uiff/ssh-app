package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// V1: aeltere Datenbanken bekommen die Spalte geraet nachgeruestet.
func TestGeraetSpalteNachruesten(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "alt.db")
	db, err := sql.Open("sqlite", "file:"+pfad)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE host (
  id INTEGER PRIMARY KEY, name TEXT NOT NULL, adresse TEXT NOT NULL, port INTEGER NOT NULL DEFAULT 22,
  benutzer TEXT NOT NULL DEFAULT '', ordner_id INTEGER NOT NULL DEFAULT 0, zugang_id INTEGER NOT NULL DEFAULT 0,
  jump_id INTEGER NOT NULL DEFAULT 0, farbe TEXT NOT NULL DEFAULT '', tags TEXT NOT NULL DEFAULT '',
  pfad_folgen INTEGER NOT NULL DEFAULT 1, notiz TEXT NOT NULL DEFAULT '');
INSERT INTO host(name,adresse,jump_id) VALUES('alt','10.0.0.1',7);`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Oeffnen(pfad)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	hs, err := s.Hosts()
	if err != nil || len(hs) != 1 || hs[0].Geraet != "" {
		t.Fatalf("Hosts nach Nachrüsten: %+v %v", hs, err)
	}
	hs[0].Geraet = "sps"
	if err := s.HostSpeichern(&hs[0]); err != nil {
		t.Fatal(err)
	}
	if h, _ := s.Host(hs[0].ID); h.Geraet != "sps" {
		t.Fatalf("Gerät: %q", h.Geraet)
	}
	// Die alte Spalte jump_id bleibt unbenutzt stehen; neue Hosts lassen sich anlegen.
	neuer := Host{Name: "neu", Adresse: "10.0.0.2"}
	if err := s.HostSpeichern(&neuer); err != nil {
		t.Fatalf("Host in alter Datenbank anlegen: %v", err)
	}
	if err := s.HostLoeschen(hs[0].ID); err != nil {
		t.Fatalf("Host mit altem Jump-Verweis loeschen: %v", err)
	}
	// Zweites Oeffnen: nichts doppelt nachruesten.
	s2, err := Oeffnen(pfad)
	if err != nil {
		t.Fatal(err)
	}
	s2.Close()
}

func TestGeraetImBackup(t *testing.T) {
	s := neu(t)
	h := Host{Name: "plc", Adresse: "10.0.0.2", Geraet: "sps"}
	if err := s.HostSpeichern(&h); err != nil {
		t.Fatal(err)
	}
	sg, err := s.Exportieren([]byte(`{}`), "test")
	if err != nil {
		t.Fatal(err)
	}
	ziel := neu(t)
	if err := ziel.Einspielen(sg, "tresor"); err != nil {
		t.Fatal(err)
	}
	if got, _ := ziel.Host(h.ID); got.Geraet != "sps" {
		t.Fatalf("Gerät nach Einspielen: %q", got.Geraet)
	}
}

// V2: Zugang anlegen und an den Host haengen in einem Schritt.
func TestHostZugangSetzen(t *testing.T) {
	s := neu(t)
	h := Host{Name: "pi", Adresse: "10.0.0.3"}
	s.HostSpeichern(&h)
	z := Zugang{Name: "pi", Benutzer: "pi", Art: "passwort", PasswortEnc: []byte{1}}
	if akt, err := s.HostZugangSetzen(h.ID, &z); err != nil || z.ID == 0 || akt {
		t.Fatalf("%v %v %+v", akt, err, z)
	}
	if got, _ := s.Host(h.ID); got.ZugangID != z.ID {
		t.Fatalf("Host-Zugang: %d", got.ZugangID)
	}
	if _, err := s.HostZugangSetzen(999, &Zugang{Name: "x", Art: "passwort"}); err == nil {
		t.Fatal("unbekannter Host angenommen")
	}
	if zs, _ := s.Zugaenge(); len(zs) != 1 {
		t.Fatalf("verwaister Zugang angelegt: %d", len(zs))
	}
	s.HostBenutzerSetzen(h.ID, "root")
	s.HostBenutzerSetzen(h.ID, "anderer") // ueberschreibt nicht
	if got, _ := s.Host(h.ID); got.Benutzer != "root" {
		t.Fatalf("Benutzer: %q", got.Benutzer)
	}
}

// SICHERHEIT-10: Datenbankdateien sind nur fuer den eigenen Benutzer lesbar.
func TestDatenbankRechte(t *testing.T) {
	verz := t.TempDir()
	os.Chmod(verz, 0o755)
	pfad := filepath.Join(verz, "ssh-app.db")
	os.WriteFile(pfad, nil, 0o644) // vorhandene Datei mit zu weiten Rechten
	s, err := Oeffnen(pfad)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SetMeta("k", []byte("v")); err != nil {
		t.Fatal(err)
	}
	for _, e := range []string{"", "-wal", "-shm"} {
		fi, err := os.Stat(pfad + e)
		if err != nil {
			continue
		}
		if fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s hat Rechte %v", filepath.Base(pfad+e), fi.Mode().Perm())
		}
	}
}

// Befund «Zugangsdaten speichern»: wiederholtes Speichern ergaenzt den eigenen
// Passwort-Zugang des Hosts, statt einen Doppelgaenger anzulegen.
func TestHostZugangSetzenOhneDoppelgaenger(t *testing.T) {
	s := neu(t)
	h := Host{Name: "werkstatt", Adresse: "10.0.0.4"}
	s.HostSpeichern(&h)
	erst := Zugang{Name: "werkstatt", Benutzer: "pi", Art: "passwort", PasswortEnc: []byte{1}}
	if _, err := s.HostZugangSetzen(h.ID, &erst); err != nil {
		t.Fatal(err)
	}
	// Passwort im Zugang-Dialog entfernt und Benutzer umbenannt, dann erneut gespeichert.
	erst.PasswortEnc, erst.Name = nil, "Werkstatt-Pi"
	s.ZugangSpeichern(&erst)
	zweit := Zugang{Name: "werkstatt", Benutzer: "root", Art: "passwort", PasswortEnc: []byte{2}}
	akt, err := s.HostZugangSetzen(h.ID, &zweit)
	if err != nil || !akt || zweit.ID != erst.ID {
		t.Fatalf("eigener Zugang nicht ergaenzt: %v %v %+v", akt, err, zweit)
	}
	zs, _ := s.Zugaenge()
	if len(zs) != 1 || zs[0].Name != "Werkstatt-Pi" || zs[0].Benutzer != "pi" || string(zs[0].PasswortEnc) != "\x02" || !zs[0].HatPasswort {
		t.Fatalf("Zugaenge: %+v", zs)
	}
	if got, _ := s.Host(h.ID); got.ZugangID != erst.ID {
		t.Fatalf("Host-Zugang: %d", got.ZugangID)
	}
	// Ohne Benutzer im Zugang gilt der erfragte.
	s.ZugangSpeichern(&Zugang{ID: erst.ID, Name: "Werkstatt-Pi", Art: "passwort"})
	s.HostZugangSetzen(h.ID, &Zugang{Name: "werkstatt", Benutzer: "admin", Art: "passwort", PasswortEnc: []byte{3}})
	if z, _ := s.Zugang(erst.ID); z.Benutzer != "admin" {
		t.Fatalf("Benutzer: %q", z.Benutzer)
	}

	// Ein gemeinsamer Zugang (zwei Hosts) und ein Schluessel-Zugang bleiben unberuehrt.
	gemeinsam := Zugang{Name: "team", Benutzer: "ops", Art: "passwort"}
	s.ZugangSpeichern(&gemeinsam)
	a := Host{Name: "a", Adresse: "10.0.0.5", ZugangID: gemeinsam.ID}
	b := Host{Name: "b", Adresse: "10.0.0.6", ZugangID: gemeinsam.ID}
	s.HostSpeichern(&a)
	s.HostSpeichern(&b)
	neu := Zugang{Name: "a", Benutzer: "ops", Art: "passwort", PasswortEnc: []byte{4}}
	if akt, err := s.HostZugangSetzen(a.ID, &neu); err != nil || akt || neu.ID == gemeinsam.ID {
		t.Fatalf("gemeinsamer Zugang veraendert: %v %v %+v", akt, err, neu)
	}
	if z, _ := s.Zugang(gemeinsam.ID); len(z.PasswortEnc) != 0 {
		t.Fatal("Passwort im gemeinsamen Zugang gelandet")
	}
	k := Zugang{Name: "key", Benutzer: "pi", Art: "schluessel"}
	s.ZugangSpeichern(&k)
	c := Host{Name: "c", Adresse: "10.0.0.7", ZugangID: k.ID}
	s.HostSpeichern(&c)
	neuK := Zugang{Name: "c", Benutzer: "pi", Art: "passwort", PasswortEnc: []byte{5}}
	if akt, err := s.HostZugangSetzen(c.ID, &neuK); err != nil || akt || neuK.ID == k.ID {
		t.Fatalf("Schlüssel-Zugang veraendert: %v %v %+v", akt, err, neuK)
	}
}
