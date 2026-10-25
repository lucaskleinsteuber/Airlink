package server

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/grandcat/zeroconf"
	"github.com/lucasfd3/Airlink/internal/model"
)

func discoveryEntry(id, fingerprint, address string) *zeroconf.ServiceEntry {
	return &zeroconf.ServiceEntry{ServiceRecord: zeroconf.ServiceRecord{Instance: "AirLink-test"}, Port: 43871,
		Text: []string{"id=" + id, "name=Nearby computer", "fingerprint=" + fingerprint}, AddrIPv4: []net.IP{net.ParseIP(address)}}
}

func TestDiscoveryHintsExpireWithoutRemovingPairedTrust(t *testing.T) {
	s := integrationHost(t)
	at := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	paired, _ := integrationDevice(t, s, true)
	paired.Kind = "engine"
	paired.Fingerprint = "paired-identity"
	paired.Address = "https://192.168.1.10:43871"
	paired.LastSeen = at.Add(-time.Hour).Format(time.RFC3339Nano)
	if err := s.store.edit(paired.ID, func(record *deviceRecord) { record.Device = paired }); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.discovered = map[string]model.Device{
		"nearby":    {ID: "nearby", LastSeen: at.Format(time.RFC3339Nano)},
		"gone":      {ID: "gone", LastSeen: at.Add(-discoveryFreshness).Format(time.RFC3339Nano)},
		"bad-clock": {ID: "bad-clock", LastSeen: at.Add(time.Minute).Format(time.RFC3339Nano)},
		"invalid":   {ID: "invalid", LastSeen: "invalid"},
		paired.ID:   paired,
	}
	live := s.discoveredDevicesLocked(map[string]bool{paired.ID: true}, at)
	if len(live) != 1 || live[0].ID != "nearby" || !live[0].Online {
		t.Fatal("fresh discovery hint missing or stale hints retained", live)
	}
	if len(s.discovered) != 1 {
		t.Fatal("expired hints were not pruned", s.discovered)
	}
	if got := s.discoveredDevicesLocked(nil, at.Add(discoveryFreshness)); len(got) != 0 {
		t.Fatal("hint remains after the full freshness interval", got)
	}
	s.mu.Unlock()
	record, ok := s.store.snapshot().Devices[paired.ID]
	if !ok || !record.Device.AutoAccept || !record.Device.Paired || record.Device.Fingerprint != paired.Fingerprint {
		t.Fatal("pruning discovery erased persistent pairing or receipt permission", record)
	}
}

func TestRepeatedDiscoveryRefreshesHintsWithoutTrustingAnUnpairedIdentity(t *testing.T) {
	s := integrationHost(t)
	at := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	entry := discoveryEntry("unpaired", "discovered-fingerprint", "192.168.1.12")
	s.recordDiscovered(entry, s.store.snapshot().ID, at)
	s.recordDiscovered(entry, s.store.snapshot().ID, at.Add(discoveryFreshness-time.Second))
	s.mu.Lock()
	got := s.discoveredDevicesLocked(nil, at.Add(discoveryFreshness+time.Second))
	s.mu.Unlock()
	if len(got) != 1 || got[0].ID != "unpaired" || got[0].Paired || got[0].CanSend || got[0].CanReceive || got[0].AutoAccept {
		t.Fatal("renewed hint disappeared or silently granted rights", got)
	}
	if len(s.store.snapshot().Devices) != 0 {
		t.Fatal("discovery created persistent trust")
	}
	entry.Port = 0
	s.recordDiscovered(entry, s.store.snapshot().ID, at.Add(time.Hour))
	if s.discovered["unpaired"].LastSeen != at.Add(discoveryFreshness-time.Second).Format(time.RFC3339Nano) {
		t.Fatal("malformed advertisement refreshed a hint")
	}
}

func TestDiscoveryRefreshUpdatesOnlyTheExistingPinnedIdentity(t *testing.T) {
	s := integrationHost(t)
	at := time.Now()
	device, _ := integrationDevice(t, s, true)
	device.Kind = "engine"
	device.Address = "https://192.168.1.10:43871"
	device.Fingerprint = "pinned-fingerprint"
	if err := s.store.edit(device.ID, func(record *deviceRecord) { record.Device = device }); err != nil {
		t.Fatal(err)
	}
	s.recordDiscovered(discoveryEntry(device.ID, "wrong-fingerprint", "192.168.1.99"), s.store.snapshot().ID, at)
	if got := s.store.snapshot().Devices[device.ID].Device; got.Address != device.Address || got.Fingerprint != device.Fingerprint || got.Name != device.Name {
		t.Fatal("untrusted advertisement replaced a paired address or certificate", got)
	}
	renamed := discoveryEntry(device.ID, device.Fingerprint, "192.168.1.20")
	renamed.Text[1] = "name=Mein MacBook"
	s.recordDiscovered(renamed, s.store.snapshot().ID, at)
	got := s.store.snapshot().Devices[device.ID].Device
	if got.Address != "https://192.168.1.20:43871" || got.Name != "Mein MacBook" || got.Fingerprint != device.Fingerprint || !got.AutoAccept || !got.CanSend || !got.CanReceive {
		t.Fatal("legitimate address refresh lost trust or permissions", got)
	}
	before := len(s.discovered)
	s.recordDiscovered(discoveryEntry(s.store.snapshot().ID, "self", "192.168.1.3"), s.store.snapshot().ID, at)
	if len(s.discovered) != before {
		t.Fatal("installation discovered itself")
	}
}

