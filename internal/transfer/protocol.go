package transfer

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

// Protocol 3 transfer messages. The receiver is authoritative for confirmed
// progress: its status messages start, acknowledge, pause and finish the
// sender's stream. A status with a non-zero epoch asks the sender to start a
// new stream at (next, offset); stream messages carry that epoch, so frames
// of an abandoned stream can never reach a newer receive job.

type wireFile struct {
	Path     string `json:"p"`
	Size     int64  `json:"s,omitempty"`
	Modified int64  `json:"m,omitempty"`
	Dir      bool   `json:"d,omitempty"`
}

type offerMsg struct {
	Type       string     `json:"type"`
	Transfer   string     `json:"transfer"`
	Part       int        `json:"part"`
	Parts      int        `json:"parts"`
	FileCount  int        `json:"fileCount"`
	TotalBytes int64      `json:"totalBytes"`
	Files      []wireFile `json:"files"`
}

// query, pause, resume and cancel.
type commandMsg struct {
	Type     string `json:"type"`
	Transfer string `json:"transfer"`
	Reason   string `json:"reason,omitempty"`
}

type statusMsg struct {
	Type     string `json:"type"`
	Transfer string `json:"transfer"`
	State    string `json:"state"`
	Next     int    `json:"next"`
	Offset   int64  `json:"offset"`
	Epoch    uint64 `json:"epoch,omitempty"`
	Error    string `json:"error,omitempty"`
}

type fileMsg struct {
	Type     string `json:"type"`
	Transfer string `json:"transfer"`
	Epoch    uint64 `json:"epoch"`
	Ordinal  int    `json:"ordinal"`
	Offset   int64  `json:"offset"`
}

type segmentMsg struct {
	Type    string `json:"type"`
	Ordinal int    `json:"ordinal"`
	Index   int64  `json:"index"`
	SHA256  string `json:"sha256"`
	Last    bool   `json:"last,omitempty"`
}

type endMsg struct {
	Type string `json:"type"`
}

// Offer parts stay well below the control message limit.
const (
	offerPartFiles = 2000
	offerPartBytes = 512 << 10
)

var epochs atomic.Uint64

type pendingOffer struct {
	msg      offerMsg
	files    []wireFile
	conn     Conn
	at       time.Time
	nextPart int
}

// Handles reports whether a control message type belongs to the engine.
func Handles(messageType string) bool {
	switch messageType {
	case "offer", "query", "pause", "resume", "cancel", "status", "file", "segment", "end":
		return true
	}
	return false
}

// HandleControl processes one control message from a peer. It runs on the
// connection's reader and never blocks on disk work for long.
func (e *Engine) HandleControl(c Conn, messageType string, stream uint32, body []byte) {
	decode := func(v any) bool { return json.Unmarshal(body, v) == nil }
	switch messageType {
	case "offer":
		var m offerMsg
		if decode(&m) {
			e.onOffer(c, m)
		}
	case "query", "pause", "resume", "cancel":
		var m commandMsg
		if decode(&m) {
			e.onCommand(c, m)
		}
	case "status":
		var m statusMsg
		if decode(&m) {
			e.onStatus(c, m)
		}
	case "file":
		var m fileMsg
		if decode(&m) {
			e.routeFile(c, stream, m)
		}
	case "segment":
		var m segmentMsg
		if decode(&m) {
			e.route(c, stream, recvOp{kind: opSegment, seg: m})
		}
	case "end":
		e.route(c, stream, recvOp{kind: opEnd})
	}
}

// HandleData receives one data frame. buf is released via release.
func (e *Engine) HandleData(c Conn, stream uint32, buf []byte, release func([]byte)) {
	e.mu.Lock()
	j := e.streams[streamKey{c, stream}]
	e.mu.Unlock()
	if j == nil {
		release(buf)
		return
	}
	j.push(recvOp{kind: opData, data: buf, release: release})
}

// SessionOpened continues outgoing work with a newly connected device.
func (e *Engine) SessionOpened(c Conn) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, r := range e.records {
		if r.t.DeviceID != c.PeerID() || r.t.Direction != "send" {
			continue
		}
		switch r.t.State {
		case "offered", "running", "reconnecting", "queued":
			e.contactReceiverLocked(r, c)
		}
	}
}

