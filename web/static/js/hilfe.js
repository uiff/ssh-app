'use strict';
// Befehlshilfe: Seitenleiste zur aktuellen Eingabe und Palette (Strg+K).

class HilfeLeiste {
  constructor() {
    this.titelCode = el('code', { style: { color: 'var(--akzent)', fontWeight: 400 } });
    this.inhalt = el('div', { class: 'hilfe-inhalt' });
    this.element = el('aside', { class: 'seite rechts', 'aria-label': 'Befehlshilfe' },
      el('div', { class: 'blatt-griff', 'aria-hidden': 'true' }),
      el('div', { class: 'seite-kopf' }, el('span', { class: 'titel' }, 'Befehlshilfe ', this.titelCode),
        ikonKnopf('suche', 'Befehl suchen', () => App.palette()),
        ikonKnopf('x', 'Hilfe schliessen', () => App.seiteUmschalten('hilfe', false))),
      el('div', { class: 'seite-inhalt' }, this.inhalt));
    this.letzte = null;
    this.leer();
  }

  leer() {
    this.letzte = '';
    this.titelCode.textContent = '';
    ersetzen(this.inhalt, 
      el('p', { style: { margin: 0, color: 'var(--text2)', lineHeight: 1.55, fontSize: '14px' } },
        'Tippe einen Befehl ins Terminal. Hier erscheinen dann Beispiele und was die Schalter bedeuten.'),
      el('button', { class: 'btn voll', type: 'button', onclick: () => App.palette() }, icon('suche'), 'Befehl in Worten suchen'),
      el('p', { class: 'quelle' }, App.status && App.status.hilfeSeiten ? `${App.status.hilfeSeiten} Befehle offline · tldr-pages, CC BY 4.0` : 'Befehlshilfe nicht installiert (tldr-Archive fehlen)'));
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
      ersetzen(this.inhalt, el('p', { style: { margin: 0, color: 'var(--text2)' } }, 'Zu diesem Befehl gibt es keine Hilfeseite.'),
        el('button', { class: 'btn voll', type: 'button', onclick: () => App.palette(befehl.split(/\s+/)[0]) }, icon('suche'), 'Suchen'));
      return;
    }
    this.titelCode.textContent = r.seite.name;
    const teile = r.teile.slice(1);
    ersetzen(this.inhalt, 
      el('div', { style: { fontSize: '14px', color: 'var(--text2)', lineHeight: 1.5 } }, r.seite.beschreibung),
      teile.length ? el('div', { class: 'erklaerung' },
        el('div', { class: 'kopfzeile' }, 'Deine Eingabe erklärt'),
        teile.map((t) => el('div', { class: 'teil' }, el('code', null, t.wort), el('span', null, t.erklaerung)))) : null,
      el('div', { class: 'kopfzeile', style: { fontSize: '11px', fontWeight: 600, letterSpacing: '.06em', color: 'var(--leise)' } }, 'BEISPIELE'),
      el('div', { style: { display: 'flex', flexDirection: 'column', gap: '14px' } }, r.seite.beispiele.map((b) => beispielZeile(b))),
      el('p', { class: 'quelle' }, (r.seite.sprache === 'en' ? 'Englisch · ' : '') + 'tldr-pages, CC BY 4.0 ',
        r.seite.mehr ? el('a', { href: r.seite.mehr, target: '_blank', rel: 'noopener noreferrer' }, 'Handbuch') : null));
  }
}

function beispielZeile(b, beiWahl) {
  return el('div', { class: 'beispiel' },
    el('div', { class: 't' }, b.text),
    el('div', { class: 'b' }, el('code', null, befehlAnzeige(b.befehl)),
      el('button', { class: 'btn klein', type: 'button', onclick: () => beiWahl ? beiWahl(b) : App.befehlVorbereiten(b.befehl, b.text) }, 'Nehmen')));
}

// ---------- Palette ----------

class Palette {
  constructor() {
    this.offen = false;
  }

