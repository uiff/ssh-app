package texte

// Texte der Datenhaltung (internal/store).
func init() {
	Eintragen(map[string]string{
		"store.nicht.gefunden":      "Eintrag nicht gefunden",
		"store.sicherung.fremd":     "Die Datei ist kein Keyway-Backup",
		"store.sicherung.neuer":     "Das Backup stammt aus einer neueren Version — zuerst Keyway aktualisieren",
		"store.sicherung.ohne.kopf": "Dem Backup fehlt der Tresor-Kopf",
	}, map[string]string{
		"store.nicht.gefunden":      "Entry not found",
		"store.sicherung.fremd":     "This file isn’t a Keyway backup",
		"store.sicherung.neuer":     "The backup is from a newer version — update Keyway first",
		"store.sicherung.ohne.kopf": "The backup is missing the vault header",
	})
}
