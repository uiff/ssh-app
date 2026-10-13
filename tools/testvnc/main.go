// Programm testvnc: ein kleiner VNC-Server fuer Browser-Proben des VNC-Viewers.
//
// Er lauscht nur auf 127.0.0.1 (oder ::1), zeigt ein Testbild oder ein PNG in
// Raw-Kodierung, malt bei Klicks Punkte (links rot, Mitte gruen, rechts blau; ein
// Rechtsklick legt ausserdem einen Text in die Zwischenablage des Viewers), zeigt die
// letzte Taste (auch Strg+Alt+Entf), Mausrad und die empfangene Zwischenablage an und
// folgt SetDesktopSize (Fernaufloesung anpassen). Oben links laeuft eine Uhr, damit
// laufend kleine Bildaktualisierungen kommen.
//
//	go run ./tools/testvnc -addr 127.0.0.1:5999 -passwort geheim
//	go run ./tools/testvnc -addr 127.0.0.1:5998 -typ vencrypt -passwort geheim   # Durchreichen
//	go run ./tools/testvnc -addr 127.0.0.1:5997 -bild bildschirm.png -version 3.3
//
// Fuer Keyway einen Host mit Adresse 127.0.0.1, VNC an, VNC-Port = Port von -addr und
// VNC-Weg «direkt» anlegen.
package main

import (
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/uiff/ssh-app/internal/vnc/testvnc"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:5999", "Adresse (nur 127.0.0.1 oder ::1)")
	passwort := flag.String("passwort", "", "VNC-Passwort (leer: keine Anmeldung)")
	typ := flag.String("typ", "", "Anmeldung: none, vnc oder vencrypt (VeNCrypt Plain ohne TLS, für das Durchreichen); leer: vnc mit Passwort, sonst none")
	benutzer := flag.String("benutzer", "", "VeNCrypt: erwarteter Benutzer (leer: beliebig)")
	version := flag.String("version", "3.8", "RFB-Version: 3.3, 3.7 oder 3.8")
	bild := flag.String("bild", "", "PNG (oder JPEG) statt des Testbilds")
	breite := flag.Int("breite", 1024, "Breite des Testbilds")
	hoehe := flag.Int("hoehe", 768, "Höhe des Testbilds")
	name := flag.String("name", "Keyway Testbild", "Desktop-Name")
	still := flag.Bool("still", false, "keine Ereignisse ausgeben")
	flag.Parse()

	o := testvnc.Optionen{Adresse: *addr, Passwort: *passwort, Typ: *typ, Benutzer: *benutzer,
		Version: *version, Breite: *breite, Hoehe: *hoehe, Name: *name}
	if !*still {
		o.Log = testvnc.LogAusgabe
	}
	if *bild != "" {
		f, err := os.Open(*bild)
		if err != nil {
			log.Fatalf("Bild: %v", err)
		}
		img, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			log.Fatalf("Bild %s: %v", *bild, err)
		}
		o.Bild = img
		o.Breite, o.Hoehe = 0, 0
	}
	s, err := testvnc.Starten(o)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(s.Beschreibung())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	s.Close()
}
