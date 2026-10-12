'use strict';
// Ein Terminal-Tab: xterm.js + WebSocket, mit automatischem Wiederanhaengen.

// ANSI-Palette des Terminals (Farben der Programme, kein Akzent). Cursor und Markierung kommen aus
// der gewaehlten Akzentfarbe (thema.js), damit sie zur Oberflaeche passen.
const TERM_FARBEN = {
  background: '#0B0D10', foreground: '#D5D9E0',
  black: '#1C212A', red: '#E06C5F', green: '#7FD1BF', yellow: '#E9A23B',
  blue: '#6FA8DC', magenta: '#C792EA', cyan: '#5DB8A6', white: '#D5D9E0', brightBlack: '#5A6270',
  brightRed: '#F08A7E', brightGreen: '#9FE3D3', brightYellow: '#F5C77A', brightBlue: '#9CC4EA',
  brightMagenta: '#DDB6F2', brightCyan: '#8FD6C8', brightWhite: '#FFFFFF',
};
function termThema() { return Object.assign({}, TERM_FARBEN, akzentTerm(akzentFuer(akzentGemerkt()))); }

const kodierer = new TextEncoder();
// Close-Codes des Servers (siehe ws.go)
const ZU_ABGEMELDET = 4001; // Anmeldung abgelaufen: nicht neu verbinden, zur Anmeldung
const ZU_ABGELOEST = 4002; // Sitzung in einem anderen Fenster geoeffnet: nicht zurueckholen
const PING_ALLE = 25000; // Lebenszeichen
const PONG_FRIST = 10000; // ohne Antwort gilt die Verbindung als tot (halb offen nach Netzwechsel)

class TerminalTab {
  constructor({ hostId, sitzungId, titel }) {
    this.hostId = hostId || 0;
    this.sitzungId = sitzungId || '';
    this.titel = titel || 'Verbinde …';
    this.zustand = 'verbinde'; // verbinde | bereit | getrennt | fehler | beendet | abgeloest
    this.grund = '';
    this.fingerprint = '';
    this.versuche = 0;
    this.eingabeStart = null; // wo die gerade getippte Eingabe beginnt (fuer die Hilfe)
    this.element = el('div', { class: 'term-wrap' });
    this.ueber = el('div', { class: 'term-ueber versteckt', role: 'status' });
    this.element.append(this.ueber);
    this.term = new Terminal({
      fontFamily: '"JBMono", ui-monospace, Menlo, monospace',
      fontSize: App.schriftGroesse(),
      lineHeight: 1.15,
      cursorBlink: true,
      scrollback: 5000,
      allowProposedApi: true,
      theme: termThema(),
      macOptionIsMeta: true,
      rightClickSelectsWord: true,
    });
    this.fit = new FitAddon.FitAddon();
    this.term.loadAddon(this.fit);
    this.term.loadAddon(new WebLinksAddon.WebLinksAddon());
    this.suche = new SearchAddon.SearchAddon();
    this.term.loadAddon(this.suche);
    const uni = new Unicode11Addon.Unicode11Addon();
    this.term.loadAddon(uni);
    this.term.unicode.activeVersion = '11';
    this.term.open(this.element);
    this.term.onData((d) => this.tippen(d));
    this.term.onBinary((d) => this.sendenRoh(Uint8Array.from(d, (c) => c.charCodeAt(0))));
    this.term.onCursorMove(() => App.hilfeBeiEingabe && App.hilfeBeiEingabe(this));
    this.term.attachCustomKeyEventHandler((e) => {
      if (e.type !== 'keydown') return true;
      const k = e.key.toLowerCase();
      // Palette: Strg+Umschalt+K (und Cmd+K). Strg+K allein gehoert der Shell (nano: Ausschneiden,
      // bash: Zeilenrest loeschen). Die Palette oeffnet der globale Tastenhandler in app.js.
      if (k === 'k' && ((e.ctrlKey && e.shiftKey) || (e.metaKey && !e.ctrlKey))) { e.preventDefault(); return false; }
      if (e.ctrlKey && e.shiftKey && k === 'c') { const s = this.term.getSelection(); if (s) kopieren(s); return false; }
      if (e.ctrlKey && e.shiftKey && k === 'v') {
        // Ohne Clipboard-API (http ueber die LAN-Adresse) fuegt der Browser selbst ein (paste-Ereignis).
        if (!navigator.clipboard || !navigator.clipboard.readText) return false;
        e.preventDefault();
        navigator.clipboard.readText().then((t) => this.einfuegen(t)).catch(() => toast('Einfügen nicht erlaubt — mit Strg+V geht es auch', 'fehler'));
        return false;
      }
      return true;
    });
    this.verbinden();
  }