func TestDiscoveryRejectsInvalidDisplayNamesWithoutChangingPairedDevices(t *testing.T) {
	s := integrationHost(t)
	device, _ := integrationDevice(t, s, true)
	device.Kind = "engine"
	device.Fingerprint = "saved-fingerprint"
	if err := s.store.edit(device.ID, func(record *deviceRecord) { record.Device = device }); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", " leading", "trailing ", "line\nbreak", "null\x00byte", strings.Repeat("a", 121), string([]byte{0xff})} {
		entry := discoveryEntry(device.ID, device.Fingerprint, "192.168.1.50")
		entry.Text[1] = "name=" + name
		s.recordDiscovered(entry, s.store.snapshot().ID, time.Now())
		if got := s.store.snapshot().Devices[device.ID].Device; got.Name != device.Name || got.Address != device.Address {
			t.Fatal("invalid display name changed a paired device", name, got)
		}
		if len(s.discovered) != 0 {
			t.Fatal("invalid name entered nearby cache")
		}
	}
	name := strings.Repeat("電", 40)
	instance := discoveryInstanceName(name, "0123456789abcdef")
	if !utf8.ValidString(instance) || len(instance) > 49 || !strings.HasSuffix(instance, "-01234567") {
		t.Fatal("instance truncation split UTF-8 or lost identity suffix", instance)
	}
}

func TestDiscoveryAdvertisementRenameHasOneOwnerAndStopsPreviousRegistration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changed := make(chan struct{}, 1)
	s := &Server{discoveryNames: changed}
	var name atomic.Value
	name.Store("MacBook")
	var mu sync.Mutex
	events := []string{}
	registered := make(chan string, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		manageDiscoveryAdvertisements(ctx, changed, 10*time.Millisecond, func() string { return name.Load().(string) }, func(current string) (func(), error) {
			mu.Lock()
			events = append(events, "start:"+current)
			mu.Unlock()
			registered <- current
			return func() { mu.Lock(); events = append(events, "stop:"+current); mu.Unlock() }, nil
		})
	}()
	wait := func(expected string) {
		t.Helper()
		select {
		case got := <-registered:
			if got != expected {
				t.Fatal(got, expected)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("advertisement did not register", expected)
		}
	}
	wait("MacBook")
	name.Store("Mein MacBook")
	for range 100 {
		s.refreshDiscoveryName()
	}
	wait("Mein MacBook")
	// Repeated settings saves and retry ticks must not restart unchanged data.
	for range 100 {
		s.refreshDiscoveryName()
	}
	time.Sleep(35 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("advertisement owner failed to stop")
	}
	mu.Lock()
	defer mu.Unlock()
	expected := []string{"start:MacBook", "stop:MacBook", "start:Mein MacBook", "stop:Mein MacBook"}
	if strings.Join(events, "|") != strings.Join(expected, "|") {
		t.Fatal("overlapping registration or repeated shutdown", events)
	}
}

func TestDiscoveryAdvertisementFailureRetriesAtBoundedInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	started := time.Now()
	manageDiscoveryAdvertisements(ctx, nil, 10*time.Millisecond, func() string { return "Laptop Windows" }, func(string) (func(), error) {
		if calls.Add(1) == 3 {
			cancel()
		}
		return nil, errors.New("fixture registration failed")
	})
	if calls.Load() != 3 || time.Since(started) < 15*time.Millisecond {
		t.Fatal("advertisement retries spun without a delay", calls.Load(), time.Since(started))
	}
}

func TestDiscoveryBrowseWindowsRenewDeduplicatedHostsAndStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var windows, delivered atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		browseDiscoveryWindows(ctx, 10*time.Millisecond, func(window context.Context, entries chan<- *zeroconf.ServiceEntry) error {
			windows.Add(1)
			go func() {
				defer close(entries)
				// Like the library, each Browse emits this host only once.
				entries <- discoveryEntry("same-host", "identity", "192.168.1.10")
				<-window.Done()
			}()
			return nil
		}, func(*zeroconf.ServiceEntry) {
			if delivered.Add(1) == 3 {
				cancel()
			}
		})
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("discovery windows did not refresh or stop")
	}
	if delivered.Load() != 3 || windows.Load() != 3 {
		t.Fatal("live host was not redelivered by fresh browse windows", delivered.Load(), windows.Load())
	}
}

func TestDiscoveryCancellationDrainsBlockedLibraryDelivery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		browseDiscoveryWindows(ctx, time.Hour, func(window context.Context, entries chan<- *zeroconf.ServiceEntry) error {
			go func() {
				defer close(entries)
				for range 128 {
					entries <- discoveryEntry("host", "identity", "192.168.1.10")
				}
			}()
			return nil
		}, func(*zeroconf.ServiceEntry) { cancel() })
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancellation left the library blocked on entry delivery")
	}
	// Failed startup still waits for the next window, instead of spinning.
	ctx, cancel = context.WithCancel(context.Background())
	var attempts atomic.Int32
	done = make(chan struct{})
	go func() {
		defer close(done)
		browseDiscoveryWindows(ctx, time.Hour, func(_ context.Context, entries chan<- *zeroconf.ServiceEntry) error {
			attempts.Add(1)
			close(entries)
			cancel()
			return errors.New("multicast unavailable")
		}, func(*zeroconf.ServiceEntry) { t.Error("failed browse returned an entry") })
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled startup did not stop")
	}
	if attempts.Load() != 1 {
		t.Fatal("failed startup spun", attempts.Load())
	}
}
