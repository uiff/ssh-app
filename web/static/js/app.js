'use strict';
// Hauptprogramm: Einrichtung, Anmeldung, Arbeitsplatz, Tabs, Telefon-Tasten.

const IOTUELI = 'https://iotueli.ch';
const telefonAbfrage = window.matchMedia('(max-width: 899px)');

App.istTelefon = () => telefonAbfrage.matches;
App.tabs = [];
App.aktiv = null;
App.daten = { ordner: [], hosts: [], zugaenge: [], snippets: [], bekannt: [] };
App.status = {};
App.rundfunk = false;
App.mod = { ctrl: false, alt: false };

// Anzeige der Tab-Zustaende (die internen Namen sind Bezeichner, keine Texte).
const ZUSTAND_TEXT = { verbinde: 'Verbindet …', bereit: 'Verbunden', getrennt: 'Getrennt — verbindet neu', fehler: 'Fehler', beendet: 'Beendet', abgeloest: 'In anderem Fenster' };
App.zustandText = (z) => ZUSTAND_TEXT[z] || z;

// Geraetesymbol eines Hosts in seiner Farbe, optional mit Zustandspunkt (gruen verbunden,
// gelb verbindet, rot Fehler/beendet, grau in anderem Fenster).
function hostSymbol(host, groesse, zustand) {
  return el('span', { class: 'hsym' + (zustand ? ' z-' + zustand : ''), style: host && host.farbe ? { color: host.farbe } : null },
    geraetIcon(host ? host.geraet : '', groesse || 18),
    zustand ? el('span', { class: 'hsym-punkt', 'aria-hidden': 'true' }) : null);
}
App.hostSymbol = hostSymbol;

App.schriftGroesse = () => lokal('schrift', App.istTelefon() ? 13 : 14);
App.schriftSetzen = (n) => {
  if (!(n >= 9 && n <= 24)) return;
  lokalSetzen('schrift', n);
  for (const t of App.tabs) { t.term.options.fontSize = n; t.anpassen(); }
};

function logoLink(hoehe) {
  return el('a', { class: 'logo-link', href: IOTUELI, target: '_blank', rel: 'noopener', title: 'IoTUeli — iotueli.ch' },
    el('img', { src: 'img/iotueli.svg', alt: 'IoTUeli', style: { height: hoehe + 'px' } }));
}
function erstelltVon() {
  return el('span', null, 'SSH-App · Erstellt von ', el('a', { href: IOTUELI, target: '_blank', rel: 'noopener' }, 'IoTUeli'));
}

// ---------- Hoehe auf dem Telefon (Bildschirmtastatur) ----------
function hoeheAnpassen() {
  const vv = window.visualViewport;
  const h = vv ? vv.height : window.innerHeight;
  document.documentElement.style.setProperty('--app-hoehe', h + 'px');
  if (vv && vv.offsetTop) window.scrollTo(0, 0);
  if (App.aktiv) App.aktiv.anpassen();
}
if (window.visualViewport) window.visualViewport.addEventListener('resize', hoeheAnpassen);
window.addEventListener('resize', hoeheAnpassen);
hoeheAnpassen();

// ---------- Start ----------
App.starten = async function () {
  // Alles abraeumen, was zur vorherigen Anmeldung gehoert: Dialoge, Menues, Palette, Blaetter,
  // Tabs und die geladenen Daten (sonst zeigt z.B. die Palette auf der Anmeldeseite noch alle Hosts).
  App.aktiv = null;
  ebenenAlleZu();
  if (App.paletteObj) { App.paletteObj.schliessen(); App.paletteObj = null; }
  for (const t of App.tabs) t.schliessen();
  App.tabs = [];
  App.daten = { ordner: [], hosts: [], zugaenge: [], snippets: [], bekannt: [] };
  App.rundfunk = false;
  App.seitenleiste = null; App.dateien = null; App.hilfe = null; App.start = null; App.tabsLeiste = null; App.blattEbene = null;
  App.modSetzen('ctrl', false); App.modSetzen('alt', false);
  try { App.status = await api('GET', '/api/status'); }
  catch (e) {
    ersetzen(document.getElementById('app'), el('div', { class: 'einstieg' }, el('div', { class: 'einstieg-form' },
      el('h2', null, 'Keine Verbindung'), el('p', { class: 'unter' }, e.message),
      el('button', { class: 'btn primaer', type: 'button', onclick: App.starten }, 'Erneut versuchen'))));
    return;
  }
  if (!App.status.eingerichtet) einrichtenSeite();
  else if (!App.status.angemeldet) anmeldeSeite();
  else arbeitsplatz();
};
App.abgemeldet = function () {
  if (App._abmeldenLaeuft) return;
  App._abmeldenLaeuft = true;
  setTimeout(() => { App._abmeldenLaeuft = false; }, 1500);
  toast('Abgemeldet — bitte neu anmelden. Laufende Sitzungen bleiben erhalten.');
  App.starten();
};

function einstiegRahmen(formular) {
  return el('div', { class: 'einstieg' },
    el('div', { class: 'einstieg-bild' },
      logoLink(40),
      el('div', { class: 'platz', style: { flex: 1 } }),
      el('div', { class: 'term-deko' }, '$ ssh admin@server\nverbunden · ed25519\n$ '),
      el('h1', null, 'Alle Server. Ein Browser-Tab.'),
      el('p', null, 'Terminal, Dateien und Befehlshilfe an einem Ort — auch auf dem Telefon. Passwörter und Schlüssel bleiben verschlüsselt.'),
      el('div', { class: 'platz', style: { flex: 1 } }),
      el('div', { class: 'fuss' }, erstelltVon())),
    el('div', { class: 'einstieg-form' }, formular));
}

function formFeld(id, label, typ, extra) {
  const inp = el('input', Object.assign({ id, type: typ || 'text', autocapitalize: 'off', spellcheck: 'false' }, extra || {}));
  return [inp, el('div', { class: 'feld' }, el('label', { for: id }, label), inp)];
}