// SessionClosed stops work bound to a connection. Transfers continue after
// the next connection.
func (e *Engine) SessionClosed(c Conn) {
	e.mu.Lock()
	for id, j := range e.senders {
		if j.conn == c {
			j.cancel()
			delete(e.senders, id)
			if r := e.records[id]; r != nil && r.t.State == "running" {
				r.t.State = "reconnecting"
				r.t.UpdatedAt = now()
				r.live = r.durableBytes
				_ = e.saveHeader(r)
			}
		}
	}
	receivers := []*recvJob{}
	for _, j := range e.receivers {
		if j.conn == c {
			receivers = append(receivers, j)
		}
	}
	for key, o := range e.offers {
		if o.conn == c {
			delete(e.offers, key)
		}
	}
	e.mu.Unlock()
	for _, j := range receivers {
		j.stop(errDisconnected)
	}
	e.changed()
}

func (e *Engine) send(c Conn, v any) {
	if c != nil {
		_ = c.Send(v)
	}
}

// statusLocked reports a receive transfer's state to its sender.
func (r *record) status() statusMsg {
	s := statusMsg{Type: "status", Transfer: r.t.ID, State: r.t.State, Next: r.next, Offset: r.nextOffset}
	switch r.t.State {
	case "reconnecting", "queued":
		s.State = "running"
	case "failed", "cancelled":
		s.Error = r.t.Error
	}
	return s
}

// contactReceiverLocked makes sure the receiver knows an outgoing transfer and
// asks for its state. Callers hold e.mu.
func (e *Engine) contactReceiverLocked(r *record, c Conn) {
	if !r.offered {
		e.sendOfferLocked(r, c)
		return
	}
	e.send(c, commandMsg{Type: "query", Transfer: r.t.ID})
}

func (e *Engine) sendOfferLocked(r *record, c Conn) {
	var chunks [][]wireFile
	current, size := []wireFile{}, 0
	for _, f := range r.files {
		w := wireFile{Path: f.Path, Size: f.Size, Modified: f.Modified, Dir: f.Directory}
		cost := len(f.Path) + 64
		if len(current) > 0 && (len(current) >= offerPartFiles || size+cost > offerPartBytes) {
			chunks = append(chunks, current)
			current, size = []wireFile{}, 0
		}
		current = append(current, w)
		size += cost
	}
	chunks = append(chunks, current)
	for part, files := range chunks {
		e.send(c, offerMsg{Type: "offer", Transfer: r.t.ID, Part: part, Parts: len(chunks), FileCount: len(r.files), TotalBytes: r.t.TotalBytes, Files: files})
	}
}

// onOffer assembles an incoming manifest and creates the transfer.
func (e *Engine) onOffer(c Conn, m offerMsg) {
	device, ok := e.opts.Device(c.PeerID())
	key := c.PeerID() + "/" + m.Transfer
	reject := func(reason string) {
		e.mu.Lock()
		delete(e.offers, key)
		e.mu.Unlock()
		e.send(c, statusMsg{Type: "status", Transfer: m.Transfer, State: "failed", Error: reason})
	}
	if !validTransferID(m.Transfer) {
		return
	}
	if !ok || !device.Paired || !device.CanSend {
		reject("Receiver stopped transfer: device permission revoked")
		return
	}
	if m.Parts < 1 || m.Parts > MaxFiles || m.Part < 0 || m.Part >= m.Parts || m.FileCount < 1 || m.FileCount > MaxFiles || m.TotalBytes < 0 || len(m.Files) == 0 || len(m.Files) > offerPartFiles {
		reject("Receiver stopped transfer: invalid offer")
		return
	}
	e.mu.Lock()
	if r := e.records[m.Transfer]; r != nil {
		if r.t.DeviceID == c.PeerID() && r.t.Direction == "receive" {
			status := r.status()
			e.mu.Unlock()
			e.send(c, status)
			return
		}
		e.mu.Unlock()
		reject("Receiver stopped transfer: transfer identifier conflict")
		return
	}
	pending := e.offers[key]
	if m.Part == 0 {
		pending = &pendingOffer{msg: m, conn: c, at: time.Now()}
		pending.files = make([]wireFile, 0, min(m.FileCount, offerPartFiles))
		e.offers[key] = pending
	}
	if pending == nil || pending.msg.Parts != m.Parts || pending.msg.FileCount != m.FileCount || pending.conn != c || m.Part != pending.nextPart {
		e.mu.Unlock()
		reject("Receiver stopped transfer: incomplete offer")
		return
	}
	pending.files = append(pending.files, m.Files...)
	pending.nextPart++
	if len(pending.files) > m.FileCount {
		e.mu.Unlock()
		reject("Receiver stopped transfer: invalid offer")
		return
	}
	if m.Part < m.Parts-1 {
		e.mu.Unlock()
		return
	}
	delete(e.offers, key)
	e.mu.Unlock()
	if len(pending.files) != m.FileCount {
		reject("Receiver stopped transfer: incomplete offer")
		return
	}
	r, err := e.createIncoming(device, m.Transfer, pending.files)
	if err != nil {
		reject("Receiver stopped transfer: " + err.Error())
		return
	}
	if device.AutoAccept {
		if err = e.Accept(r.t.ID); err == nil {
			return
		}
	}
	e.mu.Lock()
	status := r.status()
	e.mu.Unlock()
	e.send(c, status)
	e.changed()
}

