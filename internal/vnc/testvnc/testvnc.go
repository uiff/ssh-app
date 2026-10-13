// Package testvnc ist ein kleiner VNC-Server fuer Tests und Browser-Proben. Er lauscht
// nur auf der eigenen Maschine (127.0.0.1 oder ::1), zeigt ein Testbild oder ein PNG
// (Raw-Kodierung), malt bei Klicks Punkte, zeigt Tasten, Mausrad und Zwischenablage an
// und folgt SetDesktopSize (Fernaufloesung). Anmeldung: keine (Typ 1), VNC-Passwort
// (Typ 2) oder VeNCrypt mit Benutzer und Passwort ohne TLS (Typ 19, Untertyp 256 =
// Plain), damit sich auch das Durchreichen pruefen laesst.
package testvnc

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uiff/ssh-app/internal/vnc"
)

// Optionen des Test-Servers.
type Optionen struct {
	Adresse  string      // nur 127.0.0.1 oder ::1; Vorgabe 127.0.0.1:0 (freier Port)
	Passwort string      // VNC-Passwort (Typ vnc) bzw. Passwort fuer VeNCrypt
	Benutzer string      // VeNCrypt: erwarteter Benutzer ("" = beliebig)
	Typ      string      // "none", "vnc" oder "vencrypt"; leer: "vnc" mit Passwort, sonst "none"
	Version  string      // RFB-Version des Servers: "3.3", "3.7" oder "3.8" (Vorgabe)
	Bild     image.Image // nil: Testbild
	Breite   int         // Groesse des Testbilds (Vorgabe 1024x768; mit Bild dessen Groesse)
	Hoehe    int
	Name     string // Desktop-Name (Vorgabe «Keyway Testbild»)
	Log      func(format string, werte ...any)
}

// Server ist ein laufender Test-VNC-Server.
type Server struct {
	opt Optionen
	l   net.Listener
	d   *desktop

	Offen        atomic.Int32 // gerade offene Verbindungen
	Verbindungen atomic.Int32 // Verbindungen insgesamt

	evMu       sync.Mutex
	ereignisse []string

	connMu sync.Mutex
	conns  map[net.Conn]struct{}
	wg     sync.WaitGroup
	stop   chan struct{}
	einmal sync.Once
}

var typNummern = map[string]byte{"none": 1, "vnc": 2, "vencrypt": 19}

// Starten oeffnet den Server.
func Starten(o Optionen) (*Server, error) {
	if o.Adresse == "" {
		o.Adresse = "127.0.0.1:0"
	}
	host, _, err := net.SplitHostPort(o.Adresse)
	if err != nil {
		return nil, fmt.Errorf("Adresse %q: %w", o.Adresse, err)
	}
	if host == "localhost" {
		o.Adresse = "127.0.0.1" + strings.TrimPrefix(o.Adresse, "localhost")
	} else if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("Adresse %q: nur 127.0.0.1 oder ::1 erlaubt", o.Adresse)
	}
	if o.Typ == "" {
		o.Typ = "none"
		if o.Passwort != "" {
			o.Typ = "vnc"
		}
	}
	if _, ok := typNummern[o.Typ]; !ok {
		return nil, fmt.Errorf("Typ %q: erlaubt sind none, vnc, vencrypt", o.Typ)
	}
	if o.Version == "" {
		o.Version = "3.8"
	}
	if _, ok := serverVersionen[o.Version]; !ok {
		return nil, fmt.Errorf("Version %q: erlaubt sind 3.3, 3.7, 3.8", o.Version)
	}
	if o.Typ == "vencrypt" && o.Version == "3.3" {
		return nil, errors.New("VeNCrypt braucht RFB 3.7 oder 3.8")
	}
	if o.Name == "" {
		o.Name = "Keyway Testbild"
	}
	b, h := o.Breite, o.Hoehe
	if o.Bild != nil && (b == 0 || h == 0) {
		b, h = o.Bild.Bounds().Dx(), o.Bild.Bounds().Dy()
	}
	if b <= 0 || h <= 0 {
		b, h = 1024, 768
	}
	b, h = min(max(b, minGroesse), maxGroesse), min(max(h, minGroesse), maxGroesse)
	if o.Log == nil {
		o.Log = func(string, ...any) {}
	}
	l, err := net.Listen("tcp", o.Adresse)
	if err != nil {
		return nil, err
	}
	s := &Server{opt: o, l: l, conns: map[net.Conn]struct{}{}, stop: make(chan struct{})}
	s.d = neuerDesktop(s, b, h)
	s.wg.Add(2)
	go s.annehmen()
	go s.uhr()
	return s, nil
}

