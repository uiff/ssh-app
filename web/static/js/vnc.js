'use strict';
// Bildschirm-Tab (VNC) mit noVNC. Keyway meldet sich selbst am VNC-Server an — ueber SSH getunnelt
// oder direkt (/api/vnc, Rueckfragen als Text-JSON) — und reicht danach das RFB-Protokoll 1:1 an
// noVNC durch. Die Klasse hat dieselbe Form wie App.TerminalTab (element, titel, hostId, zustand,
// anpassen, schliessen, aktuelleZeile), dazu art = 'vnc': Tabs, Tab-Wahl am Telefon, Schliessen,
// «Neu verbinden» und Statusleiste behandeln beide gleich. Eine VNC-Verbindung endet mit dem Tab:
// Nach dem Neuladen gibt es kein Wiederanhaengen.
// Dazu Vorschaubilder (nur in diesem Browser, localStorage) fuer Startseite, Host-Liste und Palette.

const VNC_VORSCHAU_ALLE = 30000;
const VNC_VORSCHAU_BREITE = 320;
const VNC_VORSCHAU_MAX = 40000; // Zeichen der Data-URL (etwa 30 KB Bild)
const VNC_ZU_ABGEMELDET = 4001; // Close-Code des Servers: Anmeldung abgelaufen (wie /api/ws)
// Qualitaet: [JPEG-Qualitaet, Kompression] (je 0–9) fuer noVNC; wirkt bei Tight/JPEG, nicht bei Raw.
const VNC_QUALITAET = { hoch: [8, 2], mittel: [6, 5], niedrig: [3, 8] };
const VNC_STUFEN = ['auto', 'hoch', 'mittel', 'niedrig'];
const VNC_ANSICHTEN = [
  { id: 'anpassen', icon: 'anpassen' },
  { id: 'original', icon: 'original' },
  { id: 'fern', icon: 'aufloesung' },
];
// Unsichtbares Eingabefeld fuer die Bildschirmtastatur (Telefon): Es haelt immer etwas Text, damit
// auch die Loeschtaste ein «input» ausloest (Vorgehen wie in noVNC).
const VNC_EINGABE_LAENGE = 100;

// ---------- noVNC laden ----------
// noVNC ist in ES-Modulen geschrieben; js/vnc-rfb.mjs stellt es als window.KeywayRFB bereit. Geladen
// wird es erst, wenn es gebraucht wird (erster Bildschirm-Tab, Startseite mit VNC-Host).
let vncLaden = null;
function rfbLaden() {
  if (window.KeywayRFB) return Promise.resolve(window.KeywayRFB);
  if (!vncLaden) {
    vncLaden = new Promise((resolve, reject) => {
      const fertig = () => resolve(window.KeywayRFB);
      window.addEventListener('keyway-rfb', fertig, { once: true });
      const s = el('script', { type: 'module', src: 'js/vnc-rfb.mjs', onerror: () => {
        window.removeEventListener('keyway-rfb', fertig);
        s.remove();
        vncLaden = null; // spaeter noch einmal versuchen
        reject(new Error('novnc'));
      } });
      document.head.append(s);
    });
  }
  return vncLaden;
}

// «Automatisch»: nach der Verbindung des Browsers (falls bekannt) und dem Geraet.
function vncAutoStufe() {
  const c = navigator.connection;
  if (c && (c.saveData || /(^|-)2g$|^3g$/.test(c.effectiveType || ''))) return 'niedrig';
  if (App.istTelefon() || (c && c.type === 'cellular')) return 'mittel';
  return 'hoch';
}

// ---------- Vorschaubilder (V4) ----------
// Speichern, Lesen, Einstellung «Vorschaubilder speichern» und Loeschen gehoeren App.vorschau
// (tresor.js, localStorage «sshapp.vnc.vorschau.<hostId>»). Hier entstehen die Bilder und die Karten.

function vncVorschauLesen(hostId) { return App.vorschau && hostId ? App.vorschau.lesen(hostId) : ''; }

// Vorschaubild als Element (Bild oder Platzhalter mit Bildschirm-Symbol) fuer Karten und Listen.
// klasse: 'mini' (Trefferliste, 64 × 40) oder 'gross' (Detail, volle Breite), siehe vnc.css.
App.vncVorschauBild = function (host, klasse) {
  const url = vncVorschauLesen(host && host.id);
  return el('span', { class: 'vnc-vorschau' + (url ? '' : ' leer') + (klasse ? ' ' + klasse : '') },
    url ? el('img', { src: url, alt: t('vnc.start.vorschau', { name: host.name }), decoding: 'async', draggable: 'false' })
      : [icon('bildschirm', 28), el('small', null, t('vnc.start.keineVorschau'))]);
};

// Karte der Startseite fuer einen Host mit VNC: Vorschaubild, Name, Knoepfe Terminal und Bildschirm.
App.vncKarte = function (h) {
  const bildschirm = () => App.verbinden(h, { art: 'vnc' });
  const titel = t('vnc.start.bildschirmTitel', { name: h.name });
  return el('div', { class: 'karte vnc-karte', 'data-host-id': h.id },
    el('button', { class: 'vnc-karte-bild', type: 'button', title: titel, 'aria-label': titel, onclick: bildschirm }, App.vncVorschauBild(h)),
    el('div', { class: 'vnc-karte-text' },
      App.hostSymbol(h, 18),
      el('span', { class: 'haupt' }, el('b', null, h.name), el('small', null, hostZeile(h)))),
    el('div', { class: 'vnc-karte-knoepfe' },
      el('button', { class: 'btn klein', type: 'button', title: t('vnc.start.terminalTitel', { name: h.name }), onclick: () => App.verbinden(h) },
        icon('terminal', 16), t('vnc.start.terminal')),
      el('button', { class: 'btn klein primaer', type: 'button', title: titel, onclick: bildschirm }, icon('bildschirm', 16), t('vnc.start.bildschirm'))));
};

// Neues Vorschaubild: Karten der Startseite an Ort und Stelle nachfuehren (ohne die Seite neu zu
// zeichnen — Fokus und Bildlauf bleiben).
function vncKartenNachfuehren(hostId) {
  const host = App.daten.hosts.find((h) => h.id === hostId);
  if (!host) return;
  for (const k of document.querySelectorAll('.vnc-karte[data-host-id="' + Number(hostId) + '"] .vnc-karte-bild')) ersetzen(k, App.vncVorschauBild(host));
}

// Touch als Hauptbedienung (Telefon, Tablet): Tastatur-Knopf zeigen, Tippen aufs Bild laesst die
// Bildschirmtastatur offen.
const vncTouchAbfrage = window.matchMedia('(pointer: coarse)');
function vncTouch() { return App.istTelefon() || vncTouchAbfrage.matches; }

// noVNC vorab laden (Startseite zeigt einen VNC-Host): Der erste Bildschirm-Tab startet schneller.
App.vncVorladen = function () {
  if (window.KeywayRFB || vncLaden) return;
  const los = () => rfbLaden().catch(() => { /* beim Verbinden noch einmal */ });
  if (window.requestIdleCallback) requestIdleCallback(los, { timeout: 3000 }); else setTimeout(los, 1500);
};

// ---------- Bildschirm-Tab ----------

