'use strict';
// Linke Seitenleiste: Hosts (nach Ordnern), Tresor (Zugaenge) und Snippets, dazu der Host-Dialog.

// Farben fuer Hosts: gespeichert wird der Wert, der Name (Katalog hosts.farbe.<id>) ist fuer
// Tooltip und Screenreader.
const FARBEN = [
  { wert: '#5DB8A6', id: 'tuerkis' },
  { wert: '#E9A23B', id: 'orange' },
  { wert: '#E06C5F', id: 'rot' },
  { wert: '#6FA8DC', id: 'blau' },
  { wert: '#C792EA', id: 'violett' },
  { wert: '#5C6370', id: 'grau' },
];
function farbName(wert) {
  if (!wert) return t('hosts.farbe.keine');
  const f = FARBEN.find((x) => x.wert.toLowerCase() === wert.toLowerCase());
  return f ? t('hosts.farbe.' + f.id) : t('hosts.farbe.eigene');
}

// «1 Host» bzw. «n Hosts».
function hostsAnzahl(n) { return t(n === 1 ? 'hosts.anzahl.host' : 'hosts.anzahl.hosts', { n }); }

// Zusaetzliche Strich-Symbole (Auge fuer «Passwort anzeigen»).
const AUGE = 'M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12zM12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z';
const AUGE_ZU = 'M3 3l18 18M10.6 5.1A10.4 10.4 0 0 1 12 5c6.5 0 10 7 10 7a17.6 17.6 0 0 1-3.1 4M6.6 6.6C3.7 8.5 2 12 2 12s3.5 7 10 7c1.9 0 3.6-.6 5-1.5M9.9 9.9a3 3 0 0 0 4.2 4.2';

// Was Antippen der Host-Zeile oeffnet: das Terminal — ausser bei einem HMI-Panel, das nur ueber VNC
// direkt erreichbar ist (kein SSH-Zugang, kein Benutzername): dann gleich den Bildschirm.
function hostHauptAktion(h) {
  return h && h.vnc && h.vncWeg === 'direkt' && h.geraet === 'hmi' && !h.zugangId && !h.benutzer ? 'vnc' : 'terminal';
}
App.hostHauptAktion = hostHauptAktion;

// ---------- Vorschaubilder (VNC) ----------
// Nur in diesem Browser: localStorage «sshapp.vnc.vorschau.<hostId>» = JPEG-Data-URL (vnc.js
// schreibt sie beim Trennen und alle 30 s). Einstellung «Vorschaubilder speichern» unter
// lokal('vncVorschau') = true|false, Vorgabe an; aus = keine neuen Bilder, die alten sind geloescht.
const VORSCHAU_PRAEFIX = 'sshapp.vnc.vorschau.';
const VORSCHAU_FORM = /^data:image\/(jpeg|png|webp);base64,[A-Za-z0-9+/=]+$/;
App.vorschau = {
  an: () => lokal('vncVorschau', true) !== false,
  // Data-URL des letzten Bildes oder '' (auch bei ausgeschalteter Einstellung).
  lesen(hostId) {
    if (!App.vorschau.an() || !hostId) return '';
    let v = null;
    try { v = localStorage.getItem(VORSCHAU_PRAEFIX + hostId); } catch (e) { return ''; }
    if (v && v[0] === '"') { try { v = JSON.parse(v); } catch (e) { v = null; } }
    return typeof v === 'string' && VORSCHAU_FORM.test(v) ? v : '';
  },
  // Fuer vnc.js: neues Bild merken (nur bei eingeschalteter Einstellung, hoechstens 64 KB) und das
  // kleine Bild in der Host-Zeile gleich austauschen, ohne die Liste neu zu zeichnen (Filterfeld).
  speichern(hostId, dataUrl) {
    const url = String(dataUrl || '');
    if (!App.vorschau.an() || !hostId || url.length > 64 * 1024 || !VORSCHAU_FORM.test(url)) return false;
    try { localStorage.setItem(VORSCHAU_PRAEFIX + hostId, url); } catch (e) { return false; }
    for (const k of document.querySelectorAll('.host-zeile[data-host-id="' + Number(hostId) + '"] .aktion-bildschirm')) {
      const bild = k.querySelector('.mini-bild');
      if (bild) { bild.src = url; continue; }
      const symbol = k.querySelector('svg.ic');
      if (symbol) symbol.replaceWith(el('img', { class: 'mini-bild', src: url, alt: '' }));
      k.classList.add('mit-bild');
      k.removeAttribute('title');
    }
    return true;
  },
  loeschen(hostId) { try { localStorage.removeItem(VORSCHAU_PRAEFIX + hostId); } catch (e) { /* privat */ } },
  // Alle gespeicherten Vorschaubilder: { n, bytes }.
  bestand() {
    let n = 0, bytes = 0;
    try {
      for (let i = 0; i < localStorage.length; i++) {
        const k = localStorage.key(i);
        if (k && k.startsWith(VORSCHAU_PRAEFIX)) { n++; bytes += (localStorage.getItem(k) || '').length; }
      }
    } catch (e) { /* privat */ }
    return { n, bytes };
  },
  alleLoeschen() {
    const weg = [];
    try {
      for (let i = 0; i < localStorage.length; i++) { const k = localStorage.key(i); if (k && k.startsWith(VORSCHAU_PRAEFIX)) weg.push(k); }
      for (const k of weg) localStorage.removeItem(k);
    } catch (e) { /* privat */ }
    vorschauVerbergen();
    if (App.seitenleiste) App.seitenleiste.zeichnen();
    return weg.length;
  },
  // Bilder zu Hosts, die es nicht mehr gibt oder die kein VNC mehr haben, entfernen (etwa in einem
  // anderen Browser geloescht): Ein neu angelegter Host mit derselben ID zeigte sonst das Bild
  // eines alten Geraets. Aufruf nach jedem Laden der Daten (App.datenLaden).
  aufraeumen(hosts) {
    const mitVnc = new Set((hosts || []).filter((h) => h.vnc).map((h) => VORSCHAU_PRAEFIX + h.id));
    const weg = [];
    try {
      for (let i = 0; i < localStorage.length; i++) { const k = localStorage.key(i); if (k && k.startsWith(VORSCHAU_PRAEFIX) && !mitVnc.has(k)) weg.push(k); }
      for (const k of weg) localStorage.removeItem(k);
    } catch (e) { /* privat */ }
    return weg.length;
  },
  // Ein- oder ausschalten; liefert die Zahl der dabei geloeschten Bilder.
  setzen(an) {
    lokalSetzen('vncVorschau', !!an);
    if (!an) return App.vorschau.alleLoeschen();
    if (App.seitenleiste) App.seitenleiste.zeichnen();
    return 0;
  },
};