// Adresse liefert host:port, auf dem der Server lauscht.
func (s *Server) Adresse() string { return s.l.Addr().String() }

// Port liefert den Port.
func (s *Server) Port() int { return s.l.Addr().(*net.TCPAddr).Port }

// Groesse liefert die aktuelle Bildschirmgroesse.
func (s *Server) Groesse() (int, int) {
	s.d.mu.Lock()
	defer s.d.mu.Unlock()
	return s.d.b, s.d.h
}

// Ereignisse liefert, was die Viewer getan haben (z. B. «anmeldung ok», «klick links 10,20»,
// «taste 61 A», «ablage hallo», «groesse 800x600»).
func (s *Server) Ereignisse() []string {
	s.evMu.Lock()
	defer s.evMu.Unlock()
	return slices.Clone(s.ereignisse)
}

func (s *Server) ereignis(format string, werte ...any) {
	e := fmt.Sprintf(format, werte...)
	s.opt.Log("%s", e)
	s.evMu.Lock()
	s.ereignisse = append(s.ereignisse, e)
	if len(s.ereignisse) > 1000 {
		s.ereignisse = s.ereignisse[len(s.ereignisse)-1000:]
	}
	s.evMu.Unlock()
}

// Close beendet den Server und alle Verbindungen.
func (s *Server) Close() error {
	s.einmal.Do(func() {
		close(s.stop)
		s.l.Close()
		s.connMu.Lock()
		for c := range s.conns {
			c.Close()
		}
		s.connMu.Unlock()
	})
	s.wg.Wait()
	return nil
}

func (s *Server) annehmen() {
	defer s.wg.Done()
	for {
		c, err := s.l.Accept()
		if err != nil {
			return
		}
		s.connMu.Lock()
		select {
		case <-s.stop:
			s.connMu.Unlock()
			c.Close()
			return
		default:
		}
		s.conns[c] = struct{}{}
		s.connMu.Unlock()
		s.Offen.Add(1)
		s.Verbindungen.Add(1)
		s.wg.Add(1)
		go s.bedienen(c)
	}
}

func (s *Server) uhr() {
	defer s.wg.Done()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.d.uhrZeichnen()
		}
	}
}

// --- Verbindung ---

type klient struct {
	s      *Server
	conn   net.Conn
	wecken chan struct{}
	ende   chan struct{}

	// unter desktop.mu
	pf             pixelFormat
	kodierungen    map[int32]bool
	anfrage        bool // Bildanforderung offen
	voll           bool // ganzes Bild verlangt
	schmutzig      image.Rectangle
	groesseGrund   int // -1: keine; sonst Grund fuer ExtendedDesktopSize (0 Server, 1 dieser, 2 anderer)
	groesseStatus  int
	extGemeldet    bool
	cursorGesendet bool
	ablage         []string // ServerCutText, noch zu senden
}

func (s *Server) bedienen(c net.Conn) {
	defer s.wg.Done()
	defer s.Offen.Add(-1)
	defer func() {
		c.Close()
		s.connMu.Lock()
		delete(s.conns, c)
		s.connMu.Unlock()
	}()
	k := &klient{s: s, conn: c, wecken: make(chan struct{}, 1), ende: make(chan struct{}), pf: nativ, groesseGrund: -1}
	if err := s.begruessen(k); err != nil {
		s.opt.Log("Begrüssung %s: %v", c.RemoteAddr(), err)
		return
	}
	s.d.anmelden(k)
	defer s.d.abmelden(k)
	fertig := make(chan struct{})
	go func() {
		defer close(fertig)
		k.schreiben()
	}()
	err := k.lesen()
	if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
		s.opt.Log("Verbindung %s: %v", c.RemoteAddr(), err)
	}
	close(k.ende)
	c.Close()
	<-fertig
}

var serverVersionen = map[string]string{"3.3": "RFB 003.003\n", "3.7": "RFB 003.007\n", "3.8": "RFB 003.008\n"}