  // ---------- Verbindung ----------
  // automatisch: Wiederverbinden nach einem Abriss (nicht vom Anwender ausgeloest).
  verbinden(automatisch) {
    clearTimeout(this.neuTimer);
    if (this.geschlossen) return;
    this.automatisch = !!automatisch;
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    let q;
    if (this.sitzungId) q = 'sitzung=' + encodeURIComponent(this.sitzungId);
    else q = 'host=' + this.hostId + '&c=' + (this.term.cols || 80) + '&r=' + (this.term.rows || 24);
    // Ein WebSocket kann keinen eigenen Kopf senden: auto=1 statt X-SSH-App-Auto. Das automatische
    // Wiederverbinden verlaengert die Anmeldung so nicht (V3); Klick und «Hierher holen» schon.
    if (automatisch) q += '&auto=1';
    this.abgemeldetGemeldet = false;
    this.zustandSetzen('verbinde', this.sitzungId ? 'Verbinde neu …' : 'Verbinde …');
    const ws = new WebSocket(proto + '//' + location.host + '/api/ws?' + q);
    ws.binaryType = 'arraybuffer';
    this.ws = ws;
    ws.onmessage = (ev) => {
      if (ws !== this.ws) return;
      if (typeof ev.data !== 'string') { this.term.write(new Uint8Array(ev.data)); return; }
      let n; try { n = JSON.parse(ev.data); } catch (e) { return; }
      this.nachricht(n);
    };
    ws.onclose = (ev) => { if (ws === this.ws) this.verbindungZu(ev.code); };
  }

  // Die WebSocket ist zu oder gilt als tot. code: Close-Code des Servers (0 = selbst aufgegeben).
  verbindungZu(code) {
    clearInterval(this.pingTimer);
    clearTimeout(this.pongTimer);
    this.ws = null;
    if (this.geschlossen || this.zustand === 'beendet' || this.zustand === 'abgeloest') return;
    if (code === ZU_ABGEMELDET || this.abgemeldetGemeldet) {
      this.zustandSetzen('getrennt', 'Abgemeldet');
      App.abgemeldet();
      return;
    }
    if (code === ZU_ABGELOEST) { this.abgeloest(); return; }
    if (!this.sitzungId) { // Aufbau gescheitert
      if (this.zustand !== 'fehler') this.zustandSetzen('fehler', this.grund || 'Verbindung fehlgeschlagen');
      return;
    }
    this.zustandSetzen('getrennt', 'Verbindung unterbrochen — verbinde neu …');
    this.spaeterNeu();
  }

  spaeterNeu() {
    const warte = Math.min(15000, 800 * Math.pow(1.7, this.versuche++));
    clearTimeout(this.neuTimer);
    this.neuTimer = setTimeout(() => this.neuVersuchen(), warte);
  }

  // Vor jedem automatischen Wiederverbinden: noch angemeldet? Sonst zur Anmeldung statt Endlos-Schleife.
  // vomBenutzer: Knopf «Jetzt verbinden» (zaehlt als Bedienung), sonst Zeitgeber/Sichtbarkeit.
  async neuVersuchen(vomBenutzer) {
    if (this.geschlossen || this.zustand !== 'getrennt') return;
    let s;
    try { s = await api('GET', '/api/status', undefined, { auto: true }); }
    catch (e) { this.spaeterNeu(); return; } // Server (noch) nicht erreichbar: weiter warten
    if (this.geschlossen || this.zustand !== 'getrennt') return;
    if (!s.angemeldet) { App.abgemeldet(); return; }
    this.verbinden(!vomBenutzer);
  }