function einrichtenSeite() {
  const [b, bFeld] = formFeld('s-benutzer', 'Benutzername', 'text', { autocomplete: 'username', value: 'admin' });
  const [p1, p1Feld] = formFeld('s-pw', 'Passwort', 'password', { autocomplete: 'new-password' });
  const [p2, p2Feld] = formFeld('s-pw2', 'Passwort wiederholen', 'password', { autocomplete: 'new-password' });
  const meldung = el('div');
  const knopf = el('button', { class: 'btn primaer voll', type: 'submit' }, 'Einrichten');
  const form = el('form', { style: { display: 'flex', flexDirection: 'column', gap: '18px' }, onsubmit: async (e) => {
    e.preventDefault();
    ersetzen(meldung);
    if (p1.value !== p2.value) { meldung.append(el('div', { class: 'meldung fehler' }, 'Die Passwörter stimmen nicht überein.')); return; }
    knopf.disabled = true; knopf.textContent = 'Richte ein …';
    try {
      const r = await api('POST', '/api/einrichten', { benutzer: b.value.trim(), passwort: p1.value });
      lokalSetzen('benutzer', b.value.trim());
      sicherheitsschluesselSeite(r.sicherheitsschluessel, b.value.trim());
    } catch (err) {
      meldung.append(el('div', { class: 'meldung fehler' }, err.message));
      knopf.disabled = false; knopf.textContent = 'Einrichten';
    }
  } },
    el('h2', null, 'Willkommen'),
    el('p', { class: 'unter' }, 'Lege den Benutzer für diese App fest. Das Passwort entsperrt auch den Tresor mit deinen SSH-Zugängen.'),
    bFeld, p1Feld, el('div', { class: 'hinweis', style: { fontSize: '12.5px', color: 'var(--leise)', marginTop: '-10px' } }, 'Mindestens ' + App.status.minPasswort + ' Zeichen'), p2Feld,
    meldung, knopf,
    el('div', { class: 'reihe', style: { justifyContent: 'space-between' } },
      el('span', { style: { color: 'var(--leise)', fontSize: '14px' } }, 'Umzug von einem anderen System?'),
      el('button', { class: 'textlink', type: 'button', onclick: () => App.backupEinspielen(false) }, 'Backup einspielen')));
  ersetzen(document.getElementById('app'), einstiegRahmen(form));
  if (!App.istTelefon()) p1.focus();
}

function sicherheitsschluesselSeite(schluessel, benutzer) {
  const ok = el('input', { type: 'checkbox', onchange: () => { weiter.disabled = !ok.checked; } });
  const weiter = el('button', { class: 'btn primaer voll', type: 'button', disabled: true, onclick: () => App.starten() }, 'Weiter zur App');
  const datei = 'SSH-App Sicherheitsschlüssel\n\nBenutzer: ' + benutzer + '\nErstellt: ' + new Date().toLocaleString('de-CH') + '\nAdresse: ' + location.origin +
    '\n\nSicherheitsschlüssel:\n' + schluessel + '\n\nMit diesem Schlüssel kannst du das Passwort zurücksetzen und Backups einspielen.\nSicher aufbewahren (Passwort-Manager, ausgedruckt) und nicht weitergeben.\n';
  const inhalt = el('div', { style: { display: 'flex', flexDirection: 'column', gap: '18px' } },
    el('h2', null, 'Dein Sicherheitsschlüssel'),
    el('p', { class: 'unter' }, 'Damit setzt du das Passwort zurück und spielst ein Backup auf einem anderen System ein. Er wird nur jetzt angezeigt — ohne ihn sind Backups nicht lesbar.'),
    el('div', { class: 'schluessel-box', 'aria-label': 'Sicherheitsschlüssel' }, schluessel),
    el('div', { class: 'reihe' },
      el('button', { class: 'btn', type: 'button', onclick: () => kopieren(schluessel) }, icon('kopieren'), 'Kopieren'),
      el('button', { class: 'btn', type: 'button', onclick: () => textDatei('ssh-app-sicherheitsschluessel.txt', datei) }, icon('runter'), 'Als Datei speichern')),
    el('div', { class: 'meldung warn' }, icon('warnung'), el('span', null, 'Bewahre ihn getrennt von der App auf, zum Beispiel in deinem Passwort-Manager.')),
    el('label', { class: 'check' }, ok, el('span', null, 'Ich habe den Sicherheitsschlüssel sicher aufbewahrt.')),
    weiter);
  ersetzen(document.getElementById('app'), einstiegRahmen(inhalt));
}

function anmeldeSeite() {
  const [b, bFeld] = formFeld('a-benutzer', 'Benutzername', 'text', { autocomplete: 'username', value: lokal('benutzer', '') });
  const [p, pFeld] = formFeld('a-pw', 'Passwort', 'password', { autocomplete: 'current-password' });
  const meldung = el('div');
  const knopf = el('button', { class: 'btn primaer voll', type: 'submit' }, 'Anmelden');
  const form = el('form', { style: { display: 'flex', flexDirection: 'column', gap: '18px' }, onsubmit: async (e) => {
    e.preventDefault();
    ersetzen(meldung);
    knopf.disabled = true; knopf.textContent = 'Prüfe …';
    try {
      await api('POST', '/api/anmelden', { benutzer: b.value.trim(), passwort: p.value });
      lokalSetzen('benutzer', b.value.trim());
      App.starten();
    } catch (err) {
      meldung.append(el('div', { class: 'meldung fehler' }, err.message));
      knopf.disabled = false; knopf.textContent = 'Anmelden';
      p.select();
    }
  } },
    el('h2', null, 'Anmelden'),
    el('p', { class: 'unter' }, 'Die Anmeldung entsperrt auch den Tresor.'),
    bFeld, pFeld, meldung, knopf,
    el('button', { class: 'textlink', type: 'button', onclick: zuruecksetzenSeite }, 'Passwort vergessen? Mit Sicherheitsschlüssel zurücksetzen'),
    el('p', { style: { margin: 0, fontSize: '13px', color: 'var(--leise)', lineHeight: 1.5 } }, 'Nach 5 Fehlversuchen ist die Anmeldung von diesem Gerät 15 Minuten gesperrt.'));
  ersetzen(document.getElementById('app'), einstiegRahmen(form));
  if (!App.istTelefon()) (b.value ? p : b).focus();
}