// begruessen: Version, Sicherheit, ClientInit, ServerInit.
func (s *Server) begruessen(k *klient) error {
	c := k.conn
	c.SetDeadline(time.Now().Add(time.Minute))
	defer c.SetDeadline(time.Time{})
	sv := serverVersionen[s.opt.Version]
	if _, err := io.WriteString(c, sv); err != nil {
		return err
	}
	cv := make([]byte, 12)
	if _, err := io.ReadFull(c, cv); err != nil {
		return err
	}
	var kleiner int
	switch string(cv) {
	case "RFB 003.003\n":
		kleiner = 3
	case "RFB 003.007\n":
		kleiner = 7
	case "RFB 003.008\n":
		kleiner = 8
	default:
		return fmt.Errorf("unbekannte Client-Version %q", cv)
	}
	kleiner = min(kleiner, int(sv[10]-'0'))
	typ := typNummern[s.opt.Typ]
	if kleiner == 3 {
		if err := zahlSchreiben(c, uint32(typ)); err != nil {
			return err
		}
	} else {
		if _, err := c.Write([]byte{1, typ}); err != nil {
			return err
		}
		gewaehlt := make([]byte, 1)
		if _, err := io.ReadFull(c, gewaehlt); err != nil {
			return err
		}
		if gewaehlt[0] != typ {
			ergebnisSchreiben(c, kleiner, false, "Unsupported security type")
			return fmt.Errorf("Client wählt Typ %d statt %d", gewaehlt[0], typ)
		}
	}
	switch typ {
	case 1:
		if kleiner == 8 {
			ergebnisSchreiben(c, kleiner, true, "")
		}
	case 2:
		ch := make([]byte, 16)
		rand.Read(ch)
		if _, err := c.Write(ch); err != nil {
			return err
		}
		antwort := make([]byte, 16)
		if _, err := io.ReadFull(c, antwort); err != nil {
			return err
		}
		ok := bytes.Equal(antwort, vnc.DESAntwort(s.opt.Passwort, ch))
		if err := s.anmeldungMelden(c, kleiner, ok); err != nil {
			return err
		}
	case 19:
		if err := s.veNCrypt(c, kleiner); err != nil {
			return err
		}
	}
	init := make([]byte, 1)
	if _, err := io.ReadFull(c, init); err != nil {
		return err
	}
	s.ereignis("clientinit geteilt=%d", init[0])
	b, h := s.Groesse()
	si := binary.BigEndian.AppendUint16(nil, uint16(b))
	si = binary.BigEndian.AppendUint16(si, uint16(h))
	si = append(si, nativ.bytes()...)
	si = binary.BigEndian.AppendUint32(si, uint32(len(s.opt.Name)))
	si = append(si, s.opt.Name...)
	_, err := c.Write(si)
	return err
}

func (s *Server) anmeldungMelden(c net.Conn, kleiner int, ok bool) error {
	if !ok {
		s.ereignis("anmeldung falsch")
		ergebnisSchreiben(c, kleiner, false, "Authentication failed")
		return errors.New("Passwort falsch")
	}
	s.ereignis("anmeldung ok")
	return ergebnisSchreiben(c, kleiner, true, "")
}

// veNCrypt: Version 0.2, Untertyp 256 (Plain, ohne TLS).
func (s *Server) veNCrypt(c net.Conn, kleiner int) error {
	if _, err := c.Write([]byte{0, 2}); err != nil {
		return err
	}
	v := make([]byte, 2)
	if _, err := io.ReadFull(c, v); err != nil {
		return err
	}
	if v[0] != 0 || v[1] != 2 {
		c.Write([]byte{1})
		return fmt.Errorf("VeNCrypt-Version %d.%d", v[0], v[1])
	}
	if _, err := c.Write([]byte{0, 1, 0, 0, 1, 0}); err != nil { // OK, 1 Untertyp: 256
		return err
	}
	var unter uint32
	if err := zahlLesen(c, &unter); err != nil {
		return err
	}
	if unter != 256 {
		return fmt.Errorf("VeNCrypt-Untertyp %d", unter)
	}
	var ul, pl uint32
	if err := zahlLesen(c, &ul); err != nil {
		return err
	}
	if err := zahlLesen(c, &pl); err != nil {
		return err
	}
	if ul > 1024 || pl > 1024 {
		return errors.New("VeNCrypt: Angaben zu lang")
	}
	daten := make([]byte, ul+pl)
	if _, err := io.ReadFull(c, daten); err != nil {
		return err
	}
	benutzer, passwort := string(daten[:ul]), string(daten[ul:])
	ok := passwort == s.opt.Passwort && (s.opt.Benutzer == "" || benutzer == s.opt.Benutzer)
	return s.anmeldungMelden(c, kleiner, ok)
}

