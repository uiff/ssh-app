package testvnc

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"strings"
	"sync"
	"time"
)

const (
	minGroesse = 64
	maxGroesse = 4096
)

// Kodierungen und Pseudo-Kodierungen, die der Test-Server kennt.
const (
	kodRaw                 = 0
	kodCursor              = -239
	kodDesktopSize         = -223
	kodExtendedDesktopSize = -308
)

// Zeilen der Anzeige oben links.
const (
	zeileTitel = iota
	zeileZeit
	zeileTaste
	zeileMaus
	zeileAblage
	anzahlZeilen
)

// desktop ist das gemeinsame Bild aller Viewer.
type desktop struct {
	s        *Server
	mu       sync.Mutex
	b, h     int
	pix      []uint32 // 0xRRGGBB, zeilenweise
	zeilen   [anzahlZeilen]string
	klienten map[*klient]struct{}

	// Tastenzustand fuer die Anzeige von Kombinationen (Strg+Alt+Entf).
	gedrueckt map[uint32]bool
	// gedrueckte Maustasten (Kanten erkennen)
	maske uint8
}

func neuerDesktop(s *Server, b, h int) *desktop {
	d := &desktop{s: s, b: b, h: h, klienten: map[*klient]struct{}{}, gedrueckt: map[uint32]bool{}}
	d.zeilen[zeileTaste] = "TASTE: -"
	d.zeilen[zeileMaus] = "MAUS: -"
	d.zeilen[zeileAblage] = "ABLAGE: -"
	d.zeichnen()
	return d
}

func (d *desktop) anmelden(k *klient) {
	d.mu.Lock()
	d.klienten[k] = struct{}{}
	d.mu.Unlock()
}

func (d *desktop) abmelden(k *klient) {
	d.mu.Lock()
	delete(d.klienten, k)
	d.mu.Unlock()
}

// markieren meldet allen Viewern einen geaenderten Bereich (d.mu gehalten).
func (d *desktop) markieren(r image.Rectangle) {
	r = r.Intersect(image.Rect(0, 0, d.b, d.h))
	if r.Empty() {
		return
	}
	for k := range d.klienten {
		k.schmutzig = k.schmutzig.Union(r)
		k.aufwecken()
	}
}

// --- Zeichnen (d.mu gehalten oder beim Anlegen) ---

func (d *desktop) zeichnen() {
	d.pix = make([]uint32, d.b*d.h)
	if bild := d.s.opt.Bild; bild != nil {
		d.bildZeichnen(bild)
	} else {
		d.testbild()
	}
	d.zeilen[zeileTitel] = fmt.Sprintf("%s %dX%d", d.s.opt.Name, d.b, d.h)
	d.zeilen[zeileZeit] = "ZEIT " + time.Now().Format("15:04:05")
	for i := range d.zeilen {
		d.zeileZeichnen(i)
	}
}

func (d *desktop) bildZeichnen(bild image.Image) {
	r := bild.Bounds()
	for y := 0; y < d.h; y++ {
		for x := 0; x < d.b; x++ {
			c := uint32(0x303030)
			if x < r.Dx() && y < r.Dy() {
				c = farbe(bild.At(r.Min.X+x, r.Min.Y+y))
			}
			d.pix[y*d.b+x] = c
		}
	}
}

func farbe(c color.Color) uint32 {
	r, g, b, _ := c.RGBA()
	return (r>>8)<<16 | (g>>8)<<8 | b>>8
}

var balken = []uint32{0xFFFFFF, 0xFFFF00, 0x00FFFF, 0x00FF00, 0xFF00FF, 0xFF0000, 0x0000FF, 0x000000}

