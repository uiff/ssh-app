package store

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// Aeltere Datenbanken bekommen die VNC-Spalten nachgeruestet: VNC aus, Port 5900, Weg ssh.
func TestVncSpaltenNachruesten(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "alt.db")
	db, err := sql.Open("sqlite", "file:"+pfad)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE host (
  id INTEGER PRIMARY KEY, name TEXT NOT NULL, adresse TEXT NOT NULL, port INTEGER NOT NULL DEFAULT 22,
  benutzer TEXT NOT NULL DEFAULT '', ordner_id INTEGER NOT NULL DEFAULT 0, zugang_id INTEGER NOT NULL DEFAULT 0,
  farbe TEXT NOT NULL DEFAULT '', tags TEXT NOT NULL DEFAULT '',
  pfad_folgen INTEGER NOT NULL DEFAULT 1, notiz TEXT NOT NULL DEFAULT '', geraet TEXT NOT NULL DEFAULT '');
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
	if err != nil || len(hs) != 1 {
		t.Fatalf("%+v %v", hs, err)
	}
	if h := hs[0]; h.VNC || h.VncPort != 5900 || h.VncWeg != "ssh" || h.HatVncPasswort || h.VncPasswortEnc != nil {
		t.Fatalf("nachgerüstet: %+v", h)
	}
	// Zweites Oeffnen ruestet nichts doppelt nach.
	s2, err := Oeffnen(pfad)
	if err != nil {
		t.Fatal(err)
	}
	s2.Close()
}

// Das VNC-Passwort aendert sich beim Speichern nur mit VncPasswortSetzen.
func TestVncPasswortBleibt(t *testing.T) {
	s := neu(t)
	h := Host{Name: "hmi", Adresse: "10.0.0.5", VNC: true, VncWeg: "direkt", VncPasswortEnc: []byte{1, 2, 3}}
	if err := s.HostSpeichern(&h); err != nil {
		t.Fatal(err)
	}
	if !h.HatVncPasswort || h.VncPort != 5900 {
		t.Fatalf("anlegen: %+v", h)
	}
	aenderung := Host{ID: h.ID, Name: "hmi 2", Adresse: "10.0.0.5", VNC: true, VncPort: 5901, VncWeg: "direkt"}
	if err := s.HostSpeichern(&aenderung); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Host(h.ID)
	if string(got.VncPasswortEnc) != "\x01\x02\x03" || got.Name != "hmi 2" || got.VncPort != 5901 || !got.HatVncPasswort {
		t.Fatalf("Passwort verloren: %+v", got)
	}
	if err := s.HostVncPasswortSetzen(h.ID, []byte{9}); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.Host(h.ID); string(got.VncPasswortEnc) != "\x09" {
		t.Fatalf("setzen: %+v", got)
	}
	aenderung.VncPasswortSetzen, aenderung.VncPasswortEnc = true, nil
	if err := s.HostSpeichern(&aenderung); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.Host(h.ID); got.HatVncPasswort || got.VncPasswortEnc != nil {
		t.Fatalf("entfernen: %+v", got)
	}
	if err := s.HostVncPasswortSetzen(999, []byte{1}); err == nil {
		t.Fatal("unbekannter Host")
	}
}

// Backup: VNC-Felder samt verschluesseltem Passwort reisen mit; das Passwort steht nie
// im Host-JSON; alte Sicherungen ohne Felder ergeben Hosts ohne VNC.
func TestVncImBackup(t *testing.T) {
	a := neu(t)
	h := Host{Name: "panel", Adresse: "10.0.0.8", VNC: true, VncPort: 5902, VncWeg: "direkt", VncPasswortEnc: []byte("verschluesselt")}
	a.HostSpeichern(&h)
	sg, err := a.Exportieren([]byte(`{}`), "test")
	if err != nil {
		t.Fatal(err)
	}
	roh, _ := json.Marshal(sg.Hosts[0])
	var felder map[string]any
	json.Unmarshal(roh, &felder)
	if felder["vncPasswortEnc"] == nil || felder["vnc"] != true || felder["vncPort"] != 5902.0 || felder["vncWeg"] != "direkt" {
		t.Fatalf("Backup-JSON: %s", roh)
	}
	nurHost, _ := json.Marshal(sg.Hosts[0].Host)
	if strings.Contains(string(nurHost), "vncPasswortEnc") || strings.Contains(string(nurHost), "dmVyc2NobHVlc3NlbHQ") {
		t.Fatalf("Passwort im Host-JSON: %s", nurHost)
	}

	// Rundlauf ueber JSON.
	var zurueck Sicherung
	b, _ := json.Marshal(sg)
	json.Unmarshal(b, &zurueck)
	z := neu(t)
	if err := z.Einspielen(zurueck, "tresor"); err != nil {
		t.Fatal(err)
	}
	got, _ := z.Host(h.ID)
	if !got.VNC || got.VncPort != 5902 || got.VncWeg != "direkt" || string(got.VncPasswortEnc) != "verschluesselt" {
		t.Fatalf("nach Einspielen: %+v", got)
	}

	// Alte Sicherung ohne VNC-Felder.
	var alt Sicherung
	json.Unmarshal([]byte(`{"format":"ssh-app-sicherung","version":1,"tresorKopf":"e30=","hosts":[{"id":5,"name":"alt","adresse":"10.0.0.9","port":22}]}`), &alt)
	y := neu(t)
	if err := y.Einspielen(alt, "tresor"); err != nil {
		t.Fatal(err)
	}
	if got, _ := y.Host(5); got.VNC || got.VncPort != 5900 || got.VncWeg != "ssh" || got.HatVncPasswort {
		t.Fatalf("altes Backup: %+v", got)
	}
}
