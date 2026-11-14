package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
	"github.com/lucaskleinsteuber/Airlink/internal/peer"
)

const clipboardMaxBytes = 64 * 1024
const clipboardRetention = time.Hour

func (s *Server) clipboardLoop() {
	defer s.wg.Done()
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	defer func() { s.clipboardMu.Lock(); s.clipboardMessages = nil; s.clipboardMu.Unlock() }()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-tick.C:
			s.clipboardSnapshot("")
		}
	}
}

func validateClipboardText(text string) error {
	if len(text) == 0 || len(text) > clipboardMaxBytes || !utf8.ValidString(text) {
		return errors.New("clipboard text must contain 1–65536 UTF-8 bytes")
	}
	return nil
}
func (s *Server) clipboardSnapshot(deviceID string) []model.ClipboardMessage {
	s.clipboardMu.Lock()
	defer s.clipboardMu.Unlock()
	out := []model.ClipboardMessage{}
	retained := []model.ClipboardMessage{}
	now := time.Now()
	for _, message := range s.clipboardMessages {
		created, e := time.Parse(time.RFC3339Nano, message.CreatedAt)
		if e != nil || now.Sub(created) >= clipboardRetention {
			continue
		}
		retained = append(retained, message)
		if deviceID == "" || message.DeviceID == deviceID {
			out = append(out, message)
		}
	}
	s.clipboardMessages = retained
	return out
}
func (s *Server) recordClipboard(d model.Device, direction, id, text string) (model.ClipboardMessage, error) {
	if e := validateClipboardText(text); e != nil {
		return model.ClipboardMessage{}, e
	}
	if id == "" {
		id = randomID()
	}
	if len(id) > 100 {
		return model.ClipboardMessage{}, errors.New("invalid clipboard message id")
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return model.ClipboardMessage{}, errors.New("invalid clipboard message id")
		}
	}
	s.store.Lock()
	defer s.store.Unlock()
	current, paired := s.store.data.Devices[d.ID]
	if !paired || !current.Device.CanClipboard {
		return model.ClipboardMessage{}, errors.New("clipboard permission was revoked")
	}
	d = current.Device
	s.clipboardMu.Lock()
	defer s.clipboardMu.Unlock()
	for _, existing := range s.clipboardMessages {
		if existing.ID == id {
			if existing.DeviceID != d.ID || existing.Direction != direction || existing.Text != text {
				return model.ClipboardMessage{}, errors.New("clipboard message id already used")
			}
			return existing, nil
		}
	}
	message := model.ClipboardMessage{ID: id, DeviceID: d.ID, DeviceName: d.Name, Direction: direction, Text: text, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	s.clipboardMessages = append(s.clipboardMessages, message)
	if len(s.clipboardMessages) > 100 {
		s.clipboardMessages = append([]model.ClipboardMessage{}, s.clipboardMessages[len(s.clipboardMessages)-100:]...)
	}
	return message, nil
}
func (s *Server) dismissClipboard(id, deviceID string) error {
	s.clipboardMu.Lock()
	defer s.clipboardMu.Unlock()
	for i, message := range s.clipboardMessages {
		if message.ID == id && (deviceID == "" || message.DeviceID == deviceID) {
			s.clipboardMessages = append(s.clipboardMessages[:i], s.clipboardMessages[i+1:]...)
			return nil
		}
	}
	return errors.New("clipboard message not found")
}
func (s *Server) dismissDeviceClipboard(deviceID string) {
	s.clipboardMu.Lock()
	defer s.clipboardMu.Unlock()
	retained := []model.ClipboardMessage{}
	for _, message := range s.clipboardMessages {
		if message.DeviceID != deviceID {
			retained = append(retained, message)
		}
	}
	s.clipboardMessages = retained
}

type clipboardMessage struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Text string `json:"text"`
}

type clipboardAck struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Error string `json:"error,omitempty"`
}

