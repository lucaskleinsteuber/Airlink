package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func TestDiagnosticExportIsAggregatedAndPrivate(t *testing.T) {
	s := integrationHost(t)
	d, token := integrationDevice(t, s, false)
	d.Name = "sensitive-device-name"
	d.Address = "https://sensitive-address.invalid"
	s.store.updateDevice(d)
	tr, err := s.engine.CreateIncoming(d, []model.File{{ID: "sensitive-file-id", Path: "sensitive-file-name.bin", Size: 32}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.engine.Fail(tr.ID, "write /home/"+"sensitive-person/sensitive-file-name.bin: no space left on device; token=sensitive-token"); err != nil {
		t.Fatal(err)
	}
	value, err := s.Control("diagnostics.export", nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"sensitive-", tr.ID, d.ID, token, s.opts.DataDir, s.opts.ReceiveDir, s.fingerprint} {
		if secret != "" && bytes.Contains(data, []byte(secret)) {
			t.Fatal("diagnostic report included private data")
		}
	}
	report := value.(diagnosticReport)
	if report.Schema != 1 || report.PairedDevices != 1 || report.Transfers.Files != 1 || report.Transfers.Problems["storage_full"] != 1 {
		t.Fatal("missing aggregate diagnostics")
	}
	// An ordinary paired browser/device must not gain the host control method.
	request, _ := http.NewRequest("POST", hostURL(s)+"/api/v1/diagnostics.export", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := pinnedClient(s.fingerprint).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode == 200 {
		t.Fatal("diagnostics became a device API")
	}
}

func TestCompactPerFileResponsesPreserveLegacyShapeAndOwnership(t *testing.T) {
	s := integrationHost(t)
	d, token := integrationDevice(t, s, true)
	files := make([]model.File, 1000)
	for i := range files {
		files[i] = model.File{ID: fmt.Sprintf("f%d", i), Path: fmt.Sprintf("file-%d.bin", i), Size: 1}
	}
	tr, err := s.engine.CreateIncoming(d, files)
	if err != nil {
		t.Fatal(err)
	}
	url := hostURL(s) + "/api/v1/transfers/" + tr.ID + "/files/" + files[0].ID + "/prepare"
	for _, compact := range []bool{false, true} {
		var out struct {
			Transfer model.Transfer `json:"transfer"`
		}
		address := url
		if compact {
			address += "?fileOnly=1"
		}
		if err = requestJSON(context.Background(), pinnedClient(s.fingerprint), "POST", address, token, map[string]string{"sha256": digest([]byte("x"))}, &out); err != nil {
			t.Fatal(err)
		}
		want := len(files)
		if compact {
			want = 1
		}
		if out.Transfer.ID != tr.ID || len(out.Transfer.Files) != want || out.Transfer.Files[0].ID != files[0].ID {
			t.Fatal("file response changed identity/legacy shape")
		}
		if compact {
			body, _ := json.Marshal(out)
			if len(body) > 4096 {
				t.Fatal("compact response grew with manifest")
			}
		}
	}
	if code := uploadBlock(t, s, token, tr.ID, files[0].ID, 0, []byte("x")); code != 200 {
		t.Fatal(code)
	}
	for _, compact := range []bool{false, true} {
		address := strings.TrimSuffix(url, "prepare") + "complete"
		if compact {
			address += "?fileOnly=1"
		}
		var completed model.Transfer
		if err = requestJSON(context.Background(), pinnedClient(s.fingerprint), "POST", address, token, map[string]string{"sha256": digest([]byte("x"))}, &completed); err != nil {
			t.Fatal(err)
		}
		want := len(files)
		if compact {
			want = 1
		}
		if len(completed.Files) != want || !completed.Files[0].Completed || completed.Files[0].ID != files[0].ID {
			t.Fatal("completion response lost identity or legacy shape")
		}
	}
	_, otherToken := integrationDevice(t, s, false)
	request, _ := http.NewRequest("POST", url+"?fileOnly=1", strings.NewReader(`{"sha256":"`+digest([]byte("x"))+`"}`))
	request.Header.Set("Authorization", "Bearer "+otherToken)
	response, err := pinnedClient(s.fingerprint).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != 404 || bytes.Contains(body, []byte(files[0].ID)) {
		t.Fatal("compact response bypassed ownership")
	}
}

func TestExplicitRetryReusesRemoteCheckpointAndOriginalSource(t *testing.T) {
	source, target := integrationHost(t), integrationHost(t)
	device, token := integrationDevice(t, target, true)
	// A browser-kind fixture prevents the periodic native worker from racing
	// this deliberately interrupted, directly driven HTTPS worker.
	rec := deviceRecord{Device: model.Device{ID: target.store.snapshot().ID, Kind: "browser", Name: "Receiver", Paired: true, CanReceive: true, Address: hostURL(target), Fingerprint: target.fingerprint}, OutboundToken: token}
	if err := source.store.put(rec); err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("original transfer bytes"), 400000)
	path := filepath.Join(t.TempDir(), "original.bin")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	offered, err := source.engine.Offer(rec.Device, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = source.engine.SourceSHA256(context.Background(), offered.ID, offered.Files[0].ID); err != nil {
		t.Fatal(err)
	}
	remote, err := target.engine.CreateIncoming(device, offered.Files)
	if err != nil {
		t.Fatal(err)
	}
	if err = source.saveRemoteID(offered.ID, remote.ID); err != nil {
		t.Fatal(err)
	}
	if code := uploadBlock(t, target, token, remote.ID, offered.Files[0].ID, 0, data[:model.ChunkSize]); code != 200 {
		t.Fatal(code)
	}
	if err = source.engine.MarkOutgoingProgress(offered.ID, offered.Files[0].ID, model.ChunkSize, digest(data)); err != nil {
		t.Fatal(err)
	}
	if err = source.engine.Fail(offered.ID, "storage_full: Receiving disk is full"); err != nil {
		t.Fatal(err)
	}
	if _, err = source.Control("transfer.resume", json.RawMessage(`{"id":"`+offered.ID+`"}`)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	source.sendTransfer(ctx, offered, rec)
	completed, err := source.engine.Get(offered.ID)
	if err != nil || completed.State != "completed" || completed.ID != offered.ID {
		t.Fatal("retry did not finish the original job", completed.State, err)
	}
	results := target.engine.Transfers("")
	if len(results) != 1 || results[0].ID != remote.ID || results[0].State != "completed" {
		t.Fatal("retry created a different receiver or lost completion")
	}
	actual, err := os.ReadFile(filepath.Join(results[0].Destination, "original.bin"))
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatal("retry changed final bytes", err)
	}
	original, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, data) {
		t.Fatal("retry altered original source", err)
	}
}
