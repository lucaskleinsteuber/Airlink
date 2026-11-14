package peer

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

type identity struct {
	cert        tls.Certificate
	fingerprint string
}

func newIdentity(t testing.TB) identity {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test root"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	caCert, _ := x509.ParseCertificate(caDER)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "leaf"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, caCert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	fp, err := VerifyChain([]*x509.Certificate{mustParse(leafDER), caCert}, x509.ExtKeyUsageServerAuth)
	if err != nil {
		t.Fatal(err)
	}
	return identity{cert: tls.Certificate{Certificate: [][]byte{leafDER, caDER}, PrivateKey: key}, fingerprint: fp}
}

func mustParse(der []byte) *x509.Certificate {
	c, err := x509.ParseCertificate(der)
	if err != nil {
		panic(err)
	}
	return c
}

type recorder struct {
	mu      sync.Mutex
	control []Envelope
	data    [][]byte
	closed  int
	got     chan struct{}
}

func newRecorder() *recorder { return &recorder{got: make(chan struct{}, 1024)} }
func (r *recorder) HandleControl(_ *Session, e Envelope) {
	r.mu.Lock()
	r.control = append(r.control, e)
	r.mu.Unlock()
	r.got <- struct{}{}
}
func (r *recorder) HandleData(_ *Session, _ uint32, b []byte) {
	r.mu.Lock()
	r.data = append(r.data, append([]byte(nil), b...))
	r.mu.Unlock()
	ReleaseBuffer(b)
	r.got <- struct{}{}
}
func (r *recorder) Closed(*Session, error) {
	r.mu.Lock()
	r.closed++
	r.mu.Unlock()
}

type node struct {
	id       identity
	deviceID string
	manager  *Manager
	handler  *recorder
	address  string
	trusted  map[string]string
	boot     string
}

func newNode(t *testing.T, deviceID string) *node {
	t.Helper()
	n := &node{id: newIdentity(t), deviceID: deviceID, handler: newRecorder(), trusted: map[string]string{}, boot: deviceID + "-boot"}
	n.manager = NewManager(Config{
		Certificate: func() *tls.Certificate { return &n.id.cert },
		Local:       func() Hello { return Hello{ID: n.deviceID, Name: n.deviceID, Version: "test", Boot: n.boot} },
		Trusted: func(fp string) (string, bool) {
			id, ok := n.trusted[fp]
			return id, ok
		},
		Handler: n.handler,
	})
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{n.id.cert}, ClientAuth: tls.RequestClientCert, NextProtos: []string{ALPN}})
	if err != nil {
		t.Fatal(err)
	}
	n.address = "https://127.0.0.1:" + strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				tc := conn.(*tls.Conn)
				if tc.Handshake() != nil {
					tc.Close()
					return
				}
				n.manager.ServeConn(tc)
			}()
		}
	}()
	t.Cleanup(func() { listener.Close(); n.manager.Close() })
	return n
}

func trust(a, b *node) {
	a.trusted[b.id.fingerprint] = b.deviceID
	b.trusted[a.id.fingerprint] = a.deviceID
}

func wait(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timeout")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestMutualSessionCarriesOrderedControlAndData(t *testing.T) {
	a, b := newNode(t, "aaaa"), newNode(t, "bbbb")
	trust(a, b)
	if err := a.manager.Dial(context.Background(), "bbbb", b.address, b.id.fingerprint); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return a.manager.Get("bbbb") != nil && b.manager.Get("aaaa") != nil })
	s := a.manager.Get("bbbb")
	if !s.Initiator || b.manager.Get("aaaa").Initiator || s.Fingerprint != b.id.fingerprint {
		t.Fatal("session roles or identity")
	}
	stream := s.OpenStream()
	payload := bytes.Repeat([]byte("x"), MaxDataPayload)
	buf := GetBuffer()
	copy(buf, payload)
	if err := s.SendData(context.Background(), stream, buf, func() { ReleaseBuffer(buf) }); err != nil {
		t.Fatal(err)
	}
	if err := s.SendOrdered(stream, map[string]any{"type": "after-data"}); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool {
		b.handler.mu.Lock()
		defer b.handler.mu.Unlock()
		return len(b.handler.control) == 1 && len(b.handler.data) == 1
	})
	b.handler.mu.Lock()
	if !bytes.Equal(b.handler.data[0], payload) || b.handler.control[0].Type != "after-data" || b.handler.control[0].Stream != stream {
		t.Fatal("frames corrupted or misrouted")
	}
	b.handler.mu.Unlock()
	// The other direction uses the same connection.
	if err := b.manager.Get("aaaa").Send(map[string]string{"type": "reply"}); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { a.handler.mu.Lock(); defer a.handler.mu.Unlock(); return len(a.handler.control) == 1 })
}