// testbild: Farbbalken, Grauverlauf, Raster, Kreis (Seitenverhaeltnis) und Rahmen (Raender).
func (d *desktop) testbild() {
	b, h := d.b, d.h
	balkenEnde, verlaufEnde := h*60/100, h*75/100
	for y := 0; y < h; y++ {
		for x := 0; x < b; x++ {
			var c uint32
			switch {
			case y < balkenEnde:
				c = balken[x*len(balken)/b]
			case y < verlaufEnde:
				g := uint32(x * 255 / max(b-1, 1))
				c = g<<16 | g<<8 | g
			default:
				c = 0x14213D
				if x%32 == 0 || y%32 == 0 {
					c = 0x2A3A5A
				}
			}
			d.pix[y*b+x] = c
		}
	}
	// Kreis in der Mitte: bleibt rund, wenn das Seitenverhaeltnis stimmt.
	mx, my := b/2, h/2
	radius := min(b, h) * 2 / 5
	for y := max(0, my-radius-2); y < min(h, my+radius+3); y++ {
		for x := max(0, mx-radius-2); x < min(b, mx+radius+3); x++ {
			dx, dy := x-mx, y-my
			q := dx*dx + dy*dy
			if q >= (radius-1)*(radius-1) && q <= (radius+2)*(radius+2) {
				d.pix[y*b+x] = 0xFFFFFF
			}
		}
	}
	// Rahmen und Eckmarken.
	for x := 0; x < b; x++ {
		for _, y := range []int{0, 1, h - 2, h - 1} {
			d.pix[y*b+x] = 0xFF8800
		}
	}
	for y := 0; y < h; y++ {
		for _, x := range []int{0, 1, b - 2, b - 1} {
			d.pix[y*b+x] = 0xFF8800
		}
	}
	ecke := min(24, b/4, h/4)
	for i := 0; i < ecke; i++ {
		for _, p := range [][2]int{{i, 2}, {2, i}, {b - 1 - i, 2}, {b - 3, i}, {i, h - 3}, {2, h - 1 - i}, {b - 1 - i, h - 3}, {b - 3, h - 1 - i}} {
			d.punkt(p[0], p[1], 0xFF8800)
		}
	}
}

func (d *desktop) punkt(x, y int, c uint32) {
	if x >= 0 && y >= 0 && x < d.b && y < d.h {
		d.pix[y*d.b+x] = c
	}
}

// Masse der Textanzeige.
func (d *desktop) massstab() int {
	if d.b >= 480 && d.h >= 240 {
		return 2
	}
	return 1
}

const zeichenProZeile = 40

func (d *desktop) zeilenRechteck(i int) image.Rectangle {
	m := d.massstab()
	zelleB, zelleH := 6*m, 9*m
	x0, y0 := 8*m, 8*m+i*zelleH
	return image.Rect(x0, y0, x0+zeichenProZeile*zelleB+2*m, y0+zelleH)
}

func (d *desktop) zeileZeichnen(i int) {
	r := d.zeilenRechteck(i)
	m := d.massstab()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			d.punkt(x, y, 0x000000)
		}
	}
	text := []rune(anzeigeText(d.zeilen[i]))
	if len(text) > zeichenProZeile {
		text = text[:zeichenProZeile]
	}
	farbeText := uint32(0xFFFFFF)
	if i == zeileTitel {
		farbeText = 0xFFCC00
	}
	for n, z := range text {
		g := schrift[z]
		for zy := 0; zy < 7; zy++ {
			for zx := 0; zx < 5; zx++ {
				if g[zy]&(1<<(4-zx)) == 0 {
					continue
				}
				for py := 0; py < m; py++ {
					for px := 0; px < m; px++ {
						d.punkt(r.Min.X+m+n*6*m+zx*m+px, r.Min.Y+m+zy*m+py, farbeText)
					}
				}
			}
		}
	}
	d.markieren(r)
}

func (d *desktop) zeileSetzen(i int, text string) {
	d.zeilen[i] = text
	d.zeileZeichnen(i)
}

func (d *desktop) uhrZeichnen() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.zeileSetzen(zeileZeit, "ZEIT "+time.Now().Format("15:04:05"))
}

// --- Ereignisse der Viewer ---

var tastenNamen = map[uint32]string{
	0xff08: "RUECKTASTE", 0xff09: "TAB", 0xff0d: "ENTER", 0xff1b: "ESC", 0xffff: "ENTF",
	0xff50: "POS1", 0xff51: "LINKS", 0xff52: "HOCH", 0xff53: "RECHTS", 0xff54: "RUNTER",
	0xff55: "BILD HOCH", 0xff56: "BILD RUNTER", 0xff57: "ENDE", 0xff63: "EINFG",
	0xffe1: "SHIFT", 0xffe2: "SHIFT", 0xffe3: "STRG", 0xffe4: "STRG", 0xffe9: "ALT", 0xffea: "ALT",
	0xffe7: "META", 0xffe8: "META", 0xffeb: "SUPER", 0xffec: "SUPER", 0xfe03: "ALTGR",
}

var modifikatoren = []struct {
	name   string
	tasten []uint32
}{
	{"STRG", []uint32{0xffe3, 0xffe4}},
	{"ALT", []uint32{0xffe9, 0xffea}},
	{"ALTGR", []uint32{0xfe03}},
	{"SHIFT", []uint32{0xffe1, 0xffe2}},
	{"SUPER", []uint32{0xffeb, 0xffec}},
	{"META", []uint32{0xffe7, 0xffe8}},
}

