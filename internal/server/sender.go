package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func (s *Server) outgoingLoop() {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	s.runOutgoingLoop(tick.C)
}

// Coalescing wakes remove the polling delay for a new user submission. The
// periodic fallback still discovers recovered work and changed permissions.
func (s *Server) wakeOutgoing() {
	select {
	case s.outgoingWake <- struct{}{}:
	default:
	}
}

func (s *Server) runOutgoingLoop(ticks <-chan time.Time) {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticks:
		case <-s.outgoingWake:
		}
		p := s.store.snapshot()
		for _, t := range s.engine.Transfers("") {
			if t.Direction != "send" || t.State == "completed" || t.State == "failed" || t.State == "paused" {
				continue
			}
			r, ok := p.Devices[t.DeviceID]
			if !ok || r.Device.Kind != "engine" || r.OutboundToken == "" {
				continue
			}
			s.mu.Lock()
			if s.senders[t.ID] {
				s.mu.Unlock()
				continue
			}
			s.senders[t.ID] = true
			workerCtx, workerCancel := context.WithCancel(s.ctx)
			s.senderCancels[t.ID] = workerCancel
			s.mu.Unlock()
			s.wg.Add(1)
			go func(t model.Transfer, r deviceRecord) {
				defer s.wg.Done()
				defer func() {
					s.mu.Lock()
					delete(s.senders, t.ID)
					delete(s.senderCancels, t.ID)
					workerCancel()
					s.mu.Unlock()
				}()
				s.sendTransfer(workerCtx, t, r)
			}(t, r)
		}
	}
}
func (s *Server) remoteID(local string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, e := os.ReadFile(filepath.Join(s.opts.DataDir, "remote-"+local+".json"))
	if e != nil {
		return ""
	}
	var v struct{ ID string }
	_ = json.Unmarshal(b, &v)
	return v.ID
}
func (s *Server) saveRemoteID(local, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(map[string]string{"ID": id})
	return atomicFile(filepath.Join(s.opts.DataDir, "remote-"+local+".json"), b)
}
func (s *Server) sendTransfer(ctx context.Context, t model.Transfer, rec deviceRecord) {
	client := pinnedClient(rec.Device.Fingerprint)
	base := strings.TrimRight(rec.Device.Address, "/") + "/api/v1/transfers"
	remote := s.remoteID(t.ID)
	retry := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		local, e := s.engine.Get(t.ID)
		if e != nil {
			return
		}
		fresh, paired := s.store.snapshot().Devices[t.DeviceID]
		if !paired || !fresh.Device.CanReceive {
			_ = s.engine.Fail(t.ID, "Receiving device permission revoked")
			return
		}
		rec = fresh
		base = strings.TrimRight(rec.Device.Address, "/") + "/api/v1/transfers"
		if local.State == "cancelled" {
			if remote != "" {
				marker := filepath.Join(s.opts.DataDir, "cancel-"+t.ID)
				if _, err := os.Stat(marker); err == nil {
					return
				}
				if err := requestJSON(ctx, client, "POST", base+"/"+remote+"/cancel", rec.OutboundToken, map[string]any{}, nil); err == nil {
					_ = atomicFile(marker, []byte("acknowledged"))
				}
			}
			return
		}
		if local.State == "paused" {
			return
		}
		if local.State == "completed" || local.State == "failed" {
			return
		}
		var target model.Transfer
		if remote == "" {
			files := append([]model.File(nil), t.Files...)
			for i := range files {
				files[i] = publicFile(files[i])
				files[i].Skipped = false
				files[i].DuplicateState = ""
				files[i].Received = 0
				files[i].Completed = false
				files[i].SHA256 = ""
			}
			e = requestJSON(ctx, client, "POST", base, rec.OutboundToken, model.Offer{Files: files}, &target)
			if e == nil {
				remote = target.ID
				e = s.saveRemoteID(t.ID, remote)
			}
		} else {
			e = requestJSON(ctx, client, "GET", base+"/"+remote, rec.OutboundToken, nil, &target)
		}
		if e != nil {
			if ctx.Err() != nil {
				return
			}
			if permanentPeerError(e) {
				_ = s.engine.Fail(t.ID, e.Error())
				return
			}
			_ = s.engine.MarkReconnecting(t.ID, e.Error())
			if !sleepContext(ctx, retry) {
				return
			}
			retry = min(retry*2, 30*time.Second)
			continue
		}
		retry = time.Second
		if target.State == "cancelled" || target.State == "failed" {
			_ = s.engine.Fail(t.ID, "Receiver stopped transfer: "+target.Error)
			return
		}
		if target.State != "running" && target.State != "completed" {
			if target.State == "checkingDuplicate" || target.State == "waitingDuplicate" {
				_ = s.engine.MarkOutgoingWaiting(t.ID, target.State)
			}
			if !sleepContext(ctx, time.Second) {
				return
			}
			continue
		}
		if local.State != "running" {
			if e = s.engine.Resume(t.ID); e != nil {
				if !sleepContext(ctx, time.Second) {
					return
				}
				continue
			}
			local, _ = s.engine.Get(t.ID)
			if local.State != "running" {
				if !sleepContext(ctx, time.Second) {
					return
				}
				continue
			}
		}
		retryTransfer := false
		for _, f := range target.Files {
			current, err := s.engine.Get(t.ID)
			if err != nil {
				return
			}
			if current.State == "paused" || current.State == "cancelled" {
				return
			}
			if f.Directory {
				_ = s.engine.CompleteOutgoingFile(t.ID, f.ID, "")
				continue
			}
			var localFile *model.File
			for i := range current.Files {
				if current.Files[i].ID == f.ID {
					localFile = &current.Files[i]
					break
				}
			}
			if localFile == nil {
				_ = s.engine.Fail(t.ID, "Receiver changed the file manifest")
				return
			}
			if localFile.Skipped {
				// A later file may pause for a host decision or reconnect. Its
				// next pass revisits prior Skip receipts, which deliberately have
				// no downloadable payload. Reuse their pinned source identity.
				if !f.Skipped || !f.Completed || f.SHA256 != localFile.SHA256 || f.Size != localFile.Size || f.Received != localFile.Received {
					_ = s.engine.Fail(t.ID, "Receiver changed a confirmed skipped file")
					return
				}
				if err = s.engine.MarkOutgoingSkipped(t.ID, f.ID, f.Received, localFile.SHA256); err != nil {
					return
				}
				continue
			}
			hash, err := s.engine.SourceSHA256(ctx, t.ID, f.ID)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				_ = s.engine.Fail(t.ID, err.Error())
				return
			}
			if f.Skipped {
				if err = s.engine.MarkOutgoingSkipped(t.ID, f.ID, f.Received, hash); err != nil {
					return
				}
				continue
			}
			if f.Completed {
				_ = s.engine.MarkOutgoingProgress(t.ID, f.ID, f.Size, hash)
				_ = s.engine.CompleteOutgoingFile(t.ID, f.ID, hash)
				continue
			}
			decision, prepared, prepErr := prepareRemoteFile(ctx, client, base+"/"+remote+"/files/"+f.ID+"/prepare", rec.OutboundToken, hash, remote, f.ID)
			if prepErr != nil {
				if ctx.Err() != nil {
					return
				}
				if permanentPeerError(prepErr) {
					_ = s.engine.Fail(t.ID, prepErr.Error())
					return
				}
				_ = s.engine.MarkReconnecting(t.ID, prepErr.Error())
				retryTransfer = true
				break
			}
			if decision == "pending" {
				retryTransfer = true
				break
			}
			if decision == "skip" {
				for _, pf := range prepared.Files {
					if pf.ID == f.ID && pf.Skipped {
						if err = s.engine.MarkOutgoingSkipped(t.ID, f.ID, pf.Received, hash); err != nil {
							return
						}
					}
				}
				continue
			}
			_ = s.engine.MarkOutgoingProgress(t.ID, f.ID, f.Received, hash)
			blocks, stopPrefetch := prefetchBlocks(ctx, f.Received/model.ChunkSize, (f.Size+model.ChunkSize-1)/model.ChunkSize, func(readCtx context.Context, index int64) ([]byte, string, error) {
				return s.engine.ReadSourceChunk(readCtx, t.ID, f.ID, index)
			})
			// This scope releases a prefetched block on every pause, error or
			// early return without leaving a producer behind the transfer.
			chunkResult := func() bool {
				defer stopPrefetch()
				for offset := f.Received; offset < f.Size; {
					own, err := s.engine.Get(t.ID)
					if err != nil || own.State == "paused" || own.State == "cancelled" || ctx.Err() != nil {
						return false
					}
					if own.State != "running" {
						retryTransfer = true
						break
					}
					var block sourceBlock
					select {
					case next, ok := <-blocks:
						if !ok {
							return false
						}
						block = next
					case <-ctx.Done():
						return false
					}
					data, chunkHash, err := block.data, block.hash, block.err
					if block.index != offset/model.ChunkSize {
						return false
					}
					if err != nil {
						if ctx.Err() != nil {
							return false
						}
						_ = s.engine.Fail(t.ID, err.Error())
						return false
					}
					raw := fmt.Sprintf("%s/%s/files/%s/chunks/%d", base, remote, f.ID, offset/model.ChunkSize)
					chunkCtx, chunkCancel := context.WithCancel(ctx)
					req, err := http.NewRequestWithContext(chunkCtx, "PUT", raw, s.traffic.WrapReader(chunkCtx, bytes.NewReader(data), "send"))
					if err != nil {
						chunkCancel()
						return false
					}
					req.Header.Set("Authorization", "Bearer "+rec.OutboundToken)
					req.Header.Set("X-Chunk-SHA256", chunkHash)
					req.Header.Set("Content-Type", "application/octet-stream")
					// The block already exists in a bounded buffer. Explicit length
					// avoids chunked request framing around every metered 32 KiB read.
					req.ContentLength = int64(len(data))
					res, err := client.Do(req)
					if err == nil {
						if res.StatusCode != 200 {
							b, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
							err = &peerHTTPError{Code: res.StatusCode, Message: string(b)}
						}
						_, _ = io.Copy(io.Discard, res.Body)
						_ = res.Body.Close()
					}
					chunkCancel()
					if err != nil {
						if ctx.Err() != nil {
							return false
						}
						if permanentPeerError(err) {
							_ = s.engine.Fail(t.ID, err.Error())
							return false
						}
						_ = s.engine.MarkReconnecting(t.ID, err.Error())
						retryTransfer = true
						break
					}
					offset += int64(len(data))
					if err = s.engine.MarkOutgoingProgress(t.ID, f.ID, offset, hash); err != nil {
						return false
					}
				}
				return true
			}()
			if !chunkResult {
				return
			}
			if retryTransfer {
				break
			}
			var done model.Transfer
			e = requestJSON(ctx, client, "POST", base+"/"+remote+"/files/"+f.ID+"/complete", rec.OutboundToken, map[string]string{"sha256": hash}, &done)
			if e != nil {
				if ctx.Err() != nil {
					return
				}
				if permanentPeerError(e) {
					_ = s.engine.Fail(t.ID, e.Error())
					return
				}
				_ = s.engine.MarkReconnecting(t.ID, e.Error())
				retryTransfer = true
				break
			}
			var finished *model.File
			for i := range done.Files {
				if done.Files[i].ID == f.ID {
					finished = &done.Files[i]
					break
				}
			}
			if finished == nil || !finished.Completed {
				retryTransfer = true
				break
			}
			if finished.Skipped {
				e = s.engine.MarkOutgoingSkipped(t.ID, f.ID, finished.Received, hash)
			} else {
				e = s.engine.CompleteOutgoingFile(t.ID, f.ID, hash)
			}
			if e != nil {
				_ = s.engine.Fail(t.ID, e.Error())
				return
			}
		}
		if !retryTransfer {
			completed, _ := s.engine.Get(t.ID)
			if completed.State == "completed" {
				return
			}
		}
		if !sleepContext(ctx, time.Second) {
			return
		}
	}
}

