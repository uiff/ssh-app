<p align="right"><a href="README.md">English</a> | <a href="README.de.md"><b>Deutsch</b></a></p>

<p align="center">
  <img src="web/static/img/icon.svg" width="72" height="72" alt="">
</p>

<h1 align="center">SSH-App</h1>

<p align="center">
  Ein selbst gehosteter Web-SSH-Client: Terminal, Dateien und Befehlshilfe in einem Browser-Tab, am Rechner und auf dem Telefon.
</p>

<p align="center">
  <a href="https://iotueli.ch"><img src="web/static/img/iotueli.svg" height="32" alt="IoTUeli"></a><br>
  <sub>Erstellt von <a href="https://iotueli.ch">IoTUeli</a></sub>
</p>

![SSH-App am Rechner: eine Terminal-Sitzung mit offener Dateileiste und Befehlshilfe](docs/bilder/de/desktop-arbeitsplatz.png)

Wie MobaXterm, aber im Browser. SSH-App läuft als ein einziger Container auf deiner eigenen Hardware, ohne Cloud-Dienst
und ohne Konto bei Dritten. Die Oberfläche gibt es auf Deutsch und Englisch.

## Funktionen

### Terminal

- **Terminal im Browser:** Tabs, 256 Farben, Vollbildprogramme (`top`, `htop`, `vi`), anklickbare Links, Kopieren und
  Einfügen (<kbd>Strg</kbd>+<kbd>Umschalt</kbd>+<kbd>C</kbd> / <kbd>Strg</kbd>+<kbd>Umschalt</kbd>+<kbd>V</kbd>).
- **Sitzungen laufen weiter:** bis zu 12 Stunden auf dem Server, auch ohne offenes Fenster. Nach einem Neuladen oder
  einem Funkloch hängen sie sich von selbst wieder an, mit dem bisherigen Bildschirminhalt. Öffnest du dieselbe Sitzung
  in einem zweiten Fenster, tritt das erste zurück und holt sie mit einem Klick wieder zu sich.
- **Verständliche Fehler:** Klappt eine Verbindung nicht, steht da ein klarer Satz statt eines Netzwerkfehlers, zum
  Beispiel «Verbindung abgelehnt — läuft auf 10.0.0.5:22 ein SSH-Dienst?». Nach rund 20 Sekunden gibt die App auf.
- **Host-Schlüssel:** Beim ersten Verbinden fragt die App nach dem Host-Schlüssel und warnt deutlich, wenn er sich später
  ändert. Die bekannten Host-Schlüssel stehen in den Einstellungen, dort lassen sie sich auch entfernen.
- **An alle senden:** Eingaben gleichzeitig an mehrere Sitzungen schicken.
- **Kritische Hosts:** Bei Hosts mit dem Tag `kritisch` (oder `critical`) fragt die App nach, bevor ein gefährlicher Befehl wie `rm -rf`,
  `dd` oder `reboot` aus einem Snippet, aus der Befehlshilfe oder per «An alle senden» dort ankommt.

### Hosts mit Gerätetyp

![Host-Dialog: Gerätetyp mit Symbolen, darunter Name, Adresse, Benutzer und Passwort](docs/bilder/de/desktop-host-dialog.png)

- **Gerätetypen mit Symbol:** Jeder Host bekommt ein Symbol, zum Beispiel SPS, HMI/Panel, Gateway, IoT-Gerät, Wallbox,
  Wechselrichter, Server, virtuelle Maschine, Container, PC, Laptop, Raspberry Pi, Router, Switch, Firewall, NAS,
  Cloud/VPS oder Drucker. So erkennst du deine Geräte in der Liste auf einen Blick.
- **Einfacher Host-Dialog:** Sichtbar sind nur Gerätetyp, Name, Adresse, Benutzer, Passwort und Ordner. Das Adressfeld
  versteht auch `benutzer@host:port`, `ssh://…`, `ssh -p 2222 benutzer@host` oder `[fe80::1]:2222` und füllt Benutzer und
  Port selbst aus. Port, Schlüssel oder Zugang aus dem Tresor, Tags, Farbe, Notiz und «Dateileiste folgt dem Terminal»
  liegen unter **Erweitert**.
