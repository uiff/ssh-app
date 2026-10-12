<p align="right"><a href="README.md"><b>English</b></a> | <a href="README.de.md">Deutsch</a></p>

<p align="center">
  <img src="web/static/img/icon.svg" width="72" height="72" alt="">
</p>

<h1 align="center">SSH-App</h1>

<p align="center">
  A self-hosted web SSH client: terminal, files and command help in one browser tab, on the desktop and on your phone.
</p>

<p align="center">
  <a href="https://iotueli.ch"><img src="web/static/img/iotueli.svg" height="32" alt="IoTUeli"></a><br>
  <sub>Made by <a href="https://iotueli.ch">IoTUeli</a></sub>
</p>

![SSH-App on the desktop: a terminal session with the Files panel and the command help open](docs/bilder/en/desktop-arbeitsplatz.png)

Think MobaXterm, but in the browser. SSH-App runs as a single container on your own hardware, with no cloud service and
no third-party account. The interface is available in English and German.

## Features

### Terminal

- **Terminal in the browser:** tabs, 256 colors, full-screen programs (`top`, `htop`, `vi`), clickable links, copy and
  paste (<kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>C</kbd> / <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>V</kbd>).
- **Sessions survive:** sessions keep running on the server for up to 12 hours with no window open. After a reload or a
  dropped connection they reattach on their own, with the previous screen content. If you open the same session in a
  second window, the first one steps aside and can take it back with one click.
- **Clear errors:** when a connection fails you get a plain sentence instead of a network error code, for example
  “Connection refused — is an SSH server running on 10.0.0.5:22?”. The app gives up after about 20 seconds.
- **Host keys:** on the first connection the app asks you to confirm the host key, and it warns you clearly if the key
  changes later. Known host keys are listed in Settings, where you can remove them.
- **Send to all:** type into several sessions at once.
- **Critical hosts:** for hosts tagged `critical` (or `kritisch`), the app asks for confirmation before a dangerous command such as
  `rm -rf`, `dd` or `reboot` goes in from a snippet, the command help or Send to all.

### Hosts with device types

![Host dialog: device type picker with icons, then name, address, user and password](docs/bilder/en/desktop-host-dialog.png)

- **Device types with icons:** each host gets a symbol, for example PLC, HMI/panel, gateway, IoT device, wallbox,
  inverter, server, virtual machine, container, PC, laptop, Raspberry Pi, router, switch, firewall, NAS, cloud/VPS or
  printer. You can recognize your machines at a glance in the list.
- **Simple host dialog:** you only see device type, name, address, user, password and folder. The address field also
  takes `user@host:port`, `ssh://…`, `ssh -p 2222 user@host` or `[fe80::1]:2222` and fills in user and port for you.
  Port, key or vault credential, tags, color, note and “Files panel follows the terminal” are under **Advanced**.
- **Folders, tags, colors and notes.** The filter above the list searches names, addresses, users, device types and tags.
  If exactly one host matches, <kbd>Enter</kbd> connects.

### Vault and credentials

- **Save password:** with “Save password” ticked in the host dialog, the app stores the password as an encrypted
  credential in the vault. Without it, the app asks for user and password when you connect, and can save them there
  after a successful sign-in.
- **Vault:** credentials (a user with a password or a private key) are encrypted with AES-256-GCM, using a key derived
  from your sign-in password with Argon2id. Import keys, generate new ones (ed25519) and install the public key on a host
  with one click (it is appended to `~/.ssh/authorized_keys`). Several hosts can share one credential, so a new password
  only has to be changed in one place.
- **Automatic lock:** after 30 minutes without activity you are signed out and the vault locks. Running sessions keep
  going and are back after you sign in.
- **Recovery key:** shown once during setup. It resets a forgotten password and unlocks backups.

### Files

- **SFTP next to the terminal:** upload (also by drag and drop), download, rename, delete, create folders, copy a path,
  insert it into the terminal, or `cd` there.