class VncTab {
  constructor({ hostId, titel }) {
    this.art = 'vnc';
    this.hostId = hostId || 0;
    this.titel = titel || t('vnc.verbinde');
    this.zustand = 'verbinde'; // verbinde | bereit | beendet | fehler (kein «getrennt»: kein Neuverbinden von selbst)
    this.grund = '';
    this.sitzungId = ''; // keine Server-Sitzung (kein Wiederanhaengen, kein DELETE beim Schliessen)
    this.fingerprint = '';
    this.rfb = null;
    this.ws = null;
    this.ablageText = ''; // zuletzt vom Geraet empfangen
    this.ablageNeu = false;
    this.ablageEntwurf = '';
    this.verschieben = true; // Telefon in Originalgroesse: Ziehen verschiebt das Bild
    this.leisteZu = lokal('vncLeisteZu', false) === true;
    const g = lokal('vncEinst.' + this.hostId, {}) || {};
    this.einst = {
      skalierung: VNC_ANSICHTEN.some((a) => a.id === g.skalierung) ? g.skalierung : 'anpassen',
      qualitaet: VNC_STUFEN.includes(g.qualitaet) ? g.qualitaet : 'auto',
      nurAnsehen: g.nurAnsehen === true,
    };
    // Code ausserhalb (Palette, Befehlshilfe) ruft beim Schliessen App.aktiv.term.focus() auf.
    this.term = { focus: () => this.fokus() };

    this.ziel = el('div', { class: 'vnc-ziel' });
    this.ueber = el('div', { class: 'term-ueber versteckt', role: 'status' });
    this.flaeche = el('div', { class: 'vnc-flaeche' }, this.ziel, this.ueber);
    this.leiste = el('div', { class: 'vnc-leiste', role: 'toolbar', 'aria-label': t('vnc.leiste.name') });
    this.unten = el('div', { class: 'vnc-unten', role: 'toolbar', 'aria-label': t('vnc.leiste.name') });
    this.griff = el('button', { class: 'vnc-griff', type: 'button', 'aria-label': t('vnc.leiste.auf'), title: t('vnc.leiste.auf'),
      onpointerdown: (e) => e.preventDefault(), onclick: () => this.leisteSetzen(false) }, icon('chevronHoch', 20));
    this.eingabe = el('textarea', { class: 'vnc-eingabe', rows: '1', tabindex: '-1', 'aria-label': t('vnc.tastaturFeld'),
      autocomplete: 'off', autocorrect: 'off', autocapitalize: 'off', spellcheck: 'false',
      oninput: () => this.eingabeText(), onkeydown: (e) => this.eingabeTaste(e),
      onfocus: () => this.leistenZeichnen(), onblur: () => this.leistenZeichnen() });
    this.element = el('div', { class: 'vnc-wrap' + (this.leisteZu ? ' leiste-zu' : '') }, this.leiste, this.flaeche, this.unten, this.griff, this.eingabe);
    this.vollbildWechsel = () => this.vollbildGeaendert();
    document.addEventListener('fullscreenchange', this.vollbildWechsel);
    document.addEventListener('webkitfullscreenchange', this.vollbildWechsel);
    this.warTelefon = App.istTelefon();
    this.zustandSetzen('verbinde', t('vnc.verbinde'));
    this.verbinden();
  }

  // ---------- Verbindung ----------

  async verbinden() {
    if (this.geschlossen) return;
    this.abbauen();
    this.verbundenSeit = 0;
    this.durchreichen = false;
    this.sicherheitsFehler = null;
    this.vomBenutzer = false;
    this.abgemeldetGemeldet = false;
    this.endeText = '';
    this.pwSpeichern = null; // Wahl «speichern» der letzten Passwort-Rueckfrage (nur in diesem Aufbau)
    this.zuCode = 0;
    this.fernGeht = true;
    this.fernName = '';
    this.ablageNeu = false;
    this.flaeche.classList.remove('mit-bild');
    this.flaeche.style.backgroundImage = '';
    this.zustandSetzen('verbinde', t('vnc.verbinde'));
    try { await rfbLaden(); } catch (e) {
      if (!this.geschlossen && this.zustand === 'verbinde') this.zustandSetzen('fehler', t('vnc.ladenFehler'));
      return;
    }
    if (this.geschlossen || this.zustand !== 'verbinde' || this.ws) return;
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    // Ein WebSocket kann keinen eigenen Kopf senden: Die Sprache der Meldungen geht als Parameter mit.
    const ws = new WebSocket(proto + '//' + location.host + '/api/vnc?host=' + encodeURIComponent(this.hostId) + '&sprache=' + encodeURIComponent(I18N.sprache));
    ws.binaryType = 'arraybuffer';
    this.ws = ws;
    // Zuerst registriert, laeuft vor onclose (auch nachdem noVNC onclose uebernommen hat).
    ws.addEventListener('close', (ev) => { if (ws === this.ws) this.zuCode = ev.code; });
    // Eigene Text-Nachrichten ueber addEventListener: noVNC ersetzt bei «bereit» ws.onmessage, Text
    // nach «bereit» (ende, abgemeldet) kommt so trotzdem an. Binaerframes gehoeren noVNC.
    ws.addEventListener('message', (ev) => {
      if (ws !== this.ws || typeof ev.data !== 'string') return;
      let n;
      try { n = JSON.parse(ev.data); } catch (e) { return; }
      this.nachricht(n);
    });
    ws.onclose = (ev) => { if (ws === this.ws && !this.rfb) this.aufbauZu(ev.code); };
  }

  // Die WebSocket ging im Aufbau zu (vor «bereit»).
  async aufbauZu(code) {
    this.ws = null;
    this.rueckfrageZu();
    if (this.geschlossen || this.zustand !== 'verbinde') return; // Fehler schon gemeldet oder abgebrochen
    if (code === VNC_ZU_ABGEMELDET || this.abgemeldetGemeldet) { this.abgemeldet(); return; }
    // Ohne Meldung: abgelaufene Anmeldung? (Dann lehnt der Server schon den Aufbau ab.)
    try {
      const s = await api('GET', '/api/status', undefined, { auto: true });
      if (s && !s.angemeldet) { this.abgemeldet(); return; }
    } catch (e) { /* Server nicht erreichbar */ }
    if (!this.geschlossen && this.zustand === 'verbinde') this.zustandSetzen('fehler', t('vnc.fehlgeschlagen'));
  }

  abgemeldet() {
    this.istAbgemeldet = true;
    this.zustandSetzen('beendet', t('vnc.abgemeldet'));
    App.abgemeldet();
  }

  senden(obj) {
    if (this.ws && this.ws.readyState === 1 && !this.rfb) this.ws.send(JSON.stringify(obj));
  }