  // Sofort neu verbinden (Knopf: vomBenutzer, Rueckkehr aus dem Hintergrund: automatisch).
  sofortNeu(vomBenutzer) {
    if (this.geschlossen || this.zustand !== 'getrennt') return;
    clearTimeout(this.neuTimer);
    this.versuche = 0;
    this.neuVersuchen(vomBenutzer);
  }

  // Lebenszeichen: bleibt die Antwort aus, Verbindung aufgeben und neu anhaengen.
  lebenszeichen(frist) {
    const ws = this.ws;
    if (!ws || ws.readyState !== 1 || this.zustand !== 'bereit') return;
    this.senden({ t: 'ping' });
    clearTimeout(this.pongTimer);
    this.pongTimer = setTimeout(() => {
      if (ws !== this.ws) return;
      try { ws.close(); } catch (e) { /* schon zu */ }
      this.verbindungZu(0);
    }, frist || PONG_FRIST);
  }

  // Ein anderes Fenster hat die Sitzung uebernommen: nicht automatisch zurueckholen (sonst Ping-Pong).
  abgeloest() {
    clearTimeout(this.neuTimer);
    clearInterval(this.pingTimer);
    clearTimeout(this.pongTimer);
    const ws = this.ws;
    this.ws = null;
    if (ws) { try { ws.close(); } catch (e) { /* schon zu */ } }
    this.zustandSetzen('abgeloest', 'In einem anderen Fenster geöffnet');
  }

  nachricht(n) {
    switch (n.t) {
      case 'status':
        this.zustandSetzen('verbinde', n.text);
        break;
      case 'hostkey':
        App.hostkeyFragen(n, this).then((ok) => this.senden({ t: 'hostkey', ok }));
        break;
      case 'anmeldung':
        App.anmeldungFragen(n, this).then((a) => this.senden(Object.assign({ t: 'anmeldung' }, a || { abbrechen: true })));
        break;
      case 'bereit': {
        this.rueckfrageZu();
        // Wiederanhaengen: Der Server schickt den Puffer erst nach «bereit» — also jetzt zuruecksetzen,
        // dann zeichnen die folgenden Binaerframes das Bild neu (ohne doppelten oder vertauschten Inhalt).
        const wieder = n.wieder !== undefined ? !!n.wieder : !!this.sitzungId;
        if (wieder) { this.term.reset(); this.eingabeStart = null; }
        this.sitzungId = n.id;
        this.titel = n.titel;
        this.hostId = n.hostId || this.hostId;
        this.fingerprint = n.fp || '';
        this.versuche = 0;
        this.zustandSetzen('bereit');
        // Groesse melden zaehlt beim Server als Bedienung: beim automatischen Wiederanhaengen nur,
        // wenn sie sich seit der letzten Meldung geaendert hat (sonst verlaengert jedes Wiederverbinden
        // die Anmeldung). Sonst immer — ein anderes Geraet kann die Groesse inzwischen geaendert haben.
        this.anpassen(!(wieder && this.automatisch));
        clearInterval(this.pingTimer);
        this.pingTimer = setInterval(() => this.lebenszeichen(PONG_FRIST), PING_ALLE);
        App.tabsZeichnen();
        if (App.aktiv === this && !App.istTelefon() && !dialogStapel.length) this.term.focus();
        if (n.gespeichert) { App.datenLaden(); toast('Zugangsdaten gespeichert'); }
        break;
      }
      case 'pong':
        clearTimeout(this.pongTimer);
        break;
      case 'abgemeldet':
        this.abgemeldetGemeldet = true; // der Server schliesst gleich mit 4001
        break;
      case 'abgeloest':
        this.abgeloest();
        break;
      case 'fehler':
        this.grund = n.text;
        this.zustandSetzen('fehler', n.text);
        break;
      case 'ende':
        this.grund = n.text;
        this.zustandSetzen('beendet', n.text);
        break;
    }
  }

