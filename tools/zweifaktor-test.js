#!/usr/bin/env node
// zweifaktor-test.js — Zwei-Faktor-Anmeldung (TOTP) Ende zu Ende per Headless-Chrome (CDP),
// gegen einen echten Server, mit echten Codes (HMAC-SHA1 hier im Skript gerechnet):
// Konto einrichten (oder anmelden), Einstellungen (Gruppe aus), Einrichten auf Telefon und
// Desktop (QR-Code, otpauth-Link, Schluessel, Serverzeit), falsches Passwort, Uhrabweichung,
// Einschalten, Ersatzcodes (10, XXXX-XXXX, «Fertig» erst nach dem Haken), Abmelden, Anmelden mit
// Code (drei falsche Codes, richtiger Code mit Warnung zu den falschen, wiederverwendeter Code),
// Anmelden mit Ersatzcode (Rest im Toast), neue Ersatzcodes (alte ungueltig), Ausschalten mit
// Ersatzcode, Anmelden ohne Code, zweites «Einrichten» mit demselben Schluessel, Doppelklick ohne
// zweiten Dialog, falsches Passwort markiert das Passwort (Code bleibt), Fehler auf dem Telefon in
// den Blick gerollt, Hinweis auf der Seite «Passwort vergessen?» und — wenn der
// Sicherheitsschluessel bekannt ist — Zuruecksetzen mit eingeschalteter Zwei-Faktor-Anmeldung
// (danach ist sie aus).
// Geprueft werden auch: Tresor und Anmelde-Cookie erst nach dem zweiten Faktor, Trefferflaechen
// >= 44 px und kein waagrechter Ueberlauf auf dem Telefon (390x844), keine Skriptfehler, keine
// fehlenden Texte im Sprachkatalog und (SPRACHE=en) keine deutschen Texte in der Oberflaeche.
//
// Aufruf: node --experimental-websocket tools/zweifaktor-test.js <url> <fotoordner>
// Beispiel (frische Instanz ohne TLS):
//   SSHAPP_DATA=/tmp/zf SSHAPP_ADDR=127.0.0.1:18460 SSHAPP_TLS=aus ./ssh-app &
//   node --experimental-websocket tools/zweifaktor-test.js http://127.0.0.1:18460 /tmp/zf-fotos
// Dauer etwa 2-3 Minuten: Der Server nimmt jeden Code-Zeitschritt nur einmal an, das Skript
// wartet dann auf den naechsten 30-s-Schritt.
// Wiederholbar: Eine frische Instanz wird eingerichtet; eine eingerichtete braucht SSHAPP_USER/
// SSHAPP_PW (Vorgabe admin / zweifaktor-passwort). Am Ende ist die Zwei-Faktor-Anmeldung wieder
// aus. Brach ein Lauf mittendrin ab, nimmt der naechste den im Fotoordner abgelegten Schluessel
// (zweifaktor-schluessel.txt, nur Testdaten) und schaltet sie zuerst aus. sicherheitsschluessel.txt
// im Fotoordner (vom Einrichten) erlaubt den Zuruecksetzen-Teil auch in spaeteren Laeufen.
// SPRACHE=de|en: Sprache der Oberflaeche. Chrome: CHROME=/pfad, sonst Puppeteer-Cache
// (~/.cache/puppeteer/chrome-headless-shell); CDP_PORT=<port>: fester Port fuer die Fernsteuerung.
// Die Uhr des Servers muss mit der dieses Rechners uebereinstimmen (gleicher Rechner: ja).
'use strict';
const { spawn } = require('child_process');
const crypto = require('crypto');
const fs = require('fs');
const path = require('path');

const [url, fotos] = process.argv.slice(2);
if (!url || !fotos) {
  console.error('Aufruf: node --experimental-websocket tools/zweifaktor-test.js <url> <fotoordner>');
  process.exit(2);
}
const BENUTZER = process.env.SSHAPP_USER || 'admin', PASSWORT = process.env.SSHAPP_PW || 'zweifaktor-passwort';
const SPRACHE = process.env.SPRACHE || '', LANG = SPRACHE === 'en' ? 'en-US' : SPRACHE === 'de' ? 'de-CH' : '';
const DESKTOP = { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false };
const TELEFON = { width: 390, height: 844, deviceScaleFactor: 2, mobile: true };
const SCHLUESSEL_DATEI = path.join(fotos, 'zweifaktor-schluessel.txt');
const ERSATZ_DATEI = path.join(fotos, 'zweifaktor-ersatzcodes.txt');
const SK_DATEI = path.join(fotos, 'sicherheitsschluessel.txt');
const warte = (ms) => new Promise((r) => setTimeout(r, ms));

