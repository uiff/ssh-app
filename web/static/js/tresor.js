'use strict';
// Linke Seitenleiste: Hosts (nach Ordnern), Tresor (Zugaenge) und Snippets, dazu der Host-Dialog.

// Farben fuer Hosts: gespeichert wird der Wert, der Name ist fuer Tooltip und Screenreader.
const FARBEN = [
  { wert: '#5DB8A6', name: 'Türkis' },
  { wert: '#E9A23B', name: 'Orange' },
  { wert: '#E06C5F', name: 'Rot' },
  { wert: '#6FA8DC', name: 'Blau' },
  { wert: '#C792EA', name: 'Violett' },
  { wert: '#5C6370', name: 'Grau' },
];
function farbName(wert) {
  if (!wert) return 'Keine Farbe';
  const f = FARBEN.find((x) => x.wert.toLowerCase() === wert.toLowerCase());
  return f ? f.name : 'Eigene Farbe';
}

// Zusaetzliche Strich-Symbole (Auge fuer «Passwort anzeigen»).
const AUGE = 'M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12zM12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z';
const AUGE_ZU = 'M3 3l18 18M10.6 5.1A10.4 10.4 0 0 1 12 5c6.5 0 10 7 10 7a17.6 17.6 0 0 1-3.1 4M6.6 6.6C3.7 8.5 2 12 2 12s3.5 7 10 7c1.9 0 3.6-.6 5-1.5M9.9 9.9a3 3 0 0 0 4.2 4.2';

class Seitenleiste {
  constructor() {
    this.reiter = lokal('reiter', 'hosts');
    if (!['hosts', 'tresor', 'snippets'].includes(this.reiter)) this.reiter = 'hosts';
    this.filter = '';
    this.zu = new Set(lokal('ordnerZu', []));
    this.inhalt = el('div', { class: 'seite-inhalt' });
    this.reiterLeiste = el('div', { class: 'reiter', role: 'tablist', 'aria-label': 'Hosts, Tresor oder Snippets' });
    this.element = el('aside', { class: 'seite links', 'aria-label': 'Hosts und Tresor' },
      el('div', { class: 'seite-kopf nur-telefon' }, el('span', { class: 'titel' }, 'Hosts und Tresor'), ikonKnopf('x', 'Schliessen', () => App.seiteUmschalten('links', false))),
      this.reiterLeiste, this.inhalt);
    this.zeichnen();
  }

  reiterSetzen(r) { this.reiter = r; lokalSetzen('reiter', r); this.zeichnen(); }

