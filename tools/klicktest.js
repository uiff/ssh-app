#!/usr/bin/env node
// klicktest.js — Durchlauf der Oberflaeche per Headless-Chrome (CDP):
// Einrichtung (oder Anmeldung), Zugang + Host anlegen, verbinden, Host-Schluessel
// bestaetigen, tippen, Dateien und Hilfe oeffnen, Palette — mit Fotos.
//
// Aufruf: node --experimental-websocket tools/klicktest.js <url> <ssh-host> <ssh-port> <ssh-user> <ssh-pw> <fotoordner> [breite hoehe]
// Braucht eine FRISCHE Instanz (nicht eingerichtet) oder SSHAPP_USER/SSHAPP_PW fuer die Anmeldung.
// Chrome: CHROME=/pfad, sonst Puppeteer-Cache (~/.cache/puppeteer/chrome-headless-shell).
'use strict';
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const [url, sshHost, sshPort, sshUser, sshPw, fotos, wStr, hStr] = process.argv.slice(2);
if (!url || !sshHost || !fotos) {
  console.error('Aufruf: node --experimental-websocket tools/klicktest.js <url> <ssh-host> <ssh-port> <ssh-user> <ssh-pw> <fotoordner> [breite hoehe]');
  process.exit(2);
}
const W = +wStr || 1440, H = +hStr || 900, MOBIL = W < 900;
const BENUTZER = process.env.SSHAPP_USER || 'admin', PASSWORT = process.env.SSHAPP_PW || 'klicktest-passwort';
const warte = (ms) => new Promise((r) => setTimeout(r, ms));

function chromePfad() {
  if (process.env.CHROME) return process.env.CHROME;
  const cache = path.join(process.env.HOME || '/root', '.cache/puppeteer/chrome-headless-shell');
  for (const v of fs.readdirSync(cache).sort().reverse()) {
    const p = path.join(cache, v, 'chrome-headless-shell-linux64/chrome-headless-shell');
    if (fs.existsSync(p)) return p;
  }
  throw new Error('kein Chrome gefunden — CHROME=/pfad setzen');
}

