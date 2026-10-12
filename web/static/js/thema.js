'use strict';
// Akzentfarbe der Oberflaeche: Vorgabe Violett, umstellbar in den Einstellungen.
// Gespeichert je Browser (localStorage). Warn-/Fehlerfarben bleiben davon unberuehrt —
// sie tragen Bedeutung, kein Design.

const AKZENTE = [
  { id: 'violett', de: 'Violett', en: 'Violet', farbe: '#9B7BF7', hell: '#B9A2FA', text: '#140F24', grund: '#221B36' },
  { id: 'blau', de: 'Blau', en: 'Blue', farbe: '#5B9DF5', hell: '#8DBBF8', text: '#0A1424', grund: '#16233A' },
  { id: 'tuerkis', de: 'Türkis', en: 'Teal', farbe: '#3FC1B0', hell: '#79D6CA', text: '#06201C', grund: '#122A27' },
  { id: 'gruen', de: 'Grün', en: 'Green', farbe: '#6CC46A', hell: '#9AD898', text: '#0B1F0A', grund: '#172915' },
  { id: 'bernstein', de: 'Bernstein', en: 'Amber', farbe: '#E9A23B', hell: '#F5C77A', text: '#17130B', grund: '#2A2212' },
  { id: 'rosa', de: 'Rosa', en: 'Pink', farbe: '#E879B9', hell: '#F0A6D1', text: '#24091A', grund: '#331828' },
];

function akzentFuer(id) {
  return AKZENTE.find((a) => a.id === id) || AKZENTE[0];
}

// Terminal-Farben aus dem Akzent: Cursor in der Akzentfarbe, Markierung halbtransparent darueber
// (der Text bleibt lesbar, die Markierung ist auf dem dunklen Grund deutlich).
function akzentTerm(a) {
  return { cursor: a.farbe, cursorAccent: '#0B0D10', selectionBackground: a.farbe + '59', selectionInactiveBackground: a.farbe + '33' };
}

// Setzt die CSS-Variablen --akzent, --akzent-hell, --akzent-text, --akzent-grund
// und den Cursor offener Terminals. id unbekannt => Violett.
function akzentSetzen(id, merken) {
  const a = akzentFuer(id);
  const r = document.documentElement.style;
  r.setProperty('--akzent', a.farbe);
  r.setProperty('--akzent-hell', a.hell);
  r.setProperty('--akzent-text', a.text);
  r.setProperty('--akzent-grund', a.grund);
  const meta = document.querySelector('meta[name=theme-color]');
  if (meta) meta.setAttribute('content', '#151920');
  if (merken) { try { localStorage.setItem('sshapp.akzent', JSON.stringify(a.id)); } catch (e) { /* privat */ } }
  if (window.App && App.tabs) {
    for (const t of App.tabs) {
      if (t.term) t.term.options.theme = Object.assign({}, t.term.options.theme, akzentTerm(a));
    }
  }
  return a;
}

function akzentGemerkt() {
  try { const v = localStorage.getItem('sshapp.akzent'); return v ? JSON.parse(v) : 'violett'; } catch (e) { return 'violett'; }
}

// Sofort beim Laden anwenden, damit nichts in der alten Farbe aufblitzt.
akzentSetzen(akzentGemerkt(), false);
