#!/usr/bin/env node
// novnc-probe.js — prueft den VNC-Server-Teil von Keyway mit dem echten noVNC 1.7 im
// Headless-Chrome (ohne die VNC-Oberflaeche): /api/vnc, Rueckfrage vnc-passwort,
// «bereit», Begruessung durch Keyway (RFB 3.8, None), Durchreichen (VeNCrypt Plain),
// RFB 3.3, Klick/Strg+Alt+Entf/Zwischenablage/Fernaufloesung beim Test-VNC-Server.
//
// Aufruf: node --experimental-websocket tools/testvnc/novnc-probe.js <keyway-url> <fotoordner> <portA> <portB> <portC> [ssh-host:port]
//   A = testvnc -passwort geheim, B = testvnc -typ vencrypt -passwort plainpw, C = testvnc -version 3.3 -breite 800 -hoehe 600
//   ssh-host:port (optional) = eingebetteter Test-SSH-Server (pi/himbeere, mit direct-tcpip):
//     SSHAPP_TESTSSH_HALTEN=600 go test ./internal/server -run TestSSHHalten -v
//   Dann verbindet D ueber den SSH-Tunnel zu A (127.0.0.1:<portA> aus Sicht des SSH-Servers).
// Braucht eine FRISCHE Keyway-Instanz mit SSHAPP_TLS=aus (Einrichtung mit admin / probe-passwort-1).
// Laedt noVNC aus der Instanz (/vendor/novnc/core/rfb.js) und bindet es an den WebSocket.
'use strict';
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const [url, fotos, pA, pB, pC, sshZiel] = process.argv.slice(2);
const warte = (ms) => new Promise((r) => setTimeout(r, ms));

function chromePfad() {
  if (process.env.CHROME) return process.env.CHROME;
  const cache = path.join(process.env.HOME || '/root', '.cache/puppeteer/chrome-headless-shell');
  for (const v of fs.readdirSync(cache).sort().reverse()) {
    const p = path.join(cache, v, 'chrome-headless-shell-linux64/chrome-headless-shell');
    if (fs.existsSync(p)) return p;
  }
  throw new Error('kein Chrome');
}

