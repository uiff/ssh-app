#!/usr/bin/env node
// klicktest.js — Durchlauf der Oberflaeche per Headless-Chrome (CDP):
// Einrichtung (oder Anmeldung), Zugang + Host anlegen, verbinden, Host-Schluessel
// bestaetigen, tippen, Dateien und Hilfe oeffnen, Palette — mit Fotos.
// Prueft dazu: gleich hohe Leisten (Unterkanten auf einer Linie), Trefferflaechen >= 44 px
// auf dem Telefon, kein waagrechter Ueberlauf, keine Skriptfehler, keine fehlenden Texte im
// Sprachkatalog und (SPRACHE=en) keine deutschen Texte in der Oberflaeche.
//
// Aufruf: node --experimental-websocket tools/klicktest.js <url> <ssh-host> <ssh-port> <ssh-user> <ssh-pw> <fotoordner> [breite hoehe]
// <ssh-host> = «-»: ohne SSH-Ziel (nur Oberflaeche: Startseite, Dialoge, Palette, Blaetter, Einstellungen).
// Braucht eine FRISCHE Instanz (nicht eingerichtet) oder SSHAPP_USER/SSHAPP_PW fuer die Anmeldung.
// SPRACHE=de|en: Sprache der Oberflaeche (gemerkt wie nach der Sprachwahl), sonst die des Browsers;
// LANG_CHROME=en-US o.ae. setzt die Browsersprache (Chrome-Schalter --lang).
// Chrome: CHROME=/pfad, sonst Puppeteer-Cache (~/.cache/puppeteer/chrome-headless-shell).
// DATEIEN_ORDNER=/pfad: zusaetzlich die Dateileiste pruefen (Arten, Menue ohne Download bei Pipe/Geraet,
// Download-Fehler, Hochladen mit Konflikt-Rueckfrage, «folgt» ohne Dauerabfrage, erfolgreicher Download).
// Der Ordner wird neu angelegt und muss auf dem Rechner des SSH-Ziels liegen — also nur mit dem
// eingebetteten Test-SSH-Server (SSHAPP_TESTSSH_HALTEN=<s> go test ./internal/server -run TestSSHHalten).
'use strict';
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const [url, sshHost, sshPort, sshUser, sshPw, fotos, wStr, hStr] = process.argv.slice(2);
if (!url || !sshHost || !fotos) {
  console.error('Aufruf: node --experimental-websocket tools/klicktest.js <url> <ssh-host> <ssh-port> <ssh-user> <ssh-pw> <fotoordner> [breite hoehe]');
  process.exit(2);
}
const W = +wStr || 1440, H = +hStr || 900, MOBIL = W < 900, OHNE_SSH = sshHost === '-';
const BENUTZER = process.env.SSHAPP_USER || 'admin', PASSWORT = process.env.SSHAPP_PW || 'klicktest-passwort';
const SPRACHE = process.env.SPRACHE || '', LANG = process.env.LANG_CHROME || (SPRACHE === 'en' ? 'en-US' : SPRACHE === 'de' ? 'de-CH' : '');
const DATEIEN_ORDNER = process.env.DATEIEN_ORDNER || '';
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
    `--remote-debugging-port=${port}`, `--window-size=${W},${H}`, '--user-data-dir=' + profil, ...(LANG ? ['--lang=' + LANG] : []), 'about:blank'], { stdio: 'ignore' });
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
  // Knopf nach Text aus dem Sprachkatalog klicken (gleich in beiden Sprachen)
  const klickT = async (schluessel, wo) => klick(await ev(`t(${JSON.stringify(schluessel)})`), wo);
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
  if (SPRACHE) {
    await ev(`(() => { localStorage.setItem('sshapp.sprache', JSON.stringify(${JSON.stringify(SPRACHE)})); return 1; })()`);
    await cdp('Page.reload');
    await warte(300);
    await bis('document.readyState === "complete" && !!document.querySelector("form, .kopf")', 8000, 'Startseite nach Sprachwahl');
    await ev(`(() => { window.__kt = []; addEventListener('error', e => __kt.push('error: ' + e.message));
      addEventListener('unhandledrejection', e => __kt.push('unhandledrejection: ' + ((e.reason && e.reason.message) || e.reason)));
      const a = console.error.bind(console); console.error = (...x) => { __kt.push('console.error: ' + x.join(' ')); a(...x); }; return 1; })()`);
  }
  console.log('Sprache:', await ev('I18N.sprache + " (lang=" + document.documentElement.lang + ")"'));
  const probleme = [];
  // Sprachpruefung: im englischen Modus keine deutschen Texte in der Oberflaeche (Text, aria-label,
  // title, placeholder). Inhalte (Terminal, Befehle, Fingerabdruecke, Namen) zaehlen nicht.
  const deutschFunde = new Set();
  const deutsch = async (wo) => {
    if ((await ev('I18N.sprache')) !== 'en') return;
    const funde = await ev(`(() => {
      const re = /[äöüÄÖÜ]|\\b(und|oder|nicht|mit|für|bei|der|die|das|den|dem|eine?|ist|wird|Sitzungs?|Sitzungen|Verbind\\w*|Schliessen|Abbrechen|Einstellungen|Zugangs?|Tresor|Ordner|Dateien|Befehl\\w*|Suchen|Neue[rns]?|Anmeld\\w*|Abmelden|Speichern|Hinzu\\w*|anlegen|Passwort|Benutzer\\w*|Schlüssel|Fehler|Zurück|Weiter|Hilfe|Gerät\\w*|Erweitert|Sprache)\\b/;
      const weg = (e) => !e || e.closest('.xterm, code, kbd, .fp, .schluessel-box, .befehl-gross, .term-deko, [lang=de], .karte small, .zeile small, .datei .n, .ph');
      const aus = new Set();
      const w = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
      for (let n = w.nextNode(); n; n = w.nextNode()) {
        const e = n.parentElement, s = n.textContent.trim();
        if (!s || weg(e) || !(e.offsetParent || getComputedStyle(e).position === 'fixed')) continue;
        if (re.test(s)) aus.add('Text: ' + s.slice(0, 80));
      }
      for (const e of document.querySelectorAll('[aria-label], [title], [placeholder]')) {
        if (weg(e)) continue;
        for (const a of ['aria-label', 'title', 'placeholder']) { const v = e.getAttribute(a); if (v && re.test(v)) aus.add(a + ': ' + v.slice(0, 80)); }
      }
      return [...aus]; })()`);
    for (const f of funde) if (!deutschFunde.has(f)) { deutschFunde.add(f); console.log('Deutsch im englischen Modus (' + wo + '):', f); }
  };

  if (await ev('!!document.getElementById("s-pw")')) {
    await foto('einrichten');
    await deutsch('Einrichten');
    if (!(await ev('!!document.querySelector(".einstieg .sprachwahl button[aria-pressed=true]")'))) probleme.push('Einrichten: keine Sprachwahl');
    await setze('#s-benutzer', BENUTZER); await setze('#s-pw', PASSWORT); await setze('#s-pw2', PASSWORT);
    await klickT('app.einrichten.knopf');
    await bis('!!document.querySelector(".schluessel-box")', 15000, 'Sicherheitsschluessel');
    await foto('sicherheitsschluessel');
    await deutsch('Sicherheitsschluessel');
    const sk = await ev('document.querySelector(".schluessel-box").textContent');
    fs.writeFileSync(path.join(fotos, 'sicherheitsschluessel.txt'), sk + '\n');
    await ev('document.querySelector(".einstieg input[type=checkbox]").click()');
    await klickT('app.schluessel.weiter');
  } else if (await ev('!!document.getElementById("a-pw")')) {
    await foto('anmelden');
    await deutsch('Anmelden');
    if (!(await ev('!!document.querySelector(".einstieg .sprachwahl button[aria-pressed=true]")'))) probleme.push('Anmelden: keine Sprachwahl');
    await setze('#a-benutzer', BENUTZER); await setze('#a-pw', PASSWORT);
    await klickT('app.anmelden.knopf');
  }
  await bis('!!document.querySelector(".kopf")', 15000, 'Arbeitsplatz');
  await warte(600);
  await foto('start-leer');
  await deutsch('Start');

  // Leisten (V8): Reiter der Seitenleiste, Tab-Leiste und Seitenleisten-Koepfe enden auf derselben Zeile.
  const leisten = async (wo) => {
    if (MOBIL) return;
    const r = await ev(`(() => [...document.querySelectorAll('.reiter, .tabs, .seite-kopf')].filter(e => e.offsetParent !== null)
      .map(e => ({ k: e.className, h: e.getBoundingClientRect().height, u: e.getBoundingClientRect().bottom })))()`);
    const unten = new Set(r.map((x) => x.u)), hoehe = new Set(r.map((x) => x.h));
    console.log('Leisten (' + wo + '):', r.map((x) => x.k.split(' ')[0] + ' ' + x.h + '/' + x.u).join(', '));
    if (unten.size > 1 || hoehe.size > 1) probleme.push('Leisten ungleich (' + wo + '): ' + JSON.stringify(r));
  };
  // Trefferflaechen auf dem Telefon: sichtbare Bedienelemente unter 44 px (Polster per ::after zaehlt mit).
  const flaechen = async (wo) => {
    if (!MOBIL) return;
    const klein = await ev(`(() => { const aus = [];
      for (const e of document.querySelectorAll('button, a, input:not([type=checkbox]):not([type=hidden]), select, textarea, label.check')) {
        if (e.closest('.versteckt') || e.classList.contains('versteckt')) continue;
        if (e.classList.contains('xterm-helper-textarea')) continue; // unsichtbares Eingabefeld von xterm, Trefferflaeche ist das Terminal
        const r = e.getBoundingClientRect(); if (!r.width || !r.height || r.bottom < 0 || r.top > innerHeight || r.right < 0 || r.left > innerWidth) continue;
        let h = r.height, w = r.width; const n = getComputedStyle(e, '::after');
        if (n.content && n.content !== 'none' && n.position === 'absolute') { h -= (parseFloat(n.top) || 0) + (parseFloat(n.bottom) || 0); w -= (parseFloat(n.left) || 0) + (parseFloat(n.right) || 0); }
        if (h < 43.5 || w < 43.5) aus.push(((e.getAttribute('aria-label') || e.textContent || e.placeholder || '').trim().slice(0, 24) || e.tagName) + ' ' + Math.round(w) + 'x' + Math.round(h));
      } return aus; })()`);
    if (klein.length) probleme.push('Trefferflaechen < 44 px (' + wo + '): ' + klein.join(' | '));
  };
  await leisten('Start');
  await flaechen('Start');
  // Mehr-Menue mit Sprachwahl (andere Sprache in ihrer eigenen Schreibweise)
  await ev('document.querySelector(".kopf > .ikon:last-child").click()');
  await warte(300);
  await foto('menue');
  await deutsch('Mehr-Menue');
  if (!(await ev('!!document.querySelector(".menue button [lang]")'))) probleme.push('Mehr-Menue: kein Eintrag fuer die Sprache');
  await ev('menue.offen && menue.offen()');
  // Abschluss (beide Wege): Skriptfehler, Ueberlauf, fehlende Katalog-Texte, deutsche Texte im englischen Modus
  const abschluss = async () => {
    const fehler = await ev('window.__kt || []');
    const ueberlauf = await ev('document.documentElement.scrollWidth > innerWidth + 1');
    const fehlend = await ev('[...I18N.fehlend]');
    if (fehlend.length) probleme.push('Texte fehlen im Katalog: ' + fehlend.join(', '));
    if (deutschFunde.size) probleme.push(deutschFunde.size + ' deutsche Texte im englischen Modus (siehe oben)');
    console.log('Fehler im Browser:', fehler.length ? fehler : 'keine');
    console.log('Waagrechter Ueberlauf:', ueberlauf ? 'JA' : 'nein');
    console.log('Pruefpunkte:', probleme.length ? probleme : 'alle gut');
    process.exit(fehler.length || ueberlauf || probleme.length ? 1 : 0);
  };

  // Dateileiste (DATEIEN_ORDNER): Testordner mit Pipe, Geraet, Verknuepfungen und grosser Datei.
  const dateileiste = async () => {
    const D = DATEIEN_ORDNER;
    fs.rmSync(D, { recursive: true, force: true });
    fs.mkdirSync(path.join(D, 'ordner'), { recursive: true });
    fs.writeFileSync(path.join(D, 'a.txt'), 'hallo\n');
    require('child_process').execFileSync('mkfifo', [path.join(D, 'rohr')]);
    fs.symlinkSync('/dev/null', path.join(D, 'zu-null'));
    fs.symlinkSync('gibts-nicht', path.join(D, 'kaputt'));
    fs.writeFileSync(path.join(D, 'gross.bin'), require('crypto').randomBytes(300000));
    const toasts = () => ev('[...document.querySelectorAll("#toasts .toast")].map((x) => x.textContent)');
    const toastsWeg = () => ev('document.querySelectorAll("#toasts .toast").forEach((x) => x.remove())');
    await ev('App.seiteUmschalten("dateien", true)'); await warte(500);
    await ev(`App.dateien.folgenSetzen(false); App.dateien.laden(${JSON.stringify(D)})`);
    await bis(`App.dateien.pfad === ${JSON.stringify(D)} && App.dateien.listeAktuell`, 8000, 'Dateiliste');
    await warte(300);
    const spalte = await ev(`Object.fromEntries([...document.querySelectorAll('.seite.dateien .datei')].map((b) => [b.querySelector('.n').textContent, b.querySelector('.g').textContent]))`);
    console.log('Dateiliste:', JSON.stringify(spalte));
    if (spalte.rohr !== (await ev('t("dateien.art.pipe")'))) probleme.push('Dateileiste: Pipe nicht als solche gezeigt');
    await foto('dateien-arten'); await flaechen('Dateileiste'); await deutsch('Dateileiste');
    // Menue: Herunterladen nur bei gewoehnlichen Dateien
    const dl = await ev('t("dateien.menue.herunterladen")');
    const menueTexte = async (name) => {
      await ev(`[...document.querySelectorAll('.seite.dateien .datei')].find((b) => b.querySelector('.n').textContent === ${JSON.stringify(name)}).click()`);
      await warte(300);
      return ev('[...document.querySelectorAll(".menue button")].map((b) => b.textContent)');
    };
    if ((await menueTexte('rohr')).includes(dl)) probleme.push('Dateileiste: Pipe bietet Herunterladen an');
    await foto('dateien-menue-pipe'); await flaechen('Datei-Menue'); await deutsch('Datei-Menue');
    await ev('menue.offen && menue.offen()');
    if ((await menueTexte('zu-null')).includes(dl)) probleme.push('Dateileiste: Geraet bietet Herunterladen an');
    await ev('menue.offen && menue.offen()');
    if (!(await menueTexte('a.txt')).includes(dl)) probleme.push('Dateileiste: Datei ohne Herunterladen');
    // Download einer inzwischen geloeschten Datei: Grund als Meldung in der Sprache der Oberflaeche
    fs.rmSync(path.join(D, 'a.txt'));
    await toastsWeg();
    await ev(`[...document.querySelectorAll('.menue button')].find((b) => b.textContent === ${JSON.stringify(dl)}).click()`);
    await bis('document.querySelectorAll("#toasts .toast.fehler").length > 0', 5000, 'Fehlermeldung Download');
    console.log('Download-Meldung:', JSON.stringify(await toasts()));
    await foto('dateien-download-fehler'); await deutsch('Download-Fehler');
    await toastsWeg();
    // Hochladen mit vorhandener Datei und Ordner gleichen Namens: Rueckfrage, Ordner bleibt
    fs.writeFileSync(path.join(D, 'b.txt'), 'alt');
    await ev('App.dateien.laden(App.dateien.pfad)'); await warte(500);
    // void: sonst wartet Runtime.evaluate auf die Rueckfrage
    await ev(`void App.dateien.hochladen([new File(['neu-b'], 'b.txt'), new File(['x'], 'ordner'), new File(['n'], 'neu.txt')])`);
    await bis('!!document.querySelector(".dialog")', 5000, 'Rueckfrage Ersetzen');
    await warte(200);
    await foto('dateien-rueckfrage'); await flaechen('Rueckfrage Ersetzen'); await deutsch('Rueckfrage Ersetzen');
    await ev(`[...document.querySelectorAll('.dialog button')].find((b) => b.textContent === t('dateien.ersetzen')).click()`);
    await bis('document.querySelectorAll("#toasts .toast:not(.fehler)").length > 0', 8000, 'Hochgeladen');
    if (fs.readFileSync(path.join(D, 'b.txt'), 'utf8') !== 'neu-b' || fs.readFileSync(path.join(D, 'neu.txt'), 'utf8') !== 'n') probleme.push('Hochladen: Inhalte falsch');
    if (!fs.statSync(path.join(D, 'ordner')).isDirectory()) probleme.push('Hochladen: Ordner beschaedigt');
    await toastsWeg();
    // «folgt» fragt einen fehlenden Ordner nicht dauernd ab
    await ev(`window.__zaehler = { dateien: 0, pfad: 0 }; if (!window.__fetchOrig) { window.__fetchOrig = window.fetch; window.fetch = (u, o) => { if (String(u).includes('/dateien?')) __zaehler.dateien++; if (String(u).endsWith('/pfad')) __zaehler.pfad++; return __fetchOrig(u, o); }; } 1`);
    await ev(`App.dateien.laden('/gibts-nicht-klicktest')`); await warte(400);
    await ev('App.dateien.folgen = true; App.dateien.folgenZeichnen(); window.__zaehler = { dateien: 0, pfad: 0 }; 1');
    await warte(5000);
    const z = await ev('window.__zaehler');
    if (z.dateien > 0) probleme.push('Dateileiste: «folgt» fragt einen fehlenden Ordner dauernd ab (' + z.dateien + ')');
    // Erfolgreicher Download kommt vollstaendig an
    const dlDir = path.join(fotos, 'downloads'); fs.rmSync(dlDir, { recursive: true, force: true }); fs.mkdirSync(dlDir, { recursive: true });
    await cdp('Browser.setDownloadBehavior', { behavior: 'allow', downloadPath: dlDir });
    await ev(`App.dateien.folgen = false; App.dateien.folgenZeichnen(); App.dateien.laden(${JSON.stringify(D)})`); await warte(600);
    await ev(`App.dateien.herunterladen(${JSON.stringify(D + '/gross.bin')})`);
    let groesse = -1;
    for (let i = 0; i < 40 && groesse !== 300000; i++) { await warte(250); try { groesse = fs.statSync(path.join(dlDir, 'gross.bin')).size; } catch (e) { /* noch nicht da */ } }
    if (groesse !== 300000) probleme.push('Dateileiste: Download unvollstaendig (' + groesse + ' B)');
    console.log('Dateileiste: Download', groesse, 'B, folgt-Abfragen', JSON.stringify(z));
    await ev('App.seiteUmschalten("dateien", false)');
  };

  // Dialoge fotografieren, Zugang und Host per API anlegen (unabhaengig vom Aufbau der Dialoge)
  await ev('App.zugangBearbeiten({})');
  await warte(300);
  await foto('zugang-dialog');
  await flaechen('Zugang-Dialog');
  await deutsch('Zugang-Dialog');
  await ev('dialogStapel.slice().forEach((d) => d.zu())');
  await ev('App.hostBearbeiten({})');
  await warte(300);
  await foto('host-dialog');
  await flaechen('Host-Dialog');
  await deutsch('Host-Dialog');
  await ev('dialogStapel.slice().forEach((d) => d.zu())');
  if (OHNE_SSH) {
    // Mit gespeichertem Zugang: keine Anmelde-Rueckfrage, der Aufbau scheitert gleich (Port 1).
    const ohneZiel = await ev(`(async () => { const z = await api('POST', '/api/zugaenge', { name: 'ohne-ziel', benutzer: 'pi', passwort: 'klicktest' });
      const h = await api('POST', '/api/hosts', { name: 'ohne-ziel', adresse: '127.0.0.1', port: 1, zugangId: z.id, tags: ['labor'] }); await App.datenLaden(); return h.id; })()`);
    await warte(300);
    await foto('start-mit-host');
    await flaechen('Start mit Host');
    await deutsch('Start mit Host');
    if (await ev('App.start.scrollWidth > App.start.clientWidth + 1')) probleme.push('Startseite seitlich verschiebbar');
    if (MOBIL) {
      await ev('App.seiteUmschalten("links", true)'); await warte(400); await foto('hosts-blatt'); await flaechen('Hosts-Blatt'); await deutsch('Hosts-Blatt');
      await ev('history.back()'); await warte(400);
      if (await ev('App.seiten.links')) probleme.push('Zurueck-Geste schliesst das Hosts-Blatt nicht');
    } else {
      await ev('App.seiteUmschalten("dateien", true); App.seiteUmschalten("hilfe", true)'); await warte(500);
      await leisten('alle Seitenleisten offen'); await foto('seitenleisten'); await deutsch('Seitenleisten');
      await ev('App.seiteUmschalten("dateien", false); App.seiteUmschalten("hilfe", false)');
    }
    // Verbinden ohne Ziel: Fehlerkarte, Cursor darunter unsichtbar. Dazu eine Rueckfrage im selben
    // Augenblick: Der neue Tab darf ihr den Fokus nicht nehmen (Desktop).
    await ev(`(() => { App.verbinden(App.daten.hosts.find((h) => h.id === ${ohneZiel})); App.hostkeyFragen({ status: 'neu', ziel: 'pi@127.0.0.1:1', typ: 'ssh-ed25519', fp: 'SHA256:klicktest' }); return 1; })()`);
    await warte(120);
    if (!MOBIL && !(await ev('!!(document.activeElement && document.activeElement.closest(".dialog"))'))) probleme.push('Neuer Tab nimmt der Rueckfrage den Fokus');
    await foto('rueckfrage-hostkey'); await deutsch('Rueckfrage Host-Schluessel'); await flaechen('Rueckfrage Host-Schluessel');
    await ev('dialogStapel.slice().forEach((d) => d.zu())');
    await bis('App.aktiv && (App.aktiv.zustand === "fehler" || App.aktiv.zustand === "beendet")', 15000, 'Fehlerkarte');
    await warte(400);
    await foto('fehlerkarte'); await deutsch('Fehlerkarte'); await flaechen('Fehlerkarte');
    if (await ev('App.aktiv.term.options.theme.cursor !== "#00000000"')) probleme.push('Cursor unter der Fehlerkarte sichtbar');
    await ev('App.tabSchliessen(App.aktiv)'); await warte(300);
    await ev('App.palette("archiv entpacken")'); await warte(1500); await foto('palette'); await flaechen('Palette'); await deutsch('Palette');
    await ev('App.paletteObj.schliessen()');
    await ev('App.einstellungen()'); await warte(500); await foto('einstellungen'); await flaechen('Einstellungen'); await deutsch('Einstellungen');
    await ev('dialogStapel.slice().forEach((d) => d.zu())');
    await abschluss();
  }
  const zugang = await ev(`api('POST', '/api/zugaenge', { name: 'testziel', benutzer: ${JSON.stringify(sshUser)}, passwort: ${JSON.stringify(sshPw)} })`);
  const host = await ev(`api('POST', '/api/hosts', { name: 'testziel', adresse: ${JSON.stringify(sshHost)}, port: ${+sshPort || 22}, zugangId: ${zugang.id}, tags: ['labor', 'kritisch'], geraet: 'server' })`);
  await ev('App.datenLaden()');
  await ev(`App.verbinden(App.daten.hosts.find((h) => h.id === ${host.id}))`);

  // Host-Schluessel bestaetigen
  // Host-Schluessel: beim ersten Mal Rueckfrage, danach bekannt.
  await bis('!!document.querySelector(".dialog .fp") || (App.aktiv && App.aktiv.zustand === "bereit")', 15000, 'Host-Schluessel-Dialog oder Terminal');
  if (await ev('!!document.querySelector(".dialog .fp")')) {
    await foto('hostkey');
    await deutsch('Host-Schluessel');
    await klickT('app.hostkey.vertrauen');
  }
  await bis('App.aktiv && App.aktiv.zustand === "bereit"', 15000, 'Terminal bereit');
  await warte(1200);
  await leisten('mit Tab');
  await flaechen('Terminal');
  await ev('App.aktiv.tippen("ls -la /etc | head -n 15\\r")');
  await warte(1000);
  await ev('App.aktiv.tippen("cd /etc && tar -xzf ")');
  await warte(700);
  await foto('terminal');
  await deutsch('Terminal');

  // Dateien und Hilfe
  if (!MOBIL) {
    await ev('App.seiteUmschalten("dateien", true)');
    await warte(2500);
    await ev('App.seiteUmschalten("hilfe", true)');
    await warte(1500);
    await foto('dateien-hilfe');
    await deutsch('Dateien und Hilfe');
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
  if (DATEIEN_ORDNER) await dateileiste();

  // Palette
  await ev('App.aktiv.tippen("\\x15")'); // Zeile leeren
  await ev('App.palette("grosse dateien finden")');
  await warte(1500);
  await foto('palette');
  await deutsch('Palette');
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
  await deutsch('Einstellungen');
  await ev('document.querySelector(".dialog .dialog-kopf .ikon").click()');

  // Neuladen: Sitzung muss wieder angehaengt werden
  await cdp('Page.reload');
  await warte(500);
  await bis('document.readyState === "complete" && window.App && App.tabs && App.tabs.length > 0 && App.aktiv && App.aktiv.zustand === "bereit"', 15000, 'Wiederanhaengen nach Neuladen');
  await warte(800);
  await foto('nach-neuladen');
  await abschluss();
})().catch((e) => { console.error('ABBRUCH:', e.message); process.exit(1); });
