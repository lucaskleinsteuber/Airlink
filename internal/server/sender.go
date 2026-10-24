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

	"github.com/lucasfd3/Airlink/internal/model"
)

func (s *Server) outgoingLoop() {
	defer s.wg.Done()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-tick.C:
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
			hash, err := s.engine.SourceSHA256(ctx, t.ID, f.ID)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				_ = s.engine.Fail(t.ID, err.Error())
				return
			}
			if f.Completed {
				_ = s.engine.MarkOutgoingProgress(t.ID, f.ID, f.Size, hash)
				_ = s.engine.CompleteOutgoingFile(t.ID, f.ID, hash)
				continue
			}
			_ = s.engine.MarkOutgoingProgress(t.ID, f.ID, f.Received, hash)
			for offset := f.Received; offset < f.Size; {
				own, err := s.engine.Get(t.ID)
				if err != nil || own.State == "paused" || own.State == "cancelled" || ctx.Err() != nil {
					return
				}
				if own.State != "running" {
					retryTransfer = true
					break
				}
				data, chunkHash, err := s.engine.ReadSourceChunk(ctx, t.ID, f.ID, offset/model.ChunkSize)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					_ = s.engine.Fail(t.ID, err.Error())
					return
				}
				raw := fmt.Sprintf("%s/%s/files/%s/chunks/%d", base, remote, f.ID, offset/model.ChunkSize)
				chunkCtx, chunkCancel := context.WithTimeout(ctx, 2*time.Minute)
				req, err := http.NewRequestWithContext(chunkCtx, "PUT", raw, bytes.NewReader(data))
				if err != nil {
					chunkCancel()
					return
				}
				req.Header.Set("Authorization", "Bearer "+rec.OutboundToken)
				req.Header.Set("X-Chunk-SHA256", chunkHash)
				req.Header.Set("Content-Type", "application/octet-stream")
				res, err := client.Do(req)
				if err == nil {
					if res.StatusCode != 200 {
						b, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
						err = fmt.Errorf("receiver HTTP %d: %s", res.StatusCode, b)
					}
					_, _ = io.Copy(io.Discard, res.Body)
					_ = res.Body.Close()
				}
				chunkCancel()
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					_ = s.engine.MarkReconnecting(t.ID, err.Error())
					retryTransfer = true
					break
				}
				offset += int64(len(data))
				if err = s.engine.MarkOutgoingProgress(t.ID, f.ID, offset, hash); err != nil {
					return
				}
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
				_ = s.engine.MarkReconnecting(t.ID, e.Error())
				retryTransfer = true
				break
			}
			if e = s.engine.CompleteOutgoingFile(t.ID, f.ID, hash); e != nil {
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
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="AirLink.zip"`)
	zw := zip.NewWriter(w)
	for _, m := range t.Files {
		select {
		case <-r.Context().Done():
			_ = zw.Close()
			return
		default:
		}
		name := m.Path
		if m.Directory {
			name = strings.TrimSuffix(name, "/") + "/"
			_, _ = zw.Create(name)
			continue
		}
		f, _, err := s.engine.OpenDownload(t.ID, m.ID)
		if err != nil {
			_ = zw.Close()
			return
		}
		h := &zip.FileHeader{Name: name, Method: zip.Store}
		h.SetModTime(time.UnixMilli(m.Modified))
		entry, err := zw.CreateHeader(h)
		if err == nil {
			_, err = io.Copy(entry, f)
		}
		_ = f.Close()
		if err != nil {
			_ = zw.Close()
			return
		}
	}
	_ = zw.Close()
}
