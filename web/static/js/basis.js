'use strict';
// Grundbausteine: DOM-Helfer, Symbole, API, Dialoge, Meldungen.

const App = {};
window.App = App;

function el(tag, attrs, ...kinder) {
  const e = document.createElement(tag);
  if (attrs) {
    for (const [k, v] of Object.entries(attrs)) {
      if (v === undefined || v === null || v === false) continue;
      if (k === 'class') e.className = v;
      else if (k === 'text') e.textContent = v;
      else if (k.startsWith('on') && typeof v === 'function') e.addEventListener(k.slice(2), v);
      else if (k === 'style' && typeof v === 'object') Object.assign(e.style, v);
      else if (v === true) e.setAttribute(k, '');
      else e.setAttribute(k, v);
    }
  }
  for (const k of kinder.flat(Infinity)) {
    if (k === null || k === undefined || k === false) continue;
    e.append(k instanceof Node ? k : document.createTextNode(String(k)));
  }
  return e;
}

// Symbole als Strich-SVG (keine Emoji).
const ICONS = {
  plus: 'M12 5v14M5 12h14',
  x: 'M6 6l12 12M18 6L6 18',
  menue: 'M4 7h16M4 12h16M4 17h16',
  suche: 'M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14zM20 20l-4-4',
  ordner: 'M3 7h7l2 2h9v10H3z',
  datei: 'M6 3h8l4 4v14H6zM14 3v4h4',
  link: 'M10 14a4 4 0 0 0 5.66 0l3-3a4 4 0 0 0-5.66-5.66l-1 1M14 10a4 4 0 0 0-5.66 0l-3 3a4 4 0 0 0 5.66 5.66l1-1',
  hoch: 'M12 16V4M7 9l5-5 5 5M4 20h16',
  runter: 'M12 4v12M7 11l5 5 5-5M4 20h16',
  ordnerPlus: 'M3 7h7l2 2h9v10H3zM12 12v5M9.5 14.5h5',
  zurueck: 'M15 18l-6-6 6-6',
  weiter: 'M9 18l6-6-6-6',
  aufwaerts: 'M12 19V5M5 12l7-7 7 7',
  neu: 'M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5',
  schloss: 'M5 11h14v10H5zM8 11V8a4 4 0 0 1 8 0v3',
  schluessel: 'M15 7a4 4 0 1 1-3.5 6L4 20.5V17h3v-3h3l1.5-1.5',
  passwort: 'M4 12h16M7 9v6M12 9v6M17 9v6',
  terminal: 'M3 5h18v14H3zM7 10l3 2-3 2M12 15h5',
  hilfe: 'M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20zM9.1 9a3 3 0 0 1 5.8 1c0 2-3 3-3 3M12 17h.01',
  stift: 'M4 20h4L19 9l-4-4L4 16zM14 6l4 4',
  muell: 'M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13',
  kopieren: 'M8 8h12v12H8zM4 16V4h12',
  einfuegen: 'M5 12h14M13 6l6 6-6 6',
  funk: 'M5 12a7 7 0 0 1 14 0M8.5 12a3.5 3.5 0 0 1 7 0M12 12v8',
  zahnrad: 'M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z',
  abmelden: 'M9 21H5V3h4M16 17l5-5-5-5M21 12H9',
  warnung: 'M12 3l10 18H2zM12 10v5M12 18v.5',
  pfeilHoch: 'M12 19V5M6 11l6-6 6 6',
  pfeilRunter: 'M12 5v14M6 13l6 6 6-6',
  pfeilLinks: 'M19 12H5M11 6l-6 6 6 6',
  pfeilRechts: 'M5 12h14M13 6l6 6-6 6',
  tastatur: 'M3 6h18v12H3zM7 10h.01M11 10h.01M15 10h.01M7 14h10',
  mehr: 'M12 6h.01M12 12h.01M12 18h.01',
  speichern: 'M5 3h11l3 3v15H5zM8 3v5h7M8 21v-7h8v7',
  schrift: 'M4 20l6-16h2l6 16M7 14h8',
  fenster: 'M3 5h18v14H3zM3 9h18M6 7h.01M9 7h.01',
  sprache: 'M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20zM2 12h20M12 2a15 15 0 0 1 0 20M12 2a15 15 0 0 0 0 20',
  // Spezialdateien in der Dateileiste: Pipe (Rohrstueck), Geraet (Chip), Socket (Stecker).
  pipe: 'M2 9h13v6H2zM15 7h3v10h-3zM18 12h4',
  geraet: 'M7 7h10v10H7zM10 4v3M14 4v3M10 17v3M14 17v3M4 10h3M4 14h3M17 10h3M17 14h3',
  socket: 'M9 2v5M15 2v5M6 7h12v4a6 6 0 0 1-12 0zM12 17v5',
  // Bildschirm-Tab (VNC): Bildschirm, Ansicht, Vollbild, Zwischenablage, Sondertasten, Trennen.
  bildschirm: 'M3 4h18v12H3zM8 20h8M12 16v4',
  anpassen: 'M3 5h18v14H3zM8 9H6v2M16 9h2v2M8 15H6v-2M16 15h2v-2',
  original: 'M3 5h18v14H3zM8 9v6M16 9v6M12 10.5v.01M12 13.5v.01',
  aufloesung: 'M3 4h18v12H3zM8 20h8M12 16v4M7 10h10M9 8l-2 2 2 2M15 8l2 2-2 2',
  verschieben: 'M12 3v18M3 12h18M9 6l3-3 3 3M9 18l3 3 3-3M6 9l-3 3 3 3M18 9l3 3-3 3',
  qualitaet: 'M4 7h9M17 7h3M4 17h3M11 17h9M15 5v4M9 15v4',
  auge: 'M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12zM12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z',
  vollbild: 'M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5',
  vollbildAus: 'M9 4v5H4M15 4v5h5M9 20v-5H4M15 20v-5h5',
  ablage: 'M9 3h6v3H9zM9 4.5H5V21h14V4.5h-4M9 11h6M9 15h4',
  // Sondertasten: eine Tastenkappe (neutral, nicht das Mac-Zeichen)
  sondertaste: 'M4 4h16v16H4zM7.5 7.5h9v7h-9zM9 17.5h6',
  lupePlus: 'M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14zM20 20l-4-4M8 11h6M11 8v6',
  lupeMinus: 'M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14zM20 20l-4-4M8 11h6',
  trennen: 'M9 15l-2 2a3 3 0 0 1-4.2-4.2l2-2M15 9l2-2a3 3 0 0 1 4.2 4.2l-2 2M8 3v3M3 8h3M16 21v-3M21 16h-3',
  chevronRunter: 'M6 9l6 6 6-6',
  chevronHoch: 'M6 15l6-6 6 6',
  // Zwei-Faktor-Anmeldung: Schild mit Haken (eingeschaltet), Telefon (App oeffnen), Uhr (Serverzeit).
  schild: 'M12 3l8 3v5c0 5-3.4 8.6-8 10-4.6-1.4-8-5-8-10V6zM8.5 12l2.5 2.5 4.5-5',
  telefon: 'M7 2h10v20H7zM11 18h2',
  uhr: 'M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20zM12 7v5l3 2',
};
function icon(name, groesse) {
  const ns = 'http://www.w3.org/2000/svg';
  const s = document.createElementNS(ns, 'svg');
  s.setAttribute('viewBox', '0 0 24 24');
  s.setAttribute('class', 'ic');
  s.setAttribute('aria-hidden', 'true');
  if (groesse) { s.style.width = groesse + 'px'; s.style.height = groesse + 'px'; }
  const p = document.createElementNS(ns, 'path');
  p.setAttribute('d', ICONS[name] || ICONS.datei);
  s.append(p);
  return s;
}
function ikonKnopf(name, label, onclick, extra) {
  return el('button', Object.assign({ class: 'ikon', type: 'button', 'aria-label': label, title: label, onclick }, extra || {}), icon(name));
}

