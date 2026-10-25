package server

import (
	"context"
	"log"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/grandcat/zeroconf"
	"github.com/lucasfd3/Airlink/internal/model"
)

const discoveryRefreshInterval = 15 * time.Second
const discoveryFreshness = 45 * time.Second

func (s *Server) discovery(port int) {
	p := s.store.snapshot()
	ctx, cancel := context.WithCancel(s.ctx)
	s.stopDiscovery = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		// A registration owns immutable TXT data. The dependency's SetText
		// mutates a slice read by its responder, so replace registrations from
		// this single owner instead when the user changes the display name.
		manageDiscoveryAdvertisements(ctx, s.discoveryNames, discoveryRefreshInterval, func() string { return s.store.snapshot().Settings.Name }, func(name string) (func(), error) {
			registration, err := zeroconf.Register(discoveryInstanceName(name, p.ID), "_airlink._tcp", "local.", port,
				[]string{"id=" + p.ID, "name=" + name, "fingerprint=" + s.fingerprint, "version=" + model.Version}, nil)
			if err != nil {
				log.Printf("Device discovery advertisement unavailable: %v; use QR or address", err)
				return nil, err
			}
			return registration.Shutdown, nil
		})
	}()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		// zeroconf v1 keeps a sentEntries cache for the lifetime of a Browse.
		// Fresh windows are necessary: a live advertisement must refresh LastSeen,
		// rather than expiring forever after its first delivery to this process.
		browseDiscoveryWindows(ctx, discoveryRefreshInterval, func(window context.Context, entries chan<- *zeroconf.ServiceEntry) error {
			resolver, err := zeroconf.NewResolver(nil)
			if err != nil {
				close(entries)
				return err
			}
			return resolver.Browse(window, "_airlink._tcp", "local.", entries)
		}, func(entry *zeroconf.ServiceEntry) { s.recordDiscovered(entry, p.ID, time.Now()) })
	}()
}

func validDiscoveryName(name string) bool {
	return utf8.ValidString(name) && len(name) > 0 && len(name) <= 120 && strings.TrimSpace(name) == name && !strings.ContainsFunc(name, unicode.IsControl)
}

func discoveryInstanceName(name, id string) string {
	if !validDiscoveryName(name) {
		name = "AirLink"
	}
	for len(name) > 40 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	if len(id) > 8 {
		id = id[:8]
	}
	return name + "-" + id
}

func (s *Server) refreshDiscoveryName() {
	select {
	case s.discoveryNames <- struct{}{}:
	default:
	}
}

// The owner alone starts/stops advertisements. Repeated name events coalesce;
// failed registrations retry at a bounded interval instead of a busy loop.
func manageDiscoveryAdvertisements(ctx context.Context, changed <-chan struct{}, retryInterval time.Duration, currentName func() string, register func(string) (func(), error)) {
	ticker := time.NewTicker(retryInterval)
	defer ticker.Stop()
	var stop func()
	var activeName string
	defer func() {
		if stop != nil {
			stop()
		}
	}()
	for {
		if ctx.Err() != nil {
			return
		}
		name := currentName()
		if !validDiscoveryName(name) {
			name = "AirLink"
		}
		if stop == nil || name != activeName {
			if stop != nil {
				stop()
				stop = nil
			}
			registrationStop, err := register(name)
			if err == nil {
				stop = registrationStop
				activeName = name
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-changed:
		case <-ticker.C:
		}
	}
}

func browseDiscoveryWindows(ctx context.Context, interval time.Duration, browse func(context.Context, chan<- *zeroconf.ServiceEntry) error, receive func(*zeroconf.ServiceEntry)) {
	for ctx.Err() == nil {
		window, cancel := context.WithTimeout(ctx, interval)
		entries := make(chan *zeroconf.ServiceEntry, 32)
		_ = browse(window, entries)
		// The browser owns closing entries, including failed startup. Keep
		// draining after cancellation: the library's delivery send does not
		// select on cancellation, even when the initial query returns an error.
		for entry := range entries {
			if window.Err() == nil {
				receive(entry)
			}
		}
		// A failed resolver or an early-closing query must not spin in a retry loop.
		<-window.Done()
		cancel()
	}
}

func (s *Server) recordDiscovered(entry *zeroconf.ServiceEntry, selfID string, at time.Time) {
	if entry == nil {
		return
	}
	values := map[string]string{}
	for _, txt := range entry.Text {
		parts := strings.SplitN(txt, "=", 2)
		if len(parts) == 2 {
			values[parts[0]] = parts[1]
		}
	}
	id := values["id"]
	if id == "" || id == selfID || !validDiscoveryName(values["name"]) {
		return
	}
	var ip net.IP
	if len(entry.AddrIPv4) > 0 {
		ip = entry.AddrIPv4[0]
	} else if len(entry.AddrIPv6) > 0 {
		ip = entry.AddrIPv6[0]
	}
	if ip == nil || entry.Port < 1 || entry.Port > 65535 {
		return
	}
	d := model.Device{ID: id, Name: values["name"], Kind: "engine", Address: "https://" + net.JoinHostPort(ip.String(), strconv.Itoa(entry.Port)), Fingerprint: values["fingerprint"], Online: true, LastSeen: at.UTC().Format(time.RFC3339Nano)}
	s.mu.Lock()
	s.discovered[id] = d
	s.mu.Unlock()
	if rec, ok := s.store.snapshot().Devices[id]; ok && rec.Device.Fingerprint == d.Fingerprint && rec.Device.Kind == "engine" {
		_ = s.store.edit(id, func(current *deviceRecord) {
			if current.Device.Fingerprint == d.Fingerprint {
				current.Device.Name = d.Name
				current.Device.Address = d.Address
				current.Device.LastSeen = d.LastSeen
				current.Device.Online = true
			}
		})
	}
}

// The caller holds s.mu. Discovery is an expiring connection hint, never a trust
// record. Paired devices remain in the separate persistent store when offline.
func (s *Server) discoveredDevicesLocked(known map[string]bool, at time.Time) []model.Device {
	devices := []model.Device{}
	for id, device := range s.discovered {
		seen, err := time.Parse(time.RFC3339Nano, device.LastSeen)
		age := at.Sub(seen)
		if err != nil || age < 0 || age >= discoveryFreshness {
			delete(s.discovered, id)
			continue
		}
		if !known[id] {
			device.Online = true
			devices = append(devices, device)
		}
	}
	return devices
}
