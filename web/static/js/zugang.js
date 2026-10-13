'use strict';
// Zugaenge (Passwort/Schluessel), Snippets und Einstellungen samt Backup.
// Texte: js/sprache/hosts.js (Schluessel «hosts.»).

function feldMit(id, label, input, hinweis) {
  return el('div', { class: 'feld' }, el('label', { for: id }, label), input, hinweis ? el('div', { class: 'hinweis' }, hinweis) : null);
}
function textFeld(id, wert, extra) {
  return el('input', Object.assign({ id, type: 'text', value: wert || '', autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false' }, extra || {}));
}

App.zugangBearbeiten = function (z) {
  const neu = !z.id;
  const name = textFeld('z-name', z.name, { placeholder: t('hosts.zugang.namePlatzhalter') });
  const benutzer = textFeld('z-benutzer', z.benutzer, { placeholder: t('hosts.feld.benutzerBeispiele') });
  const passwort = el('input', { id: 'z-pw', type: 'password', autocomplete: 'new-password', placeholder: z.hatPasswort ? t('hosts.zugang.gespeichertLeer') : '' });
  const pwWeg = el('input', { type: 'checkbox' });
  const schluessel = el('textarea', { id: 'z-key', placeholder: z.oeffentlich ? t('hosts.zugang.gespeichertLeer') : '-----BEGIN OPENSSH PRIVATE KEY-----\n…', spellcheck: 'false' });
  const passphrase = el('input', { id: 'z-pp', type: 'password', autocomplete: 'off', placeholder: t(z.hatPassphrase ? 'hosts.zugang.gespeichert' : 'hosts.zugang.passphraseLeer') });
  const keyWeg = el('input', { type: 'checkbox' });
  const dateiWahl = el('input', { type: 'file', class: 'versteckt', onchange: async () => {
    const f = dateiWahl.files[0]; if (!f) return;
    if (f.size > 64 * 1024) { toast(t('hosts.zugang.dateiZuGross'), 'fehler'); return; }
    schluessel.value = await f.text();
  } });

  // Oeffentlicher Schluessel: lange Zeichenketten ohne Trennstelle umbrechen (Telefon).
  const oeffentlich = z.oeffentlich ? el('div', { class: 'feld' },
    el('span', { class: 'label' }, t('hosts.zugang.oeffentlich')),
    el('div', { class: 'fp', style: { fontSize: '12px' } }, z.oeffentlich),
    el('div', { class: 'hinweis fp-hinweis' }, (z.schluesselTyp || '') + ' · ' + (z.fingerprint || '')),
    el('div', { class: 'reihe', style: { gap: '8px' } },
      el('button', { class: 'btn klein', type: 'button', onclick: () => kopieren(z.oeffentlich) }, icon('kopieren'), t('hosts.zugang.kopieren')),
      el('button', { class: 'btn klein', type: 'button', onclick: () => schluesselInstallieren(z) }, icon('hoch'), t('hosts.zugang.eintragen')),
      el('button', { class: 'btn klein', type: 'button', onclick: async () => {
        if (await frage(t('hosts.zugang.privatFrage'), { ja: t('hosts.zugang.herunterladen'), gefahr: true })) herunterladen('/api/zugaenge/' + z.id + '/privat');
      } }, icon('runter'), t('hosts.zugang.privatHerunterladen')))) : null;

  const speichern = async () => {
    const daten = { name: name.value.trim(), benutzer: benutzer.value.trim(), passwort: passwort.value, passwortEntfernen: pwWeg.checked,
      schluessel: schluessel.value.trim(), schluesselEntfernen: keyWeg.checked, passphrase: passphrase.value };
    try {
      if (neu) await api('POST', '/api/zugaenge', daten); else await api('PUT', '/api/zugaenge/' + z.id, daten);
      dlg.zu(); await App.datenLaden(); toast(t('hosts.gespeichert'));
    } catch (e) { fehlerToast(e); }
  };
  const dlg = dialog({
    titel: t(neu ? 'hosts.zugang.titelNeu' : 'hosts.zugang.titelBearbeiten'), breit: true,
    inhalt: el('form', { onsubmit: (e) => { e.preventDefault(); speichern(); }, style: { display: 'flex', flexDirection: 'column', gap: '16px' } },
      el('div', { class: 'raster2' }, feldMit('z-name', t('hosts.feld.name'), name), feldMit('z-benutzer', t('hosts.feld.benutzer'), benutzer)),
      oeffentlich,
      feldMit('z-pw', t('hosts.feld.passwort'), passwort, z.hatPasswort
        ? el('label', { class: 'check', style: { padding: '0' } }, pwWeg, t('hosts.zugang.pwEntfernen'))
        : t('hosts.zugang.pwHinweis')),
      el('div', { class: 'feld' },
        el('div', { class: 'reihe', style: { justifyContent: 'space-between', flexWrap: 'wrap', gap: '4px 8px' } }, el('label', { for: 'z-key', style: { fontSize: '13px', color: 'var(--text2)' } }, t('hosts.zugang.privat')),
          el('button', { class: 'btn klein', type: 'button', onclick: () => dateiWahl.click() }, icon('ordner'), t('hosts.zugang.ausDatei'))),
        schluessel, dateiWahl,
        z.oeffentlich ? el('label', { class: 'check', style: { padding: '0' } }, keyWeg, t('hosts.zugang.keyEntfernen')) : null),
      feldMit('z-pp', t('hosts.zugang.passphrase'), passphrase),
      el('button', { type: 'submit', class: 'versteckt' })),
    knoepfe: [
      !neu ? el('button', { class: 'btn gefahr links', type: 'button', onclick: async () => {
        const n = z.hosts || 0;
        const text = t(n === 0 ? 'hosts.zugang.loeschenFrage' : n === 1 ? 'hosts.zugang.loeschenFrageHost' : 'hosts.zugang.loeschenFrageHosts', { name: z.name, n });
        if (!(await frage(text, { ja: t('hosts.knopf.loeschen'), gefahr: true }))) return;
        try { await api('DELETE', '/api/zugaenge/' + z.id); dlg.zu(); App.datenLaden(); } catch (e) { fehlerToast(e); }
      } }, icon('muell'), t('hosts.knopf.loeschen')) : null,
      // Telefon: ohne «Abbrechen» (Kreuz oben, Zurueck-Geste), damit der Fuss einzeilig bleibt.
      el('button', { class: 'btn nur-desktop', type: 'button', onclick: () => dlg.zu() }, t('hosts.knopf.abbrechen')),
      el('button', { class: 'btn primaer', type: 'button', onclick: speichern }, t('hosts.knopf.speichern')),
    ],
  });
  dlg.element.classList.add('zugang-dialog');
};

App.schluesselErzeugen = function () {
  const name = textFeld('k-name', '', { placeholder: t('hosts.schluessel.namePlatzhalter') });
  const benutzer = textFeld('k-benutzer', '', { placeholder: t('hosts.feld.benutzerBeispiele') });
  const los = async () => {
    if (!name.value.trim()) { toast(t('hosts.schluessel.nameFehlt'), 'fehler'); return; }
    try {
      const z = await api('POST', '/api/zugaenge/erzeugen', { name: name.value.trim(), benutzer: benutzer.value.trim() });
      dlg.zu(); await App.datenLaden();
      App.zugangBearbeiten(Object.assign({ hosts: 0 }, z));
      toast(t('hosts.schluessel.erzeugt'));
    } catch (e) { fehlerToast(e); }
  };
  const dlg = dialog({
    titel: t('hosts.schluessel.titel'),
    inhalt: el('form', { onsubmit: (e) => { e.preventDefault(); los(); }, style: { display: 'flex', flexDirection: 'column', gap: '16px' } },
      el('p', { style: { margin: 0, color: 'var(--text2)', lineHeight: 1.5 } }, t('hosts.schluessel.text')),
      feldMit('k-name', t('hosts.feld.name'), name), feldMit('k-benutzer', t('hosts.feld.benutzer'), benutzer), el('button', { type: 'submit', class: 'versteckt' })),
    knoepfe: [el('button', { class: 'btn', type: 'button', onclick: () => dlg.zu() }, t('hosts.knopf.abbrechen')), el('button', { class: 'btn primaer', type: 'button', onclick: los }, icon('schluessel'), t('hosts.schluessel.erzeugen'))],
  });
};

function schluesselInstallieren(z) {
  const hosts = App.daten.hosts;
  if (!hosts.length) { toast(t('hosts.installieren.keinHost'), 'fehler'); return; }
  const wahl = el('select', { id: 'i-host' }, hosts.map((h) => el('option', { value: h.id }, h.name + ' (' + h.adresse + ')')));
  const pw = el('input', { id: 'i-pw', type: 'password', autocomplete: 'off', placeholder: t('hosts.installieren.pwPlatzhalter') });
  const los = async () => {
    try {
      await api('POST', '/api/zugaenge/' + z.id + '/installieren', { hostId: +wahl.value, passwort: pw.value });
      dlg.zu(); toast(t('hosts.installieren.fertig'));
    } catch (e) { fehlerToast(e); }
  };
  const dlg = dialog({
    titel: t('hosts.installieren.titel'),
    inhalt: el('div', { style: { display: 'flex', flexDirection: 'column', gap: '16px' } },
      el('p', { style: { margin: 0, color: 'var(--text2)', lineHeight: 1.5 } }, t('hosts.installieren.text')),
      feldMit('i-host', t('hosts.installieren.host'), wahl), feldMit('i-pw', t('hosts.installieren.pw'), pw)),
    knoepfe: [el('button', { class: 'btn', type: 'button', onclick: () => dlg.zu() }, t('hosts.knopf.abbrechen')), el('button', { class: 'btn primaer', type: 'button', onclick: los }, t('hosts.installieren.eintragen'))],
  });
}

// ---------- Snippets ----------

App.snippetBearbeiten = function (s) {
  const neu = !s.id;
  const name = textFeld('s-name', s.name);
  const befehl = el('textarea', { id: 's-befehl', spellcheck: 'false', style: { minHeight: '90px' } }, s.befehl || '');
  const beschr = textFeld('s-beschr', s.beschreibung);
  const speichern = async () => {
    const d = { name: name.value.trim(), befehl: befehl.value, beschreibung: beschr.value.trim() };
    try {
      if (neu) await api('POST', '/api/snippets', d); else await api('PUT', '/api/snippets/' + s.id, d);
      dlg.zu(); await App.datenLaden(); toast(t('hosts.snippet.gespeichert'));
    } catch (e) { fehlerToast(e); }
  };
  const dlg = dialog({
    titel: t(neu ? 'hosts.snippet.titelNeu' : 'hosts.snippet.titelBearbeiten'),
    inhalt: el('div', { style: { display: 'flex', flexDirection: 'column', gap: '16px' } },
      feldMit('s-name', t('hosts.feld.name'), name),
      feldMit('s-befehl', t('hosts.snippet.befehl'), befehl, t('hosts.snippet.befehlHinweis', { beispiel: '{{' + t('hosts.snippet.platzhalterName') + '}}' })),
      feldMit('s-beschr', t('hosts.snippet.beschreibung'), beschr)),
    knoepfe: [
      !neu ? el('button', { class: 'btn gefahr links', type: 'button', onclick: async () => {
        if (!(await frage(t('hosts.snippet.loeschenFrage', { name: s.name }), { ja: t('hosts.knopf.loeschen'), gefahr: true }))) return;
        try { await api('DELETE', '/api/snippets/' + s.id); dlg.zu(); App.datenLaden(); } catch (e) { fehlerToast(e); }
      } }, icon('muell'), t('hosts.knopf.loeschen')) : null,
      el('button', { class: 'btn', type: 'button', onclick: () => dlg.zu() }, t('hosts.knopf.abbrechen')),
      el('button', { class: 'btn primaer', type: 'button', onclick: speichern }, t('hosts.knopf.speichern')),
    ],
  });
};

// ---------- Einstellungen ----------

// Keyway-Zeichen (Schluesselloch mit Prompt, wie img/keyway.svg) als Inline-SVG: das Loch folgt
// der Akzentfarbe (Farben in hosts.css, .ueber-zeichen).
function ueberZeichen(groesse) {
  const ns = 'http://www.w3.org/2000/svg';
  const s = document.createElementNS(ns, 'svg');
  s.setAttribute('viewBox', '0 0 64 64');
  s.setAttribute('width', groesse);
  s.setAttribute('height', groesse);
  s.setAttribute('class', 'ueber-zeichen');
  s.setAttribute('aria-hidden', 'true');
  const teil = (art, attr) => { const e = document.createElementNS(ns, art); for (const k in attr) e.setAttribute(k, attr[k]); s.append(e); };
  teil('rect', { class: 'grund', x: 0.5, y: 0.5, width: 63, height: 63, rx: 14 });
  teil('path', { class: 'loch', d: 'M32 9a11 11 0 0 1 6.6 19.8L43 54H21l4.4-25.2A11 11 0 0 1 32 9z' });
  teil('path', { class: 'prompt', d: 'M26.5 35.5l5 4-5 4M33 47.5h6' });
  return s;
}

App.einstellungen = function () {
  const st = App.status;
  const alt = el('input', { id: 'e-alt', type: 'password', autocomplete: 'current-password' });
  const neu1 = el('input', { id: 'e-neu', type: 'password', autocomplete: 'new-password' });
  const neu2 = el('input', { id: 'e-neu2', type: 'password', autocomplete: 'new-password' });
  const schrift = el('input', { id: 'e-schrift', type: 'number', min: 9, max: 24, value: App.schriftGroesse(), inputmode: 'numeric',
    onchange: () => App.schriftSetzen(parseInt(schrift.value, 10)) });
  // Sprache: Wechsel laedt die Seite neu; offene Terminals haengen sich danach von selbst wieder an.
  // Der Name jeder Sprache steht in ihr selbst (lang-Attribut fuer Screenreader).
  const sprachen = el('div', { class: 'sprachen', role: 'group', 'aria-labelledby': 'e-sprache-label' },
    SPRACHEN.map((l) => el('button', { type: 'button', class: 'sprach-knopf', lang: l.id, 'aria-pressed': l.id === I18N.sprache ? 'true' : 'false',
      onclick: () => App.spracheWechseln(l.id) }, el('span', { class: 'sprach-kuerzel', 'aria-hidden': 'true' }, l.id.toUpperCase()), l.name)));
  // Akzentfarbe: runde Farbpunkte mit Namen, sofort wirksam und je Browser gemerkt.
  const akzente = el('div', { class: 'akzente', role: 'group', 'aria-labelledby': 'e-akzent-label' });
  let akzent = akzentFuer(akzentGemerkt()).id;
  const akzenteZeichnen = () => {
    ersetzen(akzente, AKZENTE.map((a) => el('button', { type: 'button', class: 'akzent-knopf', 'aria-pressed': a.id === akzent ? 'true' : 'false',
      onclick: () => { akzent = akzentSetzen(a.id, true).id; akzenteZeichnen(); } },
    el('span', { class: 'farbpunkt', style: { background: a.farbe } }), a[I18N.sprache] || a.de)));
  };
  akzenteZeichnen();
  const bekannt = el('div', { style: { display: 'flex', flexDirection: 'column' } });
  const bekanntZeichnen = () => ersetzen(bekannt, ...(App.daten.bekannt.length ? App.daten.bekannt.map((b) => el('div', { class: 'zeile-wrap', style: { borderBottom: '1px solid var(--linie)' } },
    el('div', { class: 'zeile', style: { cursor: 'default' } }, el('span', { class: 'haupt' }, el('b', { class: 'mono', style: { fontSize: '13px' } }, b.ziel), el('small', null, b.typ + ' · ' + b.fingerprint))),
    ikonKnopf('muell', t('hosts.einst.vergessenLabel', { ziel: b.ziel }), async () => {
      if (!(await frage(t('hosts.einst.vergessenFrage', { ziel: b.ziel }), { ja: t('hosts.einst.vergessen'), gefahr: true }))) return;
      try { await api('DELETE', '/api/bekannt/' + encodeURIComponent(b.ziel)); await App.datenLaden(); bekanntZeichnen(); } catch (e) { fehlerToast(e); }
    }))) : [el('p', { style: { margin: 0, color: 'var(--leise)' } }, t('hosts.einst.keine'))]));
  bekanntZeichnen();

  dialog({
    titel: t('hosts.einst.titel'), breit: true,
    inhalt: el('div', { style: { display: 'flex', flexDirection: 'column', gap: '18px' } },
      el('div', { class: 'einstellungen-gruppe' },
        el('h4', null, t('hosts.einst.darstellung')),
        el('div', { class: 'feld' }, el('span', { class: 'label', id: 'e-sprache-label' }, t('hosts.einst.sprache')), sprachen,
          el('div', { class: 'hinweis' }, t('hosts.einst.spracheHinweis'))),
        el('div', { class: 'feld' }, el('span', { class: 'label', id: 'e-akzent-label' }, t('hosts.einst.akzent')), akzente),
        el('div', { class: 'schrift-feld' }, feldMit('e-schrift', t('hosts.einst.schrift'), schrift))),
      el('div', { class: 'einstellungen-gruppe' },
        el('h4', null, t('hosts.einst.backup')),
        el('p', null, t('hosts.einst.backupText')),
        el('div', { class: 'reihe einst-knoepfe' },
          el('button', { class: 'btn primaer', type: 'button', onclick: () => herunterladen('/api/sicherung') }, icon('runter'), t('hosts.einst.backupLaden')),
          el('button', { class: 'btn', type: 'button', onclick: () => App.backupEinspielen(true) }, icon('hoch'), t('hosts.einst.backupEinspielen')))),
      el('div', { class: 'einstellungen-gruppe' },
        el('h4', null, t('hosts.einst.pwAendern')),
        el('p', null, tTeile('hosts.einst.angemeldetAls', { benutzer: el('b', null, st.benutzer || '') })),
        el('div', { class: 'raster2' }, feldMit('e-alt', t('hosts.einst.pwAlt'), alt), el('div', { class: 'nur-desktop' }), feldMit('e-neu', t('hosts.einst.pwNeu'), neu1, t('hosts.einst.pwMin', { n: st.minPasswort })), feldMit('e-neu2', t('hosts.einst.pwNeu2'), neu2)),
        el('div', null, el('button', { class: 'btn', type: 'button', onclick: async () => {
          if (neu1.value !== neu2.value) { toast(t('hosts.einst.pwUngleich'), 'fehler'); return; }
          try { await api('POST', '/api/passwort', { alt: alt.value, neu: neu1.value }); alt.value = neu1.value = neu2.value = ''; toast(t('hosts.einst.pwGeaendert')); } catch (e) { fehlerToast(e); }
        } }, t('hosts.einst.pwAendern')))),
      el('div', { class: 'einstellungen-gruppe' },
        el('h4', null, t('hosts.einst.bekannt')),
        el('p', null, t('hosts.einst.bekanntText')),
        bekannt),
      el('div', { class: 'einstellungen-gruppe' },
        el('h4', null, t('hosts.einst.ueber')),
        el('div', { class: 'ueber-marke' }, ueberZeichen(48),
          el('div', { class: 'ueber-name' }, el('b', null, MARKE),
            el('span', null, tTeile('hosts.einst.version', { version: st.version || '–', hersteller: el('a', { href: 'https://iotueli.ch', target: '_blank', rel: 'noopener' }, 'IoTUeli') })))),
        el('p', null, tTeile('hosts.einst.verwendet', { quelle: tldrQuelle(false, true) })),
        el('div', null, el('a', { class: 'btn klein', href: 'vendor/licenses/', target: '_blank', rel: 'noopener' }, t('hosts.einst.lizenzen'))))),
  });
};

App.backupEinspielen = function (angemeldet) {
  // Eigener Knopf statt des Browser-Felds: dessen Beschriftung folgt der Browser-Sprache, nicht der App.
  const dateiName = el('span', { class: 'datei-name' }, t('hosts.backup.keineDatei'));
  const datei = el('input', { type: 'file', class: 'versteckt', accept: '.json,application/json', tabindex: '-1', 'aria-hidden': 'true',
    onchange: () => { dateiName.textContent = datei.files[0] ? datei.files[0].name : t('hosts.backup.keineDatei'); } });
  const dateiWahl = el('div', { class: 'datei-wahl' },
    el('button', { id: 'b-datei', class: 'btn', type: 'button', onclick: () => datei.click() }, icon('datei'), t('hosts.backup.dateiWaehlen')),
    dateiName, datei);
  const schluessel = el('textarea', { id: 'b-key', spellcheck: 'false', autocapitalize: 'characters', style: { minHeight: '80px' }, placeholder: 'ABCD-EFGH-…' });
  const los = async () => {
    if (!datei.files[0]) { toast(t('hosts.backup.dateiFehlt'), 'fehler'); return; }
    if (angemeldet && !(await frage(t('hosts.backup.frage'), { titel: t('hosts.backup.titel'), ja: t('hosts.backup.ersetzen'), gefahr: true }))) return;
    const fd = new FormData();
    fd.append('datei', datei.files[0]);
    fd.append('schluessel', schluessel.value);
    try {
      const r = await api('POST', angemeldet ? '/api/sicherung' : '/api/einspielen', fd);
      dlg.zu();
      toast(t('hosts.backup.fertig', { hosts: hostsAnzahl(r.hosts || 0), benutzer: r.benutzer }));
      App.starten();
    } catch (e) { fehlerToast(e); }
  };
  const dlg = dialog({
    titel: t('hosts.backup.titel'),
    inhalt: el('div', { style: { display: 'flex', flexDirection: 'column', gap: '16px' } },
      feldMit('b-datei', t('hosts.backup.datei'), dateiWahl),
      feldMit('b-key', t('hosts.backup.schluessel'), schluessel, t('hosts.backup.schluesselHinweis'))),
    knoepfe: [el('button', { class: 'btn', type: 'button', onclick: () => dlg.zu() }, t('hosts.knopf.abbrechen')), el('button', { class: 'btn primaer', type: 'button', onclick: los }, icon('hoch'), t('hosts.backup.einspielen'))],
  });
};
