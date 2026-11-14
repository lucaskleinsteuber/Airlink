package server

import (
	"testing"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

// pairForTest records the reciprocal trust that a completed comparison-code
// pairing creates, without running the interactive confirmation steps.
func pairForTest(t *testing.T, a, b *Server) (bOnA, aOnB model.Device) {
	t.Helper()
	ap, bp := a.store.snapshot(), b.store.snapshot()
	aToB, bToA := randomID(), randomID()
	bOnA = model.Device{ID: bp.ID, Name: bp.Settings.Name, Kind: "engine", Address: hostURL(b), Fingerprint: b.fingerprint, Paired: true, CanSend: true, CanReceive: true}
	aOnB = model.Device{ID: ap.ID, Name: ap.Settings.Name, Kind: "engine", Address: hostURL(a), Fingerprint: a.fingerprint, Paired: true, CanSend: true, CanReceive: true}
	if err := a.store.put(deviceRecord{Device: bOnA, TokenHash: digest([]byte(bToA)), OutboundToken: aToB}); err != nil {
		t.Fatal(err)
	}
	if err := b.store.put(deviceRecord{Device: aOnB, TokenHash: digest([]byte(aToB)), OutboundToken: bToA}); err != nil {
		t.Fatal(err)
	}
	return bOnA, aOnB
}
