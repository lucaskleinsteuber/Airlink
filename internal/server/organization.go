package server

import (
	"context"
	"encoding/json"
	"errors"
)

// Duplicate decisions and filesystem paths are available exclusively through
// the private local administration channel. Guests can only request a check.
func (s *Server) duplicateControl(method string, params json.RawMessage) (any, error) {
	return s.duplicateControlContext(s.ctx, method, params)
}
func (s *Server) duplicateControlContext(ctx context.Context, method string, params json.RawMessage) (any, error) {
	var v struct {
		TransferID  string `json:"transferId"`
		FileID      string `json:"fileId"`
		Action      string `json:"action"`
		CandidateID string `json:"candidateId"`
	}
	if err := json.Unmarshal(params, &v); err != nil {
		return nil, err
	}
	switch method {
	case "duplicate.resolve":
		return s.engine.ResolveDuplicate(ctx, v.TransferID, v.FileID, v.Action, v.CandidateID)
	case "duplicate.path":
		path, err := s.engine.DuplicatePath(v.CandidateID)
		if err != nil {
			return nil, err
		}
		return map[string]string{"path": path}, nil
	default:
		return nil, errors.New("unknown duplicate method")
	}
}