// ---------- API ----------
// daten: der gelesene JSON-Rumpf der Fehlerantwort (z.B. { fehler, zweitfaktor: true }), sonst null.
class ApiFehler extends Error {
  constructor(text, status, daten) { super(text); this.status = status; this.daten = daten || null; }
}

// Routen ohne Anmeldung: eine 401 dort ist eine Antwort (z.B. falscher Sicherheitsschluessel),
// keine abgelaufene Anmeldung.
const OFFENE_ROUTEN = ['/api/status', '/api/anmelden', '/api/einrichten', '/api/zuruecksetzen', '/api/einspielen'];

// opt.auto: automatische Abfrage (Zeitgeber, Wiederverbinden). Sie verlaengert die Anmeldung nicht.
// opt.antwort401: Auf dieser Route ist eine 401 auch eine Antwort (falsches Passwort, falscher Code
// der Zwei-Faktor-Anmeldung). Ob die Anmeldung abgelaufen ist, klaert dann /api/status.
// Jede Anfrage nennt die Sprache der Oberflaeche (Fehlermeldungen des Servers kommen darin).
async function api(methode, pfad, rumpf, opt) {
  const kopf = { 'X-SSH-App': '1', 'X-SSH-App-Sprache': I18N.sprache };
  if (opt && opt.auto) kopf['X-SSH-App-Auto'] = '1';
  const o = { method: methode, headers: kopf, credentials: 'same-origin' };
  if (rumpf instanceof FormData) o.body = rumpf;
  else if (rumpf !== undefined) { o.body = JSON.stringify(rumpf); kopf['Content-Type'] = 'application/json'; }
  let res;
  try { res = await fetch(pfad, o); }
  catch (e) { throw new ApiFehler(t('basis.netzFehler'), 0); }
  let daten = null;
  try { daten = await res.json(); } catch (e) { /* leer */ }
  if (!res.ok) {
    const f = new ApiFehler((daten && daten.fehler) || t('basis.fehlerStatus', { status: res.status }), res.status, daten);
    const offen = OFFENE_ROUTEN.some((r) => pfad === r || pfad.startsWith(r + '?'));
    if (res.status === 401 && !offen && App.status && App.status.angemeldet && App.abgemeldet &&
        !(opt && opt.antwort401 && (await nochAngemeldet()))) {
      f.abgemeldet = true; // App.abgemeldet meldet es schon — kein zweiter Toast
      App.abgemeldet();
    }
    throw f;
  }
  return daten;
}
// Gilt die Anmeldung noch? Ohne Antwort des Servers: ja (dann zeigt die Route ihre eigene Meldung).
async function nochAngemeldet() {
  try { const s = await api('GET', '/api/status', undefined, { auto: true }); return !!(s && s.angemeldet); }
  catch (e) { return true; }
}

