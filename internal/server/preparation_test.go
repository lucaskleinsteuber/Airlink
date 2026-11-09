package server

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucasfd3/Airlink/internal/model"
)

func TestPreparationIdempotentAndSummaryDoesNotLoadFullHistory(t *testing.T) {
	s := integrationHost(t)
	d, _ := integrationDevice(t, s, false)
	path := filepath.Join(t.TempDir(), "test.bin")
	if err := os.WriteFile(path, []byte("source bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(preparationInput{DeviceID: d.ID, Paths: []string{path}, RequestID: "0123456789abcdef0123456789abcdef"})
	value, err := s.Control("preparation.start", input)
	if err != nil {
		t.Fatal(err)
	}
	job := value.(model.Preparation)
	if _, err = s.Control("status.summary", nil); err != nil {
		t.Fatal(err)
	}
	value, err = s.Control("preparation.start", input)
	if err != nil || value.(model.Preparation).ID != job.ID {
		t.Fatal(value, err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		value, err = s.Control("preparation.status", json.RawMessage(`{"id":"`+job.ID+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		job = value.(model.Preparation)
		if job.State != "preparing" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if job.State != "completed" || job.TransferID == "" || len(s.engine.Transfers(d.ID)) != 1 {
		t.Fatal(job)
	}
	input, _ = json.Marshal(preparationInput{DeviceID: d.ID, Paths: []string{path + "changed"}, RequestID: job.RequestID})
	if _, err = s.Control("preparation.start", input); err == nil {
		t.Fatal("changed request reused receipt")
	}
	status := s.summaryStatus()
	if !status.Summary || len(status.Transfers) != 1 || !status.Transfers[0].Summary {
		t.Fatal(status)
	}
	encoded, _ := json.Marshal(status)
	if bytes.Contains(encoded, []byte(`"days":null`)) {
		t.Fatal("summary has undecodable statistics")
	}
}
