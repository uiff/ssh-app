package help

import "strings"

// synonyme erweitert Suchworte in Alltagssprache auf das Vokabular der
// tldr-Seiten. Die Suche geht in beide Richtungen: deutsche Worte finden
// englische Seiten und umgekehrt (die Oberflaeche zeigt je nach Sprache die
// deutsche oder die englische Seite). Schluessel und Werte sind normalisiert
// (klein, Umlaute vereinfacht, «ue» zu «u») und passen am Wortanfang; der
// laengste passende Schluessel gewinnt.
var synonyme = map[string][]string{
	// --- Deutsch ---
	"entpack":       {"extrah", "extract", "entpack", "unzip", "dekomprim", "decompress"},
	"auspack":       {"extrah", "extract", "entpack"},
	"pack":          {"archiv", "archive", "kompri", "compress"},
	"archiv":        {"archiv", "archive", "tar", "zip"},
	"zip":           {"zip", "archiv", "archive", "kompri", "compress"},
	"gzip":          {"gzip"}, // «gzipped» (Merkhilfe g[z]ipped) → «gzip»
	"datei":         {"datei", "file"},
	"ordner":        {"ordner", "verzeichnis", "director", "folder", "dir"},
	"verzeichnis":   {"verzeichnis", "director", "ordner"},
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
	"neustart":      {"neustart", "restart", "reboot"}, // «neu starten» wird vorher zu «neustart»
	"herunterfahr":  {"herunterfahr", "shutdown", "shut down", "poweroff", "power off", "halt"},
	"ausschalt":     {"ausschalt", "shutdown", "shut down", "poweroff", "power off"},
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
	"inhalt":        {"inhalt", "content"},
	"zeig":          {"zeig", "show", "list", "anzeig", "display", "print", "view"},
	"anzeig":        {"anzeig", "zeig", "show", "display", "list", "print", "view"},
	"anseh":         {"anseh", "anzeig", "zeig", "show", "display", "view", "print"},
	"anschau":       {"anschau", "anzeig", "zeig", "show", "display", "view", "print"},
	"ausgeb":        {"ausgeb", "print", "show", "display", "zeig", "anzeig"},
	"herunterlad":   {"herunterlad", "download"},
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
	"extrah":        {"extrah", "extract", "entpack", "unpack"},
	"kompri":        {"kompri", "compress", "pack", "archiv", "archive"},
	"offn":          {"offn", "open"},
	"verbind":       {"verbind", "connect"},
	"bearbeit":      {"bearbeit", "edit", "editier"},
	"erstell":       {"erstell", "create", "anleg", "generier", "generat"},
	"anleg":         {"anleg", "erstell", "create"},
	"geplant":       {"geplant", "zeitplan", "schedul", "cron"},
	"zeitplan":      {"zeitplan", "geplant", "schedul", "cron"},
	"aufgab":        {"aufgab", "task", "job"},
	"ausfuhrlich":   {"ausfuhrlich", "verbos"},
	"versteck":      {"versteck", "hidden"},
	"rekursiv":      {"rekursiv", "recurs"},
	"hochlad":       {"hochlad", "upload"},
	"grupp":         {"grupp", "group"},
	"zeil":          {"zeil", "line"},
	"wort":          {"wort", "word"},
	"leer":          {"leer", "empty"},
	"hinzufug":      {"hinzufug", "add"},
	"ander":         {"ander", "change", "setz"},

	// --- Englisch ---
	"extract":    {"extract", "extrah", "entpack", "unpack", "unzip", "decompress", "dekomprim"},
	"unpack":     {"unpack", "extract", "extrah", "entpack", "decompress"},
	"decompress": {"decompress", "dekomprim", "extract", "extrah", "entpack"},
	"unzip":      {"unzip", "extract", "extrah", "entpack"},
	"compress":   {"compress", "kompri", "pack", "archiv", "archive", "zip"},
	"file":       {"file", "datei"},
	"folder":     {"folder", "director", "ordner", "verzeichnis", "dir"},
	"dir":        {"dir", "director", "folder", "ordner", "verzeichnis"},
	"size":       {"size", "gross", "grosse"},
	"large":      {"large", "big", "size", "gross"},
	"big":        {"big", "large", "size", "gross"},
	"small":      {"small", "klein", "size"},
	"search":     {"search", "find", "such", "locate"},
	"locate":     {"locate", "find", "search", "such"},
	"delete":     {"delete", "remove", "losch", "entfern"},
	"remove":     {"remove", "delete", "entfern", "losch"},
	"copy":       {"copy", "kopier", "kopie"},
	"move":       {"move", "rename", "verschieb", "umbenenn"},
	"rename":     {"rename", "move", "umbenenn"},
	"listen":     {"listen", "port", "socket", "lausch", "offen", "open"},
	"open":       {"open", "offen", "offn", "listen"},
	"service":    {"service", "dienst", "unit", "daemon", "systemctl"},
	"restart":    {"restart", "neustart", "reboot"},
	"reboot":     {"reboot", "neustart", "restart"},
	"stop":       {"stop", "stopp", "beend", "kill", "terminat", "anhalt"},
	"kill":       {"kill", "stop", "beend", "terminat"},
	"process":    {"process", "prozess", "pid"},
	"processor":  {"processor", "prozessor", "cpu"},
	"memory":     {"memory", "speicher", "ram", "arbeitsspeicher"},
	"ram":        {"ram", "memory", "speicher"},
	"disk":       {"disk", "festplatt", "laufwerk", "drive", "partition", "space"},
	"drive":      {"drive", "laufwerk", "disk", "festplatt"},
	"space":      {"space", "platz", "speicherplatz", "disk", "frei", "free"},
	"full":       {"usage", "nutzung", "belegt", "space", "size", "available", "verfugbar"},
	"free":       {"free", "frei", "available", "verfugbar"},
	"available":  {"available", "verfugbar", "frei", "free"},
	"usage":      {"usage", "nutzung", "auslastung", "belegt"},
	"network":    {"network", "netzwerk", "interface", "schnittstelle", "ip"},
	"interface":  {"interface", "schnittstelle", "netzwerk", "network"},
	"address":    {"address", "adress", "ip"},
	"permission": {"permission", "berechtigung", "recht", "mode", "chmod"},
	"owner":      {"owner", "besitzer", "eigentum", "chown"},
	"user":       {"user", "benutzer", "nutzer"},
	"group":      {"group", "grupp"},
	"password":   {"password", "passwort"},
	"content":    {"content", "inhalt"},
	"show":       {"show", "display", "list", "print", "zeig", "anzeig"},
	"display":    {"display", "show", "list", "zeig", "anzeig"},
	"list":       {"list", "show", "zeig", "auflist", "anzeig"},
	"print":      {"print", "show", "display", "ausgeb", "zeig"},
	"upload":     {"upload", "hochlad"},
	"replace":    {"replace", "ersetz", "substitut"},
	"time":       {"time", "zeit", "date", "datum", "clock", "uhr"},
	"date":       {"date", "datum", "time", "zeit"},
	"clock":      {"clock", "uhr", "time", "zeit"},
	"key":        {"key", "schlussel"},
	"package":    {"package", "paket", "install"},
	"install":    {"install", "installier", "paket", "package"},
	"update":     {"update", "upgrade", "aktualis"},
	"upgrade":    {"upgrade", "update", "aktualis"},
	"count":      {"count", "zahl", "anzahl", "number"},
	"number":     {"number", "zahl", "anzahl", "count"},
	"sort":       {"sort", "sortier"},
	"load":       {"load", "last", "auslastung", "cpu"},
	"connect":    {"connect", "verbind"},
	"edit":       {"edit", "bearbeit", "editier"},
	"create":     {"create", "erstell", "anleg"},
	"hidden":     {"hidden", "versteck"},
	"recurs":     {"recurs", "rekursiv"},
	"line":       {"line", "zeil"},
	"word":       {"word", "wort"},
	"empty":      {"empty", "leer"},
	"change":     {"change", "ander", "setz"},
	"modif":      {"modif", "change", "ander"},
	"view":       {"view", "show", "display", "zeig", "anzeig", "anseh"},
	"shutdown":   {"shutdown", "shut down", "herunterfahr", "poweroff", "power off"},
	"poweroff":   {"poweroff", "power off", "shutdown", "herunterfahr"},
	"schedul":    {"schedul", "geplant", "zeitplan", "cron"},
	"task":       {"task", "job", "aufgab"},
	"job":        {"job", "task", "aufgab"},
	"verbos":     {"verbos", "ausfuhrlich"},
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

// kernbefehle: Alltagsworte, die fast immer einen der gaengigen Befehle meinen («löschen» → rm,
// «neu starten» → systemctl/reboot). Passt eine Seite ohnehin zur Suche, bekommt sie je solchem
// Suchwort kernBonus Punkte — so stehen rm, cp, tar, systemctl vor Seiten, die das Wort nur
// nebenbei enthalten. Schluessel wie bei synonyme (normalisiert, laengster Wortanfang gewinnt).
var kernbefehle = map[string][]string{
	// --- Deutsch: Taetigkeiten ---
	"losch":        {"rm", "rmdir"},
	"entfern":      {"rm", "rmdir"},
	"kopier":       {"cp", "rsync", "scp"},
	"verschieb":    {"mv"},
	"umbenenn":     {"mv"},
	"entpack":      {"tar", "unzip"},
	"auspack":      {"tar", "unzip"},
	"extrah":       {"tar", "unzip"},
	"pack":         {"tar", "zip", "gzip"},
	"kompri":       {"tar", "gzip", "zip"},
	"archiv":       {"tar", "zip"},
	"neustart":     {"reboot", "systemctl"},
	"herunterfahr": {"shutdown", "poweroff"},
	"ausschalt":    {"shutdown", "poweroff"},
	"anleg":        {"mkdir", "useradd", "touch"},
	"erstell":      {"mkdir", "useradd", "touch"},
	"anzeig":       {"cat", "less", "ls"},
	"zeig":         {"cat", "less", "ls"},
	"anseh":        {"cat", "less", "journalctl"},
	"anschau":      {"cat", "less", "journalctl"},
	"ausgeb":       {"cat", "echo"},
	"such":         {"find", "grep", "locate"},
	"find":         {"find", "grep", "locate"},
	"beend":        {"kill", "pkill", "killall"},
	"stopp":        {"systemctl", "kill"},
	"start":        {"systemctl"},
	"installier":   {"apt", "apt-get", "dnf"},
	"aktualis":     {"apt", "apt-get", "dnf"},
	"herunterlad":  {"wget", "curl"},
	"hochlad":      {"scp", "rsync", "curl"},
	"verbind":      {"ssh"},
	"hinzufug":     {"useradd"},
	"bearbeit":     {"nano", "vim"},
	"ersetz":       {"sed"},
	"sortier":      {"sort"},
	"auflist":      {"ls"},
	"auslast":      {"top", "htop", "uptime"},
	"zahl":         {"wc"},
	"zahle":        {"wc"},
	// --- Deutsch: Dinge ---
	"dienst":          {"systemctl", "journalctl"},
	"log":             {"journalctl", "tail"},
	"login":           {"last", "who"},
	"protokoll":       {"journalctl", "tail"},
	"prozess":         {"ps", "kill", "top"},
	"festplatt":       {"df", "du", "lsblk"},
	"speicherplatz":   {"df", "du"},
	"platz":           {"df", "du"},
	"arbeitsspeicher": {"free"},
	"cpu":             {"top", "htop", "lscpu"},
	"recht":           {"chmod"},
	"berechtigung":    {"chmod"},
	"besitzer":        {"chown"},
	"paket":           {"apt", "apt-get", "dnf"},
	"geplant":         {"crontab"},
	"zeitplan":        {"crontab"},
	"port":            {"ss", "lsof", "netstat"},
	"text":            {"grep", "sed"},
	"ip":              {"ip"},
	"adress":          {"ip"},

	// --- Englisch ---
	"delete":     {"rm", "rmdir"},
	"remove":     {"rm", "rmdir"},
	"copy":       {"cp", "rsync", "scp"},
	"move":       {"mv"},
	"rename":     {"mv"},
	"extract":    {"tar", "unzip"},
	"unpack":     {"tar", "unzip"},
	"decompress": {"tar", "unzip", "gunzip"},
	"compress":   {"tar", "gzip", "zip"},
	"restart":    {"systemctl", "reboot"},
	"reboot":     {"reboot", "systemctl"},
	"shutdown":   {"shutdown", "poweroff"},
	"shut":       {"shutdown", "poweroff"},
	"create":     {"mkdir", "useradd", "touch"},
	"show":       {"cat", "less", "ls"},
	"view":       {"cat", "less", "journalctl"},
	"display":    {"cat", "less", "ls"},
	"print":      {"cat", "echo"},
	"list":       {"ls"},
	"content":    {"cat", "ls"},
	"search":     {"find", "grep", "locate"},
	"locate":     {"find", "locate"},
	"kill":       {"kill", "pkill", "killall"},
	"terminat":   {"kill", "pkill", "killall"},
	"stop":       {"systemctl", "kill"},
	"install":    {"apt", "apt-get", "dnf"},
	"update":     {"apt", "apt-get", "dnf"},
	"upgrade":    {"apt", "apt-get", "dnf"},
	"download":   {"wget", "curl"},
	"upload":     {"scp", "rsync", "curl"},
	"connect":    {"ssh"},
	"add":        {"useradd"},
	"edit":       {"nano", "vim"},
	"replace":    {"sed"},
	"count":      {"wc"},
	"service":    {"systemctl", "journalctl"},
	"process":    {"ps", "kill", "top"},
	"disk":       {"df", "du", "lsblk"},
	"space":      {"df", "du"},
	"memory":     {"free"},
	"load":       {"uptime", "top"},
	"permission": {"chmod"},
	"owner":      {"chown"},
	"package":    {"apt", "apt-get", "dnf"},
	"schedul":    {"crontab"},
	"cron":       {"crontab"},
	"address":    {"ip"},
}

// kernFuer: Bonus je Befehl fuer die Suchworte. Der erste Befehl einer Liste ist der
// naheliegendste und bekommt den vollen kernBonus, jeder weitere 10 Punkte weniger.
func kernFuer(worte []string) map[string]int {
	bonus := map[string]int{}
	for _, w := range worte {
		for laenge := len(w); laenge >= 2; laenge-- {
			if namen, ok := kernbefehle[w[:laenge]]; ok {
				for i, n := range namen {
					bonus[n] += kernBonus - 10*i
				}
				break
			}
		}
	}
	return bonus
}

// wendungen fasst Wortgruppen zusammen, die nur zusammen etwas bedeuten («neu starten»).
// q ist normalisiert.
var wendungen = strings.NewReplacer(
	"neu starten", "neustart", "neu start", "neustart", "neu booten", "neustart",
	"herunter fahren", "herunterfahren",
).Replace

// fuellwoerter zaehlen bei der Suche nicht mit («text in datei suchen»), normalisiert.
var fuellwoerter = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`in im an am auf aus von vom mit fur bei zu zum zur und oder der die das den dem des
		ein eine einen einem einer wie ich mein meine meinen
		a an the of to on for from with and or my how i`) {
		fuellwoerter[w] = true
	}
}

// ohneFuellwoerter laesst Fuellwoerter weg, solange danach noch ein Wort bleibt.
func ohneFuellwoerter(worte []string) []string {
	var out []string
	for _, w := range worte {
		if !fuellwoerter[w] {
			out = append(out, w)
		}
	}
	if len(out) == 0 {
		return worte
	}
	return out
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
