'use strict';
// Bildschirm-Tab (VNC) mit noVNC. Keyway meldet sich selbst am VNC-Server an — ueber SSH getunnelt
// oder direkt (/api/vnc, Rueckfragen als Text-JSON) — und reicht danach das RFB-Protokoll 1:1 an
// noVNC durch. Die Klasse hat dieselbe Form wie App.TerminalTab (element, titel, hostId, zustand,
// anpassen, schliessen, aktuelleZeile), dazu art = 'vnc': Tabs, Tab-Wahl am Telefon, Schliessen,
// «Neu verbinden» und Statusleiste behandeln beide gleich. Eine VNC-Verbindung endet mit dem Tab:
// Nach dem Neuladen gibt es kein Wiederanhaengen.
// Dazu Vorschaubilder (nur in diesem Browser, localStorage) fuer Startseite, Host-Liste und Palette.
//
// Ansicht: noVNC skaliert immer selbst (scaleViewport) auf seine Zielflaeche, die «Buehne». Bei
// «Anpassen» fuellt sie die Bildflaeche; beim lokalen Zoom ist sie genau Breite × Faktor gross und
// liegt in einem verschiebbaren Rahmen (.vnc-ziel). noVNC rechnet Klicks so selbst richtig um
// (Abstand zur Leinwand / eigener Faktor) — keine CSS-Transformation, die das verfaelschte.
// Gesten: noVNC meldet sie als Ereignisse an der Leinwand; dieser Tab hoert sie vorher ab (Capture
// am Rahmen): Spreizen zoomt lokal, Ziehen verschiebt das vergroesserte Bild, alles andere geht an noVNC.

const VNC_VORSCHAU_ALLE = 30000;
const VNC_VORSCHAU_BREITE = 320;
const VNC_VORSCHAU_MAX = 40000; // Zeichen der Data-URL (etwa 30 KB Bild)
const VNC_ZU_ABGEMELDET = 4001; // Close-Code des Servers: Anmeldung abgelaufen (wie /api/ws)
// Echte Eingabe meldet der Tab dem Server als {t:'bedient'} (haelt die Anmeldung), hoechstens so oft.
const VNC_BEDIENT_ALLE = 10000;
// Qualitaet: [JPEG-Qualitaet, Kompression] (je 0–9) fuer noVNC; wirkt bei Tight/JPEG, nicht bei Raw.
const VNC_QUALITAET = { hoch: [8, 2], mittel: [6, 5], niedrig: [3, 8] };
const VNC_STUFEN = ['auto', 'hoch', 'mittel', 'niedrig'];
// Ansichten: Anpassen, lokaler Zoom (Faktor in einst.zoom, 1 = Originalgroesse), Fernaufloesung.
const VNC_ANSICHTEN = [
  { id: 'anpassen', icon: 'anpassen' },
  { id: 'zoom', icon: 'lupePlus' },
  { id: 'fern', icon: 'aufloesung' },
];
const VNC_ZOOM_MENUE = [1, 1.5, 2]; // Stufen im Ansicht-Menue (100, 150, 200 %)
const VNC_ZOOM_SCHRITTE = [0.5, 0.75, 1, 1.5, 2]; // Knoepfe −/+ (Stufen unter «Anpassen» fallen weg)
const VNC_ZOOM_MAX = 2;
// Strg, Alt, Umschalt, Win der Tastenreihe: Keysym-Name (noVNC KeyTable) und Tastencode.
const VNC_MODS = { ctrl: ['XK_Control_L', 'ControlLeft'], alt: ['XK_Alt_L', 'AltLeft'], shift: ['XK_Shift_L', 'ShiftLeft'], meta: ['XK_Super_L', 'MetaLeft'] };
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

// Bildschirme, die ein Neuladen der Seite trennen wuerde (Sprachwechsel fragt vorher nach).
App.offeneBildschirme = () => (App.tabs || []).filter((x) => x.art === 'vnc' && (x.zustand === 'bereit' || x.zustand === 'verbinde'));

// Nach dem Zuruecksetzen der Aufloesung: f aufrufen, sobald das Bild die urspruengliche Groesse
// hat (Antwort des Geraets), spaetestens nach 1 s.
function vncNachZurueck(leinwand, u, f) {
  const bis = Date.now() + 1000;
  const t = setInterval(() => {
    if ((leinwand && u && leinwand.width === u.w && leinwand.height === u.h) || Date.now() > bis) { clearInterval(t); f(); }
  }, 50);
}

