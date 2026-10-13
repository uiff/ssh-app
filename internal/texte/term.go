package texte

// Texte der Terminal-Sitzungen (internal/term).
func init() {
	Eintragen(map[string]string{
		"term.abgeloest":           "Die Sitzung ist in einem anderen Fenster geöffnet",
		"term.kein.sftp":           "Der Host bietet kein SFTP an",
		"term.beendet":             "Die Sitzung ist beendet",
		"term.kanal.voll":          "Das Gerät öffnet keine weitere Sitzung (zu viele offene Kanäle?)",
		"term.kanal.fehler":        "Das Terminal lässt sich nicht öffnen",
		"term.kein.pty":            "Das Gerät stellt kein Terminal (PTY) bereit",
		"term.kein.startbefehl":    "Das Gerät führt keinen Startbefehl aus",
		"term.keine.shell":         "Das Gerät startet keine Shell",
		"term.ende.geschlossen":    "Sitzung geschlossen",
		"term.ende.herunterfahren": "Keyway wird heruntergefahren",
		"term.ende.ohne.browser":   "Zu lange ohne Browser — Sitzung beendet",
		"term.ende.verbindung":     "Verbindung beendet",
		"term.ende.abgemeldet":     "Sitzung beendet (am Host abgemeldet)",
		"term.ende.shell":          "Shell beendet (Code %d)",
	}, map[string]string{
		"term.abgeloest":           "The session is open in another window",
		"term.kein.sftp":           "The host doesn’t offer SFTP",
		"term.beendet":             "The session has ended",
		"term.kanal.voll":          "The device won’t open another session (too many open channels?)",
		"term.kanal.fehler":        "Couldn’t open the terminal",
		"term.kein.pty":            "The device doesn’t provide a terminal (PTY)",
		"term.kein.startbefehl":    "The device won’t run the startup command",
		"term.keine.shell":         "The device won’t start a shell",
		"term.ende.geschlossen":    "Session closed",
		"term.ende.herunterfahren": "Keyway is shutting down",
		"term.ende.ohne.browser":   "No browser connected for too long — session ended",
		"term.ende.verbindung":     "Connection closed",
		"term.ende.abgemeldet":     "Session ended (shell exited)",
		"term.ende.shell":          "Shell exited (code %d)",
	})
}
