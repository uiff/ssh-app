package texte

// Texte der Zwei-Faktor-Anmeldung: Tresor (internal/vault, «vault.*») und HTTP-Server
// (internal/server/zweitfaktor.go, «zweitfaktor.*»).
func init() {
	Eintragen(map[string]string{
		// Tresor
		"vault.passwort.falsch":   "Das Passwort ist falsch",
		"vault.zweitfaktor.fehlt": "Bitte den 6-stelligen Code aus der Authenticator-App eingeben",
		"vault.zweitfaktor.aktiv": "Die Zwei-Faktor-Anmeldung ist schon eingeschaltet",
		"vault.zweitfaktor.aus":   "Die Zwei-Faktor-Anmeldung ist nicht eingeschaltet",
		"vault.zweitfaktor.kaputt": "Die Daten der Zwei-Faktor-Anmeldung sind beschädigt — " +
			"mit dem Sicherheitsschlüssel zurücksetzen",
		"vault.kopf.geaendert": "Die Anmeldedaten wurden eben geändert — bitte nochmals versuchen",

		// Server
		"zweitfaktor.code.falsch":    "Der Code stimmt nicht",
		"zweitfaktor.code.verwendet": "Dieser Code wurde schon verwendet — bitte auf den nächsten Code warten",
		"zweitfaktor.code.zeit.spaeter": "Der Code gehört zu einer Uhrzeit, die etwa %d min später ist als die Uhr dieses Servers. " +
			"Bitte die Uhrzeit von Server und Telefon prüfen oder einen Ersatzcode verwenden.",
		"zweitfaktor.code.zeit.frueher": "Der Code gehört zu einer Uhrzeit, die etwa %d min früher ist als die Uhr dieses Servers. " +
			"Bitte die Uhrzeit von Server und Telefon prüfen oder einen Ersatzcode verwenden.",
		"zweitfaktor.code.gesperrt": "Zu viele falsche Codes — Codes aus der App gelten noch %s nicht. " +
			"Ein Ersatzcode funktioniert weiterhin.",
		// Beim Einrichten gibt es noch keine Ersatzcodes.
		"zweitfaktor.einrichten.zeit.spaeter": "Der Code gehört zu einer Uhrzeit, die etwa %d min später ist als die Uhr dieses Servers. " +
			"Bitte die Uhrzeit von Server und Telefon prüfen.",
		"zweitfaktor.einrichten.zeit.frueher": "Der Code gehört zu einer Uhrzeit, die etwa %d min früher ist als die Uhr dieses Servers. " +
			"Bitte die Uhrzeit von Server und Telefon prüfen.",
		"zweitfaktor.passwort.fehlt": "Bitte das aktuelle Passwort eingeben",
		"zweitfaktor.einrichten.abgelaufen": "Die Einrichtung ist abgelaufen — bitte neu beginnen. " +
			"Einen schon angelegten Eintrag «Keyway» in der Authenticator-App bitte löschen, er gilt nicht mehr.",
		"zweitfaktor.qr": "Der QR-Code liess sich nicht erzeugen",
	}, map[string]string{
		// Vault
		"vault.passwort.falsch":   "The password is incorrect",
		"vault.zweitfaktor.fehlt": "Enter the 6-digit code from your authenticator app",
		"vault.zweitfaktor.aktiv": "Two-factor sign-in is already on",
		"vault.zweitfaktor.aus":   "Two-factor sign-in is off",
		"vault.zweitfaktor.kaputt": "The two-factor sign-in data is damaged — " +
			"reset with the recovery key",
		"vault.kopf.geaendert": "The sign-in details were just changed — please try again",

		// Server
		"zweitfaktor.code.falsch":    "The code is incorrect",
		"zweitfaktor.code.verwendet": "This code has already been used — please wait for the next one",
		"zweitfaktor.code.zeit.spaeter": "The code belongs to a time about %d min later than this server’s clock. " +
			"Check the time on the server and on your phone, or use a backup code.",
		"zweitfaktor.code.zeit.frueher": "The code belongs to a time about %d min earlier than this server’s clock. " +
			"Check the time on the server and on your phone, or use a backup code.",
		"zweitfaktor.code.gesperrt": "Too many wrong codes — codes from the app won’t be accepted for another %s. " +
			"A backup code still works.",
		"zweitfaktor.einrichten.zeit.spaeter": "The code belongs to a time about %d min later than this server’s clock. " +
			"Check the time on the server and on your phone.",
		"zweitfaktor.einrichten.zeit.frueher": "The code belongs to a time about %d min earlier than this server’s clock. " +
			"Check the time on the server and on your phone.",
		"zweitfaktor.passwort.fehlt": "Enter your current password",
		"zweitfaktor.einrichten.abgelaufen": "Setup has expired — please start again. " +
			"If you already added a “Keyway” entry to your authenticator app, delete it; it no longer works.",
		"zweitfaktor.qr": "Couldn’t create the QR code",
	})
}