function zuruecksetzenSeite() {
  const k = el('textarea', { id: 'r-key', spellcheck: 'false', autocapitalize: 'characters', style: { minHeight: '84px' }, placeholder: 'ABCD-EFGH-…' });
  const [b, bFeld] = formFeld('r-benutzer', 'Benutzername (neu oder bisher)', 'text', { autocomplete: 'username', value: lokal('benutzer', '') });
  const [p1, p1Feld] = formFeld('r-pw', 'Neues Passwort', 'password', { autocomplete: 'new-password' });
  const [p2, p2Feld] = formFeld('r-pw2', 'Neues Passwort wiederholen', 'password', { autocomplete: 'new-password' });
  const meldung = el('div');
  const form = el('form', { style: { display: 'flex', flexDirection: 'column', gap: '18px' }, onsubmit: async (e) => {
    e.preventDefault();
    ersetzen(meldung);
    if (p1.value !== p2.value) { meldung.append(el('div', { class: 'meldung fehler' }, 'Die Passwörter stimmen nicht überein.')); return; }
    try {
      await api('POST', '/api/zuruecksetzen', { schluessel: k.value, benutzer: b.value.trim(), passwort: p1.value });
      lokalSetzen('benutzer', b.value.trim());
      toast('Passwort neu gesetzt — bitte anmelden');
      App.starten();
    } catch (err) { meldung.append(el('div', { class: 'meldung fehler' }, err.message)); }
  } },
    el('h2', null, 'Passwort zurücksetzen'),
    el('p', { class: 'unter' }, 'Mit dem Sicherheitsschlüssel aus der Einrichtung. Deine Hosts und Zugänge bleiben erhalten.'),
    el('div', { class: 'feld' }, el('label', { for: 'r-key' }, 'Sicherheitsschlüssel'), k),
    bFeld, p1Feld, p2Feld, meldung,
    el('button', { class: 'btn primaer voll', type: 'submit' }, 'Passwort setzen'),
    el('button', { class: 'textlink', type: 'button', onclick: anmeldeSeite }, 'Zurück zur Anmeldung'));
  ersetzen(document.getElementById('app'), einstiegRahmen(form));
}

// ---------- Arbeitsplatz ----------

App.seiten = {};
App.seiteOffen = (n) => !!App.seiten[n];

async function arbeitsplatz() {
  App.seitenleiste = null;
  await App.datenLaden(true);
  App.seiten = {
    links: !App.istTelefon() && lokal('seiteLinks', true),
    dateien: !App.istTelefon() && lokal('seiteDateien', false),
    hilfe: !App.istTelefon() && lokal('seiteHilfe', false),
  };
  App.seitenleiste = new App.Seitenleiste();
  App.dateien = new App.Dateien();
  App.hilfe = new App.HilfeLeiste();
  App.paletteObj = new App.Palette();

  App.terms = el('div', { class: 'terms' });
  App.start = el('div', { class: 'start' });
  App.tabsLeiste = el('div', { class: 'tabs', role: 'tablist' });
  App.tabWahl = el('button', { class: 'tab-wahl nur-telefon', type: 'button', onclick: (e) => tabMenue(e.currentTarget) });
  App.statusleiste = el('div', { class: 'statusleiste' });
  App.rundfunkBand = el('div', { class: 'rundfunk versteckt' });
  App.schleier = el('div', { class: 'schleier versteckt', style: { zIndex: 35 }, onclick: () => { for (const n of ['links', 'dateien', 'hilfe']) App.seiteUmschalten(n, false); } });
  App.knoepfe = {
    dateien: ikonKnopf('ordner', 'Dateien', () => App.seiteUmschalten('dateien')),
    hilfe: ikonKnopf('hilfe', 'Befehlshilfe', () => App.seiteUmschalten('hilfe')),
    rundfunk: ikonKnopf('funk', 'Eingaben an alle Tabs senden', () => App.rundfunkSetzen(!App.rundfunk), { class: 'ikon nur-desktop' }),
  };

  const kopf = el('header', { class: 'kopf' },
    ikonKnopf('menue', 'Hosts und Tresor', () => App.seiteUmschalten('links'), { class: 'ikon nur-telefon' }),
    el('div', { class: 'marke nur-desktop' }, logoLink(24), el('span', { class: 'trenner' }), el('span', { class: 'name' }, 'SSH-App')),
    App.tabWahl,
    el('button', { class: 'btn klein primaer nur-desktop', type: 'button', style: { marginLeft: '16px' }, onclick: () => App.palette('') }, icon('plus'), 'Neue Sitzung'),
    el('div', { class: 'platz nur-desktop' }),
    el('button', { class: 'suche nur-desktop', type: 'button', onclick: () => App.palette() }, icon('suche', 15), el('span', null, 'Suchen …'), el('kbd', null, 'Strg ⇧ K')),
    App.knoepfe.rundfunk, App.knoepfe.dateien, App.knoepfe.hilfe,
    el('span', { class: 'tresor-chip nur-desktop', title: 'Tresor entsperrt — sperrt nach ' + App.status.leerlaufMin + ' Minuten ohne Bedienung' }, icon('schloss', 14), 'Tresor offen'),
    ikonKnopf('mehr', 'Menü', (e) => hauptMenue(e.currentTarget)));

  const tasten = telefonTasten();
  const mitte = el('main', { class: 'mitte' }, App.tabsLeiste, App.terms, App.statusleiste, tasten);
  App.terms.append(App.start);
  const koerper = el('div', { class: 'koerper' }, App.seitenleiste.element, App.dateien.element, mitte, App.hilfe.element, App.schleier);
  ersetzen(document.getElementById('app'), kopf, App.rundfunkBand, koerper);
  seitenZeichnen();

  // Laufende Sitzungen wieder anhaengen (Neuladen, Telefon-Wechsel).
  try {
    const laufend = await api('GET', '/api/sitzungen');
    for (const s of laufend) if (!s.beendet) App.tabs.push(new App.TerminalTab({ sitzungId: s.id, hostId: s.hostId, titel: s.titel }));
  } catch (e) { /* leer */ }
  for (const t of App.tabs) App.terms.append(t.element);
  App.aktivieren(App.tabs[App.tabs.length - 1] || null);
  if (App.tabs.length) toast(App.tabs.length === 1 ? '1 laufende Sitzung wieder angehängt' : App.tabs.length + ' laufende Sitzungen wieder angehängt');
}

