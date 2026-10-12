'use strict';
// Dateileiste (SFTP) — folgt auf Wunsch dem Arbeitsverzeichnis des Terminals.

class Dateien {
  constructor() {
    this.pfad = '';
    this.folgen = lokal('dateienFolgen', true);
    this.tab = null;
    this.eintraege = [];
    this.listeAktuell = false; // zeigt die Liste gerade this.eintraege von this.pfad?
    this.pfadFeld = el('input', { type: 'text', 'aria-label': 'Pfad', autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false',
      onkeydown: (e) => { if (e.key === 'Enter') { this.folgenSetzen(false); this.laden(this.pfadFeld.value.trim()); } } });
    this.liste = el('div', { class: 'seite-inhalt', role: 'list' });
    // Schalter «folgt Terminal»: kurzer Text, volle Hoehe als Trefferflaeche (Telefon).
    this.folgenKnopf = el('button', { type: 'button', 'aria-label': 'Dem Terminal folgen', onclick: () => this.folgenSetzen(!this.folgen),
      style: { display: 'inline-flex', alignItems: 'center', alignSelf: 'stretch', minHeight: '44px', padding: '0 4px', marginRight: 'auto', flex: 'none' } });
    this.hochInput = el('input', { type: 'file', multiple: true, class: 'versteckt', onchange: () => { this.hochladen([...this.hochInput.files]); this.hochInput.value = ''; } });
    this.fortschritt = el('div', { class: 'fortschritt versteckt' });
    this.ablage = el('div', { class: 'ablage nur-desktop' }, 'Dateien hierher ziehen zum Hochladen');
    this.knoepfe = {
      hoch: ikonKnopf('hoch', 'Hochladen', () => this.hochladenWaehlen()),
      ordner: ikonKnopf('ordnerPlus', 'Neuer Ordner', () => this.neuerOrdner()),
      hoeher: ikonKnopf('aufwaerts', 'Eine Ebene höher', () => this.hoeher()),
      neu: ikonKnopf('neu', 'Neu laden', () => this.laden(this.pfad)),
    };
    this.element = el('aside', { class: 'seite dateien', 'aria-label': 'Dateien' },
      el('div', { class: 'blatt-griff', 'aria-hidden': 'true' }),
      el('div', { class: 'seite-kopf' },
        el('span', { class: 'titel', style: { flex: 'none' } }, 'Dateien'),
        this.folgenKnopf,
        // Knopfgruppe ohne Zwischenraum: passt auch in die schmale Leiste am Desktop.
        el('span', { style: { display: 'flex', flex: 'none' } },
          this.knoepfe.hoch, this.knoepfe.ordner,
          ikonKnopf('x', 'Dateien schliessen', () => App.seiteUmschalten('dateien', false)))),
      el('div', { class: 'pfadleiste' }, this.knoepfe.hoeher, this.pfadFeld, this.knoepfe.neu),
      this.liste, this.hochInput, this.ablage, this.fortschritt);
    this.folgenZeichnen();
    this.knoepfeSetzen();
    // Ziehen und Ablegen
    this.element.addEventListener('dragover', (e) => { e.preventDefault(); this.ablage.classList.add('ziel'); });
    this.element.addEventListener('dragleave', () => this.ablage.classList.remove('ziel'));
    this.element.addEventListener('drop', (e) => { e.preventDefault(); this.ablage.classList.remove('ziel'); this.hochladen([...e.dataTransfer.files]); });
  }

  folgenSetzen(an) {
    this.folgen = an;
    lokalSetzen('dateienFolgen', an);
    this.folgenZeichnen();
    if (an) this.pfadPruefen(true);
  }
  folgenZeichnen() {
    const k = this.folgenKnopf;
    k.setAttribute('aria-pressed', this.folgen ? 'true' : 'false');
    k.title = this.folgen ? 'Folgt dem Terminal: wechselt mit cd mit. Antippen zum Festhalten.' : 'Bleibt im gewählten Ordner. Antippen, um dem Terminal zu folgen.';
    ersetzen(k, el('span', { class: 'chip' + (this.folgen ? ' gut' : '') }, icon(this.folgen ? 'terminal' : 'schloss', 13), this.folgen ? 'folgt' : 'fest'));
  }

  sichtbar() { return App.seiteOffen('dateien'); }
  bereit() { return !!this.tab && this.tab.zustand === 'bereit'; }

