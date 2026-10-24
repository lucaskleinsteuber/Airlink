package server

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lucasfd3/Airlink/internal/model"
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
	out := model.History{Transfers: []model.Transfer{}, Relays: []model.Relay{}}
	if f.Direction != "relay" {
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
			if owner != "" {
				t = publicTransfer(t)
			}
			out.Transfers = append(out.Transfers, t)
		}
	}
	if f.Direction == "" || f.Direction == "relay" {
		for _, r := range s.relays.List(owner) {
			if f.State != "" && r.State != f.State || f.DeviceID != "" && r.SenderID != f.DeviceID && r.ReceiverID != f.DeviceID {
				continue
			}
			search := r.SenderName + " " + r.ReceiverName
			for _, file := range r.Files {
				search += " " + file.Path
			}
			if query != "" && !strings.Contains(strings.ToLower(search), query) {
				continue
			}
			if owner != "" && r.Error != "" {
				r.Error = "Relay needs attention; reconnect both browsers."
			}
			out.Relays = append(out.Relays, r)
		}
	}
	sort.Slice(out.Transfers, func(i, j int) bool { return out.Transfers[i].CreatedAt > out.Transfers[j].CreatedAt })
	sort.Slice(out.Relays, func(i, j int) bool { return out.Relays[i].CreatedAt > out.Relays[j].CreatedAt })
	type entry struct {
		date  string
		relay bool
		index int
	}
	all := []entry{}
	for i, t := range out.Transfers {
		all = append(all, entry{t.CreatedAt, false, i})
	}
	for i, r := range out.Relays {
		all = append(all, entry{r.CreatedAt, true, i})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].date > all[j].date })
	if len(all) > limit {
		keepT, keepR := map[int]bool{}, map[int]bool{}
		for _, v := range all[:limit] {
			if v.relay {
				keepR[v.index] = true
			} else {
				keepT[v.index] = true
			}
		}
		ts, rs := []model.Transfer{}, []model.Relay{}
		for i, t := range out.Transfers {
			if keepT[i] {
				ts = append(ts, t)
			}
		}
		for i, r := range out.Relays {
			if keepR[i] {
				rs = append(rs, r)
			}
		}
		out.Transfers = ts
		out.Relays = rs
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
	for _, r := range s.relays.List(owner) {
		d := day(r.CreatedAt, r.UpdatedAt)
		result.RelayedBytes = plus(result.RelayedBytes, r.DeliveredBytes)
		d.RelayedBytes = plus(d.RelayedBytes, r.DeliveredBytes)
		switch r.State {
		case "delivered":
			result.RelayDeliveries++
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
	if _, e := s.engine.ClearHistory(owner); e != nil {
		return e
	}
	return s.relays.Clear(owner)
}
func (s *Server) historyControl(method string, params json.RawMessage) (any, error) {
	switch method {
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
	default:
		return nil, errors.New("unknown history method")
	}
}
func (s *Server) handleHistoryAPI(w http.ResponseWriter, r *http.Request, d model.Device) bool {
	if r.URL.Path != "/api/v1/history" {
		return false
	}
	switch r.Method {
	case http.MethodGet:
		q := r.URL.Query()
		limit, e := strconv.Atoi(q.Get("limit"))
		if q.Get("limit") != "" && e != nil {
			fail(w, 400, e)
			return true
		}
		respond(w, 200, s.history(d.ID, historyFilter{Query: q.Get("query"), State: q.Get("state"), Direction: q.Get("direction"), DeviceID: q.Get("deviceId"), Limit: limit}))
	case http.MethodDelete:
		if e := s.clearHistory(d.ID); e != nil {
			fail(w, 500, errors.New("history could not be cleared"))
		} else {
			respond(w, 200, map[string]bool{"ok": true})
		}
	default:
		http.Error(w, "method not allowed", 405)
	}
	return true
}