func validTransferID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (e *Engine) createIncoming(device model.Device, id string, wire []wireFile) (*record, error) {
	files := make([]fileEntry, len(wire))
	seen := make(map[string]bool, len(wire))
	var total int64
	for i, w := range wire {
		if err := validWirePath(w.Path); err != nil {
			return nil, err
		}
		if w.Size < 0 || w.Dir && w.Size != 0 || w.Size > 1<<62-total {
			return nil, fmt.Errorf("%w: invalid file size", ErrInvalid)
		}
		if seen[w.Path] {
			return nil, fmt.Errorf("%w: duplicate path", ErrInvalid)
		}
		seen[w.Path] = true
		total += w.Size
		files[i] = fileEntry{}
		files[i].ID, files[i].Path, files[i].Size, files[i].Modified, files[i].Directory = fileID(i), w.Path, w.Size, w.Modified, w.Dir
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, errors.New("transfer engine closed")
	}
	if e.records[id] != nil {
		return nil, ErrConflict
	}
	stamp := now()
	r := &record{files: files}
	r.t.ID, r.t.DeviceID, r.t.DeviceName, r.t.Direction, r.t.State = id, device.ID, device.Name, "receive", "offered"
	r.t.TotalBytes, r.t.FileCount, r.t.Title, r.t.CreatedAt, r.t.UpdatedAt, r.t.Destination = total, len(files), files[0].Path, stamp, stamp, e.receive
	r.preview = files[:min(3, len(files))]
	tx, err := e.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = insertRecord(tx, r); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	e.records[id] = r
	return r, nil
}

// onCommand handles query, pause, resume and cancel from a transfer's sender.
func (e *Engine) onCommand(c Conn, m commandMsg) {
	e.mu.Lock()
	r := e.records[m.Transfer]
	if r == nil || r.t.DeviceID != c.PeerID() || r.t.Direction != "receive" {
		e.mu.Unlock()
		if m.Type == "query" && validTransferID(m.Transfer) {
			e.send(c, statusMsg{Type: "status", Transfer: m.Transfer, State: "unknown"})
		}
		return
	}
	state := r.t.State
	e.mu.Unlock()
	switch m.Type {
	case "query":
		if state == "running" || state == "reconnecting" || state == "queued" {
			e.startReceiver(m.Transfer, c)
			return
		}
	case "pause":
		if !terminal(state) && state != "offered" {
			e.stopReceiver(m.Transfer, "paused", "")
		}
	case "resume":
		if state == "paused" || state == "failed" && retryableReceive(r) {
			e.startReceiver(m.Transfer, c)
			return
		}
	case "cancel":
		if !terminal(state) {
			reason := "Sender cancelled the transfer"
			if m.Reason != "" {
				reason = "Sender stopped transfer: " + m.Reason
			}
			e.stopReceiver(m.Transfer, "cancelled", reason)
		}
	}
	e.mu.Lock()
	status := r.status()
	e.mu.Unlock()
	e.send(c, status)
}