- **Ordner, Tags, Farben und Notizen.** Der Filter über der Liste sucht in Namen, Adressen, Benutzern, Gerätetypen und
  Tags. Passt genau ein Host, verbindet <kbd>Enter</kbd>.

### Tresor und Zugangsdaten

- **Passwort speichern:** Mit dem Haken «Passwort speichern» im Host-Dialog legt die App das Passwort als verschlüsselten
  Zugang im Tresor ab. Ohne Haken fragt sie beim Verbinden nach Benutzer und Passwort und kann die Zugangsdaten dort nach
  erfolgreicher Anmeldung speichern.
- **Tresor:** Zugänge (Benutzer mit Passwort oder privatem Schlüssel) liegen verschlüsselt (AES-256-GCM, Schlüssel aus dem
  Anmeldepasswort per Argon2id). Schlüssel lassen sich importieren, neu erzeugen (ed25519) und per Knopfdruck auf einem
  Host eintragen (angehängt an `~/.ssh/authorized_keys`). Mehrere Hosts können denselben Zugang nutzen, so änderst du ein
  Passwort nur an einer Stelle.
- **Automatische Sperre:** Nach 30 Minuten ohne Bedienung meldet die App dich ab und sperrt den Tresor. Laufende
  Sitzungen bleiben bestehen und sind nach der Anmeldung wieder da.
- **Sicherheitsschlüssel:** wird bei der Einrichtung einmal angezeigt. Mit ihm setzt du ein vergessenes Passwort zurück
  und spielst Backups ein.

### Dateien

- **SFTP neben dem Terminal:** Hoch- und Herunterladen (auch per Ziehen und Ablegen), Umbenennen, Löschen, Ordner anlegen,
  Pfad kopieren, ins Terminal einfügen oder mit `cd` dorthin wechseln.
- **Sicheres Ersetzen:** Beim Ersetzen per Hochladen bleibt die Datei selbst erhalten (Besitzer, Rechte, harte Links,
  per Docker eingebundene Dateien). Ein abgebrochener Upload lässt das Original unverändert. Spezialdateien (Geräte,
  Pipes, Sockets) lassen sich weder herunterladen noch ersetzen.
- **Folgt dem Terminal:** Auf Linux-Hosts zeigt die Dateileiste nach `cd /var/log` den Ordner `/var/log`, mit bash, zsh,
  fish, tcsh und anderen Shells. Versteht ein Gerät den Startbefehl dafür nicht (Router-CLI, Windows), öffnet die App von
  selbst eine normale Shell. Die Dateileiste folgt dann nur, wenn die Shell ihren Ordner meldet (OSC 7).

### Befehlshilfe und Suche

![Suche: «archiv entpacken» findet die passenden Befehle, das gewählte Beispiel ist bereit zum Einfügen](docs/bilder/de/desktop-suche.png)

