'use strict';
// Befehlshilfe: Seitenleiste zur aktuellen Eingabe und Palette (Strg+Umschalt+K).
// Texte: js/sprache/hilfe.js. Der Server liefert die tldr-Seite in der Sprache der
// Oberflaeche (Kopf X-SSH-App-Sprache aus api()), sonst in der anderen.

// Namensnennung fuer tldr-pages (CC BY 4.0): Urheber, Quelle und Lizenz verlinkt.
// Stammt die Seite aus der anderen Sprache (Rueckfall), steht das davor.
// klein: nach einem Doppelpunkt (Einstellungen «Befehlshilfe: aus …») klein beginnen.
function tldrQuelle(seitenSprache, klein) {
  const fremd = seitenSprache && seitenSprache !== I18N.sprache;
  const quelle = tTeile('hilfe.quelle', {
    tldr: el('a', { href: 'https://github.com/tldr-pages/tldr', target: '_blank', rel: 'noopener noreferrer' }, 'tldr-pages'),
    lizenz: el('a', { href: 'https://creativecommons.org/licenses/by/4.0/', target: '_blank', rel: 'noopener noreferrer' }, 'CC BY 4.0'),
  });
  if (klein && !fremd && typeof quelle[0] === 'string') quelle[0] = quelle[0].charAt(0).toLocaleLowerCase(lokalCode()) + quelle[0].slice(1);
  return [fremd ? t('hilfe.quelle.sprache', { sprache: t('hilfe.sprache.' + seitenSprache) }) + ' · ' : '', quelle];
}

// tldr-Merkhilfen: «E[x]tract a [f]ile» zeigt, welcher Buchstabe zu welchem Schalter gehoert.
// Die Klammern verschwinden, der Buchstabe wird unterstrichen. Nur innerhalb eines Wortes —
// «[Interactive]» oder «[C]» allein bleiben, wie sie sind.
const MERK_RE = /\[([A-Za-z0-9]{1,4})\]/g;
const MERK_STIL = { textDecoration: 'underline', textDecorationThickness: '1.5px', textUnderlineOffset: '2px' };
function merkhilfeHier(s, i, laenge) {
  return /[A-Za-z]/.test(s.charAt(i - 1)) || /[A-Za-z]/.test(s.charAt(i + laenge));
}
function merkhilfen(s) {
  const out = [];
  let pos = 0;
  let m;
  MERK_RE.lastIndex = 0;
  while ((m = MERK_RE.exec(s))) {
    if (!merkhilfeHier(s, m.index, m[0].length)) continue;
    out.push(s.slice(pos, m.index), el('span', { class: 'merk', style: MERK_STIL }, m[1]));
    pos = m.index + m[0].length;
  }
  if (!out.length) return s;
  out.push(s.slice(pos));
  return out.filter((x) => x !== '');
}
function ohneMerkhilfen(s) {
  return String(s || '').replace(MERK_RE, (m, b, i, ganz) => (merkhilfeHier(ganz, i, m.length) ? b : m));
}
// Text einer tldr-Seite: `Code` und Merkhilfen hervorgehoben.
function hilfeText(text) {
  const teile = mitCode(text);
  return Array.isArray(teile) ? teile.map((x) => (typeof x === 'string' ? merkhilfen(x) : x)) : merkhilfen(teile);
}

const KOPF_STIL = { fontSize: '11px', fontWeight: 600, letterSpacing: '.06em', color: 'var(--leise)', textTransform: 'uppercase' };

class HilfeLeiste {
  constructor() {
    this.titelCode = el('code', { style: { color: 'var(--akzent)', fontWeight: 400 } });
    this.inhalt = el('div', { class: 'hilfe-inhalt' });
    this.element = el('aside', { class: 'seite rechts', 'aria-label': t('hilfe.leiste.titel') },
      el('div', { class: 'blatt-griff', 'aria-hidden': 'true' }),
      el('div', { class: 'seite-kopf' }, el('span', { class: 'titel' }, t('hilfe.leiste.titel') + ' ', this.titelCode),
        ikonKnopf('suche', t('hilfe.leiste.suchen'), () => App.palette()),
        ikonKnopf('x', t('hilfe.leiste.schliessen'), () => App.seiteUmschalten('hilfe', false))),
      el('div', { class: 'seite-inhalt' }, this.inhalt));
    this.letzte = null;
    this.leer();
  }