  zeichnen() {
    const r = (id, text) => el('button', { type: 'button', role: 'tab', 'aria-selected': this.reiter === id ? 'true' : 'false', onclick: () => this.reiterSetzen(id) }, text);
    ersetzen(this.reiterLeiste, r('hosts', 'Hosts'), r('tresor', 'Tresor'), r('snippets', 'Snippets'));
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
        el('b', null, 'Noch keine Hosts'),
        el('p', null, 'Eine Adresse genügt. Benutzer und Passwort speicherst du gleich mit oder gibst sie beim Verbinden ein.'),
        el('button', { class: 'btn primaer voll', type: 'button', onclick: () => App.hostBearbeiten({}) }, icon('plus'), 'Host anlegen'),
        el('button', { class: 'btn voll', type: 'button', onclick: () => ordnerAnlegen() }, icon('ordnerPlus'), 'Ordner anlegen')));
      return;
    }
    const liste = el('div', { class: 'host-liste' });
    const filterFeld = el('input', { type: 'search', placeholder: 'Filtern …', value: this.filter, 'aria-label': 'Hosts filtern', autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false', enterkeyhint: 'go',
      oninput: () => { this.filter = filterFeld.value; this.listeZeichnen(liste); },
      // Enter verbindet, wenn genau ein Host passt.
      onkeydown: (e) => {
        if (e.key !== 'Enter') return;
        const treffer = this.treffer();
        if (treffer.length === 1) { e.preventDefault(); this.verbinden(treffer[0]); }
      } });
    ersetzen(this.inhalt,
      el('div', { class: 'leiste-suche' },
        el('label', { class: 'suchfeld' }, icon('suche', 16), filterFeld),
        ikonKnopf('plus', 'Host oder Ordner anlegen', (e) => neuMenue(e.currentTarget), { class: 'ikon plus-knopf', 'aria-haspopup': 'menu' })),
      liste);
    this.listeZeichnen(liste);
  }

  passt(h) {
    const q = this.filter.trim().toLowerCase();
    return !q || [h.name, h.adresse, h.benutzer, geraetFuer(h.geraet).de, ...(h.tags || [])].join(' ').toLowerCase().includes(q);
  }

  treffer() { return App.daten.hosts.filter((h) => this.passt(h)); }

  verbinden(h) {
    App.verbinden(h);
    App.seiteUmschalten('links', false, true);
  }

  listeZeichnen(liste) {
    const d = App.daten;
    const q = this.filter.trim();
    const verbunden = new Set(App.offeneTabs ? App.offeneTabs().filter((t) => t.zustand === 'bereit').map((t) => t.hostId) : []);
    const aktivId = App.aktiv ? App.aktiv.hostId : 0;
    const gruppen = d.ordner.map((o) => ({ o, hosts: d.hosts.filter((h) => h.ordnerId === o.id && this.passt(h)) }));
    const ohne = d.hosts.filter((h) => !d.ordner.some((o) => o.id === h.ordnerId) && this.passt(h));
    if (ohne.length) gruppen.push({ o: { id: 0, name: 'Ohne Ordner' }, hosts: ohne });
    const mitKoepfen = d.ordner.length > 0; // ohne Ordner: schlichte Liste, kein Kopf
    const kinder = [];
    for (const g of gruppen) {
      if (q && !g.hosts.length) continue;
      const istZu = mitKoepfen && this.zu.has(g.o.id) && !q;
      if (mitKoepfen) kinder.push(this.ordnerKopf(g, istZu, liste));
      if (istZu) continue;
      if (!g.hosts.length) kinder.push(el('div', { class: 'ordner-leer' }, 'Leer. Hosts ordnest du im Host-Dialog zu.'));
      for (const h of g.hosts) kinder.push(this.hostEintrag(h, verbunden.has(h.id), h.id === aktivId));
    }
    if (!kinder.length) kinder.push(el('div', { class: 'leer' }, q ? ['Kein Host passt zu «', q, '».'] : 'Noch keine Hosts.'));
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
        el('span', { class: 'anz' }, String(anzahl), el('span', { class: 'sr' }, anzahl === 1 ? ' Host' : ' Hosts'))),
      g.o.id ? ikonKnopf('mehr', 'Ordner «' + g.o.name + '» bearbeiten', (e) => ordnerMenue(g.o, e.currentTarget), { class: 'ikon ordner-mehr', 'aria-haspopup': 'menu' }) : null);
  }

  hostEintrag(h, verbunden, aktiv) {
    const geraet = geraetFuer(h.geraet);
    const feld = el('span', { class: 'geraet-feld', title: geraet.de, 'data-farbe': h.farbe ? true : null },
      geraetIcon(h.geraet, 18),
      verbunden ? el('span', { class: 'status-punkt' }) : null);
    if (h.farbe) feld.style.setProperty('--hostfarbe', h.farbe);
    return el('div', { class: 'host-zeile' + (aktiv ? ' aktiv' : '') },
      el('button', { class: 'zeile', type: 'button', 'aria-current': aktiv ? 'true' : null, onclick: () => this.verbinden(h) },
        feld,
        el('span', { class: 'haupt' }, el('b', null, h.name), el('small', null, hostZeile(h))),
        el('span', { class: 'sr' }, ', ' + geraet.de + (verbunden ? ', verbunden' : ''))),
      ikonKnopf('stift', 'Host «' + h.name + '» bearbeiten', () => App.hostBearbeiten(h), { class: 'ikon bearbeiten' }));
  }

  // ---------- Tresor ----------

  tresorZeichnen() {
    const d = App.daten;
    const zeilen = d.zugaenge.map((z) => {
      const schluessel = z.art === 'schluessel';
      const art = schluessel ? (z.schluesselTyp || 'Schlüssel').replace('ssh-', '') + (z.hatPassphrase ? ' + Passphrase' : '') : 'Passwort';
      return el('div', { class: 'host-zeile' },
        el('button', { class: 'zeile', type: 'button', onclick: () => App.zugangBearbeiten(z) },
          el('span', { class: 'geraet-feld ' + (schluessel ? 'art-schluessel' : 'art-passwort') }, icon(schluessel ? 'schluessel' : 'schloss', 17)),
          el('span', { class: 'haupt' }, el('b', null, z.name),
            el('small', null, [z.benutzer || 'ohne Benutzer', art, z.hosts + (z.hosts === 1 ? ' Host' : ' Hosts')].join(' · ')))));
    });
    ersetzen(this.inhalt,
      el('div', { class: 'leiste-knoepfe' },
        el('button', { class: 'btn primaer voll', type: 'button', onclick: () => App.zugangBearbeiten({}) }, icon('plus'), 'Zugang anlegen'),
        el('button', { class: 'btn voll', type: 'button', onclick: () => App.schluesselErzeugen() }, icon('schluessel'), 'Neuen Schlüssel erzeugen')),
      zeilen.length
        ? [el('div', { class: 'abschnitt' }, 'Zugänge', el('span', { class: 'anz' }, String(zeilen.length))), el('div', { class: 'host-liste' }, zeilen)]
        : el('div', { class: 'leer' }, 'Ein Zugang ist Benutzer plus Passwort oder Schlüssel. Mehrere Hosts können ihn nutzen — so änderst du ein Passwort nur an einer Stelle.'),
      el('div', { class: 'meldung gut tresor-hinweis' }, icon('schloss'),
        el('span', null, 'Passwörter und Schlüssel liegen verschlüsselt. Nach ', String(App.status.leerlaufMin || 30), ' Minuten ohne Bedienung sperrt sich der Tresor.')));
  }

  // ---------- Snippets ----------

  snippetsZeichnen() {
    const d = App.daten;
    const zeilen = d.snippets.map((s) => el('div', { class: 'host-zeile' },
      el('button', { class: 'zeile', type: 'button', title: 'Ins Terminal übernehmen', onclick: () => App.befehlVorbereiten(s.befehl, s.name) },
        el('span', { class: 'geraet-feld' }, icon('terminal', 17)),
        el('span', { class: 'haupt' }, el('b', null, s.name), el('small', null, s.befehl))),
      ikonKnopf('stift', 'Snippet «' + s.name + '» bearbeiten', () => App.snippetBearbeiten(s), { class: 'ikon bearbeiten' })));
    ersetzen(this.inhalt,
      el('div', { class: 'leiste-knoepfe' }, el('button', { class: 'btn primaer voll', type: 'button', onclick: () => App.snippetBearbeiten({}) }, icon('plus'), 'Snippet anlegen')),
      zeilen.length
        ? [el('div', { class: 'abschnitt' }, 'Snippets', el('span', { class: 'anz' }, String(zeilen.length))), el('div', { class: 'host-liste' }, zeilen)]
        : el('div', { class: 'leer' }, 'Snippets sind gespeicherte Befehle. Platzhalter schreibst du so: ', el('code', null, 'journalctl -u {{dienst}}')));
  }
}

