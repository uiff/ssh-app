package main

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNetzListe(t *testing.T) {
	p, err := netzListe(" 10.0.0.5, 172.16.0.0/12 ,fd00::/8,::ffff:192.0.2.1")
	if err != nil || len(p) != 4 {
		t.Fatalf("%v %v", p, err)
	}
	if p[0].String() != "10.0.0.5/32" || p[1].String() != "172.16.0.0/12" || p[3].String() != "192.0.2.1/32" {
		t.Fatalf("Netze: %v", p)
	}
	if _, err := netzListe("proxy.lan"); err == nil || !strings.Contains(err.Error(), "keine IP-Adresse") {
		t.Fatalf("Name statt Adresse: %v", err)
	}
	if l := liste(" a, ,b,"); len(l) != 2 || l[0] != "a" || l[1] != "b" {
		t.Fatalf("liste: %q", l)
	}
}

// SICHERHEIT-10 / CONTAINER-08: Datenordner nur fuer den eigenen Benutzer, klare Meldung,
// wenn er nicht beschreibbar ist.
func TestDatenordnerPruefen(t *testing.T) {
	verz := filepath.Join(t.TempDir(), "daten")
	os.MkdirAll(verz, 0o755)
	if err := datenordnerPruefen(verz); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(verz); fi.Mode().Perm() != 0o700 {
		t.Fatalf("Rechte: %v", fi.Mode().Perm())
	}
	if eintraege, _ := os.ReadDir(verz); len(eintraege) != 0 {
		t.Fatalf("Schreibprobe liegen geblieben: %v", eintraege)
	}
	if os.Getuid() == 0 {
		t.Skip("als root ist jeder Ordner beschreibbar")
	}
	os.Chmod(verz, 0o500)
	defer os.Chmod(verz, 0o700)
	if err := datenordnerPruefen(verz); err == nil || !strings.Contains(err.Error(), "nicht beschreibbar") || !strings.Contains(err.Error(), "chown") {
		t.Fatalf("Meldung: %v", err)
	}
}

// Das eigene Zertifikat traegt den Produktnamen und bleibt beim zweiten Start dasselbe.
func TestSelbstsigniertKeyway(t *testing.T) {
	verz := filepath.Join(t.TempDir(), "tls")
	c, fp, err := selbstsigniert(verz)
	if err != nil || fp == "" {
		t.Fatalf("%q %v", fp, err)
	}
	z, err := x509.ParseCertificate(c.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	// Sprachneutral: erscheint in der Zertifikatswarnung des Browsers, auch fuer englische Anwender.
	if z.Subject.CommonName != "Keyway" || len(z.Subject.Organization) != 1 || z.Subject.Organization[0] != "Keyway (self-signed)" {
		t.Fatalf("Name: %+v", z.Subject)
	}
	if _, fp2, err := selbstsigniert(verz); err != nil || fp2 != fp {
		t.Fatalf("zweiter Start: %q %v", fp2, err)
	}
}
