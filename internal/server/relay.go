package server

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
	"github.com/lucaskleinsteuber/Airlink/internal/relay"
)

func (s *Server) registerRelayRoutes(_ *http.ServeMux)         {}
func (s *Server) relaySummaries(deviceID string) []model.Relay { return s.relays.List(deviceID) }
func (s *Server) relayDevices(d model.Device) []model.Device {
	out := []model.Device{}
	fresh, ok := s.store.snapshot().Devices[d.ID]
	if !ok || !fresh.Device.Paired || !fresh.Device.CanRelay || !fresh.Device.CanSend || fresh.Device.Kind != "browser" {
		return out
	}
	for id, record := range s.store.snapshot().Devices {
		other := record.Device
		seen, err := time.Parse(time.RFC3339Nano, other.LastSeen)
		if id != d.ID && other.Kind == "browser" && other.Paired && other.CanRelay && other.CanReceive && err == nil && time.Since(seen) < 45*time.Second {
			out = append(out, model.Device{ID: id, Name: other.Name, Kind: "browser", Online: true, CanRelay: true, CanReceive: true})
		}
	}
	return out
}
func (s *Server) cancelDeviceRelays(id string) {
	if s.relays != nil {
		s.relays.CancelDevice(id)
	}
}
func (s *Server) relayControl(method string, params json.RawMessage) (any, error) {
	if method == "relay.list" {
		return s.relays.List(""), nil
	}
	var v struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &v); err != nil {
		return nil, err
	}
	action := strings.TrimPrefix(method, "relay.")
	if action == "accept" {
		return nil, errors.New("The receiving browser must explicitly accept this relay.")
	}
	if action != "pause" && action != "resume" && action != "cancel" {
		return nil, relay.ErrInvalid
	}
	if action == "cancel" {
		if current, err := s.relays.Get(v.ID, ""); err == nil && current.State == "delivered" {
			return map[string]bool{"ok": true}, s.relays.Interrupt(v.ID)
		}
	}
	return map[string]bool{"ok": true}, s.relays.Action(v.ID, "", action)
}
func (s *Server) relayACL(v model.Relay) error {
	p := s.store.snapshot()
	source, ok := p.Devices[v.SenderID]
	target, targetOK := p.Devices[v.ReceiverID]
	if !ok || !targetOK || !source.Device.Paired || !target.Device.Paired || source.Device.Kind != "browser" || target.Device.Kind != "browser" || !source.Device.CanRelay || !source.Device.CanSend || !target.Device.CanRelay || !target.Device.CanReceive {
		return relay.ErrForbidden
	}
	return nil
}
func relayFailure(w http.ResponseWriter, err error) {
	code := 500
	message := "Relay unavailable. Refresh and retry."
	switch {
	case errors.Is(err, relay.ErrNotFound):
		code = 404
		message = relay.ErrNotFound.Error()
	case errors.Is(err, relay.ErrForbidden):
		code = 403
		message = relay.ErrForbidden.Error()
	case errors.Is(err, relay.ErrInvalid):
		code = 400
		message = relay.ErrInvalid.Error()
	case errors.Is(err, relay.ErrIntegrity):
		code = 409
		message = relay.ErrIntegrity.Error()
	case errors.Is(err, relay.ErrConflict) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		code = 409
		message = relay.ErrConflict.Error()
	case errors.Is(err, relay.ErrUnavailable):
		code = 409
		message = relay.ErrUnavailable.Error()
	}
	respond(w, code, map[string]string{"error": message})
}
func (s *Server) handleRelayAPI(w http.ResponseWriter, r *http.Request, d model.Device) bool {
	p := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	if p != "relays" && !strings.HasPrefix(p, "relays/") {
		return false
	}
	if p == "relays" && r.Method == "POST" {
		var input model.RelayCreate
		if !decode(w, r, &input) {
			return true
		}
		snapshot := s.store.snapshot()
		source, ok := snapshot.Devices[d.ID]
		target, targetOK := snapshot.Devices[input.ReceiverID]
		if !ok || !targetOK {
			relayFailure(w, relay.ErrForbidden)
			return true
		}
		v, err := s.relays.Create(source.Device, target.Device, input.Files)
		if err == nil {
			if err = s.relayACL(v); err != nil {
				_ = s.relays.Action(v.ID, "", "cancel")
			}
		}
		if err != nil {
			relayFailure(w, err)
		} else {
			respond(w, 201, v)
		}
		return true
	}
	parts := strings.Split(p, "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return true
	}
	v, err := s.relays.Get(parts[1], d.ID)
	if err != nil {
		relayFailure(w, err)
		return true
	}
	if err = s.relayACL(v); err != nil {
		s.cancelDeviceRelays(d.ID)
		relayFailure(w, err)
		return true
	}
	if len(parts) == 2 && r.Method == "GET" {
		respond(w, 200, v)
		return true
	}
	if len(parts) == 3 && r.Method == "POST" {
		action := parts[2]
		if action == "accept" {
			err = s.relays.Accept(v.ID, d.ID)
		} else {
			err = s.relays.Action(v.ID, d.ID, action)
		}
		if err != nil {
			relayFailure(w, err)
		} else {
			fresh, _ := s.relays.Get(v.ID, d.ID)
			respond(w, 200, fresh)
		}
		return true
	}
	if len(parts) == 3 && parts[2] == "request" && r.Method == "GET" {
		request, err := s.relays.NextRequest(v.ID, d.ID)
		if err != nil {
			relayFailure(w, err)
		} else {
			respond(w, 200, map[string]any{"request": request})
		}
		return true
	}
	if len(parts) == 6 && parts[2] == "files" && parts[4] == "chunks" && r.Method == "PUT" {
		index, parseErr := strconv.ParseInt(parts[5], 10, 64)
		if parseErr != nil {
			relayFailure(w, relay.ErrInvalid)
			return true
		}
		requestID := r.URL.Query().Get("requestId")
		opCtx, contextErr := s.relays.RequestContext(v.ID, d.ID, requestID, parts[3], index)
		if contextErr != nil {
			relayFailure(w, contextErr)
			return true
		}
		combined, cancel := context.WithCancel(opCtx)
		stop := context.AfterFunc(r.Context(), cancel)
		defer stop()
		defer cancel()
		closeBody := context.AfterFunc(combined, func() { _ = r.Body.Close() })
		defer closeBody()
		reader := s.traffic.WrapReader(combined, http.MaxBytesReader(w, r.Body, model.ChunkSize+1), "receive")
		err = s.relays.Supply(combined, v.ID, d.ID, parts[3], index, requestID, r.Header.Get("X-Chunk-SHA256"), reader)
		if err != nil {
			relayFailure(w, err)
		} else {
			respond(w, 200, map[string]bool{"delivered": true})
		}
		return true
	}
	if len(parts) == 5 && parts[2] == "files" && parts[4] == "download" && (r.Method == "GET" || r.Method == "HEAD") {
		if d.ID != v.ReceiverID {
			relayFailure(w, relay.ErrForbidden)
		} else {
			s.relayDownload(w, r, v, parts[3])
		}
		return true
	}
	if len(parts) == 3 && parts[2] == "archive" && r.Method == "GET" {
		if d.ID != v.ReceiverID {
			relayFailure(w, relay.ErrForbidden)
		} else {
			s.relayArchive(w, r, v)
		}
		return true
	}
	http.NotFound(w, r)
	return true
}
func relayFile(v model.Relay, fileID string) (model.File, error) {
	for _, f := range v.Files {
		if f.ID == fileID && !f.Directory {
			return f, nil
		}
	}
	return model.File{}, relay.ErrNotFound
}
func relayETag(f model.File) string { return `"relay-` + f.ID + `-` + f.SHA256 + `"` }
func etagMatches(header, tag string, weak bool) bool {
	for _, value := range strings.Split(header, ",") {
		value = strings.TrimSpace(value)
		if value == "*" {
			return true
		}
		if weak {
			value = strings.TrimPrefix(value, "W/")
		}
		if value == tag {
			return true
		}
	}
	return false
}
func relayRange(header string, size int64) (int64, int64, error) {
	if header == "" {
		return 0, size, nil
	}
	if size == 0 || !strings.HasPrefix(header, "bytes=") || strings.Contains(header, ",") {
		return 0, 0, relay.ErrInvalid
	}
	parts := strings.Split(strings.TrimPrefix(header, "bytes="), "-")
	if len(parts) != 2 {
		return 0, 0, relay.ErrInvalid
	}
	if parts[0] == "" {
		n, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, relay.ErrInvalid
		}
		return max(int64(0), size-n), size, nil
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, relay.ErrInvalid
	}
	end := size
	if parts[1] != "" {
		inclusive, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || inclusive < start {
			return 0, 0, relay.ErrInvalid
		}
		if inclusive < size-1 {
			end = inclusive + 1
		}
	}
	return start, end, nil
}
func relayPreconditions(w http.ResponseWriter, r *http.Request, f model.File) (bool, string) {
	tag := relayETag(f)
	w.Header().Set("ETag", tag)
	modified := time.UnixMilli(f.Modified).UTC().Truncate(time.Second)
	if f.Modified > 0 {
		w.Header().Set("Last-Modified", modified.Format(http.TimeFormat))
	}
	if match := r.Header.Get("If-Match"); match != "" && !etagMatches(match, tag, false) {
		w.WriteHeader(412)
		return true, ""
	}
	if r.Header.Get("If-Match") == "" {
		if date, err := http.ParseTime(r.Header.Get("If-Unmodified-Since")); err == nil && f.Modified > 0 && modified.After(date) {
			w.WriteHeader(412)
			return true, ""
		}
	}
	if none := r.Header.Get("If-None-Match"); none != "" {
		if etagMatches(none, tag, true) {
			w.WriteHeader(304)
			return true, ""
		}
	} else if date, err := http.ParseTime(r.Header.Get("If-Modified-Since")); err == nil && f.Modified > 0 && !modified.After(date) {
		w.WriteHeader(304)
		return true, ""
	}
	ranges := r.Header.Get("Range")
	if ranges != "" {
		if condition := r.Header.Get("If-Range"); condition != "" {
			if condition != tag {
				if date, err := http.ParseTime(condition); err != nil || f.Modified <= 0 || !modified.Equal(date) {
					ranges = ""
				}
			}
		}
	}
	return false, ranges
}
func (s *Server) relayDownload(w http.ResponseWriter, r *http.Request, v model.Relay, fileID string) {
	f, err := relayFile(v, fileID)
	if err != nil {
		relayFailure(w, err)
		return
	}
	if v.State == "offered" || v.State == "paused" || v.State == "failed" || v.State == "cancelled" {
		relayFailure(w, relay.ErrConflict)
		return
	}
	if done, ranges := relayPreconditions(w, r, f); done {
		return
	} else {
		start, end, rangeErr := relayRange(ranges, f.Size)
		if rangeErr != nil {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", f.Size))
			http.Error(w, "Requested range is not satisfiable", 416)
			return
		}
		status := 200
		if ranges != "" {
			status = 206
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end-1, f.Size))
		}
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.FormatInt(end-start, 10))
		w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filepath.Base(f.Path)))
		contentType := mime.TypeByExtension(filepath.Ext(f.Path))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)
		if r.Method == "HEAD" {
			w.WriteHeader(status)
			return
		}
		op, err := s.relays.BeginStream(r.Context(), v.ID, v.ReceiverID)
		if err != nil {
			clearRelayHeaders(w)
			relayFailure(w, err)
			return
		}
		success := false
		defer func() { op.Close(success) }()
		stop := context.AfterFunc(op.Context(), func() { _ = http.NewResponseController(w).SetWriteDeadline(time.Now()) })
		defer stop()
		if err = s.relayACL(v); err != nil {
			clearRelayHeaders(w)
			relayFailure(w, err)
			return
		}
		if f.Size == 0 {
			if err = s.relays.EmptyDelivered(op, f.ID); err != nil {
				clearRelayHeaders(w)
				relayFailure(w, err)
				return
			}
			w.WriteHeader(status)
			success = true
			return
		}
		writer := &relayPayloadWriter{ResponseWriter: w, ctx: op.Context(), acl: func() error { return s.relayACL(v) }, status: status}
		metered := s.traffic.WrapWriter(op.Context(), writer, "send")
		full := start == 0 && end == f.Size
		err = s.streamRelayFile(op, v, f, start, end, full, metered, nil)
		if err != nil {
			if !writer.started {
				clearRelayHeaders(w)
				relayFailure(w, err)
			}
			return
		}
		success = true
	}
}
func clearRelayHeaders(w http.ResponseWriter) {
	for _, h := range []string{"Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Content-Disposition", "Last-Modified"} {
		w.Header().Del(h)
	}
}

