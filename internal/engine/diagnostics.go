package engine

import "github.com/lucaskleinsteuber/Airlink/internal/model"

// DiagnosticSummary intentionally has no names, identifiers, paths, hashes,
// source references or free-form errors. Build it directly, never from Status.
type DiagnosticSummary struct {
	Transfers      int            `json:"transfers"`
	Files          int            `json:"files"`
	States         map[string]int `json:"states"`
	Problems       map[string]int `json:"problems"`
	ConfirmedBytes int64          `json:"confirmedBytes"`
}

func (e *Engine) Diagnostics() DiagnosticSummary {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := DiagnosticSummary{States: map[string]int{}, Problems: map[string]int{}}
	for _, r := range e.records {
		out.Transfers++
		out.Files += len(r.Transfer.Files)
		out.ConfirmedBytes += r.Transfer.CompletedBytes
		// Persisted/free-form strings must not become arbitrary JSON keys.
		state := "other"
		switch r.Transfer.State {
		case "offered", "queued", "running", "paused", "reconnecting", "verifying", "checkingDuplicate", "waitingDuplicate", "completed", "failed", "cancelled":
			state = r.Transfer.State
		}
		out.States[state]++
		if p := model.DescribeTransferError(r.Transfer.Error); p != nil {
			out.Problems[p.Code]++
		}
	}
	return out
}
