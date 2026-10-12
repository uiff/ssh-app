package texte

// Texte der Datenhaltung (internal/store).
func init() {
	Eintragen(map[string]string{
		"store.nicht.gefunden":      "Eintrag nicht gefunden",
		"store.sicherung.fremd":     "Die Datei ist kein Backup dieser App",
		"store.sicherung.neuer":     "Das Backup stammt aus einer neueren Version — erst die App aktualisieren",
		"store.sicherung.ohne.kopf": "Dem Backup fehlt der Tresor-Kopf",
	}, map[string]string{
		"store.nicht.gefunden":      "Entry not found",
		"store.sicherung.fremd":     "This file is not a backup from this app",
		"store.sicherung.neuer":     "The backup is from a newer version — update the app first",
		"store.sicherung.ohne.kopf": "The backup is missing the vault header",
	})
}