// Zweite Zeile eines Hosts: benutzer@adresse:port · via Jump-Host
function hostZeile(h) {
  const z = App.daten.zugaenge.find((x) => x.id === h.zugangId);
  const benutzer = h.benutzer || (z && z.benutzer);
  let s = (benutzer ? benutzer + '@' : '') + h.adresse + (h.port && h.port !== 22 ? ':' + h.port : '');
  const j = h.jumpId && App.daten.hosts.find((x) => x.id === h.jumpId);
  if (j) s += ' · via ' + j.name;
  return s;
}

function neuMenue(anker) {
  menue(anker, [
    { text: 'Host anlegen', icon: 'plus', aktion: () => App.hostBearbeiten({}) },
    { text: 'Ordner anlegen', icon: 'ordnerPlus', aktion: () => ordnerAnlegen() },
  ]);
}

async function ordnerAnlegen() {
  const n = await eingabe('Name des Ordners', { titel: 'Neuer Ordner', ok: 'Anlegen' });
  if (!n || !n.trim()) return null;
  try { const o = await api('POST', '/api/ordner', { name: n.trim(), rang: App.daten.ordner.length }); await App.datenLaden(); return o; }
  catch (e) { fehlerToast(e); return null; }
}

function ordnerMenue(o, anker) {
  menue(anker, [
    { text: 'Umbenennen', icon: 'stift', aktion: async () => {
      const n = await eingabe('Name des Ordners', { titel: 'Ordner umbenennen', wert: o.name, ok: 'Speichern' });
      if (n && n.trim()) api('PUT', '/api/ordner/' + o.id, { name: n.trim(), rang: o.rang }).then(App.datenLaden).catch(fehlerToast);
    } },
    { text: 'Ordner löschen (Hosts bleiben)', icon: 'muell', gefahr: true, aktion: async () => {
      if (await frage(`Ordner «${o.name}» löschen? Die Hosts darin bleiben erhalten.`, { ja: 'Löschen', gefahr: true })) api('DELETE', '/api/ordner/' + o.id).then(App.datenLaden).catch(fehlerToast);
    } },
  ]);
}