// ---------- Meldungen ----------
// art: 'fehler' (rot) oder 'warn' (gelb, bleibt am laengsten stehen: z.B. nur noch wenige Ersatzcodes).
function toast(text, art) {
  const k = el('div', { class: 'toast' + (art === 'fehler' || art === 'warn' ? ' ' + art : ''), role: art === 'warn' ? 'alert' : 'status' }, text);
  document.getElementById('toasts').append(k);
  setTimeout(() => k.remove(), art === 'warn' ? 9000 : art === 'fehler' ? 6000 : 3200);
}
function fehlerToast(e) {
  if (e && e.abgemeldet) return;
  toast(e && e.message ? e.message : String(e), 'fehler');
}

// ---------- Ebenen und Zurueck-Geste ----------
// Dialoge, Palette, Menues und Blaetter (Telefon) sind Ebenen. Solange eine offen ist, liegt ein
// eigener Verlaufseintrag obenauf: Die Zurueck-Geste (Android, installierte App) schliesst dann die
// oberste Ebene, statt die App zu verlassen.
const ebenen = [];
let ebenenWaechter = false; // liegt unser Verlaufseintrag gerade obenauf?
let ebenenPopIgnorieren = false;
let seiteLaedtNeu = false; // Neuladen laeuft: kein history.back() mehr, es braeche das Neuladen ab

function ebeneAuf(schliessen) {
  const e = { schliessen };
  ebenen.push(e);
  if (!ebenenWaechter) {
    try { history.pushState({ sshappEbene: true }, ''); ebenenWaechter = true; } catch (x) { /* ohne Verlauf */ }
  }
  return e;
}
function ebeneZu(e) {
  const i = ebenen.indexOf(e);
  if (i < 0) return;
  ebenen.splice(i, 1);
  // Erst im naechsten Durchlauf: oeffnet die Aktion gleich die naechste Ebene (z.B. Rueckfrage nach
  // der Palette), bleibt der Verlaufseintrag einfach stehen.
  if (!ebenen.length) setTimeout(ebenenWaechterAbbauen, 0);
}
function ebenenWaechterAbbauen() {
  if (ebenen.length || !ebenenWaechter || seiteLaedtNeu) return;
  ebenenWaechter = false;
  if (history.state && history.state.sshappEbene) { ebenenPopIgnorieren = true; history.back(); }
}
window.addEventListener('popstate', () => {
  if (ebenenPopIgnorieren) { ebenenPopIgnorieren = false; return; }
  ebenenWaechter = false;
  const oben = ebenen[ebenen.length - 1];
  if (oben) oben.schliessen();
  if (ebenen.length && !ebenenWaechter) {
    try { history.pushState({ sshappEbene: true }, ''); ebenenWaechter = true; } catch (x) { /* ohne Verlauf */ }
  }
});
// Nach dem Neuladen liegt evtl. noch ein alter Verlaufseintrag obenauf: abbauen, sonst bewirkt der
// naechste Zurueck-Druck nichts.
if (history.state && history.state.sshappEbene) { ebenenPopIgnorieren = true; history.back(); }
// Alles schliessen (Abmelden): Dialoge, Menues, Palette.
function ebenenAlleZu() {
  for (const e of ebenen.slice().reverse()) { try { e.schliessen(true); } catch (x) { /* weiter */ } }
  for (const e of dialogStapel.slice().reverse()) e.zu();
  document.querySelectorAll('.menue, .menue-schleier').forEach((m) => m.remove());
}

