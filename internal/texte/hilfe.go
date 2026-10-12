package texte

// Texte der Befehlshilfe (internal/help, internal/server/hilfe.go).
func init() {
	Eintragen(map[string]string{
		"hilfe.keine_seite":        "Keine Hilfe zu «%s»",
		"hilfe.schalter.unbekannt": "nicht in den Beispielen — man %s",
		"hilfe.schalter.auch":      "%s — Beispiel: %s",
	}, map[string]string{
		"hilfe.keine_seite":        "No help for “%s”",
		"hilfe.schalter.unbekannt": "not in the examples — see man %s",
		"hilfe.schalter.auch":      "%s — example: %s",
	})
}
