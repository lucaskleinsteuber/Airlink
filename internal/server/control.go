package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
	"github.com/lucaskleinsteuber/Airlink/internal/traffic"
)

type rpcRequest struct {
	ID     any             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func (s *Server) Control(method string, params json.RawMessage) (any, error) {
	if strings.HasPrefix(method, "pairing.") {
		return s.pairingControl(method, params)
	}
	if strings.HasPrefix(method, "preparation.") {
		return s.preparationControl(method, params)
	}
	if strings.HasPrefix(method, "clipboard.") {
		return s.clipboardControl(method, params)
	}
	if strings.HasPrefix(method, "history.") {
		return s.historyControl(method, params)
	}
	var id struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(params, &id)
	switch method {
	case "status":
		return s.Status(), nil
	case "diagnostics.export":
		return s.diagnostics(), nil
	case "status.summary":
		return s.summaryStatus(), nil
	case "statistics.get":
		return s.statistics(""), nil
	case "transfer.files":
		var v struct {
			ID     string `json:"id"`
			Offset int    `json:"offset"`
			Limit  int    `json:"limit"`
		}
		if err := json.Unmarshal(params, &v); err != nil {
			return nil, err
		}
		return s.engine.FilePage(v.ID, v.Offset, v.Limit)
	case "transfer.get":
		return s.engine.Get(id.ID)
	case "device.update":
		var d model.Device
		if e := json.Unmarshal(params, &d); e != nil {
			return nil, e
		}
		if err := s.store.updateDevice(d); err != nil {
			return nil, err
		}
		s.pauseDisallowedTransfers(d)
		if !d.CanReceive {
			s.cancelDevicePreparations(d.ID)
		}
		return map[string]bool{"ok": true}, nil
	case "device.revoke":
		if err := s.store.revoke(id.ID); err != nil {
			return nil, err
		}
		s.cancelDevicePreparations(id.ID)
		s.dismissDeviceClipboard(id.ID)
		s.cancelDeviceRequests(id.ID)
		for _, t := range s.engine.Transfers(id.ID) {
			if t.State != "completed" && t.State != "cancelled" {
				_ = s.engine.Cancel(t.ID)
			}
		}
		return map[string]bool{"ok": true}, nil
	case "transfer.accept":
		return map[string]bool{"ok": true}, s.engine.Accept(id.ID)
	case "transfer.acceptTrusted":
		return s.acceptTrusted(id.ID)
	case "transfer.received":
		return s.engine.ReceivedItems(id.ID)
	case "transfer.pause":
		s.stopSender(id.ID)
		err := s.engine.Pause(id.ID)
		s.stopTransferRequests(id.ID)
		return map[string]bool{"ok": true}, err
	case "transfer.resume":
		err := s.engine.Resume(id.ID)
		if err == nil {
			s.wakeOutgoing()
		}
		return map[string]bool{"ok": true}, err
	case "transfer.cancel":
		s.stopSender(id.ID)
		err := s.engine.Cancel(id.ID)
		s.stopTransferRequests(id.ID)
		return map[string]bool{"ok": true}, err
	case "offer":
		return s.offerControl(s.ctx, params)
	case "settings.update":
		s.settingsMu.Lock()
		defer s.settingsMu.Unlock()
		previous := s.store.snapshot().Settings
		v := previous
		if e := json.Unmarshal(params, &v); e != nil {
			return nil, e
		}
		if strings.TrimSpace(v.Name) == "" || len(v.Name) > 120 {
			return nil, errors.New("invalid device name")
		}
		if v.Appearance != "system" && v.Appearance != "light" && v.Appearance != "dark" {
			return nil, errors.New("invalid appearance")
		}
		if err := validateShortcut(v.Shortcut); err != nil {
			return nil, err
		}
		if !traffic.ValidLimit(v.SendLimitBytesPerSecond) || !traffic.ValidLimit(v.ReceiveLimitBytesPerSecond) {
			return nil, errors.New("speed limits must be zero or at least1024 bytes/second")
		}
		// Duplicate detection and extension sorting were removed in 0.13.
		v.Organization = model.Organization{Rules: []model.SortRule{}}
		v.Name = strings.TrimSpace(v.Name)
		s.store.Lock()
		e := s.engine.ConfigureIncoming(v.ReceiveDirectory, v.Organization, func() error {
			s.store.data.Settings = v
			if err := s.store.save(); err != nil {
				s.store.data.Settings = previous
				return err
			}
			return nil
		})
		s.store.Unlock()
		if e != nil {
			return nil, e
		}
		e = s.traffic.SetLimits(v.SendLimitBytesPerSecond, v.ReceiveLimitBytesPerSecond)
		if e == nil && previous.Name != v.Name {
			s.refreshDiscoveryName()
		}
		return v, e
	case "shutdown":
		return map[string]bool{"ok": true}, nil
	default:
		return nil, errors.New("unknown control method")
	}
}
func (s *Server) Stdio(in io.Reader, out io.Writer) error {
	ctx, cancel := context.WithCancel(s.ctx)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 4096), 2<<20)
	enc := json.NewEncoder(out)
	var writeMu sync.Mutex
	var outputError error
	write := func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		if outputError == nil {
			outputError = enc.Encode(v)
		}
		return outputError
	}
	offerSlots := make(chan struct{}, 2)
	for scan.Scan() {
		var req rpcRequest
		if err := json.Unmarshal(scan.Bytes(), &req); err != nil {
			if e := write(map[string]any{"id": nil, "error": err.Error()}); e != nil {
				return e
			}
			continue
		}
		if req.Method == "offer" {
			select {
			case offerSlots <- struct{}{}:
				workers.Add(1)
				go func(req rpcRequest) {
					defer workers.Done()
					defer func() { <-offerSlots }()
					result, err := s.offerControl(ctx, req.Params)
					reply := map[string]any{"id": req.ID}
					if err != nil {
						reply["error"] = err.Error()
					} else {
						reply["result"] = result
					}
					_ = write(reply)
				}(req)
			default:
				if e := write(map[string]any{"id": req.ID, "error": "two source selections are already being prepared"}); e != nil {
					return e
				}
			}
			continue
		}
		result, err := s.Control(req.Method, req.Params)
		reply := map[string]any{"id": req.ID}
		if err != nil {
			reply["error"] = err.Error()
		} else {
			reply["result"] = result
		}
		if e := write(reply); e != nil {
			return e
		}
		if req.Method == "shutdown" && err == nil {
			return nil
		}
	}
	return scan.Err()
}
