package server

import (
	"context"
	"net/http"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func interruptRequestOnCancellation(ctx context.Context, w http.ResponseWriter, r *http.Request) func() {
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(done)
		controller := http.NewResponseController(w)
		_ = controller.SetReadDeadline(time.Now())
		_ = controller.SetWriteDeadline(time.Now())
		if r.Body != nil {
			_ = r.Body.Close()
		}
	})
	return func() {
		// Join an already-started callback before net/http reuses the connection.
		if !stop() {
			<-done
		}
	}
}

func (s *Server) trackTransferRequest(transferID, requestID string, cancel context.CancelFunc) func() {
	s.mu.Lock()
	if s.transferRequests == nil {
		s.transferRequests = map[string]map[string]context.CancelFunc{}
	}
	if s.transferRequests[transferID] == nil {
		s.transferRequests[transferID] = map[string]context.CancelFunc{}
	}
	s.transferRequests[transferID][requestID] = cancel
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.transferRequests[transferID], requestID)
		if len(s.transferRequests[transferID]) == 0 {
			delete(s.transferRequests, transferID)
		}
		s.mu.Unlock()
	}
}

func (s *Server) stopTransferRequests(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cancel := range s.transferRequests[id] {
		cancel()
	}
}

func (s *Server) pauseDisallowedTransfers(device model.Device) {
	for _, transfer := range s.engine.Transfers(device.ID) {
		if transfer.Direction == "receive" && !device.CanSend || transfer.Direction == "send" && !device.CanReceive {
			s.stopSender(transfer.ID)
			_ = s.engine.Pause(transfer.ID)
			s.stopTransferRequests(transfer.ID)
		}
	}
}