type relayPayloadWriter struct {
	http.ResponseWriter
	ctx     context.Context
	acl     func() error
	status  int
	started bool
}

func (w *relayPayloadWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *relayPayloadWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if err := w.acl(); err != nil {
		return 0, err
	}
	if !w.started {
		w.WriteHeader(w.status)
		w.started = true
	}
	return w.ResponseWriter.Write(p)
}
func (s *Server) streamRelayFile(op *relay.Stream, v model.Relay, f model.File, start, end int64, full bool, writer io.Writer, flush func() error) error {
	var whole hash.Hash
	if full {
		whole = sha256.New()
	}
	for position := start; position < end; {
		if err := s.relayACL(v); err != nil {
			return err
		}
		index := position / model.ChunkSize
		b, block, err := s.relays.Fetch(op, f.ID, index)
		if err != nil {
			return err
		}
		if whole != nil {
			_, _ = whole.Write(b)
			if index == (f.Size-1)/model.ChunkSize && hex.EncodeToString(whole.Sum(nil)) != f.SHA256 {
				_ = s.relays.FailForStream(op, relay.ErrIntegrity)
				_ = s.relays.Confirm(block, position, position, relay.ErrIntegrity)
				return relay.ErrIntegrity
			}
		}
		blockStart := index * model.ChunkSize
		sliceStart := position - blockStart
		sliceEnd := min(int64(len(b)), end-blockStart)
		payload := b[sliceStart:sliceEnd]
		written := int64(0)
		for len(payload) > 0 {
			n, writeErr := writer.Write(payload[:min(len(payload), 32*1024)])
			if n > 0 {
				written += int64(n)
				payload = payload[n:]
			}
			if writeErr != nil {
				_ = s.relays.Confirm(block, position, position+written, writeErr)
				return writeErr
			}
			if n == 0 {
				_ = s.relays.Confirm(block, position, position+written, io.ErrShortWrite)
				return io.ErrShortWrite
			}
		}
		if flush != nil {
			if err = flush(); err != nil {
				_ = s.relays.Confirm(block, position, position+written, err)
				return err
			}
		}
		if err = s.relays.Confirm(block, position, position+written, nil); err != nil {
			return err
		}
		position += written
	}
	return nil
}
func (s *Server) relayArchive(w http.ResponseWriter, r *http.Request, v model.Relay) {
	if r.Header.Get("Range") != "" {
		http.Error(w, "Archive ranges are not supported", 416)
		return
	}
	if v.State == "offered" || v.State == "paused" || v.State == "cancelled" || v.State == "failed" {
		relayFailure(w, relay.ErrConflict)
		return
	}
	op, err := s.relays.BeginStream(r.Context(), v.ID, v.ReceiverID)
	if err != nil {
		relayFailure(w, err)
		return
	}
	success := false
	defer func() { op.Close(success) }()
	stop := context.AfterFunc(op.Context(), func() { _ = http.NewResponseController(w).SetWriteDeadline(time.Now()) })
	defer stop()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="AirLink.zip"`)
	writer := &relayPayloadWriter{ResponseWriter: w, ctx: op.Context(), acl: func() error { return s.relayACL(v) }, status: 200}
	archive := zip.NewWriter(s.traffic.WrapWriter(op.Context(), writer, "send"))
	abort := func(cause error) {
		if writer.started {
			panic(http.ErrAbortHandler)
		}
		clearRelayHeaders(w)
		relayFailure(w, cause)
	}
	for _, f := range v.Files {
		if err = op.Context().Err(); err != nil {
			abort(err)
			return
		}
		name := f.Path
		if f.Directory {
			name = strings.TrimSuffix(name, "/") + "/"
		}
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		header.SetModTime(time.UnixMilli(f.Modified))
		entry, err := archive.CreateHeader(header)
		if err != nil {
			abort(err)
			return
		}
		if f.Directory {
			continue
		}
		if f.Size == 0 {
			continue
		}
		if err = s.streamRelayFile(op, v, f, 0, f.Size, true, entry, archive.Flush); err != nil {
			abort(err)
			return
		}
	}
	if err = archive.Close(); err != nil {
		abort(err)
		return
	}
	for _, f := range v.Files {
		if f.Size == 0 {
			if err = s.relays.EmptyDelivered(op, f.ID); err != nil {
				abort(err)
				return
			}
		}
	}
	success = true
}
