package server

import (
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/engine"
	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

type historyFilter struct {
	Query     string `json:"query"`
	State     string `json:"state"`
	Direction string `json:"direction"`
	DeviceID  string `json:"deviceId"`
	Limit     int    `json:"limit"`
}

func validateShortcut(s model.Shortcut) error {
	if !s.Enabled {
		return nil
	}
	const allowed uint32 = 256 | 512 | 2048 | 4096
	if s.KeyCode > 127 || s.KeyCode >= 54 && s.KeyCode <= 63 || s.Modifiers & ^allowed != 0 || s.Modifiers&(256|4096) == 0 {
		return errors.New("global shortcut requires Command or Control and a supported key")
	}
	return nil
}
func (s *Server) history(owner string, f historyFilter) model.History {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	limit = min(limit, 500)
	query := strings.ToLower(strings.TrimSpace(f.Query))
	out := model.History{Transfers: []model.Transfer{}}
	for _, t := range s.engine.Transfers(owner) {
		if f.State != "" && t.State != f.State || f.Direction != "" && t.Direction != f.Direction || f.DeviceID != "" && t.DeviceID != f.DeviceID {
			continue
		}
		search := t.DeviceName
		for _, file := range t.Files {
			search += " " + file.Path
		}
		if query != "" && !strings.Contains(strings.ToLower(search), query) {
			continue
		}
		out.Transfers = append(out.Transfers, t)
	}
	sort.Slice(out.Transfers, func(i, j int) bool { return out.Transfers[i].CreatedAt > out.Transfers[j].CreatedAt })
	if len(out.Transfers) > limit {
		out.Transfers = out.Transfers[:limit]
	}
	return out
}
func plus(a, b int64) int64 {
	if b < 0 {
		return a
	}
	if a > math.MaxInt64-b {
		return math.MaxInt64
	}
	return a + b
}
func (s *Server) statistics(owner string) model.Statistics {
	result := model.Statistics{Days: []model.DayStatistics{}}
	days := map[string]*model.DayStatistics{}
	day := func(created, updated string) *model.DayStatistics {
		at, e := time.Parse(time.RFC3339Nano, updated)
		if e != nil {
			at, e = time.Parse(time.RFC3339Nano, created)
		}
		key := "unknown"
		if e == nil {
			key = at.UTC().Format("2006-01-02")
		}
		d := days[key]
		if d == nil {
			d = &model.DayStatistics{Date: key}
			days[key] = d
		}
		return d
	}
	for _, t := range s.engine.Transfers(owner) {
		d := day(t.CreatedAt, t.UpdatedAt)
		result.SkippedBytes = plus(result.SkippedBytes, t.SkippedBytes)
		for _, f := range t.Files {
			if f.Skipped {
				result.SkippedFiles++
			}
		}
		if t.Direction == "send" {
			result.SentBytes = plus(result.SentBytes, t.CompletedBytes)
			d.SentBytes = plus(d.SentBytes, t.CompletedBytes)
		} else {
			result.ReceivedBytes = plus(result.ReceivedBytes, t.CompletedBytes)
			d.ReceivedBytes = plus(d.ReceivedBytes, t.CompletedBytes)
		}
		switch t.State {
		case "completed":
			result.CompletedTransfers++
			d.CompletedTransfers++
		case "failed":
			result.FailedTransfers++
		case "cancelled":
			result.CancelledTransfers++
		default:
			result.ActiveTransfers++
		}
	}
	for _, d := range days {
		result.Days = append(result.Days, *d)
	}
	sort.Slice(result.Days, func(i, j int) bool { return result.Days[i].Date > result.Days[j].Date })
	if owner == "" {
		c := s.traffic.Snapshot()
		result.TrafficSentBytes = c.SentBytes
		result.TrafficReceivedBytes = c.ReceivedBytes
	}
	return result
}
func (s *Server) clearHistory(owner string) error {
	_, e := s.engine.ClearHistory(owner)
	return e
}
func (s *Server) historyControl(method string, params json.RawMessage) (any, error) {
	switch method {
	case "history.page":
		var q engine.SummaryQuery
		if len(params) > 0 {
			if err := json.Unmarshal(params, &q); err != nil {
				return nil, err
			}
		}
		return s.engine.SummaryPage(q)
	case "history.list":
		var f historyFilter
		if len(params) > 0 {
			if e := json.Unmarshal(params, &f); e != nil {
				return nil, e
			}
		}
		return s.history("", f), nil
	case "history.clear":
		return map[string]bool{"ok": true}, s.clearHistory("")
	case "history.remove":
		var input struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &input); err != nil {
			return nil, engine.ErrInvalid
		}
		if err := s.engine.RemoveHistory("", input.ID); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	default:
		return nil, errors.New("unknown history method")
	}
}