  // Ohne verbundene Sitzung tun die Knoepfe nichts — also sichtbar sperren.
  knoepfeSetzen() {
    const ok = this.bereit();
    const ordnerDa = ok && !!this.pfad && this.listeAktuell;
    this.knoepfe.hoch.disabled = !ordnerDa;
    this.knoepfe.ordner.disabled = !ordnerDa;
    this.knoepfe.hoeher.disabled = !ok || !this.pfad || this.pfad === '/';
    this.knoepfe.neu.disabled = !ok;
    this.pfadFeld.disabled = !ok; // gesperrte Knoepfe sind per .ikon:disabled abgedunkelt (app.css)
  }

  platzhalter() {
    const t = this.tab;
    const text = !t ? 'Keine Sitzung offen. Erst mit einem Host verbinden.'
      : (t.zustand === 'verbinde' || t.zustand === 'getrennt') ? 'Wartet auf die Verbindung …'
        : 'Diese Sitzung ist nicht verbunden. Erst neu verbinden.';
    this.listeAktuell = false;
    ersetzen(this.liste, el('div', { class: 'leer' }, text));
  }

  // Aufruf bei Tab-Wechsel und beim Oeffnen der Leiste (auto: ohne Zutun des Benutzers).
  fuerTab(tab, auto) {
    if (this.tab !== tab) { this.tab = tab; this.pfad = ''; this.eintraege = []; this.listeAktuell = false; this.pfadZeigen(''); }
    this.knoepfeSetzen();
    if (!this.sichtbar()) return;
    if (!this.bereit()) { this.platzhalter(); return; }
    if (!this.pfad) this.pfadPruefen(true, auto);
    else if (!this.listeAktuell) this.laden(this.pfad, auto);
  }

  // Aufruf bei jedem Zustandswechsel eines Tabs (terminal.js: zustandSetzen).
  zustandGeaendert(tab) {
    if (tab === this.tab) this.fuerTab(tab, true);
  }

  // Pfad-Abfrage fuer «folgt Terminal». Ohne erzwingen kommt sie vom Zeitgeber (app.js) und
  // zaehlt nicht als Bedienung (X-SSH-App-Auto: 1, verlaengert die Anmeldung nicht).
  async pfadPruefen(erzwingen, auto = !erzwingen) {
    const tab = this.tab;
    this.knoepfeSetzen();
    if (!this.bereit() || !this.sichtbar()) return;
    if (!this.folgen) { if (erzwingen || !this.pfad || !this.listeAktuell) this.laden(this.pfad, auto); return; }
    try {
      const r = await api('GET', '/api/sitzungen/' + tab.sitzungId + '/pfad', undefined, { auto: true });
      if (tab !== this.tab) return;
      if (r.bekannt && r.pfad && r.pfad !== this.pfad) this.laden(r.pfad, auto);
      else if (erzwingen || !this.listeAktuell) this.laden(this.pfad, auto);
    } catch (e) { /* still: naechster Versuch */ }
  }

  async laden(pfad, auto) {
    const tab = this.tab;
    if (!this.bereit()) return;
    try {
      const r = await api('GET', '/api/sitzungen/' + tab.sitzungId + '/dateien?pfad=' + encodeURIComponent(pfad || ''), undefined, auto ? { auto: true } : undefined);
      if (tab !== this.tab) return;
      this.pfad = r.pfad;
      this.eintraege = r.eintraege || [];
      if (document.activeElement !== this.pfadFeld) this.pfadZeigen(r.pfad);
      this.zeichnen();
    } catch (e) {
      if (tab !== this.tab) return;
      // Den versuchten Ordner merken: «Eine Ebene höher» geht von dort aus, und «folgt Terminal»
      // fragt einen unlesbaren Ordner nicht alle zwei Sekunden neu ab.
      if (pfad && pfad.startsWith('/')) this.pfad = pfad;
      this.eintraege = [];
      this.listeAktuell = false;
      ersetzen(this.liste, el('div', { class: 'leer' }, e.message));
      this.pfadZeigen(pfad || '');
    }
    this.knoepfeSetzen();
  }

  // Pfad anzeigen, das Ende (aktueller Ordner) bleibt sichtbar.
  pfadZeigen(p) {
    this.pfadFeld.value = p;
    this.pfadFeld.scrollLeft = this.pfadFeld.scrollWidth;
  }

  voll(name) { return (this.pfad === '/' ? '' : this.pfad) + '/' + name; }

  hoeher() {
    if (!this.bereit() || !this.pfad || this.pfad === '/') return;
    this.folgenSetzen(false);
    this.laden(this.pfad.replace(/\/[^/]+\/?$/, '') || '/');
  }

