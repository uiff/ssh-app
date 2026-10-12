'use strict';
// Ein Terminal-Tab: xterm.js + WebSocket, mit automatischem Wiederanhaengen.

const TERM_THEMA = {
  background: '#0B0D10', foreground: '#D5D9E0', cursor: '#E9A23B', cursorAccent: '#0B0D10',
  selectionBackground: '#3A4250', black: '#1C212A', red: '#E06C5F', green: '#7FD1BF', yellow: '#E9A23B',
  blue: '#6FA8DC', magenta: '#C792EA', cyan: '#5DB8A6', white: '#D5D9E0', brightBlack: '#5A6270',
  brightRed: '#F08A7E', brightGreen: '#9FE3D3', brightYellow: '#F5C77A', brightBlue: '#9CC4EA',
  brightMagenta: '#DDB6F2', brightCyan: '#8FD6C8', brightWhite: '#FFFFFF',
};
const kodierer = new TextEncoder();

class TerminalTab {
  constructor({ hostId, sitzungId, titel }) {
    this.hostId = hostId || 0;
    this.sitzungId = sitzungId || '';
    this.titel = titel || 'Verbinde …';
    this.zustand = 'verbinde'; // verbinde | bereit | getrennt | beendet
    this.grund = '';
    this.fingerprint = '';
    this.versuche = 0;
    this.element = el('div', { class: 'term-wrap' });
    this.ueber = el('div', { class: 'term-über versteckt' });
    this.element.append(this.ueber);
    this.term = new Terminal({
      fontFamily: '"JBMono", ui-monospace, Menlo, monospace',
      fontSize: App.schriftGroesse(),
      lineHeight: 1.15,
      cursorBlink: true,
      scrollback: 5000,
      allowProposedApi: true,
      theme: TERM_THEMA,
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
    // Strg+Umschalt+C/V und Strg+K fuer die Palette
    this.term.attachCustomKeyEventHandler((e) => {
      if (e.type !== 'keydown') return true;
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k' && !e.shiftKey) { e.preventDefault(); App.palette(); return false; }
      if (e.ctrlKey && e.shiftKey && e.key.toLowerCase() === 'c') { const s = this.term.getSelection(); if (s) kopieren(s); return false; }
      if (e.ctrlKey && e.shiftKey && e.key.toLowerCase() === 'v') {
        navigator.clipboard.readText().then((t) => this.einfuegen(t)).catch(() => toast('Einfügen nicht erlaubt', 'fehler'));
        return false;
      }
      return true;
    });
    this.verbinden();
  }

  // ---------- Verbindung ----------
  verbinden() {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    let q;
    if (this.sitzungId) q = 'sitzung=' + encodeURIComponent(this.sitzungId);
    else q = 'host=' + this.hostId + '&c=' + (this.term.cols || 80) + '&r=' + (this.term.rows || 24);
    this.zustandSetzen('verbinde', this.sitzungId ? 'Verbinde neu …' : 'Verbinde …');
    const ws = new WebSocket(proto + '//' + location.host + '/api/ws?' + q);
    ws.binaryType = 'arraybuffer';
    this.ws = ws;
    this.ersteDaten = true;
    ws.onmessage = (ev) => {
      if (ws !== this.ws) return;
      if (typeof ev.data !== 'string') {
        if (this.ersteDaten && this.wiederanhaengen) { this.term.reset(); }
        this.ersteDaten = false;
        this.term.write(new Uint8Array(ev.data));
        return;
      }
      let n; try { n = JSON.parse(ev.data); } catch (e) { return; }
      this.nachricht(n);
    };
    ws.onclose = () => {
      if (ws !== this.ws) return;
      clearInterval(this.pingTimer);
      if (this.zustand === 'beendet') return;
      if (!this.sitzungId) { // Aufbau gescheitert
        if (this.zustand !== 'fehler') this.zustandSetzen('fehler', this.grund || 'Verbindung fehlgeschlagen');
        return;
      }
      this.zustandSetzen('getrennt', 'Verbindung unterbrochen — verbinde neu …');
      const warte = Math.min(15000, 800 * Math.pow(1.7, this.versuche++));
      clearTimeout(this.neuTimer);
      this.neuTimer = setTimeout(() => { if (this.zustand === 'getrennt') this.verbinden(); }, warte);
    };
  }

  nachricht(n) {
    switch (n.t) {
      case 'status':
        this.zustandSetzen('verbinde', n.text);
        break;
      case 'hostkey':
        App.hostkeyFragen(n).then((ok) => this.senden({ t: 'hostkey', ok }));
        break;
      case 'anmeldung':
        App.anmeldungFragen(n).then((a) => this.senden(Object.assign({ t: 'anmeldung' }, a || { abbrechen: true })));
        break;
      case 'bereit':
        this.wiederanhaengen = !!this.sitzungId;
        this.sitzungId = n.id;
        this.titel = n.titel;
        this.hostId = n.hostId || this.hostId;
        this.fingerprint = n.fp || '';
        this.versuche = 0;
        this.zustandSetzen('bereit');
        this.anpassen(true);
        clearInterval(this.pingTimer);
        this.pingTimer = setInterval(() => this.senden({ t: 'ping' }), 25000);
        App.tabsZeichnen();
        if (App.aktiv === this && !App.istTelefon()) this.term.focus();
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
    ersetzen(this.ueber);
    if (z === 'bereit') {
      this.ueber.classList.add('versteckt');
    } else {
      const knoepfe = [];
      if (z === 'beendet' || z === 'fehler') {
        if (this.hostId) knoepfe.push(el('button', { class: 'btn klein primaer', type: 'button', onclick: () => App.neuVerbinden(this) }, 'Neu verbinden'));
        knoepfe.push(el('button', { class: 'btn klein', type: 'button', onclick: () => App.tabSchliessen(this) }, 'Schliessen'));
      }
      this.ueber.append(
        icon(z === 'fehler' ? 'warnung' : z === 'beendet' ? 'terminal' : 'neu'),
        el('div', { style: { display: 'flex', flexDirection: 'column', gap: '10px' } },
          el('span', null, text || this.grund),
          knoepfe.length ? el('div', { class: 'reihe' }, knoepfe) : null));
      this.ueber.classList.remove('versteckt');
    }
    App.tabsZeichnen && App.tabsZeichnen();
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
    this.sendenRoh(kodierer.encode(d));
    if (App.rundfunk && App.aktiv === this) App.rundfunkSenden(this, d);
  }

  einfuegen(text) {
    if (!text) return;
    // Bracketed Paste, falls die Shell es verlangt hat.
    const b = this.term.modes && this.term.modes.bracketedPasteMode;
    this.tippen(b ? '\x1b[200~' + text + '\x1b[201~' : text);
    this.term.focus();
  }

  anpassen(melden) {
    if (!this.element.isConnected || this.element.offsetParent === null) return;
    const vorher = this.term.cols + 'x' + this.term.rows;
    try { this.fit.fit(); } catch (e) { return; }
    const nachher = this.term.cols + 'x' + this.term.rows;
    if (melden || vorher !== nachher) this.senden({ t: 'groesse', c: this.term.cols, r: this.term.rows });
  }

  // Text der aktuellen Eingabezeile ohne Prompt (fuer die Hilfe).
  aktuelleZeile() {
    const buf = this.term.buffer.active;
    const zeile = buf.getLine(buf.baseY + buf.cursorY);
    if (!zeile) return '';
    let t = zeile.translateToString(true).slice(0, buf.cursorX);
    const m = t.match(/^.*?[$#%>❯]\s/);
    if (m) {
      // letztes Prompt-Zeichen nehmen
      const i = Math.max(t.lastIndexOf('$ '), t.lastIndexOf('# '), t.lastIndexOf('% '), t.lastIndexOf('> '));
      t = i >= 0 ? t.slice(i + 2) : t.slice(m[0].length);
    } else {
      return '';
    }
    return t.trim();
  }

  schliessen() {
    this.zustand = 'beendet';
    clearTimeout(this.neuTimer);
    clearInterval(this.pingTimer);
    if (this.ws) this.ws.close();
    this.term.dispose();
    this.element.remove();
  }
}
App.TerminalTab = TerminalTab;
