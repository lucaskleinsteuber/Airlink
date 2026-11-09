package relay

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func hashBytes(b []byte) string { v := sha256.Sum256(b); return hex.EncodeToString(v[:]) }
func browsers() (model.Device, model.Device) {
	return model.Device{ID: strings.Repeat("a", 48), Name: "Sender", Kind: "browser", Paired: true, CanSend: true, CanRelay: true}, model.Device{ID: strings.Repeat("b", 48), Name: "Receiver", Kind: "browser", Paired: true, CanReceive: true, CanRelay: true}
}
func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, dir
}
func offered(t *testing.T, s *Store, payload []byte) model.Relay {
	t.Helper()
	a, b := browsers()
	v, err := s.Create(a, b, []model.File{{ID: "file", Path: "file.bin", Size: int64(len(payload)), SHA256: hashBytes(payload)}})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func accepted(t *testing.T, s *Store, payload []byte) model.Relay {
	t.Helper()
	v := offered(t, s, payload)
	if err := s.Accept(v.ID, v.ReceiverID); err != nil {
		t.Fatal(err)
	}
	return v
}
func pendingRequest(t *testing.T, s *Store, v model.Relay) model.RelayRequest {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		request, err := s.NextRequest(v.ID, v.SenderID)
		if err != nil {
			t.Fatal(err)
		}
		if request != nil {
			return *request
		}
		if time.Now().After(deadline) {
			t.Fatal("receiver did not request a block")
		}
		time.Sleep(time.Millisecond)
	}
}

type fetched struct {
	b   []byte
	p   *Block
	err error
}

func supplyAndFetch(t *testing.T, s *Store, op *Stream, v model.Relay, payload []byte, index int64) ([]byte, *Block, <-chan error) {
	t.Helper()
	got := make(chan fetched, 1)
	go func() { b, p, err := s.Fetch(op, "file", index); got <- fetched{b, p, err} }()
	req := pendingRequest(t, s, v)
	ack := make(chan error, 1)
	go func() {
		ack <- s.Supply(context.Background(), v.ID, v.SenderID, "file", index, req.RequestID, hashBytes(payload), bytes.NewReader(payload))
	}()
	select {
	case result := <-got:
		if result.err != nil {
			t.Fatal(result.err)
		}
		return result.b, result.p, ack
	case <-time.After(3 * time.Second):
		t.Fatal("block was not relayed")
	}
	return nil, nil, nil
}

func TestReceiverApprovalACLAndImmutableManifest(t *testing.T) {
	s, _ := newStore(t)
	v := offered(t, s, []byte("hello"))
	if _, err := s.Get(v.ID, "other"); !errors.Is(err, ErrNotFound) {
		t.Fatal("third device read relay", err)
	}
	if err := s.Accept(v.ID, v.SenderID); !errors.Is(err, ErrForbidden) {
		t.Fatal("sender accepted its own offer", err)
	}
	if err := s.Action(v.ID, v.SenderID, "resume"); !errors.Is(err, ErrConflict) {
		t.Fatal("resume bypassed approval", err)
	}
	if _, err := s.BeginStream(context.Background(), v.ID, v.ReceiverID); !errors.Is(err, ErrConflict) {
		t.Fatal("unapproved receiver downloaded", err)
	}
	a, b := browsers()
	for _, file := range []model.File{{ID: "x", Path: "../escape", Size: 1, SHA256: hashBytes([]byte("x"))}, {ID: "x", Path: "ok", Size: 1}, {ID: "x", Path: "zero", SHA256: hashBytes([]byte("wrong"))}} {
		if _, err := s.Create(a, b, []model.File{file}); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid manifest accepted", err)
		}
	}
	a.CanRelay = false
	if _, err := s.Create(a, b, v.Files); !errors.Is(err, ErrForbidden) {
		t.Fatal("permission bypass", err)
	}
}

func TestSourceAckWaitsForTransportAndPayloadNeverTouchesDisk(t *testing.T) {
	s, dir := newStore(t)
	payload := bytes.Repeat([]byte("PAYLOAD_ONLY_NOT_METADATA_"), 12000)
	v := accepted(t, s, payload)
	op, err := s.BeginStream(context.Background(), v.ID, v.ReceiverID)
	if err != nil {
		t.Fatal(err)
	}
	data, block, ack := supplyAndFetch(t, s, op, v, payload, 0)
	if !bytes.Equal(data, payload) {
		t.Fatal("payload corrupted")
	}
	select {
	case err = <-ack:
		t.Fatal("source got ACK before transport", err)
	default:
	}
	if err = s.Confirm(block, 0, int64(len(payload)), nil); err != nil {
		t.Fatal(err)
	}
	if err = <-ack; err != nil {
		t.Fatal(err)
	}
	op.Close(true)
	got, _ := s.Get(v.ID, v.ReceiverID)
	if got.State != "delivered" || got.DeliveredBytes != int64(len(payload)) {
		t.Fatal("wrong delivery checkpoint", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "relays.sqlite") {
			t.Fatal("relay created a payload file", entry.Name())
		}
		b, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, payload[:128]) {
			t.Fatal("payload was persisted")
		}
	}
}

