package server

import (
	"context"
	"strings"
	"time"
)

func (s *Server) heartbeatLoop() {
	defer s.wg.Done()
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-tick.C:
		}
		for id, record := range s.store.snapshot().Devices {
			if record.Device.Kind != "engine" || record.Device.Fingerprint == "" {
				continue
			}
			ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
			var info struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			e := requestJSON(ctx, pinnedClient(record.Device.Fingerprint), "GET", strings.TrimRight(record.Device.Address, "/")+"/api/v1/info", record.OutboundToken, nil, &info)
			cancel()
			if e == nil && info.ID == id {
				_ = s.store.edit(id, func(current *deviceRecord) {
					if current.Device.Fingerprint != record.Device.Fingerprint || current.Device.Kind != "engine" {
						return
					}
					if validDiscoveryName(info.Name) {
						current.Device.Name = info.Name
					}
					current.Device.LastSeen = time.Now().UTC().Format(time.RFC3339)
					current.Device.Online = true
				})
			}
		}
	}
}
