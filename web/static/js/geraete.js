'use strict';
// Geraetetypen fuer Hosts: Strich-Symbole (24x24, wie icons in basis.js), keine Emoji.
// id ist der gespeicherte Wert (ASCII), Texte stehen je Sprache daneben; kurz (optional) fuer enge Kacheln.

const GERAETE = [
  { id: '', de: 'Allgemein', en: 'General', gruppe: 'sonst', d: 'M4 6h16v12H4zM8 10h3M8 14h6' },
  // Industrie
  { id: 'sps', de: 'SPS / PLC', en: 'PLC', gruppe: 'industrie', d: 'M5 4h14v16H5zM5 8h14M5 16h14M8 4V2M12 4V2M16 4V2M8 22v-2M12 22v-2M16 22v-2M8 12h3' },
  { id: 'hmi', de: 'HMI / Panel', en: 'HMI / panel', gruppe: 'industrie', d: 'M3 4h18v13H3zM8 21h8M12 17v4M6 7h5v7H6zM14 8h4M14 11h4M14 14h2' },
  { id: 'gateway', de: 'Gateway', en: 'Gateway', gruppe: 'industrie', d: 'M4 12h16v8H4zM8 16h.01M12 16h.01M17 12V6M14.5 6.5a3.5 3.5 0 0 1 5 0M13 4.5a6 6 0 0 1 8 0' },
  { id: 'iot', de: 'IoT-Gerät / Sensor', en: 'IoT device / sensor', gruppe: 'industrie', d: 'M12 12h.01M8.5 8.5a5 5 0 0 0 0 7M15.5 8.5a5 5 0 0 1 0 7M5.6 5.6a9 9 0 0 0 0 12.8M18.4 5.6a9 9 0 0 1 0 12.8' },
  { id: 'wallbox', de: 'Wallbox', en: 'Wallbox', gruppe: 'industrie', d: 'M5 3h9v18H5zM14 8h2a2 2 0 0 1 2 2v5a2 2 0 0 0 4 0V8M10 7l-2 5h3l-2 5' },
  { id: 'wechselrichter', de: 'Wechselrichter', en: 'Inverter', gruppe: 'industrie', d: 'M4 4h16v16H4zM4 20L20 4M7 8h4M13 16c1-2 2-2 3 0s2 2 3 0' },
  // Rechner
  { id: 'server', de: 'Server', en: 'Server', gruppe: 'rechner', d: 'M4 4h16v6H4zM4 14h16v6H4zM8 7h.01M8 17h.01M12 7h5M12 17h5' },
  { id: 'vm', de: 'Virtuelle Maschine', en: 'Virtual machine', gruppe: 'rechner', d: 'M8 3h13v13H8zM4 7v14h13M12 7h5v5h-5z' },
  { id: 'container', de: 'Container', en: 'Container', gruppe: 'rechner', d: 'M12 2l9 5v10l-9 5-9-5V7zM3 7l9 5 9-5M12 12v10' },
  { id: 'computer', de: 'Computer / PC', en: 'Computer / PC', gruppe: 'rechner', d: 'M3 4h18v12H3zM8 20h8M12 16v4' },
  { id: 'laptop', de: 'Laptop', en: 'Laptop', gruppe: 'rechner', d: 'M5 5h14v10H5zM2 19h20l-2-4H4z' },
  { id: 'einplatine', de: 'Raspberry Pi / Einplatinenrechner', en: 'Raspberry Pi / single-board computer', kurz: 'Raspberry Pi', gruppe: 'rechner', d: 'M3 6h18v12H3zM6 6V4M9 6V4M12 6V4M15 6V4M18 6V4M7 10h4v4H7zM15 10h3M15 14h3' },
  // Netzwerk
  { id: 'router', de: 'Router', en: 'Router', gruppe: 'netz', d: 'M3 13h18v6H3zM7 16h.01M11 16h.01M8 13L6 7M16 13l2-6' },
  { id: 'switch', de: 'Switch', en: 'Switch', gruppe: 'netz', d: 'M2 7h20v10H2zM5 11h2v2H5zM9 11h2v2H9zM13 11h2v2h-2zM17 11h2v2h-2z' },
  { id: 'firewall', de: 'Firewall', en: 'Firewall', gruppe: 'netz', d: 'M3 5h18v14H3zM3 10h18M3 15h18M9 5v5M15 10v5M9 15v4' },
  { id: 'nas', de: 'NAS / Speicher', en: 'NAS / storage', gruppe: 'netz', d: 'M5 3h14v18H5zM5 9h14M5 15h14M9 6h.01M9 12h.01M9 18h.01' },
  // Sonstiges
  { id: 'cloud', de: 'Cloud / VPS', en: 'Cloud / VPS', gruppe: 'sonst', d: 'M7 18h10a4 4 0 0 0 .5-8 6 6 0 0 0-11.5 1.5A3.5 3.5 0 0 0 7 18z' },
  { id: 'drucker', de: 'Drucker', en: 'Printer', gruppe: 'sonst', d: 'M6 9V3h12v6M6 17H4v-6h16v6h-2M6 14h12v7H6z' },
];
const GERAETE_GRUPPEN = [
  { id: 'industrie', de: 'Industrie', en: 'Industrial' },
  { id: 'rechner', de: 'Rechner', en: 'Computers' },
  { id: 'netz', de: 'Netzwerk', en: 'Network' },
  { id: 'sonst', de: 'Sonstiges', en: 'Other' },
];

function geraetFuer(id) {
  return GERAETE.find((g) => g.id === (id || '')) || GERAETE[0];
}

// Strich-Symbol aus einem Pfad (24x24), gleiche Darstellung wie icon() in basis.js.
function strichIcon(d, groesse) {
  const ns = 'http://www.w3.org/2000/svg';
  const s = document.createElementNS(ns, 'svg');
  s.setAttribute('viewBox', '0 0 24 24');
  s.setAttribute('class', 'ic');
  s.setAttribute('aria-hidden', 'true');
  if (groesse) { s.style.width = groesse + 'px'; s.style.height = groesse + 'px'; }
  const p = document.createElementNS(ns, 'path');
  p.setAttribute('d', d);
  s.append(p);
  return s;
}

function geraetIcon(id, groesse) {
  return strichIcon(geraetFuer(id).d, groesse);
}