  zustandSetzen(z, text) {
    this.zustand = z;
    if (text) this.grund = text;
    if (z === 'fehler' || z === 'beendet' || z === 'abgeloest') this.rueckfrageZu();
    ersetzen(this.ueber);
    if (z === 'bereit') {
      this.ueber.classList.add('versteckt');
    } else {
      const knoepfe = [];
      if (z === 'beendet' || z === 'fehler') {
        if (this.hostId) knoepfe.push(el('button', { class: 'btn klein primaer', type: 'button', onclick: () => App.neuVerbinden(this) }, icon('neu', 16), 'Neu verbinden'));
        knoepfe.push(el('button', { class: 'btn klein', type: 'button', onclick: () => App.tabSchliessen(this) }, 'Schliessen'));
      } else if (z === 'abgeloest') {
        knoepfe.push(el('button', { class: 'btn klein primaer', type: 'button', onclick: () => { this.versuche = 0; this.verbinden(); } }, 'Hierher holen'));
        knoepfe.push(el('button', { class: 'btn klein', type: 'button', title: 'Die Sitzung läuft im anderen Fenster weiter', onclick: () => App.tabEntfernen(this) }, 'Hier schliessen'));
      } else if (z === 'getrennt' && this.sitzungId && text !== 'Abgemeldet') {
        knoepfe.push(el('button', { class: 'btn klein', type: 'button', onclick: () => this.sofortNeu(true) }, 'Jetzt verbinden'));
      }
      this.ueber.className = 'term-ueber' + (z === 'fehler' ? ' fehler' : '');
      this.ueber.append(
        icon(z === 'fehler' ? 'warnung' : z === 'beendet' ? 'terminal' : z === 'abgeloest' ? 'fenster' : 'neu', 22),
        el('div', { class: 'term-ueber-text' },
          el('span', null, text || this.grund),
          z === 'abgeloest' ? el('small', null, 'Die Sitzung läuft dort weiter. «Hierher holen» zeigt sie wieder in diesem Fenster.') : null,
          knoepfe.length ? el('div', { class: 'reihe' }, knoepfe) : null));
    }
    App.tabsZeichnen && App.tabsZeichnen();
    // Dateileiste: Knoepfe sperren/freigeben, nach dem (Wieder-)Verbinden die Liste laden.
    if (App.dateien) App.dateien.zustandGeaendert(this);
  }

  // Offene Rueckfrage (Host-Schluessel, Anmeldung) schliessen: Die Antwort ginge ins Leere.
  rueckfrageZu() {
    const r = this.rueckfrage;
    this.rueckfrage = null;
    if (r) r.zu();
  }

  senden(obj) {
    if (this.ws && this.ws.readyState === 1) this.ws.send(JSON.stringify(obj));
  }
  sendenRoh(bytes) {
    if (this.ws && this.ws.readyState === 1 && this.zustand === 'bereit') this.ws.send(bytes);
  }

  // Tastatureingabe (beachtet die Strg/Alt-Tasten der Telefon-Leiste und «An alle senden»).
  tippen(d) {
    d = App.modifikator(d);
    this.startMerken(d);
    this.sendenRoh(kodierer.encode(d));
    if (App.rundfunk && App.aktiv === this) App.rundfunkSenden(this, d);
  }

  // Text einfuegen wie Strg+V: mit Bracketed Paste, falls das Programm es verlangt hat.
  // opt.nurHier: nicht zusaetzlich an die anderen Tabs (Rundfunk); opt.ohneFokus: Fokus nicht holen.
  einfuegen(text, opt) {
    if (!text) return;
    const o = opt || {};
    this.startMerken(text);
    this.sendenRoh(kodierer.encode(this.geklammert(text)));
    if (!o.nurHier && App.rundfunk && App.aktiv === this) {
      for (const t of App.tabs) if (t !== this && t.zustand === 'bereit') t.sendenRoh(kodierer.encode(t.geklammert(text)));
    }
    if (!o.ohneFokus) this.term.focus();
  }
  geklammert(text) {
    return this.term.modes && this.term.modes.bracketedPasteMode ? '\x1b[200~' + text + '\x1b[201~' : text;
  }

