package server

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func TestPreparationIdempotentAndSummaryStaysCompact(t *testing.T) {
	a, b := pairedHosts(t)
	path := filepath.Join(t.TempDir(), "test.bin")
	if err := os.WriteFile(path, []byte("source bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(preparationInput{DeviceID: hostID(b), Paths: []string{path}, RequestID: "0123456789abcdef0123456789abcdef"})
	value, err := a.Control("preparation.start", input)
	if err != nil {
		t.Fatal(err)
	}
	job := value.(model.Preparation)
	value, err = a.Control("preparation.start", input)
	if err != nil || value.(model.Preparation).ID != job.ID {
		t.Fatal(value, err)
	}
	waitFor(t, 5*time.Second, "preparation", func() bool {
		value, err = a.Control("preparation.status", json.RawMessage(`{"id":"`+job.ID+`"}`))
		job = value.(model.Preparation)
		return err == nil && job.State != "preparing"
	})
	if job.State != "completed" || job.TransferID == "" || len(a.engine.Transfers(hostID(b))) != 1 {
		t.Fatal(job)
	}
	input, _ = json.Marshal(preparationInput{DeviceID: hostID(b), Paths: []string{path + "changed"}, RequestID: job.RequestID})
	if _, err = a.Control("preparation.start", input); err == nil {
		t.Fatal("changed request reused receipt")
	}
	waitTransfer(t, a, job.TransferID, "completed")
	status := a.summaryStatus()
	if !status.Summary || len(status.Transfers) != 1 || !status.Transfers[0].Summary || len(status.Transfers[0].Files) != 1 {
		t.Fatal(status)
	}
	encoded, _ := json.Marshal(status)
	if bytes.Contains(encoded, []byte(`"days":null`)) {
		t.Fatal("summary has undecodable statistics")
	}
}
