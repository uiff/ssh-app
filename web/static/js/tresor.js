'use strict';
// Linke Seitenleiste: Hosts (nach Ordnern) und Tresor (Zugaenge, Snippets).

const FARBEN = ['#5DB8A6', '#E9A23B', '#E06C5F', '#6FA8DC', '#C792EA', '#5C6370'];

class Seitenleiste {
  constructor() {
    this.reiter = lokal('reiter', 'hosts');
    this.filter = '';
    this.zu = new Set(lokal('ordnerZu', []));
    this.inhalt = el('div', { class: 'seite-inhalt' });
    this.reiterLeiste = el('div', { class: 'reiter', role: 'tablist' });
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

  hostsZeichnen() {
    const d = App.daten;
    const filterFeld = el('input', { type: 'search', placeholder: 'Name, IP oder Tag', value: this.filter, 'aria-label': 'Hosts filtern', autocapitalize: 'off', spellcheck: 'false',
      oninput: () => { this.filter = filterFeld.value; this.listeZeichnen(liste); } });
    const liste = el('div');
    ersetzen(this.inhalt, 
      el('div', { class: 'filter reihe', style: { flexWrap: 'nowrap', gap: '8px' } }, filterFeld,
        ikonKnopf('plus', 'Host anlegen', () => App.hostBearbeiten({})),
        ikonKnopf('ordnerPlus', 'Ordner anlegen', () => ordnerAnlegen())),
      liste);
    this.listeZeichnen(liste);
    if (!d.hosts.length) ersetzen(liste, el('div', { class: 'leer' }, 'Noch keine Hosts.', el('br'), el('button', { class: 'btn primaer', type: 'button', style: { marginTop: '14px' }, onclick: () => App.hostBearbeiten({}) }, icon('plus'), 'Ersten Host anlegen')));
  }

  listeZeichnen(liste) {
    const d = App.daten;
    const q = this.filter.trim().toLowerCase();
    const passt = (h) => !q || (h.name + ' ' + h.adresse + ' ' + (h.tags || []).join(' ')).toLowerCase().includes(q);
    const offen = new Set(App.offeneTabs().filter((t) => t.zustand === 'bereit').map((t) => t.hostId));
    const gruppen = d.ordner.map((o) => ({ o, hosts: d.hosts.filter((h) => h.ordnerId === o.id && passt(h)) }));
    const ohne = d.hosts.filter((h) => !d.ordner.some((o) => o.id === h.ordnerId) && passt(h));
    if (ohne.length) gruppen.push({ o: { id: 0, name: d.ordner.length ? 'Ohne Ordner' : 'Hosts' }, hosts: ohne });
    const kinder = [];
    for (const g of gruppen) {
      if (q && !g.hosts.length) continue;
      const istZu = this.zu.has(g.o.id) && !q;
      kinder.push(el('div', { class: 'zeile-wrap' },
        el('button', { class: 'gruppe-titel', type: 'button', style: { flex: 1, minHeight: '40px' }, 'aria-expanded': istZu ? 'false' : 'true',
          onclick: () => { istZu ? this.zu.delete(g.o.id) : this.zu.add(g.o.id); lokalSetzen('ordnerZu', [...this.zu]); this.listeZeichnen(liste); } },
          icon(istZu ? 'weiter' : 'pfeilRunter', 12), g.o.name, el('span', { class: 'anz' }, String(g.hosts.length))),
        g.o.id ? ikonKnopf('mehr', 'Ordner bearbeiten', (e) => ordnerMenue(g.o, e.currentTarget)) : null));
      if (istZu) continue;
      for (const h of g.hosts) {
        const farbe = offen.has(h.id) ? (h.farbe || '#5DB8A6') : (h.farbe || '#5C6370');
        kinder.push(el('div', { class: 'zeile-wrap' },
          el('button', { class: 'zeile', type: 'button', onclick: () => { App.verbinden(h); App.seiteUmschalten('links', false, true); } },
            el('span', { class: 'punkt', style: { background: farbe, opacity: offen.has(h.id) ? 1 : 0.55 } }),
            el('span', { class: 'haupt' }, el('b', null, h.name), el('small', null, hostZeile(h)))),
          ikonKnopf('stift', 'Host bearbeiten', () => App.hostBearbeiten(h))));
      }
    }
    if (d.hosts.length) ersetzen(liste, ...(kinder.length ? kinder : [el('div', { class: 'leer' }, 'Kein Host passt zum Filter.')]));
  }

  tresorZeichnen() {
    const d = App.daten;
    const zeilen = d.zugaenge.map((z) => el('div', { class: 'zeile-wrap' },
      el('button', { class: 'zeile', type: 'button', onclick: () => App.zugangBearbeiten(z) },
        el('span', { style: { color: z.art === 'schluessel' ? 'var(--akzent)' : 'var(--gut)', display: 'inline-flex' } }, icon(z.art === 'schluessel' ? 'schluessel' : 'passwort')),
        el('span', { class: 'haupt' }, el('b', null, z.name),
          el('small', null, (z.benutzer || '—') + ' · ' + (z.art === 'schluessel' ? (z.schluesselTyp || 'Schlüssel').replace('ssh-', '') + (z.hatPassphrase ? ' + Passphrase' : '') : 'Passwort') + ' · ' + z.hosts + (z.hosts === 1 ? ' Host' : ' Hosts'))))));
    ersetzen(this.inhalt, 
      el('div', { class: 'filter', style: { display: 'flex', flexDirection: 'column', gap: '8px' } },
        el('button', { class: 'btn primaer voll', type: 'button', onclick: () => App.zugangBearbeiten({}) }, icon('plus'), 'Zugang anlegen'),
        el('button', { class: 'btn voll', type: 'button', onclick: () => App.schluesselErzeugen() }, icon('schluessel'), 'Neuen Schlüssel erzeugen')),
      el('div', { class: 'gruppe-titel' }, 'Zugänge', el('span', { class: 'anz' }, String(d.zugaenge.length))),
      zeilen.length ? zeilen : el('div', { class: 'leer' }, 'Ein Zugang ist Benutzer plus Passwort oder Schlüssel. Hosts verweisen darauf — so aenderst du ein Passwort nur an einer Stelle.'),
      el('div', { class: 'meldung gut', style: { margin: '14px' } }, icon('schloss'),
        el('span', null, 'Passwörter und Schlüssel liegen verschlüsselt. Nach ', String(App.status.leerlaufMin || 30), ' Minuten ohne Bedienung sperrt sich der Tresor.')));
  }

  snippetsZeichnen() {
    const d = App.daten;
    const zeilen = d.snippets.map((s) => el('div', { class: 'zeile-wrap' },
      el('button', { class: 'zeile', type: 'button', onclick: () => App.befehlVorbereiten(s.befehl, s.name) },
        el('span', { class: 'haupt' }, el('b', null, s.name), el('small', null, s.befehl))),
      ikonKnopf('stift', 'Snippet bearbeiten', () => App.snippetBearbeiten(s))));
    ersetzen(this.inhalt, 
      el('div', { class: 'filter' }, el('button', { class: 'btn primaer voll', type: 'button', onclick: () => App.snippetBearbeiten({}) }, icon('plus'), 'Snippet anlegen')),
      zeilen.length ? zeilen : el('div', { class: 'leer' }, 'Snippets sind gespeicherte Befehle. Platzhalter schreibst du so: ', el('code', null, 'journalctl -u {{dienst}}')));
  }
}

function hostZeile(h) {
  const z = App.daten.zugaenge.find((x) => x.id === h.zugangId);
  const benutzer = h.benutzer || (z && z.benutzer);
  let s = (benutzer ? benutzer + '@' : '') + h.adresse + (h.port && h.port !== 22 ? ':' + h.port : '');
  const j = h.jumpId && App.daten.hosts.find((x) => x.id === h.jumpId);
  if (j) s += ' · via ' + j.name;
  return s;
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

// ---------- Host-Editor ----------

App.hostBearbeiten = function (h) {
  const d = App.daten;
  const neu = !h.id;
  const f = {};
  const feld = (id, label, input, hinweis) => el('div', { class: 'feld' }, el('label', { for: id }, label), input, hinweis ? el('div', { class: 'hinweis' }, hinweis) : null);
  const text = (id, wert, extra) => el('input', Object.assign({ id, type: 'text', value: wert || '', autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false' }, extra || {}));
  f.name = text('h-name', h.name, { placeholder: 'z.B. web-01' });
  f.adresse = text('h-adresse', h.adresse, { placeholder: '192.168.1.10 oder server.lan', inputmode: 'url' });
  f.port = el('input', { id: 'h-port', type: 'number', inputmode: 'numeric', min: 1, max: 65535, value: h.port || 22 });
  f.benutzer = text('h-benutzer', h.benutzer, { placeholder: 'aus dem Zugang' });
  f.zugang = el('select', { id: 'h-zugang' },
    el('option', { value: 0 }, 'Beim Verbinden nach Passwort fragen'),
    d.zugaenge.map((z) => el('option', { value: z.id, selected: z.id === h.zugangId }, z.name + (z.benutzer ? ' (' + z.benutzer + ')' : '') + ' · ' + (z.art === 'schluessel' ? 'Schlüssel' : 'Passwort'))));
  f.jump = el('select', { id: 'h-jump' }, el('option', { value: 0 }, 'Direkt verbinden'),
    d.hosts.filter((x) => x.id !== h.id).map((x) => el('option', { value: x.id, selected: x.id === h.jumpId }, x.name + ' (' + x.adresse + ')')));
  f.ordner = el('select', { id: 'h-ordner', onchange: async () => {
    if (f.ordner.value === 'neu') { const o = await ordnerAnlegen(); ordnerOptionen(o ? o.id : 0); }
  } });
  const ordnerOptionen = (wahl) => ersetzen(f.ordner, el('option', { value: 0 }, 'Ohne Ordner'),
    App.daten.ordner.map((o) => el('option', { value: o.id, selected: o.id === wahl }, o.name)), el('option', { value: 'neu' }, 'Neuer Ordner …'));
  ordnerOptionen(h.ordnerId || 0);
  f.tags = text('h-tags', (h.tags || []).join(', '), { placeholder: 'prod, kritisch' });
  f.folgen = el('input', { type: 'checkbox', checked: h.pfadFolgen !== false });
  f.notiz = el('textarea', { id: 'h-notiz', style: { minHeight: '70px', fontFamily: 'var(--sans)', fontSize: '15px' } }, h.notiz || '');
  let farbe = h.farbe || '';
  const farbKnoepfe = el('div', { class: 'farben', role: 'group', 'aria-label': 'Farbe' });
  const farbenZeichnen = () => ersetzen(farbKnoepfe, ...['', ...FARBEN].map((c) => el('button', { type: 'button', 'aria-label': c ? 'Farbe ' + c : 'Keine Farbe', 'aria-pressed': c === farbe ? 'true' : 'false',
    style: { background: c || 'transparent', borderColor: c === farbe ? 'var(--text)' : (c ? 'transparent' : 'var(--linie2)') }, onclick: () => { farbe = c; farbenZeichnen(); } })));
  farbenZeichnen();

  const speichern = async () => {
    const daten = {
      name: f.name.value.trim(), adresse: f.adresse.value.trim(), port: parseInt(f.port.value, 10) || 22,
      benutzer: f.benutzer.value.trim(), zugangId: +f.zugang.value, jumpId: +f.jump.value, ordnerId: +f.ordner.value || 0,
      tags: f.tags.value.split(',').map((t) => t.trim()).filter(Boolean), farbe, pfadFolgen: f.folgen.checked, notiz: f.notiz.value,
    };
    try {
      const r = neu ? await api('POST', '/api/hosts', daten) : await api('PUT', '/api/hosts/' + h.id, daten);
      dlg.zu(); await App.datenLaden(); toast('Gespeichert');
      return r;
    } catch (e) { fehlerToast(e); return null; }
  };
  const dlg = dialog({
    titel: neu ? 'Host anlegen' : 'Host bearbeiten', breit: true,
    inhalt: el('form', { onsubmit: (e) => { e.preventDefault(); speichern(); }, style: { display: 'flex', flexDirection: 'column', gap: '16px' } },
      el('div', { class: 'raster2' },
        feld('h-name', 'Anzeigename', f.name),
        el('div', { style: { display: 'grid', gridTemplateColumns: '3fr 1fr', gap: '10px' } }, feld('h-adresse', 'Adresse', f.adresse), feld('h-port', 'Port', f.port)),
        feld('h-zugang', 'Zugang', f.zugang, d.zugaenge.length ? null : el('span', null, 'Noch keine Zugänge — im Reiter «Tresor» anlegen.')),
        feld('h-benutzer', 'Benutzer (optional)', f.benutzer, 'Leer = Benutzer aus dem Zugang'),
        feld('h-jump', 'Über Jump-Host', f.jump),
        feld('h-ordner', 'Ordner', f.ordner),
        feld('h-tags', 'Tags', f.tags, 'Mit «kritisch» fragt die App vor gefährlichen Befehlen nach.'),
        el('div', { class: 'feld' }, el('span', { class: 'label' }, 'Farbe'), farbKnoepfe)),
      el('label', { class: 'check' }, f.folgen, el('span', null, el('b', null, 'Linux-Shell: Dateileiste folgt dem Terminal'), el('br'),
        el('span', { style: { color: 'var(--leise)', fontSize: '13px' } }, 'Für Router oder Geräte ohne normale Shell ausschalten.'))),
      feld('h-notiz', 'Notiz', f.notiz),
      el('button', { type: 'submit', class: 'versteckt' })),
    knoepfe: [
      !neu ? el('button', { class: 'btn gefahr links', type: 'button', onclick: async () => {
        if (!(await frage(`Host «${h.name}» löschen?`, { ja: 'Löschen', gefahr: true }))) return;
        try { await api('DELETE', '/api/hosts/' + h.id); dlg.zu(); App.datenLaden(); } catch (e) { fehlerToast(e); }
      } }, icon('muell'), 'Löschen') : null,
      el('button', { class: 'btn', type: 'button', onclick: () => dlg.zu() }, 'Abbrechen'),
      el('button', { class: 'btn', type: 'button', onclick: async () => { const r = await speichern(); if (r) App.verbinden(r); } }, 'Speichern und verbinden'),
      el('button', { class: 'btn primaer', type: 'button', onclick: speichern }, 'Speichern'),
    ],
  });
};

App.Seitenleiste = Seitenleiste;
