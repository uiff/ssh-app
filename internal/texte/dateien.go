package texte

// Texte der Dateileiste (internal/server/dateien.go: SFTP-Liste, Laden, Hochladen, Aktionen).
func init() {
	Eintragen(map[string]string{
		"dateien.zitat":           "«%s»",
		"dateien.sitzungFehlt":    "Diese Sitzung gibt es nicht mehr",
		"dateien.sitzungBeendet":  "Kein Dateizugriff: Die Sitzung ist beendet — bitte neu verbinden",
		"dateien.pfadAbsolut":     "Der Pfad muss absolut sein (mit «/» am Anfang)",
		"dateien.pfadUngueltig":   "Ungültiger Pfad",
		"dateien.nameUngueltig":   "Ungültiger neuer Name",
		"dateien.keinOrdner":      "«%s» ist kein Ordner",
		"dateien.vorhanden":       "«%s» gibt es dort schon",
		"dateien.aktionUnbekannt": "Unbekannte Aktion",

		"dateien.sftp.fehlt":             "Datei oder Ordner gibt es dort nicht (mehr)",
		"dateien.sftp.keinRecht":         "Keine Berechtigung — der angemeldete Benutzer darf das dort nicht",
		"dateien.sftp.vorhanden":         "Gibt es dort schon",
		"dateien.sftp.verbindung":        "Verbindung zum Host unterbrochen — bitte neu verbinden",
		"dateien.sftp.nichtUnterstuetzt": "Das unterstützt der SFTP-Dienst dieses Hosts nicht",
		"dateien.sftp.nichtVerstanden":   "Der SFTP-Dienst des Hosts hat die Anfrage nicht verstanden",
		"dateien.sftp.abgelehnt":         "Der Host hat das abgelehnt, ohne einen Grund zu nennen (Datenträger voll, Datei in Benutzung oder schon vorhanden?)",
		"dateien.sftp.fehler":            "Dateizugriff auf dem Host fehlgeschlagen",

		"dateien.laden.ordner":     "Ordner lassen sich nicht herunterladen",
		"dateien.laden.keineDatei": "Nur gewöhnliche Dateien lassen sich herunterladen (das ist ein Gerät, eine Pipe oder ein Socket)",

		"dateien.hochladen.multipart":        "Erwarte multipart/form-data",
		"dateien.hochladen.abgebrochen":      "Übertragung abgebrochen",
		"dateien.hochladen.nichtGespeichert": "Übertragung abgebrochen — «%s» wurde nicht gespeichert",
		"dateien.hochladen.fehlerBei":        "«%s»: %s",
		"dateien.hochladen.vorhandenMehrere": "Schon vorhanden: %s",
		"dateien.hochladen.istOrdner":        "«%s» ist dort ein Ordner und lässt sich nicht durch eine Datei ersetzen",
		"dateien.hochladen.keineDatei":       "«%s» ist dort keine gewöhnliche Datei (z.B. ein Gerät, eine Pipe oder ein Socket) und lässt sich nicht ersetzen",
		"dateien.hochladen.gesperrtMehrere":  "Lassen sich nicht ersetzen (Ordner oder keine gewöhnliche Datei): %s",
		"dateien.hochladen.keinSchreibrecht": "«%s» darf der angemeldete Benutzer nicht überschreiben (keine Schreibrechte)",
		"dateien.hochladen.abbruchDirekt":    "Übertragung abgebrochen — «%s» wurde direkt beschrieben (im Ordner fehlt das Schreibrecht für eine Zwischendatei) und ist jetzt unvollständig",
		"dateien.hochladen.einsetzenFehler":  "«%s» liess sich nicht vollständig schreiben. Die hochgeladene Datei liegt vollständig als «%s» daneben.",

		"dateien.umbenennen.inSichSelbst":      "Ein Ordner lässt sich nicht in sich selbst verschieben",
		"dateien.umbenennen.istOrdner":         "«%s» ist ein Ordner und lässt sich nicht ersetzen",
		"dateien.umbenennen.originalGesichert": "«%s» liess sich nicht ersetzen. Das bisherige Original liegt jetzt als «%s» daneben.",
	}, map[string]string{
		"dateien.zitat":           "“%s”",
		"dateien.sitzungFehlt":    "This session no longer exists",
		"dateien.sitzungBeendet":  "No file access: the session has ended. Reconnect and try again.",
		"dateien.pfadAbsolut":     "The path must be absolute (start with “/”)",
		"dateien.pfadUngueltig":   "Invalid path",
		"dateien.nameUngueltig":   "Invalid new name",
		"dateien.keinOrdner":      "“%s” isn’t a folder",
		"dateien.vorhanden":       "“%s” already exists there",
		"dateien.aktionUnbekannt": "Unknown action",

		"dateien.sftp.fehlt":             "That file or folder doesn’t exist (anymore)",
		"dateien.sftp.keinRecht":         "Permission denied: the user you’re connected as isn’t allowed to do that there",
		"dateien.sftp.vorhanden":         "Already exists",
		"dateien.sftp.verbindung":        "Lost the connection to the host. Reconnect and try again.",
		"dateien.sftp.nichtUnterstuetzt": "The host’s SFTP server doesn’t support this",
		"dateien.sftp.nichtVerstanden":   "The host’s SFTP server didn’t understand the request",
		"dateien.sftp.abgelehnt":         "The host refused without giving a reason (disk full, file in use, or already exists?)",
		"dateien.sftp.fehler":            "File access on the host failed",

		"dateien.laden.ordner":     "Folders can’t be downloaded",
		"dateien.laden.keineDatei": "Only regular files can be downloaded (this is a device, pipe, or socket)",

		"dateien.hochladen.multipart":        "Expected multipart/form-data",
		"dateien.hochladen.abgebrochen":      "Transfer interrupted",
		"dateien.hochladen.nichtGespeichert": "Transfer interrupted — “%s” wasn’t saved",
		"dateien.hochladen.fehlerBei":        "“%s”: %s",
		"dateien.hochladen.vorhandenMehrere": "Already exist: %s",
		"dateien.hochladen.istOrdner":        "“%s” is a folder there and can’t be replaced by a file",
		"dateien.hochladen.keineDatei":       "“%s” isn’t a regular file there (for example a device, pipe, or socket) and can’t be replaced",
		"dateien.hochladen.gesperrtMehrere":  "Can’t be replaced (folder or not a regular file): %s",
		"dateien.hochladen.keinSchreibrecht": "The user you’re connected as can’t overwrite “%s” (no write permission)",
		"dateien.hochladen.abbruchDirekt":    "Transfer interrupted — “%s” was written in place (no permission for a temporary file in this folder) and is now incomplete",
		"dateien.hochladen.einsetzenFehler":  "Couldn’t finish writing “%s”. The complete upload is next to it as “%s”.",

		"dateien.umbenennen.inSichSelbst":      "A folder can’t be moved into itself",
		"dateien.umbenennen.istOrdner":         "“%s” is a folder and can’t be replaced",
		"dateien.umbenennen.originalGesichert": "Couldn’t replace “%s”. The original is now next to it as “%s”.",
	})
}
