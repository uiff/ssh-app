package texte

// Texte des VNC-Viewers: Host-Felder (internal/server/api.go), WebSocket /api/vnc
// (internal/server/vnc.go) und RFB-Anmeldung (internal/vnc).
func init() {
	Eintragen(map[string]string{
		// Host-Felder
		"vnc.port": "VNC-Port muss zwischen 1 und 65535 liegen",
		"vnc.weg":  "Unbekannte VNC-Verbindung «%s» (erlaubt: ssh, direkt)",

		// Aufbau
		"vnc.aus":            "VNC ist für «%s» nicht eingeschaltet — im Host-Dialog einschalten",
		"vnc.verbinde":       "Verbinde mit %s (VNC) …",
		"vnc.tunnel.oeffnen": "Öffne den VNC-Tunnel über SSH zu Port %d …",
		"vnc.abgebrochen":    "VNC-Verbindungsaufbau abgebrochen",

		// Tunnel ueber SSH
		"vnc.tunnel.verboten":       "Der SSH-Server von %s erlaubt keine Weiterleitung (AllowTcpForwarding) — im Host-Dialog unter «Verbindung» «Direkt» wählen oder die Weiterleitung freigeben",
		"vnc.tunnel.kein.dienst":    "Auf %s antwortet kein VNC-Server auf Port %d (lokal über SSH geprüft)",
		"vnc.tunnel.fehlgeschlagen": "Der VNC-Tunnel über SSH zu %s liess sich nicht öffnen",

		// Direkte Verbindung
		"vnc.netz.keine.antwort":  "Keine Antwort von %s (Zeitüberschreitung nach %d s)",
		"vnc.netz.name.unbekannt": "Name %s unbekannt",
		"vnc.netz.name.dns":       "Name %s lässt sich nicht auflösen (Namensdienst antwortet nicht)",
		"vnc.netz.abgelehnt":      "Verbindung abgelehnt — läuft auf %s ein VNC-Server?",
		"vnc.netz.kein.weg":       "%s ist nicht erreichbar (kein Weg ins Netz)",
		"vnc.netz.zurueckgesetzt": "Die Verbindung zu %s wurde zurückgesetzt",
		"vnc.netz.fehlgeschlagen": "Verbindung zu %s fehlgeschlagen",

		// RFB-Anmeldung
		"vnc.kein.vnc":             "%s spricht kein VNC (RFB)",
		"vnc.version":              "%s meldet die unbekannte VNC-Version «%s»",
		"vnc.typ.unbekannt":        "%s verlangt ein unbekanntes Anmeldeverfahren (Typ %d)",
		"vnc.beendet":              "%s hat die Verbindung beendet — läuft dort ein VNC-Server, und lässt er diese Adresse zu?",
		"vnc.keine.antwort":        "Der VNC-Server %s antwortet nicht (Zeitüberschreitung nach %d s)",
		"vnc.abgelehnt":            "Der VNC-Server lehnt die Verbindung ab",
		"vnc.abgelehnt.grund":      "Der VNC-Server lehnt die Verbindung ab: %s",
		"vnc.passwort.noetig":      "Der VNC-Server verlangt ein Passwort",
		"vnc.passwort.falsch":      "VNC-Passwort falsch",
		"vnc.passwort.zu.oft":      "Zu viele falsche VNC-Passwörter — Verbindung beendet",
		"vnc.passwort.unlesbar":    "Das gespeicherte VNC-Passwort von «%s» lässt sich nicht entschlüsseln",
		"vnc.passwort.abgebrochen": "Anmeldung am VNC-Server abgebrochen — «Neu verbinden», um es erneut zu versuchen.",

		// Browser-Seite
		"vnc.browser.version":       "Der VNC-Viewer im Browser spricht eine unbekannte VNC-Version",
		"vnc.browser.typ":           "Der VNC-Viewer im Browser hat ein unerwartetes Anmeldeverfahren gewählt",
		"vnc.browser.keine.antwort": "Der VNC-Viewer im Browser antwortet nicht",
		"vnc.durchreichen.version":  "Der VNC-Viewer im Browser hat eine andere VNC-Version gewählt — bitte neu verbinden",

		// Schluessel des VNC-Servers (Durchreichen mit RA2)
		"vnc.schluessel.ungueltig": "Typ und Fingerabdruck des VNC-Schlüssels fehlen oder sind ungültig",
		"vnc.schluessel.geaendert": "Der Schlüssel des VNC-Servers von «%s» hat sich geändert — nur ersetzen, wenn der Fingerabdruck am Gerät stimmt",

		// Ende
		"vnc.ende.server": "Der VNC-Server hat die Verbindung beendet",
		"vnc.ende.haengt": "Der VNC-Server nimmt keine Daten mehr an — Verbindung getrennt",
	}, map[string]string{
		// Host fields
		"vnc.port": "VNC port must be between 1 and 65535",
		"vnc.weg":  "Unknown VNC connection type “%s” (allowed: ssh, direkt)",

		// Setup
		"vnc.aus":            "VNC isn’t turned on for “%s” — turn it on in the host dialog",
		"vnc.verbinde":       "Connecting to %s (VNC)…",
		"vnc.tunnel.oeffnen": "Opening the VNC tunnel over SSH to port %d…",
		"vnc.abgebrochen":    "VNC connection canceled",

		// Tunnel over SSH
		"vnc.tunnel.verboten":       "The SSH server on %s doesn’t allow forwarding (AllowTcpForwarding) — choose “Direct” under “Connection” in the host dialog or allow forwarding",
		"vnc.tunnel.kein.dienst":    "No VNC server is responding on %s, port %d (checked locally over SSH)",
		"vnc.tunnel.fehlgeschlagen": "Couldn’t open the VNC tunnel over SSH to %s",

		// Direct connection
		"vnc.netz.keine.antwort":  "No response from %s (timed out after %d s)",
		"vnc.netz.name.unbekannt": "Unknown host name %s",
		"vnc.netz.name.dns":       "Can’t resolve %s (DNS server not responding)",
		"vnc.netz.abgelehnt":      "Connection refused — is a VNC server running on %s?",
		"vnc.netz.kein.weg":       "%s is unreachable (no route to host)",
		"vnc.netz.zurueckgesetzt": "The connection to %s was reset",
		"vnc.netz.fehlgeschlagen": "Connection to %s failed",

		// RFB login
		"vnc.kein.vnc":             "%s doesn’t speak VNC (RFB)",
		"vnc.version":              "%s reports the unknown VNC version “%s”",
		"vnc.typ.unbekannt":        "%s requires an unknown authentication method (type %d)",
		"vnc.beendet":              "%s closed the connection — is a VNC server running there, and does it allow this address?",
		"vnc.keine.antwort":        "The VNC server %s isn’t responding (timed out after %d s)",
		"vnc.abgelehnt":            "The VNC server rejected the connection",
		"vnc.abgelehnt.grund":      "The VNC server rejected the connection: %s",
		"vnc.passwort.noetig":      "The VNC server requires a password",
		"vnc.passwort.falsch":      "Incorrect VNC password",
		"vnc.passwort.zu.oft":      "Too many incorrect VNC passwords — connection closed",
		"vnc.passwort.unlesbar":    "Couldn’t decrypt the stored VNC password for “%s”",
		"vnc.passwort.abgebrochen": "VNC login canceled — choose “Reconnect” to try again.",

		// Browser side
		"vnc.browser.version":       "The VNC viewer in the browser speaks an unknown VNC version",
		"vnc.browser.typ":           "The VNC viewer in the browser chose an unexpected authentication method",
		"vnc.browser.keine.antwort": "The VNC viewer in the browser isn’t responding",
		"vnc.durchreichen.version":  "The VNC viewer in the browser chose a different VNC version — please reconnect",

		// VNC server key (pass-through with RA2)
		"vnc.schluessel.ungueltig": "The VNC key’s type and fingerprint are missing or invalid",
		"vnc.schluessel.geaendert": "The VNC server key for “%s” has changed — replace it only if the fingerprint matches the one on the device",

		// End
		"vnc.ende.server": "The VNC server closed the connection",
		"vnc.ende.haengt": "The VNC server stopped accepting data — disconnected",
	})
}