func prepareRemoteFile(ctx context.Context, client *http.Client, raw, token, hash, transferID, fileID string) (string, model.Transfer, error) {
	var out struct {
		Decision string         `json:"decision"`
		Transfer model.Transfer `json:"transfer"`
	}
	err := requestJSON(ctx, client, "POST", raw, token, map[string]string{"sha256": hash}, &out)
	var status *peerHTTPError
	if errors.As(err, &status) && status.Code == http.StatusNotFound {
		// Protocol 1 before 0.4 has no preflight route; full verification still
		// happens on the receiving service's normal complete endpoint.
		return "upload", model.Transfer{}, nil
	}
	if err != nil {
		return "", model.Transfer{}, err
	}
	if out.Decision != "upload" && out.Decision != "pending" && out.Decision != "skip" {
		return "", model.Transfer{}, errors.New("invalid duplicate preflight response")
	}
	if out.Transfer.ID != transferID {
		return "", model.Transfer{}, errors.New("receiver changed transfer identity")
	}
	found := false
	for _, f := range out.Transfer.Files {
		if f.ID == fileID {
			found = true
			if out.Decision == "skip" && (!f.Completed || !f.Skipped) {
				return "", model.Transfer{}, errors.New("receiver did not confirm skipped file")
			}
		}
	}
	if !found {
		return "", model.Transfer{}, errors.New("receiver changed file manifest")
	}
	return out.Decision, out.Transfer, nil
}
func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
func (s *Server) archive(w http.ResponseWriter, r *http.Request, t model.Transfer) {
	if t.Direction != "send" || t.State == "cancelled" {
		fail(w, 403, errors.New("archive unavailable"))
		return
	}
	for _, file := range t.Files {
		if file.Directory {
			continue
		}
		opened, _, err := s.engine.OpenDownloadContext(r.Context(), t.ID, file.ID)
		if err != nil {
			s.engineError(w, err)
			return
		}
		opened.Close()
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="AirLink.zip"`)
	policy := &sourcePolicyWriter{ResponseWriter: w, ctx: r.Context()}
	zw := zip.NewWriter(s.traffic.WrapWriter(r.Context(), policy, "send"))
	for _, m := range t.Files {
		select {
		case <-r.Context().Done():
			panic(http.ErrAbortHandler)
		default:
		}
		name := m.Path
		if m.Directory {
			name = strings.TrimSuffix(name, "/") + "/"
			if _, err := zw.Create(name); err != nil {
				panic(http.ErrAbortHandler)
			}
			continue
		}
		f, _, err := s.engine.OpenDownloadContext(r.Context(), t.ID, m.ID)
		if err != nil {
			panic(http.ErrAbortHandler)
		}
		policy.validate = func() error { return s.engine.ValidateSource(t.ID, m.ID, f) }
		h := &zip.FileHeader{Name: name, Method: zip.Store}
		h.SetModTime(time.UnixMilli(m.Modified))
		entry, err := zw.CreateHeader(h)
		if err == nil {
			_, err = io.Copy(entry, &sourceReader{Context: r.Context(), File: f, server: s, transferID: t.ID, fileID: m.ID})
		}
		_ = f.Close()
		policy.validate = nil
		if err != nil {
			panic(http.ErrAbortHandler)
		}
	}
	if err := zw.Close(); err != nil {
		panic(http.ErrAbortHandler)
	}
}