// A receiver that failed on its own storage can continue once the sender
// retries; integrity failures and cancellations cannot.
func retryableReceive(r *record) bool {
	p := model.DescribeTransferError(r.t.Error)
	return p != nil && (p.Code == "storage_full" || p.Code == "file_access" || p.Code == "destination")
}

// onStatus applies the receiver's authoritative state to an outgoing transfer.
func (e *Engine) onStatus(c Conn, m statusMsg) {
	e.mu.Lock()
	r := e.records[m.Transfer]
	if r == nil || r.t.DeviceID != c.PeerID() || r.t.Direction != "send" || terminal(r.t.State) {
		e.mu.Unlock()
		return
	}
	if r.files == nil {
		if err := e.loadFiles(r); err != nil {
			e.mu.Unlock()
			return
		}
	}
	changed := true
	switch m.State {
	case "unknown":
		r.offered = false
		e.sendOfferLocked(r, c)
	case "offered":
		r.offered = true
		if r.t.State != "paused" {
			r.t.State = "offered"
		}
	case "running":
		r.offered = true
		if m.Next < 0 || m.Next > len(r.files) || m.Offset < 0 || m.Next < len(r.files) && m.Offset > r.files[m.Next].Size {
			e.mu.Unlock()
			return
		}
		e.acknowledgeLocked(r, m.Next, m.Offset)
		if m.Epoch != 0 {
			r.t.State = "running"
			r.t.Error = ""
			e.startSendingLocked(r, c, m.Next, m.Offset, m.Epoch)
		} else {
			changed = false
		}
	case "paused":
		r.offered = true
		e.stopSendingLocked(r.t.ID)
		r.t.State = "paused"
	case "completed":
		e.stopSendingLocked(r.t.ID)
		e.acknowledgeLocked(r, len(r.files), 0)
		r.t.State = "completed"
		r.t.Error = ""
	case "failed", "cancelled":
		e.stopSendingLocked(r.t.ID)
		r.t.State = m.State
		reason := m.Error
		if reason == "" {
			reason = "Receiver stopped transfer"
		}
		r.t.Error = reason
		if m.State == "cancelled" && reason == "Receiver stopped transfer" {
			r.t.Error = ""
		}
	default:
		e.mu.Unlock()
		return
	}
	r.t.UpdatedAt = now()
	_ = e.saveHeader(r)
	e.mu.Unlock()
	if changed {
		e.changed()
	}
}

// acknowledgeLocked records the receiver's durable checkpoint on the sender.
func (e *Engine) acknowledgeLocked(r *record, next int, offset int64) {
	ordinals := []int{}
	for i := r.next; i < next && i < len(r.files); i++ {
		if !r.files[i].Completed {
			r.files[i].Completed = true
			ordinals = append(ordinals, i)
		}
	}
	var sum int64
	for i := 0; i < next && i < len(r.files); i++ {
		sum += r.files[i].Size
	}
	if next >= r.next {
		r.next, r.nextOffset = next, offset
	}
	r.durableBytes = sum + offset
	if r.live < r.durableBytes {
		r.live = r.durableBytes
	}
	if len(ordinals) > 0 {
		_ = e.commitFiles(r, ordinals)
	}
}

// Accept starts an offered incoming transfer.
func (e *Engine) Accept(id string) error {
	e.mu.Lock()
	r, err := e.get(id)
	if err == nil && (r.t.Direction != "receive" || r.t.State != "offered") {
		err = ErrConflict
	}
	if err == nil {
		err = e.loadFiles(r)
	}
	if err == nil {
		err = e.prepareIncomingLocked(r)
	}
	if err != nil {
		e.mu.Unlock()
		return err
	}
	r.t.State = "reconnecting"
	r.t.UpdatedAt = now()
	if err = e.saveHeader(r); err != nil {
		e.mu.Unlock()
		return err
	}
	device := r.t.DeviceID
	e.mu.Unlock()
	if c := e.opts.Session(device); c != nil {
		e.startReceiver(id, c)
	} else if e.opts.Connect != nil {
		e.opts.Connect(device)
	}
	e.changed()
	return nil
}

