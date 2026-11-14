package peer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// Each side sends a heartbeat at this interval from its writer, independent
	// of its reader, so a peer that is busy writing to disk still looks alive.
	heartbeatInterval = 15 * time.Second
	// A session without any received frame for this long is considered dead.
	idleTimeout = 50 * time.Second
	// Queued data frames per stream (each at most MaxDataPayload).
	streamSlots = 8
)

// ErrClosed reports use of a closed session.
var ErrClosed = errors.New("peer connection closed")

// Hello is exchanged once in each direction after the TLS handshake.
type Hello struct {
	Type     string `json:"type"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	Protocol int    `json:"protocol"`
	// Boot changes with every service start; it lets a peer tell a restarted
	// device's new connection from a simultaneous duplicate.
	Boot string `json:"boot"`
	// Port is the TLS port this device accepts connections on.
	Port int `json:"port"`
}

// Envelope is one decoded control message. Body is the complete JSON object.
// Stream is non-zero for messages sent with SendOrdered.
type Envelope struct {
	Type   string
	Body   []byte
	Stream uint32
}

// Decode unmarshals the complete control message into v.
func (e Envelope) Decode(v any) error { return json.Unmarshal(e.Body, v) }

// Handler receives inbound traffic. Calls happen sequentially on the
// session's reader goroutine.
type Handler interface {
	HandleControl(s *Session, e Envelope)
	// HandleData receives one data frame. The handler owns buf and must pass it
	// to ReleaseBuffer when done. Blocking applies backpressure to the peer.
	HandleData(s *Session, stream uint32, buf []byte)
	// Closed is called once, after both connection loops have stopped.
	Closed(s *Session, err error)
}

var bufferPool = sync.Pool{New: func() any { b := make([]byte, MaxDataPayload); return &b }}

// GetBuffer returns a pooled buffer of MaxDataPayload bytes.
func GetBuffer() []byte { return (*bufferPool.Get().(*[]byte))[:MaxDataPayload] }

// ReleaseBuffer returns a buffer obtained from GetBuffer or HandleData.
func ReleaseBuffer(b []byte) {
	if cap(b) != MaxDataPayload {
		return
	}
	b = b[:MaxDataPayload]
	bufferPool.Put(&b)
}

type outFrame struct {
	kind    byte
	stream  uint32
	payload []byte
	release func()
	slot    bool
}

type outStream struct {
	frames  []outFrame
	slots   chan struct{}
	closing bool
}

// Session is one authenticated connection to a paired device.
type Session struct {
	conn        net.Conn
	Peer        Hello
	Initiator   bool
	Fingerprint string
	RemoteIP    string

	handler Handler
	mu      sync.Mutex
	control []outFrame
	streams map[uint32]*outStream
	order   []uint32
	cursor  int
	wake    chan struct{}
	nextID  atomic.Uint32
	closed  chan struct{}
	once    sync.Once
	err     error
	writer  sync.WaitGroup
	started time.Time
	// queued counts frames handed to the session; settled counts frames that
	// were flushed to the connection or dropped.
	queued  atomic.Int64
	settled atomic.Int64
}

func newSession(conn net.Conn, peer Hello, fingerprint string, initiator bool, handler Handler) *Session {
	s := &Session{conn: conn, Peer: peer, Initiator: initiator, Fingerprint: fingerprint, handler: handler, streams: map[uint32]*outStream{}, wake: make(chan struct{}, 1), closed: make(chan struct{}), started: time.Now()}
	if host, _, err := net.SplitHostPort(conn.RemoteAddr().String()); err == nil {
		s.RemoteIP = host
	}
	return s
}

// PeerID is the paired device ID verified during the handshake.
func (s *Session) PeerID() string { return s.Peer.ID }

// Done is closed when the session ends.
func (s *Session) Done() <-chan struct{} { return s.closed }

// Err reports why the session ended (nil while it is open).
func (s *Session) Err() error {
	select {
	case <-s.closed:
		return s.err
	default:
		return nil
	}
}

// Send queues a control message ahead of data. It never blocks on the network.
func (s *Session) Send(v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(body) > MaxControlPayload {
		return fmt.Errorf("control message too large: %d bytes", len(body))
	}
	s.mu.Lock()
	if s.isClosed() {
		s.mu.Unlock()
		return ErrClosed
	}
	s.control = append(s.control, outFrame{kind: frameControl, payload: body})
	s.queued.Add(1)
	s.mu.Unlock()
	s.signal()
	return nil
}

// OpenStream allocates a stream for one transfer's ordered data and messages.
func (s *Session) OpenStream() uint32 {
	id := s.nextID.Add(1)
	s.mu.Lock()
	s.streams[id] = &outStream{slots: make(chan struct{}, streamSlots)}
	s.order = append(s.order, id)
	s.mu.Unlock()
	return id
}

// SendOrdered queues a control message on a stream, after the stream's
// previously queued data.
func (s *Session) SendOrdered(stream uint32, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(body) > MaxControlPayload {
		return fmt.Errorf("control message too large: %d bytes", len(body))
	}
	s.mu.Lock()
	st := s.streams[stream]
	if s.isClosed() || st == nil || st.closing {
		s.mu.Unlock()
		return ErrClosed
	}
	st.frames = append(st.frames, outFrame{kind: frameControl, stream: stream, payload: body})
	s.queued.Add(1)
	s.mu.Unlock()
	s.signal()
	return nil
}

// SendData queues one data frame on a stream. It blocks while the stream's
// queue is full. release is called once the frame was written or dropped.
func (s *Session) SendData(ctx context.Context, stream uint32, payload []byte, release func()) error {
	if len(payload) == 0 || len(payload) > MaxDataPayload {
		if release != nil {
			release()
		}
		return fmt.Errorf("invalid data frame length %d", len(payload))
	}
	s.mu.Lock()
	st := s.streams[stream]
	s.mu.Unlock()
	if st == nil {
		if release != nil {
			release()
		}
		return ErrClosed
	}
	select {
	case st.slots <- struct{}{}:
	case <-ctx.Done():
		if release != nil {
			release()
		}
		return ctx.Err()
	case <-s.closed:
		if release != nil {
			release()
		}
		return ErrClosed
	}
	s.mu.Lock()
	if s.isClosed() || s.streams[stream] != st || st.closing {
		s.mu.Unlock()
		<-st.slots
		if release != nil {
			release()
		}
		return ErrClosed
	}
	st.frames = append(st.frames, outFrame{kind: frameData, stream: stream, payload: payload, release: release, slot: true})
	s.queued.Add(1)
	s.mu.Unlock()
	s.signal()
	return nil
}

// CloseStream lets queued frames drain, then forgets the stream.
func (s *Session) CloseStream(stream uint32) {
	s.mu.Lock()
	if st := s.streams[stream]; st != nil {
		st.closing = true
		if len(st.frames) == 0 {
			s.removeStreamLocked(stream)
		}
	}
	s.mu.Unlock()
}

// DropStream discards a stream's queued frames, for example after a pause.
func (s *Session) DropStream(stream uint32) {
	s.mu.Lock()
	st := s.streams[stream]
	var dropped []outFrame
	if st != nil {
		dropped = st.frames
		st.frames = nil
		st.closing = true
		s.removeStreamLocked(stream)
	}
	s.mu.Unlock()
	s.settled.Add(int64(len(dropped)))
	for _, f := range dropped {
		if f.release != nil {
			f.release()
		}
		if f.slot {
			<-st.slots
		}
	}
}

func (s *Session) removeStreamLocked(stream uint32) {
	delete(s.streams, stream)
	for i, id := range s.order {
		if id == stream {
			s.order = append(s.order[:i], s.order[i+1:]...)
			if s.cursor > i {
				s.cursor--
			}
			break
		}
	}
	if s.cursor >= len(s.order) {
		s.cursor = 0
	}
}

func (s *Session) isClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

func (s *Session) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// pop returns the next frame: control first, then streams in round robin.
func (s *Session) pop() (outFrame, *outStream, bool) {
	for {
		s.mu.Lock()
		if s.isClosed() {
			s.mu.Unlock()
			return outFrame{}, nil, false
		}
		if len(s.control) > 0 {
			f := s.control[0]
			s.control[0] = outFrame{}
			s.control = s.control[1:]
			s.mu.Unlock()
			return f, nil, true
		}
		for n := 0; n < len(s.order); n++ {
			i := (s.cursor + n) % len(s.order)
			id := s.order[i]
			st := s.streams[id]
			if st == nil || len(st.frames) == 0 {
				continue
			}
			f := st.frames[0]
			st.frames[0] = outFrame{}
			st.frames = st.frames[1:]
			s.cursor = (i + 1) % len(s.order)
			if st.closing && len(st.frames) == 0 {
				s.removeStreamLocked(id)
			}
			s.mu.Unlock()
			return f, st, true
		}
		s.mu.Unlock()
		select {
		case <-s.wake:
		case <-s.closed:
			return outFrame{}, nil, false
		}
	}
}

func (s *Session) pending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.control) > 0 {
		return true
	}
	for _, st := range s.streams {
		if len(st.frames) > 0 {
			return true
		}
	}
	return false
}

func (s *Session) writeLoop() {
	defer s.writer.Done()
	bw := bufio.NewWriterSize(s.conn, 256<<10)
	header := make([]byte, headerSize)
	var unflushed int64
	for {
		f, st, ok := s.pop()
		if !ok {
			return
		}
		putHeader(header, f.kind, f.stream, len(f.payload))
		_, err := bw.Write(header)
		if err == nil {
			_, err = bw.Write(f.payload)
		}
		unflushed++
		if err == nil && !s.pending() {
			if err = bw.Flush(); err == nil {
				s.settled.Add(unflushed)
				unflushed = 0
			}
		}
		if f.release != nil {
			f.release()
		}
		if f.slot && st != nil {
			<-st.slots
		}
		if err != nil {
			s.shutdown(err)
			return
		}
	}
}

func (s *Session) heartbeat() {
	defer s.writer.Done()
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.closed:
			return
		case <-ticker.C:
			_ = s.Send(map[string]string{"type": "ping"})
		}
	}
}

// run starts the writer and reads until the connection ends.
func (s *Session) run() {
	s.writer.Add(2)
	go s.writeLoop()
	go s.heartbeat()
	err := s.readLoop()
	s.shutdown(err)
	s.writer.Wait()
	s.handler.Closed(s, s.err)
}

func (s *Session) readLoop() error {
	br := bufio.NewReaderSize(s.conn, 256<<10)
	header := make([]byte, headerSize)
	for {
		if err := s.conn.SetReadDeadline(time.Now().Add(idleTimeout)); err != nil {
			return err
		}
		h, err := readHeader(br, header)
		if err != nil {
			return err
		}
		switch h.kind {
		case frameControl:
			body := make([]byte, h.length)
			if _, err = io.ReadFull(br, body); err != nil {
				return err
			}
			var head struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(body, &head) != nil || head.Type == "" || len(head.Type) > 64 {
				return fmt.Errorf("%w: malformed control message", errFrame)
			}
			if head.Type == "ping" {
				continue
			}
			s.handler.HandleControl(s, Envelope{Type: head.Type, Body: body, Stream: h.stream})
		case frameData:
			buf := GetBuffer()
			if _, err = io.ReadFull(br, buf[:h.length]); err != nil {
				ReleaseBuffer(buf)
				return err
			}
			s.handler.HandleData(s, h.stream, buf[:h.length])
		}
	}
}

// CloseAfterFlush lets already queued messages reach the peer (up to
// timeout), then ends the session.
func (s *Session) CloseAfterFlush(reason error, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for s.settled.Load() < s.queued.Load() && time.Now().Before(deadline) && !s.isClosed() {
		time.Sleep(5 * time.Millisecond)
	}
	s.Close(reason)
}

// Close ends the session.
func (s *Session) Close(reason error) {
	if reason == nil {
		reason = ErrClosed
	}
	s.shutdown(reason)
}

func (s *Session) shutdown(reason error) {
	s.once.Do(func() {
		s.mu.Lock()
		s.err = reason
		close(s.closed)
		control := s.control
		s.control = nil
		streams := s.streams
		s.streams = map[uint32]*outStream{}
		s.order = nil
		s.mu.Unlock()
		_ = s.conn.Close()
		for _, f := range control {
			if f.release != nil {
				f.release()
			}
		}
		for _, st := range streams {
			for _, f := range st.frames {
				if f.release != nil {
					f.release()
				}
			}
		}
	})
}

func writeHello(w io.Writer, h Hello) error {
	body, err := json.Marshal(h)
	if err != nil {
		return err
	}
	frame := make([]byte, headerSize+len(body))
	putHeader(frame, frameControl, 0, len(body))
	copy(frame[headerSize:], body)
	_, err = w.Write(frame)
	return err
}

func readHello(r io.Reader) (Hello, error) {
	header := make([]byte, headerSize)
	h, err := readHeader(r, header)
	if err != nil {
		return Hello{}, err
	}
	if h.kind != frameControl || h.length > 16<<10 {
		return Hello{}, fmt.Errorf("%w: expected hello", errFrame)
	}
	body := make([]byte, h.length)
	if _, err = io.ReadFull(r, body); err != nil {
		return Hello{}, err
	}
	var hello Hello
	if err = json.Unmarshal(body, &hello); err != nil || hello.Type != "hello" {
		return Hello{}, fmt.Errorf("%w: expected hello", errFrame)
	}
	return hello, nil
}