// ---------- Host-Dialog ----------

// Zerlegt «benutzer@host:port», «[fe80::1]:2222», «ssh://…» und «ssh -p 2222 root@host».
// Nicht erkannte Teile bleiben weg; die Adresse selbst kommt immer zurueck.
function adresseZerlegen(text) {
  let s = String(text || '').trim().replace(/^ssh\s+/i, '');
  const r = {};
  const p = s.match(/(?:^|\s)-p\s*(\d+)(?=\s|$)/);
  if (p) { r.port = +p[1]; s = s.replace(p[0], ' '); }
  const teile = s.split(/\s+/).filter(Boolean);
  s = teile.find((t) => t.includes('@')) || teile[teile.length - 1] || '';
  s = s.replace(/^ssh:\/\//i, '').replace(/\/+$/, '');
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
  return z.name + (z.benutzer ? ' (' + z.benutzer + ')' : '') + ' · ' + (z.art === 'schluessel' ? 'Schlüssel' : 'Passwort');
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
  const jumpHosts = d.hosts.filter((x) => x.id !== h.id);

  const feld = (id, label, input, hinweis) => el('div', { class: 'feld' }, el('label', { for: id }, label), input, hinweis ? el('div', { class: 'hinweis' }, hinweis) : null);
  const text = (id, wert, extra) => el('input', Object.assign({ id, type: 'text', value: wert || '', autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false' }, extra || {}));
  const f = {};
  let dlg = null;

  // Fehlermeldung oben im Dialog
  const meldung = el('div', { class: 'meldung fehler versteckt', role: 'alert' });
  const fehlerZeigen = (t) => {
    ersetzen(meldung, icon('warnung'), el('span', null, t));
    meldung.classList.remove('versteckt');
    if (dlg) dlg.element.scrollTo({ top: 0, behavior: 'smooth' });
  };

  // --- Geraetetyp: Knopf mit Symbol und Name, darunter das Raster zum Waehlen ---
  let geraet = GERAETE.some((g) => g.id === (h.geraet || '')) ? (h.geraet || '') : '';
  const gwBild = el('span', { class: 'geraet-feld gross' });
  const gwName = el('span', { class: 'gw-name' });
  const gwKnopf = el('button', { type: 'button', class: 'gw-knopf', 'aria-expanded': 'false', 'aria-controls': 'h-geraete', onclick: () => rasterSetzen(gwKnopf.getAttribute('aria-expanded') !== 'true') },
    gwBild,
    el('span', { class: 'gw-text' }, el('span', { class: 'gw-label' }, 'Gerätetyp'), gwName),
    el('span', { class: 'gw-aendern' }, 'Ändern', el('span', { class: 'chevron' }, icon('weiter', 14))));
  const gwRaster = el('div', { id: 'h-geraete', class: 'gw-raster versteckt', role: 'group', 'aria-label': 'Gerätetyp wählen' },
    GERAETE_GRUPPEN.map((g) => el('div', { class: 'gw-gruppe' },
      el('div', { class: 'gw-gruppe-titel' }, g.de),
      el('div', { class: 'gw-kacheln' }, GERAETE.filter((x) => x.gruppe === g.id).map((x) => el('button', {
        type: 'button', class: 'gw-kachel', 'data-id': x.id, 'aria-pressed': 'false', title: x.de,
        onclick: () => { geraet = x.id; geraetZeigen(); rasterSetzen(false); if (!App.istTelefon()) gwKnopf.focus({ preventScroll: true }); },
      }, geraetIcon(x.id, 22), el('span', null, x.kurz || x.de)))))));
  const geraetZeigen = () => {
    ersetzen(gwBild, geraetIcon(geraet, 24));
    gwName.textContent = geraetFuer(geraet).de;
    for (const k of gwRaster.querySelectorAll('.gw-kachel')) k.setAttribute('aria-pressed', k.dataset.id === geraet ? 'true' : 'false');
  };
  const rasterSetzen = (auf) => {
    gwKnopf.setAttribute('aria-expanded', auf ? 'true' : 'false');
    gwRaster.classList.toggle('versteckt', !auf);
  };
  geraetZeigen();

  // --- Einfache Felder ---
  // Name gleich Adresse (Vorgabe): Feld leer lassen, dann folgt der Name einer neuen Adresse.
  f.name = text('h-name', h.name && h.name !== h.adresse ? h.name : '', { placeholder: 'leer = wie die Adresse' });
  const namePlatzhalter = () => { f.name.placeholder = f.adresse.value.trim() || 'leer = wie die Adresse'; };
  f.adresse = text('h-adresse', h.adresse, { placeholder: '192.168.1.10 oder server.lan', inputmode: 'url', required: true,
    oninput: () => { namePlatzhalter(); f.adresse.removeAttribute('aria-invalid'); },
    onchange: () => adresseUebernehmen() });
  const adrHinweis = el('div', { class: 'hinweis' }, 'Auch «benutzer@host:port» geht.');
  f.benutzer = text('h-benutzer', h.benutzer || (eigen && eigen.benutzer) || '', { placeholder: 'z.B. root' });
  f.ordner = el('select', { id: 'h-ordner', onchange: async () => {
    if (f.ordner.value === 'neu') { const o = await ordnerAnlegen(); ordnerOptionen(o ? o.id : 0); }
  } });
  const ordnerOptionen = (wahl) => ersetzen(f.ordner, el('option', { value: 0 }, 'Ohne Ordner'),
    App.daten.ordner.map((o) => el('option', { value: o.id, selected: o.id === wahl }, o.name)), el('option', { value: 'neu' }, 'Neuer Ordner …'));
  ordnerOptionen(h.ordnerId || 0);

  // Passwort: direkt eintragen; ohne Haken fragt die App beim Verbinden.
  f.passwort = el('input', { id: 'h-passwort', type: 'password', autocomplete: 'new-password', autocapitalize: 'off', spellcheck: 'false' });
  const pwZeigen = el('button', { type: 'button', class: 'pw-zeigen', 'aria-label': 'Passwort anzeigen', 'aria-pressed': 'false', onclick: () => {
    const sichtbar = f.passwort.type === 'password';
    f.passwort.type = sichtbar ? 'text' : 'password';
    pwZeigen.setAttribute('aria-pressed', sichtbar ? 'true' : 'false');
    pwZeigen.setAttribute('aria-label', sichtbar ? 'Passwort verbergen' : 'Passwort anzeigen');
    ersetzen(pwZeigen, strichIcon(sichtbar ? AUGE_ZU : AUGE, 18));
  } }, strichIcon(AUGE, 18));
  f.merken = el('input', { type: 'checkbox', id: 'h-merken', checked: true, onchange: () => passwortZeigen() });
  const pwHinweis = el('div', { class: 'hinweis' });
  const passwortZeigen = () => {
    const an = f.merken.checked;
    f.passwort.disabled = !an;
    pwZeigen.disabled = !an;
    if (!an) f.passwort.value = '';
    f.passwort.placeholder = !an ? 'wird beim Verbinden abgefragt' : eigen && eigen.hatPasswort ? 'gespeichert — leer = unverändert' : 'leer = beim Verbinden fragen';
    pwHinweis.textContent = an ? '' : eigen ? 'Das gespeicherte Passwort wird gelöscht. Die App fragt bei jedem Verbinden.' : 'Die App fragt bei jedem Verbinden nach dem Passwort.';
    pwHinweis.classList.toggle('versteckt', an);
  };
  const pwBereich = el('div', { class: 'feld' },
    el('label', { for: 'h-passwort' }, 'Passwort'),
    el('div', { class: 'pw-feld' }, f.passwort, pwZeigen),
    el('label', { class: 'check pw-merken' }, f.merken, el('span', null, 'Passwort speichern', el('span', { class: 'leise' }, ' · verschlüsselt im Tresor'))),
    pwHinweis);
  passwortZeigen();

  // Anmeldung ueber einen Zugang aus dem Tresor (statt Passwort): Hinweis anstelle des Passwortfelds.
  const zugangInfo = el('div', { class: 'feld' });
  const anmeldungZeigen = () => {
    const z = f.anmeldung.value === 'eigen' ? null : zugangVon(+f.anmeldung.value);
    pwBereich.classList.toggle('versteckt', !!z);
    zugangInfo.classList.toggle('versteckt', !z);
    f.benutzer.placeholder = z && z.benutzer ? 'aus dem Zugang: ' + z.benutzer : 'z.B. root';
    if (z) {
      ersetzen(zugangInfo,
        el('span', { class: 'label' }, 'Anmeldung'),
        el('div', { class: 'zugang-info' },
          el('span', { class: 'geraet-feld ' + (z.art === 'schluessel' ? 'art-schluessel' : 'art-passwort') }, icon(z.art === 'schluessel' ? 'schluessel' : 'schloss', 16)),
          el('span', { class: 'haupt' }, el('b', null, z.name), el('small', null, z.art === 'schluessel' ? 'Schlüssel aus dem Tresor' : 'Passwort aus dem Tresor')),
          el('button', { class: 'btn klein', type: 'button', onclick: () => { erweitertSetzen(true); f.anmeldung.focus(); } }, 'Ändern')));
    }
  };

  // --- Erweitert ---
  f.port = el('input', { id: 'h-port', type: 'number', inputmode: 'numeric', min: 1, max: 65535, value: h.port || 22 });
  f.anmeldung = el('select', { id: 'h-anmeldung', onchange: () => anmeldungZeigen() },
    el('option', { value: 'eigen' }, 'Passwort (oben)'),
    andereZugaenge.length ? el('optgroup', { label: 'Aus dem Tresor' },
      andereZugaenge.map((z) => el('option', { value: z.id, selected: !!tresorZugang && z.id === tresorZugang.id }, zugangText(z)))) : null);
  f.jump = el('select', { id: 'h-jump', disabled: !jumpHosts.length },
    el('option', { value: 0 }, 'Direkt verbinden'),
    jumpHosts.map((x) => el('option', { value: x.id, selected: x.id === h.jumpId }, x.name + ' (' + x.adresse + ')')));
  f.tags = text('h-tags', (h.tags || []).join(', '), { placeholder: 'prod, kritisch' });
  f.folgen = el('input', { type: 'checkbox', checked: h.pfadFolgen !== false });
  f.notiz = el('textarea', { id: 'h-notiz', class: 'notiz' }, h.notiz || '');
  let farbe = h.farbe || '';
  const farbKnoepfe = el('div', { class: 'farbwahl', role: 'group', 'aria-labelledby': 'h-farbe-label' });
  const farbenZeichnen = () => ersetzen(farbKnoepfe, [{ wert: '', name: 'Keine Farbe' }, ...FARBEN].map((c) => {
    const n = c.wert ? 'Farbe ' + c.name : c.name;
    return el('button', { type: 'button', class: 'farbknopf', 'aria-label': n, title: n, 'aria-pressed': c.wert === farbe ? 'true' : 'false',
      onclick: () => { farbe = c.wert; farbenZeichnen(); zusammenfassen(); } },
    el('span', { class: 'farbpunkt' + (c.wert ? '' : ' ohne'), style: { background: c.wert || 'transparent' } }));
  }));
  farbenZeichnen();

  const tagListe = () => f.tags.value.split(',').map((t) => t.trim()).filter(Boolean);
  const erwText = el('small');
  const zusammenfassen = () => {
    const t = [];
    const port = parseInt(f.port.value, 10);
    if (port && port !== 22) t.push('Port ' + port);
    const z = f.anmeldung.value === 'eigen' ? null : zugangVon(+f.anmeldung.value);
    if (z) t.push((z.art === 'schluessel' ? 'Schlüssel' : 'Zugang') + ' «' + z.name + '»');
    const j = +f.jump.value && d.hosts.find((x) => x.id === +f.jump.value);
    if (j) t.push('über ' + j.name);
    const tags = tagListe();
    if (tags.length) t.push(tags.join(', '));
    if (farbe) t.push(farbName(farbe));
    if (f.notiz.value.trim()) t.push('Notiz');
    if (!f.folgen.checked) t.push('Dateileiste folgt nicht');
    erwText.textContent = t.length ? t.join(' · ') : 'Port, Schlüssel, Jump-Host, Tags, Farbe, Notiz';
  };
  const erwBereich = el('div', { id: 'h-erweitert', class: 'erweitert versteckt' },
    el('div', { class: 'raster2' },
      feld('h-port', 'Port', f.port),
      feld('h-anmeldung', 'Anmelden mit', f.anmeldung, andereZugaenge.length
        ? 'Schlüssel und gemeinsame Zugänge verwaltest du im Reiter «Tresor».'
        : 'Schlüssel erzeugst oder fügst du im Reiter «Tresor» ein, dann stehen sie hier zur Wahl.'),
      feld('h-jump', 'Über Jump-Host', f.jump, jumpHosts.length
        ? 'Nur nötig, wenn der Host bloss über einen anderen erreichbar ist.'
        : 'Braucht mindestens einen weiteren Host.'),
      feld('h-tags', 'Tags', f.tags, 'Mit «kritisch» fragt die App vor gefährlichen Befehlen nach.')),
    el('div', { class: 'feld' }, el('span', { class: 'label', id: 'h-farbe-label' }, 'Farbe'), farbKnoepfe),
    el('label', { class: 'check' }, f.folgen, el('span', null, el('b', null, 'Dateileiste folgt dem Terminal'), el('br'),
      el('span', { class: 'check-hinweis' }, 'Wechselt mit «cd» mit. Bei Geräten ohne normale Shell (Router, Windows) nur, wenn die Shell ihren Ordner meldet.'))),
    feld('h-notiz', 'Notiz', f.notiz));
  const erwKnopf = el('button', { type: 'button', class: 'erweitert-knopf', 'aria-expanded': 'false', 'aria-controls': 'h-erweitert',
    onclick: () => erweitertSetzen(erwKnopf.getAttribute('aria-expanded') !== 'true') },
  el('span', { class: 'chevron' }, icon('weiter', 16)),
  el('span', { class: 'erw-titel' }, el('b', null, 'Erweitert'), erwText));
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

  // «benutzer@host:port» aus dem Adressfeld auf Benutzer und Port verteilen.
  const adresseUebernehmen = () => {
    const roh = f.adresse.value.trim();
    if (!/[@\s[]|:\d+$|^ssh:/i.test(roh)) return;
    const r = adresseZerlegen(roh);
    const neuWerte = [];
    f.adresse.value = r.adresse;
    if (r.benutzer && r.benutzer !== f.benutzer.value.trim()) { f.benutzer.value = r.benutzer; neuWerte.push('Benutzer «' + r.benutzer + '»'); }
    if (r.port && String(r.port) !== String(f.port.value)) { f.port.value = r.port; neuWerte.push('Port ' + r.port); }
    adrHinweis.textContent = neuWerte.length ? neuWerte.join(' und ') + ' übernommen.' : 'Auch «benutzer@host:port» geht.';
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
      fehlerZeigen('Bitte die Adresse eintragen, z.B. 192.168.1.10 oder root@server.lan:2222.');
      f.adresse.focus();
      return null;
    }
    if (/[\s/@]/.test(adresse)) {
      f.adresse.setAttribute('aria-invalid', 'true');
      fehlerZeigen('Die Adresse darf keine Leerzeichen und kein «/» enthalten.');
      f.adresse.focus();
      return null;
    }
    const port = f.port.value.trim() === '' ? 22 : Number(f.port.value);
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      erweitertSetzen(true);
      fehlerZeigen('Der Port muss eine Zahl zwischen 1 und 65535 sein.');
      return null;
    }
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
        if (!e.abgemeldet) fehlerZeigen('Das Passwort konnte nicht gespeichert werden: ' + e.message + ' Der Host ist noch nicht gespeichert.');
        return null;
      }
      // 2. Host
      const daten = {
        name, adresse, port, benutzer, zugangId, jumpId: +f.jump.value || 0, ordnerId: +f.ordner.value || 0, geraet,
        tags: tagListe(), farbe, pfadFolgen: f.folgen.checked, notiz: f.notiz.value,
      };
      let r;
      try {
        r = neu ? await api('POST', '/api/hosts', daten) : await api('PUT', '/api/hosts/' + h.id, daten);
      } catch (e) {
        if (e.abgemeldet) return null;
        let t = 'Der Host konnte nicht gespeichert werden: ' + e.message;
        if (neuerZugang) {
          try { await api('DELETE', '/api/zugaenge/' + neuerZugang.id); }
          catch (e2) { t += ' Das Passwort liegt bereits als Zugang «' + name + '» im Tresor.'; }
        } else if (pwGeaendert) t += ' Das neue Passwort ist schon gespeichert.';
        fehlerZeigen(t);
        return null;
      }
      // 3. Eigenen Zugang aufraeumen, wenn der Host ihn nicht mehr nutzt.
      let rest = null;
      if (eigenWeg) {
        try { await api('DELETE', '/api/zugaenge/' + eigen.id); }
        catch (e) { rest = 'Das bisherige Passwort liegt noch im Tresor (Zugang «' + eigen.name + '»).'; }
      }
      dlg.zu();
      await App.datenLaden();
      toast((neu ? 'Host angelegt' : 'Gespeichert') + (neuerZugang || pwGeaendert ? ' — Passwort im Tresor' : ''));
      if (rest) toast(rest, 'fehler');
      return r;
    } finally {
      laeuft = false;
      knoepfeSperren(false);
    }
  };

  const loeschen = async () => {
    const zusatz = eigen ? ' Sein gespeichertes Passwort wird ebenfalls gelöscht.' : '';
    if (!(await frage(`Host «${h.name}» löschen?${zusatz}`, { ja: 'Löschen', gefahr: true }))) return;
    try {
      await api('DELETE', '/api/hosts/' + h.id);
      if (eigen) await api('DELETE', '/api/zugaenge/' + eigen.id).catch(() => {});
      dlg.zu(); App.datenLaden(); toast('Host gelöscht');
    } catch (e) { if (!e.abgemeldet) fehlerZeigen('Löschen nicht möglich: ' + e.message); }
  };
  // Telefon: Loeschen steht am Ende von «Erweitert», der Fuss bleibt einzeilig (ohne «Abbrechen»:
  // dafuer gibt es das Kreuz oben und die Zurueck-Geste).
  const kLoeschen = !neu ? el('button', { class: 'btn gefahr links nur-desktop', type: 'button', onclick: loeschen }, icon('muell'), 'Löschen') : null;
  if (!neu) erwBereich.append(el('div', { class: 'nur-telefon loeschen-telefon' }, el('button', { class: 'btn gefahr voll', type: 'button', onclick: loeschen }, icon('muell'), 'Host löschen')));
  const kAbbrechen = el('button', { class: 'btn nur-desktop', type: 'button', onclick: () => dlg.zu() }, 'Abbrechen');
  const kVerbinden = el('button', { class: 'btn', type: 'button', onclick: async () => { const r = await speichern(); if (r) App.verbinden(r); } }, 'Speichern und verbinden');
  const kSpeichern = el('button', { class: 'btn primaer', type: 'button', onclick: () => speichern() }, 'Speichern');
  const knoepfeSperren = (an) => { for (const k of [kLoeschen, kVerbinden, kSpeichern]) if (k) k.disabled = an; };

  const form = el('form', { class: 'host-form', novalidate: true, onsubmit: (e) => { e.preventDefault(); speichern(); },
    oninput: () => zusammenfassen(), onchange: () => zusammenfassen() },
  meldung,
  el('div', { class: 'gw' }, gwKnopf, gwRaster),
  el('div', { class: 'raster2' },
    feld('h-name', 'Name (optional)', f.name),
    el('div', { class: 'feld' }, el('label', { for: 'h-adresse' }, 'Adresse'), f.adresse, adrHinweis),
    feld('h-benutzer', 'Benutzer', f.benutzer),
    pwBereich, zugangInfo,
    feld('h-ordner', 'Ordner', f.ordner)),
  erwTeil,
  el('button', { type: 'submit', class: 'versteckt' }));

  anmeldungZeigen();
  namePlatzhalter();
  zusammenfassen();
  dlg = dialog({
    titel: neu ? 'Host anlegen' : 'Host bearbeiten', breit: true,
    inhalt: form,
    knoepfe: [kLoeschen, kAbbrechen, kVerbinden, kSpeichern],
  });
  dlg.element.classList.add('host-dialog');
  if (neu && !App.istTelefon()) f.adresse.focus();
};

App.Seitenleiste = Seitenleiste;
