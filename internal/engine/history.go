package engine

import "strings"

// RemoveHistory removes one terminal transfer's metadata. Ownership and state
// are checked under the same lock as the durable deletion. An empty deviceID
// is reserved for private host control; remote callers supply their own ID.
func (e *Engine) RemoveHistory(deviceID, id string) error {
	if strings.TrimSpace(id) == "" || strings.Contains(id, "/") {
		return ErrInvalid
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.getLocked(id)
	if err != nil {
		return err
	}
	if deviceID != "" && r.Transfer.DeviceID != deviceID {
		return ErrNotFound
	}
	if !terminal(r.Transfer.State) {
		return ErrConflict
	}
	return e.removeHistoryLocked([]string{id})
}

// ClearHistory removes terminal transfer metadata only. Payloads, originals,
// share references, active transfers and other devices' history remain intact.
func (e *Engine) ClearHistory(deviceID string) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	ids := []string{}
	for id, r := range e.records {
		if terminal(r.Transfer.State) && (deviceID == "" || r.Transfer.DeviceID == deviceID) {
			ids = append(ids, id)
		}
	}
	if err := e.removeHistoryLocked(ids); err != nil {
		return 0, err
	}
	return len(ids), nil
}

// Callers hold mu and have already checked every entry's terminal state.
// Commit before changing in-memory state or cancelling residual operations so
// a failed database write does not pretend to have removed the history.
func (e *Engine) removeHistoryLocked(ids []string) error {
	tx, err := e.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err = tx.Exec("DELETE FROM transfer_files WHERE transfer_id=?", id); err != nil {
			return err
		}
		if _, err = tx.Exec("DELETE FROM chunks WHERE transfer_id=?", id); err != nil {
			return err
		}
		if _, err = tx.Exec("DELETE FROM transfers WHERE id=?", id); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	for _, id := range ids {
		e.cancelVerificationLocked(id)
		e.cancelSourceOperationsLocked(id)
		for _, file := range e.records[id].Transfer.Files {
			e.locks.Delete(id + "/" + file.ID)
		}
		delete(e.records, id)
		delete(e.progress, id)
	}
	return nil
}