  oeffnen(start) {
    if (this.offen) { this.feld.focus(); return; }
    this.offen = true;
    this.treffer = [];
    this.wahl = 0;
    this.feld = el('input', { type: 'search', placeholder: 'Befehl, Host oder Snippet — auch in Worten: «grosse Dateien finden»', autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false', 'aria-label': 'Suchen',
      oninput: () => this.suchen(), onkeydown: (e) => this.taste(e) });
    this.liste = el('div', { class: 'palette-liste', role: 'listbox' });
    this.detail = el('div', { class: 'palette-detail versteckt' });
    this.schleier = el('div', { class: 'schleier', onclick: () => this.schliessen() });
    this.element = el('div', { class: 'palette', role: 'dialog', 'aria-label': 'Suchen' },
      el('div', { class: 'palette-suche' }, icon('suche'), this.feld, ikonKnopf('x', 'Schliessen', () => this.schliessen())),
      el('div', { class: 'palette-teile', style: { position: 'relative' } }, this.liste, this.detail),
      el('div', { class: 'palette-fuss' },
        el('span', null, el('b', null, '↑↓'), ' wählen'), el('span', null, el('b', null, 'Tab'), ' Platzhalter'),
        el('span', null, el('b', null, 'Enter'), ' einfügen'), el('span', null, el('b', null, 'Esc'), ' schliessen'),
        el('span', { style: { marginLeft: 'auto' } }, 'Einfügen führt nichts aus — Enter drückst du selbst')));
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
    this.element.remove(); this.schleier.remove();
    document.removeEventListener('keydown', this.escape);
    if (App.aktiv && !App.istTelefon()) App.aktiv.term.focus();
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
    this.treffer = q ? lokal.slice(0, 8) : lokal.filter((t) => t.art === 'snippet').concat(lokal.filter((t) => t.art === 'host')).slice(0, 12);
    this.wahl = 0;
    this.zeichnen();
    clearTimeout(this.timer);
    if (!q) return;
    this.timer = setTimeout(async () => {
      let r = [];
      try { r = await api('GET', '/api/hilfe/suche?q=' + encodeURIComponent(q)); } catch (e) { return; }
      if (this.feld.value.trim() !== q) return;
      this.treffer = lokal.slice(0, 6).concat((r || []).map((t) => ({ art: 'tldr', t })));
      this.wahl = 0;
      this.zeichnen();
    }, 180);
  }

  zeichnen() {
    if (!this.treffer.length) {
      ersetzen(this.liste, el('div', { class: 'leer' }, this.feld.value ? 'Nichts gefunden.' : 'Tippe, was du suchst.'));
      this.detail.classList.add('versteckt');
      return;
    }
    const gruppen = { host: 'HOSTS', snippet: 'MEINE SNIPPETS', tldr: 'BEFEHLE (TLDR)' };
    let letzte = '';
    const kinder = [];
    this.treffer.forEach((t, i) => {
      if (t.art !== letzte) { kinder.push(el('div', { class: 'gruppe-titel' }, gruppen[t.art])); letzte = t.art; }
      let titel, unter, q;
      if (t.art === 'host') { titel = t.host.name; unter = t.host.adresse; q = 'verbinden'; }
      else if (t.art === 'snippet') { titel = t.snippet.name; unter = t.snippet.befehl; q = 'Snippet'; }
      else { titel = t.t.beispiel ? t.t.beispiel.text : t.t.beschreibung; unter = t.t.beispiel ? optionenAufloesen(t.t.beispiel.befehl) : t.t.name; q = t.t.name; }
      kinder.push(el('button', { class: 'treffer' + (i === this.wahl ? ' aktiv' : ''), type: 'button', role: 'option', 'aria-selected': i === this.wahl ? 'true' : 'false',
        onclick: () => { this.wahl = i; this.zeichnen(); this.ausfuehrenOderZeigen(true); } },
        el('span', { class: 'haupt' }, el('span', null, titel), el('code', null, unter)), el('span', { class: 'q' }, q)));
    });
    ersetzen(this.liste, ...kinder);
    if (!App.istTelefon()) this.detailZeigen();
  }

