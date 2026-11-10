package engine

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

// The non-JSON prefix makes older clients refuse this store before modifying
// any records. Silently reading a header without its file checkpoints would
// lose confirmed progress on downgrade.
var recordPrefix = []byte("AirLink transfer store v2\n")

type storedFile struct {
	File     model.File `json:"file"`
	Source   *source    `json:"source,omitempty"`
	Verified string     `json:"verified,omitempty"`
}

func headerDocument(r *record) ([]byte, error) {
	header := *r
	header.Transfer.FileCount = len(r.Transfer.Files)
	header.Transfer.Files = nil
	header.Sources = nil
	header.LegacyVerified = nil
	b, err := json.Marshal(&header)
	return append(bytes.Clone(recordPrefix), b...), err
}

func fileDocument(r *record, file model.File, override *source) ([]byte, error) {
	value := storedFile{File: file, Verified: r.LegacyVerified[file.ID]}
	if override != nil {
		value.Source = override
	} else if src, ok := r.Sources[file.ID]; ok {
		value.Source = &src
	}
	return json.Marshal(value)
}

func (e *Engine) loadRecords() error {
	rows, err := e.db.Query("SELECT id,document FROM transfers")
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var document []byte
		if err = rows.Scan(&id, &document); err != nil {
			break
		}
		r := &record{}
		if bytes.HasPrefix(document, recordPrefix) {
			r.normalized = true
			document = document[len(recordPrefix):]
		}
		if err = json.Unmarshal(document, r); err != nil {
			break
		}
		if r.Transfer.ID != id {
			err = errors.New("transfer database identity mismatch")
			break
		}
		if r.normalized {
			r.Transfer.Files = []model.File{}
			r.Sources = map[string]source{}
			r.LegacyVerified = map[string]string{}
		}
		e.records[id] = r
	}
	if rowErr := rows.Err(); err == nil {
		err = rowErr
	}
	rows.Close()
	if err != nil {
		return err
	}
	// Close the header cursor first: the database has a single writer/connection.
	rows, err = e.db.Query("SELECT transfer_id,file_id,ordinal,document FROM transfer_files ORDER BY transfer_id,ordinal")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, fileID string
		var ordinal int
		var document []byte
		if err = rows.Scan(&id, &fileID, &ordinal, &document); err != nil {
			return err
		}
		r := e.records[id]
		if r == nil || !r.normalized {
			continue
		}
		var value storedFile
		if err = json.Unmarshal(document, &value); err != nil {
			return err
		}
		if ordinal != len(r.Transfer.Files) || fileID != value.File.ID {
			return errors.New("transfer database file checkpoint mismatch")
		}
		r.Transfer.Files = append(r.Transfer.Files, value.File)
		if value.Source != nil {
			r.Sources[fileID] = *value.Source
		}
		if value.Verified != "" {
			r.LegacyVerified[fileID] = value.Verified
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range e.records {
		if r.normalized && len(r.Transfer.Files) != r.Transfer.FileCount {
			return errors.New("transfer database manifest is incomplete")
		}
	}
	return nil
}

// saveLocked is the full snapshot path for creation, migration and bulk edits.
// Hot file operations use saveFileLocked so unchanged manifests are not written.
func writeFullRecord(tx *sql.Tx, r *record) error {
	header, err := headerDocument(r)
	if err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO transfers(id,document) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET document=excluded.document", r.Transfer.ID, header); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM transfer_files WHERE transfer_id=?", r.Transfer.ID); err != nil {
		return err
	}
	statement, err := tx.Prepare("INSERT INTO transfer_files(transfer_id,file_id,ordinal,document) VALUES(?,?,?,?)")
	if err != nil {
		return err
	}
	defer statement.Close()
	for i, file := range r.Transfer.Files {
		b, encodeErr := fileDocument(r, file, nil)
		if encodeErr != nil {
			return encodeErr
		}
		if _, err = statement.Exec(r.Transfer.ID, file.ID, i, b); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) saveLocked(r *record) error {
	r.Transfer.UpdatedAt = now()
	tx, err := e.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = writeFullRecord(tx, r); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	r.normalized = true
	return nil
}