func tastenName(k uint32) string {
	if n, ok := tastenNamen[k]; ok {
		return n
	}
	if k >= 0xffbe && k <= 0xffc9 {
		return fmt.Sprintf("F%d", k-0xffbe+1)
	}
	if k >= 0x20 && k <= 0xff {
		if k == ' ' {
			return "LEER"
		}
		return string(rune(k))
	}
	return fmt.Sprintf("%X", k)
}

func istModifikator(k uint32) bool {
	for _, m := range modifikatoren {
		for _, t := range m.tasten {
			if t == k {
				return true
			}
		}
	}
	return false
}

func (d *desktop) taste(unten bool, k uint32) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !unten {
		delete(d.gedrueckt, k)
		return
	}
	d.gedrueckt[k] = true
	var teile []string
	if !istModifikator(k) {
		for _, m := range modifikatoren {
			for _, t := range m.tasten {
				if d.gedrueckt[t] {
					teile = append(teile, m.name)
					break
				}
			}
		}
	}
	teile = append(teile, tastenName(k))
	text := strings.Join(teile, "+")
	d.s.ereignis("taste %x %s", k, text)
	d.zeileSetzen(zeileTaste, "TASTE: "+text)
}

var maustasten = []struct {
	bit   uint8
	name  string
	farbe uint32
}{
	{1, "LINKS", 0xFF2020},
	{2, "MITTE", 0x20C020},
	{4, "RECHTS", 0x2060FF},
	{8, "RAD HOCH", 0},
	{16, "RAD RUNTER", 0},
	{32, "RAD LINKS", 0},
	{64, "RAD RECHTS", 0},
}

func (d *desktop) zeiger(k *klient, maske uint8, x, y int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	neu := maske &^ d.maske
	d.maske = maske
	for _, t := range maustasten {
		if t.farbe != 0 && maske&t.bit != 0 {
			d.fleck(x, y, t.farbe) // gedrueckt halten und ziehen malt eine Spur
		}
		if neu&t.bit == 0 {
			continue
		}
		d.s.ereignis("maus %s %d,%d", strings.ToLower(t.name), x, y)
		d.zeileSetzen(zeileMaus, fmt.Sprintf("MAUS: %s %d,%d", t.name, x, y))
		if t.bit == 4 {
			// Rechtsklick legt einen Text in die Zwischenablage aller Viewer.
			text := fmt.Sprintf("testvnc %s Klick %d,%d", time.Now().Format("15:04:05"), x, y)
			for kl := range d.klienten {
				kl.ablage = append(kl.ablage, text)
				kl.aufwecken()
			}
		}
	}
}

// fleck malt einen runden Punkt (Radius 5).
func (d *desktop) fleck(x, y int, c uint32) {
	for dy := -5; dy <= 5; dy++ {
		for dx := -5; dx <= 5; dx++ {
			if dx*dx+dy*dy <= 25 {
				d.punkt(x+dx, y+dy, c)
			}
		}
	}
	d.markieren(image.Rect(x-5, y-5, x+6, y+6))
}

func (d *desktop) ablage(text string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.s.ereignis("ablage %s", text)
	einzeilig := strings.Join(strings.Fields(text), " ")
	d.zeileSetzen(zeileAblage, "ABLAGE: "+einzeilig)
}

// groesseAendern folgt SetDesktopSize eines Viewers.
func (d *desktop) groesseAendern(von *klient, b, h int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !von.kodierungen[kodExtendedDesktopSize] {
		return
	}
	if b < minGroesse || h < minGroesse || b > maxGroesse || h > maxGroesse {
		d.s.ereignis("groesse abgelehnt %dx%d", b, h)
		von.groesseGrund, von.groesseStatus = 1, 3 // ungueltiges Layout
		von.aufwecken()
		return
	}
	d.s.ereignis("groesse %dx%d", b, h)
	d.b, d.h = b, h
	for k := range d.klienten {
		k.schmutzig = image.Rectangle{}
		k.voll = true
		k.groesseGrund, k.groesseStatus = 2, 0
	}
	von.groesseGrund = 1
	d.zeichnen()
	for k := range d.klienten {
		k.aufwecken()
	}
}

// --- Nachrichten an einen Viewer ---