// Desktop mit Maus: grosses Vorschaubild neben der Leiste, solange der Zeiger auf dem
// Bildschirm-Knopf eines Hosts liegt (Touch: das kleine Bild im Knopf genuegt).
let vorschauKarte = null;
const mitMaus = window.matchMedia('(hover: hover) and (pointer: fine)');
function vorschauZeigen(knopf, h, url) {
  vorschauVerbergen();
  if (!mitMaus.matches || !url) return;
  const bild = el('img', { src: url, alt: '' });
  const karte = el('div', { class: 'vorschau-karte', 'aria-hidden': 'true' }, bild,
    el('div', { class: 'vorschau-unter' }, icon('bildschirm', 14), el('span', null, t('hosts.vorschau.letztes', { name: h.name }))));
  vorschauKarte = karte;
  document.body.append(karte);
  const setzen = () => {
    if (vorschauKarte !== karte || !knopf.isConnected) { vorschauVerbergen(); return; }
    const r = knopf.getBoundingClientRect();
    const leiste = knopf.closest('.seite');
    const links = Math.min((leiste ? leiste.getBoundingClientRect().right : r.right) + 10, window.innerWidth - karte.offsetWidth - 8);
    const oben = Math.max(8, Math.min(window.innerHeight - karte.offsetHeight - 8, r.top + r.height / 2 - karte.offsetHeight / 2));
    karte.style.left = Math.max(8, links) + 'px';
    karte.style.top = oben + 'px';
  };
  setzen();
  bild.addEventListener('load', setzen);
}
function vorschauVerbergen() {
  if (vorschauKarte) { vorschauKarte.remove(); vorschauKarte = null; }
}

class Seitenleiste {
  constructor() {
    this.reiter = lokal('reiter', 'hosts');
    if (!['hosts', 'tresor', 'snippets'].includes(this.reiter)) this.reiter = 'hosts';
    this.filter = '';
    this.zu = new Set(lokal('ordnerZu', []));
    this.inhalt = el('div', { class: 'seite-inhalt' });
    this.reiterLeiste = el('div', { class: 'reiter', role: 'tablist', 'aria-label': t('hosts.leiste.reiterLabel') });
    this.element = el('aside', { class: 'seite links', 'aria-label': t('hosts.leiste.titel') },
      el('div', { class: 'seite-kopf nur-telefon' }, el('span', { class: 'titel' }, t('hosts.leiste.titel')), ikonKnopf('x', t('hosts.knopf.schliessen'), () => App.seiteUmschalten('links', false))),
      this.reiterLeiste, this.inhalt);
    this.zeichnen();
  }

  reiterSetzen(r) { this.reiter = r; lokalSetzen('reiter', r); this.zeichnen(); }

  zeichnen() {
    vorschauVerbergen();
    const r = (id, text) => el('button', { type: 'button', role: 'tab', 'aria-selected': this.reiter === id ? 'true' : 'false', onclick: () => this.reiterSetzen(id) }, text);
    ersetzen(this.reiterLeiste, r('hosts', t('hosts.reiter.hosts')), r('tresor', t('hosts.reiter.tresor')), r('snippets', t('hosts.reiter.snippets')));
    if (this.reiter === 'tresor') this.tresorZeichnen();
    else if (this.reiter === 'snippets') this.snippetsZeichnen();
    else this.hostsZeichnen();
  }

  // ---------- Hosts ----------

  hostsZeichnen() {
    const d = App.daten;
    if (!d.hosts.length && !d.ordner.length) {
      ersetzen(this.inhalt, el('div', { class: 'leiste-leer' },
        el('span', { class: 'geraet-feld gross' }, geraetIcon('server', 24)),
        el('b', null, t('hosts.leer.titel')),
        el('p', null, t('hosts.leer.text')),
        el('button', { class: 'btn primaer voll', type: 'button', onclick: () => App.hostBearbeiten({}) }, icon('plus'), t('hosts.leiste.hostAnlegen')),
        el('button', { class: 'btn voll', type: 'button', onclick: () => ordnerAnlegen() }, icon('ordnerPlus'), t('hosts.leiste.ordnerAnlegen'))));
      return;
    }
    const liste = el('div', { class: 'host-liste' });
    const filterFeld = el('input', { type: 'search', placeholder: t('hosts.leiste.filter'), value: this.filter, 'aria-label': t('hosts.leiste.filterLabel'), autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false', enterkeyhint: 'go',
      oninput: () => { this.filter = filterFeld.value; this.listeZeichnen(liste); },
      // Enter verbindet, wenn genau ein Host passt.
      onkeydown: (e) => {
        if (e.key !== 'Enter') return;
        const treffer = this.treffer();
        if (treffer.length === 1) { e.preventDefault(); this.verbinden(treffer[0], hostHauptAktion(treffer[0])); }
      } });
    ersetzen(this.inhalt,
      el('div', { class: 'leiste-suche' },
        el('label', { class: 'suchfeld' }, icon('suche', 16), filterFeld),
        ikonKnopf('plus', t('hosts.leiste.neu'), (e) => neuMenue(e.currentTarget), { class: 'ikon plus-knopf', 'aria-haspopup': 'menu' })),
      liste);
    this.listeZeichnen(liste);
  }

  passt(h) {
    const q = this.filter.trim().toLowerCase();
    return !q || [h.name, h.adresse, h.benutzer, geraetName(h.geraet), h.vnc ? 'vnc' : '', ...(h.tags || [])].join(' ').toLowerCase().includes(q);
  }

  treffer() { return App.daten.hosts.filter((h) => this.passt(h)); }

  // art: 'terminal' (Vorgabe) oder 'vnc' (Bildschirm).
  verbinden(h, art) {
    vorschauVerbergen();
    App.verbinden(h, { art: art === 'vnc' ? 'vnc' : 'terminal' });
    App.seiteUmschalten('links', false, true);
  }

  listeZeichnen(liste) {
    vorschauVerbergen();
    const d = App.daten;
    const q = this.filter.trim();
    const verbunden = new Set(App.offeneTabs ? App.offeneTabs().filter((x) => x.zustand === 'bereit').map((x) => x.hostId) : []);
    const aktivId = App.aktiv ? App.aktiv.hostId : 0;
    const gruppen = d.ordner.map((o) => ({ o, hosts: d.hosts.filter((h) => h.ordnerId === o.id && this.passt(h)) }));
    const ohne = d.hosts.filter((h) => !d.ordner.some((o) => o.id === h.ordnerId) && this.passt(h));
    if (ohne.length) gruppen.push({ o: { id: 0, name: t('hosts.leiste.ohneOrdner') }, hosts: ohne });
    const mitKoepfen = d.ordner.length > 0; // ohne Ordner: schlichte Liste, kein Kopf
    const kinder = [];
    for (const g of gruppen) {
      if (q && !g.hosts.length) continue;
      const istZu = mitKoepfen && this.zu.has(g.o.id) && !q;
      if (mitKoepfen) kinder.push(this.ordnerKopf(g, istZu, liste));
      if (istZu) continue;
      if (!g.hosts.length) kinder.push(el('div', { class: 'ordner-leer' }, t('hosts.leiste.ordnerLeer')));
      for (const h of g.hosts) kinder.push(this.hostEintrag(h, verbunden.has(h.id), h.id === aktivId));
    }
    if (!kinder.length) kinder.push(el('div', { class: 'leer' }, q ? t('hosts.leiste.keinTreffer', { q }) : t('hosts.leiste.nochKeine')));
    ersetzen(liste, kinder);
  }