// Migrate all legacy documents in one durable transaction, preserving their
// activity timestamps. Reopening an existing history must not rewrite it or
// make every old result appear newly completed.
func (e *Engine) migrateLegacyRecords() error {
	pending := []*record{}
	for _, r := range e.records {
		if !r.normalized {
			pending = append(pending, r)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	tx, err := e.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, r := range pending {
		if err = writeFullRecord(tx, r); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	for _, r := range pending {
		r.normalized = true
	}
	return nil
}

func writeFileCheckpoint(tx *sql.Tx, r *record, index int, file model.File, src *source) error {
	header, err := headerDocument(r)
	if err != nil {
		return err
	}
	b, err := fileDocument(r, file, src)
	if err != nil {
		return err
	}
	headerResult, err := tx.Exec("UPDATE transfers SET document=? WHERE id=?", header, r.Transfer.ID)
	if err != nil {
		return err
	}
	if n, err := headerResult.RowsAffected(); err != nil || n != 1 {
		return errors.New("transfer database header is missing")
	}
	result, err := tx.Exec("UPDATE transfer_files SET document=? WHERE transfer_id=? AND file_id=? AND ordinal=?", b, r.Transfer.ID, file.ID, index)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return errors.New("transfer database file checkpoint is missing")
	}
	return nil
}

func (e *Engine) saveFileLocked(r *record, index int) error {
	if !r.normalized {
		return e.saveLocked(r)
	}
	r.Transfer.UpdatedAt = now()
	tx, err := e.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = writeFileCheckpoint(tx, r, index, r.Transfer.Files[index], nil); err != nil {
		return err
	}
	return tx.Commit()
}

// State-only transitions leave every file/source checkpoint untouched.
func (e *Engine) saveStateLocked(r *record) error {
	if !r.normalized {
		return e.saveLocked(r)
	}
	r.Transfer.UpdatedAt = now()
	header, err := headerDocument(r)
	if err != nil {
		return err
	}
	result, err := e.db.Exec("UPDATE transfers SET document=? WHERE id=?", header, r.Transfer.ID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return errors.New("transfer database header is missing")
	}
	return nil
}

func (r *record) fileIndex(id string) (int, error) {
	if i, ok := r.fileIndexes[id]; ok && r.indexedFiles == len(r.Transfer.Files) && i < len(r.Transfer.Files) && r.Transfer.Files[i].ID == id {
		return i, nil
	}
	r.fileIndexes = make(map[string]int, len(r.Transfer.Files))
	for i, file := range r.Transfer.Files {
		r.fileIndexes[file.ID] = i
	}
	r.indexedFiles = len(r.Transfer.Files)
	if i, ok := r.fileIndexes[id]; ok {
		return i, nil
	}
	return 0, ErrNotFound
}

// Header and FileSnapshot are private worker reads without a manifest copy.
func (e *Engine) Header(id string) (model.Transfer, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.getLocked(id)
	if err != nil {
		return model.Transfer{}, err
	}
	t := r.Transfer
	t.Files = nil
	return t, nil
}

func (e *Engine) FileSnapshot(id, fileID string) (model.Transfer, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.getLocked(id)
	if err != nil {
		return model.Transfer{}, err
	}
	i, err := r.fileIndex(fileID)
	if err != nil {
		return model.Transfer{}, err
	}
	t := r.Transfer
	t.Files = []model.File{r.Transfer.Files[i]}
	return clone(t), nil
}

func (e *Engine) OutgoingCandidates() []model.Transfer {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []model.Transfer{}
	for _, r := range e.records {
		if r.Transfer.Direction != "send" || r.Transfer.State == "completed" || r.Transfer.State == "failed" || r.Transfer.State == "paused" {
			continue
		}
		t := r.Transfer
		t.Files = nil
		out = append(out, t)
	}
	return out
}