  zeichnen() {
    const zeilen = this.eintraege.map((e) => {
      const kn = el('button', { class: 'datei', type: 'button', role: 'listitem',
        onclick: (ev) => e.ordner ? (this.folgenSetzen(false), this.laden(this.voll(e.name))) : this.dateiMenue(e, ev.currentTarget),
        oncontextmenu: (ev) => { ev.preventDefault(); this.dateiMenue(e, ev.currentTarget); } },
        el('span', { style: { color: e.ordner ? 'var(--akzent)' : 'var(--leise)', display: 'inline-flex' } }, icon(e.ordner ? 'ordner' : e.link ? 'link' : 'datei', 16)),
        el('span', { class: 'n' }, e.name + (e.ordner ? '/' : '')),
        el('span', { class: 'g' }, e.ordner ? '' : groesseText(e.groesse)));
      return el('div', { class: 'zeile-wrap' }, kn, ikonKnopf('mehr', 'Aktionen für ' + e.name, (ev) => this.dateiMenue(e, ev.currentTarget)));
    });
    this.listeAktuell = true;
    ersetzen(this.liste, ...(zeilen.length ? zeilen : [el('div', { class: 'leer' }, 'Ordner ist leer')]));
  }

  dateiMenue(e, anker) {
    const p = this.voll(e.name);
    menue(anker, [
      e.ordner ? { text: 'Öffnen', icon: 'ordner', aktion: () => { this.folgenSetzen(false); this.laden(p); } } : null,
      !e.ordner ? { text: 'Herunterladen', icon: 'runter', aktion: () => herunterladen('/api/sitzungen/' + this.tab.sitzungId + '/laden?pfad=' + encodeURIComponent(p)) } : null,
      { text: 'Pfad ins Terminal einfügen', icon: 'einfuegen', aktion: () => { App.inTerminal(shellZitat(p) + ' '); } },
      e.ordner ? { text: 'Im Terminal hierhin wechseln', icon: 'terminal', aktion: () => this.imTerminalWechseln(p) } : null,
      { text: 'Pfad kopieren', icon: 'kopieren', aktion: () => kopieren(p) },
      { text: 'Umbenennen', icon: 'stift', aktion: () => this.umbenennen(e) },
      { text: 'Löschen', icon: 'muell', gefahr: true, aktion: () => this.loeschen(e) },
    ]);
  }

  // cd im Terminal AUSFUEHREN. Die Eingabetaste geht hinter der schliessenden Klammer von
  // Bracketed Paste: Bash >= 5.1 fuehrt ein mit eingefuegtes \r nicht aus. Vorher wird die
  // Eingabezeile geleert (Strg+E, Strg+U), damit nichts Halbgetipptes mit ausgefuehrt wird.
  // Nur an diese Sitzung (nicht «An alle senden»), ohne die Strg/Alt-Tasten der Telefonleiste.
  imTerminalWechseln(p) {
    const t = this.tab;
    if (!t || t.zustand !== 'bereit') { toast('Keine verbundene Sitzung', 'fehler'); return; }
    const puffer = t.term && t.term.buffer && t.term.buffer.active;
    if (puffer && puffer.type === 'alternate') { toast('Im Terminal läuft ein Vollbild-Programm (z.B. nano oder less) — erst beenden', 'fehler'); return; }
    const befehl = 'cd ' + shellZitat(p);
    const klammer = t.term && t.term.modes && t.term.modes.bracketedPasteMode;
    t.sendenRoh(new TextEncoder().encode('\x05\x15' + (klammer ? '\x1b[200~' + befehl + '\x1b[201~' : befehl) + '\r'));
    if (typeof t.startMerken === 'function') t.startMerken('\r'); // Befehlshilfe: neue Eingabezeile
    App.seiteUmschalten('dateien', false, true);
    if (!App.istTelefon() && t.term) t.term.focus();
  }