// Pause stops a transfer on both devices; Resume continues it.
func (e *Engine) Pause(id string) error {
	e.mu.Lock()
	r, err := e.get(id)
	if err != nil {
		e.mu.Unlock()
		return err
	}
	if terminal(r.t.State) || r.t.State == "offered" && r.t.Direction == "receive" {
		e.mu.Unlock()
		return ErrConflict
	}
	device, direction := r.t.DeviceID, r.t.Direction
	if direction == "send" {
		e.stopSendingLocked(id)
		r.t.State = "paused"
		r.t.UpdatedAt = now()
		r.live = r.durableBytes
		_ = e.saveHeader(r)
		e.mu.Unlock()
		e.send(e.opts.Session(device), commandMsg{Type: "pause", Transfer: id})
		e.changed()
		return nil
	}
	e.mu.Unlock()
	e.stopReceiver(id, "paused", "")
	e.mu.Lock()
	status := r.status()
	e.mu.Unlock()
	e.send(e.opts.Session(device), status)
	e.changed()
	return nil
}

func (e *Engine) Resume(id string) error {
	e.mu.Lock()
	r, err := e.get(id)
	if err != nil {
		e.mu.Unlock()
		return err
	}
	if err = e.loadFiles(r); err != nil {
		e.mu.Unlock()
		return err
	}
	view := r.view(false)
	resumable := r.t.State == "paused" || r.t.State == "reconnecting" || r.t.State == "offered" && r.t.Direction == "send" || view.Retryable
	if !resumable {
		e.mu.Unlock()
		return ErrConflict
	}
	device, direction := r.t.DeviceID, r.t.Direction
	if direction == "send" {
		if r.t.State != "offered" {
			r.t.State = "reconnecting"
		}
		r.t.Error = ""
		r.t.UpdatedAt = now()
		_ = e.saveHeader(r)
		c := e.opts.Session(device)
		if c != nil {
			if r.offered {
				e.send(c, commandMsg{Type: "resume", Transfer: id})
			} else {
				e.sendOfferLocked(r, c)
			}
		}
		e.mu.Unlock()
		if c == nil && e.opts.Connect != nil {
			e.opts.Connect(device)
		}
		e.changed()
		return nil
	}
	r.t.State = "reconnecting"
	r.t.Error = ""
	r.t.UpdatedAt = now()
	_ = e.saveHeader(r)
	e.mu.Unlock()
	if c := e.opts.Session(device); c != nil {
		e.startReceiver(id, c)
	} else if e.opts.Connect != nil {
		e.opts.Connect(device)
	}
	e.changed()
	return nil
}

// Cancel ends a transfer on both devices. Published files are kept.
func (e *Engine) Cancel(id string) error {
	e.mu.Lock()
	r, err := e.get(id)
	if err != nil {
		e.mu.Unlock()
		return err
	}
	if terminal(r.t.State) {
		e.mu.Unlock()
		return ErrConflict
	}
	device, direction := r.t.DeviceID, r.t.Direction
	if direction == "send" {
		e.stopSendingLocked(id)
		r.t.State = "cancelled"
		r.t.Error = ""
		r.t.UpdatedAt = now()
		_ = e.saveHeader(r)
		e.mu.Unlock()
		e.send(e.opts.Session(device), commandMsg{Type: "cancel", Transfer: id})
		e.changed()
		return nil
	}
	e.mu.Unlock()
	e.stopReceiver(id, "cancelled", "")
	e.mu.Lock()
	status := r.status()
	e.mu.Unlock()
	e.send(e.opts.Session(device), status)
	e.changed()
	return nil
}

// failSend ends an outgoing transfer that cannot continue, for example after
// its source changed, and tells the receiver.
func (e *Engine) failSend(id string, cause error) {
	e.mu.Lock()
	r := e.records[id]
	if r == nil || terminal(r.t.State) {
		e.mu.Unlock()
		return
	}
	e.stopSendingLocked(id)
	r.t.State = "failed"
	r.t.Error = cause.Error()
	r.t.UpdatedAt = now()
	r.live = r.durableBytes
	_ = e.saveHeader(r)
	device := r.t.DeviceID
	e.mu.Unlock()
	e.send(e.opts.Session(device), commandMsg{Type: "cancel", Transfer: id, Reason: cause.Error()})
	e.changed()
}

func nextEpoch() uint64 { return epochs.Add(1) }