(async () => {
  fs.mkdirSync(fotos, { recursive: true });
  const port = 9533 + Math.floor(Math.random() * 100);
  const profil = fs.mkdtempSync(path.join(require('os').tmpdir(), 'sshapp-klicktest-'));
  const ch = spawn(chromePfad(), ['--headless=new', '--no-sandbox', '--disable-gpu', '--hide-scrollbars', '--ignore-certificate-errors',
    `--remote-debugging-port=${port}`, `--window-size=${W},${H}`, '--user-data-dir=' + profil, 'about:blank'], { stdio: 'ignore' });
  process.on('exit', () => { try { ch.kill('SIGKILL'); } catch (e) { /* */ } try { fs.rmSync(profil, { recursive: true, force: true }); } catch (e) { /* */ } });
  let ziel = null;
  for (let i = 0; i < 50 && !ziel; i++) {
    await warte(200);
    try { const t = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json(); if (t.length) ziel = t[0].webSocketDebuggerUrl; } catch (e) { /* */ }
  }
  if (!ziel) throw new Error('Chrome startet nicht');
  const ws = new WebSocket(ziel);
  await new Promise((res, rej) => { ws.onopen = res; ws.onerror = () => rej(new Error('CDP-WebSocket')); });
  let id = 0; const pend = new Map(); const ereignisse = [];
  ws.onmessage = (m) => {
    const d = JSON.parse(m.data);
    if (d.id && pend.has(d.id)) { pend.get(d.id)(d); pend.delete(d.id); } else if (d.method) ereignisse.push(d);
  };
  const cdp = (method, params = {}) => new Promise((res) => { const i = ++id; pend.set(i, res); ws.send(JSON.stringify({ id: i, method, params })); });
  const ev = async (expression) => {
    const r = await cdp('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true });
    if (r.result && r.result.exceptionDetails) throw new Error('Seite: ' + ((r.result.exceptionDetails.exception || {}).description || r.result.exceptionDetails.text));
    return r.result && r.result.result ? r.result.result.value : undefined;
  };
  let nr = 0;
  const foto = async (name) => {
    const r = await cdp('Page.captureScreenshot', { format: 'png' });
    const datei = path.join(fotos, String(++nr).padStart(2, '0') + '-' + name + '.png');
    fs.writeFileSync(datei, Buffer.from(r.result.data, 'base64'));
    console.log('Foto', datei);
  };
  const bis = async (ausdruck, ms = 8000, was) => {
    for (let t = 0; t < ms; t += 150) { if (await ev(ausdruck)) return true; await warte(150); }
    throw new Error('Zeitueberschreitung: ' + (was || ausdruck));
  };
  // Knopf nach sichtbarem Text klicken
  const klick = (text, wo = 'document') => ev(`(() => { const b = [...${wo}.querySelectorAll('button, a')].filter(x => x.offsetParent !== null && (x.textContent || '').trim().includes(${JSON.stringify(text)})).pop();
    if (!b) throw new Error('Knopf nicht gefunden: ' + ${JSON.stringify(text)}); b.click(); return true; })()`);
  const setze = (sel, wert) => ev(`(() => { const f = document.querySelector(${JSON.stringify(sel)}); if (!f) throw new Error('Feld fehlt: ${sel}');
    const proto = f.tagName === 'TEXTAREA' ? HTMLTextAreaElement.prototype : f.tagName === 'SELECT' ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(proto, 'value').set.call(f, ${JSON.stringify(wert)}); f.dispatchEvent(new Event('input', { bubbles: true })); f.dispatchEvent(new Event('change', { bubbles: true })); return true; })()`);

  await cdp('Page.enable');
  await cdp('Emulation.setDeviceMetricsOverride', { width: W, height: H, deviceScaleFactor: MOBIL ? 2 : 1, mobile: MOBIL });
  if (MOBIL) await cdp('Emulation.setTouchEmulationEnabled', { enabled: true, maxTouchPoints: 5 });
  await cdp('Page.navigate', { url });
  await bis('document.readyState === "complete"');
  await ev(`(() => { window.__kt = []; addEventListener('error', e => __kt.push('error: ' + e.message));
    addEventListener('unhandledrejection', e => __kt.push('unhandledrejection: ' + ((e.reason && e.reason.message) || e.reason)));
    const a = console.error.bind(console); console.error = (...x) => { __kt.push('console.error: ' + x.join(' ')); a(...x); }; return 1; })()`);
  await bis('!!document.querySelector("form, .kopf")', 8000, 'Startseite');

  if (await ev('!!document.getElementById("s-pw")')) {
    await foto('einrichten');
    await setze('#s-benutzer', BENUTZER); await setze('#s-pw', PASSWORT); await setze('#s-pw2', PASSWORT);
    await klick('Einrichten');
    await bis('!!document.querySelector(".schluessel-box")', 15000, 'Sicherheitsschluessel');
    await foto('sicherheitsschluessel');
    const sk = await ev('document.querySelector(".schluessel-box").textContent');
    fs.writeFileSync(path.join(fotos, 'sicherheitsschluessel.txt'), sk + '\n');
    await ev('document.querySelector(".einstieg input[type=checkbox]").click()');
    await klick('Weiter zur App');
  } else if (await ev('!!document.getElementById("a-pw")')) {
    await foto('anmelden');
    await setze('#a-benutzer', BENUTZER); await setze('#a-pw', PASSWORT);
    await klick('Anmelden');
  }
  await bis('!!document.querySelector(".kopf")', 15000, 'Arbeitsplatz');
  await warte(600);
  await foto('start-leer');

  // Zugang und Host anlegen (ueber die Dialoge)
  await ev('App.zugangBearbeiten({})');
  await setze('#z-name', 'testziel'); await setze('#z-benutzer', sshUser); await setze('#z-pw', sshPw);
  await foto('zugang-dialog');
  await klick('Speichern', 'document.querySelector(".dialog")');
  await bis('App.daten.zugaenge.length > 0', 5000, 'Zugang gespeichert');
  await ev('App.hostBearbeiten({})');
  await setze('#h-name', 'testziel'); await setze('#h-adresse', sshHost); await setze('#h-port', sshPort);
  await ev(`(() => { const s = document.getElementById('h-zugang'); s.value = String(App.daten.zugaenge[0].id); return 1; })()`);
  await setze('#h-tags', 'labor, kritisch');
  await foto('host-dialog');
  await klick('Speichern und verbinden', 'document.querySelector(".dialog")');

  // Host-Schluessel bestaetigen
  // Host-Schluessel: beim ersten Mal Rueckfrage, danach bekannt.
  await bis('!!document.querySelector(".dialog .fp") || (App.aktiv && App.aktiv.zustand === "bereit")', 15000, 'Host-Schluessel-Dialog oder Terminal');
  if (await ev('!!document.querySelector(".dialog .fp")')) {
    await foto('hostkey');
    await klick('Vertrauen und verbinden');
  }
  await bis('App.aktiv && App.aktiv.zustand === "bereit"', 15000, 'Terminal bereit');
  await warte(1200);
  await ev('App.aktiv.tippen("ls -la /etc | head -n 15\\r")');
  await warte(1000);
  await ev('App.aktiv.tippen("cd /etc && tar -xzf ")');
  await warte(700);
  await foto('terminal');

  // Dateien und Hilfe
  if (!MOBIL) {
    await ev('App.seiteUmschalten("dateien", true)');
    await warte(2500);
    await ev('App.seiteUmschalten("hilfe", true)');
    await warte(1500);
    await foto('dateien-hilfe');
  } else {
    await ev('App.seiteUmschalten("dateien", true)');
    await warte(2500);
    await foto('dateien-blatt');
    await ev('App.seiteUmschalten("hilfe", true)');
    await warte(1500);
    await foto('hilfe-blatt');
    await ev('App.seiteUmschalten("hilfe", false)');
    await ev('App.seiteUmschalten("links", true)');
    await warte(400);
    await foto('hosts-blatt');
    await ev('App.seiteUmschalten("links", false)');
  }
  const pfad = await ev('App.dateien.pfad');
  console.log('Dateileiste zeigt:', pfad);

  // Palette
  await ev('App.aktiv.tippen("\\x15")'); // Zeile leeren
  await ev('App.palette("grosse dateien finden")');
  await warte(1500);
  await foto('palette');
  if (MOBIL) {
    await ev('document.querySelector(".treffer:not(.gruppe-titel)") && [...document.querySelectorAll(".palette-liste .treffer")].pop().click()');
    await warte(900);
    await foto('palette-detail');
  }
  await ev('App.paletteObj.schliessen()');

  // Einstellungen
  await ev('App.einstellungen()');
  await warte(500);
  await foto('einstellungen');
  await ev('document.querySelector(".dialog .dialog-kopf .ikon").click()');

  // Neuladen: Sitzung muss wieder angehaengt werden
  await cdp('Page.reload');
  await warte(500);
  await bis('document.readyState === "complete" && window.App && App.tabs && App.tabs.length > 0 && App.aktiv && App.aktiv.zustand === "bereit"', 15000, 'Wiederanhaengen nach Neuladen');
  await warte(800);
  await foto('nach-neuladen');

  const fehler = await ev('window.__kt || []');
  const ueberlauf = await ev('document.documentElement.scrollWidth > innerWidth + 1');
  console.log('Fehler im Browser:', fehler.length ? fehler : 'keine');
  console.log('Waagrechter Ueberlauf:', ueberlauf ? 'JA' : 'nein');
  process.exit(fehler.length || ueberlauf ? 1 : 0);
})().catch((e) => { console.error('ABBRUCH:', e.message); process.exit(1); });