  async aktion(art, pfad, nach, erlaubt) {
    await api('POST', '/api/sitzungen/' + this.tab.sitzungId + '/aktion', { art, pfad, nach, ersetzen: !!erlaubt });
    this.laden(this.pfad);
  }
  async neuerOrdner() {
    if (!this.bereit() || !this.pfad) { toast('Zuerst eine Sitzung öffnen', 'fehler'); return; }
    const n = ((await eingabe('Name des Ordners', { titel: 'Neuer Ordner', ok: 'Anlegen' })) || '').trim();
    if (!n) return;
    if (this.eintraege.some((x) => x.name === n)) { toast(`«${n}» gibt es dort schon`, 'fehler'); return; }
    this.aktion('ordner', this.voll(n)).catch(fehlerToast);
  }
  async umbenennen(e) {
    const n = ((await eingabe('Neuer Name', { titel: 'Umbenennen', wert: e.name, ok: 'Umbenennen' })) || '').trim();
    if (!n || n === e.name) return;
    // Umbenennen ersetzt nie still eine andere Datei: erst fragen (der Server prueft ebenfalls).
    const da = this.eintraege.find((x) => x.name === n);
    let erlaubt = false;
    if (da) {
      if (da.ordner) { toast(`«${n}» ist dort ein Ordner — bitte einen anderen Namen wählen`, 'fehler'); return; }
      if (!(await frage(`«${n}» gibt es dort schon. Ersetzen? Der bisherige Inhalt von «${n}» geht dabei verloren.`, { titel: 'Umbenennen', ja: 'Ersetzen', gefahr: true }))) return;
      erlaubt = true;
    }
    this.aktion('umbenennen', this.voll(e.name), this.voll(n), erlaubt).catch(fehlerToast);
  }
  async loeschen(e) {
    const ok = await frage(e.ordner ? `Den Ordner «${e.name}» mit allem Inhalt löschen?` : `«${e.name}» löschen?`, { titel: 'Löschen', ja: 'Löschen', gefahr: true });
    if (ok) this.aktion('loeschen', this.voll(e.name)).catch(fehlerToast);
  }

  // ---------- Hochladen ----------

  hochladenWaehlen() {
    if (!this.bereit() || !this.pfad) { toast('Zuerst eine Sitzung öffnen', 'fehler'); return; }
    this.hochInput.click();
  }

  // Eine Rueckfrage fuer alle schon vorhandenen Namen. Ergebnis: 'ersetzen' | 'ueberspringen' | 'abbrechen'.
  ersetzenFragen(namen, gesamt) {
    return new Promise((resolve) => {
      let wahl = 'abbrechen';
      const waehle = (w) => () => { wahl = w; d.zu(); };
      const zuViele = namen.length > 8;
      const rest = gesamt - namen.length;
      const inhalt = el('div', { style: { display: 'flex', flexDirection: 'column', gap: '10px', lineHeight: 1.55 } },
        el('p', { style: { margin: 0 } }, namen.length === 1 ? `«${namen[0]}» gibt es dort schon.` : `Diese ${namen.length} Dateien gibt es dort schon:`),
        namen.length > 1 ? el('ul', { style: { margin: 0, paddingLeft: '20px', fontFamily: 'var(--mono)', fontSize: '13px', wordBreak: 'break-all' } },
          namen.slice(0, 8).map((n) => el('li', null, n)), zuViele ? el('li', null, `… und ${namen.length - 8} weitere`) : null) : null,
        el('p', { style: { margin: 0, color: 'var(--leise)', fontSize: '13px' } },
          'Ersetzen überschreibt ' + (namen.length === 1 ? 'sie' : 'alle') + '.' + (rest > 0 ? ` Überspringen lädt nur ${rest === 1 ? 'die neue Datei' : 'die ' + rest + ' neuen'} hoch.` : '')));
      const knoepfe = [el('button', { class: 'btn', type: 'button', onclick: waehle('abbrechen') }, 'Abbrechen')];
      if (rest > 0) knoepfe.push(el('button', { class: 'btn', type: 'button', onclick: waehle('ueberspringen') }, 'Überspringen'));
      knoepfe.push(el('button', { class: 'btn gefahr', type: 'button', onclick: waehle('ersetzen') }, 'Ersetzen'));
      const d = dialog({ titel: 'Schon vorhanden', inhalt, knoepfe, beimSchliessen: () => resolve(wahl) });
    });
  }

