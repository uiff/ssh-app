'use strict';
// Dateileiste (SFTP) — folgt auf Wunsch dem Arbeitsverzeichnis des Terminals.

// Spezialdateien (Art aus der Liste des Servers): lassen sich weder herunterladen noch ersetzen.
const DATEI_SPEZIAL = ['geraet', 'pipe', 'socket'];

class Dateien {
  constructor() {
    this.pfad = '';
    this.folgen = lokal('dateienFolgen', true);
    this.tab = null;
    this.eintraege = [];
    this.listeAktuell = false; // zeigt die Liste gerade this.eintraege von this.pfad?
    this.fehlPfad = null; // Ordner, dessen Liste zuletzt dauerhaft scheiterte (403/404/400)
    this.pfadFeld = el('input', { type: 'text', 'aria-label': t('dateien.pfad'), autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false',
      onkeydown: (e) => { if (e.key === 'Enter') { this.folgenSetzen(false); this.laden(this.pfadFeld.value.trim()); } } });
    this.liste = el('div', { class: 'seite-inhalt', role: 'list' });
    // Schalter «folgt Terminal»: kurzer Text, volle Hoehe als Trefferflaeche (Telefon).
    this.folgenKnopf = el('button', { type: 'button', 'aria-label': t('dateien.folgen.label'), onclick: () => this.folgenSetzen(!this.folgen),
      style: { display: 'inline-flex', alignItems: 'center', alignSelf: 'stretch', minHeight: '44px', padding: '0 4px', marginRight: 'auto', flex: 'none' } });
    this.hochInput = el('input', { type: 'file', multiple: true, class: 'versteckt', onchange: () => { this.hochladen([...this.hochInput.files]); this.hochInput.value = ''; } });
    this.fortschritt = el('div', { class: 'fortschritt versteckt' });
    this.ablage = el('div', { class: 'ablage nur-desktop' }, t('dateien.ablage'));
    this.knoepfe = {
      hoch: ikonKnopf('hoch', t('dateien.knopf.hochladen'), () => this.hochladenWaehlen()),
      ordner: ikonKnopf('ordnerPlus', t('dateien.knopf.neuerOrdner'), () => this.neuerOrdner()),
      hoeher: ikonKnopf('aufwaerts', t('dateien.knopf.hoeher'), () => this.hoeher()),
      neu: ikonKnopf('neu', t('dateien.knopf.neuLaden'), () => this.laden(this.pfad)),
    };
    this.element = el('aside', { class: 'seite dateien', 'aria-label': t('dateien.titel') },
      el('div', { class: 'blatt-griff', 'aria-hidden': 'true' }),
      el('div', { class: 'seite-kopf' },
        el('span', { class: 'titel', style: { flex: 'none' } }, t('dateien.titel')),
        this.folgenKnopf,
        // Knopfgruppe ohne Zwischenraum: passt auch in die schmale Leiste am Desktop.
        el('span', { style: { display: 'flex', flex: 'none' } },
          this.knoepfe.hoch, this.knoepfe.ordner,
          ikonKnopf('x', t('dateien.knopf.schliessen'), () => App.seiteUmschalten('dateien', false)))),
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
    k.title = this.folgen ? t('dateien.folgen.anTitel') : t('dateien.folgen.ausTitel');
    ersetzen(k, el('span', { class: 'chip' + (this.folgen ? ' gut' : '') }, icon(this.folgen ? 'terminal' : 'schloss', 13),
      this.folgen ? t('dateien.folgen.an') : t('dateien.folgen.aus')));
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
    const tab = this.tab;
    const text = !tab ? t('dateien.platzhalter.keineSitzung')
      : (tab.zustand === 'verbinde' || tab.zustand === 'getrennt') ? t('dateien.platzhalter.wartet')
        : t('dateien.platzhalter.nichtVerbunden');
    this.listeAktuell = false;
    ersetzen(this.liste, el('div', { class: 'leer' }, text));
  }

  // Aufruf bei Tab-Wechsel und beim Oeffnen der Leiste (auto: ohne Zutun des Benutzers).
  fuerTab(tab, auto) {
    if (this.tab !== tab) { this.tab = tab; this.pfad = ''; this.eintraege = []; this.listeAktuell = false; this.fehlPfad = null; this.pfadZeigen(''); }
    this.knoepfeSetzen();
    if (!this.sichtbar()) return;
    if (!this.bereit()) { this.platzhalter(); return; }
    if (!this.pfad) this.pfadPruefen(true, auto);
    else if (!this.listeAktuell && (!auto || this.pfad !== this.fehlPfad)) this.laden(this.pfad, auto);
  }

  // Aufruf bei jedem Zustandswechsel eines Tabs (terminal.js: zustandSetzen). Nach dem
  // Wiederverbinden darf ein zuvor unlesbarer Ordner neu versucht werden.
  zustandGeaendert(tab) {
    if (tab !== this.tab) return;
    if (!this.bereit()) this.fehlPfad = null;
    this.fuerTab(tab, true);
  }

  // Pfad-Abfrage fuer «folgt Terminal». Ohne erzwingen kommt sie vom Zeitgeber (app.js) und
  // zaehlt nicht als Bedienung (X-SSH-App-Auto: 1, verlaengert die Anmeldung nicht).
  // Ein Ordner, dessen Liste dauerhaft scheiterte (keine Rechte, gibt es nicht), wird erst
  // nach einem Pfadwechsel oder auf Knopfdruck wieder abgefragt.
  async pfadPruefen(erzwingen, auto = !erzwingen) {
    const tab = this.tab;
    this.knoepfeSetzen();
    if (!this.bereit() || !this.sichtbar()) return;
    const nochmal = () => erzwingen || (!this.listeAktuell && this.pfad !== this.fehlPfad);
    if (!this.folgen) { if (!this.pfad || nochmal()) this.laden(this.pfad, auto); return; }
    try {
      const r = await api('GET', '/api/sitzungen/' + tab.sitzungId + '/pfad', undefined, { auto: true });
      if (tab !== this.tab) return;
      if (r.bekannt && r.pfad && r.pfad !== this.pfad) this.laden(r.pfad, auto);
      else if (!this.pfad || nochmal()) this.laden(this.pfad, auto);
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
      this.fehlPfad = null;
      if (document.activeElement !== this.pfadFeld) this.pfadZeigen(r.pfad);
      this.zeichnen();
    } catch (e) {
      if (tab !== this.tab) return;
      // Den versuchten Ordner merken: «Eine Ebene höher» geht von dort aus. Bei einem
      // dauerhaften Fehler (keine Rechte, gibt es nicht, kein Ordner) fragt «folgt Terminal»
      // ihn nicht alle zwei Sekunden neu ab; Verbindungsfehler werden weiter versucht.
      if (pfad && pfad.startsWith('/')) this.pfad = pfad;
      this.fehlPfad = [400, 403, 404].includes(e.status) ? this.pfad : null;
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

  // Rechte und Aenderungszeit fuer den Tooltip einer Zeile.
  zeileTitel(e) {
    const d = new Date(e.geaendert);
    const datum = isNaN(d) || d.getFullYear() < 1971 ? '' : d.toLocaleString(lokalCode(), { dateStyle: 'medium', timeStyle: 'short' });
    return datum ? t('dateien.zeile.titel', { rechte: e.rechte || '', datum }) : (e.rechte || '');
  }

  // Rechte Spalte: Groesse bei Dateien, sonst die Art (Geraet, Pipe …).
  zeileGroesse(e) {
    if (e.ordner) return '';
    if (e.art === 'datei' || !e.art) return groesseText(e.groesse);
    return t('dateien.art.' + e.art);
  }

  zeichnen() {
    const zeilen = this.eintraege.map((e) => {
      const kn = el('button', { class: 'datei', type: 'button', role: 'listitem', title: this.zeileTitel(e),
        onclick: (ev) => e.ordner ? (this.folgenSetzen(false), this.laden(this.voll(e.name))) : this.dateiMenue(e, ev.currentTarget),
        oncontextmenu: (ev) => { ev.preventDefault(); this.dateiMenue(e, ev.currentTarget); } },
        el('span', { style: { color: e.ordner ? 'var(--akzent)' : 'var(--leise)', display: 'inline-flex' } }, icon(e.ordner ? 'ordner' : e.link ? 'link' : DATEI_SPEZIAL.includes(e.art) ? e.art : 'datei', 16)),
        el('span', { class: 'n' }, e.name + (e.ordner ? '/' : '')),
        el('span', { class: 'g' }, this.zeileGroesse(e)));
      return el('div', { class: 'zeile-wrap' }, kn, ikonKnopf('mehr', t('dateien.aktionenFuer', { name: e.name }), (ev) => this.dateiMenue(e, ev.currentTarget)));
    });
    this.listeAktuell = true;
    ersetzen(this.liste, ...(zeilen.length ? zeilen : [el('div', { class: 'leer' }, t('dateien.leer'))]));
  }

  dateiMenue(e, anker) {
    const p = this.voll(e.name);
    // Herunterladen nur fuer gewoehnliche Dateien (aeltere Server ohne «art»: alles ausser Ordnern).
    const ladbar = !e.ordner && (e.art === 'datei' || !e.art);
    menue(anker, [
      e.ordner ? { text: t('dateien.menue.oeffnen'), icon: 'ordner', aktion: () => { this.folgenSetzen(false); this.laden(p); } } : null,
      ladbar ? { text: t('dateien.menue.herunterladen'), icon: 'runter', aktion: () => this.herunterladen(p) } : null,
      { text: t('dateien.menue.pfadEinfuegen'), icon: 'einfuegen', aktion: () => { App.inTerminal(shellZitat(p) + ' '); } },
      e.ordner ? { text: t('dateien.menue.hierhinWechseln'), icon: 'terminal', aktion: () => this.imTerminalWechseln(p) } : null,
      { text: t('dateien.menue.pfadKopieren'), icon: 'kopieren', aktion: () => kopieren(p) },
      { text: t('dateien.menue.umbenennen'), icon: 'stift', aktion: () => this.umbenennen(e) },
      { text: t('dateien.menue.loeschen'), icon: 'muell', gefahr: true, aktion: () => this.loeschen(e) },
    ]);
  }

  // Erst pruefen, dann laden: Ein Download per Link zeigt eine Fehlerantwort des Servers
  // nicht an (der Browser meldet hoechstens «fehlgeschlagen»). Die Pruefung bringt den Grund.
  async herunterladen(p) {
    if (!this.bereit()) { toast(t('dateien.keineVerbindung'), 'fehler'); return; }
    const adresse = '/api/sitzungen/' + this.tab.sitzungId + '/laden?pfad=' + encodeURIComponent(p);
    try { await api('GET', adresse + '&pruefen=1'); }
    catch (e) { fehlerToast(e); return; }
    herunterladen(adresse);
  }

  // cd im Terminal AUSFUEHREN. Die Eingabetaste geht hinter der schliessenden Klammer von
  // Bracketed Paste: Bash >= 5.1 fuehrt ein mit eingefuegtes \r nicht aus. Vorher wird die
  // Eingabezeile geleert (Strg+E, Strg+U), damit nichts Halbgetipptes mit ausgefuehrt wird.
  // Nur an diese Sitzung (nicht «An alle senden»), ohne die Strg/Alt-Tasten der Telefonleiste.
  imTerminalWechseln(p) {
    const tab = this.tab;
    if (!tab || tab.zustand !== 'bereit') { toast(t('dateien.keineVerbindung'), 'fehler'); return; }
    const puffer = tab.term && tab.term.buffer && tab.term.buffer.active;
    if (puffer && puffer.type === 'alternate') { toast(t('dateien.vollbildProgramm'), 'fehler'); return; }
    const befehl = 'cd ' + shellZitat(p);
    const klammer = tab.term && tab.term.modes && tab.term.modes.bracketedPasteMode;
    tab.sendenRoh(new TextEncoder().encode('\x05\x15' + (klammer ? '\x1b[200~' + befehl + '\x1b[201~' : befehl) + '\r'));
    if (typeof tab.startMerken === 'function') tab.startMerken('\r'); // Befehlshilfe: neue Eingabezeile
    App.seiteUmschalten('dateien', false, true);
    if (!App.istTelefon() && tab.term) tab.term.focus();
  }

  async aktion(art, pfad, nach, erlaubt) {
    await api('POST', '/api/sitzungen/' + this.tab.sitzungId + '/aktion', { art, pfad, nach, ersetzen: !!erlaubt });
    this.laden(this.pfad);
  }
  async neuerOrdner() {
    if (!this.bereit() || !this.pfad) { toast(t('dateien.zuerstSitzung'), 'fehler'); return; }
    const n = ((await eingabe(t('dateien.ordner.name'), { titel: t('dateien.ordner.titel'), ok: t('dateien.ordner.anlegen') })) || '').trim();
    if (!n) return;
    if (this.eintraege.some((x) => x.name === n)) { toast(t('dateien.vorhanden', { name: n }), 'fehler'); return; }
    this.aktion('ordner', this.voll(n)).catch(fehlerToast);
  }
  async umbenennen(e) {
    const n = ((await eingabe(t('dateien.umbenennen.name'), { titel: t('dateien.umbenennen.titel'), wert: e.name, ok: t('dateien.umbenennen.ok') })) || '').trim();
    if (!n || n === e.name) return;
    // Umbenennen ersetzt nie still eine andere Datei: erst fragen (der Server prueft ebenfalls).
    const da = this.eintraege.find((x) => x.name === n);
    let erlaubt = false;
    if (da) {
      if (da.ordner) { toast(t('dateien.umbenennen.istOrdner', { name: n }), 'fehler'); return; }
      if (!(await frage(t('dateien.umbenennen.ersetzenFrage', { name: n }), { titel: t('dateien.umbenennen.titel'), ja: t('dateien.ersetzen'), gefahr: true }))) return;
      erlaubt = true;
    }
    this.aktion('umbenennen', this.voll(e.name), this.voll(n), erlaubt).catch(fehlerToast);
  }
  async loeschen(e) {
    const text = e.ordner ? t('dateien.loeschen.ordnerFrage', { name: e.name }) : t('dateien.loeschen.frage', { name: e.name });
    const ok = await frage(text, { titel: t('dateien.loeschen.titel'), ja: t('dateien.loeschen.ja'), gefahr: true });
    if (ok) this.aktion('loeschen', this.voll(e.name)).catch(fehlerToast);
  }

  // ---------- Hochladen ----------

  hochladenWaehlen() {
    if (!this.bereit() || !this.pfad) { toast(t('dateien.zuerstSitzung'), 'fehler'); return; }
    this.hochInput.click();
  }

  // Eine Rueckfrage fuer alle schon vorhandenen Namen. Ergebnis: 'ersetzen' | 'ueberspringen' | 'abbrechen'.
  ersetzenFragen(namen, gesamt) {
    return new Promise((resolve) => {
      let wahl = 'abbrechen';
      const waehle = (w) => () => { wahl = w; d.zu(); };
      const zuViele = namen.length > 8;
      const rest = gesamt - namen.length;
      const hinweis = (namen.length === 1 ? t('dateien.konflikt.ersetztEine') : t('dateien.konflikt.ersetztAlle'))
        + (rest > 0 ? ' ' + (rest === 1 ? t('dateien.konflikt.ueberspringenEine') : t('dateien.konflikt.ueberspringenMehrere', { n: rest })) : '');
      const inhalt = el('div', { style: { display: 'flex', flexDirection: 'column', gap: '10px', lineHeight: 1.55 } },
        el('p', { style: { margin: 0 } }, namen.length === 1 ? t('dateien.konflikt.eine', { name: namen[0] }) : t('dateien.konflikt.mehrere', { n: namen.length })),
        namen.length > 1 ? el('ul', { style: { margin: 0, paddingLeft: '20px', fontFamily: 'var(--mono)', fontSize: '13px', wordBreak: 'break-all' } },
          namen.slice(0, 8).map((n) => el('li', null, n)), zuViele ? el('li', null, t('dateien.konflikt.weitere', { n: namen.length - 8 })) : null) : null,
        el('p', { style: { margin: 0, color: 'var(--leise)', fontSize: '13px' } }, hinweis));
      const knoepfe = [el('button', { class: 'btn', type: 'button', onclick: waehle('abbrechen') }, t('basis.abbrechen'))];
      if (rest > 0) knoepfe.push(el('button', { class: 'btn', type: 'button', onclick: waehle('ueberspringen') }, t('dateien.ueberspringen')));
      knoepfe.push(el('button', { class: 'btn gefahr', type: 'button', onclick: waehle('ersetzen') }, t('dateien.ersetzen')));
      const d = dialog({ titel: t('dateien.konflikt.titel'), inhalt, knoepfe, beimSchliessen: () => resolve(wahl) });
    });
  }

  async hochladen(dateien) {
    if (!dateien.length) return;
    if (!this.bereit() || !this.pfad) { toast(t('dateien.zuerstSitzung'), 'fehler'); return; }
    const sitzung = this.tab.sitzungId, ziel = this.pfad;
    // Vorhandene Namen frisch abfragen, damit die Rueckfrage alle Konflikte auf einmal nennt.
    let da;
    try { da = (await api('GET', '/api/sitzungen/' + sitzung + '/dateien?pfad=' + encodeURIComponent(ziel))).eintraege || []; }
    catch (e) { fehlerToast(e); return; }
    // Ordner und Spezialdateien lassen sich nicht durch eine Datei ersetzen: gleich ueberspringen.
    const sperre = new Set(da.filter((e) => e.ordner || DATEI_SPEZIAL.includes(e.art)).map((e) => e.name));
    const vorhanden = new Set(da.filter((e) => !sperre.has(e.name)).map((e) => e.name));
    const gesperrt = dateien.filter((f) => sperre.has(f.name)).map((f) => f.name);
    if (gesperrt.length) gesperrtMelden(gesperrt, da);
    let auswahl = dateien.filter((f) => !sperre.has(f.name));
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
    xhr.setRequestHeader('X-SSH-App-Sprache', I18N.sprache);
    const balken = el('div');
    const name = dateien.length === 1 ? dateien[0].name : t('dateien.anzahl', { n: dateien.length });
    const prozentText = (p) => (p / 100).toLocaleString(lokalCode(), { style: 'percent' });
    const prozent = el('span', null, prozentText(0));
    ersetzen(this.fortschritt,
      el('div', { class: 'reihe', style: { justifyContent: 'space-between', flexWrap: 'nowrap', gap: '8px' } },
        el('span', { style: { minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' } }, name),
        el('span', { class: 'reihe', style: { flexWrap: 'nowrap', gap: '2px' } }, prozent, ikonKnopf('x', t('dateien.hochladen.abbrechen'), () => xhr.abort()))),
      el('div', { class: 'balken' }, balken));
    this.fortschritt.classList.remove('versteckt');
    const fertig = () => {
      this.fortschritt.classList.add('versteckt');
      if (this.pfad === ziel) this.laden(ziel);
    };
    xhr.upload.onprogress = (e) => {
      if (!e.lengthComputable) return;
      const p = Math.round((e.loaded / e.total) * 100);
      balken.style.width = p + '%'; prozent.textContent = prozentText(p);
    };
    xhr.onload = async () => {
      fertig();
      let a = {};
      try { a = JSON.parse(xhr.responseText) || {}; } catch (e) { /* */ }
      const hoch = a.hochgeladen || [];
      if (xhr.status === 401) { if (App.abgemeldet) App.abgemeldet(); return; }
      if (xhr.status === 409 && ((a.vorhanden && a.vorhanden.length) || (a.gesperrt && a.gesperrt.length))) {
        // Sicherheitsnetz des Servers: Der Name ist erst nach der Rueckfrage entstanden.
        if (hoch.length) toast(textHochgeladen(hoch));
        if (a.gesperrt && a.gesperrt.length) gesperrtMelden(a.gesperrt, null);
        if (!a.vorhanden || !a.vorhanden.length) return;
        const offen = dateien.filter((f) => a.vorhanden.includes(f.name));
        if ((await this.ersetzenFragen(a.vorhanden, a.vorhanden.length)) === 'ersetzen') this.senden(sitzung, ziel, offen, a.vorhanden);
        return;
      }
      if (xhr.status !== 200) {
        toast((a.fehler || t('dateien.hochladen.fehlgeschlagen')) + (hoch.length ? ' (' + textHochgeladen(hoch) + ')' : ''), 'fehler');
        return;
      }
      toast(textHochgeladen(hoch.length ? hoch : dateien.map((f) => f.name)));
    };
    xhr.onerror = () => { fertig(); toast(t('dateien.hochladen.verbindungWeg'), 'fehler'); };
    xhr.onabort = () => { fertig(); toast(t('dateien.hochladen.abgebrochen')); };
    xhr.send(fd);
  }
}

// Meldet Namen, die beim Hochladen uebersprungen wurden (dort liegt ein Ordner oder eine
// Spezialdatei). da: Liste des Ordners, falls bekannt (fuer die genauere Meldung bei einem Namen).
function gesperrtMelden(namen, da) {
  if (namen.length > 1) { toast(t('dateien.gesperrt.mehrere', { namen: namen.join(', ') }), 'fehler'); return; }
  const e = (da || []).find((x) => x.name === namen[0]);
  const schluessel = !e ? 'dateien.gesperrt.eine' : e.ordner ? 'dateien.gesperrt.ordner' : 'dateien.gesperrt.spezial';
  toast(t(schluessel, { name: namen[0] }), 'fehler');
}

function textHochgeladen(namen) {
  return namen.length === 1 ? t('dateien.hochgeladen.eine', { name: namen[0] }) : t('dateien.hochgeladen.mehrere', { n: namen.length });
}

function shellZitat(s) {
  return /^[A-Za-z0-9_\/.,:+@%=-]+$/.test(s) ? s : "'" + s.replace(/'/g, "'\\''") + "'";
}
App.Dateien = Dateien;
