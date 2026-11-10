package server

import (
	"runtime"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/engine"
	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

type diagnosticReport struct {
	Schema        int                      `json:"schemaVersion"`
	Version       string                   `json:"version"`
	Generated     string                   `json:"generatedAt"`
	Platform      string                   `json:"platform"`
	Architecture  string                   `json:"architecture"`
	PairedDevices int                      `json:"pairedDevices"`
	GoHeapBytes   uint64                   `json:"goHeapBytes"`
	Transfers     engine.DiagnosticSummary `json:"transferSummary"`
	Privacy       string                   `json:"privacy"`
}

func (s *Server) diagnostics() diagnosticReport {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	report := diagnosticReport{Schema: 1, Version: model.Version, Generated: time.Now().UTC().Format(time.RFC3339), Platform: runtime.GOOS, Architecture: runtime.GOARCH, GoHeapBytes: memory.HeapAlloc, Transfers: s.engine.Diagnostics(), Privacy: "Local aggregate report. No device/file names, addresses, paths, identifiers, hashes, credentials, clipboard content or raw logs."}
	for _, d := range s.store.snapshot().Devices {
		if d.Device.Paired {
			report.PairedDevices++
		}
	}
	return report
}