  leer() {
    this.letzte = '';
    this.titelCode.textContent = '';
    const anzahl = App.status && App.status.hilfeSeiten;
    ersetzen(this.inhalt,
      el('p', { style: { margin: 0, color: 'var(--text2)', lineHeight: 1.55, fontSize: '14px' } }, t('hilfe.leer.text')),
      el('button', { class: 'btn voll', type: 'button', onclick: () => App.palette() }, icon('suche'), t('hilfe.leer.suchen')),
      el('p', { class: 'quelle' }, anzahl
        ? [t('hilfe.leer.anzahl', { anzahl: Number(anzahl).toLocaleString(lokalCode()) }), ' ', tldrQuelle()]
        : t('hilfe.leer.fehlt')));
  }

  zeile(text) {
    if (!App.seiteOffen('hilfe')) return;
    clearTimeout(this.timer);
    this.timer = setTimeout(() => this.zeigen(text), 450);
  }

  async zeigen(text) {
    const befehl = (text || '').trim();
    if (befehl === this.letzte) return;
    this.letzte = befehl;
    if (!befehl) { this.leer(); return; }
    let r;
    try { r = await api('GET', '/api/hilfe/erklaeren?zeile=' + encodeURIComponent(befehl)); } catch (e) { return; }
    if (befehl !== this.letzte) return;
    if (!r.gefunden) {
      this.titelCode.textContent = befehl.split(/\s+/)[0];
      ersetzen(this.inhalt, el('p', { style: { margin: 0, color: 'var(--text2)' } }, t('hilfe.keineSeite')),
        el('button', { class: 'btn voll', type: 'button', onclick: () => App.palette(befehl.split(/\s+/)[0]) }, icon('suche'), t('hilfe.suchen')));
      return;
    }
    this.titelCode.textContent = r.seite.name;
    const teile = r.teile.slice(1);
    ersetzen(this.inhalt,
      el('div', { lang: r.seite.sprache, style: { fontSize: '14px', color: 'var(--text2)', lineHeight: 1.5 } }, hilfeText(r.seite.beschreibung)),
      teile.length ? el('div', { class: 'erklaerung' },
        el('div', { class: 'kopfzeile' }, t('hilfe.erklaert')),
        teile.map((x) => el('div', { class: 'teil' }, el('code', null, x.wort), el('span', null, hilfeText(x.erklaerung))))) : null,
      el('div', { class: 'kopfzeile', style: KOPF_STIL }, t('hilfe.beispiele')),
      el('div', { lang: r.seite.sprache, style: { display: 'flex', flexDirection: 'column', gap: '14px' } }, r.seite.beispiele.map((b) => beispielZeile(b))),
      el('p', { class: 'quelle' }, tldrQuelle(r.seite.sprache),
        r.seite.mehr ? [' · ', el('a', { href: r.seite.mehr, target: '_blank', rel: 'noopener noreferrer' }, t('hilfe.handbuch'))] : null));
  }
}

// Ohne beiWahl (Hilfe zur aktuellen Zeile) ersetzt «Nehmen» die schon getippte Eingabe.
function beispielZeile(b, beiWahl) {
  return el('div', { class: 'beispiel' },
    el('div', { class: 't' }, hilfeText(b.text)),
    el('div', { class: 'b' }, el('code', null, befehlAnzeige(b.befehl)),
      el('button', { class: 'btn klein', type: 'button', onclick: () => beiWahl ? beiWahl(b) : App.befehlVorbereiten(b.befehl, ohneMerkhilfen(b.text), { zeileErsetzen: true }) }, t('hilfe.nehmen'))));
}

// ---------- Palette ----------

class Palette {
  constructor() {
    this.offen = false;
  }

