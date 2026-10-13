package texte

// Texte der Befehlshilfe (internal/help, internal/server/hilfe.go).
func init() {
	Eintragen(map[string]string{
		"hilfe.keine_seite":        "Keine Hilfe zu «%s»",
		"hilfe.schalter.unbekannt": "nicht in den Beispielen — siehe man %s",
		"hilfe.schalter.auch":      "%s — Beispiel: %s",
		// Ein Buchstabe eines Schalterbuendels ohne Erklaerung («-j: siehe man tar»).
		"hilfe.schalter.man": "siehe man %s",
		// Merkhilfen fuer Kurzschalter: eigenes Wort ohne den Buchstaben, dazu die Merkhilfe der
		// anderen Seite («Erstelle (englisch: [c]reate)»). Erklaerungen, die nur die Seite der
		// anderen Sprache hat, tragen deren Herkunft («[f]ile (englische Seite)»).
		"hilfe.merk.englisch":  "%s (englisch: %s)",
		"hilfe.merk.deutsch":   "%s (deutsch: %s)",
		"hilfe.seite.englisch": "%s (englische Seite)",
		"hilfe.seite.deutsch":  "%s (deutsche Seite)",
	}, map[string]string{
		"hilfe.keine_seite":        "No help for “%s”",
		"hilfe.schalter.unbekannt": "not in the examples — see man %s",
		"hilfe.schalter.auch":      "%s — example: %s",
		"hilfe.schalter.man":       "see man %s",
		"hilfe.merk.englisch":      "%s (English: %s)",
		"hilfe.merk.deutsch":       "%s (German: %s)",
		"hilfe.seite.englisch":     "%s (English page)",
		"hilfe.seite.deutsch":      "%s (German page)",
	})
}