func TestUntrustedAndWrongIdentitiesAreRejected(t *testing.T) {
	a, b := newNode(t, "aaaa"), newNode(t, "bbbb")
	// b does not trust a: the server rejects the client certificate.
	a.trusted[b.id.fingerprint] = "bbbb"
	err := a.manager.Dial(context.Background(), "bbbb", b.address, b.id.fingerprint)
	if err == nil {
		wait(t, func() bool { return a.manager.Get("bbbb") == nil })
	}
	if b.manager.Get("aaaa") != nil {
		t.Fatal("untrusted client accepted")
	}
	// Pinned fingerprint mismatch: the client refuses the server.
	c := newNode(t, "cccc")
	trust(a, c)
	if err = a.manager.Dial(context.Background(), "cccc", c.address, b.id.fingerprint); err == nil {
		t.Fatal("wrong pinned identity accepted")
	}
}

func TestSimultaneousDialsKeepOneConnection(t *testing.T) {
	for range 5 {
		a, b := newNode(t, "aaaa"), newNode(t, "bbbb")
		trust(a, b)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _ = a.manager.Dial(context.Background(), "bbbb", b.address, b.id.fingerprint) }()
		go func() { defer wg.Done(); _ = b.manager.Dial(context.Background(), "aaaa", a.address, a.id.fingerprint) }()
		wg.Wait()
		wait(t, func() bool {
			sa, sb := a.manager.Get("bbbb"), b.manager.Get("aaaa")
			return sa != nil && sb != nil && sa.Initiator != sb.Initiator && sa.Err() == nil && sb.Err() == nil
		})
		// The surviving connection is the one dialed by the lower device ID.
		if !a.manager.Get("bbbb").Initiator {
			t.Fatal("tie-break kept the wrong connection")
		}
		if err := a.manager.Get("bbbb").Send(map[string]string{"type": "probe"}); err != nil {
			t.Fatal(err)
		}
		wait(t, func() bool { b.handler.mu.Lock(); defer b.handler.mu.Unlock(); return len(b.handler.control) > 0 })
	}
}

func TestRestartedPeerReplacesStaleSession(t *testing.T) {
	a, b := newNode(t, "aaaa"), newNode(t, "bbbb")
	trust(a, b)
	if err := a.manager.Dial(context.Background(), "bbbb", b.address, b.id.fingerprint); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return b.manager.Get("aaaa") != nil })
	old := b.manager.Get("aaaa")
	// a restarts: a new boot ID dials while b still holds the old session.
	a.boot = "aaaa-second-boot"
	a.manager.mu.Lock()
	delete(a.manager.sessions, "bbbb")
	a.manager.mu.Unlock()
	if err := a.manager.Dial(context.Background(), "bbbb", b.address, b.id.fingerprint); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool {
		s := b.manager.Get("aaaa")
		return s != nil && s != old && s.Peer.Boot == "aaaa-second-boot"
	})
	wait(t, func() bool { return old.Err() != nil })
}

func TestFrameHeaderValidation(t *testing.T) {
	b := make([]byte, headerSize)
	putHeader(b, frameData, 0, 10)
	if _, err := parseHeader(b); err == nil {
		t.Fatal("data frame on stream 0 accepted")
	}
	putHeader(b, frameControl, 0, MaxControlPayload+1)
	if _, err := parseHeader(b); err == nil {
		t.Fatal("oversized control accepted")
	}
	putHeader(b, 9, 1, 1)
	if _, err := parseHeader(b); err == nil {
		t.Fatal("unknown kind accepted")
	}
	putHeader(b, frameControl, 0, 5)
	b[2] = 1
	if _, err := parseHeader(b); err == nil {
		t.Fatal("reserved bits accepted")
	}
	var hello bytes.Buffer
	if err := writeHello(&hello, Hello{Type: "hello", ID: "x"}); err != nil {
		t.Fatal(err)
	}
	if h, err := readHello(&hello); err != nil || h.ID != "x" {
		t.Fatal(h, err)
	}
	body, _ := json.Marshal(map[string]string{"type": "nothello"})
	frame := make([]byte, headerSize+len(body))
	putHeader(frame, frameControl, 0, len(body))
	copy(frame[headerSize:], body)
	if _, err := readHello(bytes.NewReader(frame)); !errors.Is(err, errFrame) {
		t.Fatal("non-hello accepted", err)
	}
}

func FuzzParseHeader(f *testing.F) {
	seed := make([]byte, headerSize)
	putHeader(seed, frameData, 1, 100)
	f.Add(seed)
	f.Fuzz(func(t *testing.T, b []byte) {
		h, err := parseHeader(b)
		if err != nil {
			return
		}
		if h.kind != frameControl && h.kind != frameData || h.length == 0 || h.length > MaxDataPayload && h.kind == frameData || h.length > MaxControlPayload {
			t.Fatalf("accepted invalid header %+v", h)
		}
	})
}