// ---------- Dialoge ----------
const dialogStapel = [];
// feld (optional): Was beim Oeffnen den Fokus bekommt (Desktop) statt des ersten Eingabefelds —
// ein Element oder eine Funktion (Dialog) => Element, die nach dem Einhaengen laeuft (Masse
// stimmen dann). null: der Dialog selbst, z.B. wenn der Anfang sichtbar bleiben soll.
function dialog({ titel, inhalt, knoepfe, breit, beimSchliessen, schliessbar = true, feld }) {
  const wurzel = document.getElementById('modal-wurzel');
  const schleier = el('div', { class: 'schleier' });
  const fuss = knoepfe && knoepfe.length ? el('div', { class: 'dialog-fuss' }, knoepfe) : null;
  const d = el('div', { class: 'dialog' + (breit ? ' breit' : ''), role: 'dialog', 'aria-modal': 'true', 'aria-label': titel, tabindex: '-1' },
    el('div', { class: 'blatt-griff', 'aria-hidden': 'true' }),
    el('div', { class: 'dialog-kopf' }, el('h3', { text: titel }), schliessbar ? ikonKnopf('x', t('basis.schliessen'), () => zu()) : null),
    el('div', { class: 'dialog-inhalt' }, inhalt),
    fuss);
  const vorher = document.activeElement;
  let offen = true;
  function zu(wert) {
    if (!offen) return;
    offen = false;
    schleier.remove(); d.remove();
    const i = dialogStapel.indexOf(eintrag);
    if (i >= 0) dialogStapel.splice(i, 1);
    ebeneZu(ebene);
    // Fokus zurueck (nur Desktop: auf dem Telefon wuerde das die Tastatur oeffnen).
    if (!App.istTelefon() && (!document.activeElement || document.activeElement === document.body)) {
      if (vorher && vorher.isConnected && vorher !== document.body) vorher.focus({ preventScroll: true });
      else if (App.aktiv && !dialogStapel.length && !(App.paletteObj && App.paletteObj.offen)) App.tabFokus(App.aktiv);
    }
    if (beimSchliessen) beimSchliessen(wert);
  }
  const eintrag = { zu, schliessbar };
  const ebene = ebeneAuf(() => { if (schliessbar) zu(); });
  if (schliessbar) schleier.addEventListener('click', () => zu());
  wurzel.append(schleier, d);
  dialogStapel.push(eintrag);
  // Fokus in den Dialog holen, sonst gehen Enter und Tippen an das Terminal dahinter.
  const erstes = feld === undefined ? d.querySelector('input:not([type=checkbox]):not([type=file]):not([type=hidden]), select, textarea')
    : typeof feld === 'function' ? feld(d) : feld;
  if (erstes && !App.istTelefon()) setTimeout(() => { if (offen && !d.contains(document.activeElement)) erstes.focus(); }, 30);
  else if (feld !== undefined) d.focus({ preventScroll: true });
  else {
    // Ohne Eingabefeld: Hauptknopf (Enter bestaetigt). Bei gefaehrlichen Aktionen nur den Dialog
    // selbst — Enter loest dort nichts aus, Escape bricht ab. Telefon: immer der Dialog selbst (ohne
    // Ring); der Fokus muss nur aus dem Terminal heraus, eine Eingabetaste gibt es dort nicht.
    const haupt = !App.istTelefon() && fuss && fuss.querySelector('.btn.primaer:not(.links)');
    (haupt || d).focus({ preventScroll: true });
  }
  return { zu, element: d };
}
// Escape im Capture-Modus: das Terminal (xterm) haelt die Taste sonst fest.
document.addEventListener('keydown', (e) => {
  if (e.key !== 'Escape' || menue.offen) return; // ein offenes Menue schliesst sich selbst
  if (dialogStapel.length) {
    const oben = dialogStapel[dialogStapel.length - 1];
    e.preventDefault(); e.stopPropagation();
    if (oben.schliessbar) oben.zu();
  }
}, true);

