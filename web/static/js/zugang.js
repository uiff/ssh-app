'use strict';
// Zugaenge (Passwort/Schluessel), Snippets und Einstellungen samt Backup.

function feldMit(id, label, input, hinweis) {
  return el('div', { class: 'feld' }, el('label', { for: id }, label), input, hinweis ? el('div', { class: 'hinweis' }, hinweis) : null);
}
function textFeld(id, wert, extra) {
  return el('input', Object.assign({ id, type: 'text', value: wert || '', autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false' }, extra || {}));
}

App.zugangBearbeiten = function (z) {
  const neu = !z.id;
  const name = textFeld('z-name', z.name, { placeholder: 'z.B. admin-schlüssel' });
  const benutzer = textFeld('z-benutzer', z.benutzer, { placeholder: 'root, pi, admin …' });
  const passwort = el('input', { id: 'z-pw', type: 'password', autocomplete: 'new-password', placeholder: z.hatPasswort ? 'gespeichert — leer lassen = unverändert' : '' });
  const pwWeg = el('input', { type: 'checkbox' });
  const schluessel = el('textarea', { id: 'z-key', placeholder: z.oeffentlich ? 'gespeichert — leer lassen = unverändert' : '-----BEGIN OPENSSH PRIVATE KEY-----\n…', spellcheck: 'false' });
  const passphrase = el('input', { id: 'z-pp', type: 'password', autocomplete: 'off', placeholder: z.hatPassphrase ? 'gespeichert' : 'nur falls der Schlüssel geschützt ist' });
  const keyWeg = el('input', { type: 'checkbox' });
  const dateiWahl = el('input', { type: 'file', class: 'versteckt', onchange: async () => {
    const f = dateiWahl.files[0]; if (!f) return;
    if (f.size > 64 * 1024) { toast('Datei zu gross für einen Schlüssel', 'fehler'); return; }
    schluessel.value = await f.text();
  } });

  const oeffentlich = z.oeffentlich ? el('div', { class: 'feld' },
    el('span', { class: 'label' }, 'Öffentlicher Schlüssel'),
    el('div', { class: 'fp', style: { fontSize: '12px' } }, z.oeffentlich),
    el('div', { class: 'hinweis' }, (z.schluesselTyp || '') + ' · ' + (z.fingerprint || '')),
    el('div', { class: 'reihe' },
      el('button', { class: 'btn klein', type: 'button', onclick: () => kopieren(z.oeffentlich) }, icon('kopieren'), 'Kopieren'),
      el('button', { class: 'btn klein', type: 'button', onclick: () => schluesselInstallieren(z) }, icon('hoch'), 'Auf Host eintragen'),
      el('button', { class: 'btn klein', type: 'button', onclick: async () => {
        if (await frage('Den privaten Schlüssel unverschlüsselt herunterladen? Die Datei gibt Zugang zu allen Hosts mit diesem Schlüssel.', { ja: 'Herunterladen', gefahr: true })) herunterladen('/api/zugaenge/' + z.id + '/privat');
      } }, icon('runter'), 'Privaten exportieren'))) : null;

  const speichern = async () => {
    const daten = { name: name.value.trim(), benutzer: benutzer.value.trim(), passwort: passwort.value, passwortEntfernen: pwWeg.checked,
      schluessel: schluessel.value.trim(), schluesselEntfernen: keyWeg.checked, passphrase: passphrase.value };
    try {
      if (neu) await api('POST', '/api/zugaenge', daten); else await api('PUT', '/api/zugaenge/' + z.id, daten);
      dlg.zu(); await App.datenLaden(); toast('Gespeichert');
    } catch (e) { fehlerToast(e); }
  };
  const dlg = dialog({
    titel: neu ? 'Zugang anlegen' : 'Zugang bearbeiten', breit: true,
    inhalt: el('form', { onsubmit: (e) => { e.preventDefault(); speichern(); }, style: { display: 'flex', flexDirection: 'column', gap: '16px' } },
      el('div', { class: 'raster2' }, feldMit('z-name', 'Name', name), feldMit('z-benutzer', 'Benutzer', benutzer)),
      feldMit('z-pw', 'Passwort', passwort, z.hatPasswort ? el('label', { class: 'check', style: { minHeight: 0, padding: '4px 0' } }, pwWeg, 'Gespeichertes Passwort entfernen') : 'Wird auch für «keyboard-interactive» verwendet.'),
      el('div', { class: 'feld' },
        el('div', { class: 'reihe', style: { justifyContent: 'space-between' } }, el('label', { for: 'z-key' }, 'Privater Schlüssel'),
          el('button', { class: 'btn klein', type: 'button', onclick: () => dateiWahl.click() }, icon('ordner'), 'Datei wählen')),
        schluessel, dateiWahl,
        z.oeffentlich ? el('label', { class: 'check', style: { minHeight: 0, padding: '4px 0' } }, keyWeg, 'Gespeicherten Schlüssel entfernen') : null),
      feldMit('z-pp', 'Passphrase des Schlüssels', passphrase),
      oeffentlich,
      el('button', { type: 'submit', class: 'versteckt' })),
    knoepfe: [
      !neu ? el('button', { class: 'btn gefahr links', type: 'button', onclick: async () => {
        const text = z.hosts ? `Zugang «${z.name}» löschen? ${z.hosts} Host(s) fragen danach beim Verbinden nach dem Passwort.` : `Zugang «${z.name}» löschen?`;
        if (!(await frage(text, { ja: 'Löschen', gefahr: true }))) return;
        try { await api('DELETE', '/api/zugaenge/' + z.id); dlg.zu(); App.datenLaden(); } catch (e) { fehlerToast(e); }
      } }, icon('muell'), 'Löschen') : null,
      el('button', { class: 'btn', type: 'button', onclick: () => dlg.zu() }, 'Abbrechen'),
      el('button', { class: 'btn primaer', type: 'button', onclick: speichern }, 'Speichern'),
    ],
  });
};

App.schluesselErzeugen = function () {
  const name = textFeld('k-name', '', { placeholder: 'z.B. handy-2026' });
  const benutzer = textFeld('k-benutzer', '', { placeholder: 'root, pi, admin …' });
  const los = async () => {
    if (!name.value.trim()) { toast('Bitte einen Namen angeben', 'fehler'); return; }
    try {
      const z = await api('POST', '/api/zugaenge/erzeugen', { name: name.value.trim(), benutzer: benutzer.value.trim() });
      dlg.zu(); await App.datenLaden();
      App.zugangBearbeiten(Object.assign({ hosts: 0 }, z));
      toast('Schlüssel erzeugt — jetzt auf einem Host eintragen');
    } catch (e) { fehlerToast(e); }
  };
  const dlg = dialog({
    titel: 'Neuen Schlüssel erzeugen',
    inhalt: el('form', { onsubmit: (e) => { e.preventDefault(); los(); }, style: { display: 'flex', flexDirection: 'column', gap: '16px' } },
      el('p', { style: { margin: 0, color: 'var(--text2)', lineHeight: 1.5 } }, 'Erzeugt ein ed25519-Schlüsselpaar. Der private Teil bleibt verschlüsselt im Tresor.'),
      feldMit('k-name', 'Name', name), feldMit('k-benutzer', 'Benutzer', benutzer), el('button', { type: 'submit', class: 'versteckt' })),
    knoepfe: [el('button', { class: 'btn', type: 'button', onclick: () => dlg.zu() }, 'Abbrechen'), el('button', { class: 'btn primaer', type: 'button', onclick: los }, icon('schluessel'), 'Erzeugen')],
  });
};

function schluesselInstallieren(z) {
  const hosts = App.daten.hosts;
  if (!hosts.length) { toast('Zuerst einen Host anlegen', 'fehler'); return; }
  const wahl = el('select', { id: 'i-host' }, hosts.map((h) => el('option', { value: h.id }, h.name + ' (' + h.adresse + ')')));
  const pw = el('input', { id: 'i-pw', type: 'password', autocomplete: 'off', placeholder: 'nur wenn der Host keinen Zugang hat' });
  const los = async () => {
    try {
      await api('POST', '/api/zugaenge/' + z.id + '/installieren', { hostId: +wahl.value, passwort: pw.value });
      dlg.zu(); toast('Schlüssel eingetragen');
    } catch (e) { fehlerToast(e); }
  };
  const dlg = dialog({
    titel: 'Schlüssel auf Host eintragen',
    inhalt: el('div', { style: { display: 'flex', flexDirection: 'column', gap: '16px' } },
      el('p', { style: { margin: 0, color: 'var(--text2)', lineHeight: 1.5 } }, 'Haengt den öffentlichen Schlüssel an ~/.ssh/authorized_keys an. Angemeldet wird mit dem bisherigen Zugang des Hosts. Den Host vorher einmal im Terminal verbinden, damit sein Host-Schlüssel bekannt ist.'),
      feldMit('i-host', 'Host', wahl), feldMit('i-pw', 'Passwort (optional)', pw)),
    knoepfe: [el('button', { class: 'btn', type: 'button', onclick: () => dlg.zu() }, 'Abbrechen'), el('button', { class: 'btn primaer', type: 'button', onclick: los }, 'Eintragen')],
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
      dlg.zu(); await App.datenLaden(); toast('Snippet gespeichert');
    } catch (e) { fehlerToast(e); }
  };
  const dlg = dialog({
    titel: neu ? 'Snippet anlegen' : 'Snippet bearbeiten',
    inhalt: el('div', { style: { display: 'flex', flexDirection: 'column', gap: '16px' } },
      feldMit('s-name', 'Name', name),
      feldMit('s-befehl', 'Befehl', befehl, 'Platzhalter in doppelten geschweiften Klammern: {{dienst}}. Beim Einfügen fragt die App danach.'),
      feldMit('s-beschr', 'Beschreibung (optional)', beschr)),
    knoepfe: [
      !neu ? el('button', { class: 'btn gefahr links', type: 'button', onclick: async () => {
        if (!(await frage(`Snippet «${s.name}» löschen?`, { ja: 'Löschen', gefahr: true }))) return;
        try { await api('DELETE', '/api/snippets/' + s.id); dlg.zu(); App.datenLaden(); } catch (e) { fehlerToast(e); }
      } }, icon('muell'), 'Löschen') : null,
      el('button', { class: 'btn', type: 'button', onclick: () => dlg.zu() }, 'Abbrechen'),
      el('button', { class: 'btn primaer', type: 'button', onclick: speichern }, 'Speichern'),
    ],
  });
};

// ---------- Einstellungen ----------

App.einstellungen = function () {
  const st = App.status;
  const alt = el('input', { id: 'e-alt', type: 'password', autocomplete: 'current-password' });
  const neu1 = el('input', { id: 'e-neu', type: 'password', autocomplete: 'new-password' });
  const neu2 = el('input', { id: 'e-neu2', type: 'password', autocomplete: 'new-password' });
  const schrift = el('input', { id: 'e-schrift', type: 'number', min: 9, max: 24, value: App.schriftGroesse(), inputmode: 'numeric',
    onchange: () => App.schriftSetzen(parseInt(schrift.value, 10)) });
  const bekannt = el('div', { style: { display: 'flex', flexDirection: 'column' } });
  const bekanntZeichnen = () => ersetzen(bekannt, ...(App.daten.bekannt.length ? App.daten.bekannt.map((b) => el('div', { class: 'zeile-wrap', style: { borderBottom: '1px solid var(--linie)' } },
    el('div', { class: 'zeile', style: { cursor: 'default' } }, el('span', { class: 'haupt' }, el('b', { class: 'mono', style: { fontSize: '13px' } }, b.ziel), el('small', null, b.typ + ' · ' + b.fingerprint))),
    ikonKnopf('muell', 'Vergessen', async () => {
      if (!(await frage(`Host-Schlüssel von ${b.ziel} vergessen? Beim nächsten Verbinden fragt die App erneut.`, { ja: 'Vergessen', gefahr: true }))) return;
      try { await api('DELETE', '/api/bekannt/' + encodeURIComponent(b.ziel)); await App.datenLaden(); bekanntZeichnen(); } catch (e) { fehlerToast(e); }
    }))) : [el('p', { style: { margin: 0, color: 'var(--leise)' } }, 'Noch keine.')]));
  bekanntZeichnen();

  dialog({
    titel: 'Einstellungen', breit: true,
    inhalt: el('div', { style: { display: 'flex', flexDirection: 'column', gap: '18px' } },
      el('div', { class: 'einstellungen-gruppe' },
        el('h4', null, 'Backup'),
        el('p', null, 'Die Backup-Datei enthält Hosts, Ordner, Zugänge, Snippets und bekannte Host-Schlüssel — alles verschlüsselt. Einspielen geht nur mit dem Sicherheitsschlüssel, auf diesem oder einem anderen System.'),
        el('div', { class: 'reihe' },
          el('button', { class: 'btn primaer', type: 'button', onclick: () => herunterladen('/api/sicherung') }, icon('runter'), 'Backup herunterladen'),
          el('button', { class: 'btn', type: 'button', onclick: () => App.backupEinspielen(true) }, icon('hoch'), 'Backup einspielen'))),
      el('div', { class: 'einstellungen-gruppe' },
        el('h4', null, 'Passwort ändern'),
        el('p', null, 'Angemeldet als ', el('b', null, st.benutzer || ''), '. Der Sicherheitsschlüssel bleibt gültig.'),
        el('div', { class: 'raster2' }, feldMit('e-alt', 'Bisheriges Passwort', alt), el('div'), feldMit('e-neu', 'Neues Passwort', neu1, 'Mindestens ' + st.minPasswort + ' Zeichen'), feldMit('e-neu2', 'Neues Passwort wiederholen', neu2)),
        el('div', null, el('button', { class: 'btn', type: 'button', onclick: async () => {
          if (neu1.value !== neu2.value) { toast('Die neuen Passwörter stimmen nicht überein', 'fehler'); return; }
          try { await api('POST', '/api/passwort', { alt: alt.value, neu: neu1.value }); alt.value = neu1.value = neu2.value = ''; toast('Passwort geändert'); } catch (e) { fehlerToast(e); }
        } }, 'Passwort ändern'))),
      el('div', { class: 'einstellungen-gruppe' },
        el('h4', null, 'Darstellung'),
        el('div', { style: { maxWidth: '220px' } }, feldMit('e-schrift', 'Schriftgrösse im Terminal (px)', schrift))),
      el('div', { class: 'einstellungen-gruppe' },
        el('h4', null, 'Bekannte Host-Schlüssel'),
        el('p', null, 'Beim ersten Verbinden bestätigt. Aendert sich ein Schlüssel, warnt die App vor dem Verbinden.'),
        bekannt),
      el('div', { class: 'einstellungen-gruppe' },
        el('h4', null, 'Über'),
        el('p', null, 'SSH-App ', st.version, ' · Erstellt von ', el('a', { href: 'https://iotueli.ch', target: '_blank', rel: 'noopener' }, 'IoTUeli')),
        el('p', null, 'Verwendet xterm.js (MIT), IBM Plex Sans und JetBrains Mono (OFL), Befehlshilfe aus tldr-pages (CC BY 4.0). ',
          el('a', { href: 'vendor/licenses/', target: '_blank' }, 'Lizenzen')))),
  });
};

App.backupEinspielen = function (angemeldet) {
  const datei = el('input', { id: 'b-datei', type: 'file', accept: '.json,application/json' });
  const schluessel = el('textarea', { id: 'b-key', spellcheck: 'false', autocapitalize: 'characters', style: { minHeight: '80px' }, placeholder: 'ABCD-EFGH-…' });
  const los = async () => {
    if (!datei.files[0]) { toast('Bitte die Backup-Datei wählen', 'fehler'); return; }
    if (angemeldet && !(await frage('Alle Hosts, Zugänge und Snippets auf diesem System werden durch das Backup ersetzt. Danach meldest du dich mit Benutzer und Passwort des gesicherten Systems an.', { titel: 'Backup einspielen', ja: 'Ersetzen', gefahr: true }))) return;
    const fd = new FormData();
    fd.append('datei', datei.files[0]);
    fd.append('schluessel', schluessel.value);
    try {
      const r = await api('POST', angemeldet ? '/api/sicherung' : '/api/einspielen', fd);
      dlg.zu();
      toast(`Backup eingespielt (${r.hosts} Hosts). Bitte als «${r.benutzer}» anmelden.`);
      App.starten();
    } catch (e) { fehlerToast(e); }
  };
  const dlg = dialog({
    titel: 'Backup einspielen',
    inhalt: el('div', { style: { display: 'flex', flexDirection: 'column', gap: '16px' } },
      feldMit('b-datei', 'Backup-Datei', datei),
      feldMit('b-key', 'Sicherheitsschlüssel', schluessel, 'Der Schlüssel, der bei der Einrichtung des gesicherten Systems angezeigt wurde. Gross-/Kleinschreibung und Leerzeichen sind egal.')),
    knoepfe: [el('button', { class: 'btn', type: 'button', onclick: () => dlg.zu() }, 'Abbrechen'), el('button', { class: 'btn primaer', type: 'button', onclick: los }, icon('hoch'), 'Einspielen')],
  });
};
