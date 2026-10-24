package server

import (
	"context"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"
	"github.com/lucasfd3/Airlink/internal/model"
)

func (s *Server) discovery(port int) {
	p := s.store.snapshot()
	name := p.Settings.Name
	if len(name) > 40 {
		name = name[:40]
	}
	name += "-" + p.ID[:8]
	registration, err := zeroconf.Register(name, "_airlink._tcp", "local.", port, []string{"id=" + p.ID, "name=" + p.Settings.Name, "fingerprint=" + s.fingerprint, "version=" + model.Version}, nil)
	if err != nil {
		log.Printf("Device discovery advertisement unavailable: %v; use QR or address", err)
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.stopDiscovery = func() {
		cancel()
		if registration != nil {
			registration.Shutdown()
		}
	}
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return
	}
	entries := make(chan *zeroconf.ServiceEntry, 32)
	if err = resolver.Browse(ctx, "_airlink._tcp", "local.", entries); err != nil {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-entries:
				if !ok {
					return
				}
				values := map[string]string{}
				for _, txt := range e.Text {
					parts := strings.SplitN(txt, "=", 2)
					if len(parts) == 2 {
						values[parts[0]] = parts[1]
					}
				}
				id := values["id"]
				if id == "" || id == p.ID {
					continue
				}
				var ip net.IP
				if len(e.AddrIPv4) > 0 {
					ip = e.AddrIPv4[0]
				} else if len(e.AddrIPv6) > 0 {
					ip = e.AddrIPv6[0]
				}
				if ip == nil {
					continue
				}
				d := model.Device{ID: id, Name: values["name"], Kind: "engine", Address: "https://" + net.JoinHostPort(ip.String(), strconv.Itoa(e.Port)), Fingerprint: values["fingerprint"], Online: true, LastSeen: time.Now().UTC().Format(time.RFC3339)}
				s.mu.Lock()
				s.discovered[id] = d
				s.mu.Unlock()
				if rec, ok := s.store.snapshot().Devices[id]; ok && rec.Device.Fingerprint == d.Fingerprint && rec.Device.Kind == "engine" {
					_ = s.store.edit(id, func(current *deviceRecord) {
						if current.Device.Fingerprint == d.Fingerprint {
							current.Device.Address = d.Address
							current.Device.LastSeen = d.LastSeen
							current.Device.Online = true
						}
					})
				}
			}
		}
	}()
}