  ordnerKopf(g, istZu, liste) {
    const umschalten = () => {
      if (istZu) this.zu.delete(g.o.id); else this.zu.add(g.o.id);
      lokalSetzen('ordnerZu', [...this.zu]);
      this.listeZeichnen(liste);
    };
    const anzahl = g.hosts.length;
    return el('div', { class: 'ordner-kopf' },
      el('button', { class: 'ordner-knopf', type: 'button', 'aria-expanded': istZu ? 'false' : 'true', onclick: umschalten },
        el('span', { class: 'chevron' }, icon('weiter', 14)),
        el('span', { class: 'name' }, g.o.name),
        el('span', { class: 'anz' }, String(anzahl), el('span', { class: 'sr' }, ' ' + t(anzahl === 1 ? 'hosts.wort.host' : 'hosts.wort.hosts')))),
      g.o.id ? ikonKnopf('mehr', t('hosts.leiste.ordnerBearbeiten', { name: g.o.name }), (e) => ordnerMenue(g.o, e.currentTarget), { class: 'ikon ordner-mehr', 'aria-haspopup': 'menu' }) : null);
  }

  // Host mit Bildschirm (VNC): zwei Aktionen, Terminal und Bildschirm. Touch: beide immer sichtbar;
  // Maus: die Aktion, die nicht schon das Antippen der Zeile ist, beim Ueberfahren (wie der Stift).
  // Der Bildschirm-Knopf zeigt das letzte Vorschaubild, falls eines gespeichert ist.
  hostEintrag(h, verbunden, aktiv) {
    const geraet = geraetName(h.geraet);
    const vnc = !!h.vnc;
    const haupt = hostHauptAktion(h);
    const feld = el('span', { class: 'geraet-feld', title: geraet, 'data-farbe': h.farbe ? true : null },
      geraetIcon(h.geraet, 18),
      verbunden ? el('span', { class: 'status-punkt' }) : null);
    if (h.farbe) feld.style.setProperty('--hostfarbe', h.farbe);
    let aktionen = null;
    if (vnc) {
      const bild = App.vorschau.lesen(h.id);
      const lBild = t('hosts.leiste.bildschirmOeffnen', { name: h.name });
      const kBild = el('button', { class: 'ikon aktion-bildschirm' + (bild ? ' mit-bild' : ''), type: 'button', 'aria-label': lBild, title: bild ? null : lBild,
        onclick: () => this.verbinden(h, 'vnc'),
        onmouseenter: (e) => vorschauZeigen(e.currentTarget, h, App.vorschau.lesen(h.id)), onmouseleave: vorschauVerbergen },
      bild ? el('img', { class: 'mini-bild', src: bild, alt: '' }) : icon('bildschirm', 18),
      el('span', { class: 'aktion-text', 'aria-hidden': 'true' }, t('hosts.leiste.bildschirm')));
      const lTerm = t('hosts.leiste.terminalOeffnen', { name: h.name });
      // Die Woerter erscheinen nur, wenn die Aktionen in schmalen Leisten unter dem Namen stehen (hosts.css).
      aktionen = el('span', { class: 'host-aktionen' + (haupt === 'vnc' ? ' haupt-vnc' : '') },
        el('button', { class: 'ikon aktion-terminal', type: 'button', 'aria-label': lTerm, title: lTerm, onclick: () => this.verbinden(h, 'terminal') },
          icon('terminal'), el('span', { class: 'aktion-text', 'aria-hidden': 'true' }, t('hosts.leiste.terminal'))),
        kBild);
    }
    return el('div', { class: 'host-zeile' + (aktiv ? ' aktiv' : '') + (vnc ? ' mit-vnc' : ''), 'data-host-id': h.id },
      el('button', { class: 'zeile', type: 'button', 'aria-current': aktiv ? 'true' : null, onclick: () => this.verbinden(h, haupt) },
        feld,
        el('span', { class: 'haupt' }, el('b', null, h.name),
          el('small', null, vnc ? el('span', { class: 'vnc-marke', title: t('hosts.leiste.mitBildschirm') }, icon('bildschirm', 12)) : null, hostZeile(h))),
        el('span', { class: 'sr' }, ', ' + geraet + (haupt === 'vnc' ? ', ' + t('hosts.leiste.oeffnetBildschirm') : '') + (verbunden ? ', ' + t('hosts.leiste.verbunden') : ''))),
      aktionen,
      ikonKnopf('stift', t('hosts.leiste.hostBearbeiten', { name: h.name }), () => App.hostBearbeiten(h), { class: 'ikon bearbeiten' }));
  }

  // ---------- Tresor ----------

  tresorZeichnen() {
    const d = App.daten;
    const zeilen = d.zugaenge.map((z) => {
      const schluessel = z.art === 'schluessel';
      const art = schluessel ? (z.schluesselTyp || t('hosts.art.schluessel')).replace('ssh-', '') + (z.hatPassphrase ? ' ' + t('hosts.art.passphrase') : '') : t('hosts.art.passwort');
      return el('div', { class: 'host-zeile' },
        el('button', { class: 'zeile', type: 'button', onclick: () => App.zugangBearbeiten(z) },
          el('span', { class: 'geraet-feld ' + (schluessel ? 'art-schluessel' : 'art-passwort') }, icon(schluessel ? 'schluessel' : 'schloss', 17)),
          el('span', { class: 'haupt' }, el('b', null, z.name),
            el('small', null, [z.benutzer || t('hosts.tresor.ohneBenutzer'), art, hostsAnzahl(z.hosts || 0)].join(' · ')))));
    });
    ersetzen(this.inhalt,
      el('div', { class: 'leiste-knoepfe' },
        el('button', { class: 'btn primaer voll', type: 'button', onclick: () => App.zugangBearbeiten({}) }, icon('plus'), t('hosts.tresor.zugangAnlegen')),
        el('button', { class: 'btn voll', type: 'button', onclick: () => App.schluesselErzeugen() }, icon('schluessel'), t('hosts.tresor.schluesselErzeugen'))),
      zeilen.length
        ? [el('div', { class: 'abschnitt' }, t('hosts.tresor.zugaenge'), el('span', { class: 'anz' }, String(zeilen.length))), el('div', { class: 'host-liste' }, zeilen)]
        : el('div', { class: 'leer' }, t('hosts.tresor.leer')),
      el('div', { class: 'meldung gut tresor-hinweis' }, icon('schloss'),
        el('span', null, t('hosts.tresor.hinweis', { min: App.status.leerlaufMin || 30 }))));
  }

  // ---------- Snippets ----------

  snippetsZeichnen() {
    const d = App.daten;
    const zeilen = d.snippets.map((s) => el('div', { class: 'host-zeile' },
      el('button', { class: 'zeile', type: 'button', title: t('hosts.snippets.uebernehmen'), onclick: () => App.befehlVorbereiten(s.befehl, s.name) },
        el('span', { class: 'geraet-feld' }, icon('terminal', 17)),
        el('span', { class: 'haupt' }, el('b', null, s.name), el('small', null, s.befehl))),
      ikonKnopf('stift', t('hosts.snippets.bearbeiten', { name: s.name }), () => App.snippetBearbeiten(s), { class: 'ikon bearbeiten' })));
    ersetzen(this.inhalt,
      el('div', { class: 'leiste-knoepfe' }, el('button', { class: 'btn primaer voll', type: 'button', onclick: () => App.snippetBearbeiten({}) }, icon('plus'), t('hosts.snippets.anlegen'))),
      zeilen.length
        ? [el('div', { class: 'abschnitt' }, t('hosts.snippets.titel'), el('span', { class: 'anz' }, String(zeilen.length))), el('div', { class: 'host-liste' }, zeilen)]
        : el('div', { class: 'leer' }, tTeile('hosts.snippets.leer', { beispiel: el('code', null, 'journalctl -u {{' + t('hosts.snippet.platzhalterName') + '}}') })));
  }
}