  nachricht(n) {
    // Nach «bereit» spricht noVNC; von Keyway kommen nur noch «ende» (Grund) und «abgemeldet».
    if (this.rfb) {
      if (n.t === 'ende' || n.t === 'fehler') this.endeText = n.text || '';
      else if (n.t === 'abgemeldet') this.abgemeldetGemeldet = true;
      return;
    }
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
      case 'vnc-passwort':
        this.passwortFragen(n).then((a) => this.senden(Object.assign({ t: 'vnc-passwort' }, a || { passwort: '', speichern: false, abbrechen: true })));
        break;
      case 'bereit':
        this.bereit(n);
        break;
      case 'abgemeldet':
        this.abgemeldetGemeldet = true; // der Server schliesst gleich mit 4001
        break;
      case 'fehler':
        this.zustandSetzen('fehler', n.text || t('vnc.fehlgeschlagen'));
        break;
      case 'ende':
        this.zustandSetzen('beendet', n.text || t('vnc.beendet'));
        break;
    }
  }

  // Keyway hat sich am VNC-Server angemeldet (oder reicht durch): ab jetzt spricht noVNC RFB ueber
  // dieselbe WebSocket. noVNC muss sie sofort uebernehmen — die naechsten Binaerframes sind schon
  // unterwegs (deshalb ist noVNC vor dem Aufbau geladen).
  bereit(n) {
    this.rueckfrageZu();
    if (n.titel) this.titel = n.titel;
    if (n.hostId) this.hostId = n.hostId;
    this.weg = n.weg || '';
    this.durchreichen = !!n.durchreichen;
    this.fernName = n.name || '';
    if (n.gespeichert) { App.datenLaden(); toast(t('vnc.gespeichert')); }
    const ws = this.ws;
    let rfb;
    try {
      rfb = new window.KeywayRFB.RFB(this.ziel, ws, { shared: true });
    } catch (e) {
      this.zustandSetzen('fehler', t('vnc.fehlgeschlagen'));
      try { ws.close(); } catch (x) { /* schon zu */ }
      return;
    }
    this.rfb = rfb;
    // Touch: noVNC zeichnet den Zeiger als eigene Leinwand im body. Sie gehoert zu diesem Tab und
    // bleibt versteckt, solange ein anderer Tab vorn ist (vnc.css «vnc-zeiger-aus»).
    this.zeiger = (rfb._cursor && rfb._cursor._canvas) || null;
    this.zeigerSetzen(App.aktiv === this);
    rfb.background = 'var(--term)';
    rfb.focusOnClick = !vncTouch(); // Touch: Tippen aufs Bild schliesst die Tastatur nicht
    rfb.viewOnly = this.einst.nurAnsehen;
    this.ansichtAnwenden();
    this.qualitaetAnwenden();
    rfb.addEventListener('connect', () => { if (rfb === this.rfb) this.verbunden(); });
    rfb.addEventListener('disconnect', (e) => { if (rfb === this.rfb) this.getrennt(!!(e.detail && e.detail.clean)); });
    rfb.addEventListener('credentialsrequired', (e) => { if (rfb === this.rfb) this.geraetAnmeldung(e.detail && e.detail.types); });
    rfb.addEventListener('securityfailure', (e) => { this.sicherheitsFehler = e.detail || {}; });
    rfb.addEventListener('serververification', (e) => { if (rfb === this.rfb) this.serverPruefen(e.detail || {}); });
    rfb.addEventListener('clipboard', (e) => { if (rfb === this.rfb) this.ablageEmpfangen(e.detail && e.detail.text); });
    rfb.addEventListener('desktopname', (e) => {
      this.fernName = (e.detail && e.detail.name) || '';
      if (App.aktiv === this) App.tabsZeichnen();
    });
    rfb.addEventListener('clippingviewport', () => this.leistenZeichnen());
    this.zustandSetzen('verbinde', t('vnc.bildLaedt'));
  }

  // noVNC hat das erste Bild: verbunden.
  verbunden() {
    this.verbundenSeit = Date.now();
    this.zustandSetzen('bereit');
    clearInterval(this.vorschauTimer);
    this.vorschauTimer = setInterval(() => this.vorschauSpeichern(), VNC_VORSCHAU_ALLE);
    clearTimeout(this.ersteVorschau);
    this.ersteVorschau = setTimeout(() => this.vorschauSpeichern(), 4000);
    this.groesseBeobachten();
    if (this.einst.skalierung === 'fern') this.fernPruefen();
    if (App.aktiv === this && !dialogStapel.length) this.fokus();
    // Telefon: einmal zeigen, welche Gesten es gibt
    if (App.istTelefon() && !lokal('vncGestenGezeigt', false)) { lokalSetzen('vncGestenGezeigt', true); toast(t('vnc.gesten')); }
  }

  getrennt(sauber) {
    if (this.geschlossen) return;
    const vorher = this.zustand;
    this.vorschauSpeichern();
    this.rfb = null; // schon getrennt: nicht noch einmal trennen
    this.abbauen();
    if (this.zuCode === VNC_ZU_ABGEMELDET || this.abgemeldetGemeldet) { this.abgemeldet(); return; }
    // Das letzte Bild bleibt abgedunkelt stehen.
    if (this.letztesBild && vorher === 'bereit') {
      this.flaeche.style.backgroundImage = 'url("' + this.letztesBild + '")';
      this.flaeche.classList.add('mit-bild');
    }
    if (vorher !== 'bereit') {
      const f = this.sicherheitsFehler;
      this.zustandSetzen('fehler', f ? (f.reason ? t('vnc.anmeldungFehlerGrund', { grund: f.reason }) : t('vnc.anmeldungFehler')) : this.endeText || t('vnc.fehlgeschlagen'));
      return;
    }
    // Grund von Keyway (z. B. «Der VNC-Server hat die Verbindung beendet»), sonst nach noVNC.
    this.zustandSetzen('beendet', this.vomBenutzer ? t('vnc.beendet') : this.endeText || (sauber ? t('vnc.beendet') : t('vnc.unterbrochen')));
  }

  // Trennen (Knopf, Abbrechen im Aufbau). Der Tab bleibt mit «Neu verbinden» stehen.
  trennen() {
    this.vomBenutzer = true;
    if (this.rfb) {
      this.vorschauSpeichern();
      try { this.rfb.disconnect(); } catch (e) { /* schon zu */ }
      return;
    }
    this.abbauen();
    this.zustandSetzen('beendet', t('vnc.beendet'));
  }

  // Zeitgeber, Beobachter, noVNC und WebSocket abraeumen.
  abbauen() {
    clearInterval(this.vorschauTimer);
    clearTimeout(this.ersteVorschau);
    clearTimeout(this.fernTimer);
    if (this.groesseWache) { this.groesseWache.disconnect(); this.groesseWache = null; }
    const r = this.rfb, ws = this.ws;
    this.rfb = null;
    this.ws = null;
    if (r) { try { r.disconnect(); } catch (e) { /* schon zu */ } } else if (ws) {
      ws.onmessage = null;
      ws.onclose = null;
      try { ws.close(); } catch (e) { /* schon zu */ }
    }
    if (document.activeElement === this.eingabe) this.eingabe.blur();
  }

  // ---------- Zustand und Leisten ----------

  zustandSetzen(z, text) {
    this.zustand = z;
    if (text) this.grund = text;
    if (z === 'fehler' || z === 'beendet') this.rueckfrageZu();
    ersetzen(this.ueber);
    if (z === 'bereit') {
      this.ueber.classList.add('versteckt');
    } else {
      const knoepfe = [];
      if (z === 'beendet' || z === 'fehler') {
        if (this.hostId && !this.istAbgemeldet) knoepfe.push(el('button', { class: 'btn klein primaer', type: 'button', onclick: () => this.verbinden() }, icon('neu', 16), t('vnc.neuVerbinden')));
        knoepfe.push(el('button', { class: 'btn klein', type: 'button', onclick: () => App.tabSchliessen(this) }, t('basis.schliessen')));
      } else if (z === 'verbinde') {
        knoepfe.push(el('button', { class: 'btn klein', type: 'button', onclick: () => this.trennen() }, t('basis.abbrechen')));
      }
      this.ueber.className = 'term-ueber' + (z === 'fehler' ? ' fehler' : '');
      this.ueber.append(
        icon(z === 'fehler' ? 'warnung' : z === 'beendet' ? 'bildschirm' : 'neu', 22),
        el('div', { class: 'term-ueber-text' },
          el('span', null, text || this.grund),
          knoepfe.length ? el('div', { class: 'reihe' }, knoepfe) : null));
    }
    this.leistenZeichnen();
    if (App.tabsZeichnen) App.tabsZeichnen();
  }

  // Knopf der Desktop-Leiste: Symbol und Text (der Text faellt bei wenig Breite weg, vnc.css).
  // mousedown ohne Fokuswechsel: Die Tastatur bleibt beim Bildschirm.
  knopf(name, text, aktion, extra) {
    const x = extra || {};
    return el('button', Object.assign({ class: 'vnc-k', type: 'button', title: x.titel || text, 'aria-label': text,
      onmousedown: (e) => e.preventDefault(), onclick: (e) => aktion(e.currentTarget) }, x.attrs),
    icon(name, 18), el('span', { class: 'l' }, x.kurz || text), x.punkt ? el('span', { class: 'vnc-punkt', 'aria-hidden': 'true' }) : null);
  }

  // Knopf der Telefon-Leiste: Symbol ueber einem kurzen Text (x.name: ausfuehrlicher Name fuer
  // Bildschirmleser, x.titel: Tooltip). pointerdown ohne Fokuswechsel: Eine offene Bildschirmtastatur
  // bleibt offen.
  knopfT(name, text, aktion, extra) {
    const x = extra || {};
    return el('button', Object.assign({ class: 'vnc-t', type: 'button', title: x.titel || text, 'aria-label': x.name,
      onpointerdown: (e) => e.preventDefault(), onclick: (e) => aktion(e.currentTarget) }, x.attrs),
    icon(name, 20), el('span', { class: 'l' }, text), x.punkt ? el('span', { class: 'vnc-punkt', 'aria-hidden': 'true' }) : null);
  }

  leistenZeichnen() {
    if (!this.leiste) return;
    const bereit = this.zustand === 'bereit';
    const aus = !bereit;
    const nurAnsehen = this.einst.nurAnsehen;
    const s = this.einst.skalierung;
    const gedrueckt = (an) => (an ? 'true' : 'false');
    const vollbild = this.vollbildAn ? this.knopf('vollbildAus', t('vnc.vollbildAus'), () => this.vollbildUmschalten(), { attrs: { 'aria-pressed': 'true' } })
      : this.knopf('vollbild', t('vnc.vollbild'), () => this.vollbildUmschalten(), { attrs: { 'aria-pressed': 'false' } });
    const laeuft = bereit || this.zustand === 'verbinde';
    ersetzen(this.leiste,
      el('div', { class: 'vnc-segment', role: 'group', 'aria-label': t('vnc.ansicht.titel') },
        VNC_ANSICHTEN.map((a) => this.knopf(a.icon, t('vnc.ansicht.' + a.id), () => this.ansichtSetzen(a.id),
          { titel: t('vnc.ansicht.' + a.id + 'Titel'), attrs: { 'aria-pressed': gedrueckt(s === a.id) } }))),
      this.knopf('qualitaet', t('vnc.qualitaet.knopf', { stufe: t('vnc.qualitaet.' + this.einst.qualitaet) }), (k) => this.qualitaetMenue(k),
        { kurz: t('vnc.qualitaet.' + this.einst.qualitaet), attrs: { 'aria-haspopup': 'menu' } }),
      this.knopf('auge', t('vnc.nurAnsehen'), () => this.nurAnsehenSetzen(!nurAnsehen), { titel: t('vnc.nurAnsehenTitel'), attrs: { 'aria-pressed': gedrueckt(nurAnsehen) } }),
      el('span', { class: 'vnc-trenn', 'aria-hidden': 'true' }),
      vncTouch() ? this.knopf('tastatur', t('vnc.tastatur'), () => this.tastaturUmschalten(),
        { titel: t('vnc.tastaturTitel'), attrs: { disabled: aus || nurAnsehen, 'aria-pressed': gedrueckt(document.activeElement === this.eingabe), onmousedown: null, onpointerdown: (e) => e.preventDefault() } }) : null,
      this.knopf('ablage', t('vnc.ablage.knopf'), () => this.ablageDialog(), { punkt: this.ablageNeu && bereit, titel: this.ablageNeu ? t('vnc.ablage.neu') : null, attrs: { disabled: aus } }),
      this.knopf('sondertaste', t('vnc.tasten.knopf'), () => this.tastenDialog(), { attrs: { disabled: aus || nurAnsehen } }),
      el('button', { class: 'vnc-k text', type: 'button', disabled: aus || nurAnsehen, onmousedown: (e) => e.preventDefault(),
        onclick: () => this.tasteSenden(t('vnc.tasten.cad'), (r) => r.sendCtrlAltDel()) }, t('vnc.tasten.cad')),
      el('span', { class: 'platz' }),
      vollbild,
      laeuft ? this.knopf('trennen', t('vnc.trennen'), () => this.trennen())
        : this.knopf('neu', t('vnc.neuVerbinden'), () => this.verbinden(), { attrs: { disabled: !this.hostId || this.istAbgemeldet } }));

    const tastaturOffen = document.activeElement === this.eingabe;
    const ansicht = VNC_ANSICHTEN.find((a) => a.id === s);
    const clipping = !!(this.rfb && this.rfb.clippingViewport);
    ersetzen(this.unten,
      this.knopfT('tastatur', t('vnc.tastatur'), () => this.tastaturUmschalten(), { titel: t('vnc.tastaturTitel'), attrs: { disabled: aus || nurAnsehen, 'aria-pressed': gedrueckt(tastaturOffen) } }),
      this.knopfT('sondertaste', t('vnc.tasten.kurz'), () => this.tastenDialog(), { name: t('vnc.tasten.knopf'), attrs: { disabled: aus || nurAnsehen } }),
      this.knopfT('ablage', t('vnc.ablage.kurz'), () => this.ablageDialog(), { name: t('vnc.ablage.knopf'), punkt: this.ablageNeu && bereit, attrs: { disabled: aus } }),
      s === 'original' && clipping ? this.knopfT('verschieben', t('vnc.verschieben'), () => this.verschiebenSetzen(!this.verschieben),
        { titel: t('vnc.verschiebenTitel'), attrs: { 'aria-pressed': gedrueckt(this.verschieben) } }) : null,
      this.knopfT(ansicht ? ansicht.icon : 'anpassen', t('vnc.ansicht.titel'), (k) => this.ansichtMenue(k), { attrs: { 'aria-haspopup': 'menu' } }),
      this.knopfT('mehr', t('vnc.mehr'), (k) => this.mehrMenue(k), { attrs: { 'aria-haspopup': 'menu' } }));
    this.element.classList.toggle('nur-ansehen', nurAnsehen);
  }

  leisteSetzen(zu) {
    this.leisteZu = zu;
    lokalSetzen('vncLeisteZu', zu);
    this.element.classList.toggle('leiste-zu', zu);
  }

  // Wahl je Host merken (Ansicht, Qualitaet, Nur ansehen).
  einstMerken() { lokalSetzen('vncEinst.' + this.hostId, this.einst); }

  // ---------- Ansicht, Qualitaet, Nur ansehen ----------

  ansichtSetzen(id) {
    this.einst.skalierung = id;
    this.einstMerken();
    this.fernGeht = true;
    this.ansichtAnwenden();
    this.leistenZeichnen();
    if (id === 'fern' && this.zustand === 'bereit') this.fernPruefen();
  }

  // Anpassen: noVNC skaliert auf die Flaeche. Originalgroesse: 1:1 — am Desktop mit Rollbalken, am
  // Telefon mit verschiebbarem Ausschnitt. Fernaufloesung: das Geraet aendert seine Aufloesung
  // (nur, solange der Tab sichtbar ist — sonst meldete noVNC die Groesse 0).
  ansichtAnwenden() {
    const r = this.rfb;
    this.element.dataset.ansicht = this.einst.skalierung;
    if (!r) return;
    const s = this.einst.skalierung === 'fern' && !this.fernGeht ? 'anpassen' : this.einst.skalierung;
    const telefon = App.istTelefon();
    r.scaleViewport = s === 'anpassen';
    r.clipViewport = s === 'original' && telefon;
    r.dragViewport = s === 'original' && telefon && this.verschieben;
    r.resizeSession = s === 'fern' && this.istSichtbar();
  }

  istSichtbar() { return this.element.isConnected && this.element.getClientRects().length > 0; }

  zeigerSetzen(an) { if (this.zeiger && this.zeiger.classList) this.zeiger.classList.toggle('vnc-zeiger-aus', !an); }

  // Aufruf von App.aktivieren: vor dem Ausblenden false, nach dem Einblenden true. Wer den Tab
  // verlaesst (Palette, anderer Host), verlaesst auch das Vollbild.
  sichtbar(an) {
    if (!an && this.vollbildAn) this.vollbildUmschalten();
    this.zeigerSetzen(an);
    if (!this.rfb || this.einst.skalierung !== 'fern' || !this.fernGeht) return;
    this.rfb.resizeSession = an && this.istSichtbar();
  }

  // Kann das Geraet seine Aufloesung aendern? noVNC weiss es nach dem ersten Bild (ExtendedDesktopSize).
  fernPruefen() {
    clearTimeout(this.fernTimer);
    this.fernTimer = setTimeout(() => {
      const r = this.rfb;
      if (!r || this.einst.skalierung !== 'fern' || r._supportsSetDesktopSize !== false) return;
      this.fernGeht = false;
      this.ansichtAnwenden();
      toast(t('vnc.ansicht.fernNicht'));
    }, 1500);
  }

  verschiebenSetzen(an) {
    this.verschieben = an;
    this.ansichtAnwenden();
    this.leistenZeichnen();
  }

  qualitaetAnwenden() {
    const r = this.rfb;
    if (!r) return;
    const stufe = this.einst.qualitaet === 'auto' ? vncAutoStufe() : this.einst.qualitaet;
    const [q, k] = VNC_QUALITAET[stufe] || VNC_QUALITAET.hoch;
    r.qualityLevel = q;
    r.compressionLevel = k;
  }

  qualitaetSetzen(stufe) {
    this.einst.qualitaet = stufe;
    this.einstMerken();
    this.qualitaetAnwenden();
    this.leistenZeichnen();
  }

  qualitaetMenue(anker) {
    menue(anker, VNC_STUFEN.map((s) => ({ text: t('vnc.qualitaet.' + s), aktiv: this.einst.qualitaet === s, aktion: () => { this.qualitaetSetzen(s); this.fokus(); } })));
  }

  ansichtMenue(anker) {
    menue(anker, VNC_ANSICHTEN.map((a) => ({ text: el('span', { class: 'menue-text' }, el('span', null, t('vnc.ansicht.' + a.id)), el('small', null, t('vnc.ansicht.' + a.id + 'Titel'))),
      icon: a.icon, aktiv: this.einst.skalierung === a.id, aktion: () => this.ansichtSetzen(a.id) })));
  }

  // Telefon: Qualitaet, Nur ansehen, Vollbild, Werkzeuge ausblenden, Trennen.
  mehrMenue(anker) {
    const laeuft = this.zustand === 'bereit' || this.zustand === 'verbinde';
    menue(anker, [
      { text: t('vnc.qualitaet.knopf', { stufe: t('vnc.qualitaet.' + this.einst.qualitaet) }), icon: 'qualitaet', aktion: () => setTimeout(() => this.qualitaetMenue(anker), 0) },
      { text: t('vnc.nurAnsehen'), icon: 'auge', aktiv: this.einst.nurAnsehen, aktion: () => this.nurAnsehenSetzen(!this.einst.nurAnsehen) },
      { text: this.vollbildAn ? t('vnc.vollbildAus') : t('vnc.vollbild'), icon: this.vollbildAn ? 'vollbildAus' : 'vollbild', aktion: () => this.vollbildUmschalten() },
      { text: t('vnc.leiste.zu'), icon: 'chevronRunter', aktion: () => this.leisteSetzen(true) },
      laeuft ? { text: t('vnc.trennen'), icon: 'trennen', gefahr: true, aktion: () => this.trennen() }
        : (this.hostId && !this.istAbgemeldet ? { text: t('vnc.neuVerbinden'), icon: 'neu', aktion: () => this.verbinden() } : null),
    ]);
  }

  nurAnsehenSetzen(an) {
    this.einst.nurAnsehen = an;
    this.einstMerken();
    if (this.rfb) this.rfb.viewOnly = an;
    if (an && document.activeElement === this.eingabe) this.eingabe.blur();
    this.leistenZeichnen();
    if (App.aktiv === this) App.tabsZeichnen();
  }

  // ---------- Vollbild ----------
  // Vollbild betrifft die ganze Seite (Dialoge bleiben sichtbar); der Tab legt sich per CSS darueber
  // (Vollfenster). Ohne Fullscreen-API (iPhone) bleibt es beim Vollfenster.

  vollbildUmschalten() {
    if (this.vollbildAn) {
      this.vollfensterSetzen(false);
      const fe = document.fullscreenElement || document.webkitFullscreenElement;
      if (fe) {
        try {
          const p = (document.exitFullscreen || document.webkitExitFullscreen).call(document);
          if (p && p.catch) p.catch(() => { /* schon beendet */ });
        } catch (e) { /* schon beendet */ }
      }
      return;
    }
    this.vollfensterSetzen(true);
    const w = document.documentElement;
    const anfrage = w.requestFullscreen || w.webkitRequestFullscreen;
    if (!anfrage) return;
    try {
      const p = anfrage.call(w, { navigationUI: 'hide' });
      if (p && p.then) p.then(() => this.tastenSperren(true)).catch(() => { /* nicht erlaubt: Vollfenster bleibt */ });
    } catch (e) { /* Vollfenster */ }
  }

  vollfensterSetzen(an) {
    this.vollbildAn = an;
    this.element.classList.toggle('vollfenster', an);
    document.documentElement.classList.toggle('vnc-vollbild', an);
    if (!an) this.tastenSperren(false);
    this.leistenZeichnen();
    if (App.aktiv === this) setTimeout(() => this.fokus(), 50);
  }

  // Im echten Vollbild gehen auch Esc und Systemtasten an das Geraet (Chrome: Esc lange druecken beendet).
  tastenSperren(an) {
    const k = navigator.keyboard;
    if (!k || !k.lock) return;
    try {
      if (an) { const p = k.lock(); if (p && p.catch) p.catch(() => { /* nicht erlaubt */ }); } else k.unlock();
    } catch (e) { /* ohne Sperre */ }
  }

  vollbildGeaendert() {
    const fe = document.fullscreenElement || document.webkitFullscreenElement;
    if (fe) { if (this.vollbildAn) this.nativVollbild = true; return; }
    // Vollbild vom Browser beendet (Esc): Vollfenster auch
    if (this.nativVollbild && this.vollbildAn) this.vollfensterSetzen(false);
    this.nativVollbild = false;
  }

  // ---------- Tasten ----------

  // Sondertaste senden (Dialog, Leiste) mit kurzer Bestaetigung.
  tasteSenden(name, f) {
    const r = this.rfb;
    if (!r || this.zustand !== 'bereit') return;
    if (this.einst.nurAnsehen) { toast(t('vnc.tasten.nurAnsehen')); return; }
    f(r, window.KeywayRFB.KeyTable);
    toast(t('vnc.tasten.gesendet', { taste: name }));
    this.fokus();
  }

  tastenDialog() {
    if (!this.rfb) return;
    if (this.einst.nurAnsehen) { toast(t('vnc.tasten.nurAnsehen')); return; }
    const k = (name, f, klasse) => el('button', { class: 'btn' + (klasse ? ' ' + klasse : ''), type: 'button', onclick: () => { d.zu(); this.tasteSenden(name, f); } }, name);
    const haupt = [
      k(t('vnc.tasten.cad'), (r) => r.sendCtrlAltDel(), 'primaer'),
      k(t('vnc.tasten.win'), (r, K) => r.sendKey(K.XK_Super_L, 'MetaLeft')),
      k(t('vnc.tasten.altTab'), (r, K) => { r.sendKey(K.XK_Alt_L, 'AltLeft', true); r.sendKey(K.XK_Tab, 'Tab'); r.sendKey(K.XK_Alt_L, 'AltLeft', false); }),
      k(t('vnc.tasten.esc'), (r, K) => r.sendKey(K.XK_Escape, 'Escape')),
      k(t('vnc.tasten.tab'), (r, K) => r.sendKey(K.XK_Tab, 'Tab')),
      k(t('vnc.tasten.druck'), (r, K) => r.sendKey(K.XK_Print, 'PrintScreen')),
    ];
    const funktion = [];
    for (let i = 1; i <= 12; i++) funktion.push(k('F' + i, (r, K) => r.sendKey(K['XK_F' + i], 'F' + i)));
    const d = dialog({
      titel: t('vnc.tasten.knopf'),
      inhalt: el('div', { class: 'vnc-tasten' },
        el('div', { class: 'vnc-tasten-raster' }, haupt),
        el('div', { class: 'feld' }, el('span', { class: 'label' }, t('vnc.tasten.funktion')),
          el('div', { class: 'vnc-tasten-raster f' }, funktion))),
    });
  }

  // Telefon: Bildschirmtastatur ueber das unsichtbare Eingabefeld (fokussieren oeffnet sie).
  tastaturUmschalten() {
    if (document.activeElement === this.eingabe) { this.eingabe.blur(); return; }
    if (!this.rfb || this.einst.nurAnsehen) return;
    this.eingabeLeeren();
    this.eingabe.focus({ preventScroll: true });
  }

  eingabeLeeren() {
    this.eingabe.value = '_'.repeat(VNC_EINGABE_LAENGE);
    this.eingabeAlt = this.eingabe.value;
    try { this.eingabe.setSelectionRange(VNC_EINGABE_LAENGE, VNC_EINGABE_LAENGE); } catch (e) { /* egal */ }
  }

  // Getippter Text: Unterschied zum vorherigen Inhalt als Tasten senden (Loeschen = Rueckschritt,
  // Autokorrektur ersetzt Zeichen).
  eingabeText() {
    const f = this.eingabe, r = this.rfb;
    if (!r || this.zustand !== 'bereit' || this.einst.nurAnsehen) { this.eingabeLeeren(); return; }
    const K = window.KeywayRFB;
    const neu = f.value, alt = this.eingabeAlt || '';
    let rein = neu.length - alt.length;
    let zurueck = rein < 0 ? -rein : 0;
    for (let i = 0; i < Math.min(alt.length, neu.length); i++) {
      if (neu[i] !== alt[i]) { rein = neu.length - i; zurueck = alt.length - i; break; }
    }
    for (let i = 0; i < zurueck; i++) r.sendKey(K.KeyTable.XK_BackSpace, 'Backspace');
    if (rein > 0) for (const z of neu.slice(neu.length - rein)) this.zeichenSenden(z);
    if (neu.length > 2 * VNC_EINGABE_LAENGE || neu.length < 1) this.eingabeLeeren();
    else this.eingabeAlt = neu;
  }

  zeichenSenden(z) {
    const r = this.rfb, K = window.KeywayRFB;
    if (z === '\n' || z === '\r') r.sendKey(K.KeyTable.XK_Return, 'Enter');
    else if (z === '\t') r.sendKey(K.KeyTable.XK_Tab, 'Tab');
    else r.sendKey(K.keysyms.lookup(z.codePointAt(0)), null);
  }

  // Tasten ohne Text (Enter, Pfeile, Esc …) und Strg/Alt-Kombinationen einer Hardware-Tastatur.
  eingabeTaste(e) {
    const r = this.rfb;
    if (!r || this.zustand !== 'bereit' || this.einst.nurAnsehen) return;
    if (e.isComposing || e.keyCode === 229 || e.key === 'Unidentified') return; // Text kommt ueber «input»
    const K = window.KeywayRFB.KeyTable;
    const sonder = {
      Enter: [K.XK_Return, 'Enter'], Backspace: [K.XK_BackSpace, 'Backspace'], Tab: [K.XK_Tab, 'Tab'], Escape: [K.XK_Escape, 'Escape'],
      Delete: [K.XK_Delete, 'Delete'], ArrowLeft: [K.XK_Left, 'ArrowLeft'], ArrowRight: [K.XK_Right, 'ArrowRight'], ArrowUp: [K.XK_Up, 'ArrowUp'],
      ArrowDown: [K.XK_Down, 'ArrowDown'], Home: [K.XK_Home, 'Home'], End: [K.XK_End, 'End'], PageUp: [K.XK_Page_Up, 'PageUp'], PageDown: [K.XK_Page_Down, 'PageDown'],
    };
    const s = sonder[e.key] || (/^F([1-9]|1[0-2])$/.test(e.key) ? [K['XK_' + e.key], e.key] : null);
    if (s) { e.preventDefault(); r.sendKey(s[0], s[1]); return; }
    if ((e.ctrlKey || e.altKey || e.metaKey) && e.key.length === 1) {
      e.preventDefault();
      const mods = [e.ctrlKey && [K.XK_Control_L, 'ControlLeft'], e.altKey && [K.XK_Alt_L, 'AltLeft'], e.metaKey && [K.XK_Super_L, 'MetaLeft']].filter(Boolean);
      for (const m of mods) r.sendKey(m[0], m[1], true);
      r.sendKey(window.KeywayRFB.keysyms.lookup(e.key.codePointAt(0)), null);
      for (const m of mods.reverse()) r.sendKey(m[0], m[1], false);
    }
  }

  // Tastatur an den Bildschirm (Desktop). Am Telefon nicht: das oeffnete die Bildschirmtastatur nicht
  // und naehme einem Eingabefeld den Fokus.
  fokus() {
    if (this.rfb && this.zustand === 'bereit' && !App.istTelefon()) {
      try { this.rfb.focus({ preventScroll: true }); } catch (e) { /* ohne Fokus */ }
    }
  }

  // ---------- Zwischenablage ----------

  ablageEmpfangen(text) {
    this.ablageText = String(text || '');
    if (this.ablageOffen) { this.ablageOffen.zeichnen(); return; }
    this.ablageNeu = !!this.ablageText;
    this.leistenZeichnen();
  }

  ablageDialog() {
    if (!this.rfb) return;
    this.ablageNeu = false;
    this.leistenZeichnen();
    const feld = el('textarea', { id: 'vnc-ablage-text', rows: '4', spellcheck: 'false', autocapitalize: 'off' });
    feld.value = this.ablageEntwurf;
    const empfangen = el('div', { class: 'vnc-empfangen' });
    const zeichnen = () => {
      if (!this.ablageText) { ersetzen(empfangen, el('p', { class: 'vnc-leise' }, t('vnc.ablage.leer'))); return; }
      const text = el('textarea', { class: 'vnc-empfangen-text', rows: '4', readonly: true, spellcheck: 'false', 'aria-label': t('vnc.ablage.empfangen') });
      text.value = this.ablageText;
      ersetzen(empfangen, text,
        el('div', null, el('button', { class: 'btn klein', type: 'button', onclick: () => kopieren(this.ablageText) }, icon('kopieren', 16), t('vnc.ablage.kopieren'))));
    };
    zeichnen();
    const holen = navigator.clipboard && navigator.clipboard.readText ? el('button', { class: 'btn klein', type: 'button', onclick: async () => {
      try { feld.value = await navigator.clipboard.readText(); feld.focus(); } catch (e) { toast(t('vnc.ablage.lokalFehler'), 'fehler'); }
    } }, icon('einfuegen', 16), t('vnc.ablage.lokal')) : null;
    const senden = () => {
      const text = feld.value;
      if (!text || !this.rfb) return;
      this.rfb.clipboardPasteFrom(text);
      this.ablageEntwurf = text;
      toast(t('vnc.ablage.gesendet'));
      d.zu();
    };
    const d = dialog({
      titel: t('vnc.ablage.knopf'),
      inhalt: el('div', { class: 'vnc-ablage' },
        el('div', { class: 'feld' },
          el('label', { for: 'vnc-ablage-text' }, t('vnc.ablage.feld')), feld,
          el('div', { class: 'hinweis' }, t('vnc.ablage.sendenText'))),
        holen ? el('div', null, holen) : null,
        el('div', { class: 'feld' }, el('span', { class: 'label' }, t('vnc.ablage.empfangen')), empfangen)),
      knoepfe: [
        el('button', { class: 'btn', type: 'button', onclick: () => d.zu() }, t('basis.schliessen')),
        el('button', { class: 'btn primaer', type: 'button', onclick: senden }, icon('pfeilRechts'), t('vnc.ablage.senden')),
      ],
      beimSchliessen: () => {
        this.ablageOffen = null;
        if (feld.value !== this.ablageEntwurf) this.ablageEntwurf = feld.value;
        this.fokus();
      },
    });
    this.ablageOffen = { zeichnen };
  }

  // ---------- Rueckfragen ----------

  rueckfrageZu() {
    const r = this.rueckfrage;
    this.rueckfrage = null;
    if (r) r.zu();
  }

  // VNC-Passwort (Keyway meldet sich an). Antwort: { passwort, speichern } oder null (abgebrochen).
  passwortFragen(n) {
    return new Promise((resolve) => {
      let wert = null;
      const host = App.daten.hosts.find((h) => h.id === this.hostId);
      const p = el('input', { id: 'vnc-pw', type: 'password', autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false' });
      const lang = el('div', { class: 'hinweis versteckt' }, t('vnc.pw.lang'));
      p.addEventListener('input', () => lang.classList.toggle('versteckt', p.value.length <= 8));
      // War schon eines gespeichert (und ist falsch), ersetzt das neue es gleich. Nach einem falschen
      // Versuch bleibt die eigene Wahl stehen.
      const vorher = typeof this.pwSpeichern === 'boolean' ? this.pwSpeichern : !!(host && host.hatVncPasswort);
      const merken = el('input', { id: 'vnc-pw-speichern', type: 'checkbox', checked: vorher });
      const los = () => { wert = { passwort: p.value, speichern: merken.checked }; this.pwSpeichern = merken.checked; d.zu(); };
      const d = dialog({
        titel: t('vnc.pw.titel', { ziel: n.ziel || this.titel }),
        inhalt: el('form', { class: 'vnc-form', onsubmit: (e) => { e.preventDefault(); los(); } },
          n.fehler ? el('div', { class: 'meldung fehler', role: 'alert' }, icon('warnung'), el('span', null, n.fehler)) : null,
          el('div', { class: 'feld' }, el('label', { for: 'vnc-pw' }, t('vnc.pw.feld')), p, lang),
          el('label', { class: 'check', for: 'vnc-pw-speichern' }, merken,
            el('span', null, t('vnc.pw.speichern'), el('br'), el('span', { class: 'vnc-leise' }, t('app.login.speichernHinweis')))),
          el('button', { type: 'submit', class: 'versteckt', tabindex: '-1' })),
        knoepfe: [
          el('button', { class: 'btn', type: 'button', onclick: () => d.zu() }, t('basis.abbrechen')),
          el('button', { class: 'btn primaer', type: 'button', onclick: los }, t('vnc.pw.verbinden')),
        ],
        beimSchliessen: () => { if (this.rueckfrage === d) this.rueckfrage = null; resolve(wert); },
      });
      this.rueckfrage = d;
      setTimeout(() => p.focus(), 40);
    });
  }

  // Durchreich-Modus: noVNC meldet sich selbst an (Verfahren, die Keyway nicht spricht). Was hier
  // eingegeben wird, bleibt im Browser und wird nicht gespeichert.
  geraetAnmeldung(typen) {
    if (!this.rfb) return;
    const felder = {};
    const liste = typen && typen.length ? typen : ['password'];
    const zeilen = liste.map((typ) => {
      const id = 'vnc-g-' + typ;
      const inp = el('input', { id, type: typ === 'password' ? 'password' : 'text', autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false' });
      felder[typ] = inp;
      const name = typ === 'username' ? 'vnc.durch.benutzer' : typ === 'target' ? 'vnc.durch.ziel' : 'vnc.durch.passwort';
      return el('div', { class: 'feld' }, el('label', { for: id }, t(name)), inp);
    });
    let wert = null;
    const los = () => { wert = {}; for (const [k, f] of Object.entries(felder)) wert[k] = f.value; d.zu(); };
    const d = dialog({
      titel: t('vnc.durch.titel'),
      inhalt: el('form', { class: 'vnc-form', onsubmit: (e) => { e.preventDefault(); los(); } },
        el('div', { class: 'meldung warn' }, icon('warnung'), el('span', null, t('vnc.durch.hinweis'))),
        zeilen,
        el('button', { type: 'submit', class: 'versteckt', tabindex: '-1' })),
      knoepfe: [
        el('button', { class: 'btn', type: 'button', onclick: () => d.zu() }, t('basis.abbrechen')),
        el('button', { class: 'btn primaer', type: 'button', onclick: los }, t('vnc.pw.verbinden')),
      ],
      beimSchliessen: () => {
        if (this.rueckfrage === d) this.rueckfrage = null;
        if (!this.rfb) return;
        if (wert) this.rfb.sendCredentials(wert); else this.trennen();
      },
    });
    this.rueckfrage = d;
    setTimeout(() => { const f = felder.username || felder.password || Object.values(felder)[0]; if (f) f.focus(); }, 40);
  }

  // Durchreich-Modus mit Schluessel des Geraets (RealVNC RA2): Fingerabdruck bestaetigen.
  async serverPruefen(detail) {
    const schluessel = detail.publickey || new Uint8Array(0);
    let fp = '';
    try {
      const h = new Uint8Array(await crypto.subtle.digest('SHA-256', schluessel));
      fp = 'SHA256:' + btoa(String.fromCharCode(...h)).replace(/=+$/, '');
    } catch (e) {
      fp = Array.from(schluessel.slice(0, 32), (b) => b.toString(16).padStart(2, '0')).join(':');
    }
    if (!this.rfb) return;
    let ok = false;
    const d = dialog({
      titel: t('vnc.pruefen.titel'),
      inhalt: el('div', { class: 'vnc-form' }, el('p', { class: 'frage-text' }, t('vnc.pruefen.text')), el('div', { class: 'fp' }, fp)),
      knoepfe: [
        el('button', { class: 'btn', type: 'button', onclick: () => d.zu() }, t('basis.abbrechen')),
        el('button', { class: 'btn primaer', type: 'button', onclick: () => { ok = true; d.zu(); } }, t('vnc.pruefen.vertrauen')),
      ],
      beimSchliessen: () => {
        if (this.rueckfrage === d) this.rueckfrage = null;
        if (!this.rfb) return;
        if (ok) this.rfb.approveServer(); else this.trennen();
      },
    });
    this.rueckfrage = d;
  }

  // ---------- Vorschaubild, Groesse, Statusleiste ----------

  // Verkleinertes JPEG des aktuellen Bilds: als Vorschau gespeichert (falls eingeschaltet) und als
  // letztes Bild nach dem Trennen gezeigt. Laeuft auch bei «Nur ansehen».
  vorschauSpeichern() {
    const r = this.rfb;
    if (!r || !this.verbundenSeit) return;
    let bild;
    try { bild = r.getImageData(); } catch (e) { return; }
    if (!bild || !bild.width || !bild.height) return;
    const b = Math.min(VNC_VORSCHAU_BREITE, bild.width);
    const h = Math.max(1, Math.round(bild.height * b / bild.width));
    const gross = el('canvas', { width: String(bild.width), height: String(bild.height) });
    const klein = el('canvas', { width: String(b), height: String(h) });
    let url = '';
    try {
      gross.getContext('2d').putImageData(bild, 0, 0);
      const ctx = klein.getContext('2d');
      ctx.imageSmoothingEnabled = true;
      ctx.imageSmoothingQuality = 'high';
      ctx.drawImage(gross, 0, 0, b, h);
      for (const q of [0.7, 0.55, 0.4, 0.25]) {
        url = klein.toDataURL('image/jpeg', q);
        if (url.length <= VNC_VORSCHAU_MAX) break;
      }
    } catch (e) { return; }
    if (!url.startsWith('data:image/jpeg') || url.length > VNC_VORSCHAU_MAX) return;
    this.letztesBild = url;
    if (this.hostId && App.vorschau && App.vorschau.speichern(this.hostId, url)) vncKartenNachfuehren(this.hostId);
  }

  // Aufloesung des Geraets fuer die Statusleiste: Die Zeichenflaeche von noVNC hat sie (ausser beim
  // verschiebbaren Ausschnitt am Telefon, dort gibt es keine Statusleiste).
  groesseBeobachten() {
    if (this.groesseWache) this.groesseWache.disconnect();
    const c = this.ziel.querySelector('canvas');
    if (!c || !window.MutationObserver) return;
    this.groesseWache = new MutationObserver(() => this.anpassen());
    this.groesseWache.observe(c, { attributes: true, attributeFilter: ['width', 'height'] });
    this.anpassen();
  }

  groesseText() {
    const c = this.zustand === 'bereit' ? this.ziel.querySelector('canvas') : null;
    return c && c.width ? c.width + ' × ' + c.height : '';
  }

  // Teile der Statusleiste (Desktop) nach dem Zustand: Adresse mit VNC-Port, Weg, Name des Bildschirms.
  statusTeile(h) {
    const weg = this.weg || (h && h.vncWeg) || 'ssh';
    return [
      el('span', { class: 'vnc-weg' }, icon(weg === 'direkt' ? 'bildschirm' : 'schloss', 13), t(weg === 'direkt' ? 'vnc.weg.direkt' : 'vnc.weg.ssh')),
      h ? el('span', { class: 'kuerzbar', title: h.adresse + ':' + (h.vncPort || 5900) }, h.adresse + ':' + (h.vncPort || 5900)) : null,
      // Name, den das Geraet seinem Bildschirm gibt: Inhalt, nicht uebersetzt
      this.fernName ? el('span', { class: 'kuerzbar vnc-fern-name', title: this.fernName, translate: 'no' }, this.fernName) : null,
      this.einst.nurAnsehen ? el('span', { class: 'vnc-weg warn' }, icon('auge', 13), t('vnc.nurAnsehen')) : null,
    ];
  }

  // Wie TerminalTab.anpassen: Aufruf bei Groessenwechseln. noVNC passt sich selbst an (ResizeObserver);
  // hier nur die Anzeige und der Wechsel zwischen Telefon- und Desktop-Ansicht.
  anpassen() {
    if (App.aktiv === this && App.groesseAnzeige) App.groesseAnzeige.textContent = this.groesseText();
    const telefon = App.istTelefon();
    if (telefon !== this.warTelefon) {
      this.warTelefon = telefon;
      if (this.rfb) { this.rfb.focusOnClick = !vncTouch(); this.ansichtAnwenden(); }
      this.leistenZeichnen();
    }
  }

  aktuelleZeile() { return ''; }
  farbenNeu() { /* noVNC zeichnet ohne Akzentfarbe */ }

  schliessen() {
    if (this.zustand === 'bereit') this.vorschauSpeichern();
    this.geschlossen = true;
    this.zustand = 'beendet';
    this.rueckfrageZu();
    if (this.vollbildAn) this.vollbildUmschalten();
    document.removeEventListener('fullscreenchange', this.vollbildWechsel);
    document.removeEventListener('webkitfullscreenchange', this.vollbildWechsel);
    this.abbauen();
    this.element.remove();
  }
}
App.VncTab = VncTab;