// sendClipboard delivers text over the device's session and records it once
// the other device accepted it. Both devices must allow clipboard text.
func (s *Server) sendClipboard(deviceID, text string) (model.ClipboardMessage, error) {
	if e := validateClipboardText(text); e != nil {
		return model.ClipboardMessage{}, e
	}
	record, ok := s.store.snapshot().Devices[deviceID]
	if !ok || !record.Device.Paired || !record.Device.CanClipboard {
		return model.ClipboardMessage{}, errors.New("enable clipboard permission for this paired device first")
	}
	session := s.peers.Get(deviceID)
	if session == nil {
		s.requestConnection(deviceID)
		return model.ClipboardMessage{}, errors.New("the other device is not connected; open AirLink on it and try again")
	}
	id := randomID()
	reply := make(chan string, 1)
	s.clipboardMu.Lock()
	s.clipboardWaits[id] = reply
	s.clipboardMu.Unlock()
	defer func() {
		s.clipboardMu.Lock()
		delete(s.clipboardWaits, id)
		s.clipboardMu.Unlock()
	}()
	if err := session.Send(clipboardMessage{Type: "clipboard", ID: id, Text: text}); err != nil {
		return model.ClipboardMessage{}, fmt.Errorf("clipboard delivery failed: %w", err)
	}
	select {
	case problem := <-reply:
		if problem != "" {
			return model.ClipboardMessage{}, fmt.Errorf("the other device did not accept the text: %s", problem)
		}
	case <-session.Done():
		return model.ClipboardMessage{}, errors.New("clipboard delivery failed: connection interrupted")
	case <-time.After(10 * time.Second):
		return model.ClipboardMessage{}, errors.New("clipboard delivery failed: no answer from the other device")
	}
	fresh, ok := s.store.snapshot().Devices[deviceID]
	if !ok || !fresh.Device.CanClipboard {
		return model.ClipboardMessage{}, errors.New("clipboard permission was revoked")
	}
	return s.recordClipboard(fresh.Device, "send", id, text)
}

func (s *Server) receiveClipboard(session *peer.Session, e peer.Envelope) {
	var m clipboardMessage
	if e.Decode(&m) != nil {
		return
	}
	ack := clipboardAck{Type: "clipboard.ack", ID: m.ID}
	record, ok := s.store.snapshot().Devices[session.PeerID()]
	if !ok || !record.Device.Paired || !record.Device.CanClipboard {
		ack.Error = "clipboard text is not allowed from this device"
	} else if _, err := s.recordClipboard(record.Device, "receive", m.ID, m.Text); err != nil {
		ack.Error = err.Error()
	} else {
		s.changed()
	}
	if len(ack.ID) > 100 {
		return
	}
	_ = session.Send(ack)
}

func (s *Server) clipboardAcknowledged(e peer.Envelope) {
	var m clipboardAck
	if e.Decode(&m) != nil {
		return
	}
	s.clipboardMu.Lock()
	reply := s.clipboardWaits[m.ID]
	s.clipboardMu.Unlock()
	if reply != nil {
		select {
		case reply <- m.Error:
		default:
		}
	}
}

func (s *Server) clipboardControl(method string, params json.RawMessage) (any, error) {
	switch method {
	case "clipboard.list":
		return s.clipboardSnapshot(""), nil
	case "clipboard.send":
		var v struct {
			DeviceID string `json:"deviceId"`
			Text     string `json:"text"`
		}
		if e := json.Unmarshal(params, &v); e != nil {
			return nil, e
		}
		return s.sendClipboard(v.DeviceID, v.Text)
	case "clipboard.dismiss":
		var v struct {
			ID string `json:"id"`
		}
		if e := json.Unmarshal(params, &v); e != nil {
			return nil, e
		}
		return map[string]bool{"ok": true}, s.dismissClipboard(v.ID, "")
	default:
		return nil, errors.New("unknown clipboard method")
	}
}
