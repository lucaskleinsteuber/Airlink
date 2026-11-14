package transfer

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

// Older services (0.12 and earlier) used transfers.sqlite with JSON documents.
// Their history is imported once; unfinished old transfers cannot continue
// with protocol 3 and are recorded as failed with a clear reason.

var legacyPrefix = []byte("AirLink transfer store v2\n")

const legacyUnfinished = "Started with an older AirLink version; send it again"

type legacyRecord struct {
	Transfer      model.Transfer `json:"transfer"`
	ReceiveRoot   string         `json:"receiveRoot"`
	DirectReceive bool           `json:"directReceive"`
}

type legacyFile struct {
	File model.File `json:"file"`
}

func (e *Engine) migrateLegacy() error {
	var done string
	err := e.db.QueryRow(`SELECT value FROM meta WHERE key='legacy-import'`).Scan(&done)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	oldPath := filepath.Join(e.dataDir, "transfers.sqlite")
	if _, err = os.Stat(oldPath); errors.Is(err, os.ErrNotExist) {
		_, err = e.db.Exec(`INSERT INTO meta(key,value) VALUES('legacy-import',?)`, now())
		return err
	}
	old, err := sql.Open("sqlite", oldPath)
	if err != nil {
		return err
	}
	records, err := readLegacy(old)
	old.Close()
	if err != nil {
		return err
	}
	tx, err := e.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, r := range records {
		if err = insertRecord(tx, r); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`INSERT INTO meta(key,value) VALUES('legacy-import',?)`, now()); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	// Keep the old store under another name: older versions must not reuse it.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, statErr := os.Stat(oldPath + suffix); statErr == nil {
			_ = os.Rename(oldPath+suffix, filepath.Join(e.dataDir, "transfers-0.12.sqlite"+suffix))
		}
	}
	return nil
}

func readLegacy(db *sql.DB) ([]*record, error) {
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='transfers'`).Scan(&count); err != nil || count == 0 {
		return nil, err
	}
	rows, err := db.Query(`SELECT id,document FROM transfers`)
	if err != nil {
		return nil, err
	}
	type entry struct {
		doc        legacyRecord
		normalized bool
	}
	entries := map[string]*entry{}
	order := []string{}
	for rows.Next() {
		var id string
		var document []byte
		if err = rows.Scan(&id, &document); err != nil {
			rows.Close()
			return nil, err
		}
		item := &entry{}
		if bytes.HasPrefix(document, legacyPrefix) {
			item.normalized = true
			document = document[len(legacyPrefix):]
		}
		if json.Unmarshal(document, &item.doc) != nil || item.doc.Transfer.ID != id {
			continue
		}
		if item.normalized {
			item.doc.Transfer.Files = nil
		}
		entries[id] = item
		order = append(order, id)
	}
	rows.Close()
	var hasFiles int
	if err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='transfer_files'`).Scan(&hasFiles); err != nil {
		return nil, err
	}
	if hasFiles > 0 {
		fileRows, err := db.Query(`SELECT transfer_id,document FROM transfer_files ORDER BY transfer_id,ordinal`)
		if err != nil {
			return nil, err
		}
		for fileRows.Next() {
			var id string
			var document []byte
			if err = fileRows.Scan(&id, &document); err != nil {
				fileRows.Close()
				return nil, err
			}
			item := entries[id]
			var f legacyFile
			if item == nil || !item.normalized || json.Unmarshal(document, &f) != nil {
				continue
			}
			item.doc.Transfer.Files = append(item.doc.Transfer.Files, f.File)
		}
		fileRows.Close()
	}
	out := []*record{}
	for _, id := range order {
		item := entries[id]
		t := item.doc.Transfer
		if len(t.Files) == 0 || t.Direction != "send" && t.Direction != "receive" {
			continue
		}
		r := &record{}
		r.t = model.Transfer{ID: t.ID, DeviceID: t.DeviceID, DeviceName: t.DeviceName, Direction: t.Direction, State: t.State, Error: t.Error, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, TotalBytes: t.TotalBytes}
		if r.t.UpdatedAt == "" {
			r.t.UpdatedAt = r.t.CreatedAt
		}
		if !terminal(r.t.State) {
			r.t.State, r.t.Error = "failed", legacyUnfinished
		}
		if t.Direction == "receive" {
			r.t.Destination = t.Destination
			if !item.doc.DirectReceive && item.doc.ReceiveRoot != "" {
				r.t.Destination = item.doc.ReceiveRoot
			}
		}
		r.files = make([]fileEntry, len(t.Files))
		for i, f := range t.Files {
			entry := fileEntry{}
			entry.Path, entry.Size, entry.Modified, entry.Directory, entry.SHA256 = f.Path, f.Size, f.Modified, f.Directory, f.SHA256
			entry.Completed = f.Completed && !f.Skipped
			if t.Direction == "receive" && entry.Completed {
				entry.DestinationPath = f.DestinationPath
				if entry.DestinationPath == "" {
					entry.DestinationPath = f.Path
				}
			}
			if entry.Completed {
				r.durableBytes += f.Size
			}
			r.files[i] = entry
		}
		r.t.FileCount, r.t.Title = len(r.files), r.files[0].Path
		r.next = len(r.files)
		out = append(out, r)
	}
	return out, nil
}