  oeffnen(start) {
    if (this.offen) { this.feld.focus(); return; }
    this.offen = true;
    this.ebene = ebeneAuf(() => this.schliessen());
    this.treffer = [];
    this.wahl = 0;
    this.feld = el('input', { type: 'search', placeholder: t('hilfe.palette.feld'), autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false', 'aria-label': t('hilfe.palette.feldLabel'),
      oninput: () => this.suchen(), onkeydown: (e) => this.taste(e) });
    this.liste = el('div', { class: 'palette-liste', role: 'listbox', 'aria-label': t('hilfe.palette.titel') });
    this.detail = el('div', { class: 'palette-detail versteckt' });
    this.schleier = el('div', { class: 'schleier', onclick: () => this.schliessen() });
    this.element = el('div', { class: 'palette', role: 'dialog', 'aria-label': t('hilfe.palette.titel') },
      el('div', { class: 'palette-suche' }, icon('suche'), this.feld, ikonKnopf('x', t('hilfe.palette.schliessen'), () => this.schliessen())),
      el('div', { class: 'palette-teile', style: { position: 'relative' } }, this.liste, this.detail),
      el('div', { class: 'palette-fuss' },
        el('span', null, el('b', null, '↑↓'), ' ', t('hilfe.fuss.waehlen')), el('span', null, el('b', null, 'Tab'), ' ', t('hilfe.fuss.platzhalter')),
        el('span', null, el('b', null, 'Enter'), ' ', t('hilfe.fuss.einfuegen')), el('span', null, el('b', null, 'Esc'), ' ', t('hilfe.fuss.schliessen')),
        el('span', { style: { marginLeft: 'auto' } }, t('hilfe.fuss.hinweis'))));
    document.body.append(this.schleier, this.element);
    this.feld.value = start || '';
    this.suchen();
    setTimeout(() => this.feld.focus(), 30);
    this.escape = (e) => { if (e.key === 'Escape' && this.offen && !dialogStapel.length) { e.preventDefault(); this.schliessen(); } };
    document.addEventListener('keydown', this.escape);
  }

  schliessen() {
    if (!this.offen) return;
    this.offen = false;
    clearTimeout(this.timer);
    this.element.remove(); this.schleier.remove();
    document.removeEventListener('keydown', this.escape);
    if (this.detailEbene) { ebeneZu(this.detailEbene); this.detailEbene = null; }
    ebeneZu(this.ebene);
    // Fokus erst danach ins Terminal: Folgt eine Rueckfrage (gefaehrlicher Befehl, an alle Tabs),
    // gehoert der Fokus ihr — sonst ginge Enter an die Shell.
    setTimeout(() => {
      if (this.offen || dialogStapel.length || App.istTelefon() || !App.aktiv) return;
      const f = document.activeElement;
      if (!f || f === document.body) App.aktiv.term.focus();
    }, 0);
  }

  suchen() {
    const q = this.feld.value.trim();
    const ql = q.toLowerCase();
    const lokal = [];
    const d = App.daten;
    for (const h of d.hosts) {
      if (!q || (h.name + ' ' + h.adresse + ' ' + (h.tags || []).join(' ')).toLowerCase().includes(ql)) lokal.push({ art: 'host', host: h });
    }
    for (const s of d.snippets) {
      if (!q || (s.name + ' ' + s.befehl + ' ' + s.beschreibung).toLowerCase().includes(ql)) lokal.push({ art: 'snippet', snippet: s });
    }
    this.treffer = q ? lokal.slice(0, 8) : lokal.filter((x) => x.art === 'snippet').concat(lokal.filter((x) => x.art === 'host')).slice(0, 12);
    this.wahl = 0;
    this.zeichnen();
    clearTimeout(this.timer);
    if (!q) return;
    this.timer = setTimeout(async () => {
      let r = [];
      try { r = await api('GET', '/api/hilfe/suche?q=' + encodeURIComponent(q)); } catch (e) { return; }
      if (this.feld.value.trim() !== q) return;
      this.treffer = lokal.slice(0, 6).concat((r || []).map((x) => ({ art: 'tldr', t: x })));
      this.wahl = 0;
      this.zeichnen();
    }, 180);
  }

