# ssh-app

Ein Web-SSH-Client im Stil von MobaXterm: Terminal, Dateileiste und Befehlshilfe in einem Browser-Tab, auch auf dem Telefon.
Selbst gehostet als ein einziger Container, ohne Cloud-Dienst und ohne Konto bei Dritten.

Erstellt von [IoTUeli](https://iotueli.ch).

## Funktionen

- **Terminal im Browser:** Tabs, 256 Farben, Vollbildprogramme (top, vi, htop), Kopieren und Einfügen.
  Sitzungen laufen auf dem Server weiter (bis zu 12 Stunden ohne offenes Fenster) und hängen sich nach einem Neuladen
  oder einem Funkloch wieder an, mit dem bisherigen Bildschirminhalt. Öffnest du dieselbe Sitzung in einem zweiten Fenster,
  zeigt das erste «In einem anderen Fenster geöffnet»; mit «Hierher holen» kommt sie dorthin zurück.
- **Hosts mit Gerätetyp:** Jeder Host bekommt ein Symbol, zum Beispiel SPS, HMI/Panel, Gateway, IoT-Gerät, Wallbox,
  Wechselrichter, Server, virtuelle Maschine, Raspberry Pi, Router, Switch, Firewall oder NAS. Dazu Ordner, Tags, Farben und Notizen.
- **Einfacher Host-Dialog:** sichtbar sind nur Gerätetyp, Name, Adresse, Benutzer, Passwort und Ordner. Die Adresse versteht
  `benutzer@host:port` und füllt Benutzer und Port selbst aus. Port, Schlüssel oder vorhandener Zugang, Jump-Host, Tags,
  Farbe, Notiz und «Dateileiste folgt» liegen unter **Erweitert**.
- **Passwort speichern:** Mit dem Haken «Passwort speichern» im Host-Dialog legt die App einen verschlüsselten Zugang im Tresor an
  (Name = Hostname). Ohne Haken fragt sie beim Verbinden nach Benutzer und Passwort. Dort speichert der Haken
  «Zugangsdaten für diesen Host speichern» die Angaben, aber erst nach erfolgreicher Anmeldung.
- **Tresor:** Zugänge (Benutzer mit Passwort oder privatem Schlüssel) liegen verschlüsselt (AES-256-GCM, Schlüssel aus dem
  Anmeldepasswort per Argon2id). Schlüssel lassen sich importieren, neu erzeugen (ed25519) und per Knopfdruck auf einem Host eintragen.
- **Host-Schlüssel:** Beim ersten Verbinden fragt die App nach dem Host-Schlüssel und warnt deutlich, wenn er sich später ändert.
  Klappt eine Verbindung nicht, steht da ein verständlicher Satz statt eines Netzwerkfehlers, zum Beispiel
  «Verbindung abgelehnt — läuft auf 10.0.0.5:22 ein SSH-Dienst?». Der Verbindungsaufbau gibt nach rund 20 Sekunden auf.
- **Dateileiste (SFTP):** Hoch- und Herunterladen, Umbenennen, Löschen, Ordner anlegen. Auf Linux-Hosts folgt sie dem
  Terminal: Nach `cd /var/log` zeigt sie `/var/log`, mit bash, zsh, fish, tcsh und anderen Shells. Versteht ein Gerät den
  Startbefehl dafür nicht (Router-CLI, Windows), öffnet die App von selbst eine normale Shell; die Dateileiste folgt dann nur,
  wenn die Shell ihren Ordner meldet (OSC 7).
- **Befehlshilfe:** rund 7000 Befehle offline (tldr-pages, Deutsch mit englischem Rückfall). Suche auch in Alltagssprache
  («archiv entpacken», «port belegt»), Erklärung der gerade getippten Zeile, Beispiele mit Platzhaltern, eigene Snippets.
- **Palette:** <kbd>Strg</kbd>+<kbd>Umschalt</kbd>+<kbd>K</kbd> öffnet die Suche über Hosts, Snippets und Befehle, von überall aus.
  <kbd>Strg</kbd>+<kbd>K</kbd> geht nur, wenn der Fokus nicht im Terminal liegt, denn dort gehört die Taste der Shell (nano, bash).
- **Telefon:** Tastenleiste mit Esc, Tab, Ctrl, Alt, Pfeilen und Sonderzeichen. Menü, Dateien und Hilfe öffnen als Blätter.
  Lässt sich als App auf den Startbildschirm legen.
- **Akzentfarbe:** Vorgabe Violett. In den Einstellungen umstellbar auf Blau, Türkis, Grün, Bernstein oder Rosa;
  die Wahl gilt für den jeweiligen Browser. Warnungen behalten ihre eigenen Farben.
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

**Gleich nach dem ersten Start erledigen.** Solange die App nicht eingerichtet ist, kann jeder, der ihre Adresse erreicht,
das Konto anlegen oder ein Backup einspielen: Wer zuerst kommt, richtet ein. Den Port deshalb erst freigeben, wenn die
Einrichtung erledigt ist.

1. Benutzername und Passwort festlegen (mindestens 10 Zeichen). Das Passwort entsperrt auch den Tresor.
2. Die App zeigt **einmal** den **Sicherheitsschlüssel** an. Kopieren oder als Datei speichern und getrennt aufbewahren,
   zum Beispiel im Passwort-Manager. Mit ihm kannst du
   - das Passwort zurücksetzen («Passwort vergessen?» auf der Anmeldeseite),
   - ein Backup einspielen, auch auf einem anderen System.

   Ohne Sicherheitsschlüssel ist ein Backup nicht lesbar.
3. Hosts anlegen: Gerätetyp wählen, Adresse und Benutzer eintragen, Passwort eingeben oder beim Verbinden fragen lassen.

Umzug auf ein neues System: dort beim ersten Start «Backup einspielen» wählen, Backup-Datei und Sicherheitsschlüssel angeben.
Danach meldest du dich mit Benutzer und Passwort des alten Systems an.

## Einstellungen (Umgebungsvariablen)

Das Image setzt einige Variablen selbst (Spalte «im Container»). Wer das Programm ohne Container startet, etwa mit
`go run`, bekommt die Vorgaben des Programms (Spalte «ohne Container»).

| Variable | Vorgabe im Container | Vorgabe ohne Container | Bedeutung |
|---|---|---|---|
| `SSHAPP_ADDR` | `:8443` | `:8443` | Adresse und Port. Im Container immer in der Form `:<port>`, die eingebaute Gesundheitsprüfung fragt `127.0.0.1:<port>` ab |
| `SSHAPP_DATA` | `/data` | `./data` | Datenordner (Datenbank, Zertifikat) |
| `SSHAPP_HILFE` | `/app/hilfe` | `<SSHAPP_DATA>/hilfe` | Ordner mit `tldr-pages.de.zip` und `tldr-pages.en.zip` |
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
und ohne `SSHAPP_HOSTS`, denn dann kennt die App den Namen nicht. Ohne Eintrag erlaubt sind IP-Adressen, `localhost`, Rechnernamen ohne Punkt und die lokalen Endungen `.local`, `.lan`,
`.home`, `.home.arpa`, `.internal`, `.intranet`, `.localdomain`, `.localhost`, `.fritz.box` und `.ts.net` (Tailscale).
Andere Namen (etwa `ssh.firma.ch`) gehören in `SSHAPP_HOSTS`.
Anfragen von einem Proxy aus `SSHAPP_PROXY` prüft die App nicht, das übernimmt der Proxy.

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
«Anmeldung von …»). Läuft der Proxy auf demselben Rechner, ist das die Gateway-Adresse des Docker-Netzes, meist aus
`172.16.0.0/12`. Liegt das eigene Netz selbst in diesem Bereich, besser nur die genaue Adresse eintragen.

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

