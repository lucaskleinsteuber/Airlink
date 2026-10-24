package server

import (
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/http"
)

func (s *Server) serveCertificate(w http.ResponseWriter, r *http.Request) bool {
	block, _ := pem.Decode(s.certificate)
	if block == nil {
		http.Error(w, "certificate unavailable", 500)
		return true
	}
	switch r.URL.Path {
	case "/certificate.cer":
		w.Header().Set("Content-Type", "application/pkix-cert")
		w.Header().Set("Content-Disposition", `attachment; filename="AirLink.cer"`)
		_, _ = w.Write(block.Bytes)
		return true
	case "/certificate.mobileconfig":
		w.Header().Set("Content-Type", "application/x-apple-aspen-config")
		w.Header().Set("Content-Disposition", `attachment; filename="AirLink.mobileconfig"`)
		fp := s.fingerprint
		_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>PayloadType</key><string>Configuration</string><key>PayloadVersion</key><integer>1</integer><key>PayloadIdentifier</key><string>com.lucasfd3.airlink.%s</string><key>PayloadUUID</key><string>%s-%s-%s-%s-%s</string><key>PayloadDisplayName</key><string>AirLink local certificate</string><key>PayloadDescription</key><string>Verify the SHA-256 fingerprint on your AirLink host before installing. This profile contains only its public local certificate.</string><key>PayloadRemovalDisallowed</key><false/><key>PayloadContent</key><array><dict><key>PayloadType</key><string>com.apple.security.root</string><key>PayloadVersion</key><integer>1</integer><key>PayloadIdentifier</key><string>com.lucasfd3.airlink.%s.certificate</string><key>PayloadUUID</key><string>%s-%s-%s-%s-%s</string><key>PayloadDisplayName</key><string>AirLink local trust</string><key>PayloadContent</key><data>%s</data></dict></array></dict></plist>`, fp, fp[:8], fp[8:12], fp[12:16], fp[16:20], fp[20:32], fp, fp[32:40], fp[40:44], fp[44:48], fp[48:52], fp[52:64], base64.StdEncoding.EncodeToString(block.Bytes))
		return true
	}
	return false
}