App.datenLaden = async function () {
  try { App.daten = await api('GET', '/api/daten'); } catch (e) { if (e.status !== 401) fehlerToast(e); return; }
  if (App.seitenleiste) App.seitenleiste.zeichnen();
  if (App.start && !App.aktiv) startZeichnen();
};

App.seiteUmschalten = function (n, an, nurTelefon) {
  if (nurTelefon && !App.istTelefon()) return;
  const neu = an === undefined ? !App.seiten[n] : an;
  if (App.istTelefon() && neu) for (const k of Object.keys(App.seiten)) App.seiten[k] = false;
  App.seiten[n] = neu;
  if (!App.istTelefon()) lokalSetzen('seite' + n[0].toUpperCase() + n.slice(1), neu);
  seitenZeichnen();
  if (n === 'dateien' && neu) App.dateien.fuerTab(App.aktiv);
  if (n === 'hilfe' && neu && App.aktiv) App.hilfe.zeile(App.aktiv.aktuelleZeile());
  if (n === 'links' && neu && App.istTelefon()) App.seitenleiste.zeichnen();
  setTimeout(() => App.aktiv && App.aktiv.anpassen(), 30);
};

function seitenZeichnen() {
  App.seitenleiste.element.classList.toggle('versteckt', !App.seiten.links);
  App.dateien.element.classList.toggle('versteckt', !App.seiten.dateien);
  App.hilfe.element.classList.toggle('versteckt', !App.seiten.hilfe);
  App.knoepfe.dateien.classList.toggle('an', App.seiten.dateien);
  App.knoepfe.hilfe.classList.toggle('an', App.seiten.hilfe);
  const blatt = App.istTelefon() && (App.seiten.links || App.seiten.dateien || App.seiten.hilfe);
  App.schleier.classList.toggle('versteckt', !blatt);
  // Telefon: Ein offenes Blatt ist eine Ebene — die Zurueck-Geste schliesst es.
  if (blatt && !App.blattEbene) App.blattEbene = ebeneAuf(() => App.blaetterZu());
  else if (!blatt && App.blattEbene) { ebeneZu(App.blattEbene); App.blattEbene = null; }
}
// Telefon: alle Blaetter zu, damit das Terminal frei liegt.
App.blaetterZu = function () {
  if (!App.seitenleiste) return;
  for (const n of ['links', 'dateien', 'hilfe']) if (App.seiten[n]) App.seiteUmschalten(n, false, true);
};
telefonAbfrage.addEventListener('change', () => { if (App.seitenleiste) { App.seiten.links = !App.istTelefon(); seitenZeichnen(); App.tabsZeichnen(); } });

function hauptMenue(anker) {
  menue(anker, [
    App.istTelefon() ? { text: 'Suchen und neue Sitzung', icon: 'suche', aktion: () => App.palette('') } : null,
    App.istTelefon() && App.offeneTabs().length > 1 ? { text: App.rundfunk ? 'An alle senden: aus' : 'An alle Tabs senden', icon: 'funk', aktion: () => App.rundfunkSetzen(!App.rundfunk) } : null,
    App.istTelefon() ? { text: 'Schrift grösser', icon: 'schrift', aktion: () => App.schriftSetzen(App.schriftGroesse() + 1) } : null,
    App.istTelefon() ? { text: 'Schrift kleiner', icon: 'schrift', aktion: () => App.schriftSetzen(App.schriftGroesse() - 1) } : null,
    { text: 'Einstellungen und Backup', icon: 'zahnrad', aktion: () => App.einstellungen() },
    { text: 'Abmelden (Sitzungen laufen weiter)', icon: 'abmelden', aktion: abmelden },
    App.offeneTabs().length ? { text: 'Alle Sitzungen beenden und abmelden', icon: 'abmelden', gefahr: true, aktion: async () => {
      if (!(await frage('Alle Terminal-Sitzungen beenden und abmelden?', { ja: 'Beenden', gefahr: true }))) return;
      for (const t of App.tabs) if (t.sitzungId) await api('DELETE', '/api/sitzungen/' + t.sitzungId).catch(() => {});
      abmelden();
    } } : null,
    { text: 'IoTUeli — iotueli.ch', icon: 'link', aktion: () => window.open(IOTUELI, '_blank', 'noopener') },
  ]);
}

async function abmelden() {
  try { await api('POST', '/api/abmelden'); } catch (e) { /* trotzdem */ }
  App.starten();
}

// ---------- Tabs ----------

App.offeneTabs = () => App.tabs.filter((t) => t.zustand !== 'beendet' && t.zustand !== 'fehler');

App.verbinden = function (host) {
  const t = new App.TerminalTab({ hostId: host.id, titel: host.name });
  App.tabs.push(t);
  App.terms.append(t.element);
  App.aktivieren(t);
};

App.neuVerbinden = function (tab) {
  const host = App.daten.hosts.find((h) => h.id === tab.hostId);
  App.tabEntfernen(tab);
  if (host) App.verbinden(host);
};

App.aktivieren = function (tab) {
  App.aktiv = tab;
  for (const t of App.tabs) t.element.style.display = t === tab ? '' : 'none';
  App.start.classList.toggle('versteckt', !!tab);
  if (!tab) startZeichnen();
  App.tabsZeichnen();
  if (tab) {
    setTimeout(() => { tab.anpassen(true); if (!App.istTelefon()) tab.term.focus(); }, 20);
  }
  if (App.dateien) App.dateien.fuerTab(tab);
  if (App.hilfe && tab) App.hilfe.zeile(tab.aktuelleZeile());
};

App.tabEntfernen = function (tab) {
  const i = App.tabs.indexOf(tab);
  if (i < 0) return;
  tab.schliessen();
  App.tabs.splice(i, 1);
  if (App.aktiv === tab) App.aktivieren(App.tabs[Math.min(i, App.tabs.length - 1)] || null);
  else App.tabsZeichnen();
};

