package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func neu(t *testing.T) *Store {
	t.Helper()
	s, err := Oeffnen(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestHostsOrdnerZugaenge(t *testing.T) {
	s := neu(t)
	o := Ordner{Name: "Zuhause"}
	if err := s.OrdnerSpeichern(&o); err != nil {
		t.Fatal(err)
	}
	z := Zugang{Name: "pi", Benutzer: "pi", Art: "passwort", PasswortEnc: []byte{1, 2, 3}}
	if err := s.ZugangSpeichern(&z); err != nil {
		t.Fatal(err)
	}
	anderer := Host{Name: "bastion", Adresse: "10.0.0.1"}
	if err := s.HostSpeichern(&anderer); err != nil {
		t.Fatal(err)
	}
	h := Host{Name: "pi", Adresse: "192.168.0.42", OrdnerID: o.ID, ZugangID: z.ID, Tags: []string{"a", " b ", ""}, PfadFolgen: true}
	if err := s.HostSpeichern(&h); err != nil {
		t.Fatal(err)
	}
	got, err := s.Host(h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Port != 22 || len(got.Tags) != 2 || got.Tags[1] != "b" || !got.PfadFolgen {
		t.Fatalf("Host: %+v", got)
	}
	n, _ := s.ZugangNutzung()
	if n[z.ID] != 1 {
		t.Fatalf("Nutzung: %v", n)
	}
	gz, _ := s.Zugang(z.ID)
	if !gz.HatPasswort || gz.HatPassphrase {
		t.Fatalf("Zugang-Flags: %+v", gz)
	}

	// Loeschen haengt Verweise aus statt Hosts mitzureissen.
	if err := s.ZugangLoeschen(z.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.OrdnerLoeschen(o.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.HostLoeschen(anderer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Host(anderer.ID); !errors.Is(err, ErrNichtGefunden) {
		t.Fatalf("geloeschter Host: %v", err)
	}
	got, _ = s.Host(h.ID)
	if got.ZugangID != 0 || got.OrdnerID != 0 {
		t.Fatalf("Verweise nicht geloest: %+v", got)
	}
	if err := s.HostLoeschen(999); !errors.Is(err, ErrNichtGefunden) {
		t.Fatalf("Loeschen unbekannt: %v", err)
	}
}

func TestBekannteHosts(t *testing.T) {
	s := neu(t)
	if _, err := s.Bekannt("a:22"); !errors.Is(err, ErrNichtGefunden) {
		t.Fatal(err)
	}
	if err := s.BekanntSetzen(BekannterHost{Ziel: "a:22", Typ: "ssh-ed25519", Schluessel: "x", Fingerprint: "SHA256:1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.BekanntSetzen(BekannterHost{Ziel: "a:22", Typ: "ssh-ed25519", Schluessel: "y", Fingerprint: "SHA256:2"}); err != nil {
		t.Fatal(err)
	}
	b, err := s.Bekannt("a:22")
	if err != nil || b.Fingerprint != "SHA256:2" {
		t.Fatalf("%+v %v", b, err)
	}
}

func TestSicherungRundlauf(t *testing.T) {
	a := neu(t)
	o := Ordner{Name: "Prod"}
	a.OrdnerSpeichern(&o)
	z := Zugang{Name: "k", Benutzer: "root", Art: "schluessel", SchluesselEnc: []byte{9, 9}, PassphraseEnc: []byte{7}}
	a.ZugangSpeichern(&z)
	h := Host{Name: "web", Adresse: "10.0.0.5", OrdnerID: o.ID, ZugangID: z.ID, Tags: []string{"x"}}
	a.HostSpeichern(&h)
	a.BekanntSetzen(BekannterHost{Ziel: "10.0.0.5:22", Typ: "ssh-ed25519", Schluessel: "k", Fingerprint: "SHA256:f"})
	a.SnippetSpeichern(&Snippet{Name: "df", Befehl: "df -h"})
	sg, err := a.Exportieren([]byte(`{"v":1}`), "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := sg.Pruefen(); err != nil {
		t.Fatal(err)
	}

	b := neu(t)
	alt := Host{Name: "wird-ersetzt", Adresse: "1.1.1.1"}
	b.HostSpeichern(&alt)
	if err := b.Einspielen(sg, "tresor"); err != nil {
		t.Fatal(err)
	}
	hs, _ := b.Hosts()
	if len(hs) != 1 || hs[0].Name != "web" || hs[0].OrdnerID != o.ID || hs[0].ZugangID != z.ID {
		t.Fatalf("Hosts: %+v", hs)
	}
	gz, _ := b.Zugang(z.ID)
	if string(gz.SchluesselEnc) != "\x09\x09" || !gz.HatPassphrase {
		t.Fatalf("Zugang: %+v", gz)
	}
	if k, _ := b.Meta("tresor"); string(k) != `{"v":1}` {
		t.Fatalf("Kopf: %s", k)
	}
	if (Sicherung{Format: "anders"}).Pruefen() == nil {
		t.Fatal("fremdes Format angenommen")
	}
}
