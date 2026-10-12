package texte

// Texte des HTTP-Servers und des Terminal-WebSockets (internal/server: server.go,
// api.go, ws.go). Die Bereiche «dateien» und «hilfe» tragen ihre Texte selbst ein.
func init() {
	Eintragen(map[string]string{
		// Anfragen und Schutz
		"server.json.ungueltig":           "Anfrage ist kein gültiges JSON",
		"server.csrf":                     "Anfrage ohne App-Kennung abgelehnt",
		"server.anmelden":                 "Bitte anmelden",
		"server.host.gesperrt":            "Zugriff über den Namen «%s» ist nicht freigegeben (Schutz vor DNS-Rebinding). Den Namen in SSHAPP_HOSTS eintragen.",
		"server.sperre.aktiv":             "Zu viele Fehlversuche — noch %s gesperrt",
		"server.sperre.gleichzeitig":      "Zu viele Anmeldeversuche gleichzeitig — bitte kurz warten",
		"server.argon.belegt":             "Gerade zu viele Anmeldeversuche — bitte kurz warten",
		"server.speichern.fehlgeschlagen": "Speichern fehlgeschlagen",
		"server.datenbank":                "Speichern oder Lesen in der Datenbank fehlgeschlagen — Details im Server-Log",

		// Bestand
		"server.ordner.name":                     "Ordnername: 1 bis 60 Zeichen",
		"server.host.geraet":                     "Unbekannter Gerätetyp «%s»",
		"server.host.adresse":                    "Adresse fehlt oder enthält Leerzeichen, / oder @",
		"server.host.port":                       "Port muss zwischen 1 und 65535 liegen",
		"server.host.unbekannt":                  "Host nicht gefunden",
		"server.zugang.name":                     "Name oder Benutzer angeben",
		"server.zugang.name.fehlt":               "Name angeben",
		"server.zugang.kein.privat":              "Kein privater Schlüssel hinterlegt",
		"server.zugang.kein.schluessel":          "Dieser Zugang hat keinen Schlüssel",
		"server.zugang.fehlt":                    "Der Zugang von «%s» fehlt — im Host-Dialog einen anderen wählen",
		"server.zugang.unlesbar":                 "Der Zugang von «%s» lässt sich nicht lesen",
		"server.zugang.entschluesseln":           "Die gespeicherten Zugangsdaten von «%s» lassen sich nicht entschlüsseln",
		"server.installieren.kein.benutzer":      "Für diesen Host ist kein Benutzer hinterlegt — im Host oder im Zugang eintragen",
		"server.installieren.fehlgeschlagen":     "Eintragen des Schlüssels fehlgeschlagen",
		"server.installieren.fehlgeschlagen.mit": "Eintragen des Schlüssels fehlgeschlagen: %s",
		"server.snippet.leer":                    "Name und Befehl angeben",

		// Backup
		"server.sicherung.hinweis":         "Verschlüsselt. Einspielen nur mit dem Sicherheitsschlüssel.",
		"server.sicherung.zu.gross":        "Datei fehlt oder ist zu gross",
		"server.sicherung.datei.fehlt":     "Datei fehlt",
		"server.sicherung.unlesbar":        "Die Datei liess sich nicht lesen",
		"server.sicherung.fremd":           "Die Datei ist kein Backup dieser App",
		"server.sicherung.beschaedigt":     "Der Inhalt des Backups ist beschädigt",
		"server.sicherung.tresor.falsch":   "Der Tresor im Backup passt nicht zum Sicherheitsschlüssel — nichts geändert",
		"server.sicherung.tresor.neuer":    "Der Tresor im Backup ist unvollständig oder stammt aus einer neueren Version — nichts geändert",
		"server.einspielen.eingerichtet":   "Die App ist schon eingerichtet — Backup nach der Anmeldung unter Einstellungen einspielen",
		"server.einspielen.fehlgeschlagen": "Einspielen fehlgeschlagen — nichts geändert",
		"server.einspielen.tresor":         "Tresor übernehmen fehlgeschlagen: %s",

		// Host-Schluessel
		"server.hostkey.unbekannt": "Host-Schlüssel von %s ist noch unbekannt — zuerst einmal im Terminal verbinden und bestätigen",
		"server.hostkey.geaendert": "Host-Schlüssel von %s hat sich geändert — Verbindung abgebrochen",
		"server.hostkey.abgelehnt": "Host-Schlüssel nicht bestätigt — Verbindung abgebrochen",
		"server.hostkey.speichern": "Host-Schlüssel konnte nicht gespeichert werden",

		// Terminal-WebSocket
		"server.sitzung.weg":              "Sitzung existiert nicht mehr",
		"server.ws.verbinde":              "Verbinde mit %s …",
		"server.ws.abgeloest":             "In einem anderen Fenster geöffnet",
		"server.ws.abgemeldet":            "Abgemeldet — bitte neu anmelden",
		"server.ws.getrennt":              "Browser getrennt",
		"server.ws.keine.antwort":         "Keine Antwort — abgebrochen",
		"server.ws.zeitueberschreitung":   "Zeitüberschreitung beim Verbindungsaufbau",
		"server.ws.anmeldung.abgebrochen": "Anmeldung abgebrochen — «Neu\u00a0verbinden», um es erneut zu versuchen.",
		"server.ws.kein.benutzer":         "Kein Benutzername angegeben",
	}, map[string]string{
		// Requests and protection
		"server.json.ungueltig":           "The request is not valid JSON",
		"server.csrf":                     "Request rejected: app header missing",
		"server.anmelden":                 "Please sign in",
		"server.host.gesperrt":            "Access via the name “%s” is not allowed (DNS rebinding protection). Add the name to SSHAPP_HOSTS.",
		"server.sperre.aktiv":             "Too many failed attempts — locked for another %s",
		"server.sperre.gleichzeitig":      "Too many sign-in attempts at once — please wait a moment",
		"server.argon.belegt":             "Too many sign-in attempts right now — please wait a moment",
		"server.speichern.fehlgeschlagen": "Saving failed",
		"server.datenbank":                "Reading or writing the database failed — see the server log for details",

		// Inventory
		"server.ordner.name":                     "Folder name: 1 to 60 characters",
		"server.host.geraet":                     "Unknown device type “%s”",
		"server.host.adresse":                    "Address is missing or contains spaces, / or @",
		"server.host.port":                       "Port must be between 1 and 65535",
		"server.host.unbekannt":                  "Host not found",
		"server.zugang.name":                     "Enter a name or a user",
		"server.zugang.name.fehlt":               "Enter a name",
		"server.zugang.kein.privat":              "No private key stored",
		"server.zugang.kein.schluessel":          "This credential has no key",
		"server.zugang.fehlt":                    "The credential of “%s” is missing — choose another one in the host dialog",
		"server.zugang.unlesbar":                 "The credential of “%s” cannot be read",
		"server.zugang.entschluesseln":           "The stored credentials of “%s” cannot be decrypted",
		"server.installieren.kein.benutzer":      "No user is set for this host — add one to the host or the credential",
		"server.installieren.fehlgeschlagen":     "Adding the key failed",
		"server.installieren.fehlgeschlagen.mit": "Adding the key failed: %s",
		"server.snippet.leer":                    "Enter a name and a command",

		// Backup
		"server.sicherung.hinweis":         "Encrypted. Can only be restored with the recovery key.",
		"server.sicherung.zu.gross":        "File is missing or too large",
		"server.sicherung.datei.fehlt":     "File is missing",
		"server.sicherung.unlesbar":        "The file could not be read",
		"server.sicherung.fremd":           "This file is not a backup from this app",
		"server.sicherung.beschaedigt":     "The backup content is damaged",
		"server.sicherung.tresor.falsch":   "The vault in the backup does not match the recovery key — nothing was changed",
		"server.sicherung.tresor.neuer":    "The vault in the backup is incomplete or from a newer version — nothing was changed",
		"server.einspielen.eingerichtet":   "The app is already set up — sign in and restore the backup under Settings",
		"server.einspielen.fehlgeschlagen": "Restoring the backup failed — nothing was changed",
		"server.einspielen.tresor":         "Taking over the vault failed: %s",

		// Host keys
		"server.hostkey.unbekannt": "The host key of %s is not known yet — connect once in the terminal and confirm it first",
		"server.hostkey.geaendert": "The host key of %s has changed — connection aborted",
		"server.hostkey.abgelehnt": "Host key not confirmed — connection aborted",
		"server.hostkey.speichern": "The host key could not be saved",

		// Terminal WebSocket
		"server.sitzung.weg":              "The session no longer exists",
		"server.ws.verbinde":              "Connecting to %s …",
		"server.ws.abgeloest":             "Opened in another window",
		"server.ws.abgemeldet":            "Signed out — please sign in again",
		"server.ws.getrennt":              "Browser disconnected",
		"server.ws.keine.antwort":         "No response — canceled",
		"server.ws.zeitueberschreitung":   "Connection timed out",
		"server.ws.anmeldung.abgebrochen": "Sign-in canceled — choose “Reconnect” to try again.",
		"server.ws.kein.benutzer":         "No user name entered",
	})
}
