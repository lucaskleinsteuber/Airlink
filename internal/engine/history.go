package engine

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
	tx, err := e.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, id := range ids {
		e.cancelSourceOperationsLocked(id)
		if _, err = tx.Exec("DELETE FROM chunks WHERE transfer_id=?", id); err != nil {
			return 0, err
		}
		if _, err = tx.Exec("DELETE FROM transfers WHERE id=?", id); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	for _, id := range ids {
		for _, file := range e.records[id].Transfer.Files {
			e.locks.Delete(id + "/" + file.ID)
		}
		delete(e.records, id)
		delete(e.progress, id)
	}
	return len(ids), nil
}