- **Befehlshilfe offline:** rund 7000 Befehle aus den [tldr-pages](https://github.com/tldr-pages/tldr), in der Sprache
  der Oberfläche, wo es eine Übersetzung gibt, sonst in der anderen Sprache. Ohne Internetverbindung.
- **Erklärt, was du tippst:** Die Hilfe-Leiste zeigt Beispiele zum Befehl in der aktuellen Zeile und was seine Schalter
  bedeuten. Beispiele haben Platzhalter, die du ausfüllst, bevor sie ins Terminal gehen.
- **Suche in Alltagssprache:** «archiv entpacken» oder «port belegt» findet die passenden Befehle.
- **Snippets:** eigene Befehle speichern, mit Platzhaltern wie `journalctl -u {{dienst}}`. Beim Einfügen fragt die App
  nach den Werten.
- **Eine Suche für alles:** <kbd>Strg</kbd>+<kbd>Umschalt</kbd>+<kbd>K</kbd> durchsucht Hosts, Snippets und Befehle, von
  überall aus (am Mac <kbd>Cmd</kbd>+<kbd>K</kbd>). <kbd>Strg</kbd>+<kbd>K</kbd> allein geht nur ausserhalb des Terminals,
  denn dort gehört die Taste der Shell (nano, bash).

### Auf dem Telefon

<table>
  <tr>
    <td align="center"><img src="docs/bilder/de/telefon-terminal.png" width="300" alt="Terminal auf dem Telefon, über der Tastatur die Tastenleiste mit Esc, Tab, Ctrl, Alt und Pfeilen"></td>
    <td align="center"><img src="docs/bilder/de/telefon-hosts.png" width="300" alt="Host-Liste auf dem Telefon, jeder Host mit seinem Gerätesymbol"></td>
  </tr>
</table>

- **Tastenleiste** mit Esc, Tab, Ctrl, Alt, Pfeilen, Pos1/Ende, Bild auf/ab, <kbd>Strg</kbd>+<kbd>C</kbd>/<kbd>D</kbd>/<kbd>Z</kbd>/<kbd>R</kbd>,
  den häufigsten Sonderzeichen der Shell sowie Einfügen und Kopieren.
- **Für die Finger gemacht:** Menü, Dateien und Hilfe öffnen als Blätter, jede Trefferfläche ist mindestens 44 px gross.
- **Startbildschirm:** Leg SSH-App auf den Startbildschirm, dann läuft sie wie eine App.

### Sprache und Aussehen

- **Deutsch und Englisch:** Die Oberfläche folgt der Sprache des Browsers. Umstellen kannst du sie auf der Anmeldeseite,
  im Menü oder unter Einstellungen › Darstellung. Die Seite lädt dann neu, laufende Terminals verbinden sich von selbst wieder.
  Auch Meldungen des Servers, etwa zu Verbindungsfehlern, kommen in dieser Sprache.
- **Akzentfarbe:** Vorgabe Violett, umstellbar auf Blau, Türkis, Grün, Bernstein oder Rosa. Die Wahl gilt für den
  jeweiligen Browser. Warnungen behalten ihre eigenen Farben.
- **Schriftgrösse im Terminal**, ebenfalls je Browser.

### Backup

- **Eine verschlüsselte Datei** mit allen Hosts, Ordnern, Zugängen, Snippets und bekannten Host-Schlüsseln. Mit dem
  Sicherheitsschlüssel lässt sie sich auf diesem oder einem anderen System wieder einspielen.

## Schnellstart

```bash
git clone https://github.com/uiff/ssh-app.git
cd ssh-app
docker compose up -d --build
```

Danach im Browser `https://<adresse-des-rechners>:8443` öffnen und **sofort einrichten** (siehe unten). Die App erzeugt
beim ersten Start ein eigenes Zertifikat. Der Browser warnt deshalb einmal, das ist erwartet. Den Fingerabdruck zum
Vergleichen zeigt `docker logs ssh-app`.

Ohne Compose:

```bash
docker build -t ssh-app https://github.com/uiff/ssh-app.git
docker run -d --name ssh-app -p 8443:8443 -v ssh-app-daten:/data --restart unless-stopped ssh-app
```

Die Compose-Datei härtet den Container zusätzlich (nur lesbares Dateisystem, keine Linux-Capabilities). Wer `docker run`
nimmt, kann das mit `--read-only --tmpfs /tmp --cap-drop ALL --security-opt no-new-privileges:true` nachbilden.

Statt des Volumes geht auch ein Ordner des Rechners (`-v /pfad/zu/daten:/data`). Er muss dem Benutzer gehören, als der die
App im Container läuft: `sudo chown 65532:65532 /pfad/zu/daten`. Sonst bricht der Start mit einem Hinweis im Log ab.

### Für Raspberry Pi und andere Plattformen bauen

Das Image gibt es für `linux/amd64` und `linux/arm64` (z.B. Raspberry Pi oder Industrie-Controller). Das Programm wird per
Cross-Compile übersetzt, ein Emulator (QEMU) ist nicht nötig.

- **Auf dem Gerät selbst:** `docker compose up -d --build` wie oben, die Plattform ergibt sich von selbst.
- **Auf einem anderen Rechner für ein Gerät** (eine Plattform, klappt mit dem Standard-Builder):

  ```bash
  docker buildx build --platform linux/arm64 -t ssh-app --load .
  docker save ssh-app | gzip > ssh-app-arm64.tar.gz
  ```

  Die Datei aufs Gerät kopieren, dort `docker load -i ssh-app-arm64.tar.gz` und den Container mit `docker run` wie oben
  starten. Mit der Compose-Datei geht es auch: `docker compose up -d` (ohne `--build`) nimmt das geladene Image `ssh-app:latest`.
- **Mehrere Plattformen in einem Image:** Das braucht einen buildx-Builder mit dem Treiber `docker-container`. Der
  Standard-Builder (Treiber `docker`) lehnt mehrere Plattformen ab, ausser der containerd-Image-Speicher ist eingeschaltet.
  Ein Image für mehrere Plattformen lässt sich ausserdem nicht in den lokalen Image-Speicher laden, es gehört in eine Registry:

  ```bash
  docker buildx create --name ssh-app-bau --driver docker-container --use   # einmalig
  docker buildx build --platform linux/amd64,linux/arm64 -t <registry>/ssh-app:<version> --push .
  ```

  Ohne `--push` (oder `--output`) bleibt das Ergebnis nur im Build-Cache liegen.

## Ersteinrichtung

![Einrichtung: Der Sicherheitsschlüssel wird einmal angezeigt, mit Knöpfen zum Kopieren und zum Speichern als Datei](docs/bilder/de/einrichten.png)

**Gleich nach dem ersten Start erledigen.** Solange die App nicht eingerichtet ist, kann jeder, der ihre Adresse erreicht,
das Konto anlegen oder ein Backup einspielen: Wer zuerst kommt, richtet ein. Den Port deshalb erst freigeben, wenn die
Einrichtung erledigt ist.

1. Benutzername und Passwort festlegen (mindestens 10 Zeichen). Das Passwort entsperrt auch den Tresor.
2. Die App zeigt **einmal** den **Sicherheitsschlüssel** an. Kopieren oder als Datei speichern und getrennt von der App
   aufbewahren, zum Beispiel im Passwort-Manager. Mit ihm kannst du
   - das Passwort zurücksetzen («Passwort vergessen?» auf der Anmeldeseite),
   - ein Backup einspielen, auch auf einem anderen System.

   Ohne Sicherheitsschlüssel ist ein Backup nicht lesbar.
3. Hosts anlegen: Gerätetyp wählen, Adresse und Benutzer eintragen, Passwort eingeben oder beim Verbinden fragen lassen.

## Backup und Umzug

- **Backup herunterladen:** Einstellungen › Backup. Du bekommst eine verschlüsselte Datei, Passwörter und Schlüssel darin
  bleiben verschlüsselt.
- **Umzug auf ein neues System:** dort beim ersten Start «Backup einspielen» wählen, Backup-Datei und
  Sicherheitsschlüssel angeben. Danach meldest du dich mit Benutzer und Passwort des alten Systems an.
- **Auf einem laufenden System einspielen:** Einstellungen › Backup › **Backup einspielen** ersetzt alle Hosts, Zugänge
  und Snippets auf diesem System.
- **Passwort ändern:** Der Sicherheitsschlüssel bleibt dabei gültig.

## Einstellungen (Umgebungsvariablen)

Das Image setzt einige Variablen selbst (Spalte «Container»). Wer das Programm ohne Container startet, etwa mit
`go run`, bekommt die Vorgaben des Programms (Spalte «ohne Container»).

| Variable | Container | ohne Container | Bedeutung |
|---|---|---|---|
| `SSHAPP_ADDR` | `:8443` | `:8443` | Adresse und Port. Im Container immer in der Form `:<port>`, die eingebaute Gesundheitsprüfung fragt `127.0.0.1:<port>` ab |
| `SSHAPP_DATA` | `/data` | `./data` | Datenordner (Datenbank, Zertifikat) |
| `SSHAPP_HILFE` | `/app/hilfe` | `<SSHAPP_DATA>/hilfe` | Ordner mit `tldr-pages.de.zip` und `tldr-pages.en.zip` (Befehlshilfe) |
| `SSHAPP_TLS` | `auto` | `auto` | `auto` = selbst erzeugtes Zertifikat, `aus` = nur HTTP (nur hinter einem Reverse-Proxy), `eigen` = eigenes Zertifikat aus `SSHAPP_TLS_CERT` und `SSHAPP_TLS_KEY` |
| `SSHAPP_TLS_CERT` | – | – | Bei `SSHAPP_TLS=eigen`: Pfad zum Zertifikat (PEM, mit Zwischenzertifikaten) |
| `SSHAPP_TLS_KEY` | – | – | Bei `SSHAPP_TLS=eigen`: Pfad zum privaten Schlüssel (PEM). Im Container muss er für den Benutzer 65532 lesbar sein |
| `SSHAPP_COOKIE_SECURE` | – | – | `1` = Cookie nur über HTTPS (bei `SSHAPP_TLS=aus` hinter einem HTTPS-Proxy setzen) |
| `SSHAPP_PROXY` | – | – | Adressen oder Netze vertrauenswürdiger Reverse-Proxys, durch Komma getrennt (z.B. `172.16.0.0/12` oder `10.0.0.5, fd00::/8`). Nur bei Anfragen von dort gelten `X-Forwarded-For` (Anmeldesperre je Browser statt je Proxy) und `X-Forwarded-Host` (Terminal-WebSocket) |
| `SSHAPP_HOSTS` | – | – | Zusätzlich erlaubte Namen der App, durch Komma getrennt, `*.firma.ch` für alle Unternamen (Schutz vor DNS-Rebinding, siehe unten). Ist die Variable gesetzt, prüft die App den Namen immer, auch mit HTTPS |
| `SSHAPP_ORIGINS` | – | – | Zusätzlich erlaubte Herkunft (`Origin`) für den Terminal-WebSocket, durch Komma getrennt, Muster wie `https://*.firma.ch`. Nur nötig, wenn ein Proxy den Host-Kopf verändert |
| `SSHAPP_HSTS` | – | – | `1` = Strict-Transport-Security senden: Browser rufen den Namen dann ein Jahr lang nur noch per HTTPS auf, auf allen Ports. Nur setzen, wenn alles unter diesem Namen HTTPS spricht |
| `SSHAPP_LEERLAUF_MIN` | `30` | `30` | Minuten ohne Bedienung, bis die Anmeldung abläuft und der Tresor sich sperrt. Bedienung heisst Tippen im Terminal und Aktionen in der App; automatische Abfragen im Hintergrund zählen nicht. Laufende Sitzungen bleiben bestehen und lassen sich nach der Anmeldung wieder öffnen |

**Schutz vor DNS-Rebinding:** Eine fremde Webseite könnte ihren eigenen Namen auf die Adresse der App zeigen lassen. Deshalb
prüft die App im HTTP-Betrieb (`SSHAPP_TLS=aus`) und immer, sobald `SSHAPP_HOSTS` gesetzt ist, unter welchem Namen sie
aufgerufen wird. Ausgenommen ist nur der Betrieb hinter einem HTTPS-Proxy (`SSHAPP_COOKIE_SECURE=1`) ohne `SSHAPP_PROXY`
und ohne `SSHAPP_HOSTS`, denn dann kennt die App den Namen nicht. Ohne Eintrag erlaubt sind IP-Adressen, `localhost`,
Rechnernamen ohne Punkt und die lokalen Endungen `.local`, `.lan`, `.home`, `.home.arpa`, `.internal`, `.intranet`,
`.localdomain`, `.localhost`, `.fritz.box` und `.ts.net` (Tailscale). Andere Namen (etwa `ssh.firma.ch`) gehören in
`SSHAPP_HOSTS`. Anfragen von einem Proxy aus `SSHAPP_PROXY` prüft die App nicht, das übernimmt der Proxy.

## Betrieb hinter einem Reverse-Proxy

Mit einem Proxy (Caddy, nginx, Traefik) und einem richtigen Zertifikat entfällt die Zertifikatswarnung. Die App spricht
dann nur HTTP und ist nur für den Proxy erreichbar. In `compose.yml`:

```yaml
    ports:
      - "127.0.0.1:8080:8080"     # nur für den Proxy auf demselben Rechner
    environment:
      SSHAPP_ADDR: ":8080"
      SSHAPP_TLS: "aus"
      SSHAPP_COOKIE_SECURE: "1"   # Cookie nur über HTTPS
      SSHAPP_PROXY: "172.16.0.0/12"  # so sieht die App einen Proxy auf dem Rechner (Docker-Netze)
```

Unter welcher Adresse die App den Proxy sieht, steht nach einem Anmeldeversuch im Log (`docker logs ssh-app`, Zeile
«Anmeldung von …»). Läuft der
Proxy auf demselben Rechner, ist das die Gateway-Adresse des Docker-Netzes, meist aus `172.16.0.0/12`. Liegt das eigene
Netz selbst in diesem Bereich, besser nur die genaue Adresse eintragen.

Was der Proxy können muss:

- **WebSockets** weiterreichen (Terminal unter `/api/ws`). Der Browser schickt alle 25 Sekunden ein Lebenszeichen, die
  üblichen Zeitlimits von 60 Sekunden reichen.
- **Den Host-Kopf unverändert weitergeben, samt Port** (oder `X-Forwarded-Host` setzen; das gilt nur, wenn der Proxy in
  `SSHAPP_PROXY` steht). Die App lässt einen WebSocket nur zu, wenn `Origin` und `Host` übereinstimmen. Sonst öffnet sich
  die Seite, aber kein Terminal.
- **`X-Forwarded-For`** setzen, damit die Anmeldesperre den einzelnen Browser trifft und nicht alle.
- **Grosse Uploads** zulassen. Die Dateileiste lädt Dateien beliebiger Grösse hoch, ein Backup darf bis 64 MB gross sein.

Caddy erfüllt das von sich aus:

```
ssh.example.lan {
    reverse_proxy 127.0.0.1:8080
}
```

nginx braucht ein paar Zeilen mehr:

```nginx
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 443 ssl;
    server_name ssh.example.lan;
    # ssl_certificate und ssl_certificate_key wie üblich

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $http_host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        client_max_body_size 0;
    }
}
```

Die App zählt Fehlversuche bei der Anmeldung je Absender-Adresse. Hinter einem Proxy sieht sie nur dessen Adresse,
ausser er steht in `SSHAPP_PROXY`: Dann nimmt sie die Adresse des Browsers aus `X-Forwarded-For`. Ohne `SSHAPP_PROXY` ist
die Anmeldung nach 5 Fehlversuchen 15 Minuten lang für alle gesperrt, nicht nur für den Absender. Darauf weist die App
beim Start im Log hin.

## Sicherheit

- Anmeldung mit Sperre: nach 5 Fehlversuchen von einer Adresse 15 Minuten lang. IPv6-Adressen aus fremden Netzen zählen
  zusätzlich gemeinsam je /64-Netz (Grenze 30), denn ein Anschluss hat dort viele Adressen. Laufende Versuche zählen mit,
  paralleles Raten stösst also an dieselbe Grenze, und bei Überlast bittet die Anmeldung kurz um Geduld. Dazu Cookies
  `HttpOnly` und `SameSite=Strict`, eigener Kopf gegen CSRF, strenge Content-Security-Policy, Schutz vor DNS-Rebinding im
  HTTP-Betrieb.
- Passwörter und private Schlüssel liegen nur verschlüsselt in der Datenbank. Der Schlüssel dazu liegt nur im Arbeitsspeicher
  und wird beim Sperren verworfen (Abmelden oder Leerlauf). Kopien, die Go selbst anlegt (etwa das eingegebene Passwort oder
  interne Puffer), lassen sich nicht garantiert löschen. Wer den Speicher des Prozesses lesen kann (root, Core-Dump, Swap),
  findet sie unter Umständen. Für den Verbindungsaufbau muss der Server ein Geheimnis kurz entschlüsseln. Eine
  Ende-zu-Ende-Verschlüsselung bis in den Browser ist für ein SSH-Gateway nicht möglich.
- Das Image enthält keine Shell und läuft als unprivilegierter Benutzer (65532). Die Compose-Datei setzt zusätzlich ein nur
  lesbares Dateisystem und entzieht alle Linux-Capabilities.
- Die Ersteinrichtung ist offen, bis jemand sie erledigt. Deshalb gleich nach dem Start einrichten (siehe oben).
- Die App ist für das eigene Netz gedacht. Nicht ungeschützt ins Internet stellen.

## Entwicklung

```bash
go test ./...          # Tests inkl. Ende-zu-Ende mit eingebettetem SSH-Server
go run ./cmd/ssh-app   # startet auf https://localhost:8443, Daten in ./data
node --experimental-websocket tools/klicktest.js <url> <ssh-host> <port> <benutzer> <passwort> <fotoordner> [breite hoehe]
```

`tools/klicktest.js` klickt sich per Headless-Chrome durch die Oberfläche und macht dabei Fotos. Mit `-` als
`<ssh-host>` prüft es die Oberfläche ohne SSH-Ziel. `SPRACHE=de|en` wählt die Sprache der Oberfläche;
`DATEIEN_ORDNER=<ordner>` prüft zusätzlich die Dateileiste (nur mit dem eingebetteten Test-SSH-Server:
`SSHAPP_TESTSSH_HALTEN=<sekunden> go test ./internal/server -run 'TestSSHHalten$' -v` nennt seine Adresse).

Für die Befehlshilfe in der Entwicklung `tldr-pages.de.zip` und `tldr-pages.en.zip` von
<https://github.com/tldr-pages/tldr/releases> nach `./data/hilfe/` legen (bzw. nach `<SSHAPP_DATA>/hilfe/`).
Das Image packt die Archive beim Bau selbst, aus einem festen Stand des tldr-Repositorys (`TLDR_COMMIT` im Dockerfile).

Aufbau: Go-Backend (`internal/`), eingebettete Oberfläche ohne Build-Schritt (`web/static/`), SQLite als Speicher.

Übersetzungen: Jeder Text liegt auf Deutsch und Englisch vor.

- Browser: Kataloge in `web/static/js/sprache/<bereich>.js` (`i18nErgaenzen({ de: {…}, en: {…} })`). Im Code steht
  `t('bereich.schluessel', { name })`.
- Server: Kataloge in `internal/texte/<bereich>.go` (`texte.Eintragen(de, en)`), Aufruf mit `texte.T(sprache, schluessel, …)`.
  `internal/texte/texte_test.go` prüft, dass jeder Schlüssel beide Sprachen mit denselben Platzhaltern hat.
- Der Browser schickt seine Sprache bei jeder Anfrage mit (Kopf `X-SSH-App-Sprache`, beim WebSocket und bei Downloads
  der Parameter `sprache`).
- Wächter-Tests: `web/sprache_test.go` (jeder im Browser-Code benutzte Schlüssel steht in beiden Sprachen, keiner ist
  ungenutzt, kein deutscher Text ausserhalb der Kataloge, keine Emoji) und `internal/server/rohtext_test.go` (keine
  Text-Literale in HTTP- und WebSocket-Antworten, jeder im Go-Code benutzte Schlüssel existiert mit passenden Platzhaltern).

## Lizenz

MIT, siehe [LICENSE](LICENSE). Drittsoftware und Daten: siehe [NOTICE](NOTICE). Die Lizenztexte aller mitgelieferten und
ins Programm übersetzten Bibliotheken sind eingebettet und in der App unter Einstellungen › Über › Lizenzen abrufbar.
Die Befehlshilfe stammt aus den [tldr-pages](https://github.com/tldr-pages/tldr) (CC BY 4.0). Name und Logo «IoTUeli»
sind Kennzeichen von IoTUeli und nicht Teil der MIT-Lizenz.