// Rueckfrage. befehl (optional) steht abgesetzt in Festbreitenschrift unter dem Text.
// nein: Text des Knopfs, der ablehnt (wie Schliessen und Escape); Vorgabe «Abbrechen».
function frage(text, { titel = t('basis.bestaetigen'), ja = t('basis.ok'), nein = t('basis.abbrechen'), gefahr = false, befehl } = {}) {
  return new Promise((resolve) => {
    let ergebnis = false;
    const d = dialog({
      titel,
      inhalt: el('div', { style: { display: 'flex', flexDirection: 'column', gap: '14px' } },
        el('p', { class: 'frage-text' }, text),
        befehl ? el('div', { class: 'befehl-gross' }, befehlWoerter(befehl)) : null),
      knoepfe: [
        el('button', { class: 'btn', type: 'button', onclick: () => d.zu() }, nein),
        el('button', { class: 'btn ' + (gefahr ? 'gefahr' : 'primaer'), type: 'button', onclick: () => { ergebnis = true; d.zu(); } }, ja),
      ],
      beimSchliessen: () => resolve(ergebnis),
    });
  });
}

function eingabe(label, { titel, wert = '', hinweis, passwort = false, ok = t('basis.ok') } = {}) {
  return new Promise((resolve) => {
    let ergebnis = null;
    const inp = el('input', { type: passwort ? 'password' : 'text', value: wert, autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false' });
    const form = el('form', { onsubmit: (e) => { e.preventDefault(); ergebnis = inp.value; d.zu(); } },
      el('div', { class: 'feld' }, el('label', null, label), inp, hinweis ? el('div', { class: 'hinweis' }, hinweis) : null));
    const d = dialog({
      titel: titel || label, inhalt: form,
      knoepfe: [
        el('button', { class: 'btn', type: 'button', onclick: () => d.zu() }, t('basis.abbrechen')),
        el('button', { class: 'btn primaer', type: 'button', onclick: () => { ergebnis = inp.value; d.zu(); } }, ok),
      ],
      beimSchliessen: () => resolve(ergebnis),
    });
    setTimeout(() => inp.focus(), 40);
  });
}

// Feld fuer den Code der Zwei-Faktor-Anmeldung (Anmelden, Einstellungen). Zwei Arten:
// - 6 Ziffern aus der Authenticator-App: Ziffernblock auf dem Telefon, andere Zeichen fallen weg
//   (auch beim Einfuegen von «123 456»); mit der 6. Ziffer laeuft beiVoll() (z.B. Absenden).
// - Ersatzcode XXXX-XXXX: Texttastatur, Grossbuchstaben; der Server ignoriert Leerzeichen und Striche.
// Ergebnis: { input, ersatz(an), istErsatz(), wert(), voll() }.
function codeFeld(id, beiVoll) {
  const input = el('input', { id, type: 'text', class: 'code-feld', spellcheck: 'false' });
  let ersatz = false;
  const f = {
    input,
    istErsatz: () => ersatz,
    wert: () => input.value.trim(),
    voll: () => (ersatz ? input.value.replace(/[\s-]/g, '').length >= 8 : input.value.length === 6),
    ersatz(an) {
      ersatz = !!an;
      input.value = '';
      input.removeAttribute('aria-invalid');
      input.classList.toggle('ersatz', ersatz);
      input.setAttribute('inputmode', ersatz ? 'text' : 'numeric');
      input.setAttribute('autocomplete', ersatz ? 'off' : 'one-time-code');
      input.setAttribute('autocapitalize', ersatz ? 'characters' : 'off');
      input.placeholder = ersatz ? 'XXXX-XXXX' : '000000';
    },
  };
  input.addEventListener('input', () => {
    input.removeAttribute('aria-invalid');
    if (ersatz) return;
    const z = input.value.replace(/\D/g, '').slice(0, 6);
    if (z !== input.value) input.value = z;
    if (z.length === 6 && beiVoll) beiVoll();
  });
  f.ersatz(false);
  return f;
}

// Kontextmenue: am Element (Desktop) oder als Blatt unten (Telefon).
// Escape schliesst, Pfeiltasten waehlen; ein zweites Oeffnen ersetzt das erste samt Schleier.
// opt.klasse: zusaetzliche Klasse (z. B. «raster»: Eintraege als Raster, Funktionstasten).
function menue(anker, eintraege, opt) {
  document.querySelectorAll('.menue, .menue-schleier').forEach((m) => m.remove());
  if (menue.offen) menue.offen();
  const schleier = el('div', { class: 'schleier menue-schleier', style: { background: App.istTelefon() ? 'rgba(5,6,8,.5)' : 'transparent' } });
  const m = el('div', { class: 'menue' + (opt && opt.klasse ? ' ' + opt.klasse : ''), role: 'menu' });
  const vorher = document.activeElement;
  let offen = true;
  function zu(ohneFokus) {
    if (!offen) return;
    offen = false;
    m.remove(); schleier.remove();
    document.removeEventListener('keydown', taste, true);
    ebeneZu(ebene);
    if (menue.offen === zu) menue.offen = null;
    if (!ohneFokus && !App.istTelefon() && vorher && vorher.isConnected && !dialogStapel.length) vorher.focus({ preventScroll: true });
  }
  function taste(e) {
    const knoepfe = [...m.querySelectorAll('button')];
    const i = knoepfe.indexOf(document.activeElement);
    if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); zu(); }
    else if (e.key === 'ArrowDown') { e.preventDefault(); e.stopPropagation(); (knoepfe[i + 1] || knoepfe[0]).focus(); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); e.stopPropagation(); (knoepfe[i - 1] || knoepfe[knoepfe.length - 1]).focus(); }
    else if (e.key === 'Tab') { e.preventDefault(); e.stopPropagation(); (knoepfe[e.shiftKey ? i - 1 : i + 1] || knoepfe[e.shiftKey ? knoepfe.length - 1 : 0]).focus(); }
  }
  for (const e of eintraege) {
    if (!e) continue;
    m.append(el('button', { type: 'button', role: 'menuitem', class: (e.gefahr ? 'gefahr' : '') + (e.aktiv ? ' aktiv' : '') + (e.klasse ? ' ' + e.klasse : ''), 'aria-current': e.aktiv ? 'true' : null,
      onclick: () => { zu(true); e.aktion(); } },
      e.symbol || (e.icon ? icon(e.icon) : null), e.text));
  }
  schleier.addEventListener('click', () => zu());
  document.body.append(schleier, m);
  const ebene = ebeneAuf(() => zu(true));
  menue.offen = zu;
  document.addEventListener('keydown', taste, true);
  if (!App.istTelefon() && anker) {
    const r = anker.getBoundingClientRect();
    const mw = m.offsetWidth, mh = m.offsetHeight;
    let x = Math.min(r.left, window.innerWidth - mw - 8), y = r.bottom + 4;
    if (y + mh > window.innerHeight - 8) y = Math.max(8, r.top - mh - 4);
    m.style.left = Math.max(8, x) + 'px'; m.style.top = y + 'px';
  }
  // Fokus ins Menue, damit Tasten nicht ans Terminal gehen (auf dem Telefon ohne sichtbaren Ring).
  const erster = m.querySelector('button');
  if (erster) erster.focus({ preventScroll: true });
}

