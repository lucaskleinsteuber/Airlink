package peer

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sync"
	"time"
)

// ErrUntrusted reports a connection from or to an identity that is not a
// paired device.
var ErrUntrusted = errors.New("device is not paired")

// ErrOutdated reports a peer that does not speak protocol 3.
var ErrOutdated = errors.New("the other device needs a newer AirLink version")

// Config wires the manager to the service's identity and trust store.
type Config struct {
	// Certificate is this device's leaf certificate chain ending in its
	// self-signed identity root, with the private key.
	Certificate func() *tls.Certificate
	// Local returns the hello announced to peers.
	Local func() Hello
	// Trusted maps a verified identity fingerprint to a paired device ID.
	Trusted func(fingerprint string) (deviceID string, ok bool)
	// Handler receives traffic of every session.
	Handler Handler
	// Opened is called after a session was registered as the active one.
	Opened func(s *Session)
}

// Manager keeps at most one active session per paired device.
type Manager struct {
	cfg      Config
	mu       sync.Mutex
	sessions map[string]*Session
	dialing  map[string]bool
	closed   bool
	wg       sync.WaitGroup
}

func NewManager(cfg Config) *Manager {
	return &Manager{cfg: cfg, sessions: map[string]*Session{}, dialing: map[string]bool{}}
}

// Get returns the active session for a device, or nil.
func (m *Manager) Get(deviceID string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[deviceID]
}

// Connected lists device IDs with an active session.
func (m *Manager) Connected() map[string]*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]*Session, len(m.sessions))
	for id, s := range m.sessions {
		out[id] = s
	}
	return out
}

// Disconnect closes a device's session, for example after revocation, after
// letting queued messages (such as cancellations) reach the device.
func (m *Manager) Disconnect(deviceID string, reason error) {
	if s := m.Get(deviceID); s != nil {
		s.CloseAfterFlush(reason, 2*time.Second)
	}
}

// Close ends all sessions and waits for their goroutines.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.Unlock()
	for _, s := range sessions {
		s.Close(ErrClosed)
	}
	m.wg.Wait()
}

// VerifyChain checks a presented chain against its own root and returns the
// root's SHA-256 fingerprint. Trust is decided by the fingerprint.
func VerifyChain(certs []*x509.Certificate, usage x509.ExtKeyUsage) (string, error) {
	if len(certs) < 2 {
		return "", errors.New("peer omitted its identity certificate")
	}
	root := certs[len(certs)-1]
	roots := x509.NewCertPool()
	roots.AddCert(root)
	intermediates := x509.NewCertPool()
	for _, c := range certs[1 : len(certs)-1] {
		intermediates.AddCert(c)
	}
	if _, err := certs[0].Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{usage}}); err != nil {
		return "", err
	}
	sum := sha256.Sum256(root.Raw)
	return hex.EncodeToString(sum[:]), nil
}

// ServeConn runs an accepted protocol 3 connection until it ends. The TLS
// handshake has completed; the client must present a paired identity.
func (m *Manager) ServeConn(conn *tls.Conn) {
	state := conn.ConnectionState()
	if state.NegotiatedProtocol != ALPN || state.Version != tls.VersionTLS13 {
		_ = conn.Close()
		return
	}
	fingerprint, err := VerifyChain(state.PeerCertificates, x509.ExtKeyUsageClientAuth)
	if err != nil {
		_ = conn.Close()
		return
	}
	deviceID, ok := m.cfg.Trusted(fingerprint)
	if !ok {
		_ = conn.Close()
		return
	}
	s, err := m.handshake(conn, deviceID, fingerprint, false)
	if err != nil {
		_ = conn.Close()
		return
	}
	if !m.register(s) {
		return
	}
	s.run()
	m.unregister(s)
}

