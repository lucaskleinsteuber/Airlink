package server

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

type deviceRecord struct {
	Device             model.Device `json:"device"`
	TokenHash          string       `json:"tokenHash"`
	OutboundToken      string       `json:"outboundToken,omitempty"`
	PendingReturnToken string       `json:"pendingReturnToken,omitempty"`
}
type persisted struct {
	ID       string                   `json:"id"`
	Settings model.Settings           `json:"settings"`
	Devices  map[string]deviceRecord  `json:"devices"`
	Pairings map[string]pairingRecord `json:"pairings,omitempty"`
}
type stateStore struct {
	sync.Mutex
	path string
	data persisted
}

func openStore(dir, receive string) (*stateStore, error) {
	s := &stateStore{path: filepath.Join(dir, "settings.json")}
	b, err := os.ReadFile(s.path)
	if err == nil {
		if err = json.Unmarshal(b, &s.data); err != nil {
			return nil, err
		}
		var presence struct {
			Settings map[string]json.RawMessage `json:"settings"`
		}
		_ = json.Unmarshal(b, &presence)
		if _, exists := presence.Settings["shortcut"]; !exists {
			s.data.Settings.Shortcut = model.Shortcut{Enabled: true, KeyCode: 37, Modifiers: 768}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if s.data.ID == "" {
		name := initialDeviceName()
		s.data = persisted{ID: randomID(), Settings: model.Settings{Name: name, ReceiveDirectory: receive, Appearance: "system", Language: "system", Notifications: true}, Devices: map[string]deviceRecord{}}
		s.data.Settings.Shortcut = model.Shortcut{Enabled: true, KeyCode: 37, Modifiers: 768}
	}
	if s.data.Devices == nil {
		s.data.Devices = map[string]deviceRecord{}
	}
	if s.data.Pairings == nil {
		s.data.Pairings = map[string]pairingRecord{}
	}
	if s.data.Settings.Organization.Rules == nil {
		s.data.Settings.Organization.Rules = []model.SortRule{}
	}
	if receive != "" {
		s.data.Settings.ReceiveDirectory = receive
	}
	if s.data.Settings.ReceiveDirectory == "" {
		s.data.Settings.ReceiveDirectory = defaultReceiveDirectory()
	}
	return s, s.save()
}

func fallbackReceiveDirectory() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

func initialDeviceName() string {
	name, _ := os.Hostname()
	name = strings.TrimSpace(strings.SplitN(name, ".", 2)[0])
	if name != "" && len(name) <= 40 && !strings.Contains(name, "-") {
		return name
	}
	switch runtime.GOOS {
	case "darwin":
		return "Mac"
	case "windows":
		return "Windows-PC"
	default:
		return "Linux-PC"
	}
}
func (s *stateStore) save() error {
	b, e := json.MarshalIndent(s.data, "", "  ")
	if e != nil {
		return e
	}
	return atomicFile(s.path, b)
}
func (s *stateStore) snapshot() persisted {
	s.Lock()
	defer s.Unlock()
	p := s.data
	p.Settings.Organization.Rules = append([]model.SortRule{}, s.data.Settings.Organization.Rules...)
	for i := range p.Settings.Organization.Rules {
		p.Settings.Organization.Rules[i].Extensions = append([]string{}, p.Settings.Organization.Rules[i].Extensions...)
	}
	p.Devices = map[string]deviceRecord{}
	for k, v := range s.data.Devices {
		p.Devices[k] = v
	}
	p.Pairings = map[string]pairingRecord{}
	for k, v := range s.data.Pairings {
		p.Pairings[k] = v
	}
	return p
}
func (s *stateStore) put(r deviceRecord) error {
	s.Lock()
	defer s.Unlock()
	s.data.Devices[r.Device.ID] = r
	return s.save()
}
func (s *stateStore) edit(id string, fn func(*deviceRecord)) error {
	s.Lock()
	defer s.Unlock()
	r, ok := s.data.Devices[id]
	if !ok {
		return errors.New("device no longer paired")
	}
	fn(&r)
	s.data.Devices[id] = r
	return s.save()
}
func (s *stateStore) authenticate(token string) (model.Device, bool) {
	s.Lock()
	defer s.Unlock()
	h := digest([]byte(token))
	for k, r := range s.data.Devices {
		if r.TokenHash == h && token != "" {
			r.Device.LastSeen = time.Now().UTC().Format(time.RFC3339)
			r.Device.Online = true
			s.data.Devices[k] = r
			return r.Device, true
		}
	}
	return model.Device{}, false
}
func (s *stateStore) updateDevice(d model.Device) error {
	s.Lock()
	defer s.Unlock()
	r, ok := s.data.Devices[d.ID]
	if !ok {
		return errors.New("device not found")
	}
	r.Device.CanSend = d.CanSend
	r.Device.CanReceive = d.CanReceive
	r.Device.CanClipboard = d.CanClipboard
	r.Device.CanRelay = d.CanRelay
	r.Device.AutoAccept = d.AutoAccept
	if d.Name != "" {
		r.Device.Name = d.Name
	}
	s.data.Devices[d.ID] = r
	return s.save()
}
func (s *stateStore) revoke(id string) error {
	s.Lock()
	defer s.Unlock()
	delete(s.data.Devices, id)
	for key, p := range s.data.Pairings {
		if p.Peer.ID == id {
			delete(s.data.Pairings, key)
		}
	}
	return s.save()
}
