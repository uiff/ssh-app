package help

// synonyme erweitert Suchworte in Alltagssprache auf das Vokabular der
// tldr-Seiten (deutsch und englisch). Schluessel und Werte sind normalisiert
// (klein, Umlaute vereinfacht) und passen am Wortanfang.
var synonyme = map[string][]string{
	"entpack":       {"extrah", "extract", "entpack", "unzip", "dekomprim", "decompress"},
	"auspack":       {"extrah", "extract", "entpack"},
	"pack":          {"archiv", "archive", "kompri", "compress"},
	"archiv":        {"archiv", "archive", "tar", "zip"},
	"zip":           {"zip", "archiv", "archive", "kompri", "compress"},
	"datei":         {"datei", "file"},
	"ordner":        {"ordner", "verzeichnis", "directory", "folder", "dir"},
	"verzeichnis":   {"verzeichnis", "directory", "ordner"},
	"gross":         {"gross", "size", "large", "big", "grosse"},
	"grosse":        {"gross", "size", "large"},
	"klein":         {"klein", "small", "size"},
	"find":          {"find", "such", "search", "locate"},
	"such":          {"such", "find", "search"},
	"los":           {"losch", "delete", "remove", "entfern"},
	"losch":         {"losch", "delete", "remove", "entfern"},
	"entfern":       {"entfern", "remove", "delete", "losch"},
	"kopier":        {"kopier", "copy", "kopie"},
	"verschieb":     {"verschieb", "move", "umbenenn", "rename"},
	"umbenenn":      {"umbenenn", "rename", "move"},
	"port":          {"port", "socket", "listen", "lausch"},
	"belegt":        {"listen", "lausch", "offen", "open", "process", "prozess"},
	"offen":         {"offen", "open", "listen", "lausch"},
	"dienst":        {"dienst", "service", "unit", "daemon", "systemctl"},
	"neustart":      {"neustart", "restart", "neu", "reboot"},
	"start":         {"start", "starte"},
	"stopp":         {"stopp", "stop", "beende", "kill"},
	"beend":         {"beend", "stop", "kill", "terminat"},
	"prozess":       {"prozess", "process", "pid"},
	"speicher":      {"speicher", "memory", "disk", "space", "platz", "ram"},
	"speicherplatz": {"speicherplatz", "disk", "space", "festplatte", "frei", "free"},
	"festplatt":     {"festplatte", "disk", "laufwerk", "drive", "space", "partition"},
	"platz":         {"platz", "space", "disk", "frei", "free"},
	"voll":          {"belegt", "usage", "nutzung", "space", "gross", "size", "verfugbar", "available"},
	"frei":          {"frei", "free", "available", "verfugbar"},
	"netzwerk":      {"netzwerk", "network", "interface", "schnittstelle", "ip"},
	"ip":            {"ip", "address", "adresse", "interface"},
	"recht":         {"recht", "berechtigung", "permission", "chmod", "mode"},
	"berechtigung":  {"berechtigung", "permission", "recht", "mode"},
	"besitzer":      {"besitzer", "owner", "eigentum", "chown"},
	"benutzer":      {"benutzer", "user", "nutzer"},
	"passwort":      {"passwort", "password"},
	"log":           {"log", "protokoll", "journal"},
	"protokoll":     {"protokoll", "log", "journal"},
	"inhalt":        {"inhalt", "content", "zeig", "show", "print"},
	"zeig":          {"zeig", "show", "list", "anzeig", "display", "print"},
	"anzeig":        {"anzeig", "zeig", "show", "display", "list"},
	"herunterlad":   {"herunterlad", "download", "lade"},
	"download":      {"download", "herunterlad"},
	"text":          {"text", "string", "muster", "pattern"},
	"ersetz":        {"ersetz", "replace", "substitut"},
	"zeit":          {"zeit", "time", "date", "datum"},
	"uhr":           {"uhr", "time", "clock", "date"},
	"schluessel":    {"schlussel", "key", "ssh"},
	"schlussel":     {"schlussel", "key", "ssh"},
	"paket":         {"paket", "package", "install"},
	"installier":    {"installier", "install", "paket", "package"},
	"aktualis":      {"aktualis", "update", "upgrade"},
	"container":     {"container", "docker", "podman"},
	"cpu":           {"cpu", "prozessor", "processor", "load", "last"},
	"last":          {"last", "load", "auslastung", "usage", "cpu"},
	"sortier":       {"sortier", "sort"},
	"zahl":          {"zahl", "count", "anzahl", "number"},
	"zaehl":         {"zahl", "count", "anzahl"},
	"zahle":         {"zahl", "count", "anzahl"},
}

// erweitern liefert zu einem normalisierten Suchwort die Alternativen (inkl. sich selbst).
func erweitern(w string) []string {
	alt := []string{w}
	for laenge := len(w); laenge >= 3; laenge-- {
		if s, ok := synonyme[w[:laenge]]; ok {
			return append(alt, s...)
		}
	}
	return alt
}

// gaengig: Befehle, die auf praktisch jedem Linux-Server vorhanden sind.
// Sie stehen bei gleich guten Treffern vor Spezialwerkzeugen.
var gaengig = map[string]bool{}

func init() {
	for _, n := range []string{
		"tar", "zip", "unzip", "gzip", "gunzip", "xz", "find", "locate", "du", "df", "ncdu", "ls", "cp", "mv", "rm", "mkdir", "rmdir",
		"ln", "cat", "less", "head", "tail", "grep", "sed", "awk", "sort", "uniq", "wc", "cut", "tr", "diff", "chmod", "chown", "chgrp",
		"stat", "file", "touch", "ps", "top", "htop", "kill", "killall", "pkill", "pgrep", "free", "uptime", "uname", "hostname",
		"systemctl", "journalctl", "service", "ss", "netstat", "lsof", "fuser", "ip", "ping", "traceroute", "curl", "wget", "dig",
		"nslookup", "nc", "nmap", "ssh", "scp", "sftp", "rsync", "ssh-keygen", "ssh-copy-id", "crontab", "mount", "umount", "lsblk",
		"blkid", "fdisk", "mkfs", "fsck", "dd", "sudo", "su", "useradd", "usermod", "userdel", "passwd", "groups", "id", "who", "w",
		"last", "date", "timedatectl", "hostnamectl", "apt", "apt-get", "dpkg", "dnf", "yum", "rpm", "apk", "docker", "docker-compose",
		"ufw", "iptables", "nft", "vim", "nano", "watch", "screen", "tmux", "history", "env", "export", "echo", "printf", "xargs",
		"tee", "basename", "dirname", "realpath", "readlink", "md5sum", "sha256sum", "base64", "openssl", "reboot", "shutdown",
		"dmesg", "lscpu", "lsusb", "lspci", "vmstat", "iostat", "nproc", "tree", "jq", "git", "make",
	} {
		gaengig[n] = true
	}
}