App.tabSchliessen = async function (tab) {
  if (tab.zustand === 'bereit' || tab.zustand === 'getrennt' || tab.zustand === 'abgeloest') {
    const text = tab.zustand === 'abgeloest' ? `Sitzung «${tab.titel}» beenden? Sie ist gerade in einem anderen Fenster offen und wird dort ebenfalls beendet.` : `Sitzung «${tab.titel}» beenden?`;
    if (!(await frage(text, { ja: 'Beenden', gefahr: true }))) return;
  }
  if (tab.sitzungId) api('DELETE', '/api/sitzungen/' + tab.sitzungId).catch(() => {});
  App.tabEntfernen(tab);
};

function tabHost(t) { return App.daten.hosts.find((x) => x.id === t.hostId); }

App.tabsZeichnen = function () {
  if (!App.tabsLeiste) return;
  ersetzen(App.tabsLeiste,
    ...App.tabs.map((t) => el('div', { class: 'tab' + (t === App.aktiv ? ' aktiv' : ''), role: 'tab', 'aria-selected': t === App.aktiv ? 'true' : 'false', title: t.titel + ' — ' + App.zustandText(t.zustand) },
      el('button', { class: 'tab-knopf', type: 'button', onclick: () => App.aktivieren(t), onauxclick: (e) => { if (e.button === 1) App.tabSchliessen(t); } },
        hostSymbol(tabHost(t), 16, t.zustand), el('span', { class: 't' }, t.titel)),
      el('button', { class: 'zu', type: 'button', 'aria-label': 'Tab schliessen', onclick: () => App.tabSchliessen(t) }, icon('x', 14)))),
    ikonKnopf('plus', 'Neue Sitzung', () => App.palette(''), { class: 'ikon tab-neu' }));
  // Telefon: Titel des aktiven Tabs als Auswahl
  const t = App.aktiv;
  ersetzen(App.tabWahl,
    t ? hostSymbol(tabHost(t), 18, t.zustand) : null,
    el('span', { class: 't' }, t ? t.titel : 'SSH-App'),
    t && t.zustand !== 'bereit' ? el('span', { class: 'sr' }, App.zustandText(t.zustand)) : null,
    App.tabs.length > 1 ? el('span', { class: 'chip' }, String(App.tabs.length)) : null,
    icon('pfeilRunter', 14));
  // Statusleiste (Desktop)
  if (t) {
    const h = tabHost(t);
    ersetzen(App.statusleiste,
      el('span', { class: 'zustand z-' + t.zustand }, el('span', { class: 'hsym-punkt', 'aria-hidden': 'true' }), App.zustandText(t.zustand)),
      h ? el('span', { class: 'geraet' }, geraetIcon(h.geraet, 14), geraetFuer(h.geraet).de) : null,
      h ? el('span', null, hostZeile(h)) : null,
      t.fingerprint ? el('span', null, t.fingerprint) : null,
      el('span', null, t.term.cols + ' × ' + t.term.rows));
  } else ersetzen(App.statusleiste, el('span', null, 'Keine Sitzung'));
  // Punkte der verbundenen Hosts nachfuehren, ohne ein Filterfeld beim Tippen zu stoeren.
  if (App.seitenleiste && App.seitenleiste.reiter === 'hosts' && !App.seitenleiste.element.contains(document.activeElement)) App.seitenleiste.zeichnen();
};

function tabMenue(anker) {
  if (!App.tabs.length) { App.palette(''); return; }
  menue(anker, [
    ...App.tabs.map((t) => ({ text: el('span', { class: 'menue-text' }, el('span', null, t.titel), t.zustand !== 'bereit' ? el('small', null, App.zustandText(t.zustand)) : null),
      symbol: hostSymbol(tabHost(t), 18, t.zustand), aktiv: t === App.aktiv, aktion: () => App.aktivieren(t) })),
    { text: 'Neue Sitzung', icon: 'plus', aktion: () => App.palette('') },
    App.aktiv ? { text: 'Diesen Tab schliessen', icon: 'x', gefahr: true, aktion: () => App.tabSchliessen(App.aktiv) } : null,
  ]);
}

function startZeichnen() {
  const d = App.daten;
  const zuletzt = lokal('zuletzt', []);
  const sortiert = [...d.hosts].sort((a, b) => {
    const ia = zuletzt.indexOf(a.id), ib = zuletzt.indexOf(b.id);
    return (ia < 0 ? 999 : ia) - (ib < 0 ? 999 : ib) || a.name.localeCompare(b.name);
  });
  ersetzen(App.start, 
    el('h2', null, d.hosts.length ? 'Verbinden mit …' : 'Willkommen'),
    d.hosts.length ? el('div', { class: 'karten' }, sortiert.map((h) => el('button', { class: 'karte', type: 'button', title: geraetFuer(h.geraet).de, onclick: () => App.verbinden(h) },
      hostSymbol(h, 20),
      el('span', { class: 'haupt' }, el('b', null, h.name), el('small', null, hostZeile(h))),
      icon('weiter')))) :
      el('p', { style: { margin: 0, color: 'var(--text2)', lineHeight: 1.6, maxWidth: '560px' } }, 'Lege deinen ersten Host an. Zugangsdaten kannst du im Tresor speichern oder beim Verbinden eingeben.'),
    el('div', { class: 'reihe' },
      el('button', { class: 'btn primaer', type: 'button', onclick: () => App.hostBearbeiten({}) }, icon('plus'), 'Host anlegen'),
      el('button', { class: 'btn', type: 'button', onclick: () => App.zugangBearbeiten({}) }, icon('schluessel'), 'Zugang anlegen'),
      el('button', { class: 'btn', type: 'button', onclick: () => App.palette() }, icon('hilfe'), 'Befehlshilfe')),
    el('p', { style: { marginTop: 'auto', fontSize: '13px', color: 'var(--leise)' } }, erstelltVon()));
}

// Merkt die zuletzt verbundenen Hosts (Startseite sortiert danach).
const altVerbinden = App.verbinden;
App.verbinden = function (host) {
  const z = lokal('zuletzt', []).filter((x) => x !== host.id);
  z.unshift(host.id);
  lokalSetzen('zuletzt', z.slice(0, 30));
  App.seiteUmschalten('links', false, true);
  altVerbinden(host);
};

// ---------- Rueckfragen beim Verbinden ----------

