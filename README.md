# ssh-app

Ein Web-SSH-Client im Stil von MobaXterm: Terminal, Dateileiste und Befehlshilfe in einem Browser-Tab, auch auf dem Telefon.
Selbst gehostet als ein einziger Container, ohne Cloud-Dienst und ohne Konto bei Dritten.

Erstellt von [IoTUeli](https://iotueli.ch).

## Funktionen

- **Terminal im Browser:** Tabs, 256 Farben, Vollbildprogramme (top, vi, htop), Kopieren und Einfügen.
  Sitzungen laufen auf dem Server weiter und hängen sich nach einem Neuladen oder einem Funkloch wieder an.
- **Dateileiste (SFTP):** Hoch- und Herunterladen, Umbenennen, Löschen, Ordner anlegen. Auf Linux-Hosts folgt sie dem
  Terminal: Nach `cd /var/log` zeigt sie `/var/log`.
- **Tresor:** Zugänge (Benutzer mit Passwort oder privatem Schlüssel) liegen verschlüsselt (AES-256-GCM, Schlüssel aus dem
  Anmeldepasswort per Argon2id). Schlüssel lassen sich importieren, neu erzeugen (ed25519) und per Knopfdruck auf einem Host eintragen.
- **Hosts ordnen:** Ordner, Tags, Farben, Jump-Host, Notizen. Beim ersten Verbinden fragt die App nach dem Host-Schlüssel und warnt
  deutlich, wenn er sich später ändert.
- **Befehlshilfe:** rund 7000 Befehle offline (tldr-pages, Deutsch mit englischem Rückfall). Suche auch in Alltagssprache
  («archiv entpacken», «port belegt»), Erklärung der gerade getippten Zeile, Beispiele mit Platzhaltern, eigene Snippets.
- **Telefon:** Tastenleiste mit Esc, Tab, Ctrl, Alt, Pfeilen und Sonderzeichen. Menü, Dateien und Hilfe öffnen als Blätter.
  Lässt sich als App auf den Startbildschirm legen.
- **An alle senden:** Eingaben gleichzeitig an mehrere Sitzungen schicken. Bei Hosts mit dem Tag `kritisch` fragt die App vor
  gefährlichen Befehlen nach.
- **Backup:** eine verschlüsselte Datei mit allen Hosts, Zugängen, Snippets und bekannten Host-Schlüsseln. Lässt sich auf einem
  anderen System wieder einspielen.

## Schnellstart

```bash
git clone https://github.com/uiff/ssh-app.git
cd ssh-app
docker compose up -d --build
```

Danach im Browser `https://<adresse-des-rechners>:8443` öffnen. Die App erzeugt beim ersten Start ein eigenes Zertifikat.
Der Browser warnt deshalb einmal, das ist erwartet. Den Fingerabdruck zum Vergleichen zeigt `docker logs ssh-app`.

Ohne Compose:

```bash
docker build -t ssh-app https://github.com/uiff/ssh-app.git
docker run -d --name ssh-app -p 8443:8443 -v ssh-app-daten:/data --restart unless-stopped ssh-app
```

Das Image baut für `linux/amd64` und `linux/arm64` (z.B. Raspberry Pi oder Industrie-Controller):
`docker buildx build --platform linux/amd64,linux/arm64 -t ssh-app .`

## Ersteinrichtung

1. Benutzername und Passwort festlegen (mindestens 10 Zeichen). Das Passwort entsperrt auch den Tresor.
2. Die App zeigt **einmal** den **Sicherheitsschlüssel** an. Kopieren oder als Datei speichern und getrennt aufbewahren,
   zum Beispiel im Passwort-Manager. Mit ihm kannst du
   - das Passwort zurücksetzen («Passwort vergessen?» auf der Anmeldeseite),
   - ein Backup einspielen, auch auf einem anderen System.
   Ohne Sicherheitsschlüssel ist ein Backup nicht lesbar.
3. Hosts und Zugänge anlegen und verbinden.

Umzug auf ein neues System: dort beim ersten Start «Backup einspielen» wählen, Backup-Datei und Sicherheitsschlüssel angeben.
Danach meldest du dich mit Benutzer und Passwort des alten Systems an.

## Einstellungen (Umgebungsvariablen)

| Variable | Vorgabe | Bedeutung |
|---|---|---|
| `SSHAPP_ADDR` | `:8443` | Adresse und Port |
| `SSHAPP_DATA` | `/data` | Datenordner (Datenbank, Zertifikat) |
| `SSHAPP_TLS` | `auto` | `auto` = eigenes Zertifikat, `aus` = nur HTTP (nur hinter einem Reverse-Proxy), `eigen` = eigenes Zertifikat aus `SSHAPP_TLS_CERT` und `SSHAPP_TLS_KEY` |
| `SSHAPP_COOKIE_SECURE` | – | `1` = Cookie nur über HTTPS (bei `SSHAPP_TLS=aus` hinter einem HTTPS-Proxy setzen) |
| `SSHAPP_LEERLAUF_MIN` | `30` | Minuten ohne Bedienung, bis der Tresor sich sperrt und eine neue Anmeldung nötig ist |
| `SSHAPP_HILFE` | `/app/hilfe` | Ordner mit `tldr-pages.de.zip` und `tldr-pages.en.zip` |

## Sicherheit

- Anmeldung mit Sperre nach 5 Fehlversuchen (15 Minuten je Adresse), Cookies `HttpOnly` und `SameSite=Strict`,
  eigener Kopf gegen CSRF, strenge Content-Security-Policy.
- Passwörter und private Schlüssel liegen nur verschlüsselt in der Datenbank. Der Schlüssel dazu existiert nur im Arbeitsspeicher,
  solange jemand angemeldet ist. Für den Verbindungsaufbau muss der Server ein Geheimnis kurz entschlüsseln. Eine
  Ende-zu-Ende-Verschlüsselung bis in den Browser ist für ein SSH-Gateway nicht möglich.
- Das Image enthält keine Shell und läuft als unprivilegierter Benutzer. Die Compose-Datei setzt zusätzlich ein nur lesbares
  Dateisystem und entzieht alle Linux-Capabilities.
- Die App ist für das eigene Netz gedacht. Nicht ungeschützt ins Internet stellen.

## Entwicklung

```bash
go test ./...                     # Tests inkl. Ende-zu-Ende mit eingebettetem SSH-Server
go run ./cmd/ssh-app              # startet auf https://localhost:8443, Daten in ./data
node --experimental-websocket tools/klicktest.js <url> <ssh-host> <port> <user> <pw> <fotoordner> [breite hoehe]
```

Für die Befehlshilfe in der Entwicklung `tldr-pages.de.zip` und `tldr-pages.en.zip` von
<https://github.com/tldr-pages/tldr/releases> nach `./data/hilfe/` legen.

Aufbau: Go-Backend (`internal/`), eingebettete Oberfläche ohne Build-Schritt (`web/static/`), SQLite als Speicher.

## Lizenz

MIT, siehe [LICENSE](LICENSE). Drittsoftware und Daten: siehe [NOTICE](NOTICE).
Die Befehlshilfe stammt aus den [tldr-pages](https://github.com/tldr-pages/tldr) (CC BY 4.0).
