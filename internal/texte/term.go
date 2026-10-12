package texte

// Texte der Terminal-Sitzungen (internal/term).
func init() {
	Eintragen(map[string]string{
		"term.abgeloest":           "Die Sitzung ist in einem anderen Fenster geöffnet",
		"term.kein.sftp":           "Der Server bietet kein SFTP an",
		"term.beendet":             "Die Sitzung ist beendet",
		"term.kanal.voll":          "Das Gerät öffnet keine weitere Sitzung (zu viele offene Kanäle?)",
		"term.kanal.fehler":        "Das Terminal lässt sich nicht öffnen",
		"term.kein.pty":            "Das Gerät stellt kein Terminal (PTY) bereit",
		"term.kein.startbefehl":    "Das Gerät führt keinen Startbefehl aus",
		"term.keine.shell":         "Das Gerät startet keine Shell",
		"term.ende.geschlossen":    "Sitzung geschlossen",
		"term.ende.herunterfahren": "Der Server fährt herunter",
		"term.ende.ohne.browser":   "Zu lange ohne Browser — Sitzung beendet",
		"term.ende.verbindung":     "Verbindung beendet",
		"term.ende.abgemeldet":     "Abgemeldet",
		"term.ende.shell":          "Shell beendet (Code %d)",
	}, map[string]string{
		"term.abgeloest":           "The session is open in another window",
		"term.kein.sftp":           "The server does not offer SFTP",
		"term.beendet":             "The session has ended",
		"term.kanal.voll":          "The device won't open another session (too many open channels?)",
		"term.kanal.fehler":        "The terminal could not be opened",
		"term.kein.pty":            "The device does not provide a terminal (PTY)",
		"term.kein.startbefehl":    "The device does not run the start command",
		"term.keine.shell":         "The device does not start a shell",
		"term.ende.geschlossen":    "Session closed",
		"term.ende.herunterfahren": "The server is shutting down",
		"term.ende.ohne.browser":   "Too long without a browser — session ended",
		"term.ende.verbindung":     "Connection closed",
		"term.ende.abgemeldet":     "Signed out",
		"term.ende.shell":          "Shell exited (code %d)",
	})
}