  async hochladen(dateien) {
    if (!dateien.length) return;
    if (!this.bereit() || !this.pfad) { toast('Zuerst eine Sitzung öffnen', 'fehler'); return; }
    const sitzung = this.tab.sitzungId, ziel = this.pfad;
    // Vorhandene Namen frisch abfragen, damit die Rueckfrage alle Konflikte auf einmal nennt.
    let da;
    try { da = (await api('GET', '/api/sitzungen/' + sitzung + '/dateien?pfad=' + encodeURIComponent(ziel))).eintraege || []; }
    catch (e) { fehlerToast(e); return; }
    const ordner = new Set(da.filter((e) => e.ordner).map((e) => e.name));
    const vorhanden = new Set(da.filter((e) => !e.ordner).map((e) => e.name));
    const gesperrt = dateien.filter((f) => ordner.has(f.name)).map((f) => f.name);
    if (gesperrt.length) toast((gesperrt.length === 1 ? `«${gesperrt[0]}» ist dort ein Ordner` : 'Dort schon als Ordner vorhanden: ' + gesperrt.join(', ')) + ' — übersprungen', 'fehler');
    let auswahl = dateien.filter((f) => !ordner.has(f.name));
    const konflikte = [...new Set(auswahl.filter((f) => vorhanden.has(f.name)).map((f) => f.name))];
    let erlaubt = [];
    if (konflikte.length) {
      const wahl = await this.ersetzenFragen(konflikte, auswahl.length);
      if (wahl === 'abbrechen') return;
      if (wahl === 'ersetzen') erlaubt = konflikte;
      else auswahl = auswahl.filter((f) => !vorhanden.has(f.name));
    }
    if (auswahl.length) this.senden(sitzung, ziel, auswahl, erlaubt);
  }

  // Uebertraegt die Dateien. erlaubt = Namen, die vorhandene Dateien ersetzen duerfen (je Datei bestaetigt).
  senden(sitzung, ziel, dateien, erlaubt) {
    const fd = new FormData();
    dateien.forEach((f) => fd.append('datei', f, f.name));
    const xhr = new XMLHttpRequest();
    xhr.open('POST', '/api/sitzungen/' + sitzung + '/hochladen?pfad=' + encodeURIComponent(ziel) + erlaubt.map((n) => '&ersetzen=' + encodeURIComponent(n)).join(''));
    xhr.setRequestHeader('X-SSH-App', '1');
    const balken = el('div');
    const name = dateien.length === 1 ? dateien[0].name : dateien.length + ' Dateien';
    const prozent = el('span', null, '0 %');
    ersetzen(this.fortschritt,
      el('div', { class: 'reihe', style: { justifyContent: 'space-between', flexWrap: 'nowrap', gap: '8px' } },
        el('span', { style: { minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' } }, name),
        el('span', { class: 'reihe', style: { flexWrap: 'nowrap', gap: '2px' } }, prozent, ikonKnopf('x', 'Hochladen abbrechen', () => xhr.abort()))),
      el('div', { class: 'balken' }, balken));
    this.fortschritt.classList.remove('versteckt');
    const fertig = () => {
      this.fortschritt.classList.add('versteckt');
      if (this.pfad === ziel) this.laden(ziel);
    };
    xhr.upload.onprogress = (e) => {
      if (!e.lengthComputable) return;
      const p = Math.round((e.loaded / e.total) * 100);
      balken.style.width = p + '%'; prozent.textContent = p + ' %';
    };
    xhr.onload = async () => {
      fertig();
      let a = {};
      try { a = JSON.parse(xhr.responseText) || {}; } catch (e) { /* */ }
      const hoch = a.hochgeladen || [];
      if (xhr.status === 401) { if (App.abgemeldet) App.abgemeldet(); return; }
      if (xhr.status === 409 && a.vorhanden && a.vorhanden.length) {
        // Sicherheitsnetz des Servers: Der Name ist erst nach der Rueckfrage entstanden.
        if (hoch.length) toast(textHochgeladen(hoch));
        const offen = dateien.filter((f) => a.vorhanden.includes(f.name));
        if ((await this.ersetzenFragen(a.vorhanden, a.vorhanden.length)) === 'ersetzen') this.senden(sitzung, ziel, offen, a.vorhanden);
        return;
      }
      if (xhr.status !== 200) {
        toast((a.fehler || 'Hochladen fehlgeschlagen') + (hoch.length ? ' (' + textHochgeladen(hoch) + ')' : ''), 'fehler');
        return;
      }
      toast(textHochgeladen(hoch.length ? hoch : dateien.map((f) => f.name)));
    };
    xhr.onerror = () => { fertig(); toast('Hochladen abgebrochen — die Verbindung ist unterbrochen. Unvollständige Dateien wurden verworfen.', 'fehler'); };
    xhr.onabort = () => { fertig(); toast('Hochladen abgebrochen. Unvollständige Dateien wurden verworfen.'); };
    xhr.send(fd);
  }
}

function textHochgeladen(namen) {
  return (namen.length === 1 ? '«' + namen[0] + '»' : namen.length + ' Dateien') + ' hochgeladen';
}

function shellZitat(s) {
  return /^[A-Za-z0-9_\/.,:+@%=-]+$/.test(s) ? s : "'" + s.replace(/'/g, "'\\''") + "'";
}
App.Dateien = Dateien;