  zeichnen() {
    if (!this.treffer.length) {
      ersetzen(this.liste, el('div', { class: 'leer' }, this.feld.value ? t('hilfe.palette.nichts') : t('hilfe.palette.leer')));
      this.detailZu();
      return;
    }
    const gruppen = { host: t('hilfe.gruppe.host'), snippet: t('hilfe.gruppe.snippet'), tldr: t('hilfe.gruppe.tldr') };
    let letzte = '';
    const kinder = [];
    this.treffer.forEach((x, i) => {
      if (x.art !== letzte) { kinder.push(el('div', { class: 'gruppe-titel' }, gruppen[x.art])); letzte = x.art; }
      let titel, unter, q, symbol = null, lang = null;
      if (x.art === 'host') {
        const tab = App.tabs.find((y) => y.hostId === x.host.id && y.zustand === 'bereit');
        titel = x.host.name; unter = hostZeile(x.host); q = t(tab ? 'hilfe.host.verbunden' : 'hilfe.host.verbinden');
        symbol = hostSymbol(x.host, 20, tab ? 'bereit' : '');
      } else if (x.art === 'snippet') { titel = x.snippet.name; unter = x.snippet.befehl; q = t('hilfe.snippet'); }
      else {
        titel = hilfeText(x.t.beispiel ? x.t.beispiel.text : x.t.beschreibung); unter = x.t.beispiel ? optionenAufloesen(x.t.beispiel.befehl) : x.t.name; q = x.t.name;
        lang = x.t.sprache || null;
      }
      kinder.push(el('button', { class: 'treffer' + (i === this.wahl ? ' aktiv' : ''), type: 'button', role: 'option', 'aria-selected': i === this.wahl ? 'true' : 'false',
        onclick: () => { this.wahl = i; this.zeichnen(); this.ausfuehrenOderZeigen(true); } },
        symbol, el('span', { class: 'haupt' }, el('span', { lang }, titel), el('code', null, unter)), el('span', { class: 'q' }, q)));
    });
    ersetzen(this.liste, ...kinder);
    // Desktop: Detail daneben mitzeigen — ohne den Fokus aus dem Suchfeld zu nehmen.
    if (!App.istTelefon()) this.detailZeigen(false);
    const aktiv = this.liste.querySelector('.treffer.aktiv');
    if (aktiv && aktiv.scrollIntoView) aktiv.scrollIntoView({ block: 'nearest' });
  }

