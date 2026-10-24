package server

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// HTTP bootstrapping exposes only public trust material. It never carries
// credentials, transfer offers, user filenames or control commands. Trust must
// be checked against the fingerprint displayed physically on the host.
func (s *Server) startSetup() error {
	ln, e := net.Listen("tcp", ":0")
	if e != nil {
		return e
	}
	port := ln.Addr().(*net.TCPAddr).Port
	for _, ip := range localIPs() {
		if !ip.IsLoopback() {
			s.setupURLs = append(s.setupURLs, "http://"+net.JoinHostPort(ip.String(), strconv.Itoa(port))+"/setup")
		}
	}
	s.setupURLs = append(s.setupURLs, "http://localhost:"+strconv.Itoa(port)+"/setup")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'")
		if !s.validHost(r.Host) || r.Method != "GET" {
			http.Error(w, "not available", 403)
			return
		}
		if s.serveCertificate(w, r) {
			return
		}
		switch r.URL.Path {
		case "/certificate.pem":
			w.Header().Set("Content-Type", "application/x-pem-file")
			w.Header().Set("Content-Disposition", `attachment; filename="AirLink-certificate.pem"`)
			_, _ = w.Write(s.certificate)
		case "/setup", "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, fmt.Sprintf(`<!doctype html><html lang="de"><meta name="viewport" content="width=device-width"><title>AirLink · Einrichtung</title><style>body{font:17px system-ui;max-width:680px;margin:40px auto;padding:24px;line-height:1.65;color-scheme:light dark}a{color:#2488ed}code{overflow-wrap:anywhere}</style><h1>AirLink verbinden</h1><p>Diese Einrichtungsseite ist unverschlüsselt und enthält ausschließlich das öffentliche Zertifikat. Überprüfe den folgenden Fingerabdruck direkt in der App oder Konsole des Gastgebers. Vertraue dem Zertifikat erst nach dieser Prüfung.</p><p><code>%s</code></p><p><a href="/certificate.cer">Zertifikat für Windows/macOS/Linux laden</a> · <a href="/certificate.mobileconfig">iOS-Zertifikatsprofil laden</a></p><p>iOS: Profil in Einstellungen → Allgemein → VPN und Geräteverwaltung installieren. Danach Allgemein → Info → Zertifikatsvertrauenseinstellungen → volles Vertrauen aktivieren.</p><p>macOS: Zertifikat im Schlüsselbund importieren und nach Prüfung vertrauen. Windows: Zertifikat für den aktuellen Benutzer in Vertrauenswürdige Stammzertifizierungsstellen importieren. Linux: im Browser oder lokalen Zertifikatsspeicher importieren.</p><p>Nach der Einrichtung: <a href="%s">AirLink verschlüsselt öffnen</a>. Transfers und Kopplung laufen ausschließlich über HTTPS.</p><p>First verify the fingerprint on the host. Install its certificate in your device trust store, then open the secure AirLink link above. Remove trust from device settings when no longer needed.</p>`, s.fingerprint, strings.ReplaceAll(s.currentURLs(s.urls)[0], "&", "&amp;")))
		default:
			http.NotFound(w, r)
		}
	})
	s.setupServer = &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 15 * time.Second}
	s.wg.Add(1)
	go func() { defer s.wg.Done(); _ = s.setupServer.Serve(ln) }()
	return nil
}
