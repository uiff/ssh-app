'use strict';
// Texte Bereich «terminal» (Deutsch/Englisch). Schluessel mit Praefix «terminal.».
// Zustandskarte eines Terminal-Tabs (Verbinden, Abbruch, anderes Fenster) und Einfuegen.
// Meldungen des Servers (Status, Fehler, Ende) kommen schon uebersetzt ueber die WebSocket.
i18nErgaenzen({
  de: {
    'terminal.verbinde': 'Verbinde …',
    'terminal.verbindeNeu': 'Verbinde neu …',
    'terminal.abgemeldet': 'Abgemeldet',
    'terminal.fehlgeschlagen': 'Verbindung fehlgeschlagen',
    'terminal.unterbrochen': 'Verbindung unterbrochen — verbinde neu …',
    'terminal.abgeloest': 'In einem anderen Fenster geöffnet',
    'terminal.abgeloestHinweis': 'Die Sitzung läuft dort weiter. «Hierher holen» zeigt sie wieder in diesem Fenster.',
    'terminal.neuVerbinden': 'Neu verbinden',
    'terminal.schliessen': 'Schliessen',
    'terminal.hierherHolen': 'Hierher holen',
    'terminal.hierSchliessen': 'Hier schliessen',
    'terminal.hierSchliessenTitel': 'Die Sitzung läuft im anderen Fenster weiter',
    'terminal.jetztVerbinden': 'Jetzt verbinden',
    'terminal.gespeichert': 'Zugangsdaten gespeichert',
    'terminal.einfuegenVerboten': 'Einfügen nicht erlaubt — mit Strg+V geht es auch',
    // Fuer Bildschirmleser (xterm.js): Eingabefeld des Terminals, Ansage bei viel Ausgabe
    'terminal.eingabe': 'Terminal-Eingabe',
    'terminal.zuVielAusgabe': 'Zu viel Ausgabe zum Vorlesen — die Zeilen einzeln ansteuern',
  },
  en: {
    'terminal.verbinde': 'Connecting…',
    'terminal.verbindeNeu': 'Reconnecting…',
    'terminal.abgemeldet': 'Signed out',
    'terminal.fehlgeschlagen': 'Connection failed',
    'terminal.unterbrochen': 'Connection lost — reconnecting…',
    'terminal.abgeloest': 'Opened in another window',
    'terminal.abgeloestHinweis': 'The session keeps running there. “Move here” shows it in this window again.',
    'terminal.neuVerbinden': 'Reconnect',
    'terminal.schliessen': 'Close',
    'terminal.hierherHolen': 'Move here',
    'terminal.hierSchliessen': 'Close here',
    'terminal.hierSchliessenTitel': 'The session keeps running in the other window',
    'terminal.jetztVerbinden': 'Connect now',
    'terminal.gespeichert': 'Credentials saved',
    'terminal.einfuegenVerboten': 'Paste not allowed — Ctrl+V works too',
    'terminal.eingabe': 'Terminal input',
    'terminal.zuVielAusgabe': 'Too much output to announce, navigate to rows manually to read',
  },
});