  taste(e) {
    if (e.key === 'ArrowDown') { e.preventDefault(); this.wahl = Math.min(this.treffer.length - 1, this.wahl + 1); this.zeichnen(); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); this.wahl = Math.max(0, this.wahl - 1); this.zeichnen(); }
    else if (e.key === 'Enter') { e.preventDefault(); this.ausfuehrenOderZeigen(false); }
    else if (e.key === 'Tab' && !e.shiftKey && this.detail.querySelector('input')) { e.preventDefault(); this.detail.querySelector('input').focus(); }
  }

  ausfuehrenOderZeigen(geklickt) {
    const x = this.treffer[this.wahl];
    if (!x) return;
    if (x.art === 'host') { this.schliessen(); App.verbinden(x.host); return; }
    if (App.istTelefon() || geklickt) this.detailZeigen(geklickt);
    if (!geklickt && this.aktuell) this.aktuell.einfuegen();
  }

  detailZu() {
    this.detail.classList.add('versteckt');
    if (this.detailEbene) { ebeneZu(this.detailEbene); this.detailEbene = null; }
  }

  // fokus: nach ausdruecklicher Wahl (Klick) ins erste Platzhalterfeld springen.
  async detailZeigen(fokus) {
    const x = this.treffer[this.wahl];
    const nr = this.detailNr = (this.detailNr || 0) + 1; // nur die letzte Anfrage zeichnet
    this.aktuell = null;
    if (!x) return;
    const zurueck = App.istTelefon() ? el('button', { class: 'btn klein', type: 'button', style: { alignSelf: 'flex-start' }, onclick: () => this.detailZu() }, icon('zurueck'), t('hilfe.zurueck')) : null;
    this.detail.classList.remove('versteckt');
    // Telefon: Das Detail liegt ueber der Liste — die Zurueck-Geste fuehrt zur Liste zurueck.
    if (App.istTelefon() && !this.detailEbene) this.detailEbene = ebeneAuf(() => this.detailZu());
    if (x.art === 'host') {
      ersetzen(this.detail, zurueck || '', el('div', { class: 'reihe' }, hostSymbol(x.host, 24), el('h3', { style: { margin: 0 } }, x.host.name)),
        el('div', { style: { fontSize: '13px', color: 'var(--leise)' } }, geraetName(x.host.geraet)), el('code', null, hostZeile(x.host)),
        el('button', { class: 'btn primaer', type: 'button', onclick: () => { this.schliessen(); App.verbinden(x.host); } }, icon('terminal'), t('hilfe.verbinden')));
      return;
    }
    if (x.art === 'snippet') {
      this.befehlFormular(zurueck, t('hilfe.snippet'), x.snippet.name, x.snippet.befehl, x.snippet.beschreibung, null, null, fokus);
      return;
    }
    // Die Seite in der Sprache des Treffers holen — er kann aus der anderen Sprache stammen.
    let seite;
    const pfad = '/api/hilfe/seite/' + encodeURIComponent(x.t.name) + (x.t.sprache ? '?seitensprache=' + encodeURIComponent(x.t.sprache) : '');
    try { seite = await api('GET', pfad); } catch (e) { ersetzen(this.detail, zurueck || '', el('p', null, e.message)); return; }
    if (this.treffer[this.wahl] !== x || nr !== this.detailNr || !this.offen) return;
    const b = x.t.beispiel || seite.beispiele[0];
    const andere = seite.beispiele.filter((y) => !b || y.befehl !== b.befehl);
    this.befehlFormular(zurueck, t('hilfe.ausTldr', { name: seite.name }), b ? b.text : seite.name, b ? b.befehl : seite.name, seite.beschreibung, andere, seite, fokus);
  }

  befehlFormular(zurueck, ueber, titel, befehl, beschreibung, andere, seite, fokus) {
    const namen = platzhalter(optionenAufloesen(befehl));
    const werte = {};
    const gross = el('div', { class: 'befehl-gross' });
    const aktualisieren = () => {
      ersetzen(gross);
      const fertig = befehlFuellen(befehl, werte);
      if (namen.some((n) => !werte[n])) gross.append(befehlAnzeige(befehl.replace(/\{\{(.+?)\}\}/g, (m, n) => werte[n] ? werte[n] : m)));
      else gross.append(fertig);
    };
    const felder = namen.map((n) => {
      const id = 'ph-' + Math.random().toString(36).slice(2);
      const inp = el('input', { type: 'text', id, autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false', placeholder: n,
        oninput: () => { werte[n] = inp.value; aktualisieren(); },
        onkeydown: (e) => { if (e.key === 'Enter') { e.preventDefault(); this.aktuell.einfuegen(); } } });
      return el('div', { class: 'feld' }, el('label', { for: id }, n), inp);
    });
    aktualisieren();
    const einfuegen = () => { this.schliessen(); App.befehlEinfuegen(befehlFuellen(befehl, werte)); };
    this.aktuell = { einfuegen };
    // Texte einer tldr-Seite tragen deren Sprache (Vorlesen, Silbentrennung).
    const lang = seite ? seite.sprache : null;
    ersetzen(this.detail,
      zurueck || '',
      el('div', { style: { display: 'flex', flexDirection: 'column', gap: '6px' } },
        el('div', { style: { fontSize: '12px', color: 'var(--leise)' } }, ueber),
        el('div', { lang, style: { fontSize: '17px', fontWeight: 600, lineHeight: 1.35 } }, seite ? hilfeText(titel) : mitCode(titel)),
        beschreibung ? el('div', { lang, style: { fontSize: '14px', color: 'var(--text2)', lineHeight: 1.5 } }, seite ? hilfeText(beschreibung) : mitCode(beschreibung)) : null),
      gross,
      felder.length ? el('div', { style: { display: 'flex', flexDirection: 'column', gap: '10px', padding: '16px', borderRadius: '10px', background: 'var(--flaeche2)' } },
        el('div', { style: { fontSize: '13px', fontWeight: 600 } }, t('hilfe.platzhalter')), el('div', { class: 'raster2' }, felder)) : null,
      el('div', { class: 'reihe' },
        el('button', { class: 'btn primaer', type: 'button', onclick: einfuegen }, icon('einfuegen'), t('hilfe.einfuegen')),
        el('button', { class: 'btn', type: 'button', onclick: () => { this.schliessen(); App.snippetBearbeiten({ name: ohneMerkhilfen(ohneCode(titel)), befehl: optionenAufloesen(befehl), beschreibung: '' }); } }, icon('speichern'), t('hilfe.alsSnippet')),
        App.offeneTabs().length > 1 ? el('button', { class: 'btn', type: 'button', onclick: () => { this.schliessen(); App.anAlleSenden(befehlFuellen(befehl, werte)); } }, icon('funk'), t('hilfe.anAlle')) : null),
      andere && andere.length ? el('div', { style: { display: 'flex', flexDirection: 'column', gap: '12px', marginTop: '6px' } },
        el('div', { style: KOPF_STIL }, t('hilfe.weitere')),
        el('div', { lang, style: { display: 'flex', flexDirection: 'column', gap: '12px' } },
          andere.map((b) => beispielZeile(b, (y) => this.befehlFormular(zurueck, ueber, y.text, y.befehl, beschreibung, (seite ? seite.beispiele : andere).filter((z) => z.befehl !== y.befehl), seite, true))))) : null,
      seite ? el('p', { class: 'quelle' }, tldrQuelle(seite.sprache)) : null);
    if (fokus && felder.length && !App.istTelefon()) setTimeout(() => { if (this.offen) felder[0].querySelector('input').focus(); }, 20);
  }
}
App.HilfeLeiste = HilfeLeiste;
App.Palette = Palette;