func ergebnisSchreiben(c net.Conn, kleiner int, ok bool, grund string) error {
	if ok {
		return zahlSchreiben(c, 0)
	}
	b := binary.BigEndian.AppendUint32(nil, 1)
	if kleiner == 8 {
		b = binary.BigEndian.AppendUint32(b, uint32(len(grund)))
		b = append(b, grund...)
	}
	_, err := c.Write(b)
	return err
}

func zahlSchreiben(c net.Conn, z uint32) error {
	_, err := c.Write(binary.BigEndian.AppendUint32(nil, z))
	return err
}

func zahlLesen(r io.Reader, z *uint32) error {
	b := make([]byte, 4)
	if _, err := io.ReadFull(r, b); err != nil {
		return err
	}
	*z = binary.BigEndian.Uint32(b)
	return nil
}

// lesen verarbeitet die Nachrichten des Viewers.
func (k *klient) lesen() error {
	r := bufio.NewReader(k.conn)
	d := k.s.d
	buf := make([]byte, 64)
	lies := func(n int) ([]byte, error) {
		if n > len(buf) {
			buf = make([]byte, n)
		}
		_, err := io.ReadFull(r, buf[:n])
		return buf[:n], err
	}
	for {
		typ, err := r.ReadByte()
		if err != nil {
			return err
		}
		switch typ {
		case 0: // SetPixelFormat
			b, err := lies(19)
			if err != nil {
				return err
			}
			pf := pixelFormatAus(b[3:19])
			if !pf.gueltig() {
				return fmt.Errorf("Pixelformat nicht unterstützt: %+v", pf)
			}
			d.mu.Lock()
			k.pf, k.voll, k.cursorGesendet = pf, true, false
			d.mu.Unlock()
		case 2: // SetEncodings
			b, err := lies(3)
			if err != nil {
				return err
			}
			n := int(binary.BigEndian.Uint16(b[1:3]))
			b, err = lies(4 * n)
			if err != nil {
				return err
			}
			kod := map[int32]bool{}
			for i := 0; i < n; i++ {
				kod[int32(binary.BigEndian.Uint32(b[4*i:]))] = true
			}
			d.mu.Lock()
			k.kodierungen, k.cursorGesendet = kod, false
			d.mu.Unlock()
		case 3: // FramebufferUpdateRequest
			b, err := lies(9)
			if err != nil {
				return err
			}
			d.mu.Lock()
			k.anfrage = true
			if b[0] == 0 {
				k.voll = true
			}
			d.mu.Unlock()
			k.aufwecken()
		case 4: // KeyEvent
			b, err := lies(7)
			if err != nil {
				return err
			}
			d.taste(b[0] != 0, binary.BigEndian.Uint32(b[3:7]))
		case 5: // PointerEvent
			b, err := lies(5)
			if err != nil {
				return err
			}
			maske, x, y := b[0], int(binary.BigEndian.Uint16(b[1:3])), int(binary.BigEndian.Uint16(b[3:5]))
			if maske&0x80 != 0 { // erweitert: zweites Tastenbyte
				if _, err := lies(1); err != nil {
					return err
				}
			}
			d.zeiger(k, maske&0x7f, x, y)
		case 6: // ClientCutText
			b, err := lies(7)
			if err != nil {
				return err
			}
			n := int32(binary.BigEndian.Uint32(b[3:7]))
			if n < 0 { // erweiterte Zwischenablage: hier nicht angeboten, ueberspringen
				if _, err := io.CopyN(io.Discard, r, -int64(n)); err != nil {
					return err
				}
				continue
			}
			var text bytes.Buffer
			if _, err := io.CopyN(&text, io.LimitReader(r, int64(n)), int64(n)); err != nil {
				return err
			}
			d.ablage(latin1(text.Bytes()))
		case 150: // EnableContinuousUpdates (nicht angeboten)
			if _, err := lies(9); err != nil {
				return err
			}
		case 248: // ClientFence (nicht angeboten)
			b, err := lies(8)
			if err != nil {
				return err
			}
			if _, err := lies(int(b[7])); err != nil {
				return err
			}
		case 250: // xvp
			if _, err := lies(3); err != nil {
				return err
			}
		case 251: // SetDesktopSize
			b, err := lies(7)
			if err != nil {
				return err
			}
			breite, hoehe := int(binary.BigEndian.Uint16(b[1:3])), int(binary.BigEndian.Uint16(b[3:5]))
			if _, err := lies(16 * int(b[5])); err != nil {
				return err
			}
			d.groesseAendern(k, breite, hoehe)
		case 255: // QEMU
			sub, err := r.ReadByte()
			if err != nil {
				return err
			}
			if sub != 0 {
				return fmt.Errorf("QEMU-Nachricht %d", sub)
			}
			b, err := lies(10)
			if err != nil {
				return err
			}
			d.taste(binary.BigEndian.Uint16(b[0:2]) != 0, binary.BigEndian.Uint32(b[2:6]))
		default:
			return fmt.Errorf("unbekannte Nachricht %d", typ)
		}
	}
}