  // Groesse an das Fenster anpassen und dem Server melden, wenn sie neu ist (melden: in jedem Fall).
  anpassen(melden) {
    if (!this.element.isConnected || this.element.offsetParent === null) return;
    try { this.fit.fit(); } catch (e) { return; }
    const g = this.term.cols + 'x' + this.term.rows;
    if ((melden || g !== this.gemeldet) && this.ws && this.ws.readyState === 1) {
      this.senden({ t: 'groesse', c: this.term.cols, r: this.term.rows });
      this.gemeldet = g;
    }
  }

  // ---------- Aktuelle Eingabe (fuer die Befehlshilfe) ----------

  // Merkt beim ersten getippten Zeichen, wo die Eingabe beginnt (der Cursor steht dann direkt
  // hinter dem Prompt). Enter, Strg+C/D/L beginnen eine neue Zeile.
  startMerken(d) {
    if (/[\r\n\x03\x04\x0c]/.test(d)) { this.eingabeStart = null; return; }
    if (this.eingabeStart) return;
    const c = d.replace(/^\x1b\[200~/, '').charCodeAt(0);
    if (!(c >= 32 && c !== 127)) return;
    const buf = this.term.buffer.active;
    if (buf.type !== 'normal') return; // Vollbildprogramme (vi, top) haben keine Befehlszeile
    this.eingabeStart = { y: buf.baseY + buf.cursorY, x: buf.cursorX };
  }

  // Text der aktuellen Eingabezeile ohne Prompt. Vom Terminal umgebrochene Zeilen gehoeren dazu.
  aktuelleZeile() {
    const buf = this.term.buffer.active;
    if (buf.type !== 'normal') return '';
    const cy = buf.baseY + buf.cursorY, cx = buf.cursorX;
    let y0 = cy;
    while (y0 > 0 && buf.getLine(y0) && buf.getLine(y0).isWrapped) y0--;
    const s = this.eingabeStart;
    if (s && s.y >= y0 && (s.y < cy || (s.y === cy && s.x <= cx))) return this.textZwischen(buf, s.y, s.x, cy, cx).trim();
    // Ohne gemerkten Anfang: das erste Prompt-Zeichen mit Leerzeichen am Zeilenanfang.
    // «>» nach einem Leerzeichen ist eher eine Umleitung (ls > datei) als ein Prompt.
    const t = this.textZwischen(buf, y0, 0, cy, cx);
    for (let i = 0; i < t.length - 1 && i < 200; i++) {
      const z = t[i];
      if (!/\s/.test(t[i + 1])) continue;
      if ('$#%❯»'.includes(z) || (z === '>' && (i === 0 || t[i - 1] !== ' '))) return t.slice(i + 2).trim();
    }
    return '';
  }

  textZwischen(buf, y1, x1, y2, x2) {
    let t = '';
    for (let y = y1; y <= y2; y++) {
      const zeile = buf.getLine(y);
      if (!zeile) break;
      t += zeile.translateToString(y === y2, y === y1 ? x1 : 0, y === y2 ? x2 : undefined);
    }
    return t;
  }

  schliessen() {
    this.geschlossen = true;
    this.zustand = 'beendet';
    clearTimeout(this.neuTimer);
    clearInterval(this.pingTimer);
    clearTimeout(this.pongTimer);
    this.rueckfrageZu();
    const ws = this.ws;
    this.ws = null;
    if (ws) { try { ws.close(); } catch (e) { /* schon zu */ } }
    this.term.dispose();
    this.element.remove();
  }
}
App.TerminalTab = TerminalTab;