- **Safe replace:** replacing a file on upload keeps the file itself (owner, permissions, hard links, Docker bind
  mounts); an interrupted upload leaves the original untouched. Special files (devices, pipes, sockets) can't be
  downloaded or replaced.
- **Follows the terminal:** on Linux hosts, after `cd /var/log` the Files panel shows `/var/log`, with bash, zsh, fish,
  tcsh and other shells. If a device does not understand the start command for this (router CLIs, Windows), the app
  falls back to a normal shell on its own. The panel then follows only if the shell reports its directory (OSC 7).

### Command help and search

![Search: “extract archive” finds the matching commands, with the selected example ready to insert](docs/bilder/en/desktop-suche.png)

- **Offline command help:** about 7,000 commands from [tldr-pages](https://github.com/tldr-pages/tldr), shown in the
  interface language where a translation exists and in the other language otherwise. No internet connection needed.
- **Explains what you type:** the help panel shows examples for the command on your current line and what its options
  mean. Examples have placeholders that you fill in before they go to the terminal.
- **Search in plain words:** “extract archive” or “free disk space” finds the right commands.
- **Snippets:** save your own commands, with placeholders like `journalctl -u {{service}}`. The app asks for the values
  when you insert one.
- **One search for everything:** <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>K</kbd> searches hosts, snippets and commands
  from anywhere (<kbd>Cmd</kbd>+<kbd>K</kbd> on a Mac). Plain <kbd>Ctrl</kbd>+<kbd>K</kbd> only works outside the
  terminal, because inside it the key belongs to the shell (nano, bash).

### On your phone

<table>
  <tr>
    <td align="center"><img src="docs/bilder/en/telefon-terminal.png" width="300" alt="Terminal on a phone, with the key bar for Esc, Tab, Ctrl, Alt and arrows above the keyboard"></td>
    <td align="center"><img src="docs/bilder/en/telefon-hosts.png" width="300" alt="Host list on a phone, each host with its device icon"></td>
  </tr>
</table>

- **Key bar** with Esc, Tab, Ctrl, Alt, arrows, Home/End, Page Up/Down, <kbd>Ctrl</kbd>+<kbd>C</kbd>/<kbd>D</kbd>/<kbd>Z</kbd>/<kbd>R</kbd>,
  common shell symbols, and paste and copy.
- **Built for touch:** the menu, Files and the command help open as sheets, and every touch target is at least 44 px.
- **Home screen:** add SSH-App to your home screen and it runs like an app.

### Language and look

- **English and German:** the interface follows your browser language. You can switch it on the sign-in page, in the
  menu or in Settings › Appearance. The page reloads and running terminals reconnect on their own. Messages from the
  server, such as connection errors, come in the same language.
- **Accent color:** violet by default, or blue, teal, green, amber or pink. The choice is saved per browser. Warnings
  keep their own colors.
- **Terminal font size** can be set per browser.

### Backup

- **One encrypted file** with all hosts, folders, credentials, snippets and known host keys. You can restore it on this
  system or on another one with the recovery key.

## Quick start

```bash
git clone https://github.com/uiff/ssh-app.git
cd ssh-app
docker compose up -d --build
```

Then open `https://<address-of-the-machine>:8443` in your browser and **set it up right away** (see below). On first
start the app creates its own certificate, so the browser warns you once. That is expected. To compare the
certificate fingerprint, run `docker logs ssh-app`.

Without Compose:

```bash
docker build -t ssh-app https://github.com/uiff/ssh-app.git
docker run -d --name ssh-app -p 8443:8443 -v ssh-app-daten:/data --restart unless-stopped ssh-app
```

The Compose file also hardens the container (read-only file system, no Linux capabilities). With `docker run` you get
the same with `--read-only --tmpfs /tmp --cap-drop ALL --security-opt no-new-privileges:true`.

Instead of a volume you can use a folder on the host (`-v /path/to/data:/data`). It must belong to the user the app runs
as inside the container: `sudo chown 65532:65532 /path/to/data`. Otherwise the app stops at startup with a hint in the log.

### Raspberry Pi and other platforms

The image builds for `linux/amd64` and `linux/arm64` (for example a Raspberry Pi or an industrial controller). The
program is cross-compiled, so no emulator (QEMU) is needed.

- **On the device itself:** `docker compose up -d --build` as above. The platform is picked automatically.
- **On another machine, for one device** (one platform, works with the default builder):

  ```bash
  docker buildx build --platform linux/arm64 -t ssh-app --load .
  docker save ssh-app | gzip > ssh-app-arm64.tar.gz
  ```

  Copy the file to the device, run `docker load -i ssh-app-arm64.tar.gz` there and start the container with `docker run`
  as above. The Compose file works too: `docker compose up -d` (without `--build`) uses the loaded image `ssh-app:latest`.
- **Several platforms in one image:** this needs a buildx builder with the `docker-container` driver. The default
  builder (`docker` driver) refuses multiple platforms unless the containerd image store is turned on. A multi-platform
  image also cannot be loaded into the local image store. Push it to a registry instead:

  ```bash
  docker buildx create --name ssh-app-bau --driver docker-container --use   # once
  docker buildx build --platform linux/amd64,linux/arm64 -t <registry>/ssh-app:<version> --push .
  ```

  Without `--push` (or `--output`) the result only stays in the build cache.

## First-time setup

![Setup: the recovery key is shown once, with buttons to copy it or save it as a file](docs/bilder/en/einrichten.png)

**Do this right after the first start.** Until the app is set up, anyone who can reach its address can create the
account or restore a backup. Whoever comes first sets it up. So only open the port to others once setup is done.

1. Choose a user name and a password (at least 10 characters). The password also unlocks the vault.
2. The app shows the **recovery key** only **once**. Copy it or save it as a file, and keep it separate from the app, for
   example in your password manager. With it you can
   - reset the password (“Forgot your password?” on the sign-in page),
   - restore a backup, also on another system.

   Without the recovery key, a backup cannot be read.
3. Add hosts: pick a device type, enter address and user, then type the password or let the app ask when you connect.

## Backup and moving to a new system

- **Download backup:** Settings › Backup. You get one encrypted file. Passwords and keys inside stay encrypted.
- **Move to a new system:** on the first start there, choose **Restore backup** and enter the backup file and the
  recovery key. Then sign in with the user name and password of the old system.
- **Restore on a running system:** Settings › Backup › **Restore backup** replaces all hosts, credentials and snippets
  on this system.
- **Changing the password** keeps the recovery key valid.

## Configuration (environment variables)

The image sets some variables itself (column “Container”). If you run the program without a container, for example
with `go run`, the program defaults apply (column “Without container”).

| Variable | Container | Without container | Meaning |
|---|---|---|---|
| `SSHAPP_ADDR` | `:8443` | `:8443` | Address and port. In the container always as `:<port>`, because the built-in health check queries `127.0.0.1:<port>` |
| `SSHAPP_DATA` | `/data` | `./data` | Data folder (database, certificate) |
| `SSHAPP_HILFE` | `/app/hilfe` | `<SSHAPP_DATA>/hilfe` | Folder with `tldr-pages.de.zip` and `tldr-pages.en.zip` (command help) |
| `SSHAPP_TLS` | `auto` | `auto` | `auto` = self-signed certificate, `aus` (“off”) = plain HTTP (only behind a reverse proxy), `eigen` (“own”) = your own certificate from `SSHAPP_TLS_CERT` and `SSHAPP_TLS_KEY` |
| `SSHAPP_TLS_CERT` | – | – | With `SSHAPP_TLS=eigen`: path to the certificate (PEM, including intermediate certificates) |
| `SSHAPP_TLS_KEY` | – | – | With `SSHAPP_TLS=eigen`: path to the private key (PEM). In the container it must be readable by user 65532 |
| `SSHAPP_COOKIE_SECURE` | – | – | `1` = send the cookie over HTTPS only (set this with `SSHAPP_TLS=aus` behind an HTTPS proxy) |
| `SSHAPP_PROXY` | – | – | Addresses or networks of trusted reverse proxies, comma-separated (e.g. `172.16.0.0/12` or `10.0.0.5, fd00::/8`). Only for requests from there does the app use `X-Forwarded-For` (sign-in lockout per browser instead of per proxy) and `X-Forwarded-Host` (terminal WebSocket) |
| `SSHAPP_HOSTS` | – | – | Additional host names for the app, comma-separated; `*.example.com` covers all subdomains (DNS rebinding protection, see below). When set, the app always checks the name, also over HTTPS |
| `SSHAPP_ORIGINS` | – | – | Additional allowed origins (`Origin`) for the terminal WebSocket, comma-separated, patterns like `https://*.example.com`. Only needed if a proxy changes the Host header |
| `SSHAPP_HSTS` | – | – | `1` = send Strict-Transport-Security: browsers then use only HTTPS for this name for one year, on all ports. Only set it if everything under this name speaks HTTPS |
| `SSHAPP_LEERLAUF_MIN` | `30` | `30` | Minutes without activity until the sign-in expires and the vault locks. Activity means typing in the terminal and actions in the app; background polling does not count. Running sessions stay alive and can be reopened after signing in |

**DNS rebinding protection:** a malicious website could point its own name at the app’s address. That is why the app
checks the name it is called by in HTTP mode (`SSHAPP_TLS=aus`), and always as soon as `SSHAPP_HOSTS` is set. The only
exception is running behind an HTTPS proxy (`SSHAPP_COOKIE_SECURE=1`) without `SSHAPP_PROXY` and without
`SSHAPP_HOSTS`, because then the app does not know the name. Allowed without any setting are IP addresses, `localhost`,
host names without a dot and the local suffixes `.local`, `.lan`, `.home`, `.home.arpa`, `.internal`, `.intranet`,
`.localdomain`, `.localhost`, `.fritz.box` and `.ts.net` (Tailscale). Other names (such as `ssh.example.com`) go into
`SSHAPP_HOSTS`. Requests from a proxy listed in `SSHAPP_PROXY` are not checked; the proxy does that.

## Running behind a reverse proxy

With a proxy (Caddy, nginx, Traefik) and a real certificate, the certificate warning goes away. The app then speaks
plain HTTP and is reachable only by the proxy. In `compose.yml`:

```yaml
    ports:
      - "127.0.0.1:8080:8080"     # only for the proxy on the same machine
    environment:
      SSHAPP_ADDR: ":8080"
      SSHAPP_TLS: "aus"
      SSHAPP_COOKIE_SECURE: "1"   # cookie over HTTPS only
      SSHAPP_PROXY: "172.16.0.0/12"  # how the app sees a proxy on the same machine (Docker networks)
```

The address the app sees the proxy at appears in the log after a sign-in attempt (`docker logs ssh-app`, line
`Anmeldung von …`; the log is in German). If the proxy
runs on the same machine, it is the gateway address of the Docker network, usually from `172.16.0.0/12`. If your own
network is in that range too, enter only the exact address.

What the proxy has to do:

- **Pass WebSockets through** (terminal at `/api/ws`). The browser sends a keepalive every 25 seconds, so the usual
  60-second timeouts are enough.
- **Pass the Host header through unchanged, including the port** (or set `X-Forwarded-Host`; that only counts if the
  proxy is listed in `SSHAPP_PROXY`). The app only accepts a WebSocket if `Origin` and `Host` match. Otherwise the page
  opens, but no terminal does.
- **Set `X-Forwarded-For`**, so the sign-in lockout hits the individual browser and not everyone.
- **Allow large uploads.** The Files panel uploads files of any size, and a backup may be up to 64 MB.

Caddy does all of this out of the box:

```
ssh.example.lan {
    reverse_proxy 127.0.0.1:8080
}
```

nginx needs a few more lines:

```nginx
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 443 ssl;
    server_name ssh.example.lan;
    # ssl_certificate and ssl_certificate_key as usual

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

The app counts failed sign-ins per sender address. Behind a proxy it only sees the proxy’s address, unless the proxy is
listed in `SSHAPP_PROXY`: then it takes the browser’s address from `X-Forwarded-For`. Without `SSHAPP_PROXY`, sign-in is
locked for everyone for 15 minutes after 5 failed attempts, not just for the sender. The app points this out in the log
at startup.

## Security

- Sign-in is locked for 15 minutes after 5 failed attempts from one address. IPv6 addresses from other networks also
  count together per /64 network (limit 30), because one connection there has many addresses. Attempts still in
  progress count too, so guessing in parallel runs into the same limit, and under heavy load sign-in briefly asks you to
  wait. Cookies are `HttpOnly` and `SameSite=Strict`, there is a custom header against CSRF, a strict Content Security
  Policy, and DNS rebinding protection in HTTP mode.
- Passwords and private keys are stored only encrypted in the database. The key for them lives only in memory and is
  discarded when the vault locks (sign-out or inactivity). Copies that Go makes on its own (such as the password you
  typed or internal buffers) cannot be wiped reliably. Anyone who can read the process memory (root, core dump, swap) may
  find them. To connect, the server has to decrypt a secret briefly. End-to-end encryption all the way to the browser is
  not possible for an SSH gateway.
- The image contains no shell and runs as an unprivileged user (65532). The Compose file also sets a read-only file
  system and drops all Linux capabilities.
- Setup is open until someone completes it. So set the app up right after starting it (see above).
- The app is meant for your own network. Do not expose it to the internet unprotected.

## Development

```bash
go test ./...          # tests, including end-to-end tests with an embedded SSH server
go run ./cmd/ssh-app   # starts on https://localhost:8443, data in ./data
node --experimental-websocket tools/klicktest.js <url> <ssh-host> <port> <user> <password> <photo-folder> [width height]
```

`tools/klicktest.js` clicks through the interface in headless Chrome and takes photos along the way. With `-` as
`<ssh-host>` it tests the interface without an SSH target. `SPRACHE=de|en` picks the interface language;
`DATEIEN_ORDNER=<dir>` also tests the Files panel (only with the embedded test SSH server:
`SSHAPP_TESTSSH_HALTEN=<seconds> go test ./internal/server -run 'TestSSHHalten$' -v` prints its address).

For the command help during development, put `tldr-pages.de.zip` and `tldr-pages.en.zip` from
<https://github.com/tldr-pages/tldr/releases> into `./data/hilfe/` (or `<SSHAPP_DATA>/hilfe/`). The image packs these
archives itself during the build, from a pinned commit of the tldr repository (`TLDR_COMMIT` in the Dockerfile).

Structure: Go backend (`internal/`), embedded interface without a build step (`web/static/`), SQLite for storage.

Translations: each text exists in German and English.

- Browser: catalogs in `web/static/js/sprache/<area>.js` (`i18nErgaenzen({ de: {…}, en: {…} })`). The code uses
  `t('area.key', { name })`.
- Server: catalogs in `internal/texte/<area>.go` (`texte.Eintragen(de, en)`), used with `texte.T(language, key, …)`.
  `internal/texte/texte_test.go` checks that every key has both languages with the same placeholders.
- The browser sends its language with every request (header `X-SSH-App-Sprache`, parameter `sprache` for the WebSocket
  and downloads).
- Guard tests: `web/sprache_test.go` (every key used in the browser code exists in both languages, no unused keys, no
  German text outside the catalogs, no emoji) and `internal/server/rohtext_test.go` (no text literals in HTTP and
  WebSocket responses, every key used in the Go code exists with matching placeholders).

## License

MIT, see [LICENSE](LICENSE). Third-party software and data: see [NOTICE](NOTICE). The license texts of all bundled and
compiled-in libraries are embedded and can be viewed in the app under Settings › About › Licenses. The command help
comes from [tldr-pages](https://github.com/tldr-pages/tldr) (CC BY 4.0). The name and logo “IoTUeli” are trademarks of
IoTUeli and are not covered by the MIT License.