func latin1(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

func (k *klient) aufwecken() {
	select {
	case k.wecken <- struct{}{}:
	default:
	}
}

// schreiben schickt Bildaktualisierungen und Zwischenablage, sobald es etwas gibt.
func (k *klient) schreiben() {
	for {
		select {
		case <-k.wecken:
		case <-k.ende:
			return
		}
		for {
			b := k.s.d.aktualisierung(k)
			if b == nil {
				break
			}
			if _, err := k.conn.Write(b); err != nil {
				k.conn.Close()
				return
			}
		}
	}
}

// --- Pixelformat ---

type pixelFormat struct {
	bpp, tiefe       uint8
	gross, echtfarbe bool
	rMax, gMax, bMax uint16
	rS, gS, bS       uint8
}

// nativ: 32 bpp, Tiefe 24, little-endian, Rot ab Bit 16 (= Speicherformat 0xRRGGBB).
var nativ = pixelFormat{bpp: 32, tiefe: 24, echtfarbe: true, rMax: 255, gMax: 255, bMax: 255, rS: 16, gS: 8, bS: 0}

func (pf pixelFormat) bytes() []byte {
	b := []byte{pf.bpp, pf.tiefe, 0, 0}
	if pf.gross {
		b[2] = 1
	}
	if pf.echtfarbe {
		b[3] = 1
	}
	b = binary.BigEndian.AppendUint16(b, pf.rMax)
	b = binary.BigEndian.AppendUint16(b, pf.gMax)
	b = binary.BigEndian.AppendUint16(b, pf.bMax)
	return append(b, pf.rS, pf.gS, pf.bS, 0, 0, 0)
}

func pixelFormatAus(b []byte) pixelFormat {
	return pixelFormat{bpp: b[0], tiefe: b[1], gross: b[2] != 0, echtfarbe: b[3] != 0,
		rMax: binary.BigEndian.Uint16(b[4:6]), gMax: binary.BigEndian.Uint16(b[6:8]), bMax: binary.BigEndian.Uint16(b[8:10]),
		rS: b[10], gS: b[11], bS: b[12]}
}

func (pf pixelFormat) gueltig() bool {
	return pf.echtfarbe && (pf.bpp == 8 || pf.bpp == 16 || pf.bpp == 32) && pf.rS < 32 && pf.gS < 32 && pf.bS < 32
}

// anhaengen schreibt eine Farbe 0xRRGGBB im Format des Viewers.
func (pf pixelFormat) anhaengen(out []byte, c uint32) []byte {
	if pf == nativ {
		return binary.LittleEndian.AppendUint32(out, c)
	}
	r, g, b := c>>16&0xff, c>>8&0xff, c&0xff
	v := (r*uint32(pf.rMax)/255)<<pf.rS | (g*uint32(pf.gMax)/255)<<pf.gS | (b*uint32(pf.bMax)/255)<<pf.bS
	switch pf.bpp {
	case 8:
		return append(out, byte(v))
	case 16:
		if pf.gross {
			return binary.BigEndian.AppendUint16(out, uint16(v))
		}
		return binary.LittleEndian.AppendUint16(out, uint16(v))
	}
	if pf.gross {
		return binary.BigEndian.AppendUint32(out, v)
	}
	return binary.LittleEndian.AppendUint32(out, v)
}

// --- Hilfen fuer das Programm tools/testvnc ---

// Beschreibung fasst die Einstellungen fuer die Ausgabe des Programms zusammen.
func (s *Server) Beschreibung() string {
	b, h := s.Groesse()
	anm := s.opt.Typ
	if s.opt.Typ != "none" {
		anm += " (Passwort " + strconv.Quote(s.opt.Passwort) + ")"
	}
	return fmt.Sprintf("Test-VNC-Server auf %s: RFB %s, Anmeldung %s, %dx%d, «%s»", s.Adresse(), s.opt.Version, anm, b, h, s.opt.Name)
}

// LogAusgabe ist eine Log-Funktion fuer Optionen.Log, die ueber das Paket log schreibt.
func LogAusgabe(format string, werte ...any) { log.Printf(format, werte...) }