  taste(e) {
    if (e.key === 'ArrowDown') { e.preventDefault(); this.wahl = Math.min(this.treffer.length - 1, this.wahl + 1); this.zeichnen(); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); this.wahl = Math.max(0, this.wahl - 1); this.zeichnen(); }
    else if (e.key === 'Enter') { e.preventDefault(); this.ausfuehrenOderZeigen(false); }
    else if (e.key === 'Tab' && this.detail.querySelector('input')) { e.preventDefault(); this.detail.querySelector('input').focus(); }
  }

  ausfuehrenOderZeigen(geklickt) {
    const t = this.treffer[this.wahl];
    if (!t) return;
    if (t.art === 'host') { this.schliessen(); App.verbinden(t.host); return; }
    if (App.istTelefon() || geklickt) this.detailZeigen();
    if (!geklickt && this.aktuell) this.aktuell.einfuegen();
  }

  async detailZeigen() {
    const t = this.treffer[this.wahl];
    this.aktuell = null;
    if (!t) return;
    const zurueck = App.istTelefon() ? el('button', { class: 'btn klein', type: 'button', style: { alignSelf: 'flex-start' }, onclick: () => this.detail.classList.add('versteckt') }, icon('zurueck'), 'Zurück') : null;
    this.detail.classList.remove('versteckt');
    if (t.art === 'host') {
      ersetzen(this.detail, zurueck || '', el('h3', { style: { margin: 0 } }, t.host.name), el('code', null, t.host.adresse + ':' + t.host.port),
        el('button', { class: 'btn primaer', type: 'button', onclick: () => { this.schliessen(); App.verbinden(t.host); } }, icon('terminal'), 'Verbinden'));
      return;
    }
    if (t.art === 'snippet') {
      this.befehlFormular(zurueck, 'Snippet', t.snippet.name, t.snippet.befehl, t.snippet.beschreibung, null);
      return;
    }
    let seite;
    try { seite = await api('GET', '/api/hilfe/seite/' + encodeURIComponent(t.t.name)); } catch (e) { ersetzen(this.detail, zurueck || '', el('p', null, e.message)); return; }
    if (this.treffer[this.wahl] !== t) return;
    const b = t.t.beispiel || seite.beispiele[0];
    const andere = seite.beispiele.filter((x) => !b || x.befehl !== b.befehl);
    this.befehlFormular(zurueck, 'Beispiel aus tldr · ' + seite.name, b ? b.text : seite.name, b ? b.befehl : seite.name, seite.beschreibung, andere, seite);
  }

  befehlFormular(zurueck, ueber, titel, befehl, beschreibung, andere, seite) {
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
    ersetzen(this.detail, 
      zurueck || '',
      el('div', { style: { display: 'flex', flexDirection: 'column', gap: '6px' } },
        el('div', { style: { fontSize: '12px', color: 'var(--leise)' } }, ueber),
        el('div', { style: { fontSize: '20px', fontWeight: 600 } }, titel),
        beschreibung ? el('div', { style: { fontSize: '14px', color: 'var(--text2)', lineHeight: 1.5 } }, beschreibung) : null),
      gross,
      felder.length ? el('div', { style: { display: 'flex', flexDirection: 'column', gap: '10px', padding: '16px', borderRadius: '10px', background: 'var(--flaeche2)' } },
        el('div', { style: { fontSize: '13px', fontWeight: 600 } }, 'Platzhalter ausfüllen'), el('div', { class: 'raster2' }, felder)) : null,
      el('div', { class: 'reihe' },
        el('button', { class: 'btn primaer', type: 'button', onclick: einfuegen }, icon('einfuegen'), 'Ins Terminal einfügen'),
        el('button', { class: 'btn', type: 'button', onclick: () => { this.schliessen(); App.snippetBearbeiten({ name: titel, befehl: optionenAufloesen(befehl), beschreibung: '' }); } }, icon('speichern'), 'Als Snippet speichern'),
        App.offeneTabs().length > 1 ? el('button', { class: 'btn', type: 'button', onclick: () => { this.schliessen(); App.anAlleSenden(befehlFuellen(befehl, werte)); } }, icon('funk'), 'An alle Tabs') : null),
      andere && andere.length ? el('div', { style: { display: 'flex', flexDirection: 'column', gap: '12px', marginTop: '6px' } },
        el('div', { style: { fontSize: '11px', fontWeight: 600, letterSpacing: '.06em', color: 'var(--leise)' } }, 'WEITERE BEISPIELE'),
        andere.map((b) => beispielZeile(b, (x) => this.befehlFormular(zurueck, ueber, x.text, x.befehl, beschreibung, (seite ? seite.beispiele : andere).filter((y) => y.befehl !== x.befehl), seite)))) : null,
      seite ? el('p', { class: 'quelle' }, (seite.sprache === 'en' ? 'Englisch · ' : '') + 'tldr-pages, CC BY 4.0') : null);
    if (felder.length && !App.istTelefon()) setTimeout(() => felder[0].querySelector('input').focus(), 20);
  }
}
App.HilfeLeiste = HilfeLeiste;
App.Palette = Palette;
