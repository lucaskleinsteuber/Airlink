package server

import (
	"strings"
	"time"
)

// Durable reciprocal credentials make a lost pairing-confirmation response
// recoverable. Retry the same credential rather than invalidating a live peer.
func (s *Server) confirmationLoop() {
	defer s.wg.Done()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-tick.C:
		}
		for id, record := range s.store.snapshot().Devices {
			if record.PendingReturnToken == "" {
				continue
			}
			e := requestJSON(s.ctx, pinnedClient(record.Device.Fingerprint), "POST", strings.TrimRight(record.Device.Address, "/")+"/api/v1/peer/confirm", record.OutboundToken, map[string]string{"token": record.PendingReturnToken}, nil)
			if e == nil {
				_ = s.store.edit(id, func(current *deviceRecord) {
					if current.PendingReturnToken == record.PendingReturnToken {
						current.PendingReturnToken = ""
					}
				})
			}
		}
	}
}
