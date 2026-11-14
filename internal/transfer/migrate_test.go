package transfer

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func TestLegacyHistoryIsImportedOnce(t *testing.T) {
	base := t.TempDir()
	data, receive := filepath.Join(base, "state"), filepath.Join(base, "received")
	if err := os.MkdirAll(data, 0700); err != nil {
		t.Fatal(err)
	}
	old, err := sql.Open("sqlite", filepath.Join(data, "transfers.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = old.Exec(`CREATE TABLE transfers(id TEXT PRIMARY KEY, document BLOB NOT NULL);
CREATE TABLE transfer_files(transfer_id TEXT NOT NULL,file_id TEXT NOT NULL,ordinal INTEGER NOT NULL,document BLOB NOT NULL,PRIMARY KEY(transfer_id,file_id),UNIQUE(transfer_id,ordinal));`); err != nil {
		t.Fatal(err)
	}
	received := model.Transfer{ID: "received-id", DeviceID: "mac", DeviceName: "Mac", Direction: "receive", State: "completed", TotalBytes: 3, CreatedAt: "2026-11-01T10:00:00Z", UpdatedAt: "2026-11-01T10:00:01Z", Destination: receive}
	header, _ := json.Marshal(map[string]any{"transfer": received, "directReceive": true})
	if _, err = old.Exec(`INSERT INTO transfers(id,document) VALUES(?,?)`, received.ID, append(append([]byte{}, legacyPrefix...), header...)); err != nil {
		t.Fatal(err)
	}
	for i, f := range []model.File{{ID: "x1", Path: "Folder", Directory: true, Completed: true}, {ID: "x2", Path: "Folder/a.txt", Size: 3, Completed: true, Received: 3, DestinationPath: "Folder (2)/a.txt", SHA256: strings.Repeat("a", 64)}} {
		doc, _ := json.Marshal(map[string]any{"file": f})
		if _, err = old.Exec(`INSERT INTO transfer_files(transfer_id,file_id,ordinal,document) VALUES(?,?,?,?)`, received.ID, f.ID, i, doc); err != nil {
			t.Fatal(err)
		}
	}
	unfinished := model.Transfer{ID: "sent-id", DeviceID: "arch", DeviceName: "Arch", Direction: "send", State: "running", TotalBytes: 10, CreatedAt: "2026-11-02T10:00:00Z", Files: []model.File{{ID: "s1", Path: "b.bin", Size: 10}}}
	legacy, _ := json.Marshal(map[string]any{"transfer": unfinished})
	if _, err = old.Exec(`INSERT INTO transfers(id,document) VALUES(?,?)`, unfinished.ID, legacy); err != nil {
		t.Fatal(err)
	}
	old.Close()

	e, err := New(data, receive, Options{Session: func(string) Conn { return nil }, Device: func(string) (model.Device, bool) { return model.Device{}, false }})
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.Get("received-id")
	if err != nil || got.State != "completed" || len(got.Files) != 2 || got.Files[1].DestinationPath != "Folder (2)/a.txt" || got.CompletedBytes != 3 {
		t.Fatal("completed history", got, err)
	}
	sent, err := e.Header("sent-id")
	if err != nil || sent.State != "failed" || sent.Error != legacyUnfinished {
		t.Fatal("unfinished legacy transfer", sent, err)
	}
	if _, err = os.Stat(filepath.Join(data, "transfers.sqlite")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("legacy store still active", err)
	}
	e.Close()
	// A second start does not import again (the old file is gone anyway).
	e, err = New(data, receive, Options{Session: func(string) Conn { return nil }, Device: func(string) (model.Device, bool) { return model.Device{}, false }})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if n := len(e.Transfers("")); n != 2 {
		t.Fatal("history count", n)
	}
}

func TestOfferWithLongPathsIsSplitBySize(t *testing.T) {
	a, b := newHost(t, "aaaa"), newHost(t, "bbbb")
	connect(a, b, true)
	root := filepath.Join(t.TempDir(), "Tief")
	segment := strings.Repeat("v", 200)
	deep := filepath.Join(root, segment, segment, segment)
	for i := range 1200 {
		writeRandom(t, filepath.Join(deep, strings.Repeat("n", 150)+"-"+string(rune('a'+i%26))+time.Duration(i).String()+".txt"), 1)
	}
	out := send(t, a, "bbbb", root)
	waitState(t, b, out.ID, "completed", 30*time.Second)
	if got := inventory(t, b.receive); len(got) != 1200+4 {
		t.Fatal("entries", len(got))
	}
}
