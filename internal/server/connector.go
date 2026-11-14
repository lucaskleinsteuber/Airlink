package server

import (
	"net"
	"strconv"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/peer"
	"github.com/lucaskleinsteuber/Airlink/internal/transfer"
)

// Every paired device is kept connected while it is reachable. Either side
// may establish the session; once it exists, both use it, so a device that
// cannot accept incoming connections (for example behind a firewall) still
// receives over the connection it opened itself.

type dialState struct {
	next    time.Time
	backoff time.Duration
	running bool
}

const maxDialBackoff = 30 * time.Second

// HandleControl implements peer.Handler.
func (s *Server) HandleControl(session *peer.Session, e peer.Envelope) {
	switch {
	case transfer.Handles(e.Type):
		s.engine.HandleControl(session, e.Type, e.Stream, e.Body)
	case e.Type == "clipboard":
		s.receiveClipboard(session, e)
	case e.Type == "clipboard.ack":
		s.clipboardAcknowledged(e)
	}
}

// HandleData implements peer.Handler.
func (s *Server) HandleData(session *peer.Session, stream uint32, buf []byte) {
	s.engine.HandleData(session, stream, buf, peer.ReleaseBuffer)
}

// Closed implements peer.Handler.
func (s *Server) Closed(session *peer.Session, _ error) {
	s.engine.SessionClosed(session)
	s.changed()
	select {
	case <-s.ctx.Done():
	default:
		s.requestConnectionAfter(session.PeerID(), time.Second)
	}
}

// sessionOpened records what the connection proved about the device and
// continues its transfers.
func (s *Server) sessionOpened(session *peer.Session) {
	id := session.PeerID()
	hello := session.Peer
	_ = s.store.edit(id, func(current *deviceRecord) {
		if current.Device.Fingerprint != session.Fingerprint {
			return
		}
		if validDiscoveryName(hello.Name) {
			current.Device.Name = hello.Name
		}
		if ip := net.ParseIP(session.RemoteIP); ip != nil && hello.Port > 0 {
			current.Device.Address = "https://" + net.JoinHostPort(ip.String(), strconv.Itoa(hello.Port))
		}
		current.Device.LastSeen = time.Now().UTC().Format(time.RFC3339)
		if validPlatform(hello.Platform) && validForm(hello.Form) && hello.Platform != "" {
			current.Device.Platform, current.Device.Form = hello.Platform, hello.Form
		}
	})
	s.mu.Lock()
	if st := s.dials[id]; st != nil {
		st.backoff = 0
	}
	s.mu.Unlock()
	s.engine.SessionOpened(session)
	s.changed()
}

// requestConnection asks for an immediate connection attempt.
func (s *Server) requestConnection(deviceID string) { s.requestConnectionAfter(deviceID, 0) }

func (s *Server) requestConnectionAfter(deviceID string, delay time.Duration) {
	s.mu.Lock()
	st := s.dials[deviceID]
	if st == nil {
		st = &dialState{}
		s.dials[deviceID] = st
	}
	st.next = time.Now().Add(delay)
	if delay == 0 {
		st.backoff = 0
	}
	s.mu.Unlock()
	select {
	case s.dialWake <- struct{}{}:
	default:
	}
}

func (s *Server) connectLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		case <-s.dialWake:
		}
		for id, record := range s.store.snapshot().Devices {
			d := record.Device
			if !d.Paired || d.Kind != "engine" || d.Fingerprint == "" || s.peers.Get(id) != nil {
				continue
			}
			candidates := []string{}
			if d.Address != "" {
				candidates = append(candidates, d.Address)
			}
			s.mu.Lock()
			if hint, ok := s.discovered[id]; ok && hint.Fingerprint == d.Fingerprint && hint.Address != d.Address {
				candidates = append(candidates, hint.Address)
			}
			st := s.dials[id]
			if st == nil {
				st = &dialState{}
				s.dials[id] = st
			}
			if st.running || time.Now().Before(st.next) || len(candidates) == 0 {
				s.mu.Unlock()
				continue
			}
			st.running = true
			s.mu.Unlock()
			s.wg.Add(1)
			go func(id, fingerprint string, candidates []string) {
				defer s.wg.Done()
				var err error
				for _, address := range candidates {
					if err = s.peers.Dial(s.ctx, id, address, fingerprint); err == nil {
						break
					}
				}
				s.mu.Lock()
				defer s.mu.Unlock()
				st.running = false
				if err == nil {
					st.backoff = 0
					return
				}
				st.backoff = min(max(2*st.backoff, time.Second), maxDialBackoff)
				st.next = time.Now().Add(st.backoff)
			}(id, d.Fingerprint, candidates)
		}
	}
}