// ---------- Kleinkram ----------
function groesseText(n) {
  if (n < 1024) return n + ' B';
  const e = ['KB', 'MB', 'GB', 'TB'];
  let i = -1;
  do { n /= 1024; i++; } while (n >= 1024 && i < e.length - 1);
  const stellen = n < 10 ? 1 : 0;
  return n.toLocaleString(lokalCode(), { minimumFractionDigits: stellen, maximumFractionDigits: stellen }) + ' ' + e[i];
}
async function kopieren(text) {
  try { await navigator.clipboard.writeText(text); toast(t('basis.kopiert')); }
  catch (e) {
    const feld = el('textarea', { style: { position: 'fixed', opacity: 0 } }, text);
    document.body.append(feld); feld.select();
    try { document.execCommand('copy'); toast(t('basis.kopiert')); } catch (e2) { toast(t('basis.kopierenFehler'), 'fehler'); }
    feld.remove();
  }
}
// Ein Download per Link kann keinen Kopf senden: Die Sprache fuer eine Fehlerantwort des Servers
// geht als Parameter «sprache» mit.
function herunterladen(href) {
  if (/^\/api\//.test(href) && !/[?&]sprache=/.test(href)) href += (href.includes('?') ? '&' : '?') + 'sprache=' + I18N.sprache;
  const a = el('a', { href, download: '' });
  document.body.append(a); a.click(); a.remove();
}
function textDatei(name, text) {
  const url = URL.createObjectURL(new Blob([text], { type: 'text/plain;charset=utf-8' }));
  const a = el('a', { href: url, download: name });
  document.body.append(a); a.click(); a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 2000);
}
function lokal(k, standard) {
  try { const v = localStorage.getItem('sshapp.' + k); return v === null ? standard : JSON.parse(v); } catch (e) { return standard; }
}
function lokalSetzen(k, v) { try { localStorage.setItem('sshapp.' + k, JSON.stringify(v)); } catch (e) { /* privat */ } }