func TestRangeUnionAndConfirmedHashSurviveRestart(t *testing.T) {
	s, dir := newStore(t)
	payload := append(bytes.Repeat([]byte("a"), int(model.ChunkSize)), []byte("tail")...)
	v := accepted(t, s, payload)
	op, err := s.BeginStream(context.Background(), v.ID, v.ReceiverID)
	if err != nil {
		t.Fatal(err)
	}
	_, block, ack := supplyAndFetch(t, s, op, v, payload[:model.ChunkSize], 0)
	if err = s.Confirm(block, 0, 4, nil); err != nil {
		t.Fatal(err)
	}
	if err = <-ack; err != nil {
		t.Fatal(err)
	}
	op.Close(true)
	op, err = s.BeginStream(context.Background(), v.ID, v.ReceiverID)
	if err != nil {
		t.Fatal(err)
	}
	_, block, ack = supplyAndFetch(t, s, op, v, payload[:model.ChunkSize], 0)
	if err = s.Confirm(block, 2, 6, nil); err != nil {
		t.Fatal(err)
	}
	<-ack
	op.Close(true)
	got, _ := s.Get(v.ID, v.ReceiverID)
	if got.DeliveredBytes != 6 {
		t.Fatal("overlapping retries counted twice", got.DeliveredBytes)
	}
	s.Close()
	s, err = New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	op, err = s.BeginStream(context.Background(), v.ID, v.ReceiverID)
	if err != nil {
		t.Fatal(err)
	}
	defer op.Close(false)
	wait := make(chan error, 1)
	go func() { _, _, err := s.Fetch(op, "file", 0); wait <- err }()
	req := pendingRequest(t, s, v)
	changed := append([]byte{}, payload[:model.ChunkSize]...)
	changed[0] = 'z'
	if err = s.Supply(context.Background(), v.ID, v.SenderID, "file", 0, req.RequestID, hashBytes(changed), bytes.NewReader(changed)); !errors.Is(err, ErrIntegrity) {
		t.Fatal("changed confirmed block accepted", err)
	}
	select {
	case <-wait:
	case <-time.After(time.Second):
		t.Fatal("failed source didn't interrupt receiver")
	}
}

func TestPauseCancelRestartInterruptRequestsAndRespectTwoStreamBound(t *testing.T) {
	s, dir := newStore(t)
	one := accepted(t, s, []byte("1"))
	two := accepted(t, s, []byte("2"))
	three := accepted(t, s, []byte("3"))
	a, err := s.BeginStream(context.Background(), one.ID, one.ReceiverID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.BeginStream(context.Background(), two.ID, two.ReceiverID)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close(false)
	third := make(chan *Stream, 1)
	go func() { op, _ := s.BeginStream(context.Background(), three.ID, three.ReceiverID); third <- op }()
	deadline := time.Now().Add(time.Second)
	for {
		got, _ := s.Get(three.ID, three.ReceiverID)
		if got.State == "queued" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("third stream wasn't queued")
		}
		time.Sleep(time.Millisecond)
	}
	if len(s.slots) != 2 {
		t.Fatal("stream memory bound exceeded")
	}
	select {
	case <-third:
		t.Fatal("third stream bypassed two-slot limit")
	default:
	}
	if err = s.Action(one.ID, one.ReceiverID, "pause"); err != nil {
		t.Fatal(err)
	}
	if a.Context().Err() == nil {
		t.Fatal("pause failed to interrupt stream")
	}
	a.Close(false)
	var c *Stream
	select {
	case c = <-third:
		if c == nil {
			t.Fatal("queued stream failed")
		}
	case <-time.After(time.Second):
		t.Fatal("slot not released")
	}
	defer c.Close(false)
	if err = s.Action(one.ID, one.ReceiverID, "resume"); err != nil {
		t.Fatal(err)
	}
	s.CancelDevice(three.SenderID)
	if c.Context().Err() == nil || b.Context().Err() == nil {
		t.Fatal("revoke did not interrupt both participants' streams")
	}
	b.Close(false)
	c.Close(false)
	four := accepted(t, s, []byte("restart"))
	op, err := s.BeginStream(context.Background(), four.ID, four.ReceiverID)
	if err != nil {
		t.Fatal(err)
	}
	waiting := make(chan error, 1)
	go func() { _, _, err := s.Fetch(op, "file", 0); waiting <- err }()
	req := pendingRequest(t, s, four)
	s.Close()
	op.Close(false)
	select {
	case <-waiting:
	case <-time.After(time.Second):
		t.Fatal("close didn't interrupt receiver")
	}
	s, err = New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, _ := s.Get(four.ID, four.ReceiverID)
	if got.State != "reconnecting" {
		t.Fatal("restart claimed an active transport", got.State)
	}
	if request, _ := s.NextRequest(four.ID, four.SenderID); request != nil {
		t.Fatal("RAM request survived restart")
	}
	if err = s.Supply(context.Background(), four.ID, four.SenderID, "file", 0, req.RequestID, hashBytes([]byte("restart")), bytes.NewReader([]byte("restart"))); !errors.Is(err, ErrConflict) {
		t.Fatal("stale request accepted after restart", err)
	}
}

