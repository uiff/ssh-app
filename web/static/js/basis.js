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
class ApiFehler extends Error {
  constructor(text, status) { super(text); this.status = status; }
}

async function api(methode, pfad, rumpf) {
  const opt = { method: methode, headers: { 'X-SSH-App': '1' }, credentials: 'same-origin' };
  if (rumpf instanceof FormData) opt.body = rumpf;
  else if (rumpf !== undefined) { opt.body = JSON.stringify(rumpf); opt.headers['Content-Type'] = 'application/json'; }
  let res;
  try { res = await fetch(pfad, opt); }
  catch (e) { throw new ApiFehler('Server nicht erreichbar — Netzwerkverbindung prüfen', 0); }
  let daten = null;
  try { daten = await res.json(); } catch (e) { /* leer */ }
  if (!res.ok) {
    if (res.status === 401 && !pfad.startsWith('/api/anmelden') && App.abgemeldet) App.abgemeldet();
    throw new ApiFehler((daten && daten.fehler) || ('Fehler ' + res.status), res.status);
  }
  return daten;
}

// ---------- Meldungen ----------
function toast(text, art) {
  const t = el('div', { class: 'toast' + (art === 'fehler' ? ' fehler' : ''), role: 'status' }, text);
  document.getElementById('toasts').append(t);
  setTimeout(() => t.remove(), art === 'fehler' ? 6000 : 3200);
}
function fehlerToast(e) { toast(e && e.message ? e.message : String(e), 'fehler'); }

// ---------- Dialoge ----------
const dialogStapel = [];
function dialog({ titel, inhalt, knoepfe, breit, beimSchliessen, schliessbar = true }) {
  const wurzel = document.getElementById('modal-wurzel');
  const schleier = el('div', { class: 'schleier' });
  const fuss = knoepfe && knoepfe.length ? el('div', { class: 'dialog-fuss' }, knoepfe) : null;
  const d = el('div', { class: 'dialog' + (breit ? ' breit' : ''), role: 'dialog', 'aria-modal': 'true', 'aria-label': titel },
    el('div', { class: 'blatt-griff', 'aria-hidden': 'true' }),
    el('div', { class: 'dialog-kopf' }, el('h3', { text: titel }), schliessbar ? ikonKnopf('x', 'Schliessen', () => zu()) : null),
    el('div', { class: 'dialog-inhalt' }, inhalt),
    fuss);
  let offen = true;
  function zu(wert) {
    if (!offen) return;
    offen = false;
    schleier.remove(); d.remove();
    const i = dialogStapel.indexOf(eintrag);
    if (i >= 0) dialogStapel.splice(i, 1);
    if (beimSchliessen) beimSchliessen(wert);
  }
  const eintrag = { zu, schliessbar };
  if (schliessbar) schleier.addEventListener('click', () => zu());
  wurzel.append(schleier, d);
  dialogStapel.push(eintrag);
  const erstes = d.querySelector('input:not([type=checkbox]):not([type=file]), select, textarea');
  if (erstes && !App.istTelefon()) setTimeout(() => erstes.focus(), 30);
  return { zu, element: d };
}
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape' && dialogStapel.length) {
    const oben = dialogStapel[dialogStapel.length - 1];
    if (oben.schliessbar) { e.preventDefault(); oben.zu(); }
  }
});

function frage(text, { titel = 'Bitte bestätigen', ja = 'OK', gefahr = false } = {}) {
  return new Promise((resolve) => {
    let ergebnis = false;
    const d = dialog({
      titel, inhalt: el('p', { style: { margin: 0, lineHeight: 1.55 } }, text),
      knoepfe: [
        el('button', { class: 'btn', type: 'button', onclick: () => d.zu() }, 'Abbrechen'),
        el('button', { class: 'btn ' + (gefahr ? 'gefahr' : 'primaer'), type: 'button', onclick: () => { ergebnis = true; d.zu(); } }, ja),
      ],
      beimSchliessen: () => resolve(ergebnis),
    });
  });
}

