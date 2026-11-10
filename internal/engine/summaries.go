package engine

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

// SummaryQuery is local metadata navigation. File manifests are loaded separately.
type SummaryQuery struct {
	Cursor    string `json:"cursor"`
	Limit     int    `json:"limit"`
	DeviceID  string `json:"deviceId"`
	State     string `json:"state"`
	Direction string `json:"direction"`
	Query     string `json:"query"`
}

type SummaryPage struct {
	Transfers []model.Transfer `json:"transfers"`
	Next      string           `json:"nextCursor,omitempty"`
}

type summaryCursor struct{ CreatedAt, ID string }

func summarize(t model.Transfer) model.Transfer {
	files := t.Files
	t.Summary, t.FileCount = true, len(files)
	t.Files = make([]model.File, 0, min(3, len(files)))
	for _, f := range files[:min(3, len(files))] {
		f.DuplicateCandidates = nil
		t.Files = append(t.Files, f)
	}
	if len(files) > 0 {
		t.Title = files[0].Path
	}
	if t.State == "completed" && t.Direction == "receive" {
		hasRegular, keptRegular, keptDirectory := false, false, false
		for _, f := range files {
			if f.Directory {
				keptDirectory = keptDirectory || f.Completed && !f.Skipped
			} else {
				hasRegular = true
				keptRegular = keptRegular || f.Completed && !f.Skipped
			}
		}
		t.HasContent = keptRegular || !hasRegular && keptDirectory
	}
	return t
}

// StatusSummaries copies only bounded previews, never full retained manifests.
// Keep three terminal results per peer and at most 200 entries in total.
func (e *Engine) StatusSummaries() ([]model.Transfer, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	items := make([]*record, 0, len(e.records))
	for _, r := range e.records {
		items = append(items, r)
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i].Transfer, items[j].Transfer
		if terminal(a.State) != terminal(b.State) {
			return !terminal(a.State)
		}
		if a.UpdatedAt != b.UpdatedAt {
			return a.UpdatedAt > b.UpdatedAt
		}
		return a.ID > b.ID
	})
	out := []model.Transfer{}
	recent := map[string]int{}
	active := 0
	for _, r := range items {
		t := r.Transfer
		if !terminal(t.State) {
			active++
		}
		if terminal(t.State) {
			if recent[t.DeviceID] >= 3 {
				continue
			}
			recent[t.DeviceID]++
		}
		if len(out) < 200 {
			out = append(out, summarize(t))
		}
	}
	return out, active
}

func (e *Engine) SummaryPage(q SummaryQuery) (SummaryPage, error) {
	var before summaryCursor
	if q.Cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil || len(b) > 512 || json.Unmarshal(b, &before) != nil || before.ID == "" || before.CreatedAt == "" {
			return SummaryPage{}, ErrInvalid
		}
	}
	if q.Limit <= 0 {
		q.Limit = 50
	}
	q.Limit = min(q.Limit, 100)
	query := strings.ToLower(strings.TrimSpace(q.Query))
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return SummaryPage{}, errors.New("engine closed")
	}
	items := make([]*record, 0)
	for _, r := range e.records {
		t := &r.Transfer
		if q.DeviceID != "" && t.DeviceID != q.DeviceID || q.Direction != "" && t.Direction != q.Direction {
			continue
		}
		if q.State == "active" && terminal(t.State) || q.State == "finished" && !terminal(t.State) {
			continue
		}
		if q.State != "" && q.State != "active" && q.State != "finished" && t.State != q.State {
			continue
		}
		if before.ID != "" && (t.CreatedAt > before.CreatedAt || t.CreatedAt == before.CreatedAt && t.ID >= before.ID) {
			continue
		}
		if query != "" {
			match := strings.Contains(strings.ToLower(t.DeviceName), query)
			for _, f := range t.Files {
				if strings.Contains(strings.ToLower(f.Path), query) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		items = append(items, r)
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i].Transfer, items[j].Transfer
		if a.CreatedAt != b.CreatedAt {
			return a.CreatedAt > b.CreatedAt
		}
		return a.ID > b.ID
	})
	out := SummaryPage{Transfers: []model.Transfer{}}
	for _, r := range items[:min(q.Limit, len(items))] {
		out.Transfers = append(out.Transfers, summarize(r.Transfer))
	}
	if len(items) > q.Limit {
		last := out.Transfers[len(out.Transfers)-1]
		b, _ := json.Marshal(summaryCursor{last.CreatedAt, last.ID})
		out.Next = base64.RawURLEncoding.EncodeToString(b)
	}
	return out, nil
}

type FilePage struct {
	Files []model.File `json:"files"`
	Next  int          `json:"nextOffset"`
	Total int          `json:"total"`
}

func (e *Engine) FilePage(id string, offset, limit int) (FilePage, error) {
	if offset < 0 || limit < 0 {
		return FilePage{}, ErrInvalid
	}
	if limit == 0 {
		limit = 50
	}
	limit = min(limit, 100)
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.getLocked(id)
	if err != nil {
		return FilePage{}, err
	}
	if offset > len(r.Transfer.Files) {
		return FilePage{}, ErrInvalid
	}
	end := offset + min(limit, len(r.Transfer.Files)-offset)
	preview := clone(model.Transfer{Files: r.Transfer.Files[offset:end]})
	next := -1
	if end < len(r.Transfer.Files) {
		next = end
	}
	return FilePage{Files: preview.Files, Next: next, Total: len(r.Transfer.Files)}, nil
}