func TestParticipantHistoryClearRetainsOtherPartyAndStopsSourceRequests(t *testing.T) {
	s, _ := newStore(t)
	payload := []byte("delivered")
	v := accepted(t, s, payload)
	op, err := s.BeginStream(context.Background(), v.ID, v.ReceiverID)
	if err != nil {
		t.Fatal(err)
	}
	_, block, ack := supplyAndFetch(t, s, op, v, payload, 0)
	if err = s.Confirm(block, 0, int64(len(payload)), nil); err != nil {
		t.Fatal(err)
	}
	<-ack
	op.Close(true)
	if err = s.Clear(v.SenderID); err != nil {
		t.Fatal(err)
	}
	if len(s.List(v.SenderID)) != 0 || len(s.List(v.ReceiverID)) != 1 {
		t.Fatal("clear removed another participant's history")
	}
	if _, err = s.BeginStream(context.Background(), v.ID, v.ReceiverID); !errors.Is(err, ErrUnavailable) {
		t.Fatal("source history removal left a hanging download", err)
	}
	if err = s.Clear(""); err != nil {
		t.Fatal(err)
	}
	if len(s.List("")) != 0 {
		t.Fatal("host clear didn't delete terminal metadata")
	}
}

type callbackEOFReader struct {
	io.Reader
	once     sync.Once
	callback func()
}

func (r *callbackEOFReader) Read(b []byte) (int, error) {
	n, err := r.Reader.Read(b)
	if errors.Is(err, io.EOF) {
		r.once.Do(r.callback)
	}
	return n, err
}
func TestStaleSourceHashFailureCannotPoisonResumedEpoch(t *testing.T) {
	s, _ := newStore(t)
	payload := []byte("original")
	v := accepted(t, s, payload)
	op, err := s.BeginStream(context.Background(), v.ID, v.ReceiverID)
	if err != nil {
		t.Fatal(err)
	}
	defer op.Close(false)
	fetch := make(chan error, 1)
	go func() { _, _, err := s.Fetch(op, "file", 0); fetch <- err }()
	request := pendingRequest(t, s, v)
	reader := &callbackEOFReader{Reader: bytes.NewReader(payload), callback: func() {
		if err := s.Action(v.ID, v.ReceiverID, "pause"); err != nil {
			t.Error(err)
		}
		if err := s.Action(v.ID, v.ReceiverID, "resume"); err != nil {
			t.Error(err)
		}
	}}
	err = s.Supply(context.Background(), v.ID, v.SenderID, "file", 0, request.RequestID, hashBytes([]byte("incorrect header hash")), reader)
	if !errors.Is(err, ErrConflict) {
		t.Fatal("stale source frame wasn't rejected", err)
	}
	<-fetch
	if err = s.FailForStream(op, ErrIntegrity); !errors.Is(err, ErrConflict) {
		t.Fatal("stale full-file hash failure was accepted", err)
	}
	got, _ := s.Get(v.ID, v.ReceiverID)
	if got.State != "ready" {
		t.Fatal("old operation poisoned resumed relay", got.State)
	}
}

func TestOverlappingSourceBlocksPinIdentityBeforeEitherTransportReceipt(t *testing.T) {
	s, _ := newStore(t)
	payload := append(bytes.Repeat([]byte("a"), int(model.ChunkSize)), byte('b'))
	v := accepted(t, s, payload)
	first, err := s.BeginStream(context.Background(), v.ID, v.ReceiverID)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close(false)
	_, _, ack := supplyAndFetch(t, s, first, v, payload[:model.ChunkSize], 0)
	second, err := s.BeginStream(context.Background(), v.ID, v.ReceiverID)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(false)
	waiting := make(chan error, 1)
	go func() { _, _, err := s.Fetch(second, "file", 0); waiting <- err }()
	request := pendingRequest(t, s, v)
	changed := append([]byte{}, payload[:model.ChunkSize]...)
	changed[0] = 'z'
	if err = s.Supply(context.Background(), v.ID, v.SenderID, "file", 0, request.RequestID, hashBytes(changed), bytes.NewReader(changed)); !errors.Is(err, ErrIntegrity) {
		t.Fatal("conflicting overlapping response accepted before first receipt", err)
	}
	select {
	case <-waiting:
	case <-time.After(time.Second):
		t.Fatal("second receiver remained blocked")
	}
	select {
	case <-ack:
	case <-time.After(time.Second):
		t.Fatal("first source PUT remained blocked")
	}
	got, _ := s.Get(v.ID, v.ReceiverID)
	if got.DeliveredBytes != 0 {
		t.Fatal("RAM readiness was mistaken for transport delivery")
	}
}