(async () => {
  fs.mkdirSync(fotos, { recursive: true });
  const port = +process.env.CDP_PORT || (9600 + Math.floor(Math.random() * 300));
  const profil = fs.mkdtempSync(path.join(require('os').tmpdir(), 'vnc-probe-'));
  const ch = spawn(chromePfad(), ['--headless=new', '--no-sandbox', '--disable-gpu', '--hide-scrollbars',
    `--remote-debugging-port=${port}`, '--window-size=1280,900', '--user-data-dir=' + profil, 'about:blank'], { stdio: 'ignore' });
  process.on('exit', () => { try { ch.kill('SIGKILL'); } catch (e) { /* */ } try { fs.rmSync(profil, { recursive: true, force: true }); } catch (e) { /* */ } });
  let ziel = null;
  for (let i = 0; i < 50 && !ziel; i++) {
    await warte(200);
    try { const t = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json(); if (t.length) ziel = t[0].webSocketDebuggerUrl; } catch (e) { /* */ }
  }
  const ws = new WebSocket(ziel);
  await new Promise((res, rej) => { ws.onopen = res; ws.onerror = () => rej(new Error('CDP')); });
  let id = 0; const pend = new Map(); const konsole = [];
  ws.onmessage = (m) => {
    const d = JSON.parse(m.data);
    if (d.id && pend.has(d.id)) { pend.get(d.id)(d); pend.delete(d.id); }
    else if (d.method === 'Runtime.consoleAPICalled') konsole.push(d.params.type + ': ' + d.params.args.map((a) => a.value || a.description).join(' '));
    else if (d.method === 'Runtime.exceptionThrown') konsole.push('exception: ' + JSON.stringify(d.params.exceptionDetails).slice(0, 300));
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
  const maus = async (typ, x, y, taste = 'left') => cdp('Input.dispatchMouseEvent', { type: typ, x, y, button: taste, buttons: typ === 'mouseReleased' ? 0 : (taste === 'left' ? 1 : 2), clickCount: 1 });

  await cdp('Page.enable');
  await cdp('Runtime.enable');
  await cdp('Emulation.setDeviceMetricsOverride', { width: 1280, height: 900, deviceScaleFactor: 1, mobile: false });
  await cdp('Page.navigate', { url });
  for (let i = 0; i < 50; i++) { await warte(100); if (await ev('document.readyState === "complete"')) break; }

  // Einrichten, Hosts anlegen, noVNC laden, Verbindungsfunktion bereitstellen.
  const hosts = await ev(`(async () => {
    const api = async (m, p, b) => { const r = await fetch(p, { method: m, headers: { 'X-SSH-App': '1', 'Content-Type': 'application/json' }, body: b ? JSON.stringify(b) : undefined }); return { code: r.status, daten: await r.json().catch(() => null) }; };
    let r = await api('POST', '/api/einrichten', { benutzer: 'admin', passwort: 'probe-passwort-1' });
    if (r.code !== 200) r = await api('POST', '/api/anmelden', { benutzer: 'admin', passwort: 'probe-passwort-1' });
    const h = {};
    for (const [n, p] of [['A', ${pA}], ['B', ${pB}], ['C', ${pC}]]) {
      h[n] = (await api('POST', '/api/hosts', { name: 'probe-' + n, adresse: '127.0.0.1', vnc: true, vncPort: p, vncWeg: 'direkt' })).daten;
    }
    const ssh = ${JSON.stringify(sshZiel || '')};
    if (ssh) {
      const z = (await api('POST', '/api/zugaenge', { name: 'pi', benutzer: 'pi', passwort: 'himbeere' })).daten;
      const [sh, sp] = ssh.split(':');
      h.D = (await api('POST', '/api/hosts', { name: 'probe-D', adresse: sh, port: +sp, zugangId: z.id, vnc: true, vncPort: ${pA}, vncWeg: 'ssh', vncPasswort: 'geheim' })).daten;
    }
    const { default: RFB } = await import('/vendor/novnc/core/rfb.js');
    window.__api = api;
    window.__rfb = {};
    window.__verbinde = (host, opt) => new Promise((ok, nein) => {
      const e = { texte: [], binaerVorBereit: 0 };
      const sock = new WebSocket('ws://' + location.host + '/api/vnc?host=' + host.id + '&sprache=' + (opt.sprache || 'de'));
      sock.binaryType = 'arraybuffer';
      let rfb = null;
      const zeit = setTimeout(() => nein(new Error('Zeit: ' + JSON.stringify(e))), 15000);
      sock.addEventListener('message', (m) => {
        if (typeof m.data !== 'string') { if (!rfb) e.binaerVorBereit++; return; }
        const n = JSON.parse(m.data);
        e.texte.push(n);
        if (n.t === 'hostkey') sock.send(JSON.stringify({ t: 'hostkey', ok: true }));
        else if (n.t === 'vnc-passwort') sock.send(JSON.stringify({ t: 'vnc-passwort', passwort: opt.vncPasswort, speichern: !!opt.speichern }));
        else if (n.t === 'fehler') { clearTimeout(zeit); ok(e); }
        else if (n.t === 'bereit') {
          const z = document.createElement('div');
          z.style.cssText = 'position:fixed;left:0;top:0;width:' + (opt.b || 1024) + 'px;height:' + (opt.h || 768) + 'px;z-index:99999;background:#222';
          document.body.append(z);
          rfb = new RFB(z, sock, { credentials: opt.cred || {} });
          window.__rfb[host.name] = rfb;
          rfb.addEventListener('credentialsrequired', (x) => { e.credTypen = x.detail.types; rfb.sendCredentials(opt.cred); });
          rfb.addEventListener('desktopname', (x) => { e.name = x.detail.name; });
          rfb.addEventListener('clipboard', (x) => { (window.__ablage = window.__ablage || []).push(x.detail.text); });
          rfb.addEventListener('securityfailure', (x) => { e.secFehler = x.detail; });
          rfb.addEventListener('disconnect', (x) => { window.__getrennt = (window.__getrennt || []).concat([host.name + ':' + JSON.stringify(x.detail)]); });
          rfb.addEventListener('connect', () => { setTimeout(() => { clearTimeout(zeit); e.verbunden = true; e.breite = rfb._fbWidth; e.hoehe = rfb._fbHeight; ok(e); }, 700); });
        }
      });
      sock.addEventListener('close', (c) => { e.schluss = c.code; });
    });
    return h;
  })()`);
  console.log('Hosts:', Object.values(hosts).map((h) => h.name + '#' + h.id + ' vnc=' + h.vnc + ' port=' + h.vncPort + ' weg=' + h.vncWeg).join(', '));

  const fehler = [];
  const pruefe = (bed, text) => { if (!bed) { fehler.push(text); console.log('FEHLER:', text); } else console.log('ok:', text); };

  // A: Rueckfrage, speichern, verbinden.
  let e = await ev(`__verbinde(${JSON.stringify(hosts.A)}, { vncPasswort: 'geheim', speichern: true })`);
  const bereitA = e.texte.find((n) => n.t === 'bereit') || {};
  pruefe(e.texte.some((n) => n.t === 'vnc-passwort' && n.ziel === 'probe-A'), 'A: Rueckfrage vnc-passwort');
  pruefe(e.verbunden && e.name === 'Keyway Testbild' && e.breite === 1024 && e.hoehe === 768, 'A: noVNC verbunden ' + JSON.stringify([e.name, e.breite, e.hoehe]));
  pruefe(bereitA.durchreichen === false && bereitA.weg === 'direkt' && bereitA.gespeichert === true, 'A: bereit ' + JSON.stringify(bereitA));
  pruefe(e.binaerVorBereit === 0, 'A: keine Binaerdaten vor bereit');
  await warte(500);
  await foto('A-verbunden');
  // Klick in die Bildflaeche (noVNC skaliert nicht: Pixel = Pixel).
  await maus('mouseMoved', 300, 400); await maus('mousePressed', 300, 400); await maus('mouseReleased', 300, 400);
  await ev(`(() => { __rfb['probe-A'].sendCtrlAltDel(); __rfb['probe-A'].clipboardPasteFrom('Hallo Keyway'); return 1; })()`);
  await maus('mouseMoved', 600, 500); await maus('mousePressed', 600, 500, 'right'); await maus('mouseReleased', 600, 500, 'right');
  await warte(800);
  await foto('A-nach-klick');
  pruefe((await ev('(window.__ablage || []).join("|")')).includes(' Klick '), 'A: Zwischenablage vom Server empfangen');
  // Fernaufloesung: Bildflaeche verkleinern, resizeSession an.
  await ev(`(() => { const r = __rfb['probe-A']; r._target.style.width = '900px'; r._target.style.height = '640px'; r.resizeSession = true; return 1; })()`);
  await warte(1500);
  const groesse = await ev(`[__rfb['probe-A']._fbWidth, __rfb['probe-A']._fbHeight]`);
  pruefe(groesse[0] === 900 && groesse[1] === 640, 'A: Fernaufloesung ' + groesse);
  await foto('A-fernaufloesung');
  await ev(`(() => { __rfb['probe-A'].disconnect(); document.querySelectorAll('body > div[style*="z-index:99999"]').forEach((d) => d.remove()); return 1; })()`);

  // A erneut: gespeichertes Passwort, keine Rueckfrage.
  e = await ev(`__verbinde(${JSON.stringify(hosts.A)}, {})`);
  pruefe(e.verbunden && !e.texte.some((n) => n.t === 'vnc-passwort'), 'A: zweites Mal ohne Rueckfrage');
  await ev(`(() => { __rfb['probe-A'].disconnect(); document.querySelectorAll('body > div[style*="z-index:99999"]').forEach((d) => d.remove()); return 1; })()`);

  // B: Durchreichen, noVNC meldet sich mit VeNCrypt Plain an.
  e = await ev(`__verbinde(${JSON.stringify(hosts.B)}, { cred: { username: 'ui', password: 'plainpw' } })`);
  const bereitB = e.texte.find((n) => n.t === 'bereit') || {};
  pruefe(bereitB.durchreichen === true, 'B: bereit durchreichen ' + JSON.stringify(bereitB));
  pruefe(e.verbunden && e.name === 'Mit VeNCrypt', 'B: noVNC per VeNCrypt verbunden ' + JSON.stringify([e.name, e.credTypen]));
  await foto('B-durchreichen');
  await ev(`(() => { __rfb['probe-B'].disconnect(); document.querySelectorAll('body > div[style*="z-index:99999"]').forEach((d) => d.remove()); return 1; })()`);

  // C: RFB 3.3 ohne Passwort.
  e = await ev(`__verbinde(${JSON.stringify(hosts.C)}, { sprache: 'en', b: 800, h: 600 })`);
  pruefe(e.verbunden && e.name === 'Alt 3.3' && e.breite === 800, 'C: RFB 3.3 verbunden ' + JSON.stringify([e.name, e.breite, e.hoehe]));
  await foto('C-rfb33');
  await ev(`(() => { __rfb['probe-C'].disconnect(); document.querySelectorAll('body > div[style*="z-index:99999"]').forEach((d) => d.remove()); return 1; })()`);

  // D: ueber den SSH-Tunnel zu A.
  if (hosts.D) {
    e = await ev(`__verbinde(${JSON.stringify(hosts.D)}, {})`);
    const bereitD = e.texte.find((n) => n.t === 'bereit') || {};
    pruefe(e.texte.some((n) => n.t === 'hostkey'), 'D: Host-Schluessel-Rueckfrage');
    pruefe(bereitD.weg === 'ssh' && /^SHA256:/.test(bereitD.fp || ''), 'D: bereit ueber SSH ' + JSON.stringify(bereitD));
    pruefe(e.verbunden && e.name === 'Keyway Testbild', 'D: noVNC ueber den Tunnel verbunden ' + JSON.stringify([e.name, e.breite, e.hoehe]));
    await foto('D-ssh-tunnel');
    await ev(`(() => { __rfb['probe-D'].disconnect(); document.querySelectorAll('body > div[style*="z-index:99999"]').forEach((d) => d.remove()); return 1; })()`);
  }

  // Host-Daten: Passwort gespeichert, nie im Klartext.
  const d = await ev(`__api('GET', '/api/daten').then((r) => r.daten)`);
  const a = d.hosts.find((h) => h.name === 'probe-A');
  pruefe(a.hatVncPasswort === true && !JSON.stringify(d).includes('geheim'), 'API: hatVncPasswort, kein Klartext');
  console.log('getrennt:', await ev('JSON.stringify(window.__getrennt || [])'));
  const kons = konsole.filter((k) => /^(error|exception)/.test(k));
  if (kons.length) console.log('Konsole:', kons.join('\n'));
  console.log(fehler.length ? 'PROBE ROT: ' + fehler.length + ' Fehler' : 'PROBE GRUEN');
  process.exit(fehler.length ? 1 : 0);
})().catch((err) => { console.error(err); process.exit(2); });
