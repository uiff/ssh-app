package server

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/uiff/ssh-app/internal/store"
)

// --- Befund: IPv6-Sperre nicht fuer das ganze /64 im eigenen LAN ---

func TestIPv6SperreNichtFuersGanzeNetz(t *testing.T) {
	_, s, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	c.rufe("POST", "/api/abmelden", nil, nil)
	s.schnittstellen = func() []netip.Prefix { return []netip.Prefix{netip.MustParsePrefix("2a02:1210:1:2::/64")} }
	anmelden := func(von, passwort string) int {
		req := httptest.NewRequest("POST", "/api/anmelden", strings.NewReader(`{"benutzer":"admin","passwort":"`+passwort+`"}`))
		req.RemoteAddr = von
		req.Header.Set("X-SSH-App", "1")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec.Code
	}
	// Eigenes LAN (SLAAC) und link-lokal: der Nachbar sperrt nur sich selbst.
	for _, f := range []struct{ angreifer, besitzer string }{
		{"[2a02:1210:1:2:aaaa:bbbb:cccc:dddd]:5000", "[2a02:1210:1:2::10]:5001"},
		{"[fe80::bad%eth0]:5000", "[fe80::1%eth0]:5001"},
		{"[fd00::bad]:5000", "[fd00::1]:5001"},
	} {
		for i := 0; i < maxFehlversuche; i++ {
			if code := anmelden(f.angreifer, "falsch-falsch"); code != 401 {
				t.Fatalf("%s Fehlversuch %d: %d", f.angreifer, i, code)
			}
		}
		if code := anmelden(f.angreifer, testPasswort); code != 429 {
			t.Fatalf("%s nicht gesperrt: %d", f.angreifer, code)
		}
		if code := anmelden(f.besitzer, testPasswort); code != 200 {
			t.Fatalf("Besitzer %s ausgesperrt: %d", f.besitzer, code)
		}
	}

	// Fremdes Netz: je Adresse 5 Versuche, gemeinsam je /64 hoechstens maxFehlversucheNetz.
	versuch := func(von string) error {
		r := httptest.NewRequest("POST", "/api/anmelden", nil)
		r.RemoteAddr = von
		v, err := s.versuchBeginnen(r)
		if err != nil {
			return err
		}
		v.falsch()
		return nil
	}
	for i := 0; i < maxFehlversucheNetz; i++ {
		if err := versuch(fmt.Sprintf("[2001:db8:7:7::%x]:5000", i+1)); err != nil {
			t.Fatalf("Versuch %d aus fremdem Netz: %v", i, err)
		}
	}
	if err := versuch("[2001:db8:7:7::ffff]:5000"); err == nil || !strings.HasPrefix(err.Error(), "Zu viele Fehlversuche") {
		t.Fatalf("fremdes /64 nach %d Fehlversuchen nicht gesperrt: %v", maxFehlversucheNetz, err)
	}
	if err := versuch("[2001:db8:7:8::1]:5000"); err != nil {
		t.Fatalf("anderes Netz mitgesperrt: %v", err)
	}
	if code := anmelden("192.0.2.1:5000", testPasswort); code != 200 {
		t.Fatalf("IPv4 mitgesperrt: %d", code)
	}
}

// --- Befund: DELETE /api/sitzungen/{id} blockiert nicht bis zur Schreibfrist ---

func TestSitzungBeendenMitTotemBrowser(t *testing.T) {
	srv := sshTestServerMit(t, sshOptionen{benutzer: "pi", passwort: "himbeere", flut: true})
	ts, _, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	h := hostAnlegen(t, c, srv.addr, true, nil)
	ws, bereit := terminalOeffnen(t, ts, c, h.ID)
	defer ws.CloseNow()
	// Der Browser liest ab jetzt nicht mehr; die Puffer laufen voll.
	time.Sleep(1500 * time.Millisecond)
	start := time.Now()
	if code := c.rufe("DELETE", "/api/sitzungen/"+bereit.ID, nil, nil); code != 200 {
		t.Fatalf("Beenden: %d", code)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("Beenden dauerte %s", d)
	}
	var liste []map[string]any
	c.rufe("GET", "/api/sitzungen", nil, &liste)
	if len(liste) != 0 {
		t.Fatalf("Sitzung noch da: %v", liste)
	}
}

// --- Befund: «Zugangsdaten speichern» legt keinen Doppelgaenger an ---