- Anmeldung mit Sperre nach 5 Fehlversuchen (15 Minuten je Adresse, bei IPv6 je /64-Netz). Gleichzeitige Versuche zählen
  mit: Mehr als 5 laufende oder gescheiterte je Adresse weist die App sofort ab, und bei Überlast antwortet die Anmeldung
  kurz mit «bitte kurz warten». Dazu Cookies `HttpOnly` und `SameSite=Strict`, eigener Kopf gegen CSRF, strenge
  Content-Security-Policy, Schutz vor DNS-Rebinding im HTTP-Betrieb.
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
go test ./...                     # Tests inkl. Ende-zu-Ende mit eingebettetem SSH-Server
go run ./cmd/ssh-app              # startet auf https://localhost:8443, Daten in ./data
node --experimental-websocket tools/klicktest.js <url> <ssh-host> <port> <user> <pw> <fotoordner> [breite hoehe]
```

Für die Befehlshilfe in der Entwicklung `tldr-pages.de.zip` und `tldr-pages.en.zip` von
<https://github.com/tldr-pages/tldr/releases> nach `./data/hilfe/` legen (bzw. nach `<SSHAPP_DATA>/hilfe/`).
Das Image packt die Archive beim Bau selbst, aus einem festen Stand des tldr-Repositorys (`TLDR_COMMIT` im Dockerfile).

Aufbau: Go-Backend (`internal/`), eingebettete Oberfläche ohne Build-Schritt (`web/static/`), SQLite als Speicher.

## Lizenz

MIT, siehe [LICENSE](LICENSE). Drittsoftware und Daten: siehe [NOTICE](NOTICE). Die Lizenztexte aller mitgelieferten und
ins Programm übersetzten Bibliotheken sind eingebettet und in der App unter Einstellungen › Über › «Lizenzen» abrufbar.
Die Befehlshilfe stammt aus den [tldr-pages](https://github.com/tldr-pages/tldr) (CC BY 4.0).
