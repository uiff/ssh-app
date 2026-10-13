// ES-Modul-Bruecke: noVNC (vendor/novnc, MPL-2.0, unveraendert) ist in ES-Modulen geschrieben,
// Keyway in klassischen Skripten. js/vnc.js laedt dieses Modul beim ersten Bildschirm-Tab
// (oder vorab, wenn die Startseite einen VNC-Host zeigt) und bekommt danach window.KeywayRFB samt
// Ereignis «keyway-rfb». Kein Build-Schritt: der Browser loest die relativen Importe selbst auf.
import RFB from '../vendor/novnc/core/rfb.js';
import KeyTable from '../vendor/novnc/core/input/keysym.js';
import keysyms from '../vendor/novnc/core/input/keysymdef.js';
import { initLogging } from '../vendor/novnc/core/util/logging.js';

// noVNC schreibt erwartete Fehler (falsches Passwort am Geraet) als Fehler in die Konsole; Keyway
// zeigt sie selbst an. Zur Fehlersuche: localStorage «sshapp.vncProtokoll» = "debug" | "info" |
// "warn" | "error", danach neu laden.
let stufe = 'none';
try { stufe = JSON.parse(localStorage.getItem('sshapp.vncProtokoll') || '"none"'); } catch (e) { /* privat */ }
initLogging(['debug', 'info', 'warn', 'error'].includes(stufe) ? stufe : 'none');

window.KeywayRFB = { RFB, KeyTable, keysyms };
window.dispatchEvent(new Event('keyway-rfb'));
