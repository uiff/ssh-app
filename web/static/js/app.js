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
const ZUSTAENDE = ['verbinde', 'bereit', 'getrennt', 'fehler', 'beendet', 'abgeloest'];
App.zustandText = (z) => (ZUSTAENDE.includes(z) ? t('app.zustand.' + z) : z);

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
  for (const tt of App.tabs) { tt.term.options.fontSize = n; tt.anpassen(); }
};

function logoLink(hoehe) {
  return el('a', { class: 'logo-link', href: IOTUELI, target: '_blank', rel: 'noopener', title: 'IoTUeli — iotueli.ch' },
    el('img', { src: 'img/iotueli.svg', alt: 'IoTUeli', style: { height: hoehe + 'px' } }));
}
function erstelltVon() {
  return el('span', null, tTeile('app.erstelltVon', { marke: el('a', { href: IOTUELI, target: '_blank', rel: 'noopener' }, 'IoTUeli') }));
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
  for (const tt of App.tabs) tt.schliessen();
  App.tabs = [];
  App.daten = { ordner: [], hosts: [], zugaenge: [], snippets: [], bekannt: [] };
  App.rundfunk = false;
  App.seitenleiste = null; App.dateien = null; App.hilfe = null; App.start = null; App.tabsLeiste = null; App.blattEbene = null;
  App.modSetzen('ctrl', false); App.modSetzen('alt', false);
  try { App.status = await api('GET', '/api/status'); }
  catch (e) {
    ersetzen(document.getElementById('app'), el('div', { class: 'einstieg' }, el('div', { class: 'einstieg-form' },
      el('h2', null, t('app.keineVerbindung')), el('p', { class: 'unter' }, e.message),
      el('button', { class: 'btn primaer', type: 'button', onclick: App.starten }, t('app.erneut')))));
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
  toast(t('app.abgemeldet'));
  App.starten();
};

// mitSprache: Umschalter «Deutsch / English» oben rechts (nicht auf der Seite mit dem
// Sicherheitsschluessel: Er wird nur einmal angezeigt, ein Neuladen wuerde ihn verlieren).
function einstiegRahmen(formular, mitSprache) {
  return el('div', { class: 'einstieg' },
    mitSprache ? App.sprachUmschalter('oben') : null,
    el('div', { class: 'einstieg-bild' },
      logoLink(40),
      el('div', { class: 'platz', style: { flex: 1 } }),
      el('div', { class: 'term-deko', 'aria-hidden': 'true' }, t('app.einstieg.deko')),
      el('h1', null, t('app.einstieg.titel')),
      el('p', null, t('app.einstieg.text')),
      el('div', { class: 'platz', style: { flex: 1 } }),
      el('div', { class: 'fuss' }, erstelltVon())),
    el('div', { class: 'einstieg-form' }, formular));
}

function formFeld(id, label, typ, extra) {
  const inp = el('input', Object.assign({ id, type: typ || 'text', autocapitalize: 'off', spellcheck: 'false' }, extra || {}));
  return [inp, el('div', { class: 'feld' }, el('label', { for: id }, label), inp)];
}

function einrichtenSeite() {
  const [b, bFeld] = formFeld('s-benutzer', t('app.feld.benutzer'), 'text', { autocomplete: 'username', value: 'admin' });
  const [p1, p1Feld] = formFeld('s-pw', t('app.feld.passwort'), 'password', { autocomplete: 'new-password' });
  const [p2, p2Feld] = formFeld('s-pw2', t('app.feld.passwort2'), 'password', { autocomplete: 'new-password' });
  const meldung = el('div');
  const knopf = el('button', { class: 'btn primaer voll', type: 'submit' }, t('app.einrichten.knopf'));
  const form = el('form', { style: { display: 'flex', flexDirection: 'column', gap: '18px' }, onsubmit: async (e) => {
    e.preventDefault();
    ersetzen(meldung);
    if (p1.value !== p2.value) { meldung.append(el('div', { class: 'meldung fehler' }, t('app.pwUngleich'))); return; }
    knopf.disabled = true; knopf.textContent = t('app.einrichten.laeuft');
    try {
      const r = await api('POST', '/api/einrichten', { benutzer: b.value.trim(), passwort: p1.value });
      lokalSetzen('benutzer', b.value.trim());
      sicherheitsschluesselSeite(r.sicherheitsschluessel, b.value.trim());
    } catch (err) {
      meldung.append(el('div', { class: 'meldung fehler' }, err.message));
      knopf.disabled = false; knopf.textContent = t('app.einrichten.knopf');
    }
  } },
    el('h2', null, t('app.einrichten.titel')),
    el('p', { class: 'unter' }, t('app.einrichten.text')),
    bFeld, p1Feld, el('div', { class: 'hinweis', style: { fontSize: '12.5px', color: 'var(--leise)', marginTop: '-10px' } }, t('app.einrichten.minPw', { n: App.status.minPasswort })), p2Feld,
    meldung, knopf,
    el('div', { class: 'reihe', style: { justifyContent: 'space-between' } },
      el('span', { style: { color: 'var(--leise)', fontSize: '14px' } }, t('app.einrichten.umzug')),
      el('button', { class: 'textlink', type: 'button', onclick: () => App.backupEinspielen(false) }, t('app.einrichten.einspielen'))));
  ersetzen(document.getElementById('app'), einstiegRahmen(form, true));
  if (!App.istTelefon()) p1.focus();
}

function sicherheitsschluesselSeite(schluessel, benutzer) {
  const ok = el('input', { type: 'checkbox', onchange: () => { weiter.disabled = !ok.checked; } });
  const weiter = el('button', { class: 'btn primaer voll', type: 'button', disabled: true, onclick: () => App.starten() }, t('app.schluessel.weiter'));
  const datei = t('app.schluessel.datei', { benutzer, zeit: new Date().toLocaleString(lokalCode()), adresse: location.origin, schluessel });
  const inhalt = el('div', { style: { display: 'flex', flexDirection: 'column', gap: '18px' } },
    el('h2', null, t('app.schluessel.titel')),
    el('p', { class: 'unter' }, t('app.schluessel.text')),
    el('div', { class: 'schluessel-box', 'aria-label': t('app.schluessel.name') }, schluessel),
    el('div', { class: 'reihe' },
      el('button', { class: 'btn', type: 'button', onclick: () => kopieren(schluessel) }, icon('kopieren'), t('app.schluessel.kopieren')),
      el('button', { class: 'btn', type: 'button', onclick: () => textDatei(t('app.schluessel.dateiname'), datei) }, icon('runter'), t('app.schluessel.speichern'))),
    el('div', { class: 'meldung warn' }, icon('warnung'), el('span', null, t('app.schluessel.warn'))),
    el('label', { class: 'check' }, ok, el('span', null, t('app.schluessel.ok'))),
    weiter);
  ersetzen(document.getElementById('app'), einstiegRahmen(inhalt));
}

function anmeldeSeite() {
  const [b, bFeld] = formFeld('a-benutzer', t('app.feld.benutzer'), 'text', { autocomplete: 'username', value: lokal('benutzer', '') });
  const [p, pFeld] = formFeld('a-pw', t('app.feld.passwort'), 'password', { autocomplete: 'current-password' });
  const meldung = el('div');
  const knopf = el('button', { class: 'btn primaer voll', type: 'submit' }, t('app.anmelden.knopf'));
  const form = el('form', { style: { display: 'flex', flexDirection: 'column', gap: '18px' }, onsubmit: async (e) => {
    e.preventDefault();
    ersetzen(meldung);
    knopf.disabled = true; knopf.textContent = t('app.anmelden.laeuft');
    try {
      await api('POST', '/api/anmelden', { benutzer: b.value.trim(), passwort: p.value });
      lokalSetzen('benutzer', b.value.trim());
      App.starten();
    } catch (err) {
      meldung.append(el('div', { class: 'meldung fehler' }, err.message));
      knopf.disabled = false; knopf.textContent = t('app.anmelden.knopf');
      p.select();
    }
  } },
    el('h2', null, t('app.anmelden.titel')),
    el('p', { class: 'unter' }, t('app.anmelden.text')),
    bFeld, pFeld, meldung, knopf,
    el('button', { class: 'textlink', type: 'button', onclick: zuruecksetzenSeite }, t('app.anmelden.vergessen')),
    el('p', { style: { margin: 0, fontSize: '13px', color: 'var(--leise)', lineHeight: 1.5 } }, t('app.anmelden.sperre')));
  ersetzen(document.getElementById('app'), einstiegRahmen(form, true));
  if (!App.istTelefon()) (b.value ? p : b).focus();
}

function zuruecksetzenSeite() {
  const k = el('textarea', { id: 'r-key', spellcheck: 'false', autocapitalize: 'characters', style: { minHeight: '84px' }, placeholder: 'ABCD-EFGH-…' });
  const [b, bFeld] = formFeld('r-benutzer', t('app.zurueck.benutzer'), 'text', { autocomplete: 'username', value: lokal('benutzer', '') });
  const [p1, p1Feld] = formFeld('r-pw', t('app.zurueck.pw'), 'password', { autocomplete: 'new-password' });
  const [p2, p2Feld] = formFeld('r-pw2', t('app.zurueck.pw2'), 'password', { autocomplete: 'new-password' });
  const meldung = el('div');
  const form = el('form', { style: { display: 'flex', flexDirection: 'column', gap: '18px' }, onsubmit: async (e) => {
    e.preventDefault();
    ersetzen(meldung);
    if (p1.value !== p2.value) { meldung.append(el('div', { class: 'meldung fehler' }, t('app.pwUngleich'))); return; }
    try {
      await api('POST', '/api/zuruecksetzen', { schluessel: k.value, benutzer: b.value.trim(), passwort: p1.value });
      lokalSetzen('benutzer', b.value.trim());
      toast(t('app.zurueck.ok'));
      App.starten();
    } catch (err) { meldung.append(el('div', { class: 'meldung fehler' }, err.message)); }
  } },
    el('h2', null, t('app.zurueck.titel')),
    el('p', { class: 'unter' }, t('app.zurueck.text')),
    el('div', { class: 'feld' }, el('label', { for: 'r-key' }, t('app.schluessel.name')), k),
    bFeld, p1Feld, p2Feld, meldung,
    el('button', { class: 'btn primaer voll', type: 'submit' }, t('app.zurueck.knopf')),
    el('button', { class: 'textlink', type: 'button', onclick: anmeldeSeite }, t('app.zurueck.zurAnmeldung')));
  ersetzen(document.getElementById('app'), einstiegRahmen(form, true));
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
    dateien: ikonKnopf('ordner', t('app.kopf.dateien'), () => App.seiteUmschalten('dateien')),
    hilfe: ikonKnopf('hilfe', t('app.kopf.hilfe'), () => App.seiteUmschalten('hilfe')),
    rundfunk: ikonKnopf('funk', t('app.kopf.rundfunk'), () => App.rundfunkSetzen(!App.rundfunk), { class: 'ikon nur-desktop' }),
  };

  const kopf = el('header', { class: 'kopf' },
    ikonKnopf('menue', t('app.kopf.hosts'), () => App.seiteUmschalten('links'), { class: 'ikon nur-telefon' }),
    el('div', { class: 'marke nur-desktop' }, logoLink(24), el('span', { class: 'trenner' }), el('span', { class: 'name' }, 'SSH-App')),
    App.tabWahl,
    el('button', { class: 'btn klein primaer nur-desktop', type: 'button', style: { marginLeft: '16px' }, onclick: () => App.palette('') }, icon('plus'), t('app.kopf.neu')),
    el('div', { class: 'platz nur-desktop' }),
    el('button', { class: 'suche nur-desktop', type: 'button', onclick: () => App.palette() }, icon('suche', 15), el('span', null, t('app.kopf.suchen')), el('kbd', null, t('app.kopf.suchenTaste'))),
    App.knoepfe.rundfunk, App.knoepfe.dateien, App.knoepfe.hilfe,
    el('span', { class: 'tresor-chip nur-desktop', title: t('app.kopf.tresorTitel', { min: App.status.leerlaufMin }) }, icon('schloss', 14), t('app.kopf.tresor')),
    ikonKnopf('mehr', t('app.kopf.menue'), (e) => hauptMenue(e.currentTarget)));

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
  for (const tt of App.tabs) App.terms.append(tt.element);
  App.aktivieren(App.tabs[App.tabs.length - 1] || null);
  if (App.tabs.length) toast(App.tabs.length === 1 ? t('app.wiederAngehaengt1') : t('app.wiederAngehaengt', { n: App.tabs.length }));
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
  // Sprache: Der Eintrag nennt die andere Sprache in ihrer eigenen Schreibweise (so findet ihn auch,
  // wer die aktuelle nicht liest), darunter klein «Sprache wechseln».
  const andere = SPRACHEN.find((s) => s.id !== I18N.sprache) || SPRACHEN[0];
  menue(anker, [
    App.istTelefon() ? { text: t('app.menue.suchen'), icon: 'suche', aktion: () => App.palette('') } : null,
    App.istTelefon() && App.offeneTabs().length > 1 ? { text: App.rundfunk ? t('app.menue.rundfunkAus') : t('app.menue.rundfunkAn'), icon: 'funk', aktion: () => App.rundfunkSetzen(!App.rundfunk) } : null,
    App.istTelefon() ? { text: t('app.menue.schriftGroesser'), icon: 'schrift', aktion: () => App.schriftSetzen(App.schriftGroesse() + 1) } : null,
    App.istTelefon() ? { text: t('app.menue.schriftKleiner'), icon: 'schrift', aktion: () => App.schriftSetzen(App.schriftGroesse() - 1) } : null,
    { text: t('app.menue.einstellungen'), icon: 'zahnrad', aktion: () => App.einstellungen() },
    { text: el('span', { class: 'menue-text' }, el('span', { lang: andere.id }, andere.name), el('small', null, t('basis.spracheWechseln'))),
      icon: 'sprache', aktion: () => App.spracheWechseln(andere.id) },
    { text: t('app.menue.abmelden'), icon: 'abmelden', aktion: abmelden },
    App.offeneTabs().length ? { text: t('app.menue.alleBeenden'), icon: 'abmelden', gefahr: true, aktion: async () => {
      if (!(await frage(t('app.menue.alleBeendenFrage'), { ja: t('app.beenden'), gefahr: true }))) return;
      for (const tt of App.tabs) if (tt.sitzungId) await api('DELETE', '/api/sitzungen/' + tt.sitzungId).catch(() => {});
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

App.offeneTabs = () => App.tabs.filter((tt) => tt.zustand !== 'beendet' && tt.zustand !== 'fehler');

App.verbinden = function (host) {
  const tt = new App.TerminalTab({ hostId: host.id, titel: host.name });
  App.tabs.push(tt);
  App.terms.append(tt.element);
  App.aktivieren(tt);
};

App.neuVerbinden = function (tab) {
  const host = App.daten.hosts.find((h) => h.id === tab.hostId);
  App.tabEntfernen(tab);
  if (host) App.verbinden(host);
};

App.aktivieren = function (tab) {
  App.aktiv = tab;
  for (const tt of App.tabs) tt.element.style.display = tt === tab ? '' : 'none';
  App.start.classList.toggle('versteckt', !!tab);
  if (!tab) startZeichnen();
  App.tabsZeichnen();
  if (tab) {
    // Nur in das Terminal, wenn nichts anderes den Fokus braucht: Eine Rueckfrage (Host-Schluessel,
    // Anmeldung) kann bei nahen Hosts schon vor Ablauf der 20 ms offen sein.
    setTimeout(() => {
      tab.anpassen(true);
      if (!App.istTelefon() && App.aktiv === tab && !dialogStapel.length && !menue.offen && !(App.paletteObj && App.paletteObj.offen)) tab.term.focus();
    }, 20);
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
    const text = t(tab.zustand === 'abgeloest' ? 'app.tab.beendenFrageAnderes' : 'app.tab.beendenFrage', { titel: tab.titel });
    if (!(await frage(text, { ja: t('app.beenden'), gefahr: true }))) return;
  }
  if (tab.sitzungId) api('DELETE', '/api/sitzungen/' + tab.sitzungId).catch(() => {});
  App.tabEntfernen(tab);
};

function tabHost(tt) { return App.daten.hosts.find((x) => x.id === tt.hostId); }

App.tabsZeichnen = function () {
  if (!App.tabsLeiste) return;
  ersetzen(App.tabsLeiste,
    ...App.tabs.map((tt) => el('div', { class: 'tab' + (tt === App.aktiv ? ' aktiv' : ''), role: 'tab', 'aria-selected': tt === App.aktiv ? 'true' : 'false', title: tt.titel + ' — ' + App.zustandText(tt.zustand) },
      el('button', { class: 'tab-knopf', type: 'button', onclick: () => App.aktivieren(tt), onauxclick: (e) => { if (e.button === 1) App.tabSchliessen(tt); } },
        hostSymbol(tabHost(tt), 16, tt.zustand), el('span', { class: 't' }, tt.titel)),
      el('button', { class: 'zu', type: 'button', 'aria-label': t('app.tab.schliessen'), title: t('app.tab.schliessen'), onclick: () => App.tabSchliessen(tt) }, icon('x', 14)))),
    ikonKnopf('plus', t('app.kopf.neu'), () => App.palette(''), { class: 'ikon tab-neu' }));
  // Telefon: Titel des aktiven Tabs als Auswahl
  const tt = App.aktiv;
  ersetzen(App.tabWahl,
    tt ? hostSymbol(tabHost(tt), 18, tt.zustand) : null,
    el('span', { class: 't' }, tt ? tt.titel : 'SSH-App'),
    tt && tt.zustand !== 'bereit' ? el('span', { class: 'sr' }, App.zustandText(tt.zustand)) : null,
    App.tabs.length > 1 ? el('span', { class: 'chip' }, String(App.tabs.length)) : null,
    icon('pfeilRunter', 14));
  // Statusleiste (Desktop)
  if (tt) {
    const h = tabHost(tt);
    ersetzen(App.statusleiste,
      el('span', { class: 'zustand z-' + tt.zustand }, el('span', { class: 'hsym-punkt', 'aria-hidden': 'true' }), App.zustandText(tt.zustand)),
      h ? el('span', { class: 'geraet' }, geraetIcon(h.geraet, 14), geraetName(h.geraet)) : null,
      h ? el('span', null, hostZeile(h)) : null,
      tt.fingerprint ? el('span', null, tt.fingerprint) : null,
      el('span', null, tt.term.cols + ' × ' + tt.term.rows));
  } else ersetzen(App.statusleiste, el('span', null, t('app.keineSitzung')));
  // Punkte der verbundenen Hosts nachfuehren, ohne ein Filterfeld beim Tippen zu stoeren.
  if (App.seitenleiste && App.seitenleiste.reiter === 'hosts' && !App.seitenleiste.element.contains(document.activeElement)) App.seitenleiste.zeichnen();
};

function tabMenue(anker) {
  if (!App.tabs.length) { App.palette(''); return; }
  menue(anker, [
    ...App.tabs.map((tt) => ({ text: el('span', { class: 'menue-text' }, el('span', null, tt.titel), tt.zustand !== 'bereit' ? el('small', null, App.zustandText(tt.zustand)) : null),
      symbol: hostSymbol(tabHost(tt), 18, tt.zustand), aktiv: tt === App.aktiv, aktion: () => App.aktivieren(tt) })),
    { text: t('app.kopf.neu'), icon: 'plus', aktion: () => App.palette('') },
    App.aktiv ? { text: t('app.tab.diesenSchliessen'), icon: 'x', gefahr: true, aktion: () => App.tabSchliessen(App.aktiv) } : null,
  ]);
}

function startZeichnen() {
  const d = App.daten;
  const zuletzt = lokal('zuletzt', []);
  const sortiert = [...d.hosts].sort((a, b) => {
    const ia = zuletzt.indexOf(a.id), ib = zuletzt.indexOf(b.id);
    return (ia < 0 ? 999 : ia) - (ib < 0 ? 999 : ib) || a.name.localeCompare(b.name, lokalCode());
  });
  ersetzen(App.start, 
    el('h2', null, d.hosts.length ? t('app.start.verbinden') : t('app.start.willkommen')),
    d.hosts.length ? el('div', { class: 'karten' }, sortiert.map((h) => el('button', { class: 'karte', type: 'button', title: geraetName(h.geraet), onclick: () => App.verbinden(h) },
      hostSymbol(h, 20),
      el('span', { class: 'haupt' }, el('b', null, h.name), el('small', null, hostZeile(h))),
      icon('weiter')))) :
      el('p', { style: { margin: 0, color: 'var(--text2)', lineHeight: 1.6, maxWidth: '560px' } }, t('app.start.leer')),
    el('div', { class: 'reihe' },
      el('button', { class: 'btn primaer', type: 'button', onclick: () => App.hostBearbeiten({}) }, icon('plus'), t('app.start.hostAnlegen')),
      el('button', { class: 'btn', type: 'button', onclick: () => App.zugangBearbeiten({}) }, icon('schluessel'), t('app.start.zugangAnlegen')),
      el('button', { class: 'btn', type: 'button', onclick: () => App.palette() }, icon('hilfe'), t('app.start.hilfe'))),
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
      titel: geaendert ? t('app.hostkey.titelGeaendert') : t('app.hostkey.titelNeu'),
      inhalt: el('div', { style: { display: 'flex', flexDirection: 'column', gap: '14px' } },
        geaendert ? el('div', { class: 'meldung fehler' }, icon('warnung'), el('span', null, el('b', null, t('app.hostkey.vorsicht')), ' ', t('app.hostkey.geaendert'))) :
          el('p', { style: { margin: 0, lineHeight: 1.55, color: 'var(--text2)' } },
            tTeile('app.hostkey.erste', { ziel: el('b', { class: 'mono' }, n.ziel), befehl: el('code', null, 'ssh-keygen -lf /etc/ssh/ssh_host_*_key.pub') })),
        geaendert && n.altFp ? el('div', { class: 'feld' }, el('span', { class: 'label' }, t('app.hostkey.bisher')), el('div', { class: 'fp' }, n.altFp)) : null,
        el('div', { class: 'feld' }, el('span', { class: 'label' }, (geaendert ? t('app.hostkey.jetzt') : t('app.hostkey.fingerabdruck')) + ' · ' + n.typ), el('div', { class: 'fp' }, n.fp))),
      knoepfe: [
        el('button', { class: 'btn', type: 'button', onclick: () => d.zu() }, t('basis.abbrechen')),
        el('button', { class: 'btn ' + (geaendert ? 'gefahr' : 'primaer'), type: 'button', onclick: () => { ok = true; d.zu(); } }, geaendert ? t('app.hostkey.annehmen') : t('app.hostkey.vertrauen')),
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
      titel: t('app.login.titel', { ziel: n.ziel }),
      inhalt: el('form', { onsubmit: (e) => { e.preventDefault(); los(); }, style: { display: 'flex', flexDirection: 'column', gap: '14px' } },
        el('div', { class: 'feld' }, el('label', { for: 'f-b' }, t('app.login.benutzer')), b),
        el('div', { class: 'feld' }, el('label', { for: 'f-p' }, t('app.login.passwort')), p),
        el('label', { class: 'check', for: 'f-speichern' }, merken,
          el('span', null, t('app.login.speichern'), el('br'),
            el('span', { style: { color: 'var(--leise)', fontSize: '13px' } }, t('app.login.speichernHinweis')))),
        el('button', { type: 'submit', class: 'versteckt' })),
      knoepfe: [el('button', { class: 'btn', type: 'button', onclick: () => d.zu() }, t('basis.abbrechen')), el('button', { class: 'btn primaer', type: 'button', onclick: los }, t('app.login.verbinden'))],
      beimSchliessen: () => { if (tab && tab.rueckfrage === d) tab.rueckfrage = null; resolve(wert); },
    });
    if (tab) tab.rueckfrage = d;
    setTimeout(() => (n.benutzer ? p : b).focus(), 40);
  });
};

// ---------- Befehle einfuegen ----------

function aktiverHost() { return App.aktiv && App.daten.hosts.find((h) => h.id === App.aktiv.hostId); }
// Tag «kritisch» (deutsch) oder «critical» (englisch): Rueckfrage vor gefaehrlichen Befehlen.
const KRITISCH_TAGS = ['kritisch', 'critical'];
function istKritisch(h) { return !!h && (h.tags || []).some((tt) => KRITISCH_TAGS.includes(String(tt).trim().toLowerCase())); }

// opt.zeileErsetzen: die schon getippte Eingabe ersetzen (Beispiel aus der Hilfe zur aktuellen Zeile),
// statt den Befehl an «tar» anzuhaengen («tartar -xf …»).
App.befehlEinfuegen = async function (text, opt) {
  const o = opt || {};
  const tt = App.aktiv;
  if (!tt || tt.zustand !== 'bereit') { toast(t('app.keineSitzungVerbunden'), 'fehler'); return; }
  const h = aktiverHost();
  if (istGefaehrlich(text) && istKritisch(h)) {
    if (!(await frage(t('app.gefahr.frage', { name: h.name }), { titel: t('app.gefahr.titel'), ja: t('app.gefahr.trotzdem'), gefahr: true, befehl: text }))) return;
    if (App.aktiv !== tt || tt.zustand !== 'bereit') return; // inzwischen gewechselt oder getrennt
  }
  App.blaetterZu();
  if (o.zeileErsetzen && tt.aktuelleZeile()) tt.tippen('\x05\x15'); // ans Zeilenende, dann die Zeile leeren (readline)
  tt.einfuegen(text);
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
    titel: ohneCode(titel || t('app.befehl.titel')),
    inhalt: el('form', { onsubmit: (e) => { e.preventDefault(); los(); }, style: { display: 'flex', flexDirection: 'column', gap: '14px' } }, vorschau, felder, el('button', { type: 'submit', class: 'versteckt' })),
    knoepfe: [el('button', { class: 'btn', type: 'button', onclick: () => d.zu() }, t('basis.abbrechen')), el('button', { class: 'btn primaer', type: 'button', onclick: los }, icon('einfuegen'), t('app.befehl.einfuegen'))],
  });
};

// Aus der Dateileiste: Pfad einfuegen bzw. mit ausfuehren=true gleich ausfuehren (cd …).
// Nur an diese Sitzung (nicht «An alle senden»): ein Pfad gehoert zu genau einem Host.
// Ausfuehren leert zuerst die Eingabezeile (Strg+E, Strg+U), damit nichts Halbgetipptes
// mitlaeuft («rm -rf » + «cd …»), und schickt Enter hinter der Klammer von Bracketed Paste.
App.inTerminal = function (text, ausfuehren) {
  const tt = App.aktiv;
  if (!tt || tt.zustand !== 'bereit') { toast(t('app.keineSitzungVerbunden'), 'fehler'); return; }
  App.seiteUmschalten('dateien', false, true);
  if (!ausfuehren) { tt.einfuegen(text, { nurHier: true }); return; }
  if (tt.term.buffer.active.type === 'alternate') { toast(t('app.vollbild'), 'fehler'); return; }
  tt.sendenRoh(kodierer.encode('\x05\x15' + tt.geklammert(text.replace(/\r$/, '')) + '\r'));
  tt.startMerken('\r');
  if (!App.istTelefon()) tt.term.focus();
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
  const andere = App.offeneTabs().filter((tt) => tt.zustand === 'bereit');
  if (an && andere.length < 2) { toast(t('app.rundfunk.zuWenig'), 'fehler'); return; }
  App.rundfunk = an;
  App.knoepfe.rundfunk.classList.toggle('an', an);
  App.rundfunkBand.classList.toggle('versteckt', !an);
  ersetzen(App.rundfunkBand, icon('funk'), el('span', { style: { flex: 1 } }, t('app.rundfunk.band', { n: andere.length, namen: andere.map((tt) => tt.titel).join(', ') })),
    el('button', { class: 'btn klein', type: 'button', onclick: () => App.rundfunkSetzen(false) }, t('app.rundfunk.beenden')));
  setTimeout(() => App.aktiv && App.aktiv.anpassen(), 20);
};
App.rundfunkSenden = function (von, d) {
  // Einfuegen aus xterm kommt bei Bracketed Paste geklammert — jeder Ziel-Tab nach seinem eigenen Modus.
  const geklammert = d.startsWith('\x1b[200~') && d.endsWith('\x1b[201~');
  const roh = geklammert ? d.slice(6, -6) : d;
  for (const tt of App.tabs) if (tt !== von && tt.zustand === 'bereit') tt.sendenRoh(kodierer.encode(geklammert ? tt.geklammert(roh) : d));
};
App.anAlleSenden = async function (text) {
  const ziele = App.tabs.filter((tt) => tt.zustand === 'bereit');
  const kritisch = ziele.map((tt) => App.daten.hosts.find((h) => h.id === tt.hostId)).filter(istKritisch);
  const gefahr = istGefaehrlich(text);
  let frageText = t('app.rundfunk.frage', { n: ziele.length, namen: ziele.map((tt) => tt.titel).join(', ') });
  if (gefahr && kritisch.length) frageText = t('app.rundfunk.gefahr', { namen: kritisch.map((h) => h.name).join(', ') }) + '\n\n' + frageText;
  if (!(await frage(frageText, { titel: t('app.rundfunk.titel'), ja: t('app.befehl.einfuegen'), gefahr, befehl: text }))) return;
  // nurHier: jeder Tab genau einmal, auch wenn «An alle senden» gerade an ist.
  for (const tt of ziele) if (tt.zustand === 'bereit') tt.einfuegen(text, { nurHier: true, ohneFokus: tt !== App.aktiv });
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
    const tt = App.aktiv; if (!tt) return;
    const seq = tt.term.modes && tt.term.modes.applicationCursorKeysMode ? app : normal;
    tt.tippen(seq);
  };
  const taste = (beschr, aktion, extra) => el('button', Object.assign({ type: 'button', 'aria-label': (extra && extra.label) || beschr,
    onpointerdown: (e) => e.preventDefault(), // Tastatur bleibt offen
    onclick: () => { aktion(); } }, extra && extra.attrs), extra && extra.icon ? icon(extra.icon, 18) : beschr);
  const senden = (s) => () => App.aktiv && App.aktiv.tippen(s);
  return el('div', { class: 'tasten', role: 'toolbar', 'aria-label': t('app.tasten.name') },
    taste('Esc', senden('\x1b')),
    taste('Tab', senden('\t')),
    taste('Ctrl', () => App.modSetzen('ctrl', !App.mod.ctrl), { attrs: { 'data-mod': 'ctrl', 'aria-pressed': 'false' } }),
    taste('Alt', () => App.modSetzen('alt', !App.mod.alt), { attrs: { 'data-mod': 'alt', 'aria-pressed': 'false' } }),
    taste('^C', senden('\x03'), { label: t('app.tasten.strgC') }),
    el('span', { class: 'trenn' }),
    taste('', pfeil('\x1b[D', '\x1bOD'), { icon: 'pfeilLinks', label: t('app.tasten.links') }),
    taste('', pfeil('\x1b[A', '\x1bOA'), { icon: 'pfeilHoch', label: t('app.tasten.hoch') }),
    taste('', pfeil('\x1b[B', '\x1bOB'), { icon: 'pfeilRunter', label: t('app.tasten.runter') }),
    taste('', pfeil('\x1b[C', '\x1bOC'), { icon: 'pfeilRechts', label: t('app.tasten.rechts') }),
    el('span', { class: 'trenn' }),
    taste('|', senden('|')), taste('~', senden('~')), taste('/', senden('/')), taste('-', senden('-')),
    taste('$', senden('$')), taste('&', senden('&')), taste('>', senden('>')),
    el('span', { class: 'trenn' }),
    taste(t('app.tasten.pos1'), senden('\x1b[H'), { label: t('app.tasten.pos1Name') }), taste(t('app.tasten.ende'), senden('\x1b[F'), { label: t('app.tasten.endeName') }),
    taste(t('app.tasten.bildHoch'), senden('\x1b[5~'), { label: t('app.tasten.bildHochName') }), taste(t('app.tasten.bildRunter'), senden('\x1b[6~'), { label: t('app.tasten.bildRunterName') }),
    taste('^D', senden('\x04'), { label: t('app.tasten.strgD') }), taste('^Z', senden('\x1a'), { label: t('app.tasten.strgZ') }), taste('^R', senden('\x12'), { label: t('app.tasten.strgR') }),
    el('span', { class: 'trenn' }),
    taste('', async () => {
      try { const text = await navigator.clipboard.readText(); App.aktiv && App.aktiv.einfuegen(text); }
      catch (e) { toast(t('app.tasten.einfuegenFehler'), 'fehler'); }
    }, { icon: 'einfuegen', label: t('app.tasten.einfuegen') }),
    taste('', () => { const s = App.aktiv && App.aktiv.term.getSelection(); if (s) kopieren(s); else toast(t('app.tasten.erstMarkieren')); }, { icon: 'kopieren', label: t('app.tasten.kopieren') }),
    taste('', () => App.aktiv && App.aktiv.term.focus(), { icon: 'tastatur', label: t('app.tasten.tastatur') }));
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
  for (const tt of App.tabs) {
    if (tt.zustand === 'getrennt') tt.sofortNeu();
    else if (tt.zustand === 'bereit') {
      if (!tt.ws || tt.ws.readyState > 1) { tt.verbindungZu(0); tt.sofortNeu(); } else tt.lebenszeichen(4000);
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
