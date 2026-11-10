package server

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"sort"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/engine"
	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

type preparationInput struct {
	DeviceID  string   `json:"deviceId"`
	Paths     []string `json:"paths"`
	RequestID string   `json:"requestId"`
}
type preparationJob struct {
	model.Preparation
	input    preparationInput
	cancel   context.CancelFunc
	finished time.Time
}

func (s *Server) offerControl(ctx context.Context, params json.RawMessage) (model.Transfer, error) {
	var v preparationInput
	if err := json.Unmarshal(params, &v); err != nil {
		return model.Transfer{}, err
	}
	rec, ok := s.store.snapshot().Devices[v.DeviceID]
	if !ok || !rec.Device.CanReceive {
		return model.Transfer{}, errors.New("pair an allowed receiving device first")
	}
	t, err := s.engine.OfferWithRequestIDContext(ctx, rec.Device, v.Paths, v.RequestID)
	if err == nil {
		s.wakeOutgoing()
	}
	return t, err
}

func (s *Server) preparationSnapshots() []model.Preparation {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []model.Preparation{}
	for _, j := range s.preparations {
		out = append(out, j.Preparation)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Server) cancelDevicePreparations(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.preparations {
		if j.DeviceID == id && j.State == "preparing" {
			j.cancel()
		}
	}
}

func (s *Server) preparationControl(method string, params json.RawMessage) (any, error) {
	if method != "preparation.start" {
		var v struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(params, &v) != nil {
			return nil, engine.ErrInvalid
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		j := s.preparations[v.ID]
		if j == nil {
			return nil, engine.ErrNotFound
		}
		switch method {
		case "preparation.status":
			return j.Preparation, nil
		case "preparation.cancel":
			if j.State == "preparing" {
				j.cancel()
			}
			return j.Preparation, nil
		default:
			return nil, errors.New("unknown preparation method")
		}
	}
	var input preparationInput
	if json.Unmarshal(params, &input) != nil || len(input.RequestID) != 32 || len(input.Paths) == 0 || len(input.Paths) > 10000 {
		return nil, engine.ErrInvalid
	}
	for _, c := range input.RequestID {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return nil, engine.ErrInvalid
		}
	}
	for i, p := range input.Paths {
		if !filepath.IsAbs(p) {
			return nil, engine.ErrInvalid
		}
		input.Paths[i] = filepath.Clean(p)
	}
	rec, ok := s.store.snapshot().Devices[input.DeviceID]
	if !ok || !rec.Device.CanReceive {
		return nil, errors.New("pair an allowed receiving device first")
	}
	s.mu.Lock()
	if s.preparations == nil {
		s.preparations = map[string]*preparationJob{}
	}
	active := 0
	for id, j := range s.preparations {
		if j.RequestID == input.RequestID {
			if j.input.DeviceID != input.DeviceID || !reflect.DeepEqual(j.input.Paths, input.Paths) {
				s.mu.Unlock()
				return nil, engine.ErrConflict
			}
			if j.State == "preparing" || j.State == "completed" {
				result := j.Preparation
				s.mu.Unlock()
				return result, nil
			}
		}
		if j.State == "preparing" {
			active++
		} else if time.Since(j.finished) > 5*time.Minute {
			delete(s.preparations, id)
		}
	}
	if active >= 2 || len(s.preparations) >= 64 {
		s.mu.Unlock()
		return nil, errors.New("preparation queue is full; wait for an active selection")
	}
	ctx, cancel := context.WithCancel(s.ctx)
	j := &preparationJob{Preparation: model.Preparation{ID: randomID(), RequestID: input.RequestID, DeviceID: input.DeviceID, State: "preparing"}, input: input, cancel: cancel}
	s.preparations[j.ID] = j
	result := j.Preparation
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		defer cancel()
		t, err := s.engine.OfferWithRequestIDContext(ctx, rec.Device, input.Paths, input.RequestID)
		s.mu.Lock()
		if err == nil {
			j.State, j.TransferID = "completed", t.ID
		} else if errors.Is(err, context.Canceled) {
			j.State = "cancelled"
		} else {
			j.State, j.Error = "failed", err.Error()
		}
		j.finished = time.Now()
		s.mu.Unlock()
		if err == nil {
			s.wakeOutgoing()
		}
	}()
	return result, nil
}