// Zweite Zeile eines Hosts: benutzer@adresse:port (ohne Text, gilt in jeder Sprache).
function hostZeile(h) {
  const z = App.daten.zugaenge.find((x) => x.id === h.zugangId);
  const benutzer = h.benutzer || (z && z.benutzer);
  return (benutzer ? benutzer + '@' : '') + h.adresse + (h.port && h.port !== 22 ? ':' + h.port : '');
}

function neuMenue(anker) {
  menue(anker, [
    { text: t('hosts.leiste.hostAnlegen'), icon: 'plus', aktion: () => App.hostBearbeiten({}) },
    { text: t('hosts.leiste.ordnerAnlegen'), icon: 'ordnerPlus', aktion: () => ordnerAnlegen() },
  ]);
}

async function ordnerAnlegen() {
  const n = await eingabe(t('hosts.ordner.name'), { titel: t('hosts.ordner.neu'), ok: t('hosts.ordner.anlegen') });
  if (!n || !n.trim()) return null;
  try { const o = await api('POST', '/api/ordner', { name: n.trim(), rang: App.daten.ordner.length }); await App.datenLaden(); return o; }
  catch (e) { fehlerToast(e); return null; }
}

function ordnerMenue(o, anker) {
  menue(anker, [
    { text: t('hosts.ordner.umbenennen'), icon: 'stift', aktion: async () => {
      const n = await eingabe(t('hosts.ordner.name'), { titel: t('hosts.ordner.umbenennenTitel'), wert: o.name, ok: t('hosts.knopf.speichern') });
      if (n && n.trim()) api('PUT', '/api/ordner/' + o.id, { name: n.trim(), rang: o.rang }).then(App.datenLaden).catch(fehlerToast);
    } },
    { text: t('hosts.ordner.loeschen'), icon: 'muell', gefahr: true, aktion: async () => {
      if (await frage(t('hosts.ordner.loeschenFrage', { name: o.name }), { ja: t('hosts.knopf.loeschen'), gefahr: true })) api('DELETE', '/api/ordner/' + o.id).then(App.datenLaden).catch(fehlerToast);
    } },
  ]);
}

// ---------- Host-Dialog ----------

// ssh-Optionen mit Wert («-p 2222», «-i datei», «-l root» …); der Wert steht direkt dahinter oder im
// naechsten Wort.
const SSH_OPTIONEN_MIT_WERT = 'BbcDEeFIiJLlmOoPpQRSWw';

// Zerlegt «benutzer@host:port», «[fe80::1]:2222», «ssh://…», «host -p 2222» und ssh-Aufrufe wie
// «ssh -p 2222 -l root host» (Ziel = erstes Wort, das keine Option ist; ein Befehl dahinter faellt weg).
// Ohne «ssh» davor ist ein Leerzeichen kein Trenner, sondern ein Fehler: dann ist r.leerzeichen
// gesetzt und r.adresse die unveraenderte Eingabe — nichts wird still abgeschnitten.
function adresseZerlegen(text) {
  const roh = String(text || '').trim();
  const r = {};
  const ssh = /^ssh\s/i.test(roh);
  const woerter = roh.split(/\s+/).filter(Boolean);
  if (ssh) woerter.shift();
  const rest = [];
  for (let i = 0; i < woerter.length; i++) {
    const w = woerter[i];
    if (!ssh) {
      // Ohne «ssh» gilt nur «-p 2222» bzw. «-p2222» als Option.
      const p = w.match(/^-p(\d*)$/);
      const wert = p ? p[1] || woerter[i + 1] : '';
      if (p && /^\d+$/.test(wert || '')) { r.port = +wert; if (!p[1]) i++; } else rest.push(w);
      continue;
    }
    if (!/^-./.test(w)) { rest.push(w); continue; }
    for (let j = 1; j < w.length; j++) {
      if (!SSH_OPTIONEN_MIT_WERT.includes(w[j])) continue; // Schalter wie -v oder -4
      const wert = j + 1 < w.length ? w.slice(j + 1) : woerter[++i];
      if (w[j] === 'p' && /^\d+$/.test(wert || '')) r.port = +wert;
      if (w[j] === 'l' && wert) r.benutzer = wert;
      break;
    }
  }
  if (!ssh && rest.length > 1) { r.leerzeichen = true; r.adresse = roh; return r; }
  let s = (rest[0] || '').replace(/^ssh:\/\//i, '').replace(/\/+$/, '');
  const at = s.lastIndexOf('@');
  if (at >= 0) { if (at > 0) r.benutzer = s.slice(0, at); s = s.slice(at + 1); }
  const klammer = s.match(/^\[([^\]]+)\](?::(\d+))?$/);
  if (klammer) { s = klammer[1]; if (klammer[2]) r.port = +klammer[2]; }
  else {
    const m = s.match(/^([^:]+):(\d+)$/); // genau ein Doppelpunkt: host:port (IPv6 ohne Klammern bleibt ganz)
    if (m) { s = m[1]; r.port = +m[2]; }
  }
  r.adresse = s;
  return r;
}

function zugangText(z) {
  return z.name + (z.benutzer ? ' (' + z.benutzer + ')' : '') + ' · ' + t(z.art === 'schluessel' ? 'hosts.art.schluessel' : 'hosts.art.passwort');
}

