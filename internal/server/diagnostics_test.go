package server

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestDiagnosticExportIsAggregatedAndPrivate(t *testing.T) {
	a, b := pairedHosts(t)
	if err := a.store.edit(hostID(b), func(r *deviceRecord) { r.Device.Name = "sensitive-device-name" }); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "sensitive-file-name.bin")
	randomFile(t, source, 32)
	out := offer(t, a, hostID(b), source)
	waitTransfer(t, a, out.ID, "completed")
	value, err := a.Control("diagnostics.export", nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"sensitive-", out.ID, hostID(b), hostID(a), a.opts.DataDir, a.opts.ReceiveDir, a.fingerprint, b.fingerprint} {
		if secret != "" && bytes.Contains(data, []byte(secret)) {
			t.Fatalf("diagnostic report included private data %q", secret)
		}
	}
	report := value.(diagnosticReport)
	if report.Schema != 1 || report.PairedDevices != 1 || report.Transfers.Files != 1 || report.Transfers.States["completed"] != 1 {
		t.Fatal("missing aggregate diagnostics", report)
	}
}