// tldr-Texte zeichnen Befehle mit `Backticks` aus: als <code> zeigen bzw. fuer Namen entfernen.
function mitCode(text) {
  const s = String(text || '');
  const teile = s.split('`');
  if (teile.length < 3 || teile.length % 2 === 0) return s;
  return teile.map((x, i) => (i % 2 ? el('code', { class: 'inline' }, x) : x)).filter((x) => x !== '');
}
function ohneCode(text) { return String(text || '').replace(/`/g, ''); }

// Platzhalter {{name}} in Befehlen (tldr und Snippets).
function platzhalter(befehl) {
  const namen = [];
  befehl.replace(/\{\{(.+?)\}\}/g, (m, n) => { if (!namen.includes(n)) namen.push(n); return m; });
  return namen;
}
// Optionsplatzhalter {{[-C|--directory]}} werden zur Kurzform.
function optionenAufloesen(befehl) {
  return befehl.replace(/\{\{\[([^\]]+)\]\}\}/g, (m, alt) => alt.split('|')[0]);
}
function befehlFuellen(befehl, werte) {
  return optionenAufloesen(befehl).replace(/\{\{(.+?)\}\}/g, (m, n) => (werte[n] !== undefined && werte[n] !== '' ? werte[n] : n));
}
// Befehl zum Lesen (Befehlshilfe, Palette, Rueckfragen): Jedes Wort steht in einem eigenen Kasten
// (.wort, app.css). So bricht die Zeile nur an Leerzeichen um, nie mitten in einem Schalter wie
// «--strip-components=1» — Chrome bricht sonst nach jedem Bindestrich um. Nur ein Wort, das allein
// nicht in die Zeile passt (langer Pfad), bricht in sich um. Mit mitPlatzhaltern werden {{name}}
// hervorgehoben und gehoeren zu ihrem Wort, auch wenn sie Leerzeichen enthalten.
function befehlWoerter(text, mitPlatzhaltern) {
  const frag = document.createDocumentFragment();
  const teile = String(text || '').match(mitPlatzhaltern ? /\{\{.+?\}\}|\s+|[^\s{]+|\{/g : /\s+|\S+/g) || [];
  let wort = null;
  for (const s of teile) {
    if (/^\s+$/.test(s)) { frag.append(s); wort = null; continue; }
    if (!wort) { wort = el('span', { class: 'wort' }); frag.append(wort); }
    const m = mitPlatzhaltern ? /^\{\{(.+)\}\}$/.exec(s) : null;
    wort.append(m ? el('span', { class: 'ph' }, m[1]) : s);
  }
  return frag;
}
function befehlAnzeige(befehl) { return befehlWoerter(optionenAufloesen(befehl), true); }

// Erkennung gefaehrlicher Befehle (Rueckfrage bei Hosts mit Tag «kritisch»/«critical»). Prueft jeden Teil einer
// Befehlskette einzeln und laesst Vorsilben wie sudo, doas, env, nohup oder VAR=wert weg.
// Vorsilben, nach denen der eigentliche Befehl kommt, mit ihren Schaltern, die einen eigenen Wert haben.
const VORSILBEN = {
  sudo: ['-u', '-g', '-p', '-C', '-D', '-h', '-r', '-t', '-U', '-T'], doas: ['-u', '-C'], run0: ['-u', '-g', '-D'],
  env: ['-u', '-C', '-S'], nice: ['-n'], ionice: ['-c', '-n', '-p'], time: ['-f', '-o'], xargs: ['-I', '-n', '-P', '-L', '-s', '-d', '-E', '-a'],
  exec: ['-a'], nohup: [], command: [], builtin: [], stdbuf: [], chroot: [],
};
const SCHLUESSELWOERTER = ['{', '}', '!', 'then', 'do', 'else', 'if', 'while', 'until'];
function istGefaehrlich(text) {
  const s = String(text || '');
  if (/:\(\)\s*\{/.test(s)) return true; // Fork-Bombe
  if (/>\s*\/dev\/(sd|hd|vd|xvd|nvme|mmcblk|disk)/i.test(s)) return true; // Geraet ueberschreiben
  for (const teil of s.split(/;|&&|\|\||\||\n|\$\(|`|\(|\)/)) {
    const w = teil.trim().split(/\s+/).filter(Boolean);
    while (w.length) {
      if (/^\w+=/.test(w[0]) || SCHLUESSELWOERTER.includes(w[0])) { w.shift(); continue; }
      const k = w[0].replace(/^\\/, '').split('/').pop();
      const mitWert = Object.prototype.hasOwnProperty.call(VORSILBEN, k) ? VORSILBEN[k] : null;
      if (!mitWert) break;
      w.shift();
      while (w.length && (w[0].startsWith('-') || /^\w+=/.test(w[0]))) { const o = w.shift(); if (mitWert.includes(o)) w.shift(); }
      if (k === 'chroot' && w.length) w.shift(); // Zielordner
    }
    if (!w.length) continue;
    const befehl = w[0].replace(/^\\/, '').split('/').pop().toLowerCase();
    const args = w.slice(1);
    const schalter = [];
    for (const a of args) { if (a === '--') break; if (a.startsWith('-')) schalter.push(a); }
    if (befehl === 'rm') {
      const r = schalter.some((o) => o === '--recursive' || /^-[^-]*[rR]/.test(o));
      const f = schalter.some((o) => o === '--force' || /^-[^-]*f/.test(o));
      if (r && f) return true;
      continue;
    }
    if (/^(dd|wipefs|shred|shutdown|reboot|poweroff|halt)$/.test(befehl) || befehl.startsWith('mkfs')) return true;
    if (/^(init|telinit)$/.test(befehl) && args.some((a) => a === '0' || a === '6')) return true;
    if (befehl === 'systemctl' && args.some((a) => /^(reboot|poweroff|halt|kexec|soft-reboot)$/.test(a))) return true;
  }
  return false;
}
const GEFAEHRLICH = { test: istGefaehrlich };

