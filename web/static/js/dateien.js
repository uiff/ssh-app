'use strict';
// Dateileiste (SFTP) — folgt auf Wunsch dem Arbeitsverzeichnis des Terminals.

class Dateien {
  constructor() {
    this.pfad = '';
    this.folgen = lokal('dateienFolgen', true);
    this.tab = null;
    this.eintraege = [];
    this.pfadFeld = el('input', { type: 'text', 'aria-label': 'Pfad', autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false',
      onkeydown: (e) => { if (e.key === 'Enter') { this.folgenSetzen(false); this.laden(this.pfadFeld.value.trim()); } } });
    this.liste = el('div', { class: 'seite-inhalt', role: 'list' });
    this.folgenChip = el('button', { class: 'chip', type: 'button', onclick: () => this.folgenSetzen(!this.folgen) });
    this.hochInput = el('input', { type: 'file', multiple: true, class: 'versteckt', onchange: () => { this.hochladen([...this.hochInput.files]); this.hochInput.value = ''; } });
    this.fortschritt = el('div', { class: 'fortschritt versteckt' });
    this.ablage = el('div', { class: 'ablage nur-desktop' }, 'Dateien hierher ziehen zum Hochladen');
    this.element = el('aside', { class: 'seite dateien', 'aria-label': 'Dateien' },
      el('div', { class: 'blatt-griff', 'aria-hidden': 'true' }),
      el('div', { class: 'seite-kopf' },
        el('span', { class: 'titel' }, 'Dateien'), this.folgenChip,
        ikonKnopf('hoch', 'Hochladen', () => this.hochInput.click()),
        ikonKnopf('ordnerPlus', 'Neuer Ordner', () => this.neuerOrdner()),
        ikonKnopf('x', 'Dateien schliessen', () => App.seiteUmschalten('dateien', false))),
      el('div', { class: 'pfadleiste' },
        ikonKnopf('aufwaerts', 'Eine Ebene höher', () => this.hoeher()),
        this.pfadFeld,
        ikonKnopf('neu', 'Neu laden', () => this.laden(this.pfad))),
      this.liste, this.hochInput, this.ablage, this.fortschritt);
    this.folgenZeichnen();
    // Ziehen und Ablegen
    for (const z of [this.element]) {
      z.addEventListener('dragover', (e) => { e.preventDefault(); this.ablage.classList.add('ziel'); });
      z.addEventListener('dragleave', () => this.ablage.classList.remove('ziel'));
      z.addEventListener('drop', (e) => { e.preventDefault(); this.ablage.classList.remove('ziel'); this.hochladen([...e.dataTransfer.files]); });
    }
  }

  folgenSetzen(an) {
    this.folgen = an;
    lokalSetzen('dateienFolgen', an);
    this.folgenZeichnen();
    if (an) this.pfadPruefen(true);
  }
  folgenZeichnen() {
    this.folgenChip.className = 'chip' + (this.folgen ? ' gut' : '');
    this.folgenChip.textContent = this.folgen ? 'folgt Terminal' : 'fest';
    this.folgenChip.title = this.folgen ? 'Wechselt mit cd im Terminal mit. Antippen zum Festhalten.' : 'Bleibt im gewählten Ordner. Antippen, um dem Terminal zu folgen.';
  }

  sichtbar() { return App.seiteOffen('dateien'); }

  // Aufruf bei Tab-Wechsel
  fuerTab(tab) {
    if (this.tab !== tab) { this.tab = tab; this.pfad = ''; this.eintraege = []; }
    if (!this.sichtbar()) return;
    if (!tab || tab.zustand !== 'bereit') {
      ersetzen(this.liste, el('div', { class: 'leer' }, tab ? 'Wartet auf die Verbindung …' : 'Keine Sitzung offen. Erst mit einem Host verbinden.'));
      return;
    }
    if (!this.pfad) this.pfadPruefen(true); else if (!this.eintraege.length) this.laden(this.pfad);
  }

  async pfadPruefen(erzwingen) {
    const tab = this.tab;
    if (!tab || tab.zustand !== 'bereit' || !this.sichtbar()) return;
    if (!this.folgen) { if (erzwingen || !this.pfad) this.laden(this.pfad); return; }
    try {
      const r = await api('GET', '/api/sitzungen/' + tab.sitzungId + '/pfad');
      if (tab !== this.tab) return;
      if (r.bekannt && r.pfad && r.pfad !== this.pfad) this.laden(r.pfad);
      else if (erzwingen) this.laden(this.pfad);
    } catch (e) { /* still: naechster Versuch */ }
  }

  async laden(pfad) {
    const tab = this.tab;
    if (!tab || tab.zustand !== 'bereit') return;
    try {
      const r = await api('GET', '/api/sitzungen/' + tab.sitzungId + '/dateien?pfad=' + encodeURIComponent(pfad || ''));
      if (tab !== this.tab) return;
      this.pfad = r.pfad;
      this.eintraege = r.eintraege;
      if (document.activeElement !== this.pfadFeld) this.pfadFeld.value = r.pfad;
      this.zeichnen();
    } catch (e) {
      ersetzen(this.liste, el('div', { class: 'leer' }, e.message));
      this.pfadFeld.value = pfad || '';
    }
  }