func TestZugangsdatenSpeichernOhneDoppelgaenger(t *testing.T) {
	srv := sshTestServer(t, "pi", "himbeere")
	ts, _, c, _ := angemeldeteApp(t, Optionen{Version: "test"})
	h := hostAnlegen(t, c, srv, false, map[string]any{"name": "werkstatt-pi"})
	verbinden := func() nachricht {
		ws := wsOeffnen(t, ts, c, fmt.Sprintf("host=%d", h.ID))
		defer ws.Close(websocket.StatusNormalClosure, "")
		if n := naechste(t, ws); n.T != "anmeldung" {
			t.Fatalf("statt Rückfrage: %+v", n)
		}
		senden(t, ws, nachricht{T: "anmeldung", Benutzer: "pi", Passwort: "himbeere", Speichern: true})
		for {
			n := naechste(t, ws)
			if n.T == "hostkey" {
				senden(t, ws, nachricht{T: "hostkey", OK: true})
				continue
			}
			if n.T == "fehler" || n.T == "bereit" {
				return n
			}
		}
	}
	zugaenge := func() []store.Zugang {
		var d struct{ Zugaenge []store.Zugang }
		c.rufe("GET", "/api/daten", nil, &d)
		return d.Zugaenge
	}
	if n := verbinden(); n.T != "bereit" || !n.Gespeichert {
		t.Fatalf("erstes Speichern: %+v", n)
	}
	zs := zugaenge()
	if len(zs) != 1 {
		t.Fatalf("Zugänge: %+v", zs)
	}
	// Gespeichertes Passwort im Zugang-Dialog entfernen: der Server fragt wieder.
	if code := c.rufe("PUT", fmt.Sprintf("/api/zugaenge/%d", zs[0].ID), map[string]any{"name": zs[0].Name, "benutzer": "pi", "passwortEntfernen": true}, nil); code != 200 {
		t.Fatalf("Passwort entfernen: %d", code)
	}
	if n := verbinden(); n.T != "bereit" || !n.Gespeichert {
		t.Fatalf("zweites Speichern: %+v", n)
	}
	if zs2 := zugaenge(); len(zs2) != 1 || zs2[0].ID != zs[0].ID || !zs2[0].HatPasswort {
		t.Fatalf("Doppelgänger oder ohne Passwort: %+v", zs2)
	}
	ws, _ := terminalOeffnen(t, ts, c, h.ID) // danach ohne Rueckfrage
	ws.Close(websocket.StatusNormalClosure, "")
}

// --- Jump-Host entfernt: alte Backups bleiben einlesbar ---

func TestBackupMitAltemJumpHost(t *testing.T) {
	_, quellApp, quelle, sk := angemeldeteApp(t, Optionen{Version: "test"})
	quelle.rufe("POST", "/api/hosts", map[string]any{"name": "bastion", "adresse": "10.0.0.1", "jumpId": 5}, nil)
	var d struct{ Hosts []map[string]any }
	quelle.rufe("GET", "/api/daten", nil, &d)
	if len(d.Hosts) != 1 {
		t.Fatalf("Hosts: %+v", d.Hosts)
	}
	if _, gibt := d.Hosts[0]["jumpId"]; gibt {
		t.Fatalf("Host-JSON mit jumpId: %+v", d.Hosts[0])
	}
	backup := backupLaden(t, quelle)
	alt := backupUmbauen(t, quellApp, backup, sk, func(sg map[string]any) {
		hosts := sg["hosts"].([]any)
		hosts[0].(map[string]any)["jumpId"] = 1
		sg["hosts"] = append(hosts, map[string]any{"id": 9, "name": "ziel", "adresse": "10.0.0.9", "jumpId": hosts[0].(map[string]any)["id"]})
	})
	if bytes.Equal(alt, backup) {
		t.Fatal("Backup nicht umgebaut")
	}
	ts2, _ := testApp(t)
	c2 := neuerClient(t, ts2.URL, ts2.Client())
	if code, text := einspielenAn(c2, "/api/einspielen", alt, sk); code != 200 {
		t.Fatalf("altes Backup mit jumpId: %d %s", code, text)
	}
	c2.rufe("POST", "/api/anmelden", map[string]string{"benutzer": "admin", "passwort": testPasswort}, nil)
	var d2 struct{ Hosts []store.Host }
	if code := c2.rufe("GET", "/api/daten", nil, &d2); code != http.StatusOK || len(d2.Hosts) != 2 {
		t.Fatalf("nach Einspielen: %d %+v", code, d2.Hosts)
	}
}
