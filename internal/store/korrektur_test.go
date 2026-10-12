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
INSERT INTO host(name,adresse) VALUES('alt','10.0.0.1');`); err != nil {
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
	if err := s.HostZugangSetzen(h.ID, &z); err != nil || z.ID == 0 {
		t.Fatalf("%v %+v", err, z)
	}
	if got, _ := s.Host(h.ID); got.ZugangID != z.ID {
		t.Fatalf("Host-Zugang: %d", got.ZugangID)
	}
	if err := s.HostZugangSetzen(999, &Zugang{Name: "x", Art: "passwort"}); err == nil {
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
