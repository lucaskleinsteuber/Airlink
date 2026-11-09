package server

import (
	"errors"
	"fmt"

	"github.com/lucaskleinsteuber/Airlink/internal/engine"
	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

// Accepting and saving future receipt permission are explicit local actions.
// Device administration remains locked across acceptance so a revoked sender
// cannot regain access through a stale trusted-accept request.
func (s *Server) acceptTrusted(id string) (model.Transfer, error) {
	t, err := s.engine.Get(id)
	if err != nil {
		return model.Transfer{}, err
	}
	if t.Direction != "receive" || t.State != "offered" {
		return model.Transfer{}, engine.ErrConflict
	}
	s.store.Lock()
	defer s.store.Unlock()
	previous, ok := s.store.data.Devices[t.DeviceID]
	if !ok || !previous.Device.Paired || !previous.Device.CanSend {
		return model.Transfer{}, errors.New("the sending device is no longer paired or allowed to send")
	}
	if err = s.engine.Accept(id); err != nil {
		return model.Transfer{}, err
	}
	if !previous.Device.AutoAccept {
		updated := previous
		updated.Device.AutoAccept = true
		s.store.data.Devices[t.DeviceID] = updated
		if err = s.store.save(); err != nil {
			s.store.data.Devices[t.DeviceID] = previous
			return model.Transfer{}, fmt.Errorf("transfer accepted; automatic receipt could not be saved: %w", err)
		}
	}
	return s.engine.Get(id)
}