// --- TOTP (RFC 6238: HMAC-SHA1, 6 Stellen, 30 s) ---
function base32Lesen(s) {
  const A = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
  let bits = '';
  for (const c of s.replace(/[\s=]/g, '').toUpperCase()) {
    const i = A.indexOf(c);
    if (i < 0) throw new Error('kein Base32: ' + s);
    bits += i.toString(2).padStart(5, '0');
  }
  const out = [];
  for (let i = 0; i + 8 <= bits.length; i += 8) out.push(parseInt(bits.slice(i, i + 8), 2));
  return Buffer.from(out);
}
function codeBei(geheimnis, schritt) {
  const z = Buffer.alloc(8);
  z.writeBigUInt64BE(BigInt(schritt));
  const h = crypto.createHmac('sha1', geheimnis).update(z).digest();
  const o = h[19] & 15;
  const n = (((h[o] & 0x7f) << 24) | (h[o + 1] << 16) | (h[o + 2] << 8) | h[o + 3]) % 1000000;
  return String(n).padStart(6, '0');
}
const jetztSchritt = () => Math.floor(Date.now() / 30000);
// Ein Code, der zu keinem Schritt in +-45 passt (sonst kaeme der Hinweis auf die Uhr).
function falscherCode(geheimnis) {
  const s = jetztSchritt(), nah = new Set();
  for (let d = -45; d <= 45; d++) nah.add(codeBei(geheimnis, s + d));
  for (let c = 123456; ; c = (c + 111111) % 1000000) { const x = String(c).padStart(6, '0'); if (!nah.has(x)) return x; }
}

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
  const port = +process.env.CDP_PORT || 9633 + Math.floor(Math.random() * 100);
  const profil = fs.mkdtempSync(path.join(require('os').tmpdir(), 'sshapp-zweifaktor-'));
  const ch = spawn(chromePfad(), ['--headless=new', '--no-sandbox', '--disable-gpu', '--hide-scrollbars', '--ignore-certificate-errors',
    `--remote-debugging-port=${port}`, `--window-size=${DESKTOP.width},${DESKTOP.height}`, '--user-data-dir=' + profil,
    ...(LANG ? ['--lang=' + LANG] : []), 'about:blank'], { stdio: 'ignore' });
  process.on('exit', () => { try { ch.kill('SIGKILL'); } catch (e) { /* */ } try { fs.rmSync(profil, { recursive: true, force: true }); } catch (e) { /* */ } });
  let ziel = null;
  for (let i = 0; i < 50 && !ziel; i++) {
    await warte(200);
    try { const t = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json(); if (t.length) ziel = t[0].webSocketDebuggerUrl; } catch (e) { /* */ }
  }
  if (!ziel) throw new Error('Chrome startet nicht');
  const ws = new WebSocket(ziel);
  await new Promise((res, rej) => { ws.onopen = res; ws.onerror = () => rej(new Error('CDP-WebSocket')); });
  let id = 0; const pend = new Map();
  ws.onmessage = (m) => { const d = JSON.parse(m.data); if (d.id && pend.has(d.id)) { pend.get(d.id)(d); pend.delete(d.id); } };
  const cdp = (method, params = {}) => new Promise((res) => { const i = ++id; pend.set(i, res); ws.send(JSON.stringify({ id: i, method, params })); });
  const ev = async (expression) => {
    const r = await cdp('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true });
    if (r.result && r.result.exceptionDetails) throw new Error('Seite: ' + ((r.result.exceptionDetails.exception || {}).description || r.result.exceptionDetails.text));
    return r.result && r.result.result ? r.result.result.value : undefined;
  };
  let nr = 0;
  const foto = async (name) => {
    await warte(250);
    const r = await cdp('Page.captureScreenshot', { format: 'png' });
    const datei = path.join(fotos, String(++nr).padStart(2, '0') + '-' + name + '.png');
    fs.writeFileSync(datei, Buffer.from(r.result.data, 'base64'));
    console.log('Foto', datei);
  };
  const bis = async (ausdruck, ms = 8000, was) => {
    for (let t = 0; t < ms; t += 150) { if (await ev(ausdruck)) return true; await warte(150); }
    throw new Error('Zeitueberschreitung: ' + (was || ausdruck));
  };
  const probleme = [];
  const pruef = (ok, text) => { console.log((ok ? 'ok      ' : 'PROBLEM ') + text); if (!ok) probleme.push(text); return ok; };
  const T = (schluessel, w) => ev(`t(${JSON.stringify(schluessel)}${w ? ', ' + JSON.stringify(w) : ''})`);
  // Ausdruecke fuer Behaelter: oberster Dialog, Gruppe «Zwei-Faktor-Anmeldung» in den Einstellungen.
  const OBEN = '[...document.querySelectorAll(".dialog")].pop()';
  const GRUPPE = '[...document.querySelectorAll(".dialog .einstellungen-gruppe")].find((g) => (g.querySelector("h4") || {}).textContent === t("hosts.tf.titel"))';
  // Knopf (oder Link) mit genau diesem Katalogtext im Behaelter klicken.
  const klickT = (schluessel, wo = 'document') => ev(`(() => { const s = t(${JSON.stringify(schluessel)});
    const b = [...(${wo}).querySelectorAll('button, a')].filter((x) => x.offsetParent !== null && (x.textContent || '').trim() === s).pop();
    if (!b) throw new Error('Knopf nicht gefunden: ' + s); b.click(); return true; })()`);
  const setze = (sel, wert) => ev(`(() => { const f = document.querySelector(${JSON.stringify(sel)}); if (!f) throw new Error('Feld fehlt: ${sel}');
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(f, ${JSON.stringify(wert)});
    f.dispatchEvent(new Event('input', { bubbles: true })); return true; })()`);
  // Tippen wie auf der Tastatur (ein input-Ereignis mit dem ganzen Text, wie Einfuegen/Autofill).
  const tippen = async (sel, text) => {
    await ev(`(() => { const f = document.querySelector(${JSON.stringify(sel)}); if (!f) throw new Error('Feld fehlt: ${sel}'); f.focus(); f.value = ''; return 1; })()`);
    await cdp('Input.insertText', { text });
    await warte(150);
  };
  const ansicht = async (a) => {
    await cdp('Emulation.setDeviceMetricsOverride', a);
    await cdp('Emulation.setTouchEmulationEnabled', { enabled: a.mobile, maxTouchPoints: a.mobile ? 5 : 1 });
    await warte(400);
  };
  const dialogeZu = () => ev(`(async () => { for (let i = 0; i < 6; i++) { const d = ${OBEN}; if (!d) break;
    const x = d.querySelector('.dialog-kopf .ikon'); if (!x) break; x.click(); await new Promise((r) => setTimeout(r, 200)); } return 1; })()`);
  const toasts = () => ev('[...document.querySelectorAll("#toasts .toast")].map((x) => (x.classList.contains("warn") ? "[warn] " : "") + x.textContent)');
  const toastsWeg = () => ev('document.querySelectorAll("#toasts .toast").forEach((x) => x.remove())');
  const status = () => ev('fetch("/api/status").then((r) => r.json())');
  const sitzungsCookie = async () => ((await cdp('Network.getCookies', { urls: [url] })).result.cookies || []).some((c) => c.name === 'sshapp');
  const meldungFehler = (wo = 'document') => ev(`(() => { const m = [...(${wo}).querySelectorAll('.meldung.fehler')].filter((x) => x.offsetParent !== null).pop(); return m ? m.textContent : ''; })()`);
  const fehlerFrisch = async (wo, ms = 8000) => { await bis(`!!(${wo}) && [...(${wo}).querySelectorAll('.meldung.fehler')].some((x) => x.offsetParent !== null)`, ms, 'Fehlermeldung'); return meldungFehler(wo); };

  // Trefferflaechen (Telefon) und Ueberlauf in einem Behaelter.
  const flaechen = async (wo, name) => {
    if (!(await ev('App.istTelefon()'))) return;
    const klein = await ev(`(() => { const aus = []; const w = ${wo};
      for (const e of w.querySelectorAll('button, a, input:not([type=checkbox]):not([type=hidden]), select, textarea, label.check')) {
        if (e.closest('.versteckt') || e.classList.contains('versteckt')) continue;
        const r = e.getBoundingClientRect(); if (!r.width || !r.height || r.bottom < 0 || r.top > innerHeight || r.right < 0 || r.left > innerWidth) continue;
        let h = r.height, b = r.width; const n = getComputedStyle(e, '::after');
        if (n.content && n.content !== 'none' && n.position === 'absolute') { h -= (parseFloat(n.top) || 0) + (parseFloat(n.bottom) || 0); b -= (parseFloat(n.left) || 0) + (parseFloat(n.right) || 0); }
        if (h < 43.5 || b < 43.5) aus.push(((e.getAttribute('aria-label') || e.textContent || e.placeholder || '').trim().slice(0, 24) || e.tagName) + ' ' + Math.round(b) + 'x' + Math.round(h));
      } return aus; })()`);
    pruef(!klein.length, 'Trefferflaechen >= 44 px (' + name + ')' + (klein.length ? ': ' + klein.join(' | ') : ''));
    const ueber = await ev(`document.documentElement.scrollWidth > innerWidth + 1 || [...document.querySelectorAll('.dialog')].some((d) => d.scrollWidth > d.clientWidth + 1)`);
    pruef(!ueber, 'kein waagrechter Ueberlauf (' + name + ')');
  };
  const deutschFunde = new Set();
  const deutsch = async (wo) => {
    if ((await ev('I18N.sprache')) !== 'en') return;
    const funde = await ev(`(() => {
      const re = /[äöüÄÖÜ]|\\b(und|oder|nicht|mit|für|bei|der|die|das|den|dem|eine?|ist|wird|Einstellungen|Tresor|Neue[rns]?|Anmeld\\w*|Abmelden|Speichern|Passwort|Benutzer\\w*|Schlüssel|Fehler|Zurück|Weiter|Ersatz\\w*|Zwei\\w*|Einrichten|Einschalten|Ausschalten|Uhrzeit)\\b/;
      const weg = (e) => !e || e.closest('code, kbd, .fp, .schluessel-box, .tf-schluessel, .tf-codes, [lang=de]');
      const aus = new Set();
      const w = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
      for (let n = w.nextNode(); n; n = w.nextNode()) {
        const e = n.parentElement, s = n.textContent.trim();
        if (!s || weg(e) || !(e.offsetParent || getComputedStyle(e).position === 'fixed')) continue;
        if (re.test(s)) aus.add('Text: ' + s.slice(0, 80));
      }
      for (const e of document.querySelectorAll('[aria-label], [title], [placeholder], img[alt]')) {
        if (weg(e)) continue;
        for (const a of ['aria-label', 'title', 'placeholder', 'alt']) { const v = e.getAttribute(a); if (v && re.test(v)) aus.add(a + ': ' + v.slice(0, 80)); }
      }
      return [...aus]; })()`);
    for (const f of funde) if (!deutschFunde.has(f)) { deutschFunde.add(f); console.log('Deutsch im englischen Modus (' + wo + '):', f); }
  };

  // Codes: Der Server nimmt jeden Zeitschritt nur einmal an. letzterSchritt = zuletzt angenommen.
  let geheimnis = null, letzterSchritt = 0;
  const frischerCode = async () => {
    if (jetztSchritt() <= letzterSchritt) {
      const ms = (letzterSchritt + 1) * 30000 - Date.now() + 300;
      console.log('… warte ' + Math.ceil(ms / 1000) + ' s auf den naechsten Code-Zeitschritt');
      await warte(ms);
    }
    const s = jetztSchritt();
    return { code: codeBei(geheimnis, s), schritt: s };
  };

  await cdp('Page.enable');
  await cdp('Network.enable');
  await ansicht(DESKTOP);
  await cdp('Page.navigate', { url });
  await bis('document.readyState === "complete"');
  const fehlerFangen = () => ev(`(() => { if (window.__kt) return 1; window.__kt = []; addEventListener('error', (e) => __kt.push('error: ' + e.message));
    addEventListener('unhandledrejection', (e) => __kt.push('unhandledrejection: ' + ((e.reason && e.reason.message) || e.reason)));
    const a = console.error.bind(console); console.error = (...x) => { __kt.push('console.error: ' + x.join(' ')); a(...x); }; return 1; })()`);
  await fehlerFangen();
  await bis('!!document.querySelector("form, .kopf")', 8000, 'Startseite');
  if (SPRACHE) {
    await ev(`(() => { localStorage.setItem('sshapp.sprache', JSON.stringify(${JSON.stringify(SPRACHE)})); return 1; })()`);
    await cdp('Page.reload');
    await warte(300);
    await bis('document.readyState === "complete" && !!document.querySelector("form, .kopf")', 8000, 'Startseite nach Sprachwahl');
    await fehlerFangen();
  }
  console.log('Sprache:', await ev('I18N.sprache'));

  // --- 1. Konto einrichten oder anmelden ---
  let sicherheitsschluessel = fs.existsSync(SK_DATEI) ? fs.readFileSync(SK_DATEI, 'utf8').trim() : '';
  if (await ev('!!document.getElementById("s-pw")')) {
    await setze('#s-benutzer', BENUTZER); await setze('#s-pw', PASSWORT); await setze('#s-pw2', PASSWORT);
    await klickT('app.einrichten.knopf');
    await bis('!!document.querySelector(".schluessel-box")', 15000, 'Sicherheitsschluessel');
    sicherheitsschluessel = (await ev('document.querySelector(".schluessel-box").textContent')).trim();
    fs.writeFileSync(SK_DATEI, sicherheitsschluessel + '\n');
    await ev('document.querySelector(".einstieg input[type=checkbox]").click()');
    await klickT('app.schluessel.weiter');
    console.log('Konto eingerichtet:', BENUTZER);
  } else if (await ev('!!document.getElementById("a-pw")')) {
    await setze('#a-benutzer', BENUTZER); await setze('#a-pw', PASSWORT);
    await klickT('app.anmelden.knopf');
    await bis('!!document.querySelector(".kopf") || !!document.getElementById("a-code") || [...document.querySelectorAll(".meldung.fehler")].length > 0', 15000, 'Anmeldung');
    if (await ev('!!document.getElementById("a-code")')) {
      // Ein frueherer Lauf brach mit eingeschalteter Zwei-Faktor-Anmeldung ab: Schluessel aus dem Fotoordner.
      if (!fs.existsSync(SCHLUESSEL_DATEI)) throw new Error('Zwei-Faktor-Anmeldung ist eingeschaltet, aber ' + SCHLUESSEL_DATEI + ' fehlt');
      geheimnis = base32Lesen(fs.readFileSync(SCHLUESSEL_DATEI, 'utf8'));
      for (let i = 0; i < 3 && !(await ev('!!document.querySelector(".kopf")')); i++) {
        const { code, schritt } = await frischerCode();
        await tippen('#a-code', code);
        await warte(2500);
        letzterSchritt = schritt; // angenommen oder schon verbraucht: so oder so der naechste
      }
      console.log('Aufraeumen: angemeldet mit dem Schluessel eines frueheren Laufs');
    }
  }
  await bis('!!document.querySelector(".kopf")', 15000, 'Arbeitsplatz');
  await warte(500);
  if ((await status()).zweitfaktor) {
    // Aufraeumen: ausschalten, damit der Lauf mit «aus» beginnt.
    if (!geheimnis) throw new Error('Zwei-Faktor-Anmeldung ist eingeschaltet, Schluessel unbekannt');
    const { code, schritt } = await frischerCode();
    await ev(`api('POST', '/api/zweitfaktor/ausschalten', { passwort: ${JSON.stringify(PASSWORT)}, code: ${JSON.stringify(code)} }, { antwort401: true })`);
    letzterSchritt = schritt;
    await ev('(async () => { App.status = await api("GET", "/api/status"); return 1; })()');
    console.log('Aufraeumen: Zwei-Faktor-Anmeldung eines frueheren Laufs ausgeschaltet');
  }
  const st0 = await status();
  pruef(st0.angemeldet && st0.zweitfaktor === false && st0.ersatzcodesUebrig === undefined, 'Vorgabe: Zwei-Faktor-Anmeldung aus (/api/status zweitfaktor:false)');

  // --- 2. Einstellungen, Gruppe «aus» (Desktop, Telefon) ---
  const einstellungenOeffnen = async () => {
    await dialogeZu();
    await ev('App.einstellungen()');
    await bis(`!!(${GRUPPE})`, 5000, 'Gruppe Zwei-Faktor-Anmeldung');
    await warte(400); // der Dialog setzt den Fokus (und rollt) nach dem Oeffnen selbst
    await ev(`(${GRUPPE}).scrollIntoView({ block: 'start', behavior: 'instant' })`);
    await warte(300);
  };
  await einstellungenOeffnen();
  pruef(await ev(`(() => { const g = ${GRUPPE}; const v = g.previousElementSibling; return !!v && [...v.querySelectorAll('button')].some((b) => b.textContent.trim() === t('hosts.einst.pwAendern')); })()`),
    'Gruppe steht direkt nach der Passwort-Gruppe');
  const textAus = await ev(`(${GRUPPE}).textContent`);
  pruef(textAus.includes(await T('hosts.tf.aus')) && /Aegis/.test(textAus) && !!(await ev(`[...(${GRUPPE}).querySelectorAll('button')].some((b) => b.textContent.trim() === t('hosts.tf.einrichten'))`)),
    'Gruppe aus: Erklaerung (optional, offline, Aegis …), Stand «aus», Knopf Einrichten');
  await foto('einstellungen-aus-desktop');
  await deutsch('Einstellungen aus');

  await ansicht(TELEFON);
  await einstellungenOeffnen();
  await foto('einstellungen-aus-telefon');
  await flaechen(GRUPPE, 'Einstellungen aus, Telefon');

  // --- 3. Einrichten auf dem Telefon: QR-Code, Link, Schluessel, Serverzeit ---
  await klickT('hosts.tf.einrichten', GRUPPE);
  await bis(`!!document.querySelector('.tf-qr img') && document.querySelector('.tf-qr img').complete`, 8000, 'Einrichten-Dialog');
  await warte(300);
  await foto('einrichten-telefon');
  const e1 = await ev(`(() => { const d = ${OBEN}; const img = d.querySelector('.tf-qr img'); const a = d.querySelector('.tf-oeffnen a');
    const c = d.querySelector('#tf-e-code');
    return { qrB: img.naturalWidth, qrSrc: img.getAttribute('src').split(',')[0], alt: img.alt, href: a && a.getAttribute('href'), sk: d.querySelector('.tf-schluessel').textContent,
      uhr: (d.querySelector('.tf-uhr') || {}).textContent || '', warn: !!d.querySelector('.meldung.warn'),
      inputmode: c.getAttribute('inputmode'), ac: c.getAttribute('autocomplete'), weiss: getComputedStyle(d.querySelector('.tf-qr')).backgroundColor }; })()`);
  pruef(e1.qrB > 0 && e1.qrSrc === 'data:image/svg+xml;base64' && e1.alt === (await T('hosts.tf.qrAlt')), 'QR-Code geladen (SVG als data:-URI unter der CSP, Alternativtext; Breite ' + e1.qrB + ', ' + e1.qrSrc + ')');
  pruef(/^rgb\(255, 255, 255/.test(e1.weiss), 'QR-Code auf weissem Grund (' + e1.weiss + ')');
  pruef(/^([A-Z2-7]{4} ){7}[A-Z2-7]{4}$/.test(e1.sk), 'Schluessel: 32 Zeichen Base32 in Vierergruppen (' + e1.sk + ')');
  pruef(/\d{1,2}:\d{2}:\d{2}/.test(e1.uhr), 'Serverzeit sichtbar (' + e1.uhr + ')');
  pruef(!e1.warn, 'keine Warnung zur Uhrabweichung (gleiche Uhr)');
  pruef(e1.inputmode === 'numeric' && e1.ac === 'one-time-code', 'Codefeld: inputmode=numeric, autocomplete=one-time-code');
  {
    const u = new URL(e1.href);
    const p = u.searchParams;
    pruef(u.protocol === 'otpauth:' && u.host === 'totp' && decodeURIComponent(u.pathname).startsWith('/Keyway:' + BENUTZER + '@127.0.0.1') &&
      p.get('secret') === e1.sk.replace(/ /g, '') && p.get('issuer') === 'Keyway' && p.get('algorithm') === 'SHA1' && p.get('digits') === '6' && p.get('period') === '30',
    'otpauth-Link passt zum Schluessel (' + e1.href.replace(/secret=[A-Z2-7]+/, 'secret=…') + ')');
  }
  await flaechen(OBEN, 'Einrichten, Telefon');
  await deutsch('Einrichten');
  // «Einschalten» ohne Code: Das Codefeld liegt unter dem Rand; der Fehler rollt es (ohne Fokus,
  // die Tastatur bleibt zu) in den Blick, samt Meldung.
  {
    const unsichtbar = await ev(`(() => { const d = ${OBEN}; d.scrollTop = 0; for (const x of d.querySelectorAll('*')) if (x.scrollTop) x.scrollTop = 0;
      const r = document.getElementById('tf-e-code').getBoundingClientRect(); const f = d.querySelector('.dialog-fuss').getBoundingClientRect(); return r.top >= f.top || r.bottom <= 0; })()`);
    await klickT('hosts.tf.einschalten', OBEN);
    await warte(900);
    const sicht = await ev(`(() => { const d = ${OBEN}; const c = document.getElementById('tf-e-code'); const r = c.getBoundingClientRect();
      const f = d.querySelector('.dialog-fuss').getBoundingClientRect(); const m = [...d.querySelectorAll('.meldung.fehler')].pop();
      const mr = m ? m.getBoundingClientRect() : null;
      return { feld: r.top >= 0 && r.bottom <= f.top, meldung: !!mr && mr.top >= 0 && mr.bottom <= f.top, markiert: c.getAttribute('aria-invalid') === 'true', fokus: document.activeElement === c }; })()`);
    pruef(sicht.feld && sicht.meldung && sicht.markiert && !sicht.fokus,
      'Telefon, Einschalten ohne Code: Feld und Meldung in den Blick gerollt, markiert, ohne Fokus' + (unsichtbar ? '' : ' (Feld war schon sichtbar)') + ' ' + JSON.stringify(sicht));
    await foto('einrichten-telefon-fehler');
  }
  // Schritt 2 (Code + Passwort) unten im Dialog
  await ev(`(() => { const d = ${OBEN}; d.scrollTop = d.scrollHeight; return 1; })()`);
  await foto('einrichten-telefon-schritt2');
  await flaechen(OBEN, 'Einrichten Schritt 2, Telefon');
  await dialogeZu();

  // --- 4. Einrichten auf dem Desktop: falsches Passwort, Uhrabweichung, dann einschalten ---
  await ansicht(DESKTOP);
  await einstellungenOeffnen();
  // Doppelklick auf «Einrichten»: ein einziger Dialog (der Knopf ist waehrend der Anfrage gesperrt).
  await ev(`(() => { const s = t('hosts.tf.einrichten'); const b = [...(${GRUPPE}).querySelectorAll('button')].find((x) => x.textContent.trim() === s);
    b.click(); b.click(); return 1; })()`);
  await bis(`!!document.querySelector('.tf-qr img') && document.querySelector('.tf-qr img').complete`, 8000, 'Einrichten-Dialog (Desktop)');
  await warte(600);
  pruef((await ev(`document.querySelectorAll('.dialog .tf-qr').length`)) === 1, 'Doppelklick auf Einrichten: ein einziger Dialog');
  await foto('einrichten-desktop');
  pruef(await ev(`(() => { const r = document.querySelector('.tf-qr img').getBoundingClientRect(); const f = (${OBEN}).querySelector('.dialog-fuss').getBoundingClientRect(); return r.bottom <= f.top && r.top >= 0; })()`),
    'Desktop: QR-Code ohne Rollen sichtbar');
  const schluessel = (await ev('document.querySelector(".tf-schluessel").textContent')).trim();
  // Dieselbe Anmeldung hat oben (Telefon) schon einmal eingerichtet: derselbe Schluessel, ein
  // schon gescannter Eintrag in der Authenticator-App bliebe gueltig.
  pruef(schluessel === e1.sk.trim(), 'zweites Einrichten: derselbe Schluessel wie beim ersten (' + schluessel.slice(0, 9) + '…)');
  geheimnis = base32Lesen(schluessel);
  pruef(geheimnis.length === 20, 'Geheimnis 20 Byte');
  fs.writeFileSync(SCHLUESSEL_DATEI, schluessel + '\n');
  // Falsches Passwort: Passwort zuerst, dann der Code — die 6. Ziffer sendet ab.
  await setze('#tf-e-pw', 'falsches-passwort-123');
  await tippen('#tf-e-code', codeBei(geheimnis, jetztSchritt()));
  const fPw = await fehlerFrisch(OBEN);
  pruef(fPw.length > 0 && (await ev(`!!document.getElementById('tf-e-code')`)), 'Einrichten, falsches Passwort: Meldung im Dialog (' + fPw + ')');
  {
    const f = await ev(`(() => { const p = document.getElementById('tf-e-pw'), c = document.getElementById('tf-e-code');
      return { pwLeer: p.value === '', pwMarkiert: p.getAttribute('aria-invalid') === 'true', pwFokus: document.activeElement === p, code: c.value, codeMarkiert: c.getAttribute('aria-invalid') === 'true' }; })()`);
    pruef(f.pwLeer && f.pwMarkiert && f.pwFokus && f.code.length === 6 && !f.codeMarkiert,
      'falsches Passwort: Passwort geleert, markiert, im Fokus; der Code bleibt stehen ' + JSON.stringify(f));
  }
  pruef(!(await status()).zweitfaktor, 'nach falschem Passwort weiter aus');
  // Uhr des Telefons 5 min vor: Hinweis mit Minuten
  await setze('#tf-e-pw', PASSWORT);
  await tippen('#tf-e-code', codeBei(geheimnis, jetztSchritt() + 10));
  await bis(`(() => { const m = [...(${OBEN}).querySelectorAll('.meldung.fehler')].pop(); return !!m && m.textContent !== ${JSON.stringify(fPw)}; })()`, 8000, 'Hinweis Uhrabweichung');
  const fUhr = await meldungFehler(OBEN);
  pruef(/\b5 min\b/.test(fUhr), 'Uhrabweichung: Hinweis nennt etwa 5 min (' + fUhr + ')');
  pruef(!/Ersatzcode|backup code/i.test(fUhr), 'Uhrabweichung beim Einrichten: kein Rat zu Ersatzcodes (es gibt noch keine)');
  await foto('einrichten-uhrabweichung');
  // Richtig: Code des aktuellen Schritts + Passwort -> Ersatzcodes
  await setze('#tf-e-pw', PASSWORT);
  {
    const s = jetztSchritt();
    await tippen('#tf-e-code', codeBei(geheimnis, s));
    letzterSchritt = s;
  }
  await bis('!!document.querySelector(".tf-codes")', 10000, 'Ersatzcodes');
  await warte(300);

  // --- 5. Ersatzcodes ---
  await foto('ersatzcodes');
  let ersatz = await ev('[...document.querySelectorAll(".tf-codes span")].map((s) => s.textContent)');
  fs.writeFileSync(ERSATZ_DATEI, ersatz.join('\n') + '\n');
  const ersatzForm = /^[2-9A-HJ-NP-Z]{4}-[2-9A-HJ-NP-Z]{4}$/;
  pruef(ersatz.length === 10 && ersatz.every((c) => ersatzForm.test(c)) && new Set(ersatz).size === 10, '10 verschiedene Ersatzcodes XXXX-XXXX aus dem vereinbarten Alphabet');
  pruef(await ev(`(() => { const s = [...document.querySelectorAll('.tf-codes span')]; return s.length > 1 && Math.round(s[0].getBoundingClientRect().top) === Math.round(s[1].getBoundingClientRect().top) && getComputedStyle(s[0]).fontFamily.toLowerCase().includes('mono'); })()`),
    'Ersatzcodes zweispaltig, Festbreitenschrift');
  const fertigAus = await ev(`(() => { const s = t('hosts.tf.fertig'); return [...document.querySelectorAll('.dialog button')].find((b) => b.textContent.trim() === s).disabled; })()`);
  await ev(`document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))`);
  await warte(300);
  pruef(fertigAus && (await ev('!!document.querySelector(".tf-codes")')), '«Fertig» gesperrt bis zum Haken, Escape schliesst nicht');
  pruef(await ev(`[...document.querySelectorAll('.dialog button')].some((b) => b.textContent.trim() === t('hosts.tf.kopieren')) && [...document.querySelectorAll('.dialog button')].some((b) => b.textContent.trim() === t('hosts.tf.speichern'))`),
    'Knoepfe Kopieren und Als Datei speichern');
  await deutsch('Ersatzcodes');
  await ev(`(${OBEN}).querySelector('input[type=checkbox]').click()`);
  await klickT('hosts.tf.fertig', OBEN);
  await warte(300);
  pruef(!(await ev('!!document.querySelector(".tf-codes")')), 'Ersatzcodes-Ansicht nach dem Haken geschlossen');

  // --- 6. Einstellungen, Gruppe «an» ---
  const st1 = await status();
  pruef(st1.zweitfaktor === true && st1.ersatzcodesUebrig === 10, '/api/status: zweitfaktor:true, ersatzcodesUebrig:10');
  await warte(300);
  await ev(`(${GRUPPE}).scrollIntoView({ block: 'start', behavior: 'instant' })`);
  const anText = await T('hosts.tf.an', { n: 10 });
  pruef((await ev(`(${GRUPPE}).textContent`)).includes(anText), 'Gruppe an: «' + anText + '»');
  pruef(await ev(`(() => { const b = [...(${GRUPPE}).querySelectorAll('button')]; return b.some((x) => x.textContent.trim() === t('hosts.tf.neueCodes')) && b.some((x) => x.textContent.trim() === t('hosts.tf.ausschalten') && x.classList.contains('gefahr')); })()`),
    'Knoepfe Neue Ersatzcodes und Ausschalten (Gefahr)');
  await foto('einstellungen-an');
  await deutsch('Einstellungen an');
  await dialogeZu();

  // --- 7. Abmelden, Anmelden: Passwort -> Code-Schritt (Tresor zu, kein Cookie) ---
  const abmeldenUndPasswort = async () => {
    await dialogeZu();
    await ev('abmelden()');
    await bis('!!document.getElementById("a-pw")', 8000, 'Anmeldeseite');
    await setze('#a-benutzer', BENUTZER); await setze('#a-pw', PASSWORT);
    await klickT('app.anmelden.knopf');
    await bis('!!document.getElementById("a-code") || !!document.querySelector(".kopf")', 15000, 'Code-Schritt');
  };
  await abmeldenUndPasswort();
  pruef(await ev('!!document.getElementById("a-code")'), 'Passwort richtig -> Code-Schritt');
  {
    const st = await status();
    pruef(!st.angemeldet && st.zweitfaktor === undefined, 'Code-Schritt: nicht angemeldet, /api/status verraet nichts');
    pruef(!(await sitzungsCookie()), 'Code-Schritt: kein Anmelde-Cookie');
    const c = await ev(`(() => { const c = document.getElementById('a-code'); return { im: c.getAttribute('inputmode'), ac: c.getAttribute('autocomplete'), pw: document.querySelectorAll('input[type=password]').length, h: (document.querySelector('.einstieg h2') || {}).textContent }; })()`);
    pruef(c.im === 'numeric' && c.ac === 'one-time-code' && c.pw === 0 && c.h === (await T('app.code.titel')), 'Code-Schritt: Ueberschrift, Ziffernfeld, kein Passwortfeld im DOM');
  }
  await foto('code-schritt-desktop');
  await deutsch('Code-Schritt');
  // Falsche Codes, drei in Folge (jeweils die Antwort abwarten: das Feld leert sich)
  for (let i = 0; i < 3; i++) {
    await tippen('#a-code', falscherCode(geheimnis));
    await bis(`(() => { const c = document.getElementById('a-code'), k = document.querySelector('.einstieg button[type=submit]');
      return !!c && c.value === '' && !!k && !k.disabled && [...document.querySelectorAll('.meldung.fehler')].some((x) => x.offsetParent !== null); })()`, 15000, 'Antwort auf falschen Code');
  }
  const fCode = await meldungFehler('document');
  pruef(fCode.length > 0 && (await ev('!!document.getElementById("a-code")')) && !(await sitzungsCookie()), 'falscher Code: Meldung, weiter im Code-Schritt, kein Cookie (' + fCode + ')');
  await foto('code-falsch');
  // Richtiger Code (naechster freier Zeitschritt), Absenden mit der 6. Ziffer
  await toastsWeg();
  let ok = await frischerCode();
  await tippen('#a-code', ok.code);
  await bis('!!document.querySelector(".kopf")', 15000, 'Arbeitsplatz nach richtigem Code');
  letzterSchritt = ok.schritt;
  pruef((await status()).angemeldet && (await sitzungsCookie()), 'richtiger Code: angemeldet (Cookie gesetzt)');
  // Die drei falschen Codes (mit richtigem Passwort) meldet die Anmeldung als Warnung.
  await bis('document.querySelectorAll("#toasts .toast.warn").length > 0', 4000, 'Warnung falsche Codes');
  {
    const tF = await toasts(), fehlerText = await T('app.code.fehler', { n: 3 });
    pruef(tF.some((x) => x.startsWith('[warn]') && x.includes(fehlerText)), 'drei falsche Codes: Warnung bei der Anmeldung «' + fehlerText.slice(0, 60) + '…»');
  }
  await foto('code-falsch-warnung');

  // --- 8. Telefon: Code-Schritt, Wiederverwendung, Ersatzcode ---
  await ansicht(TELEFON);
  await abmeldenUndPasswort();
  await foto('code-schritt-telefon');
  await flaechen('document.querySelector(".einstieg") || document.body', 'Code-Schritt, Telefon');
  await tippen('#a-code', ok.code); // derselbe Code noch einmal
  const fWieder = await fehlerFrisch('document');
  pruef(/verwendet|already been used/.test(fWieder), 'derselbe Code ein zweites Mal: abgelehnt (' + fWieder + ')');
  pruef(!(await sitzungsCookie()), 'Wiederverwendung: kein Cookie');
  await klickT('basis.code.ersatzVerwenden');
  await warte(200);
  const ez = await ev(`(() => { const c = document.getElementById('a-code'); const v = document.querySelector('.code-verloren');
    return { im: c.getAttribute('inputmode'), ph: c.placeholder, verloren: !!v && !v.classList.contains('versteckt') && v.offsetParent !== null }; })()`);
  pruef(ez.im === 'text' && ez.ph === 'XXXX-XXXX' && ez.verloren, 'Ersatzcode-Modus: Texttastatur, XXXX-XXXX, Hinweis auf den Sicherheitsschluessel');
  await foto('code-ersatz-telefon');
  await flaechen('document.querySelector(".einstieg") || document.body', 'Ersatzcode, Telefon');
  await toastsWeg();
  await tippen('#a-code', ersatz[0].toLowerCase().replace('-', ' ')); // klein und mit Leerzeichen
  await klickT('app.code.knopf');
  await bis('!!document.querySelector(".kopf")', 15000, 'Arbeitsplatz nach Ersatzcode');
  await bis('document.querySelectorAll("#toasts .toast").length > 0', 4000, 'Toast Ersatzcode');
  const tE = await toasts();
  const restText = await T('app.code.ersatzRest', { n: 9 });
  pruef(tE.some((x) => x.includes(restText)), 'Ersatzcode (klein, mit Leerzeichen) angenommen, Toast «' + restText + '»');
  await foto('ersatzcode-angemeldet-telefon');
  await einstellungenOeffnen();
  pruef((await ev(`(${GRUPPE}).textContent`)).includes(await T('hosts.tf.an', { n: 9 })), 'Einstellungen: 9 Ersatzcodes uebrig');
  await foto('einstellungen-an-telefon');
  await flaechen(GRUPPE, 'Einstellungen an, Telefon');
  await dialogeZu();
  // Derselbe Ersatzcode ein zweites Mal: abgelehnt
  await abmeldenUndPasswort();
  await klickT('basis.code.ersatzVerwenden');
  await tippen('#a-code', ersatz[0]);
  await klickT('app.code.knopf');
  const fE2 = await fehlerFrisch('document');
  pruef(fE2.length > 0 && !(await sitzungsCookie()), 'verbrauchter Ersatzcode: abgelehnt (' + fE2 + ')');
  await tippen('#a-code', ersatz[1]);
  await klickT('app.code.knopf');
  await bis('!!document.querySelector(".kopf")', 15000, 'Arbeitsplatz nach zweitem Ersatzcode');
  pruef((await status()).ersatzcodesUebrig === 8, 'zweiter Ersatzcode: noch 8');
  const tE8 = await toasts(), rest8 = await T('app.code.ersatzRest', { n: 8 });
  pruef(tE8.some((x) => x.includes(rest8)), 'Toast «' + rest8 + '»');

  // --- 9. Neue Ersatzcodes (Desktop) ---
  await ansicht(DESKTOP);
  await einstellungenOeffnen();
  await klickT('hosts.tf.neueCodes', GRUPPE);
  await bis('!!document.getElementById("tf-b-pw")', 5000, 'Dialog Neue Ersatzcodes');
  pruef(await ev(`[...(${OBEN}).querySelectorAll('button')].some((b) => b.textContent.trim() === t('basis.code.ersatzVerwenden'))`),
    'Neue Ersatzcodes: Umschalter «Ersatzcode verwenden» (der Server nimmt dort Ersatzcodes)');
  await setze('#tf-b-pw', PASSWORT);
  ok = await frischerCode();
  await foto('neue-ersatzcodes-dialog');
  await tippen('#tf-b-code', ok.code);
  await bis('!!document.querySelector(".tf-codes")', 10000, 'neue Ersatzcodes');
  letzterSchritt = ok.schritt;
  const ersatzNeu = await ev('[...document.querySelectorAll(".tf-codes span")].map((s) => s.textContent)');
  pruef(ersatzNeu.length === 10 && ersatzNeu.every((c) => ersatzForm.test(c)) && !ersatzNeu.some((c) => ersatz.includes(c)), '10 neue Ersatzcodes, keiner der alten');
  pruef((await ev('(' + OBEN + ').textContent')).includes(await T('hosts.tf.codesNeuText')), 'Hinweis: die bisherigen gelten nicht mehr');
  await foto('ersatzcodes-neu');
  await ev(`(${OBEN}).querySelector('input[type=checkbox]').click()`);
  await klickT('hosts.tf.fertig', OBEN);
  await warte(300);
  fs.writeFileSync(ERSATZ_DATEI, ersatzNeu.join('\n') + '\n');
  pruef((await status()).ersatzcodesUebrig === 10 && (await ev(`(${GRUPPE}).textContent`)).includes(await T('hosts.tf.an', { n: 10 })), 'wieder 10 Ersatzcodes (Status und Gruppe)');

  // --- 10. Ausschalten: erst mit altem Ersatzcode (abgelehnt), dann mit neuem ---
  await klickT('hosts.tf.ausschalten', GRUPPE);
  await bis('!!document.getElementById("tf-b-pw")', 5000, 'Dialog Ausschalten');
  await klickT('basis.code.ersatzVerwenden', OBEN);
  // Falsches Passwort mit gueltigem Ersatzcode: das Passwort wird markiert, der Code bleibt
  // (und ist nicht verbraucht: der Server prueft das Passwort zuerst).
  await setze('#tf-b-pw', 'falsches-passwort-123');
  await tippen('#tf-b-code', ersatzNeu[1]);
  await klickT('hosts.tf.ausschalten', OBEN);
  const fPwAus = await fehlerFrisch(OBEN);
  {
    const f = await ev(`(() => { const p = document.getElementById('tf-b-pw'), c = document.getElementById('tf-b-code');
      return { pwLeer: p.value === '', pwMarkiert: p.getAttribute('aria-invalid') === 'true', code: c.value }; })()`);
    pruef(f.pwLeer && f.pwMarkiert && f.code === ersatzNeu[1] && (await status()).zweitfaktor === true,
      'Ausschalten, falsches Passwort: Passwort markiert, Code bleibt, weiter an (' + fPwAus + ')');
  }
  await setze('#tf-b-pw', PASSWORT);
  await tippen('#tf-b-code', ersatz[2]); // alter, nie benutzter Ersatzcode
  await klickT('hosts.tf.ausschalten', OBEN);
  const fAlt = await fehlerFrisch(OBEN);
  pruef(fAlt.length > 0 && (await status()).zweitfaktor === true, 'Ausschalten mit altem Ersatzcode: abgelehnt, bleibt an (' + fAlt + ')');
  await foto('ausschalten-dialog');
  await setze('#tf-b-pw', PASSWORT);
  await tippen('#tf-b-code', ersatzNeu[0]);
  await toastsWeg();
  await klickT('hosts.tf.ausschalten', OBEN);
  await bis('!document.getElementById("tf-b-pw")', 10000, 'Ausschalten');
  await warte(300);
  const tAus = await toasts();
  const stAus = await status();
  const ausText = await T('hosts.tf.ausGeschaltet');
  pruef(stAus.zweitfaktor === false && stAus.ersatzcodesUebrig === undefined, 'ausgeschaltet (/api/status zweitfaktor:false)');
  pruef(tAus.some((x) => x.includes(ausText)), 'Toast «' + ausText + '»');
  pruef((await ev(`(${GRUPPE}).textContent`)).includes(await T('hosts.tf.aus')), 'Gruppe zeigt wieder «aus»');
  await foto('einstellungen-wieder-aus');
  fs.rmSync(SCHLUESSEL_DATEI, { force: true });
  fs.rmSync(ERSATZ_DATEI, { force: true });

  // --- 11. Anmelden ohne Code ---
  await abmeldenUndPasswort();
  pruef(await ev('!!document.querySelector(".kopf") && !document.getElementById("a-code")'), 'aus: Anmeldung nur mit Passwort, kein Code-Schritt');

  // --- 12. «Passwort vergessen?»: Hinweis; mit Sicherheitsschluessel auch der Ernstfall ---
  let mitReset = !!sicherheitsschluessel;
  if (mitReset) {
    // Wieder einschalten (direkt ueber die Schnittstelle; die Oberflaeche ist oben geprueft)
    const b = await ev(`api('POST', '/api/zweitfaktor/beginnen', {})`);
    geheimnis = base32Lesen(b.schluessel);
    fs.writeFileSync(SCHLUESSEL_DATEI, b.schluessel + '\n');
    await ev(`api('POST', '/api/zweitfaktor/einschalten', { passwort: ${JSON.stringify(PASSWORT)}, code: ${JSON.stringify(codeBei(geheimnis, jetztSchritt()))} }, { antwort401: true })`);
    letzterSchritt = jetztSchritt();
    pruef((await status()).zweitfaktor === true, 'fuer den Ernstfall wieder eingeschaltet');
  }
  await ev('abmelden()');
  await bis('!!document.getElementById("a-pw")', 8000, 'Anmeldeseite');
  await toastsWeg();
  await klickT('app.anmelden.vergessen');
  await bis('!!document.getElementById("r-key")', 5000, 'Seite Zuruecksetzen');
  const hinweis = await T('app.zurueck.zweitfaktor');
  pruef((await ev('[...document.querySelectorAll(".code-hinweis")].map((x) => x.textContent).join(" ")')).includes(hinweis), 'Zuruecksetzen: Hinweis «' + hinweis.slice(0, 50) + '…»');
  await foto('zuruecksetzen-hinweis');
  await deutsch('Zuruecksetzen');
  await ansicht(TELEFON);
  await foto('zuruecksetzen-hinweis-telefon');
  await flaechen('document.querySelector(".einstieg") || document.body', 'Zuruecksetzen, Telefon');
  await ansicht(DESKTOP);
  if (mitReset) {
    await ev(`(() => { const k = document.getElementById('r-key'); Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(k, ${JSON.stringify(sicherheitsschluessel)}); k.dispatchEvent(new Event('input', { bubbles: true })); return 1; })()`);
    await setze('#r-benutzer', BENUTZER);
    await ev(`(() => { const p = [...document.querySelectorAll('.einstieg input[type=password]')]; for (const f of p) { Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(f, ${JSON.stringify(PASSWORT)}); f.dispatchEvent(new Event('input', { bubbles: true })); } return p.length; })()`);
    await toastsWeg();
    await klickT('app.zurueck.knopf');
    await bis('document.querySelectorAll("#toasts .toast.warn").length > 0 || [...document.querySelectorAll(".meldung.fehler")].length > 0', 15000, 'Zuruecksetzen');
    const tR = await toasts();
    const okText = await T('app.zurueck.okZweitfaktor');
    pruef(tR.some((x) => x.startsWith('[warn]') && x.includes(okText)), 'Zuruecksetzen mit Sicherheitsschluessel: Warn-Toast «Zwei-Faktor-Anmeldung ausgeschaltet»');
    await foto('zuruecksetzen-zweitfaktor-aus');
    await bis('!!document.getElementById("a-pw") || !!document.querySelector(".kopf")', 8000, 'nach Zuruecksetzen');
    if (await ev('!!document.getElementById("a-pw")')) {
      await setze('#a-benutzer', BENUTZER); await setze('#a-pw', PASSWORT);
      await klickT('app.anmelden.knopf');
      await bis('!!document.getElementById("a-code") || !!document.querySelector(".kopf")', 15000, 'Anmelden nach Zuruecksetzen');
    }
    pruef(await ev('!!document.querySelector(".kopf")'), 'nach dem Zuruecksetzen: Anmeldung ohne Code');
    pruef((await status()).zweitfaktor === false, 'nach dem Zuruecksetzen: /api/status zweitfaktor:false');
    fs.rmSync(SCHLUESSEL_DATEI, { force: true });
  } else {
    console.log('(Zuruecksetzen mit Sicherheitsschluessel uebersprungen: ' + SK_DATEI + ' fehlt)');
  }

  // --- Abschluss ---
  const fehler = await ev('window.__kt || []');
  const fehlend = await ev('[...I18N.fehlend]');
  if (fehlend.length) probleme.push('Texte fehlen im Katalog: ' + fehlend.join(', '));
  if (deutschFunde.size) probleme.push(deutschFunde.size + ' deutsche Texte im englischen Modus (siehe oben)');
  console.log('Fehler im Browser:', fehler.length ? fehler : 'keine');
  console.log('Pruefpunkte:', probleme.length ? probleme : 'alle gut');
  process.exit(fehler.length || probleme.length ? 1 : 0);
})().catch((e) => { console.error('ABBRUCH:', e.message); process.exit(1); });