// Wie replaceChildren, aber Listen werden flach gemacht und null/false ausgelassen
// (das native replaceChildren schreibt sonst «null» oder «[object …]» in die Seite).
function ersetzen(knoten, ...kinder) {
  knoten.replaceChildren(...kinder.flat(Infinity).filter((k) => k !== null && k !== undefined && k !== false && k !== ''));
}

// Uebersetzter Text mit Bausteinen: Platzhalter {name} werden durch Knoten (oder Texte) aus
// elemente ersetzt. Ergebnis ist eine Liste fuer el()/ersetzen().
// Beispiel: tTeile('app.hostkey.erste', { ziel: el('b', null, 'pi@host') })
function tTeile(schluessel, elemente) {
  const roh = t(schluessel);
  const aus = [];
  let rest = roh;
  const re = /\{(\w+)\}/;
  for (let m = re.exec(rest); m; m = re.exec(rest)) {
    if (m.index) aus.push(rest.slice(0, m.index));
    const e = elemente && elemente[m[1]];
    aus.push(e !== undefined && e !== null ? e : m[0]);
    rest = rest.slice(m.index + m[0].length);
  }
  if (rest) aus.push(rest);
  return aus;
}

// Sprache wechseln: merken und neu laden (laufende Terminals haengen sich danach wieder an).
// Aus dem Mehr-Menue heraus baut das Schliessen des Menues gleich danach seinen Verlaufseintrag
// ab (history.back()); diese Verlaufs-Navigation braeche das Neuladen ab, die Seite bliebe in
// der alten Sprache. Den Eintrag raeumt nach dem Neuladen der Start dieser Datei ab.
// Bildschirme (VNC) enden mit dem Neuladen und lassen sich nicht wieder anhaengen: vorher fragen.
App.spracheWechseln = async function (l) {
  if (spracheNormal(l) === I18N.sprache) return;
  const bildschirme = App.offeneBildschirme ? App.offeneBildschirme() : [];
  if (bildschirme.length) {
    const text = bildschirme.length === 1 ? t('basis.spracheFrage1', { titel: bildschirme[0].titel }) : t('basis.spracheFrage', { n: bildschirme.length });
    if (!(await frage(text, { titel: t('basis.spracheWechseln'), ja: t('basis.spracheJa'), gefahr: true }))) return;
  }
  spracheSetzen(l, true);
  seiteLaedtNeu = true;
  location.reload();
};

// Umschalter «Deutsch / English» (Einrichten, Anmelden; auch fuer die Einstellungen nutzbar).
App.sprachUmschalter = function (klasse) {
  return el('div', { class: 'sprachwahl' + (klasse ? ' ' + klasse : ''), role: 'group', 'aria-label': t('basis.sprache') },
    SPRACHEN.map((s) => el('button', { type: 'button', lang: s.id, 'aria-pressed': s.id === I18N.sprache ? 'true' : 'false',
      onclick: () => App.spracheWechseln(s.id) }, s.name)));
};