// Gueltiger Zoomfaktor (gemerkt je Host), sonst 1.
function vncZoomGueltig(z) { return typeof z === 'number' && z >= 0.05 && z <= 8 ? z : 1; }

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
    this.verschieben = true; // vergroessert: Ziehen mit einem Finger verschiebt das Bild
    this.leisteZu = lokal('vncLeisteZu', false) === true;
    this.zeileOffen = lokal('vncTastenZeile', false) === true; // Tastenreihe (Touch)
    this.mods = { ctrl: false, alt: false, shift: false, meta: false }; // Feststeller der Tastenreihe
    // Je Host gemerkt: Ansicht, Zoom, Qualitaet. «Nur ansehen» gilt nur fuer diesen Tab (nicht gemerkt).
    // Fernaufloesung aendert das Geraet fuer alle Betrachter: am Touch-Geraet nie von selbst.
    const g = lokal('vncEinst.' + this.hostId, {}) || {};
    let skalierung = g.skalierung === 'original' ? 'zoom' : g.skalierung; // 'original' aus frueheren Versionen
    if (!VNC_ANSICHTEN.some((a) => a.id === skalierung) || (skalierung === 'fern' && vncTouch())) skalierung = 'anpassen';
    this.einst = {
      skalierung,
      zoom: g.skalierung === 'original' ? 1 : vncZoomGueltig(g.zoom),
      qualitaet: VNC_STUFEN.includes(g.qualitaet) ? g.qualitaet : 'auto',
    };
    this.nurAnsehen = false;
    // Code ausserhalb (Palette, Befehlshilfe) ruft beim Schliessen App.aktiv.term.focus() auf.
    this.term = { focus: () => this.fokus() };

    this.buehne = el('div', { class: 'vnc-buehne' }); // Ziel von noVNC
    this.ziel = el('div', { class: 'vnc-ziel' }, this.buehne);
    this.ueber = el('div', { class: 'term-ueber versteckt', role: 'status' });
    // «Nur ansehen» sichtbar im Bild; Antippen schaltet es aus.
    this.plakette = el('button', { class: 'vnc-plakette versteckt', type: 'button', title: t('vnc.nurAnsehenAus'), 'aria-label': t('vnc.nurAnsehenAus'),
      onpointerdown: (e) => e.preventDefault(), onclick: () => this.nurAnsehenSetzen(false) },
    icon('auge', 16), el('span', null, t('vnc.nurAnsehen')), icon('x', 14));
    // Telefon, vergroessert: Zoomstufe mit − und +
    const ohneFokus = (e) => e.preventDefault();
    this.pilleKleiner = el('button', { type: 'button', 'aria-label': t('vnc.zoom.kleiner'), title: t('vnc.zoom.kleiner'), onpointerdown: ohneFokus, onclick: () => this.zoomSchritt(-1) }, icon('lupeMinus', 20));
    this.pilleText = el('button', { type: 'button', class: 'p', 'aria-haspopup': 'menu', onpointerdown: ohneFokus, onclick: (e) => this.ansichtMenue(e.currentTarget) });
    this.pilleGroesser = el('button', { type: 'button', 'aria-label': t('vnc.zoom.groesser'), title: t('vnc.zoom.groesser'), onpointerdown: ohneFokus, onclick: () => this.zoomSchritt(1) }, icon('lupePlus', 20));
    this.zoomPille = el('div', { class: 'vnc-zoom-pille versteckt', role: 'group', 'aria-label': t('vnc.zoom.titel') }, this.pilleKleiner, this.pilleText, this.pilleGroesser);
    this.flaeche = el('div', { class: 'vnc-flaeche' }, this.ziel, this.plakette, this.zoomPille, this.ueber);
    this.leiste = el('div', { class: 'vnc-leiste', role: 'toolbar', 'aria-label': t('vnc.leiste.name') });
    this.tastenZeile = this.tastenZeileBauen();
    this.unten = el('div', { class: 'vnc-unten', role: 'toolbar', 'aria-label': t('vnc.leiste.name') });
    this.griff = el('button', { class: 'vnc-griff', type: 'button', 'aria-label': t('vnc.leiste.auf'), title: t('vnc.leiste.auf'),
      onpointerdown: (e) => e.preventDefault(), onclick: () => this.leisteSetzen(false) }, icon('chevronHoch', 20));
    this.eingabe = el('textarea', { class: 'vnc-eingabe', rows: '1', tabindex: '-1', 'aria-label': t('vnc.tastaturFeld'),
      autocomplete: 'off', autocorrect: 'off', autocapitalize: 'off', spellcheck: 'false',
      oninput: () => this.eingabeText(), onkeydown: (e) => this.eingabeTaste(e),
      onfocus: () => this.leistenZeichnen(), onblur: () => this.leistenZeichnen() });
    this.element = el('div', { class: 'vnc-wrap' + (this.leisteZu ? ' leiste-zu' : '') + (this.zeileOffen ? ' zeile-offen' : '') + (vncTouch() ? ' touch' : '') },
      this.leiste, this.flaeche, this.tastenZeile, this.unten, this.griff, this.eingabe);
    // Gesten vor noVNC abhoeren (Capture am Rahmen der Leinwand)
    for (const typ of ['gesturestart', 'gesturemove', 'gestureend']) this.ziel.addEventListener(typ, (e) => this.geste(e), true);
    this.ziel.addEventListener('mousedown', (e) => { if (this.nurAnsehen && e.target && e.target.tagName === 'CANVAS') this.nurAnsehenHinweis(); }, true);
    // Bedienung auf der Bildflaeche (Klick, Tippen, Mausrad) haelt die Anmeldung (siehe bedient)
    for (const typ of ['pointerdown', 'touchstart', 'wheel']) this.ziel.addEventListener(typ, () => this.bedient(), { capture: true, passive: true });
    // Strg+Umschalt+K (Suche) auch, wenn die Leinwand den Fokus hat — vor noVNC, nur diese Kombination
    this.element.addEventListener('keydown', (e) => { this.bedient(); this.kuerzel(e); }, true);
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
    this.ursprung = null; // Aufloesung des Geraets beim Verbinden (Fernaufloesung stellt sie wieder her)
    this.trenntGerade = null;
    this.bedientZeit = 0;
    this.modsZuruecksetzen();
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

  // Echte Eingabe (Tasten, Klick und Tippen aufs Bild, Sondertasten, Einfuegen, Anmeldedialog des
  // Geraets) dem Server melden: Nur so verlaengert sie die Anmeldung beim Durchreichen (der Strom ist
  // dort nicht lesbar) und beim Einfuegen (Zwischenablage zaehlt der Server nie). Auf derselben
  // WebSocket wie noVNC (Text neben dessen Binaerframes), auch nach «bereit»; gedrosselt. Nicht bei
  // «Nur ansehen» und nicht bei automatischen Vorgaengen (Vorschau, Aufloesung zuruecksetzen).
  bedient() {
    const ws = this.ws;
    if (this.nurAnsehen || !this.rfb || !ws || ws.readyState !== 1) return;
    const jetzt = Date.now();
    if (jetzt - (this.bedientZeit || 0) < VNC_BEDIENT_ALLE) return;
    this.bedientZeit = jetzt;
    try { ws.send(JSON.stringify({ t: 'bedient' })); } catch (e) { /* schon zu */ }
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
      rfb = new window.KeywayRFB.RFB(this.buehne, ws, { shared: true });
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
    rfb.viewOnly = this.nurAnsehen;
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
    this.zustandSetzen('verbinde', t('vnc.bildLaedt'));
  }

  // noVNC hat das erste Bild: verbunden.
  verbunden() {
    this.verbundenSeit = Date.now();
    // Aufloesung vor jeder Fernaufloesung. Blieb das Geraet von einer frueheren Sitzung veraendert
    // (Tab ohne Zuruecksetzen geschlossen), gilt die damals gemerkte.
    const c = this.leinwand();
    const offen = this.hostId ? lokal('vncFernOffen.' + this.hostId, null) : null;
    this.ursprung = offen && offen.w > 0 && offen.h > 0 ? { w: offen.w, h: offen.h } : c && c.width ? { w: c.width, h: c.height } : null;
    this.zustandSetzen('bereit');
    clearInterval(this.vorschauTimer);
    this.vorschauTimer = setInterval(() => this.vorschauSpeichern(), VNC_VORSCHAU_ALLE);
    clearTimeout(this.ersteVorschau);
    this.ersteVorschau = setTimeout(() => this.vorschauSpeichern(), 4000);
    this.groesseBeobachten();
    if (this.einst.skalierung === 'fern') this.fernPruefen();
    if (App.aktiv === this && !dialogStapel.length) this.fokus();
    // Telefon: einmal zeigen, welche Gesten es gibt
    if (App.istTelefon() && lokal('vncGestenGezeigt', 0) !== 2) { lokalSetzen('vncGestenGezeigt', 2); toast(t('vnc.gesten')); }
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
      const r = this.rfb;
      if (this.trenntGerade === r) return;
      this.vorschauSpeichern();
      // Fernaufloesung: Das Geraet bekommt seine Aufloesung zurueck. Erst nach seiner Antwort (oder
      // 1 s) trennen — sonst ginge die Nachricht mit dem Schliessen verloren.
      if (this.aufloesungZurueck()) {
        this.trenntGerade = r;
        vncNachZurueck(this.leinwand(), this.ursprung, () => { try { r.disconnect(); } catch (e) { /* schon zu */ } });
        return;
      }
      try { r.disconnect(); } catch (e) { /* schon zu */ }
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

  // Knopf der Desktop-Leiste: Symbol und Text (der Text faellt bei wenig Breite weg, vnc.css; Name
  // und Tooltip bleiben). x.kurz: sichtbarer Kurztext, x.klasse: weitere Klasse.
  // mousedown ohne Fokuswechsel: Die Tastatur bleibt beim Bildschirm.
  knopf(name, text, aktion, extra) {
    const x = extra || {};
    return el('button', Object.assign({ class: 'vnc-k' + (x.klasse ? ' ' + x.klasse : ''), type: 'button', title: x.titel || text, 'aria-label': text,
      onmousedown: (e) => e.preventDefault(), onclick: (e) => aktion(e.currentTarget) }, x.attrs),
    icon(name, 18), el('span', { class: 'l' }, x.kurz || text), x.punkt ? el('span', { class: 'vnc-punkt', 'aria-hidden': 'true' }) : null);
  }

  // Knopf der Telefon-Leiste: Symbol ueber einem kurzen Text (x.name: ausfuehrlicher Name fuer
  // Bildschirmleser, x.titel: Tooltip). pointerdown ohne Fokuswechsel: Eine offene Bildschirmtastatur
  // bleibt offen.
  knopfT(name, text, aktion, extra) {
    const x = extra || {};
    return el('button', Object.assign({ class: 'vnc-t', type: 'button', title: x.titel || x.name || text, 'aria-label': x.name || text,
      onpointerdown: (e) => e.preventDefault(), onclick: (e) => aktion(e.currentTarget) }, x.attrs),
    icon(name, 20), el('span', { class: 'l' }, text), x.punkt ? el('span', { class: 'vnc-punkt', 'aria-hidden': 'true' }) : null);
  }

  // Name einer Qualitaetsstufe: lang (Menue, Tooltip) oder kurz (sichtbar in der Leiste).
  stufeName(kurz) {
    const q = this.einst.qualitaet;
    return kurz && q === 'auto' ? t('vnc.qualitaet.autoKurz') : t('vnc.qualitaet.' + q);
  }

  leistenZeichnen() {
    if (!this.leiste) return;
    const bereit = this.zustand === 'bereit';
    const aus = !bereit;
    const nurAnsehen = this.nurAnsehen;
    const s = this.wirksameAnsicht();
    const gedrueckt = (an) => (an ? 'true' : 'false');
    const vollbild = this.vollbildAn ? this.knopf('vollbildAus', t('vnc.vollbildAus'), () => this.vollbildUmschalten(), { attrs: { 'aria-pressed': 'true' } })
      : this.knopf('vollbild', t('vnc.vollbild'), () => this.vollbildUmschalten(), { attrs: { 'aria-pressed': 'false' } });
    const laeuft = bereit || this.zustand === 'verbinde';
    const touch = vncTouch();
    const prozent = this.prozentText();
    const stufeTitel = t('vnc.zoom.stufeTitel', { prozent });
    const verschiebbar = bereit && this.verschiebbar();
    ersetzen(this.leiste,
      el('div', { class: 'vnc-segment', role: 'group', 'aria-label': t('vnc.ansicht.titel') },
        this.knopf('anpassen', t('vnc.ansicht.anpassen'), () => this.ansichtSetzen('anpassen'), { titel: t('vnc.ansicht.anpassenTitel'), attrs: { 'aria-pressed': gedrueckt(s === 'anpassen') } }),
        this.knopf('lupeMinus', t('vnc.zoom.kleiner'), () => this.zoomSchritt(-1), { klasse: 'symbol', attrs: { disabled: !this.zoomSchrittMoeglich(-1) } }),
        el('button', { class: 'vnc-k prozent', type: 'button', title: stufeTitel, 'aria-label': stufeTitel, 'aria-haspopup': 'menu', 'aria-pressed': gedrueckt(s === 'zoom'),
          disabled: aus, onmousedown: (e) => e.preventDefault(), onclick: (e) => this.ansichtMenue(e.currentTarget) }, el('span', { class: 'p' }, prozent)),
        this.knopf('lupePlus', t('vnc.zoom.groesser'), () => this.zoomSchritt(1), { klasse: 'symbol', attrs: { disabled: !this.zoomSchrittMoeglich(1) } }),
        this.knopf('aufloesung', t('vnc.ansicht.fern'), () => this.ansichtSetzen('fern'), { titel: t('vnc.ansicht.fernTitel'), attrs: { 'aria-pressed': gedrueckt(s === 'fern') } })),
      // Touch (Tablet), vergroessert: Ziehen verschiebt das Bild oder bewegt die Maus
      touch && verschiebbar ? this.knopf('verschieben', t('vnc.verschieben'), () => this.verschiebenSetzen(!this.verschieben), { titel: t('vnc.verschiebenTitel'), attrs: { 'aria-pressed': gedrueckt(this.verschieben) } }) : null,
      this.knopf('qualitaet', t('vnc.qualitaet.knopf', { stufe: this.stufeName(false) }), (k) => this.qualitaetMenue(k),
        { kurz: t('vnc.qualitaet.knopf', { stufe: this.stufeName(true) }), attrs: { 'aria-haspopup': 'menu' } }),
      this.knopf('auge', t('vnc.nurAnsehen'), () => this.nurAnsehenSetzen(!nurAnsehen), { titel: t('vnc.nurAnsehenTitel'), attrs: { 'aria-pressed': gedrueckt(nurAnsehen) } }),
      el('span', { class: 'vnc-trenn', 'aria-hidden': 'true' }),
      touch ? this.knopf('tastatur', t('vnc.tastatur'), () => this.tastaturUmschalten(),
        { titel: t('vnc.tastaturTitel'), attrs: { disabled: aus || nurAnsehen, 'aria-pressed': gedrueckt(document.activeElement === this.eingabe), onmousedown: null, onpointerdown: (e) => e.preventDefault() } }) : null,
      this.knopf('ablage', t('vnc.ablage.knopf'), () => this.ablageDialog(), { punkt: this.ablageNeu && bereit, titel: this.ablageNeu ? t('vnc.ablage.neu') : null, attrs: { disabled: aus } }),
      // Touch: Sondertasten als Tastenreihe unter dem Bild, sonst als Dialog
      touch ? this.knopf('sondertaste', t('vnc.tasten.knopf'), () => this.zeileUmschalten(), { titel: t('vnc.tasten.zeileKnopf'), attrs: { disabled: aus, 'aria-pressed': gedrueckt(this.zeileOffen) } })
        : this.knopf('sondertaste', t('vnc.tasten.knopf'), () => this.tastenDialog(), { attrs: { disabled: aus || nurAnsehen, 'aria-haspopup': 'dialog' } }),
      el('button', { class: 'vnc-k text', type: 'button', disabled: aus || nurAnsehen, title: t('vnc.tasten.cadTitel'), 'aria-label': t('vnc.tasten.cadTitel'), onmousedown: (e) => e.preventDefault(),
        onclick: () => this.tasteSenden(t('vnc.tasten.cad'), (r) => r.sendCtrlAltDel()) }, t('vnc.tasten.cad')),
      el('span', { class: 'platz' }),
      vollbild,
      laeuft ? this.knopf('trennen', t('vnc.trennen'), () => this.trennen())
        : this.knopf('neu', t('vnc.neuVerbinden'), () => this.verbinden(), { attrs: { disabled: !this.hostId || this.istAbgemeldet } }));

    const tastaturOffen = document.activeElement === this.eingabe;
    const ansicht = VNC_ANSICHTEN.find((a) => a.id === s);
    ersetzen(this.unten,
      this.knopfT('tastatur', t('vnc.tastatur'), () => this.tastaturUmschalten(), { titel: t('vnc.tastaturTitel'), attrs: { disabled: aus || nurAnsehen, 'aria-pressed': gedrueckt(tastaturOffen) } }),
      this.knopfT('sondertaste', t('vnc.tasten.kurz'), () => this.zeileUmschalten(), { name: t('vnc.tasten.zeileKnopf'), attrs: { disabled: aus, 'aria-pressed': gedrueckt(this.zeileOffen) } }),
      this.knopfT('ablage', t('vnc.ablage.kurz'), () => this.ablageDialog(), { name: t('vnc.ablage.knopf'), punkt: this.ablageNeu && bereit, attrs: { disabled: aus } }),
      verschiebbar ? this.knopfT('verschieben', t('vnc.verschieben'), () => this.verschiebenSetzen(!this.verschieben),
        { titel: t('vnc.verschiebenTitel'), attrs: { 'aria-pressed': gedrueckt(this.verschieben) } }) : null,
      this.knopfT(ansicht ? ansicht.icon : 'anpassen', t('vnc.ansicht.titel'), (k) => this.ansichtMenue(k), { attrs: { 'aria-haspopup': 'menu' } }),
      this.knopfT('mehr', t('vnc.mehr'), (k) => this.mehrMenue(k), { attrs: { 'aria-haspopup': 'menu' } }));
    this.element.classList.toggle('nur-ansehen', nurAnsehen);
    this.plakette.classList.toggle('versteckt', !(nurAnsehen && laeuft));
    // Tastenreihe: gesperrt ohne Verbindung und bei «Nur ansehen»
    for (const b of this.tastenZeile.querySelectorAll('button')) b.disabled = aus || nurAnsehen;
    this.zoomPilleZeichnen();
  }

  leisteSetzen(zu) {
    this.leisteZu = zu;
    lokalSetzen('vncLeisteZu', zu);
    this.element.classList.toggle('leiste-zu', zu);
  }

  // Wahl je Host merken (Ansicht, Zoom, Qualitaet). Fernaufloesung am Touch-Geraet nicht: Sie aenderte
  // beim naechsten Verbinden ungefragt die Aufloesung des Geraets (fuer alle, die es sehen).
  einstMerken() {
    const g = { skalierung: this.einst.skalierung, zoom: this.einst.zoom, qualitaet: this.einst.qualitaet };
    if (g.skalierung === 'fern' && vncTouch()) {
      const alt = lokal('vncEinst.' + this.hostId, {}) || {};
      g.skalierung = VNC_ANSICHTEN.some((a) => a.id === alt.skalierung) && alt.skalierung !== 'fern' ? alt.skalierung : 'anpassen';
    }
    lokalSetzen('vncEinst.' + this.hostId, g);
  }

  // ---------- Ansicht, Zoom, Qualitaet, Nur ansehen ----------

  // Die Ansicht, die gerade gilt: Fernaufloesung faellt auf «Anpassen» zurueck, wenn das Geraet
  // seine Aufloesung nicht aendern kann.
  wirksameAnsicht() {
    const s = this.einst.skalierung;
    return s === 'fern' && !this.fernGeht ? 'anpassen' : s;
  }

  leinwand() { return this.ziel.querySelector('canvas'); }

  // Faktor von «Anpassen»: so gross, dass das ganze Bild in die Flaeche passt (wie noVNC rechnet).
  anpassenFaktor() {
    const c = this.leinwand();
    const f = this.flaeche.getBoundingClientRect();
    if (!c || !c.width || !c.height || !f.width || !f.height) return 1;
    return Math.min(f.width / c.width, f.height / c.height);
  }

  aktuellerZoom() {
    const s = this.wirksameAnsicht();
    return s === 'zoom' ? this.einst.zoom : s === 'anpassen' ? this.anpassenFaktor() : 1;
  }

  prozentText() { return t('vnc.zoom.prozent', { n: Math.round(this.aktuellerZoom() * 100) }); }

  // Groesser als die Flaeche: Ziehen kann das Bild verschieben.
  verschiebbar() {
    const z = this.ziel;
    return this.wirksameAnsicht() === 'zoom' && (z.scrollWidth > z.clientWidth + 1 || z.scrollHeight > z.clientHeight + 1);
  }

  ansichtSetzen(id, zoom) {
    if (id === 'original') { id = 'zoom'; zoom = 1; } // frueherer Name von 100 %
    if (id === 'zoom') { this.zoomSetzen(zoom || this.einst.zoom || 1); this.leistenZeichnen(); return; }
    this.fernVerlassen(id);
    this.einst.skalierung = id;
    this.einstMerken();
    this.fernGeht = true;
    this.ansichtAnwenden();
    this.leistenZeichnen();
    if (id === 'fern' && this.zustand === 'bereit') this.fernPruefen();
  }

  // Lokaler Zoom: z = CSS-Pixel je Bildpunkt des Geraets (1 = Originalgroesse). Der Punkt unter
  // opt.x/opt.y (Bildschirmkoordinaten; sonst die Mitte der Flaeche) bleibt an seiner Stelle.
  // opt.geste: aus dem Spreizen (nicht kleiner als «Anpassen»), opt.merken === false: noch nicht merken.
  zoomSetzen(z, opt) {
    const o = opt || {};
    const fit = this.anpassenFaktor();
    const min = o.geste ? fit : Math.min(fit, VNC_ZOOM_SCHRITTE[0]);
    z = Math.min(Math.max(VNC_ZOOM_MAX, fit), Math.max(min, z));
    const c = this.leinwand();
    let anker = null;
    if (c && c.width && this.rfb) {
      const rc = c.getBoundingClientRect();
      const f = this.flaeche.getBoundingClientRect();
      const cx = typeof o.x === 'number' ? o.x : f.left + f.width / 2;
      const cy = typeof o.y === 'number' ? o.y : f.top + f.height / 2;
      const s = rc.width / c.width || 1;
      anker = { cx, cy, fx: (cx - rc.left) / s, fy: (cy - rc.top) / s };
    }
    this.fernVerlassen('zoom');
    this.einst.skalierung = 'zoom';
    this.einst.zoom = Math.round(z * 1000) / 1000;
    this.ansichtAnwenden();
    if (anker) {
      const rc = c.getBoundingClientRect();
      const s = rc.width / c.width || 1;
      this.ziel.scrollLeft += rc.left + anker.fx * s - anker.cx;
      this.ziel.scrollTop += rc.top + anker.fy * s - anker.cy;
    }
    if (o.merken !== false) this.einstMerken();
    this.zoomPilleZeichnen();
  }

  // Stufen fuer − und +: «Anpassen» und die festen Stufen darueber.
  zoomZiel(richtung) {
    if (this.zustand !== 'bereit' || !this.rfb || this.wirksameAnsicht() === 'fern') return null;
    const fit = this.anpassenFaktor();
    const jetzt = this.aktuellerZoom();
    const liste = [fit].concat(VNC_ZOOM_SCHRITTE.filter((z) => z > fit * 1.02));
    const z = richtung > 0 ? liste.find((x) => x > jetzt * 1.01) : liste.slice().reverse().find((x) => x < jetzt * 0.99);
    return z === undefined ? null : { z, anpassen: z === fit };
  }

  zoomSchrittMoeglich(richtung) { return !!this.zoomZiel(richtung); }

  zoomSchritt(richtung) {
    const ziel = this.zoomZiel(richtung);
    if (!ziel) return;
    if (ziel.anpassen) this.ansichtSetzen('anpassen'); else { this.zoomSetzen(ziel.z); this.leistenZeichnen(); }
  }

  // Telefon: Zoomstufe ueber dem Bild, solange vergroessert ist.
  zoomPilleZeichnen() {
    if (!this.zoomPille) return;
    const an = this.zustand === 'bereit' && this.wirksameAnsicht() === 'zoom';
    this.zoomPille.classList.toggle('versteckt', !an);
    if (!an) return;
    const prozent = this.prozentText();
    this.pilleText.textContent = prozent;
    this.pilleText.setAttribute('aria-label', t('vnc.zoom.stufeTitel', { prozent }));
    this.pilleText.title = t('vnc.zoom.stufeTitel', { prozent });
    this.pilleKleiner.disabled = !this.zoomSchrittMoeglich(-1);
    this.pilleGroesser.disabled = !this.zoomSchrittMoeglich(1);
  }

  // noVNC skaliert immer selbst auf die Buehne: «Anpassen» = Buehne fuellt die Flaeche, Zoom = Buehne
  // genau Bild × Faktor (in einem verschiebbaren Rahmen). Fernaufloesung: Massstab 1, das Geraet
  // aendert seine Aufloesung (nur, solange der Tab sichtbar ist — sonst meldete noVNC die Groesse 0).
  ansichtAnwenden() {
    const r = this.rfb;
    const s = this.wirksameAnsicht();
    this.element.dataset.ansicht = s;
    const c = this.leinwand();
    const b = this.buehne.style;
    if (s === 'zoom' && c && c.width && c.height) {
      b.width = Math.max(1, Math.round(c.width * this.einst.zoom)) + 'px';
      b.height = Math.max(1, Math.round(c.height * this.einst.zoom)) + 'px';
    } else {
      b.width = '';
      b.height = '';
    }
    if (s !== 'zoom') { this.ziel.scrollLeft = 0; this.ziel.scrollTop = 0; }
    if (!r) return;
    r.clipViewport = false;
    r.dragViewport = false;
    r.resizeSession = s === 'fern' && this.istSichtbar();
    r.scaleViewport = s !== 'fern'; // rechnet sofort neu (Groesse der Buehne)
  }

  istSichtbar() { return this.element.isConnected && this.element.getClientRects().length > 0; }

  zeigerSetzen(an) { if (this.zeiger && this.zeiger.classList) this.zeiger.classList.toggle('vnc-zeiger-aus', !an); }

  // Aufruf von App.aktivieren: vor dem Ausblenden false, nach dem Einblenden true. Wer den Tab
  // verlaesst (Palette, anderer Host), verlaesst auch das Vollbild.
  sichtbar(an) {
    if (!an && this.vollbildAn) this.vollbildUmschalten();
    this.zeigerSetzen(an);
    if (!this.rfb || this.wirksameAnsicht() !== 'fern') return;
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
      this.leistenZeichnen();
      toast(t('vnc.ansicht.fernNicht'));
    }, 1500);
  }

  // Wechsel weg von der Fernaufloesung: Das Geraet bekommt seine Aufloesung von vorher zurueck.
  fernVerlassen(neu) {
    if (this.einst.skalierung !== 'fern' || neu === 'fern') return;
    this.aufloesungZurueck();
  }

  // Hat dieser Tab (oder eine fruehere Sitzung, siehe verbunden) die Aufloesung veraendert?
  aufloesungAnders() {
    const c = this.leinwand(), u = this.ursprung;
    return !!(c && c.width && u && (c.width !== u.w || c.height !== u.h));
  }

  // Aufloesung von vorher an das Geraet (SetDesktopSize). noVNC hat dafuer keine oeffentliche
  // Methode; die Nachricht baut es selbst (RFB.messages), Socket und Bildschirm-Kennung sind intern.
  aufloesungZurueck() {
    const r = this.rfb, u = this.ursprung;
    if (!r) return false;
    r.resizeSession = false;
    if (!u || !this.aufloesungAnders() || r._supportsSetDesktopSize !== true) return false;
    try {
      window.KeywayRFB.RFB.messages.setDesktopSize(r._sock, u.w, u.h, r._screenID, r._screenFlags);
      return true;
    } catch (e) { return false; }
  }

  verschiebenSetzen(an) {
    this.verschieben = an;
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

  // Jede Stufe mit Name und kurzer Erklaerung (auch, wenn die Leiste nur Symbole zeigt).
  qualitaetMenue(anker) {
    const auto = t('vnc.qualitaet.' + vncAutoStufe());
    menue(anker, VNC_STUFEN.map((s) => ({
      text: el('span', { class: 'menue-text' }, el('span', null, t('vnc.qualitaet.knopf', { stufe: t('vnc.qualitaet.' + s) })), el('small', null, t('vnc.qualitaet.' + s + 'Titel', { stufe: auto }))),
      icon: 'qualitaet', aktiv: this.einst.qualitaet === s, aktion: () => { this.qualitaetSetzen(s); this.fokus(); } })));
  }

  // Ansicht: Anpassen, Zoomstufen, Fernaufloesung; dazu «Aufloesung zuruecksetzen», wenn das Geraet
  // noch eine veraenderte Aufloesung hat.
  ansichtMenue(anker) {
    const s = this.wirksameAnsicht();
    const eintrag = (text, klein, symbol, aktiv, aktion) => ({ text: el('span', { class: 'menue-text' }, el('span', null, text), el('small', null, klein)), icon: symbol, aktiv, aktion });
    const u = this.ursprung;
    const zurueck = this.rfb && this.zustand === 'bereit' && s !== 'fern' && this.aufloesungAnders() && this.rfb._supportsSetDesktopSize === true;
    menue(anker, [
      eintrag(t('vnc.ansicht.anpassen'), t('vnc.ansicht.anpassenTitel'), 'anpassen', s === 'anpassen', () => this.ansichtSetzen('anpassen')),
      ...VNC_ZOOM_MENUE.map((z) => eintrag(t('vnc.zoom.prozent', { n: Math.round(z * 100) }), t(z === 1 ? 'vnc.ansicht.originalTitel' : 'vnc.zoom.lokalTitel'),
        z === 1 ? 'original' : 'lupePlus', s === 'zoom' && Math.abs(this.einst.zoom - z) < 0.005, () => this.ansichtSetzen('zoom', z))),
      eintrag(t('vnc.ansicht.fern'), t('vnc.ansicht.fernTitel'), 'aufloesung', s === 'fern', () => this.ansichtSetzen('fern')),
      zurueck ? eintrag(t('vnc.ansicht.zurueck'), t('vnc.ansicht.zurueckTitel', { breite: u.w, hoehe: u.h }), 'neu', false, () => {
        if (this.aufloesungZurueck()) toast(t('vnc.ansicht.zurueckGesendet', { breite: u.w, hoehe: u.h }));
      }) : null,
    ]);
  }

  // Telefon: Qualitaet, Nur ansehen, Vollbild, Werkzeuge ausblenden, Trennen.
  mehrMenue(anker) {
    const laeuft = this.zustand === 'bereit' || this.zustand === 'verbinde';
    menue(anker, [
      { text: t('vnc.qualitaet.knopf', { stufe: this.stufeName(false) }), icon: 'qualitaet', aktion: () => setTimeout(() => this.qualitaetMenue(anker), 0) },
      { text: t('vnc.nurAnsehen'), icon: 'auge', aktiv: this.nurAnsehen, aktion: () => this.nurAnsehenSetzen(!this.nurAnsehen) },
      { text: this.vollbildAn ? t('vnc.vollbildAus') : t('vnc.vollbild'), icon: this.vollbildAn ? 'vollbildAus' : 'vollbild', aktion: () => this.vollbildUmschalten() },
      { text: t('vnc.leiste.zu'), icon: 'chevronRunter', aktion: () => this.leisteSetzen(true) },
      laeuft ? { text: t('vnc.trennen'), icon: 'trennen', gefahr: true, aktion: () => this.trennen() }
        : (this.hostId && !this.istAbgemeldet ? { text: t('vnc.neuVerbinden'), icon: 'neu', aktion: () => this.verbinden() } : null),
    ]);
  }

  // «Nur ansehen» gilt fuer diesen Tab (auch nach «Neu verbinden»), nicht fuer das naechste Oeffnen.
  nurAnsehenSetzen(an) {
    this.nurAnsehen = an;
    if (this.rfb) this.rfb.viewOnly = an;
    if (an) this.modsZuruecksetzen();
    if (an && document.activeElement === this.eingabe) this.eingabe.blur();
    this.leistenZeichnen();
    if (App.aktiv === this) App.tabsZeichnen();
  }

  // Tippen oder Klicken bei «Nur ansehen»: kurz sagen, warum nichts passiert (hoechstens alle 3 s).
  nurAnsehenHinweis() {
    const jetzt = Date.now();
    if (jetzt - (this.hinweisZeit || 0) < 3000) return;
    this.hinweisZeit = jetzt;
    toast(t('vnc.nurAnsehenHinweis'));
  }

  // ---------- Gesten (Touch) ----------

  // Gesten von noVNC (an der Leinwand), hier vorher abgehoert. Die Art legt der Anfang fest; Bewegung
  // und Ende derselben Geste folgen ihr.
  geste(e) {
    const d = e.detail || {};
    if (e.type === 'gesturestart') this.gesteArt = this.gesteWaehlen(d.type);
    const art = this.gesteArt;
    if (e.type === 'gestureend') this.gesteArt = null;
    switch (art) {
      case 'zoom': e.stopPropagation(); this.spreizen(e.type, d); break;
      case 'schieben': e.stopPropagation(); this.ziehen(e.type, d); break;
      case 'aus': e.stopPropagation(); break;
      case 'mods':
        // Feststeller der Tastenreihe gelten fuer diesen Klick (Strg+Klick …): vor noVNC druecken,
        // nach seinem Ende loslassen
        if (e.type === 'gesturestart') this.modsDruecken();
        else if (e.type === 'gestureend') queueMicrotask(() => this.modsLoslassen());
        break;
      default: break; // noVNC
    }
  }

  gesteWaehlen(typ) {
    if (typ === 'pinch') {
      // Zwei Finger spreizen: immer lokal, nie als Strg+Mausrad an das Geraet
      if (this.zustand !== 'bereit') return 'aus';
      if (this.wirksameAnsicht() === 'fern') { toast(t('vnc.zoom.fernNicht')); return 'aus'; }
      return 'zoom';
    }
    if (typ === 'drag' && this.verschieben && this.verschiebbar()) return 'schieben';
    if (this.nurAnsehen) { this.nurAnsehenHinweis(); return 'novnc'; }
    return this.modsAktiv() ? 'mods' : 'novnc';
  }

  spreizen(phase, d) {
    const abstand = Math.hypot(d.magnitudeX || 0, d.magnitudeY || 0);
    if (phase === 'gesturestart') {
      this.spreizStart = { abstand: Math.max(abstand, 1), zoom: this.aktuellerZoom(), x: d.clientX, y: d.clientY };
      return;
    }
    const p = this.spreizStart;
    if (!p) return;
    if (phase === 'gesturemove') { this.zoomSetzen(p.zoom * abstand / p.abstand, { geste: true, x: p.x, y: p.y, merken: false }); return; }
    this.spreizStart = null;
    // Kaum groesser als «Anpassen»: dort einrasten
    if (this.einst.skalierung === 'zoom' && this.einst.zoom <= this.anpassenFaktor() * 1.04) this.ansichtSetzen('anpassen');
    else { this.einstMerken(); this.leistenZeichnen(); }
  }

  ziehen(phase, d) {
    if (phase === 'gesturestart') { this.ziehPos = { x: d.clientX, y: d.clientY }; return; }
    const p = this.ziehPos;
    if (!p) return;
    this.ziel.scrollLeft += p.x - d.clientX;
    this.ziel.scrollTop += p.y - d.clientY;
    this.ziehPos = phase === 'gestureend' ? null : { x: d.clientX, y: d.clientY };
  }

  // Strg+Umschalt+K: Suche (Palette). noVNC faengt Tasten an der Leinwand ab und schickte sie sonst
  // an das Geraet.
  kuerzel(e) {
    if (!e.ctrlKey || !e.shiftKey || e.altKey || e.metaKey || (e.code !== 'KeyK' && String(e.key).toLowerCase() !== 'k')) return;
    if (!App.paletteObj || !App.status.angemeldet || dialogStapel.length) return;
    e.preventDefault();
    e.stopPropagation();
    // Strg und Umschalt sind beim Geraet schon als gedrueckt angekommen; die Palette nimmt den Fokus,
    // das Loslassen erreicht noVNC nicht mehr. Ohne ausdrueckliches Loslassen blieben sie dort haengen.
    const r = this.rfb;
    if (r && this.zustand === 'bereit' && window.KeywayRFB) {
      const K = window.KeywayRFB.KeyTable;
      for (const [sym, code] of [[K.XK_Control_L, 'ControlLeft'], [K.XK_Control_R, 'ControlRight'], [K.XK_Shift_L, 'ShiftLeft'], [K.XK_Shift_R, 'ShiftRight']]) {
        try { r.sendKey(sym, code, false); } catch (err) { /* Verbindung gerade weg */ }
      }
    }
    this.modsZuruecksetzen();
    App.palette();
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
    if (this.nurAnsehen) { toast(t('vnc.tasten.nurAnsehen')); return; }
    this.bedient();
    f(r, window.KeywayRFB.KeyTable);
    toast(t('vnc.tasten.gesendet', { taste: name }));
    this.fokus();
  }

  // Desktop: Sondertasten als Dialog (Touch-Geraete haben die Tastenreihe).
  tastenDialog() {
    if (!this.rfb) return;
    if (this.nurAnsehen) { toast(t('vnc.tasten.nurAnsehen')); return; }
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

  // Tastenreihe (Touch): Esc, Tab, Feststeller Strg/Alt/Umschalt/Win, Pfeile, Pos1/Ende, Bild auf/ab,
  // Entf, Funktionstasten (Menue), Strg+Alt+Entf. Wie beim Terminal: pointerdown ohne Fokuswechsel,
  // eine offene Bildschirmtastatur bleibt offen; ein Feststeller gilt fuer die naechste Taste, das
  // naechste Zeichen oder den naechsten Klick.
  tastenZeileBauen() {
    const taste = (beschr, aktion, x) => el('button', Object.assign({ type: 'button', class: (x && x.klasse) || null, 'aria-label': (x && x.name) || beschr, title: (x && x.name) || beschr,
      onpointerdown: (e) => e.preventDefault(), onclick: (e) => aktion(e.currentTarget) }, x && x.attrs), x && x.icon ? icon(x.icon, 18) : beschr);
    const senden = (sym, code) => () => this.zeileSenden((r, K) => r.sendKey(K[sym], code));
    const mod = (k, beschr, name) => taste(beschr, () => this.modSetzen(k, !this.mods[k]), { name, attrs: { 'data-mod': k, 'aria-pressed': 'false' } });
    const trenn = () => el('span', { class: 'trenn', 'aria-hidden': 'true' });
    return el('div', { class: 'vnc-tastenzeile', role: 'toolbar', 'aria-label': t('vnc.tasten.zeile') },
      taste(t('vnc.tasten.esc'), senden('XK_Escape', 'Escape')),
      taste(t('vnc.tasten.tab'), senden('XK_Tab', 'Tab')),
      trenn(),
      mod('ctrl', t('app.tasten.strgTaste'), t('vnc.tasten.strgName')),
      mod('alt', 'Alt', t('vnc.tasten.altName')),
      mod('shift', t('vnc.tasten.umschalt'), t('vnc.tasten.umschaltName')),
      mod('meta', 'Win', t('vnc.tasten.winName')),
      trenn(),
      taste('', senden('XK_Left', 'ArrowLeft'), { icon: 'pfeilLinks', name: t('app.tasten.links') }),
      taste('', senden('XK_Up', 'ArrowUp'), { icon: 'pfeilHoch', name: t('app.tasten.hoch') }),
      taste('', senden('XK_Down', 'ArrowDown'), { icon: 'pfeilRunter', name: t('app.tasten.runter') }),
      taste('', senden('XK_Right', 'ArrowRight'), { icon: 'pfeilRechts', name: t('app.tasten.rechts') }),
      trenn(),
      taste(t('app.tasten.pos1'), senden('XK_Home', 'Home')),
      taste(t('app.tasten.ende'), senden('XK_End', 'End')),
      taste(t('app.tasten.bildHoch'), senden('XK_Page_Up', 'PageUp'), { name: t('app.tasten.bildHochName') }),
      taste(t('app.tasten.bildRunter'), senden('XK_Page_Down', 'PageDown'), { name: t('app.tasten.bildRunterName') }),
      taste(t('vnc.tasten.entf'), senden('XK_Delete', 'Delete'), { name: t('vnc.tasten.entfName') }),
      trenn(),
      taste('F1–F12', (k) => this.funktionsMenue(k), { name: t('vnc.tasten.funktion'), attrs: { 'aria-haspopup': 'menu' } }),
      taste(t('vnc.tasten.cad'), () => this.zeileSenden((r) => r.sendCtrlAltDel(), true), { klasse: 'cad', name: t('vnc.tasten.cadTitel') }));
  }

  zeileUmschalten() {
    this.zeileOffen = !this.zeileOffen;
    lokalSetzen('vncTastenZeile', this.zeileOffen);
    this.element.classList.toggle('zeile-offen', this.zeileOffen);
    if (!this.zeileOffen) this.modsZuruecksetzen();
    this.leistenZeichnen();
  }

  // Taste aus der Tastenreihe; ohneMods: die Feststeller gelten nicht (Strg+Alt+Entf).
  zeileSenden(f, ohneMods) {
    const r = this.rfb;
    if (!r || this.zustand !== 'bereit') return;
    if (this.nurAnsehen) { this.nurAnsehenHinweis(); return; }
    this.bedient();
    const K = window.KeywayRFB.KeyTable;
    if (ohneMods) { this.modsZuruecksetzen(); f(r, K); return; }
    this.mitMods(() => f(r, K));
  }

  // Funktionstasten und Tasten, die allein gedrueckt werden (Windows-Taste, Druck, Alt+Tab). Danach
  // geht eine vorher offene Bildschirmtastatur wieder auf (das Menue nimmt ihr den Fokus).
  funktionsMenue(anker) {
    const tastaturWarOffen = document.activeElement === this.eingabe;
    const nachher = () => { if (tastaturWarOffen) this.tastaturOeffnen(); };
    const eintrag = (text, f, klasse) => ({ text, klasse, aktion: () => { this.zeileSenden(f); nachher(); } });
    const liste = [];
    for (let i = 1; i <= 12; i++) liste.push(eintrag('F' + i, (r, K) => r.sendKey(K['XK_F' + i], 'F' + i), 'f'));
    liste.push(
      eintrag(t('vnc.tasten.win'), (r, K) => r.sendKey(K.XK_Super_L, 'MetaLeft')),
      eintrag(t('vnc.tasten.altTab'), (r, K) => { r.sendKey(K.XK_Alt_L, 'AltLeft', true); r.sendKey(K.XK_Tab, 'Tab'); r.sendKey(K.XK_Alt_L, 'AltLeft', false); }),
      eintrag(t('vnc.tasten.druck'), (r, K) => r.sendKey(K.XK_Print, 'PrintScreen')),
      eintrag(t('vnc.tasten.einfg'), (r, K) => r.sendKey(K.XK_Insert, 'Insert')));
    menue(anker, liste, { klasse: 'vnc-f-menue' });
  }

  // Feststeller (Strg, Alt, Umschalt, Win) der Tastenreihe
  modSetzen(k, an) {
    this.mods[k] = an;
    const b = this.tastenZeile && this.tastenZeile.querySelector('[data-mod=' + k + ']');
    if (b) { b.classList.toggle('an', an); b.setAttribute('aria-pressed', an ? 'true' : 'false'); }
  }

  modsAktiv() { return Object.values(this.mods).some(Boolean); }

  modsZuruecksetzen() {
    for (const k of Object.keys(this.mods)) if (this.mods[k]) this.modSetzen(k, false);
  }

  // Gesetzte Feststeller am Geraet druecken bzw. wieder loslassen (und loesen).
  modsDruecken() {
    const r = this.rfb;
    this.modsUnten = [];
    if (!r || this.nurAnsehen) return;
    const K = window.KeywayRFB.KeyTable;
    for (const [k, [sym, code]] of Object.entries(VNC_MODS)) {
      if (!this.mods[k]) continue;
      r.sendKey(K[sym], code, true);
      this.modsUnten.push([K[sym], code]);
    }
  }

  modsLoslassen() {
    const r = this.rfb;
    const unten = this.modsUnten || [];
    this.modsUnten = [];
    if (r) for (const [sym, code] of unten.reverse()) r.sendKey(sym, code, false);
    this.modsZuruecksetzen();
  }

  mitMods(f) {
    if (!this.modsAktiv()) { f(); return; }
    this.modsDruecken();
    try { f(); } finally { this.modsLoslassen(); }
  }

  // Telefon: Bildschirmtastatur ueber das unsichtbare Eingabefeld (fokussieren oeffnet sie).
  tastaturUmschalten() {
    if (document.activeElement === this.eingabe) { this.eingabe.blur(); return; }
    this.tastaturOeffnen();
  }

  tastaturOeffnen() {
    if (!this.rfb || this.nurAnsehen) return;
    this.eingabeLeeren();
    this.eingabe.focus({ preventScroll: true });
  }

  eingabeLeeren() {
    this.eingabe.value = '_'.repeat(VNC_EINGABE_LAENGE);
    this.eingabeAlt = this.eingabe.value;
    try { this.eingabe.setSelectionRange(VNC_EINGABE_LAENGE, VNC_EINGABE_LAENGE); } catch (e) { /* egal */ }
  }

  // Getippter Text: Unterschied zum vorherigen Inhalt als Tasten senden (Loeschen = Rueckschritt,
  // Autokorrektur ersetzt Zeichen). Gesetzte Feststeller gelten fuer die erste Taste.
  eingabeText() {
    const f = this.eingabe, r = this.rfb;
    if (!r || this.zustand !== 'bereit' || this.nurAnsehen) { this.eingabeLeeren(); return; }
    this.bedient();
    const K = window.KeywayRFB;
    const neu = f.value, alt = this.eingabeAlt || '';
    let rein = neu.length - alt.length;
    let zurueck = rein < 0 ? -rein : 0;
    for (let i = 0; i < Math.min(alt.length, neu.length); i++) {
      if (neu[i] !== alt[i]) { rein = neu.length - i; zurueck = alt.length - i; break; }
    }
    for (let i = 0; i < zurueck; i++) this.mitMods(() => r.sendKey(K.KeyTable.XK_BackSpace, 'Backspace'));
    if (rein > 0) for (const z of neu.slice(neu.length - rein)) this.mitMods(() => this.zeichenSenden(z));
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
    if (!r || this.zustand !== 'bereit' || this.nurAnsehen) return;
    if (e.isComposing || e.keyCode === 229 || e.key === 'Unidentified') return; // Text kommt ueber «input»
    const K = window.KeywayRFB.KeyTable;
    const sonder = {
      Enter: [K.XK_Return, 'Enter'], Backspace: [K.XK_BackSpace, 'Backspace'], Tab: [K.XK_Tab, 'Tab'], Escape: [K.XK_Escape, 'Escape'],
      Delete: [K.XK_Delete, 'Delete'], ArrowLeft: [K.XK_Left, 'ArrowLeft'], ArrowRight: [K.XK_Right, 'ArrowRight'], ArrowUp: [K.XK_Up, 'ArrowUp'],
      ArrowDown: [K.XK_Down, 'ArrowDown'], Home: [K.XK_Home, 'Home'], End: [K.XK_End, 'End'], PageUp: [K.XK_Page_Up, 'PageUp'], PageDown: [K.XK_Page_Down, 'PageDown'],
    };
    const s = sonder[e.key] || (/^F([1-9]|1[0-2])$/.test(e.key) ? [K['XK_' + e.key], e.key] : null);
    if (s) { e.preventDefault(); this.mitMods(() => r.sendKey(s[0], s[1])); return; }
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
    // Ohne erweiterte Zwischenablage des Servers spricht RFB nur Latin-1: Andere Zeichen kommen als
    // «?» an. Das steht vor dem Senden unter dem Feld.
    const warnung = el('div', { class: 'meldung warn versteckt', role: 'status' });
    const pruefen = () => {
      const fremd = this.erweiterteAblage() ? [] : [...new Set([...feld.value].filter((z) => z.codePointAt(0) > 0xff))];
      warnung.classList.toggle('versteckt', !fremd.length);
      if (fremd.length) ersetzen(warnung, icon('warnung'), el('span', null, t('vnc.ablage.latin1', { zeichen: fremd.slice(0, 12).join(' ') + (fremd.length > 12 ? ' …' : '') })));
    };
    feld.addEventListener('input', pruefen);
    pruefen();
    const holen = navigator.clipboard && navigator.clipboard.readText ? el('button', { class: 'btn klein', type: 'button', onclick: async () => {
      try { feld.value = await navigator.clipboard.readText(); pruefen(); feld.focus(); } catch (e) { toast(t('vnc.ablage.lokalFehler'), 'fehler'); }
    } }, icon('einfuegen', 16), t('vnc.ablage.lokal')) : null;
    const senden = () => {
      const text = feld.value;
      if (!text || !this.rfb) return;
      if (this.nurAnsehen) { toast(t('vnc.tasten.nurAnsehen')); return; } // noVNC sendet dann nichts
      this.bedient();
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
        warnung,
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

  // Kann der Server Text als UTF-8 empfangen (Extended Clipboard)? noVNC merkt es sich intern.
  erweiterteAblage() {
    try { return !!(this.rfb && this.rfb._clipboardServerCapabilitiesFormats && this.rfb._clipboardServerCapabilitiesFormats[1]); } catch (e) { return false; }
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
      const inp = el('input', { id, type: typ === 'password' ? 'password' : 'text', autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false',
        oninput: () => this.bedient() });
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
        if (wert) { this.bedient(); this.rfb.sendCredentials(wert); } else this.trennen();
      },
    });
    this.rueckfrage = d;
    setTimeout(() => { const f = felder.username || felder.password || Object.values(felder)[0]; if (f) f.focus(); }, 40);
  }

  // Durchreich-Modus mit Schluessel des Geraets (RealVNC RA2). Wie beim SSH-Host-Schluessel merkt
  // Keyway ihn (Server: /api/hosts/{id}/vnc-schluessel, in «bekannt» unter Adresse und VNC-Port) und
  // fragt nur beim ersten Mal oder wenn er sich geaendert hat. Angezeigt wird die Signatur so, wie
  // RealVNC sie am Geraet zeigt (SHA-1, erste 8 Bytes, mit Bindestrichen); gemerkt und verglichen
  // wird der SHA-256-Fingerabdruck.
  async serverPruefen(detail) {
    const rfb = this.rfb;
    const roh = detail.publickey;
    const schluessel = roh instanceof Uint8Array ? roh : new Uint8Array(roh || 0);
    const typ = String(detail.type || 'RSA');
    let signatur = '', fp = '';
    try {
      const h1 = new Uint8Array(await crypto.subtle.digest('SHA-1', schluessel));
      signatur = Array.from(h1.slice(0, 8), (b) => b.toString(16).padStart(2, '0')).join('-');
      const h2 = new Uint8Array(await crypto.subtle.digest('SHA-256', schluessel));
      fp = 'SHA256:' + btoa(String.fromCharCode(...h2)).replace(/=+$/, '');
    } catch (e) { /* ohne WebCrypto gibt es kein RA2 (noVNC braucht es selbst) */ }
    if (!rfb || this.rfb !== rfb) return;
    if (!fp) { this.trennen(); return; }
    // Vergleich beim Server: status «neu», «gleich» oder «geaendert» (mit dem bisher gemerkten)
    let status = 'neu', alt = null;
    if (this.hostId) {
      try {
        const b = await api('GET', '/api/hosts/' + this.hostId + '/vnc-schluessel?typ=' + encodeURIComponent(typ) + '&fingerprint=' + encodeURIComponent(fp));
        if (b && b.status) {
          status = b.status;
          if (status === 'geaendert') alt = { typ: b.altTyp || '', fingerprint: b.altFingerprint || '' };
        } else if (b && b.fingerprint) { // ohne Vergleich: selbst vergleichen
          status = b.fingerprint === fp && (!b.typ || b.typ === typ) ? 'gleich' : 'geaendert';
          if (status === 'geaendert') alt = { typ: b.typ || '', fingerprint: b.fingerprint };
        }
      } catch (e) { if (e.abgemeldet) return; /* nicht lesbar: wie beim ersten Mal fragen */ }
    }
    if (this.rfb !== rfb) return;
    if (status === 'gleich') { rfb.approveServer(); return; }
    const geaendert = status === 'geaendert' && !!alt;
    const bekannt = alt;
    let ok = false;
    const feld = (label, wert) => el('div', { class: 'feld' }, el('span', { class: 'label' }, label), el('div', { class: 'fp' }, wert));
    const d = dialog({
      titel: geaendert ? t('vnc.pruefen.titelGeaendert') : t('vnc.pruefen.titel'),
      inhalt: el('div', { class: 'vnc-form' },
        geaendert ? el('div', { class: 'meldung fehler', role: 'alert' }, icon('warnung'), el('span', null, el('b', null, t('app.hostkey.vorsicht')), ' ', t('vnc.pruefen.geaendert')))
          : el('p', { class: 'frage-text' }, t('vnc.pruefen.text')),
        feld(t('vnc.pruefen.signatur'), signatur),
        geaendert ? feld(t('app.hostkey.bisher') + ' · ' + (bekannt.typ || typ), bekannt.fingerprint) : null,
        feld((geaendert ? t('app.hostkey.jetzt') : t('app.hostkey.fingerabdruck')) + ' · ' + typ, fp)),
      knoepfe: [
        el('button', { class: 'btn', type: 'button', onclick: () => d.zu() }, t('basis.abbrechen')),
        el('button', { class: 'btn ' + (geaendert ? 'gefahr' : 'primaer'), type: 'button', onclick: () => { ok = true; d.zu(); } },
          geaendert ? t('vnc.pruefen.annehmen') : t('vnc.pruefen.vertrauen')),
      ],
      beimSchliessen: () => {
        if (this.rueckfrage === d) this.rueckfrage = null;
        if (this.rfb !== rfb) return;
        if (!ok) { this.trennen(); return; }
        this.bedient();
        rfb.approveServer();
        if (this.hostId) api('POST', '/api/hosts/' + this.hostId + '/vnc-schluessel', { typ, fingerprint: fp, ersetzen: geaendert }).catch((e) => fehlerToast(e));
      },
    });
    this.rueckfrage = d;
  }

  // ---------- Vorschaubild, Groesse, Statusleiste ----------

  // Verkleinertes JPEG des aktuellen Bilds: als Vorschau gespeichert (falls eingeschaltet) und als
  // letztes Bild nach dem Trennen gezeigt. Laeuft auch bei «Nur ansehen». Die Leinwand von noVNC
  // (immer das ganze Bild in voller Aufloesung) wird direkt verkleinert gezeichnet — ohne
  // getImageData (Chrome warnt sonst vor wiederholtem Auslesen) und ohne Zwischenleinwand.
  vorschauSpeichern() {
    const c = this.leinwand();
    if (!this.rfb || !this.verbundenSeit || !c || !c.width || !c.height) return;
    const b = Math.min(VNC_VORSCHAU_BREITE, c.width);
    const h = Math.max(1, Math.round(c.height * b / c.width));
    const klein = el('canvas', { width: String(b), height: String(h) });
    let url = '';
    try {
      const ctx = klein.getContext('2d');
      ctx.imageSmoothingEnabled = true;
      ctx.imageSmoothingQuality = 'high';
      ctx.drawImage(c, 0, 0, b, h);
      for (const q of [0.7, 0.55, 0.4, 0.25]) {
        url = klein.toDataURL('image/jpeg', q);
        if (url.length <= VNC_VORSCHAU_MAX) break;
      }
    } catch (e) { return; }
    if (!url.startsWith('data:image/jpeg') || url.length > VNC_VORSCHAU_MAX) return;
    this.letztesBild = url;
    if (this.hostId && App.vorschau && App.vorschau.speichern(this.hostId, url)) vncKartenNachfuehren(this.hostId);
  }

  // Aufloesung des Geraets: Die Zeichenflaeche von noVNC hat sie (Statusleiste, Zoom, Fernaufloesung).
  groesseBeobachten() {
    if (this.groesseWache) this.groesseWache.disconnect();
    const c = this.leinwand();
    if (!c || !window.MutationObserver) return;
    this.groesseWache = new MutationObserver(() => this.groesseGeaendert());
    this.groesseWache.observe(c, { attributes: true, attributeFilter: ['width', 'height'] });
    this.groesseGeaendert();
  }

  // Neue Aufloesung des Geraets: Zoom-Buehne nachfuehren. Weicht sie wegen der Fernaufloesung von der
  // urspruenglichen ab, merkt sich der Browser die urspruengliche (Zuruecksetzen auch nach einem
  // Absturz des Tabs); ist sie wieder erreicht, faellt die Notiz weg.
  groesseGeaendert() {
    const c = this.leinwand();
    if (c && c.width && this.ursprung && this.hostId) {
      if (!this.aufloesungAnders()) { if (lokal('vncFernOffen.' + this.hostId, null)) lokalSetzen('vncFernOffen.' + this.hostId, null); }
      else if (this.einst.skalierung === 'fern') lokalSetzen('vncFernOffen.' + this.hostId, this.ursprung);
    }
    if (this.wirksameAnsicht() === 'zoom') this.ansichtAnwenden();
    this.anpassen();
    this.leistenZeichnen();
  }

  groesseText() {
    const c = this.zustand === 'bereit' ? this.leinwand() : null;
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
      this.nurAnsehen ? el('span', { class: 'vnc-weg warn' }, icon('auge', 13), t('vnc.nurAnsehen')) : null,
    ];
  }

  // Wie TerminalTab.anpassen: Aufruf bei Groessenwechseln. noVNC passt sich selbst an (ResizeObserver);
  // hier nur die Anzeige und der Wechsel zwischen Telefon- und Desktop-Ansicht.
  anpassen() {
    if (App.aktiv === this && App.groesseAnzeige) App.groesseAnzeige.textContent = this.groesseText();
    const telefon = App.istTelefon();
    this.element.classList.toggle('touch', vncTouch());
    if (telefon !== this.warTelefon) {
      this.warTelefon = telefon;
      if (this.rfb) { this.rfb.focusOnClick = !vncTouch(); this.ansichtAnwenden(); }
      this.leistenZeichnen();
      return;
    }
    // Andere Flaeche (Drehen, Fenster): Prozentanzeige von «Anpassen», Verschieben-Knopf
    const stand = this.prozentText() + '|' + (this.zustand === 'bereit' && this.verschiebbar());
    if (stand !== this.anzeigeStand) { this.anzeigeStand = stand; this.leistenZeichnen(); }
  }

  aktuelleZeile() { return ''; }
  farbenNeu() { /* noVNC zeichnet ohne Akzentfarbe */ }

  schliessen() {
    if (this.zustand === 'bereit') {
      this.vorschauSpeichern();
      // Fernaufloesung zuruecksetzen: Die Verbindung lebt ohne Tab weiter, bis das Geraet antwortet
      // (hoechstens 1 s), dann trennt sie.
      if (this.aufloesungZurueck()) {
        const r = this.rfb;
        this.rfb = null;
        this.ws = null;
        vncNachZurueck(this.leinwand(), this.ursprung, () => { try { r.disconnect(); } catch (e) { /* schon zu */ } });
      }
    }
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