function eingabe(label, { titel, wert = '', hinweis, passwort = false, ok = 'OK' } = {}) {
  return new Promise((resolve) => {
    let ergebnis = null;
    const inp = el('input', { type: passwort ? 'password' : 'text', value: wert, autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false' });
    const form = el('form', { onsubmit: (e) => { e.preventDefault(); ergebnis = inp.value; d.zu(); } },
      el('div', { class: 'feld' }, el('label', null, label), inp, hinweis ? el('div', { class: 'hinweis' }, hinweis) : null));
    const d = dialog({
      titel: titel || label, inhalt: form,
      knoepfe: [
        el('button', { class: 'btn', type: 'button', onclick: () => d.zu() }, 'Abbrechen'),
        el('button', { class: 'btn primaer', type: 'button', onclick: () => { ergebnis = inp.value; d.zu(); } }, ok),
      ],
      beimSchliessen: () => resolve(ergebnis),
    });
    setTimeout(() => inp.focus(), 40);
  });
}

// Kontextmenue: am Element (Desktop) oder als Blatt unten (Telefon).
function menue(anker, eintraege) {
  document.querySelectorAll('.menue').forEach((m) => m.remove());
  const schleier = el('div', { class: 'schleier', style: { background: App.istTelefon() ? 'rgba(5,6,8,.5)' : 'transparent' } });
  const m = el('div', { class: 'menue', role: 'menu' });
  function zu() { m.remove(); schleier.remove(); }
  for (const e of eintraege) {
    if (!e) continue;
    m.append(el('button', { type: 'button', role: 'menuitem', class: e.gefahr ? 'gefahr' : '', onclick: () => { zu(); e.aktion(); } },
      e.icon ? icon(e.icon) : null, e.text));
  }
  schleier.addEventListener('click', zu);
  document.body.append(schleier, m);
  if (!App.istTelefon() && anker) {
    const r = anker.getBoundingClientRect();
    const mw = m.offsetWidth, mh = m.offsetHeight;
    let x = Math.min(r.left, window.innerWidth - mw - 8), y = r.bottom + 4;
    if (y + mh > window.innerHeight - 8) y = Math.max(8, r.top - mh - 4);
    m.style.left = Math.max(8, x) + 'px'; m.style.top = y + 'px';
  }
}

// ---------- Kleinkram ----------
function groesseText(n) {
  if (n < 1024) return n + ' B';
  const e = ['KB', 'MB', 'GB', 'TB'];
  let i = -1;
  do { n /= 1024; i++; } while (n >= 1024 && i < e.length - 1);
  return n.toFixed(n < 10 ? 1 : 0).replace('.', ',') + ' ' + e[i];
}
async function kopieren(text) {
  try { await navigator.clipboard.writeText(text); toast('Kopiert'); }
  catch (e) {
    const t = el('textarea', { style: { position: 'fixed', opacity: 0 } }, text);
    document.body.append(t); t.select();
    try { document.execCommand('copy'); toast('Kopiert'); } catch (e2) { toast('Kopieren nicht möglich', 'fehler'); }
    t.remove();
  }
}
function herunterladen(href) {
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
function befehlAnzeige(befehl) {
  const frag = document.createDocumentFragment();
  const b = optionenAufloesen(befehl);
  let rest = b;
  const re = /\{\{(.+?)\}\}/;
  while (rest) {
    const m = re.exec(rest);
    if (!m) { frag.append(rest); break; }
    frag.append(rest.slice(0, m.index), el('span', { class: 'ph' }, m[1]));
    rest = rest.slice(m.index + m[0].length);
  }
  return frag;
}

const GEFAEHRLICH = /(^|[;&|]\s*|sudo\s+)(rm\s+-[a-z]*r[a-z]*f|rm\s+-[a-z]*f[a-z]*r|dd\s+|mkfs|shutdown|reboot|poweroff|halt\b|:\(\)\s*\{)/i;

// Wie replaceChildren, aber Listen werden flach gemacht und null/false ausgelassen
// (das native replaceChildren schreibt sonst «null» oder «[object …]» in die Seite).
function ersetzen(knoten, ...kinder) {
  knoten.replaceChildren(...kinder.flat(Infinity).filter((k) => k !== null && k !== undefined && k !== false && k !== ''));
}