// tab: der fragende Tab. Er schliesst die Rueckfrage, wenn die Verbindung vorher abbricht.
App.hostkeyFragen = function (n, tab) {
  return new Promise((resolve) => {
    let ok = false;
    const geaendert = n.status === 'geaendert';
    const d = dialog({
      titel: geaendert ? 'Host-Schlüssel hat sich geändert' : 'Neuer Host',
      inhalt: el('div', { style: { display: 'flex', flexDirection: 'column', gap: '14px' } },
        geaendert ? el('div', { class: 'meldung fehler' }, icon('warnung'), el('span', null, el('b', null, 'Vorsicht. '),
          'Der Server meldet sich mit einem anderen Schlüssel als beim letzten Mal. Das passiert nach einer Neuinstallation — oder wenn jemand die Verbindung abfängt. Nur annehmen, wenn du den Grund kennst.')) :
          el('p', { style: { margin: 0, lineHeight: 1.55, color: 'var(--text2)' } }, 'Erste Verbindung zu ', el('b', { class: 'mono' }, n.ziel), '. Wenn du sicher sein willst: Fingerabdruck auf dem Server mit ', el('code', null, 'ssh-keygen -lf /etc/ssh/ssh_host_*_key.pub'), ' vergleichen.'),
        geaendert && n.altFp ? el('div', { class: 'feld' }, el('span', { class: 'label' }, 'Bisher'), el('div', { class: 'fp' }, n.altFp)) : null,
        el('div', { class: 'feld' }, el('span', { class: 'label' }, (geaendert ? 'Jetzt' : 'Fingerabdruck') + ' · ' + n.typ), el('div', { class: 'fp' }, n.fp))),
      knoepfe: [
        el('button', { class: 'btn', type: 'button', onclick: () => d.zu() }, 'Abbrechen'),
        el('button', { class: 'btn ' + (geaendert ? 'gefahr' : 'primaer'), type: 'button', onclick: () => { ok = true; d.zu(); } }, geaendert ? 'Neuen Schlüssel annehmen' : 'Vertrauen und verbinden'),
      ],
      beimSchliessen: () => { if (tab && tab.rueckfrage === d) tab.rueckfrage = null; resolve(ok); },
    });
    if (tab) tab.rueckfrage = d;
  });
};

// Antwort: { benutzer, passwort, speichern } — speichern legt nach erfolgreicher Anmeldung einen
// Zugang im Tresor an und weist ihn dem Host zu (der Server meldet das mit «bereit» + gespeichert).
App.anmeldungFragen = function (n, tab) {
  return new Promise((resolve) => {
    let wert = null;
    const b = el('input', { id: 'f-b', type: 'text', value: n.benutzer || '', autocomplete: 'username', autocapitalize: 'off', spellcheck: 'false' });
    const p = el('input', { id: 'f-p', type: 'password', autocomplete: 'current-password' });
    const merken = el('input', { id: 'f-speichern', type: 'checkbox' });
    const los = () => { wert = { benutzer: b.value.trim(), passwort: p.value, speichern: merken.checked }; d.zu(); };
    const d = dialog({
      titel: 'Anmelden bei ' + n.ziel,
      inhalt: el('form', { onsubmit: (e) => { e.preventDefault(); los(); }, style: { display: 'flex', flexDirection: 'column', gap: '14px' } },
        el('div', { class: 'feld' }, el('label', { for: 'f-b' }, 'Benutzer'), b),
        el('div', { class: 'feld' }, el('label', { for: 'f-p' }, 'Passwort'), p),
        el('label', { class: 'check', for: 'f-speichern' }, merken,
          el('span', null, 'Zugangsdaten für diesen Host speichern', el('br'),
            el('span', { style: { color: 'var(--leise)', fontSize: '13px' } }, 'Verschlüsselt im Tresor — beim nächsten Mal ohne Rückfrage.'))),
        el('button', { type: 'submit', class: 'versteckt' })),
      knoepfe: [el('button', { class: 'btn', type: 'button', onclick: () => d.zu() }, 'Abbrechen'), el('button', { class: 'btn primaer', type: 'button', onclick: los }, 'Verbinden')],
      beimSchliessen: () => { if (tab && tab.rueckfrage === d) tab.rueckfrage = null; resolve(wert); },
    });
    if (tab) tab.rueckfrage = d;
    setTimeout(() => (n.benutzer ? p : b).focus(), 40);
  });
};

// ---------- Befehle einfuegen ----------

function aktiverHost() { return App.aktiv && App.daten.hosts.find((h) => h.id === App.aktiv.hostId); }
function istKritisch(h) { return h && (h.tags || []).some((t) => t.toLowerCase() === 'kritisch'); }

// opt.zeileErsetzen: die schon getippte Eingabe ersetzen (Beispiel aus der Hilfe zur aktuellen Zeile),
// statt den Befehl an «tar» anzuhaengen («tartar -xf …»).
App.befehlEinfuegen = async function (text, opt) {
  const o = opt || {};
  const t = App.aktiv;
  if (!t || t.zustand !== 'bereit') { toast('Keine verbundene Sitzung', 'fehler'); return; }
  const h = aktiverHost();
  if (istGefaehrlich(text) && istKritisch(h)) {
    if (!(await frage(`«${h.name}» ist als kritisch markiert, und der Befehl kann Daten löschen oder den Host anhalten:`, { titel: 'Gefährlicher Befehl', ja: 'Trotzdem einfügen', gefahr: true, befehl: text }))) return;
    if (App.aktiv !== t || t.zustand !== 'bereit') return; // inzwischen gewechselt oder getrennt
  }
  App.blaetterZu();
  if (o.zeileErsetzen && t.aktuelleZeile()) t.tippen('\x05\x15'); // ans Zeilenende, dann die Zeile leeren (readline)
  t.einfuegen(text);
};