// aktualisierung baut die naechste Nachricht fuer k (nil: nichts zu senden).
func (d *desktop) aktualisierung(k *klient) []byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(k.ablage) > 0 {
		text := k.ablage[0]
		k.ablage = k.ablage[1:]
		b := []byte{3, 0, 0, 0}
		b = binary.BigEndian.AppendUint32(b, uint32(len(text)))
		return append(b, text...) // nur ASCII
	}
	if !k.anfrage {
		return nil
	}
	ext := k.kodierungen[kodExtendedDesktopSize] && (!k.extGemeldet || k.groesseGrund >= 0)
	einfach := !ext && k.groesseGrund >= 0 && k.kodierungen[kodDesktopSize]
	cursor := !k.cursorGesendet && k.kodierungen[kodCursor]
	bereich := k.schmutzig
	if k.voll {
		bereich = image.Rect(0, 0, d.b, d.h)
	}
	bereich = bereich.Intersect(image.Rect(0, 0, d.b, d.h))
	if !ext && !einfach && !cursor && bereich.Empty() {
		return nil
	}
	var rechtecke [][]byte
	if ext {
		r := rechteckKopf(k.groesseGrund, k.groesseStatus, d.b, d.h, kodExtendedDesktopSize)
		if k.groesseGrund < 0 {
			r = rechteckKopf(0, 0, d.b, d.h, kodExtendedDesktopSize)
		}
		r = append(r, 1, 0, 0, 0)               // ein Bildschirm
		r = binary.BigEndian.AppendUint32(r, 0) // id
		r = append(r, 0, 0, 0, 0)               // x, y
		r = binary.BigEndian.AppendUint16(r, uint16(d.b))
		r = binary.BigEndian.AppendUint16(r, uint16(d.h))
		r = binary.BigEndian.AppendUint32(r, 0) // flags
		rechtecke = append(rechtecke, r)
	}
	if einfach {
		rechtecke = append(rechtecke, rechteckKopf(0, 0, d.b, d.h, kodDesktopSize))
	}
	if cursor {
		rechtecke = append(rechtecke, cursorRechteck(k.pf))
	}
	if !bereich.Empty() {
		r := rechteckKopf(bereich.Min.X, bereich.Min.Y, bereich.Dx(), bereich.Dy(), kodRaw)
		bpp := int(k.pf.bpp) / 8
		r = slicesGrow(r, bereich.Dx()*bereich.Dy()*bpp)
		for y := bereich.Min.Y; y < bereich.Max.Y; y++ {
			zeile := d.pix[y*d.b+bereich.Min.X : y*d.b+bereich.Max.X]
			for _, c := range zeile {
				r = k.pf.anhaengen(r, c)
			}
		}
		rechtecke = append(rechtecke, r)
	}
	k.anfrage, k.voll, k.schmutzig = false, false, image.Rectangle{}
	k.groesseGrund, k.groesseStatus = -1, 0
	if ext {
		k.extGemeldet = true
	}
	if cursor {
		k.cursorGesendet = true
	}
	aus := []byte{0, 0}
	aus = binary.BigEndian.AppendUint16(aus, uint16(len(rechtecke)))
	for _, r := range rechtecke {
		aus = append(aus, r...)
	}
	return aus
}

func slicesGrow(b []byte, n int) []byte {
	if cap(b)-len(b) >= n {
		return b
	}
	neu := make([]byte, len(b), len(b)+n)
	copy(neu, b)
	return neu
}

func rechteckKopf(x, y, b, h int, kod int32) []byte {
	r := binary.BigEndian.AppendUint16(nil, uint16(x))
	r = binary.BigEndian.AppendUint16(r, uint16(y))
	r = binary.BigEndian.AppendUint16(r, uint16(b))
	r = binary.BigEndian.AppendUint16(r, uint16(h))
	return binary.BigEndian.AppendUint32(r, uint32(kod))
}

// cursorRechteck: ein Fadenkreuz 13x13 (schwarz mit weissem Rand), Hotspot in der Mitte.
func cursorRechteck(pf pixelFormat) []byte {
	const n, mitte = 13, 6
	r := rechteckKopf(mitte, mitte, n, n, kodCursor)
	maske := make([]byte, (n+7)/8*n)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			dx, dy := x-mitte, y-mitte
			sichtbar := (dx >= -1 && dx <= 1) || (dy >= -1 && dy <= 1)
			c := uint32(0xFFFFFF)
			if dx == 0 || dy == 0 {
				c = 0x000000
			}
			r = pf.anhaengen(r, c)
			if sichtbar {
				maske[y*((n+7)/8)+x/8] |= 0x80 >> (x % 8)
			}
		}
	}
	return append(r, maske...)
}
