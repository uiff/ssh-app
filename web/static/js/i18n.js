'use strict';
// Sprache der Oberflaeche (Deutsch/Englisch). Texte stehen je Bereich in
// js/sprache/<bereich>.js und werden per i18nErgaenzen() eingetragen.
// Aufruf: t('bereich.schluessel') bzw. t('bereich.schluessel', { name: 'x' }) fuer {name}.
// Wechsel der Sprache: spracheSetzen(l, true) und Seite neu laden (Terminals haengen sich wieder an).

const SPRACHEN = [
  { id: 'de', name: 'Deutsch' },
  { id: 'en', name: 'English' },
];
const I18N = { sprache: 'de', texte: { de: {}, en: {} }, fehlend: new Set() };

function i18nErgaenzen(paket) {
  for (const l of ['de', 'en']) {
    for (const [k, v] of Object.entries((paket && paket[l]) || {})) {
      if (l === 'de' && Object.prototype.hasOwnProperty.call(I18N.texte.de, k)) console.error('i18n: duplicate key: ' + k);
      I18N.texte[l][k] = v;
    }
  }
}

function t(schluessel, werte) {
  let s = I18N.texte[I18N.sprache][schluessel];
  if (s === undefined) s = I18N.texte.de[schluessel];
  if (s === undefined) { I18N.fehlend.add(schluessel); s = schluessel; }
  if (werte) s = s.replace(/\{(\w+)\}/g, (m, k) => (werte[k] !== undefined && werte[k] !== null ? String(werte[k]) : m));
  return s;
}

function spracheNormal(l) { return String(l || '').toLowerCase().startsWith('en') ? 'en' : 'de'; }

function spracheGemerkt() {
  try { const v = localStorage.getItem('sshapp.sprache'); if (v) return spracheNormal(JSON.parse(v)); } catch (e) { /* privat */ }
  const nav = (navigator.languages && navigator.languages[0]) || navigator.language || 'de';
  return nav.toLowerCase().startsWith('de') ? 'de' : 'en';
}

function spracheSetzen(l, merken) {
  I18N.sprache = spracheNormal(l);
  document.documentElement.lang = I18N.sprache;
  if (merken) { try { localStorage.setItem('sshapp.sprache', JSON.stringify(I18N.sprache)); } catch (e) { /* privat */ } }
  return I18N.sprache;
}

// Zahlen und Daten im Format der Sprache (Schweiz fuer Deutsch).
function lokalCode() { return I18N.sprache === 'en' ? 'en-GB' : 'de-CH'; }

spracheSetzen(spracheGemerkt(), false);