App.befehlVorbereiten = function (befehl, titel, opt) {
  const namen = platzhalter(optionenAufloesen(befehl));
  if (!namen.length) { App.befehlEinfuegen(optionenAufloesen(befehl), opt); return; }
  const werte = {};
  const vorschau = el('div', { class: 'befehl-gross' });
  const zeichnen = () => { ersetzen(vorschau, befehlAnzeige(befehl.replace(/\{\{(.+?)\}\}/g, (m, n) => (werte[n] ? werte[n] : m)))); };
  const los = () => { d.zu(); App.befehlEinfuegen(befehlFuellen(befehl, werte), opt); };
  const felder = namen.map((n, i) => {
    const inp = el('input', { id: 'pv-' + i, type: 'text', placeholder: n, autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false', oninput: () => { werte[n] = inp.value; zeichnen(); } });
    return el('div', { class: 'feld' }, el('label', { for: 'pv-' + i }, n), inp);
  });
  zeichnen();
  const d = dialog({
    titel: ohneCode(titel || 'Befehl'),
    inhalt: el('form', { onsubmit: (e) => { e.preventDefault(); los(); }, style: { display: 'flex', flexDirection: 'column', gap: '14px' } }, vorschau, felder, el('button', { type: 'submit', class: 'versteckt' })),
    knoepfe: [el('button', { class: 'btn', type: 'button', onclick: () => d.zu() }, 'Abbrechen'), el('button', { class: 'btn primaer', type: 'button', onclick: los }, icon('einfuegen'), 'Einfügen')],
  });
};

// Aus der Dateileiste: Pfad einfuegen bzw. mit ausfuehren=true gleich ausfuehren (cd …).
// Nur an diese Sitzung (nicht «An alle senden»): ein Pfad gehoert zu genau einem Host.
// Ausfuehren leert zuerst die Eingabezeile (Strg+E, Strg+U), damit nichts Halbgetipptes
// mitlaeuft («rm -rf » + «cd …»), und schickt Enter hinter der Klammer von Bracketed Paste.
App.inTerminal = function (text, ausfuehren) {
  const t = App.aktiv;
  if (!t || t.zustand !== 'bereit') { toast('Keine verbundene Sitzung', 'fehler'); return; }
  App.seiteUmschalten('dateien', false, true);
  if (!ausfuehren) { t.einfuegen(text, { nurHier: true }); return; }
  if (t.term.buffer.active.type === 'alternate') { toast('Im Terminal läuft ein Vollbild-Programm (z.B. nano oder less) — erst beenden', 'fehler'); return; }
  t.sendenRoh(kodierer.encode('\x05\x15' + t.geklammert(text.replace(/\r$/, '')) + '\r'));
  t.startMerken('\r');
  if (!App.istTelefon()) t.term.focus();
};

App.palette = function (start) {
  App.seiteUmschalten('links', false, true);
  App.paletteObj.oeffnen(start);
};

App.hilfeBeiEingabe = function (tab) {
  if (tab === App.aktiv && App.hilfe && App.seiteOffen('hilfe')) App.hilfe.zeile(tab.aktuelleZeile());
};

// ---------- An alle senden ----------

App.rundfunkSetzen = function (an) {
  const andere = App.offeneTabs().filter((t) => t.zustand === 'bereit');
  if (an && andere.length < 2) { toast('Dafür braucht es mindestens zwei verbundene Tabs', 'fehler'); return; }
  App.rundfunk = an;
  App.knoepfe.rundfunk.classList.toggle('an', an);
  App.rundfunkBand.classList.toggle('versteckt', !an);
  ersetzen(App.rundfunkBand, icon('funk'), el('span', { style: { flex: 1 } }, 'Eingaben gehen an alle ' + andere.length + ' verbundenen Tabs: ' + andere.map((t) => t.titel).join(', ')),
    el('button', { class: 'btn klein', type: 'button', onclick: () => App.rundfunkSetzen(false) }, 'Beenden'));
  setTimeout(() => App.aktiv && App.aktiv.anpassen(), 20);
};
App.rundfunkSenden = function (von, d) {
  // Einfuegen aus xterm kommt bei Bracketed Paste geklammert — jeder Ziel-Tab nach seinem eigenen Modus.
  const geklammert = d.startsWith('\x1b[200~') && d.endsWith('\x1b[201~');
  const roh = geklammert ? d.slice(6, -6) : d;
  for (const t of App.tabs) if (t !== von && t.zustand === 'bereit') t.sendenRoh(kodierer.encode(geklammert ? t.geklammert(roh) : d));
};
App.anAlleSenden = async function (text) {
  const ziele = App.tabs.filter((t) => t.zustand === 'bereit');
  const kritisch = ziele.map((t) => App.daten.hosts.find((h) => h.id === t.hostId)).filter(istKritisch);
  const gefahr = istGefaehrlich(text);
  let frageText = `Einfügen in ${ziele.length} Tabs (${ziele.map((t) => t.titel).join(', ')})?`;
  if (gefahr && kritisch.length) frageText = 'Gefährlicher Befehl auf kritischen Hosts (' + kritisch.map((h) => h.name).join(', ') + ')!\n\n' + frageText;
  if (!(await frage(frageText, { titel: 'An alle Tabs', ja: 'Einfügen', gefahr, befehl: text }))) return;
  // nurHier: jeder Tab genau einmal, auch wenn «An alle senden» gerade an ist.
  for (const t of ziele) if (t.zustand === 'bereit') t.einfuegen(text, { nurHier: true, ohneFokus: t !== App.aktiv });
};

// ---------- Tastenleiste fuers Telefon ----------

// Strg/Alt der Telefon-Leiste gelten fuer genau eine Eingabe. Pfeile, Pos1, Ende und Bild↑↓ bekommen
// die xterm-Form mit Modifikator (Strg+Links = ESC [1;5D), Zeichen werden zum Steuerzeichen.
const TASTEN_FOLGE = /^\x1b(?:\[|O)([ABCDHF])$|^\x1b\[(\d+)~$/;
App.modifikator = function (d) {
  const ctrl = App.mod.ctrl, alt = App.mod.alt;
  if (!ctrl && !alt) return d;
  App.modSetzen('ctrl', false);
  App.modSetzen('alt', false);
  const m = TASTEN_FOLGE.exec(d);
  if (m) {
    const n = 1 + (alt ? 2 : 0) + (ctrl ? 4 : 0);
    return m[1] ? '\x1b[1;' + n + m[1] : '\x1b[' + m[2] + ';' + n + '~';
  }
  if (ctrl && d.length) {
    const z = d[0], c = z.toUpperCase().charCodeAt(0);
    let s = z;
    if (c >= 64 && c <= 95) s = String.fromCharCode(c - 64);
    else if (z === ' ') s = '\x00';
    else if (z === '?') s = '\x7f';
    d = s + d.slice(1);
  }
  if (alt) d = '\x1b' + d;
  return d;
};
App.modSetzen = function (k, an) {
  App.mod[k] = an;
  const b = document.querySelector('.tasten [data-mod=' + k + ']');
  if (b) { b.classList.toggle('an', an); b.setAttribute('aria-pressed', an ? 'true' : 'false'); }
};

function telefonTasten() {
  const pfeil = (normal, app) => () => {
    const t = App.aktiv; if (!t) return;
    const seq = t.term.modes && t.term.modes.applicationCursorKeysMode ? app : normal;
    t.tippen(seq);
  };
  const taste = (beschr, aktion, extra) => el('button', Object.assign({ type: 'button', 'aria-label': (extra && extra.label) || beschr,
    onpointerdown: (e) => e.preventDefault(), // Tastatur bleibt offen
    onclick: () => { aktion(); } }, extra && extra.attrs), extra && extra.icon ? icon(extra.icon, 18) : beschr);
  const senden = (s) => () => App.aktiv && App.aktiv.tippen(s);
  return el('div', { class: 'tasten', role: 'toolbar', 'aria-label': 'Sondertasten' },
    taste('Esc', senden('\x1b')),
    taste('Tab', senden('\t')),
    taste('Ctrl', () => App.modSetzen('ctrl', !App.mod.ctrl), { attrs: { 'data-mod': 'ctrl', 'aria-pressed': 'false' } }),
    taste('Alt', () => App.modSetzen('alt', !App.mod.alt), { attrs: { 'data-mod': 'alt', 'aria-pressed': 'false' } }),
    taste('^C', senden('\x03'), { label: 'Strg C (abbrechen)' }),
    el('span', { class: 'trenn' }),
    taste('', pfeil('\x1b[D', '\x1bOD'), { icon: 'pfeilLinks', label: 'Pfeil links' }),
    taste('', pfeil('\x1b[A', '\x1bOA'), { icon: 'pfeilHoch', label: 'Pfeil hoch' }),
    taste('', pfeil('\x1b[B', '\x1bOB'), { icon: 'pfeilRunter', label: 'Pfeil runter' }),
    taste('', pfeil('\x1b[C', '\x1bOC'), { icon: 'pfeilRechts', label: 'Pfeil rechts' }),
    el('span', { class: 'trenn' }),
    taste('|', senden('|')), taste('~', senden('~')), taste('/', senden('/')), taste('-', senden('-')),
    taste('$', senden('$')), taste('&', senden('&')), taste('>', senden('>')),
    el('span', { class: 'trenn' }),
    taste('Pos1', senden('\x1b[H'), { label: 'Zeilenanfang' }), taste('Ende', senden('\x1b[F'), { label: 'Zeilenende' }),
    taste('Bild↑', senden('\x1b[5~'), { label: 'Seite hoch' }), taste('Bild↓', senden('\x1b[6~'), { label: 'Seite runter' }),
    taste('^D', senden('\x04'), { label: 'Strg D (Ende)' }), taste('^Z', senden('\x1a'), { label: 'Strg Z' }), taste('^R', senden('\x12'), { label: 'Strg R (Verlauf suchen)' }),
    el('span', { class: 'trenn' }),
    taste('', async () => {
      try { const t = await navigator.clipboard.readText(); App.aktiv && App.aktiv.einfuegen(t); }
      catch (e) { toast('Einfügen braucht HTTPS und die Erlaubnis des Browsers', 'fehler'); }
    }, { icon: 'einfuegen', label: 'Aus Zwischenablage einfügen' }),
    taste('', () => { const s = App.aktiv && App.aktiv.term.getSelection(); if (s) kopieren(s); else toast('Erst Text markieren (lange drücken)'); }, { icon: 'kopieren', label: 'Markierung kopieren' }),
    taste('', () => App.aktiv && App.aktiv.term.focus(), { icon: 'tastatur', label: 'Tastatur zeigen' }));
}

// ---------- globale Tasten und Lebenszeichen ----------

// Palette: Strg+Umschalt+K immer, Strg+K nur ausserhalb des Terminals (dort gehoert es der Shell:
// nano Ausschneiden, bash Zeilenrest loeschen). Cmd+K (Mac) immer.
document.addEventListener('keydown', (e) => {
  if (!(e.ctrlKey || e.metaKey) || e.altKey || e.key.toLowerCase() !== 'k') return;
  if (!App.paletteObj || !App.status.angemeldet || dialogStapel.length) return;
  const imTerminal = e.target && e.target.closest && e.target.closest('.xterm');
  if (imTerminal && e.ctrlKey && !e.shiftKey && !e.metaKey) return;
  e.preventDefault();
  App.palette();
});
document.addEventListener('visibilitychange', () => {
  if (document.visibilityState !== 'visible') return;
  // Telefon aus dem Hintergrund: sofort neu verbinden statt auf den Zeitgeber zu warten, und
  // scheinbar offene Verbindungen pruefen (nach einem Netzwechsel oft halb offen).
  for (const t of App.tabs) {
    if (t.zustand === 'getrennt') t.sofortNeu();
    else if (t.zustand === 'bereit') {
      if (!t.ws || t.ws.readyState > 1) { t.verbindungZu(0); t.sofortNeu(); } else t.lebenszeichen(4000);
    }
  }
  if (App.status.angemeldet) api('GET', '/api/status', undefined, { auto: true }).then((s) => { if (!s.angemeldet) App.abgemeldet(); }).catch(() => {});
});
// Ohne offene Sitzung meldet niemand die Abmeldung: einmal pro Minute nachfragen (verlaengert nichts).
setInterval(() => {
  if (!App.status.angemeldet || document.visibilityState !== 'visible') return;
  api('GET', '/api/status', undefined, { auto: true }).then((s) => { if (!s.angemeldet) App.abgemeldet(); }).catch(() => {});
}, 60000);
setInterval(() => { if (App.dateien && App.dateien.folgen && App.seiteOffen('dateien') && document.visibilityState === 'visible') App.dateien.pfadPruefen(false); }, 2000);

App.starten();
