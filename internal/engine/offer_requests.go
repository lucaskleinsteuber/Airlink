package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

// OfferWithRequestID recovers a local submission whose durable commit may have
// preceded a lost private-pipe reply. Its identity is stored in the same SQLite
// document as the transfer, without a second commit window or source copy.
// Legacy callers omit the ID. This does not resume or modify an existing job.
func (e *Engine) OfferWithRequestID(device model.Device, paths []string, requestID string) (model.Transfer, error) {
	return e.OfferWithRequestIDContext(e.ctx, device, paths, requestID)
}

func (e *Engine) OfferWithRequestIDContext(ctx context.Context, device model.Device, paths []string, requestID string) (model.Transfer, error) {
	if requestID == "" {
		return e.offerContext(ctx, device, paths, "", "")
	}
	if len(requestID) != 32 || !device.CanReceive {
		return model.Transfer{}, ErrInvalid
	}
	for _, c := range requestID {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return model.Transfer{}, ErrInvalid
		}
	}
	clean := make([]string, len(paths))
	for i, path := range paths {
		if !filepath.IsAbs(path) {
			return model.Transfer{}, ErrInvalid
		}
		clean[i] = filepath.Clean(path)
	}
	payload, _ := json.Marshal(struct {
		DeviceID string
		Paths    []string
	}{device.ID, clean})
	hash := sha256.Sum256(payload)
	inputHash := hex.EncodeToString(hash[:])
	// Only keyed submissions serialize with one another. The filesystem walk
	// happens outside mu, so status, pause, receipt and shutdown stay responsive.
	for !e.offerMu.TryLock() {
		select {
		case <-ctx.Done():
			return model.Transfer{}, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer e.offerMu.Unlock()
	if err := ctx.Err(); err != nil {
		return model.Transfer{}, err
	}
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return model.Transfer{}, errors.New("engine closed")
	}
	for _, record := range e.records {
		if record.Transfer.Direction != "send" || record.OfferRequestID != requestID {
			continue
		}
		if record.OfferInputHash != inputHash {
			e.mu.Unlock()
			return model.Transfer{}, ErrConflict
		}
		result := clone(record.Transfer)
		e.mu.Unlock()
		return result, nil
	}
	e.mu.Unlock()
	return e.offerContext(ctx, device, clean, requestID, inputHash)
}
