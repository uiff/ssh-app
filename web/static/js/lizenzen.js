'use strict';
// Lizenzseite (vendor/licenses/): Tab-Titel in der Sprache der Seite. Der Text kommt aus der
// sichtbaren Ueberschrift (lang=de bzw. lang=en), damit er nur an einer Stelle steht.
(function () {
  const h = document.querySelector('h1 [lang="' + I18N.sprache + '"]');
  if (h) document.title = h.textContent + ' · SSH-App';
})();