App.hostBearbeiten = function (h) {
  const d = App.daten;
  const neu = !h.id;
  const zugangVon = (id) => d.zugaenge.find((z) => z.id === id);
  // Eigener Zugang: Passwort-Zugang, den nur dieser Host nutzt und der nach ihm benannt ist
  // (angelegt ueber «Passwort speichern» hier oder beim Verbinden). Alles andere ist ein
  // Zugang aus dem Tresor (Schluessel oder gemeinsam genutzt) und wird unter «Erweitert» gewaehlt.
  const bisher = zugangVon(h.zugangId);
  const eigen = bisher && bisher.art === 'passwort' && (bisher.hosts || 0) <= 1 && (bisher.name === h.name || bisher.name === h.adresse) ? bisher : null;
  const tresorZugang = bisher && !eigen ? bisher : null;
  const andereZugaenge = d.zugaenge.filter((z) => !eigen || z.id !== eigen.id);

  const feld = (id, label, input, hinweis) => el('div', { class: 'feld' }, el('label', { for: id }, label), input, hinweis ? el('div', { class: 'hinweis' }, hinweis) : null);
  const text = (id, wert, extra) => el('input', Object.assign({ id, type: 'text', value: wert || '', autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false' }, extra || {}));
  const f = {};
  let dlg = null;

  // Fehlermeldung oben im Dialog
  const meldung = el('div', { class: 'meldung fehler versteckt', role: 'alert' });
  const fehlerZeigen = (text) => {
    ersetzen(meldung, icon('warnung'), el('span', null, text));
    meldung.classList.remove('versteckt');
    if (dlg) dlg.element.scrollTo({ top: 0, behavior: 'smooth' });
  };

  // --- Geraetetyp: Knopf mit Symbol und Name, darunter das Raster zum Waehlen ---
  let geraet = GERAETE.some((g) => g.id === (h.geraet || '')) ? (h.geraet || '') : '';
  const gwBild = el('span', { class: 'geraet-feld gross' });
  const gwName = el('span', { class: 'gw-name' });
  const gwKnopf = el('button', { type: 'button', class: 'gw-knopf', 'aria-expanded': 'false', 'aria-controls': 'h-geraete', onclick: () => rasterSetzen(gwKnopf.getAttribute('aria-expanded') !== 'true') },
    gwBild,
    el('span', { class: 'gw-text' }, el('span', { class: 'gw-label' }, t('hosts.host.geraetetyp')), gwName),
    el('span', { class: 'gw-aendern' }, t('hosts.knopf.aendern'), el('span', { class: 'chevron' }, icon('weiter', 14))));
  const gwRaster = el('div', { id: 'h-geraete', class: 'gw-raster versteckt', role: 'group', 'aria-label': t('hosts.host.geraetWaehlen') },
    GERAETE_GRUPPEN.map((g) => el('div', { class: 'gw-gruppe' },
      el('div', { class: 'gw-gruppe-titel' }, geraetGruppeName(g)),
      el('div', { class: 'gw-kacheln' }, GERAETE.filter((x) => x.gruppe === g.id).map((x) => el('button', {
        type: 'button', class: 'gw-kachel', 'data-id': x.id, 'aria-pressed': 'false', title: geraetName(x.id),
        onclick: () => { geraet = x.id; geraetZeigen(); rasterSetzen(false); if (!App.istTelefon()) gwKnopf.focus({ preventScroll: true }); },
      }, geraetIcon(x.id, 22), el('span', null, geraetKurz(x.id))))))));
  const geraetZeigen = () => {
    ersetzen(gwBild, geraetIcon(geraet, 24));
    gwName.textContent = geraetName(geraet);
    for (const k of gwRaster.querySelectorAll('.gw-kachel')) k.setAttribute('aria-pressed', k.dataset.id === geraet ? 'true' : 'false');
  };
  const rasterSetzen = (auf) => {
    gwKnopf.setAttribute('aria-expanded', auf ? 'true' : 'false');
    gwRaster.classList.toggle('versteckt', !auf);
  };
  geraetZeigen();

  // --- Einfache Felder ---
  // Name gleich Adresse (Vorgabe): Feld leer lassen, dann folgt der Name einer neuen Adresse.
  f.name = text('h-name', h.name && h.name !== h.adresse ? h.name : '', { placeholder: t('hosts.host.namePlatzhalter') });
  const namePlatzhalter = () => { f.name.placeholder = f.adresse.value.trim() || t('hosts.host.namePlatzhalter'); };
  f.adresse = text('h-adresse', h.adresse, { placeholder: t('hosts.host.adressePlatzhalter'), inputmode: 'url', required: true,
    'aria-describedby': 'h-adresse-hinweis',
    oninput: () => { namePlatzhalter(); f.adresse.removeAttribute('aria-invalid'); adrHinweisSetzen(t('hosts.host.adresseHinweis')); },
    onchange: () => adresseUebernehmen() });
  const adrHinweis = el('div', { id: 'h-adresse-hinweis', class: 'hinweis' }, t('hosts.host.adresseHinweis'));
  // Hinweis unter der Adresse; als Fehler rot und mit aria-invalid am Feld.
  const adrHinweisSetzen = (text, fehler) => {
    adrHinweis.textContent = text;
    adrHinweis.classList.toggle('fehler', !!fehler);
    if (fehler) f.adresse.setAttribute('aria-invalid', 'true');
  };
  f.benutzer = text('h-benutzer', h.benutzer || (eigen && eigen.benutzer) || '', { placeholder: t('hosts.host.benutzerPlatzhalter') });
  f.ordner = el('select', { id: 'h-ordner', onchange: async () => {
    if (f.ordner.value === 'neu') { const o = await ordnerAnlegen(); ordnerOptionen(o ? o.id : 0); }
  } });
  const ordnerOptionen = (wahl) => ersetzen(f.ordner, el('option', { value: 0 }, t('hosts.host.ohneOrdner')),
    App.daten.ordner.map((o) => el('option', { value: o.id, selected: o.id === wahl }, o.name)), el('option', { value: 'neu' }, t('hosts.host.neuerOrdner')));
  ordnerOptionen(h.ordnerId || 0);

  // Passwort: direkt eintragen; ohne Haken fragt die App beim Verbinden.
  f.passwort = el('input', { id: 'h-passwort', type: 'password', autocomplete: 'new-password', autocapitalize: 'off', spellcheck: 'false' });
  const pwZeigen = el('button', { type: 'button', class: 'pw-zeigen', 'aria-label': t('hosts.host.pwAnzeigen'), 'aria-pressed': 'false', onclick: () => {
    const sichtbar = f.passwort.type === 'password';
    f.passwort.type = sichtbar ? 'text' : 'password';
    pwZeigen.setAttribute('aria-pressed', sichtbar ? 'true' : 'false');
    pwZeigen.setAttribute('aria-label', t(sichtbar ? 'hosts.host.pwVerbergen' : 'hosts.host.pwAnzeigen'));
    ersetzen(pwZeigen, strichIcon(sichtbar ? AUGE_ZU : AUGE, 18));
  } }, strichIcon(AUGE, 18));
  f.merken = el('input', { type: 'checkbox', id: 'h-merken', checked: true, onchange: () => passwortZeigen() });
  const pwHinweis = el('div', { class: 'hinweis' });
  const passwortZeigen = () => {
    const an = f.merken.checked;
    f.passwort.disabled = !an;
    pwZeigen.disabled = !an;
    if (!an) f.passwort.value = '';
    f.passwort.placeholder = t(!an ? 'hosts.host.pwAbfragen' : eigen && eigen.hatPasswort ? 'hosts.host.pwGespeichert' : 'hosts.host.pwLeer');
    pwHinweis.textContent = an ? '' : t(eigen ? 'hosts.host.pwWirdGeloescht' : 'hosts.host.pwFragtImmer');
    pwHinweis.classList.toggle('versteckt', an);
  };
  const pwBereich = el('div', { class: 'feld' },
    el('label', { for: 'h-passwort' }, t('hosts.feld.passwort')),
    el('div', { class: 'pw-feld' }, f.passwort, pwZeigen),
    el('label', { class: 'check pw-merken' }, f.merken, el('span', null, t('hosts.host.pwSpeichern'), el('span', { class: 'leise' }, ' · ' + t('hosts.host.pwVerschluesselt')))),
    pwHinweis);
  passwortZeigen();

  // Anmeldung ueber einen Zugang aus dem Tresor (statt Passwort): Hinweis anstelle des Passwortfelds.
  const zugangInfo = el('div', { class: 'feld' });
  const anmeldungZeigen = () => {
    const z = f.anmeldung.value === 'eigen' ? null : zugangVon(+f.anmeldung.value);
    pwBereich.classList.toggle('versteckt', !!z);
    zugangInfo.classList.toggle('versteckt', !z);
    f.benutzer.placeholder = z && z.benutzer ? t('hosts.host.benutzerAusZugang', { benutzer: z.benutzer }) : t('hosts.host.benutzerPlatzhalter');
    if (z) {
      ersetzen(zugangInfo,
        el('span', { class: 'label' }, t('hosts.host.anmeldung')),
        el('div', { class: 'zugang-info' },
          el('span', { class: 'geraet-feld ' + (z.art === 'schluessel' ? 'art-schluessel' : 'art-passwort') }, icon(z.art === 'schluessel' ? 'schluessel' : 'schloss', 16)),
          el('span', { class: 'haupt' }, el('b', null, z.name), el('small', null, t(z.art === 'schluessel' ? 'hosts.host.schluesselAusTresor' : 'hosts.host.passwortAusTresor'))),
          el('button', { class: 'btn klein', type: 'button', onclick: () => { erweitertSetzen(true); f.anmeldung.focus(); } }, t('hosts.knopf.aendern'))));
    }
  };

  // --- Erweitert ---
  f.port = el('input', { id: 'h-port', type: 'number', inputmode: 'numeric', min: 1, max: 65535, value: h.port || 22 });
  f.anmeldung = el('select', { id: 'h-anmeldung', onchange: () => anmeldungZeigen() },
    el('option', { value: 'eigen' }, t('hosts.host.passwortOben')),
    andereZugaenge.length ? el('optgroup', { label: t('hosts.host.ausTresor') },
      andereZugaenge.map((z) => el('option', { value: z.id, selected: !!tresorZugang && z.id === tresorZugang.id }, zugangText(z)))) : null);
  f.tags = text('h-tags', (h.tags || []).join(', '), { placeholder: t('hosts.host.tagsPlatzhalter') });
  f.folgen = el('input', { type: 'checkbox', checked: h.pfadFolgen !== false });
  f.notiz = el('textarea', { id: 'h-notiz', class: 'notiz' }, h.notiz || '');
  let farbe = h.farbe || '';
  const farbKnoepfe = el('div', { class: 'farbwahl', role: 'group', 'aria-labelledby': 'h-farbe-label' });
  const farbenZeichnen = () => ersetzen(farbKnoepfe, [{ wert: '' }, ...FARBEN].map((c) => {
    const n = c.wert ? t('hosts.farbe.mitName', { name: farbName(c.wert) }) : farbName('');
    return el('button', { type: 'button', class: 'farbknopf', 'aria-label': n, title: n, 'aria-pressed': c.wert === farbe ? 'true' : 'false',
      onclick: () => { farbe = c.wert; farbenZeichnen(); zusammenfassen(); } },
    el('span', { class: 'farbpunkt' + (c.wert ? '' : ' ohne'), style: { background: c.wert || 'transparent' } }));
  }));
  farbenZeichnen();

  // --- Bildschirm (VNC) ---
  // Weg «ssh» (Vorgabe): Keyway meldet sich per SSH an und verbindet sich dort mit 127.0.0.1:Port.
  // Weg «direkt»: TCP zur Adresse des Hosts, unverschluesselt (HMI-Panels ohne SSH).
  // Passwort: verschluesselt im Tresor; leer = unveraendert, ohne gespeichertes fragt Keyway beim Verbinden.
  f.vnc = el('input', { type: 'checkbox', id: 'h-vnc', class: 'schalter', role: 'switch', checked: !!h.vnc, 'aria-describedby': 'h-vnc-hinweis' });
  f.vncPort = el('input', { id: 'h-vnc-port', type: 'number', inputmode: 'numeric', min: 1, max: 65535, value: h.vncPort || 5900, 'aria-describedby': 'h-vnc-port-hinweis' });
  let vncWeg = h.vncWeg === 'direkt' ? 'direkt' : 'ssh';
  const wegKarte = (wert, titel, text, marke) => el('label', { class: 'weg-karte' },
    el('input', { type: 'radio', name: 'h-vnc-weg', value: wert, checked: vncWeg === wert, onchange: () => { vncWeg = wert; vncZeigen(); } }),
    el('span', { class: 'weg-text' }, el('span', { class: 'weg-titel' }, el('b', null, titel), marke), el('small', null, text)));
  const wegWahl = el('div', { class: 'weg-wahl', role: 'radiogroup', 'aria-labelledby': 'h-vnc-weg-label' },
    wegKarte('ssh', t('hosts.vnc.wegSsh'), t('hosts.vnc.wegSshText'), el('span', { class: 'weg-marke gut' }, t('hosts.vnc.empfohlen'))),
    wegKarte('direkt', t('hosts.vnc.wegDirekt'), t('hosts.vnc.wegDirektText'), el('span', { class: 'weg-marke warn' }, t('hosts.vnc.unverschluesselt'))));
  const direktWarnung = el('div', { class: 'meldung warn vnc-warnung' }, icon('warnung'), el('span', null, t('hosts.vnc.direktWarnung')));
  const vncHatPw = !!h.hatVncPasswort;
  f.vncPw = el('input', { id: 'h-vnc-pw', type: 'password', autocomplete: 'new-password', autocapitalize: 'off', spellcheck: 'false',
    placeholder: t(vncHatPw ? 'hosts.vnc.pwGespeichert' : 'hosts.vnc.pwLeer'), 'aria-describedby': 'h-vnc-pw-hinweis', oninput: () => vncPwHinweis() });
  const vncPwZeigen = el('button', { type: 'button', class: 'pw-zeigen', 'aria-label': t('hosts.host.pwAnzeigen'), 'aria-pressed': 'false', onclick: () => {
    const sichtbar = f.vncPw.type === 'password';
    f.vncPw.type = sichtbar ? 'text' : 'password';
    vncPwZeigen.setAttribute('aria-pressed', sichtbar ? 'true' : 'false');
    vncPwZeigen.setAttribute('aria-label', t(sichtbar ? 'hosts.host.pwVerbergen' : 'hosts.host.pwAnzeigen'));
    ersetzen(vncPwZeigen, strichIcon(sichtbar ? AUGE_ZU : AUGE, 18));
  } }, strichIcon(AUGE, 18));
  f.vncPwWeg = el('input', { type: 'checkbox', id: 'h-vnc-pw-weg', onchange: () => vncZeigen() });
  const vncPwHinweisEl = el('div', { id: 'h-vnc-pw-hinweis', class: 'hinweis' });
  const vncPortHinweis = el('div', { id: 'h-vnc-port-hinweis', class: 'hinweis' });
  const vncPwHinweis = () => {
    if (f.vncPwWeg.checked) vncPwHinweisEl.textContent = t('hosts.vnc.pwWirdGeloescht');
    else if (f.vncPw.value.length > 8) vncPwHinweisEl.textContent = t('hosts.vnc.pwAcht');
    else vncPwHinweisEl.textContent = t(vncHatPw ? 'hosts.vnc.pwHinweisGespeichert' : 'hosts.vnc.pwHinweis');
  };
  const vncFelder = el('div', { id: 'h-vnc-felder', class: 'vnc-felder' },
    el('div', { class: 'feld' }, el('span', { class: 'label', id: 'h-vnc-weg-label' }, t('hosts.vnc.weg')), wegWahl),
    direktWarnung,
    el('div', { class: 'raster2 vnc-raster' },
      el('div', { class: 'feld' }, el('label', { for: 'h-vnc-port' }, t('hosts.vnc.port')), f.vncPort,
        vncPortHinweis),
      el('div', { class: 'feld' }, el('label', { for: 'h-vnc-pw' }, t('hosts.vnc.passwort')),
        el('div', { class: 'pw-feld' }, f.vncPw, vncPwZeigen),
        vncHatPw ? el('label', { class: 'check pw-merken' }, f.vncPwWeg, el('span', null, t('hosts.vnc.pwEntfernen'))) : null,
        vncPwHinweisEl)));
  const vncAusHinweis = el('div', { class: 'hinweis vnc-aus-hinweis' });
  const vncZeigen = () => {
    const an = f.vnc.checked;
    vncFelder.classList.toggle('versteckt', !an);
    direktWarnung.classList.toggle('versteckt', vncWeg !== 'direkt');
    vncPortHinweis.textContent = t(vncWeg === 'direkt' ? 'hosts.vnc.portHinweis' : 'hosts.vnc.portHinweisSsh');
    // Aus mit gespeichertem Passwort: es wird beim Speichern geloescht (wie beim SSH-Passwort).
    vncAusHinweis.textContent = !an && vncHatPw ? t('hosts.vnc.ausPwWeg') : '';
    vncAusHinweis.classList.toggle('versteckt', an || !vncHatPw);
    f.vncPw.disabled = f.vncPwWeg.checked;
    vncPwZeigen.disabled = f.vncPwWeg.checked;
    if (f.vncPwWeg.checked) f.vncPw.value = '';
    vncPwHinweis();
  };
  f.vnc.addEventListener('change', () => vncZeigen());
  const vncTeil = el('div', { class: 'vnc-teil' },
    el('label', { class: 'check vnc-schalter', for: 'h-vnc' },
      el('span', { class: 'geraet-feld vnc-symbol' }, icon('bildschirm', 18)),
      el('span', { class: 'vnc-schalter-text' }, el('b', null, t('hosts.vnc.titel')),
        el('span', { id: 'h-vnc-hinweis', class: 'check-hinweis' }, t('hosts.vnc.hinweis'))),
      f.vnc),
    vncAusHinweis,
    vncFelder);
  vncZeigen();

  const tagListe = () => f.tags.value.split(',').map((x) => x.trim()).filter(Boolean);
  const erwText = el('small');
  const zusammenfassen = () => {
    const teile = [];
    const port = parseInt(f.port.value, 10);
    if (port && port !== 22) teile.push(t('hosts.host.erwPort', { port }));
    const z = f.anmeldung.value === 'eigen' ? null : zugangVon(+f.anmeldung.value);
    if (z) teile.push(t(z.art === 'schluessel' ? 'hosts.host.erwSchluessel' : 'hosts.host.erwZugang', { name: z.name }));
    if (f.vnc.checked) teile.push(t(vncWeg === 'direkt' ? 'hosts.vnc.erwDirekt' : 'hosts.vnc.erwSsh'));
    const tags = tagListe();
    if (tags.length) teile.push(tags.join(', '));
    if (farbe) teile.push(farbName(farbe));
    if (f.notiz.value.trim()) teile.push(t('hosts.host.erwNotiz'));
    if (!f.folgen.checked) teile.push(t('hosts.host.erwFolgtNicht'));
    erwText.textContent = teile.length ? teile.join(' · ') : t('hosts.host.erwLeer');
  };
  const erwBereich = el('div', { id: 'h-erweitert', class: 'erweitert versteckt' },
    feld('h-anmeldung', t('hosts.host.anmeldenMit'), f.anmeldung, t(andereZugaenge.length ? 'hosts.host.anmeldenHinweis' : 'hosts.host.anmeldenHinweisLeer')),
    el('div', { class: 'raster2' },
      feld('h-port', t('hosts.host.port'), f.port),
      feld('h-tags', t('hosts.host.tags'), f.tags, t('hosts.host.tagsHinweis'))),
    vncTeil,
    el('div', { class: 'feld' }, el('span', { class: 'label', id: 'h-farbe-label' }, t('hosts.host.farbe')), farbKnoepfe),
    el('label', { class: 'check' }, f.folgen, el('span', null, el('b', null, t('hosts.host.folgen')), el('br'),
      el('span', { class: 'check-hinweis' }, t('hosts.host.folgenHinweis')))),
    feld('h-notiz', t('hosts.host.notiz'), f.notiz));
  const erwKnopf = el('button', { type: 'button', class: 'erweitert-knopf', 'aria-expanded': 'false', 'aria-controls': 'h-erweitert',
    onclick: () => erweitertSetzen(erwKnopf.getAttribute('aria-expanded') !== 'true') },
  el('span', { class: 'chevron' }, icon('weiter', 16)),
  el('span', { class: 'erw-titel' }, el('b', null, t('hosts.host.erweitert')), erwText));
  const erweitertSetzen = (auf) => {
    erwKnopf.setAttribute('aria-expanded', auf ? 'true' : 'false');
    erwBereich.classList.toggle('versteckt', !auf);
    // Aufgeklappt: «Erweitert» unter den (klebenden) Dialogkopf schieben, damit der Inhalt sichtbar wird.
    if (auf && dlg) {
      const box = dlg.element;
      const kopf = box.querySelector('.dialog-kopf');
      const ziel = erwTeil.getBoundingClientRect().top - box.getBoundingClientRect().top + box.scrollTop - (kopf ? kopf.offsetHeight : 0) - 8;
      setTimeout(() => box.scrollTo({ top: Math.max(0, ziel), behavior: 'smooth' }), 20);
    }
  };

  const erwTeil = el('div', { class: 'erweitert-teil' }, erwKnopf, erwBereich);

  // «benutzer@host:port» aus dem Adressfeld auf Benutzer und Port verteilen. Eine Adresse mit
  // Leerzeichen (kein ssh-Aufruf) bleibt stehen und bekommt eine sichtbare Meldung.
  const adresseUebernehmen = () => {
    const roh = f.adresse.value.trim();
    if (!/[@\s[]|:\d+$|^ssh:/i.test(roh)) return;
    const r = adresseZerlegen(roh);
    if (r.leerzeichen) { adrHinweisSetzen(t('hosts.host.fehlerAdresseLeerzeichen'), true); return; }
    const neuWerte = [];
    f.adresse.value = r.adresse;
    if (r.benutzer && r.benutzer !== f.benutzer.value.trim()) { f.benutzer.value = r.benutzer; neuWerte.push(t('hosts.host.uebBenutzer', { benutzer: r.benutzer })); }
    if (r.port && String(r.port) !== String(f.port.value)) { f.port.value = r.port; neuWerte.push(t('hosts.host.uebPort', { port: r.port })); }
    const was = neuWerte.length === 2 ? t('hosts.host.uebZwei', { a: neuWerte[0], b: neuWerte[1] }) : neuWerte[0];
    adrHinweisSetzen(was ? t('hosts.host.uebernommen', { was }) : t('hosts.host.adresseHinweis'));
    namePlatzhalter();
    zusammenfassen();
  };

  // --- Speichern: erst den Zugang (Passwort), dann den Host ---
  let laeuft = false;
  const speichern = async () => {
    if (laeuft) return null;
    adresseUebernehmen();
    meldung.classList.add('versteckt');
    const adresse = f.adresse.value.trim();
    if (!adresse) {
      f.adresse.setAttribute('aria-invalid', 'true');
      fehlerZeigen(t('hosts.host.fehlerAdresse'));
      f.adresse.focus();
      return null;
    }
    if (/[\s/@]/.test(adresse)) {
      const text = t(/\s/.test(adresse) ? 'hosts.host.fehlerAdresseLeerzeichen' : 'hosts.host.fehlerAdresseZeichen');
      adrHinweisSetzen(text, true);
      fehlerZeigen(text);
      f.adresse.focus();
      return null;
    }
    const port = f.port.value.trim() === '' ? 22 : Number(f.port.value);
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      erweitertSetzen(true);
      fehlerZeigen(t('hosts.host.fehlerPort'));
      return null;
    }
    const vnc = f.vnc.checked;
    let vncPort = f.vncPort.value.trim() === '' ? 5900 : Number(f.vncPort.value);
    if (!Number.isInteger(vncPort) || vncPort < 1 || vncPort > 65535) {
      if (vnc) {
        erweitertSetzen(true);
        f.vncPort.setAttribute('aria-invalid', 'true');
        fehlerZeigen(t('hosts.vnc.fehlerPort'));
        return null;
      }
      vncPort = 5900; // ausgeschaltet: der Port zaehlt nicht
    }
    f.vncPort.removeAttribute('aria-invalid');
    const name = f.name.value.trim() || adresse;
    const benutzer = f.benutzer.value.trim();
    const pw = f.passwort.value;
    const mitTresorZugang = f.anmeldung.value !== 'eigen';
    laeuft = true;
    knoepfeSperren(true);
    try {
      // 1. Zugang
      let zugangId = 0;
      let neuerZugang = null;
      let pwGeaendert = false;
      const eigenWeg = !!eigen && (mitTresorZugang || !f.merken.checked);
      try {
        if (mitTresorZugang) zugangId = +f.anmeldung.value;
        else if (f.merken.checked && eigen) {
          zugangId = eigen.id;
          if (pw || eigen.name !== name || (eigen.benutzer || '') !== benutzer) {
            await api('PUT', '/api/zugaenge/' + eigen.id, { name, benutzer, passwort: pw });
            pwGeaendert = !!pw;
          }
        } else if (f.merken.checked && pw) {
          neuerZugang = await api('POST', '/api/zugaenge', { name, benutzer, passwort: pw });
          zugangId = neuerZugang.id;
        }
      } catch (e) {
        if (!e.abgemeldet) fehlerZeigen(t('hosts.host.fehlerPwSpeichern', { fehler: e.message }));
        return null;
      }
      // 2. Host
      const daten = {
        name, adresse, port, benutzer, zugangId, ordnerId: +f.ordner.value || 0, geraet,
        tags: tagListe(), farbe, pfadFolgen: f.folgen.checked, notiz: f.notiz.value,
        vnc, vncPort, vncWeg,
        // Neues VNC-Passwort nur, wenn eingetragen; ausgeschaltet oder «entfernen» loescht das gespeicherte.
        vncPasswort: vnc && !f.vncPwWeg.checked ? f.vncPw.value : '',
        vncPasswortEntfernen: vncHatPw && (!vnc || f.vncPwWeg.checked),
      };
      let r;
      try {
        r = neu ? await api('POST', '/api/hosts', daten) : await api('PUT', '/api/hosts/' + h.id, daten);
      } catch (e) {
        if (e.abgemeldet) return null;
        const text = [t('hosts.host.fehlerSpeichern', { fehler: e.message })];
        if (neuerZugang) {
          try { await api('DELETE', '/api/zugaenge/' + neuerZugang.id); }
          catch (e2) { text.push(t('hosts.host.pwSchonImTresor', { name })); }
        } else if (pwGeaendert) text.push(t('hosts.host.pwSchonGespeichert'));
        fehlerZeigen(text.join(' '));
        return null;
      }
      // Ohne Bildschirm kein Vorschaubild mehr.
      if (!vnc && r && r.id) App.vorschau.loeschen(r.id);
      // 3. Eigenen Zugang aufraeumen, wenn der Host ihn nicht mehr nutzt.
      let rest = null;
      if (eigenWeg) {
        try { await api('DELETE', '/api/zugaenge/' + eigen.id); }
        catch (e) { rest = t('hosts.host.pwNochImTresor', { name: eigen.name }); }
      }
      dlg.zu();
      await App.datenLaden();
      const mitPw = !!(neuerZugang || pwGeaendert);
      toast(neu ? t(mitPw ? 'hosts.host.angelegtMitPw' : 'hosts.host.angelegt') : t(mitPw ? 'hosts.host.gespeichertMitPw' : 'hosts.gespeichert'));
      if (rest) toast(rest, 'fehler');
      return r;
    } finally {
      laeuft = false;
      knoepfeSperren(false);
    }
  };

  const loeschen = async () => {
    if (!(await frage(t(eigen ? 'hosts.host.loeschenFrageMitPw' : 'hosts.host.loeschenFrage', { name: h.name }), { ja: t('hosts.knopf.loeschen'), gefahr: true }))) return;
    try {
      await api('DELETE', '/api/hosts/' + h.id);
      App.vorschau.loeschen(h.id);
      if (eigen) await api('DELETE', '/api/zugaenge/' + eigen.id).catch(() => {});
      dlg.zu(); App.datenLaden(); toast(t('hosts.host.geloescht'));
    } catch (e) { if (!e.abgemeldet) fehlerZeigen(t('hosts.host.fehlerLoeschen', { fehler: e.message })); }
  };
  // Telefon: Loeschen steht am Ende von «Erweitert», der Fuss bleibt einzeilig (ohne «Abbrechen»:
  // dafuer gibt es das Kreuz oben und die Zurueck-Geste).
  const kLoeschen = !neu ? el('button', { class: 'btn gefahr links nur-desktop', type: 'button', onclick: loeschen }, icon('muell'), t('hosts.knopf.loeschen')) : null;
  if (!neu) erwBereich.append(el('div', { class: 'nur-telefon loeschen-telefon' }, el('button', { class: 'btn gefahr voll', type: 'button', onclick: loeschen }, icon('muell'), t('hosts.host.loeschen'))));
  const kAbbrechen = el('button', { class: 'btn nur-desktop', type: 'button', onclick: () => dlg.zu() }, t('hosts.knopf.abbrechen'));
  const kVerbinden = el('button', { class: 'btn', type: 'button', onclick: async () => {
    const r = await speichern();
    if (r) App.verbinden(r, { art: hostHauptAktion(r) });
  } }, t('hosts.host.speichernVerbinden'));
  const kSpeichern = el('button', { class: 'btn primaer', type: 'button', onclick: () => speichern() }, t('hosts.knopf.speichern'));
  const knoepfeSperren = (an) => { for (const k of [kLoeschen, kVerbinden, kSpeichern]) if (k) k.disabled = an; };

  const form = el('form', { class: 'host-form', novalidate: true, onsubmit: (e) => { e.preventDefault(); speichern(); },
    oninput: () => zusammenfassen(), onchange: () => zusammenfassen() },
  meldung,
  el('div', { class: 'gw' }, gwKnopf, gwRaster),
  el('div', { class: 'raster2' },
    feld('h-name', t('hosts.host.name'), f.name),
    el('div', { class: 'feld' }, el('label', { for: 'h-adresse' }, t('hosts.host.adresse')), f.adresse, adrHinweis),
    feld('h-benutzer', t('hosts.feld.benutzer'), f.benutzer),
    pwBereich, zugangInfo,
    feld('h-ordner', t('hosts.host.ordner'), f.ordner)),
  erwTeil,
  el('button', { type: 'submit', class: 'versteckt' }));

  anmeldungZeigen();
  namePlatzhalter();
  zusammenfassen();
  dlg = dialog({
    titel: t(neu ? 'hosts.host.titelNeu' : 'hosts.host.titelBearbeiten'), breit: true,
    inhalt: form,
    knoepfe: [kLoeschen, kAbbrechen, kVerbinden, kSpeichern],
  });
  dlg.element.classList.add('host-dialog');
  if (neu && !App.istTelefon()) f.adresse.focus();
};

App.Seitenleiste = Seitenleiste;
