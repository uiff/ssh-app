'use strict';
// Bildschirmtastatur auf dem Telefon.
// iOS verkleinert beim Tippen nur den sichtbaren Bereich (visualViewport), nicht die Seite:
// Blaetter und Dialoge mit «bottom: 0» laegen hinter der Tastatur. Hier wird die Ueberdeckung
// gemessen (--tastatur) und das Eingabefeld mit dem Fokus in den sichtbaren Bereich geholt.

(function () {
  const vv = window.visualViewport;
  if (!vv) return;
  const wurzel = document.documentElement;
  let letzte = -1;

  function istEingabe(e) {
    return e && /^(INPUT|TEXTAREA|SELECT)$/.test(e.tagName) && !e.closest('.xterm');
  }

  // Haelt das fokussierte Feld sichtbar (nicht unter der Tastatur, nicht unter dem oberen Rand).
  function sichtbarHalten() {
    const e = document.activeElement;
    if (!istEingabe(e)) return;
    const r = e.getBoundingClientRect();
    const oben = vv.offsetTop + 8;
    const unten = vv.offsetTop + vv.height - 12;
    // Passt der Absende-Knopf des Formulars zusammen mit dem Feld ins Bild, beide zeigen.
    const form = e.closest('form');
    const knopf = form && [...form.querySelectorAll('button[type=submit], .btn.primaer')].filter((b) => b.offsetParent).pop();
    if (knopf) {
      const k = knopf.getBoundingClientRect();
      if (k.bottom > r.top && k.bottom - r.top <= unten - oben) {
        if (k.bottom > unten || r.top < oben) knopf.scrollIntoView({ block: 'end', behavior: 'smooth' });
        return;
      }
    }
    if (r.bottom > unten || r.top < oben) e.scrollIntoView({ block: 'center', behavior: 'smooth' });
  }

  function messen() {
    const ueber = Math.max(0, Math.round(window.innerHeight - vv.height - vv.offsetTop));
    if (ueber !== letzte) {
      letzte = ueber;
      wurzel.style.setProperty('--tastatur', ueber + 'px');
      // Unter 80 px ist es keine Tastatur, sondern z.B. die einklappende Adressleiste.
      wurzel.classList.toggle('tastatur-offen', ueber > 80);
    }
    wurzel.style.setProperty('--vv-oben', Math.round(vv.offsetTop) + 'px');
    // Nach dem Verschieben der Blaetter das Feld nachziehen.
    requestAnimationFrame(sichtbarHalten);
  }

  vv.addEventListener('resize', messen);
  vv.addEventListener('scroll', messen);
  // iOS oeffnet die Tastatur animiert; erst danach stimmt die Hoehe.
  document.addEventListener('focusin', (ev) => { if (istEingabe(ev.target)) setTimeout(messen, 320); });
  messen();
})();
