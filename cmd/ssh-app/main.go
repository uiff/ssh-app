// ssh-app: Web-SSH-Client mit Tresor, Dateileiste und Befehlshilfe.
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/uiff/ssh-app/internal/help"
	"github.com/uiff/ssh-app/internal/server"
	"github.com/uiff/ssh-app/internal/store"
	"github.com/uiff/ssh-app/internal/term"
	"github.com/uiff/ssh-app/internal/vault"
)

var version = "0.1.0"

func env(k, standard string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return standard
}

func main() {
	log.SetFlags(log.LstdFlags)
	daten := env("SSHAPP_DATA", "./data")
	adresse := env("SSHAPP_ADDR", ":8443")
	tlsArt := env("SSHAPP_TLS", "auto") // auto | aus | eigen
	hilfeVerz := env("SSHAPP_HILFE", filepath.Join(daten, "hilfe"))
	leerlaufMin, _ := strconv.Atoi(env("SSHAPP_LEERLAUF_MIN", "30"))

	// «ssh-app gesund» fuer den Docker-Healthcheck (das Image hat weder Shell noch curl).
	if len(os.Args) > 1 && os.Args[1] == "gesund" {
		os.Exit(gesund(adresse, tlsArt != "aus"))
	}

	if err := os.MkdirAll(daten, 0o700); err != nil {
		log.Fatalf("Datenordner: %v", err)
	}
	st, err := store.Oeffnen(filepath.Join(daten, "ssh-app.db"))
	if err != nil {
		log.Fatalf("Datenbank: %v", err)
	}
	kopf, err := st.Meta(server.MetaTresor)
	if err != nil {
		log.Fatalf("Tresor lesen: %v", err)
	}
	tr, err := vault.Neu(kopf)
	if err != nil {
		log.Fatalf("Tresor: %v", err)
	}
	hilfe, err := help.Laden(hilfeVerz)
	if err != nil {
		log.Printf("Befehlshilfe: %v", err)
	}
	if hilfe.Anzahl() == 0 {
		log.Printf("Befehlshilfe: keine tldr-Archive in %s gefunden (tldr-pages.de.zip / tldr-pages.en.zip)", hilfeVerz)
	} else {
		log.Printf("Befehlshilfe: %d Befehle (%s)", hilfe.Anzahl(), hilfe.Quelle)
	}
	tm := term.NeuerManager()

	var tlsCfg *tls.Config
	switch tlsArt {
	case "aus":
	case "eigen":
		cert, err := tls.LoadX509KeyPair(env("SSHAPP_TLS_CERT", ""), env("SSHAPP_TLS_KEY", ""))
		if err != nil {
			log.Fatalf("Zertifikat laden (SSHAPP_TLS_CERT/SSHAPP_TLS_KEY): %v", err)
		}
		tlsCfg = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	default:
		cert, fp, err := selbstsigniert(filepath.Join(daten, "tls"))
		if err != nil {
			log.Fatalf("Zertifikat erzeugen: %v", err)
		}
		log.Printf("HTTPS mit eigenem Zertifikat, Fingerabdruck SHA-256 %s", fp)
		tlsCfg = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	}

	srv := server.Neu(st, tr, tm, hilfe, server.Optionen{
		Version:        version,
		Leerlauf:       time.Duration(leerlaufMin) * time.Minute,
		SicheresCookie: tlsCfg != nil || env("SSHAPP_COOKIE_SECURE", "") == "1",
	})
	hs := &http.Server{Addr: adresse, Handler: srv, TLSConfig: tlsCfg,
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second,
		ErrorLog: log.New(filterLog{}, "", 0)}

	go func() {
		ziel := "http"
		if tlsCfg != nil {
			ziel = "https"
		}
		log.Printf("ssh-app %s läuft auf %s://%s", version, ziel, anzeigeAdresse(adresse))
		if tlsCfg != nil {
			err = hs.ListenAndServeTLS("", "")
		} else {
			err = hs.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Printf("Beende …")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hs.Shutdown(ctx)
	srv.Close()
	tm.Close()
	st.Close()
}

func anzeigeAdresse(a string) string {
	if strings.HasPrefix(a, ":") {
		return "0.0.0.0" + a
	}
	return a
}

// filterLog unterdrueckt die TLS-Handshake-Meldungen, die jeder Browser beim
// ersten Besuch mit eigenem Zertifikat ausloest.
type filterLog struct{}

func (filterLog) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "TLS handshake error") {
		return len(p), nil
	}
	return os.Stderr.Write(p)
}

// selbstsigniert erzeugt (einmalig) ein Zertifikat und laedt es danach wieder.
func selbstsigniert(verz string) (tls.Certificate, string, error) {
	certPfad, keyPfad := filepath.Join(verz, "cert.pem"), filepath.Join(verz, "key.pem")
	if c, err := tls.LoadX509KeyPair(certPfad, keyPfad); err == nil {
		return c, fingerabdruck(c), nil
	}
	if err := os.MkdirAll(verz, 0o700); err != nil {
		return tls.Certificate{}, "", err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	host, _ := os.Hostname()
	vorlage := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "ssh-app", Organization: []string{"ssh-app (selbst signiert)"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost", host},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, vorlage, vorlage, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	kb, _ := x509.MarshalECPrivateKey(key)
	if err := os.WriteFile(keyPfad, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		return tls.Certificate{}, "", err
	}
	if err := os.WriteFile(certPfad, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return tls.Certificate{}, "", err
	}
	c, err := tls.LoadX509KeyPair(certPfad, keyPfad)
	return c, fingerabdruck(c), err
}

func fingerabdruck(c tls.Certificate) string {
	if len(c.Certificate) == 0 {
		return ""
	}
	sum := sha256.Sum256(c.Certificate[0])
	teile := make([]string, len(sum))
	for i, b := range sum {
		teile[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(teile, ":")
}

// gesund fragt den eigenen Status-Endpunkt ab: 0 = laeuft, 1 = nicht erreichbar.
func gesund(adresse string, mitTLS bool) int {
	_, port, err := net.SplitHostPort(adresse)
	if err != nil {
		return 1
	}
	schema := "http"
	if mitTLS {
		schema = "https"
	}
	c := &http.Client{Timeout: 4 * time.Second, Transport: &http.Transport{
		// Eigenes, selbstsigniertes Zertifikat; es geht nur an 127.0.0.1.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	}}
	res, err := c.Get(schema + "://127.0.0.1:" + port + "/api/status")
	if err != nil {
		return 1
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
