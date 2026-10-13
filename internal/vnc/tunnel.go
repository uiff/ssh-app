package vnc

import (
	"context"
	"errors"
	"log"
	"net"
	"strconv"

	"golang.org/x/crypto/ssh"

	"github.com/uiff/ssh-app/internal/texte"
)

// TunnelZiel ist das einzige Ziel eines VNC-Tunnels ueber SSH: der VNC-Dienst auf dem
// Host selbst (127.0.0.1:port aus Sicht des Hosts).
func TunnelZiel(port int) string { return net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) }

// Tunnel oeffnet ueber eine bestehende SSH-Verbindung einen Kanal (direct-tcpip) zum
// VNC-Dienst auf dem Host. ziel benennt den Host in Meldungen.
func Tunnel(ctx context.Context, c *ssh.Client, ziel string, port int) (net.Conn, error) {
	conn, err := c.DialContext(ctx, "tcp", TunnelZiel(port))
	if err == nil {
		return conn, nil
	}
	log.Printf("VNC-Tunnel über SSH zu %s, Port %d: %v", ziel, port, err)
	var oc *ssh.OpenChannelError
	switch {
	case ctx.Err() != nil:
		return nil, ErrAbgebrochen
	case errors.As(err, &oc) && oc.Reason == ssh.Prohibited:
		return nil, texte.Neu("vnc.tunnel.verboten", ziel)
	case errors.As(err, &oc) && oc.Reason == ssh.ConnectionFailed:
		return nil, texte.Neu("vnc.tunnel.kein.dienst", ziel, port)
	}
	return nil, texte.Neu("vnc.tunnel.fehlgeschlagen", ziel)
}