  voll(name) { return (this.pfad === '/' ? '' : this.pfad) + '/' + name; }

  hoeher() {
    if (!this.pfad || this.pfad === '/') return;
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
    ersetzen(this.liste, ...(zeilen.length ? zeilen : [el('div', { class: 'leer' }, 'Ordner ist leer')]));
  }

  dateiMenue(e, anker) {
    const p = this.voll(e.name);
    menue(anker, [
      e.ordner ? { text: 'Öffnen', icon: 'ordner', aktion: () => { this.folgenSetzen(false); this.laden(p); } } : null,
      !e.ordner ? { text: 'Herunterladen', icon: 'runter', aktion: () => herunterladen('/api/sitzungen/' + this.tab.sitzungId + '/laden?pfad=' + encodeURIComponent(p)) } : null,
      { text: 'Pfad ins Terminal einfügen', icon: 'einfuegen', aktion: () => { App.inTerminal(shellZitat(p) + ' '); } },
      e.ordner ? { text: 'Im Terminal hierhin wechseln', icon: 'terminal', aktion: () => { App.inTerminal('cd ' + shellZitat(p) + '\r', true); } } : null,
      { text: 'Pfad kopieren', icon: 'kopieren', aktion: () => kopieren(p) },
      { text: 'Umbenennen', icon: 'stift', aktion: () => this.umbenennen(e) },
      { text: 'Löschen', icon: 'muell', gefahr: true, aktion: () => this.loeschen(e) },
    ]);
  }

  async aktion(art, pfad, nach) {
    await api('POST', '/api/sitzungen/' + this.tab.sitzungId + '/aktion', { art, pfad, nach });
    this.laden(this.pfad);
  }
  async neuerOrdner() {
    if (!this.tab || !this.pfad) return;
    const n = await eingabe('Name des Ordners', { titel: 'Neuer Ordner', ok: 'Anlegen' });
    if (!n) return;
    this.aktion('ordner', this.voll(n.trim())).catch(fehlerToast);
  }
  async umbenennen(e) {
    const n = await eingabe('Neuer Name', { titel: 'Umbenennen', wert: e.name, ok: 'Umbenennen' });
    if (!n || n === e.name) return;
    this.aktion('umbenennen', this.voll(e.name), this.voll(n.trim())).catch(fehlerToast);
  }
  async loeschen(e) {
    const ok = await frage(e.ordner ? `Den Ordner «${e.name}» mit allem Inhalt löschen?` : `«${e.name}» löschen?`, { titel: 'Löschen', ja: 'Löschen', gefahr: true });
    if (ok) this.aktion('loeschen', this.voll(e.name)).catch(fehlerToast);
  }

  hochladen(dateien) {
    if (!dateien.length) return;
    if (!this.tab || !this.pfad) { toast('Zuerst eine Sitzung öffnen', 'fehler'); return; }
    const sitzung = this.tab.sitzungId, ziel = this.pfad;
    const senden = (ueberschreiben) => {
      const fd = new FormData();
      dateien.forEach((f) => fd.append('datei', f, f.name));
      const xhr = new XMLHttpRequest();
      xhr.open('POST', '/api/sitzungen/' + sitzung + '/hochladen?pfad=' + encodeURIComponent(ziel) + (ueberschreiben ? '&ueberschreiben=1' : ''));
      xhr.setRequestHeader('X-SSH-App', '1');
      const balken = el('div');
      const name = dateien.length === 1 ? dateien[0].name : dateien.length + ' Dateien';
      const prozent = el('span', null, '0 %');
      ersetzen(this.fortschritt, el('div', { class: 'reihe', style: { justifyContent: 'space-between' } }, el('span', null, name), prozent), el('div', { class: 'balken' }, balken));
      this.fortschritt.classList.remove('versteckt');
      xhr.upload.onprogress = (e) => {
        if (!e.lengthComputable) return;
        const p = Math.round((e.loaded / e.total) * 100);
        balken.style.width = p + '%'; prozent.textContent = p + ' %';
      };
      xhr.onload = async () => {
        this.fortschritt.classList.add('versteckt');
        if (xhr.status === 409 && !ueberschreiben) {
          let text = 'Datei gibt es schon';
          try { text = JSON.parse(xhr.responseText).fehler; } catch (e) { /* */ }
          if (await frage(text + '. Überschreiben?', { titel: 'Hochladen', ja: 'Überschreiben', gefahr: true })) senden(true);
          return;
        }
        if (xhr.status !== 200) {
          let text = 'Hochladen fehlgeschlagen';
          try { text = JSON.parse(xhr.responseText).fehler; } catch (e) { /* */ }
          toast(text, 'fehler');
          return;
        }
        toast(name + ' hochgeladen');
        if (this.pfad === ziel) this.laden(ziel);
      };
      xhr.onerror = () => { this.fortschritt.classList.add('versteckt'); toast('Hochladen abgebrochen', 'fehler'); };
      xhr.send(fd);
    };
    senden(false);
  }
}

function shellZitat(s) {
  return /^[A-Za-z0-9_\/.,:+@%=-]+$/.test(s) ? s : "'" + s.replace(/'/g, "'\\''") + "'";
}
App.Dateien = Dateien;