// Dial connects to a paired device at address (https://host:port) unless a
// session already exists or a dial is in progress. The session keeps running
// in the background after Dial returns.
func (m *Manager) Dial(ctx context.Context, deviceID, address, fingerprint string) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	if m.sessions[deviceID] != nil {
		m.mu.Unlock()
		return nil
	}
	if m.dialing[deviceID] {
		m.mu.Unlock()
		return errors.New("connection attempt in progress")
	}
	m.dialing[deviceID] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.dialing, deviceID)
		m.mu.Unlock()
	}()
	hostport, err := dialAddress(address)
	if err != nil {
		return err
	}
	certificate := m.cfg.Certificate()
	config := &tls.Config{
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{ALPN},
		Certificates:       []tls.Certificate{*certificate},
		InsecureSkipVerify: true, // replaced by the pinned identity check below
		VerifyConnection: func(cs tls.ConnectionState) error {
			got, err := VerifyChain(cs.PeerCertificates, x509.ExtKeyUsageServerAuth)
			if err != nil {
				return err
			}
			if got != fingerprint {
				return fmt.Errorf("%w: device identity changed", ErrUntrusted)
			}
			if cs.NegotiatedProtocol != ALPN {
				return ErrOutdated
			}
			return nil
		},
	}
	dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 6 * time.Second, KeepAlive: 15 * time.Second}, Config: config}
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	raw, err := dialer.DialContext(dialCtx, "tcp", hostport)
	cancel()
	if err != nil {
		if errors.Is(err, ErrOutdated) || isALPNFailure(err) {
			return ErrOutdated
		}
		return err
	}
	conn := raw.(*tls.Conn)
	s, err := m.handshake(conn, deviceID, fingerprint, true)
	if err != nil {
		_ = conn.Close()
		return err
	}
	if !m.register(s) {
		return nil
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		s.run()
		m.unregister(s)
	}()
	return nil
}

func isALPNFailure(err error) bool {
	var alert tls.AlertError
	return errors.As(err, &alert) && alert == 120 // no_application_protocol
}

func dialAddress(address string) (string, error) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.Port() == "" {
		return "", fmt.Errorf("invalid device address %q", address)
	}
	return net.JoinHostPort(u.Hostname(), u.Port()), nil
}

func (m *Manager) handshake(conn *tls.Conn, deviceID, fingerprint string, initiator bool) (*Session, error) {
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	local := m.cfg.Local()
	local.Type = "hello"
	local.Protocol = ProtocolVersion
	if err := writeHello(conn, local); err != nil {
		return nil, err
	}
	hello, err := readHello(conn)
	if err != nil {
		return nil, err
	}
	if hello.ID != deviceID {
		return nil, fmt.Errorf("%w: unexpected device identity", ErrUntrusted)
	}
	if hello.Protocol < ProtocolVersion {
		return nil, ErrOutdated
	}
	if len(hello.Name) > 120 || len(hello.Boot) > 64 || hello.Port < 0 || hello.Port > 65535 {
		return nil, fmt.Errorf("%w: invalid hello", errFrame)
	}
	_ = conn.SetDeadline(time.Time{})
	return newSession(conn, hello, fingerprint, initiator, m.cfg.Handler), nil
}

// register makes s the device's active session. A newer boot of the peer
// replaces an older session; for simultaneous connections of the same boot,
// both sides keep the connection initiated by the lower device ID.
func (m *Manager) register(s *Session) bool {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		s.Close(ErrClosed)
		return false
	}
	local := m.cfg.Local().ID
	existing := m.sessions[s.Peer.ID]
	if existing != nil && existing.Peer.Boot == s.Peer.Boot {
		initiatorOf := func(x *Session) string {
			if x.Initiator {
				return local
			}
			return x.Peer.ID
		}
		winner := min(local, s.Peer.ID)
		if initiatorOf(existing) == winner || initiatorOf(s) != winner {
			m.mu.Unlock()
			s.Close(errors.New("duplicate connection"))
			return false
		}
	}
	m.sessions[s.Peer.ID] = s
	m.mu.Unlock()
	if existing != nil {
		existing.Close(errors.New("replaced by a newer connection"))
	}
	if m.cfg.Opened != nil {
		m.cfg.Opened(s)
	}
	return true
}

func (m *Manager) unregister(s *Session) {
	m.mu.Lock()
	if m.sessions[s.Peer.ID] == s {
		delete(m.sessions, s.Peer.ID)
	}
	m.mu.Unlock()
}
