package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
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
func (s *Server) sendClipboard(deviceID, text string) (model.ClipboardMessage, error) {
	if e := validateClipboardText(text); e != nil {
		return model.ClipboardMessage{}, e
	}
	record, ok := s.store.snapshot().Devices[deviceID]
	if !ok || !record.Device.CanClipboard {
		return model.ClipboardMessage{}, errors.New("enable clipboard permission for this paired device first")
	}
	id := randomID()
	if record.Device.Kind == "engine" {
		if record.OutboundToken == "" {
			return model.ClipboardMessage{}, errors.New("finish pairing this device first")
		}
		ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		defer cancel()
		var response model.ClipboardMessage
		err := requestJSON(ctx, pinnedClient(record.Device.Fingerprint), http.MethodPost, strings.TrimRight(record.Device.Address, "/")+"/api/v1/clipboard", record.OutboundToken, map[string]string{"id": id, "text": text}, &response)
		if err != nil {
			return model.ClipboardMessage{}, fmt.Errorf("clipboard delivery failed; enable clipboard on both devices and use AirLink 0.2 or newer: %w", err)
		}
	}
	fresh, ok := s.store.snapshot().Devices[deviceID]
	if !ok || !fresh.Device.CanClipboard {
		return model.ClipboardMessage{}, errors.New("clipboard permission was revoked")
	}
	return s.recordClipboard(fresh.Device, "send", id, text)
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
func (s *Server) handleClipboardAPI(w http.ResponseWriter, r *http.Request, d model.Device) bool {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	if path != "clipboard" && !strings.HasPrefix(path, "clipboard/") {
		return false
	}
	if strings.HasPrefix(path, "clipboard/") && r.Method == http.MethodDelete {
		id := strings.TrimPrefix(path, "clipboard/")
		if e := s.dismissClipboard(id, d.ID); e != nil {
			fail(w, 404, e)
		} else {
			respond(w, 200, map[string]bool{"ok": true})
		}
		return true
	}
	if !d.CanClipboard {
		fail(w, 403, errors.New("clipboard permission is disabled on the host"))
		return true
	}
	if path == "clipboard" && r.Method == http.MethodPost {
		var v struct {
			ID   string `json:"id,omitempty"`
			Text string `json:"text"`
		}
		if !decode(w, r, &v) {
			return true
		}
		fresh, ok := s.store.snapshot().Devices[d.ID]
		if !ok || !fresh.Device.CanClipboard {
			fail(w, 403, errors.New("clipboard permission revoked"))
			return true
		}
		message, e := s.recordClipboard(fresh.Device, "receive", v.ID, v.Text)
		if e != nil {
			fail(w, 400, e)
			return true
		}
		respond(w, 201, message)
		return true
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return true
}
