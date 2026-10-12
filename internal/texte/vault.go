package texte

// Texte des Tresors (internal/vault).
func init() {
	Eintragen(map[string]string{
		"vault.gesperrt":            "Der Tresor ist gesperrt — bitte anmelden",
		"vault.anmeldung.falsch":    "Benutzername oder Passwort ist falsch",
		"vault.schluessel.falsch":   "Der Sicherheitsschlüssel passt nicht",
		"vault.schluessel.format":   "Das ist kein gültiger Sicherheitsschlüssel",
		"vault.schon.eingerichtet":  "Die App ist schon eingerichtet",
		"vault.nicht.eingerichtet":  "Die App ist noch nicht eingerichtet",
		"vault.passwort.kurz":       "Das Passwort braucht mindestens %d Zeichen",
		"vault.benutzer.ungueltig":  "Benutzername: 1 bis 40 Zeichen, keine Leerzeichen",
		"vault.passwort.alt.falsch": "Das bisherige Passwort ist falsch",
		"vault.kopf.unlesbar":       "Der Tresor-Kopf lässt sich nicht lesen",
		"vault.kopf.unvollstaendig": "Der Tresor-Kopf ist unvollständig",
		"vault.geheimnis.kaputt":    "Die verschlüsselten Daten sind beschädigt",
	}, map[string]string{
		"vault.gesperrt":            "The vault is locked — please sign in",
		"vault.anmeldung.falsch":    "Incorrect user name or password",
		"vault.schluessel.falsch":   "The recovery key does not match",
		"vault.schluessel.format":   "This is not a valid recovery key",
		"vault.schon.eingerichtet":  "The app is already set up",
		"vault.nicht.eingerichtet":  "The app has not been set up yet",
		"vault.passwort.kurz":       "The password needs at least %d characters",
		"vault.benutzer.ungueltig":  "User name: 1 to 40 characters, no spaces",
		"vault.passwort.alt.falsch": "The current password is incorrect",
		"vault.kopf.unlesbar":       "The vault header cannot be read",
		"vault.kopf.unvollstaendig": "The vault header is incomplete",
		"vault.geheimnis.kaputt":    "The encrypted data is damaged",
	})
}
