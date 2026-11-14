package transfer

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS meta(key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS transfers(
 id TEXT PRIMARY KEY,
 device_id TEXT NOT NULL,
 device_name TEXT NOT NULL,
 direction TEXT NOT NULL,
 state TEXT NOT NULL,
 error TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 total_bytes INTEGER NOT NULL,
 completed_bytes INTEGER NOT NULL DEFAULT 0,
 file_count INTEGER NOT NULL,
 title TEXT NOT NULL DEFAULT '',
 destination TEXT NOT NULL DEFAULT '',
 next_ordinal INTEGER NOT NULL DEFAULT 0,
 next_offset INTEGER NOT NULL DEFAULT 0,
 request_id TEXT NOT NULL DEFAULT '',
 offered INTEGER NOT NULL DEFAULT 0);
CREATE INDEX IF NOT EXISTS transfers_request ON transfers(request_id);
CREATE TABLE IF NOT EXISTS files(
 transfer_id TEXT NOT NULL,
 ordinal INTEGER NOT NULL,
 path TEXT NOT NULL,
 size INTEGER NOT NULL,
 modified INTEGER NOT NULL DEFAULT 0,
 directory INTEGER NOT NULL DEFAULT 0,
 completed INTEGER NOT NULL DEFAULT 0,
 sha256 TEXT NOT NULL DEFAULT '',
 destination TEXT NOT NULL DEFAULT '',
 source TEXT NOT NULL DEFAULT '',
 identity TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(transfer_id, ordinal)) WITHOUT ROWID;`

func openStore(dataDir string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "transfers-v3.sqlite"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	// Transfer metadata names files and devices: owner access only.
	path := filepath.Join(dataDir, "transfers-v3.sqlite")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_ = os.Chmod(path+suffix, 0600)
	}
	return db, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func insertRecord(tx *sql.Tx, r *record) error {
	t := r.t
	if _, err := tx.Exec(`INSERT INTO transfers(id,device_id,device_name,direction,state,error,created_at,updated_at,total_bytes,completed_bytes,file_count,title,destination,next_ordinal,next_offset,request_id,offered) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.DeviceID, t.DeviceName, t.Direction, t.State, t.Error, t.CreatedAt, t.UpdatedAt, t.TotalBytes, r.durableBytes, len(r.files), t.Title, t.Destination, r.next, r.nextOffset, r.requestID, boolInt(r.offered)); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO files(transfer_id,ordinal,path,size,modified,directory,completed,sha256,destination,source,identity) VALUES(?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i, f := range r.files {
		if _, err = stmt.Exec(t.ID, i, f.Path, f.Size, f.Modified, boolInt(f.Directory), boolInt(f.Completed), f.SHA256, f.DestinationPath, f.source, f.identity); err != nil {
			return err
		}
	}
	return nil
}

// saveHeader persists the transfer row (state, progress and checkpoint).
func (e *Engine) saveHeader(r *record) error {
	t := r.t
	_, err := e.db.Exec(`UPDATE transfers SET device_name=?,state=?,error=?,updated_at=?,completed_bytes=?,destination=?,next_ordinal=?,next_offset=?,offered=? WHERE id=?`,
		t.DeviceName, t.State, t.Error, t.UpdatedAt, r.durableBytes, t.Destination, r.next, r.nextOffset, boolInt(r.offered), t.ID)
	return err
}

func (e *Engine) loadHeaders() error {
	rows, err := e.db.Query(`SELECT id,device_id,device_name,direction,state,error,created_at,updated_at,total_bytes,completed_bytes,file_count,title,destination,next_ordinal,next_offset,request_id,offered FROM transfers`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		r := &record{}
		var offered int
		t := &r.t
		if err = rows.Scan(&t.ID, &t.DeviceID, &t.DeviceName, &t.Direction, &t.State, &t.Error, &t.CreatedAt, &t.UpdatedAt, &t.TotalBytes, &r.durableBytes, &t.FileCount, &t.Title, &t.Destination, &r.next, &r.nextOffset, &r.requestID, &offered); err != nil {
			return err
		}
		r.offered = offered == 1
		t.CompletedBytes = r.durableBytes
		e.records[t.ID] = r
	}
	return rows.Err()
}

// loadFiles reads a transfer's manifest. Callers hold e.mu.
func (e *Engine) loadFiles(r *record) error {
	if r.files != nil {
		return nil
	}
	rows, err := e.db.Query(`SELECT ordinal,path,size,modified,directory,completed,sha256,destination,source,identity FROM files WHERE transfer_id=? ORDER BY ordinal`, r.t.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	files := make([]fileEntry, 0, r.t.FileCount)
	for rows.Next() {
		var f fileEntry
		var ordinal, directory, completed int
		if err = rows.Scan(&ordinal, &f.Path, &f.Size, &f.Modified, &directory, &completed, &f.SHA256, &f.DestinationPath, &f.source, &f.identity); err != nil {
			return err
		}
		if ordinal != len(files) {
			return errors.New("transfer database manifest is out of order")
		}
		f.ID = fileID(ordinal)
		f.Directory, f.Completed = directory == 1, completed == 1
		files = append(files, f)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(files) != r.t.FileCount {
		return fmt.Errorf("transfer database manifest is incomplete (%d of %d files)", len(files), r.t.FileCount)
	}
	r.files = files
	return nil
}

// commitFiles marks received or acknowledged files completed together with
// the transfer's new checkpoint, in one transaction.
func (e *Engine) commitFiles(r *record, ordinals []int) error {
	tx, err := e.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if len(ordinals) > 0 {
		stmt, err := tx.Prepare(`UPDATE files SET completed=1,sha256=?,destination=? WHERE transfer_id=? AND ordinal=?`)
		if err != nil {
			return err
		}
		for _, i := range ordinals {
			f := r.files[i]
			if _, err = stmt.Exec(f.SHA256, f.DestinationPath, r.t.ID, i); err != nil {
				stmt.Close()
				return err
			}
		}
		stmt.Close()
	}
	t := r.t
	if _, err = tx.Exec(`UPDATE transfers SET state=?,error=?,updated_at=?,completed_bytes=?,next_ordinal=?,next_offset=? WHERE id=?`, t.State, t.Error, t.UpdatedAt, r.durableBytes, r.next, r.nextOffset, t.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (e *Engine) saveDestinations(r *record, ordinals []int) error {
	tx, err := e.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`UPDATE files SET destination=? WHERE transfer_id=? AND ordinal=?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, i := range ordinals {
		if _, err = stmt.Exec(r.files[i].DestinationPath, r.t.ID, i); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (e *Engine) deleteRecords(ids []string) error {
	tx, err := e.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err = tx.Exec(`DELETE FROM files WHERE transfer_id=?`, id); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM transfers WHERE id=?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// searchTransfers returns IDs of transfers whose file paths contain query.
func (e *Engine) searchTransfers(query string) (map[string]bool, error) {
	rows, err := e.db.Query(`SELECT DISTINCT transfer_id FROM files WHERE instr(lower(path), ?) > 0`, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
