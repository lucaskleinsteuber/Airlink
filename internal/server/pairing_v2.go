package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/engine"
	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

// All pairing receipts are private persisted host data. The local comparison
// code and bearer credentials are never included in any public pairing reply.
type pairingRecord struct {
	ID              string       `json:"id"`
	Initiator       bool         `json:"initiator"`
	Peer            model.Device `json:"peer"`
	RemoteID        string       `json:"remoteId"`
	Nonce           string       `json:"nonce"`
	Secret          string       `json:"secret,omitempty"`
	SecretHash      string       `json:"secretHash,omitempty"`
	ReturnToken     string       `json:"returnToken,omitempty"`
	GrantToken      string       `json:"grantToken,omitempty"`
	Code            string       `json:"code"`
	Expires         time.Time    `json:"expires"`
	State           string       `json:"state"`
	LocalConfirmed  bool         `json:"localConfirmed"`
	RemoteConfirmed bool         `json:"remoteConfirmed"`
	Automatic       bool         `json:"automatic"`
	Granted         bool         `json:"granted"`
	Error           string       `json:"error,omitempty"`
}

func (p pairingRecord) snapshot() model.PairingSession {
	state := p.State
	if !p.Granted && time.Now().After(p.Expires) && (state == "starting" || state == "comparing") {
		state = "expired"
	}
	code := p.Code
	if state != "comparing" {
		code = ""
	}
	return model.PairingSession{ID: p.ID, PeerID: p.Peer.ID, PeerName: p.Peer.Name, State: state, ComparisonCode: code, ExpiresAt: p.Expires.UTC().Format(time.RFC3339), LocalConfirmed: p.LocalConfirmed, RemoteConfirmed: p.RemoteConfirmed, AutomaticReceive: p.Automatic, Error: p.Error}
}

func (s *Server) pairingSnapshots() []model.PairingSession {
	p := s.store.snapshot()
	out := []model.PairingSession{}
	for _, v := range p.Pairings {
		out = append(out, v.snapshot())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func pairingHex(v string, n int) bool {
	b, e := hex.DecodeString(v)
	return e == nil && len(b) == n && strings.ToLower(v) == v
}

// Use the actual initial TLS connection on each side. Codes are keyed with its
// exporter and bind the role-ordered installation identities, nonces and ID.
func pairingCode(c *tls.ConnectionState, aID, aFP, aNonce, bID, bFP, bNonce, id string) (string, error) {
	if c == nil || c.Version != tls.VersionTLS13 || !c.HandshakeComplete {
		return "", errors.New("pairing requires a completed TLS 1.3 connection")
	}
	key, err := c.ExportKeyingMaterial("EXPORTER-AirLink-Pairing-v2", []byte("AirLink desktop pairing version 2"), 32)
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal([]string{"AirLink pairing v2", aID, aFP, aNonce, bID, bFP, bNonce, id})
	h := hmac.New(sha256.New, key)
	_, _ = h.Write(body)
	return fmt.Sprintf("%08d", binary.BigEndian.Uint64(h.Sum(nil)[:8])%100000000), nil
}

// Commit a pairing transition and its device credentials together. Failed
// persistence restores memory too, so no request can observe an unsaved grant.
func (s *Server) editPairings(fn func(*persisted) error) error {
	s.store.Lock()
	defer s.store.Unlock()
	old := s.store.data
	devices := map[string]deviceRecord{}
	for k, v := range old.Devices {
		devices[k] = v
	}
	pairs := map[string]pairingRecord{}
	for k, v := range old.Pairings {
		pairs[k] = v
	}
	s.store.data.Devices, s.store.data.Pairings = devices, pairs
	if err := fn(&s.store.data); err != nil {
		s.store.data = old
		return err
	}
	if reflect.DeepEqual(old, s.store.data) {
		return nil
	}
	if err := s.store.save(); err != nil {
		s.store.data = old
		return err
	}
	return nil
}

func trimPairings(p *persisted) {
	for id, r := range p.Pairings {
		if time.Since(r.Expires) > 5*time.Minute && (r.State == "paired" || r.State == "cancelled" || r.State == "denied" || !r.Granted) {
			delete(p.Pairings, id)
		}
	}
}

func (s *Server) pairingControl(method string, params json.RawMessage) (any, error) {
	if method == "pairing.start" {
		var in struct {
			URL         string `json:"url"`
			Fingerprint string `json:"fingerprint"`
		}
		if json.Unmarshal(params, &in) != nil || !validPeerURL(in.URL) {
			return nil, engine.ErrInvalid
		}
		in.URL = strings.TrimRight(in.URL, "/")
		in.Fingerprint = strings.ToLower(strings.TrimSpace(in.Fingerprint))
		if !pairingHex(in.Fingerprint, 32) {
			return nil, engine.ErrInvalid
		}
		id := randomID()
		err := s.editPairings(func(p *persisted) error {
			trimPairings(p)
			for _, r := range p.Pairings {
				if r.Peer.Fingerprint == in.Fingerprint && r.Peer.Address == in.URL && (r.State == "starting" || r.State == "comparing" || r.State == "finishing") && time.Now().Before(r.Expires) {
					id = r.ID
					return nil
				}
			}
			if len(p.Pairings) >= 64 {
				return errors.New("pairing queue is full")
			}
			for _, r := range p.Devices {
				if r.Device.Fingerprint == in.Fingerprint {
					return errors.New("device is already trusted")
				}
			}
			p.Pairings[id] = pairingRecord{ID: id, Initiator: true, Peer: model.Device{Kind: "engine", Address: in.URL, Fingerprint: in.Fingerprint, Name: ""}, Nonce: randomID(), Secret: randomID(), ReturnToken: randomID(), Expires: time.Now().Add(5 * time.Minute), State: "starting"}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return s.store.snapshot().Pairings[id].snapshot(), nil
	}
	var in struct {
		ID        string `json:"id"`
		Automatic bool   `json:"automaticReceive"`
	}
	if json.Unmarshal(params, &in) != nil {
		return nil, engine.ErrInvalid
	}
	if method == "pairing.status" {
		r, ok := s.store.snapshot().Pairings[in.ID]
		if !ok {
			return nil, engine.ErrNotFound
		}
		return r.snapshot(), nil
	}
	err := s.editPairings(func(p *persisted) error {
		r, ok := p.Pairings[in.ID]
		if !ok {
			return engine.ErrNotFound
		}
		if method == "pairing.cancel" {
			if r.LocalConfirmed || r.Granted {
				return errors.New("pairing is being completed; revoke the trusted device after completion")
			}
			r.State = "cancelled"
		} else if method == "pairing.confirm" {
			if r.snapshot().State != "comparing" || r.Code == "" {
				return errors.New("pairing is not ready for comparison or has expired")
			}
			r.LocalConfirmed, r.Automatic = true, in.Automatic
		} else {
			return errors.New("unknown pairing method")
		}
		p.Pairings[in.ID] = r
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.store.snapshot().Pairings[in.ID].snapshot(), nil
}

type pairingBeginInput struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Address           string `json:"address"`
	Fingerprint       string `json:"fingerprint"`
	TargetID          string `json:"targetId"`
	TargetFingerprint string `json:"targetFingerprint"`
	AttemptID         string `json:"attemptId"`
	Nonce             string `json:"nonce"`
	Secret            string `json:"secret"`
}
type pairingBeginReply struct {
	ID    string `json:"id"`
	Nonce string `json:"nonce"`
}
type pairingRemoteInput struct {
	ID          string `json:"id"`
	Secret      string `json:"secret"`
	ReturnToken string `json:"returnToken,omitempty"`
}
type pairingRemoteReply struct {
	State     string `json:"state"`
	Confirmed bool   `json:"confirmed"`
	Token     string `json:"token,omitempty"`
}

func (s *Server) pairingBegin(w http.ResponseWriter, r *http.Request) {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	now := time.Now()
	s.mu.Lock()
	var recent []time.Time
	for _, v := range s.rate[ip] {
		if now.Sub(v) < time.Minute {
			recent = append(recent, v)
		}
	}
	if len(recent) >= 10 {
		s.mu.Unlock()
		fail(w, 429, errors.New("too many pairing attempts; wait one minute"))
		return
	}
	s.rate[ip] = append(recent, now)
	s.mu.Unlock()
	var in pairingBeginInput
	if !decode(w, r, &in) {
		return
	}
	if !pairingHex(in.ID, 24) || !validDiscoveryName(in.Name) || !validPeerURL(in.Address) || !pairingHex(in.Fingerprint, 32) || !pairingHex(in.Nonce, 24) || !pairingHex(in.Secret, 24) || !pairingHex(in.AttemptID, 24) {
		fail(w, 400, engine.ErrInvalid)
		return
	}
	var reply pairingBeginReply
	err := s.editPairings(func(p *persisted) error {
		if in.TargetID != p.ID || in.TargetFingerprint != s.fingerprint || in.ID == p.ID {
			return errors.New("pairing target identity changed")
		}
		if _, ok := p.Devices[in.ID]; ok {
			return errors.New("device is already trusted")
		}
		trimPairings(p)
		for id, old := range p.Pairings {
			if !old.Initiator && old.Peer.ID == in.ID && old.State == "comparing" && now.Before(old.Expires) {
				if old.RemoteID != in.AttemptID || old.SecretHash != digest([]byte(in.Secret)) {
					return errors.New("this device already has an active pairing attempt")
				}
				// A lost initial reply requires a new TLS comparison. Never carry
				// confirmation from the old TLS connection onto a replacement.
				delete(p.Pairings, id)
			}
		}
		if len(p.Pairings) >= 64 {
			return errors.New("pairing queue is full")
		}
		reply = pairingBeginReply{randomID(), randomID()}
		code, err := pairingCode(r.TLS, in.ID, in.Fingerprint, in.Nonce, p.ID, s.fingerprint, reply.Nonce, reply.ID)
		if err != nil {
			return err
		}
		p.Pairings[reply.ID] = pairingRecord{ID: reply.ID, Peer: model.Device{ID: in.ID, Name: in.Name, Kind: "engine", Address: in.Address, Fingerprint: in.Fingerprint, CanSend: true, CanReceive: true}, RemoteID: in.AttemptID, Nonce: reply.Nonce, SecretHash: digest([]byte(in.Secret)), Code: code, Expires: now.Add(5 * time.Minute), State: "comparing"}
		return nil
	})
	if err != nil {
		fail(w, 409, err)
		return
	}
	respond(w, 202, reply)
}

func (s *Server) pairingRemote(w http.ResponseWriter, r *http.Request) {
	var in pairingRemoteInput
	if !decode(w, r, &in) {
		return
	}
	action := strings.TrimPrefix(r.URL.Path, "/api/v1/pair/v2/")
	var reply pairingRemoteReply
	err := s.editPairings(func(p *persisted) error {
		pair, ok := p.Pairings[in.ID]
		if !ok || pair.Initiator || !pairingHex(in.Secret, 24) || !hmac.Equal([]byte(pair.SecretHash), []byte(digest([]byte(in.Secret)))) {
			return engine.ErrNotFound
		}
		if !pair.Granted && time.Now().After(pair.Expires) {
			return engine.ErrNotFound
		}
		if pair.State == "cancelled" || pair.State == "denied" {
			reply.State = pair.State
			return nil
		}
		switch action {
		case "cancel":
			if pair.Granted {
				return engine.ErrConflict
			}
			pair.State = "cancelled"
		case "confirm":
			if !pairingHex(in.ReturnToken, 24) {
				return engine.ErrInvalid
			}
			if pair.ReturnToken != "" && pair.ReturnToken != in.ReturnToken {
				return engine.ErrConflict
			}
			pair.RemoteConfirmed, pair.ReturnToken = true, in.ReturnToken
		case "poll":
		case "ack":
			if !pair.Granted {
				return engine.ErrConflict
			}
			if _, exists := p.Devices[pair.Peer.ID]; !exists {
				return engine.ErrNotFound
			}
			pair.State = "paired"
		default:
			return engine.ErrInvalid
		}
		if pair.LocalConfirmed && pair.RemoteConfirmed && !pair.Granted && pair.State != "cancelled" {
			if _, exists := p.Devices[pair.Peer.ID]; exists {
				return engine.ErrConflict
			}
			pair.Granted, pair.State, pair.GrantToken = true, "finishing", randomID()
			d := pair.Peer
			d.Paired = true
			d.AutoAccept = pair.Automatic
			p.Devices[d.ID] = deviceRecord{Device: d, TokenHash: digest([]byte(pair.GrantToken)), OutboundToken: pair.ReturnToken}
		}
		if pair.Granted {
			rec, exists := p.Devices[pair.Peer.ID]
			if !exists || rec.TokenHash != digest([]byte(pair.GrantToken)) {
				return engine.ErrNotFound
			}
			reply.Token = pair.GrantToken
		}
		p.Pairings[in.ID] = pair
		reply.State, reply.Confirmed = pair.State, pair.LocalConfirmed
		return nil
	})
	if err != nil {
		fail(w, 409, errors.New("pairing request unavailable or no longer valid"))
		return
	}
	respond(w, 200, reply)
}

func (s *Server) pairingLoop() {
	defer s.wg.Done()
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-tick.C:
		}
		for id, p := range s.store.snapshot().Pairings {
			if !p.Initiator || p.State != "starting" && p.State != "comparing" && p.State != "finishing" && p.State != "cancelled" || p.State == "cancelled" && p.RemoteID == "" || !p.Granted && time.Now().After(p.Expires) {
				continue
			}
			s.mu.Lock()
			if s.pairingWorkers[id] {
				s.mu.Unlock()
				continue
			}
			select {
			case s.pairingSlots <- struct{}{}:
			default:
				s.mu.Unlock()
				continue
			}
			s.pairingWorkers[id] = true
			s.wg.Add(1)
			s.mu.Unlock()
			go func(id string, p pairingRecord) {
				defer s.wg.Done()
				defer func() { s.mu.Lock(); delete(s.pairingWorkers, id); s.mu.Unlock(); <-s.pairingSlots }()
				s.pairingStep(id, p)
			}(id, p)
		}
	}
}

func (s *Server) pairingStep(id string, p pairingRecord) {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	client := pinnedClient(p.Peer.Fingerprint)
	defer client.CloseIdleConnections()
	base := strings.TrimRight(p.Peer.Address, "/") + "/api/v1/"
	local := s.store.snapshot()
	if p.State == "starting" {
		var info struct {
			ID, Name     string
			Capabilities []string
		}
		if err := requestJSON(ctx, client, "GET", base+"info", "", nil, &info); err != nil {
			return
		}
		capable := false
		for _, v := range info.Capabilities {
			capable = capable || v == "pairing-v2"
		}
		if !capable || !pairingHex(info.ID, 24) || info.ID == local.ID {
			_ = s.editPairings(func(data *persisted) error {
				current := data.Pairings[id]
				if current.State == "starting" {
					current.State = "denied"
					current.Error = "This device needs the previous fingerprint-verification flow or an AirLink update."
					data.Pairings[id] = current
				}
				return nil
			})
			return
		}
		input := pairingBeginInput{ID: local.ID, Name: local.Settings.Name, Address: s.addressForPeer(p.Peer.Address), Fingerprint: s.fingerprint, TargetID: info.ID, TargetFingerprint: p.Peer.Fingerprint, AttemptID: p.ID, Nonce: p.Nonce, Secret: p.Secret}
		body, _ := json.Marshal(input)
		req, err := http.NewRequestWithContext(ctx, "POST", base+"pair/v2/start", bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			return
		}
		content, readErr := io.ReadAll(io.LimitReader(res.Body, 16385))
		res.Body.Close()
		var reply pairingBeginReply
		if readErr != nil || res.StatusCode != 202 || len(content) > 16384 || json.Unmarshal(content, &reply) != nil || !pairingHex(reply.ID, 24) || !pairingHex(reply.Nonce, 24) {
			return
		}
		code, err := pairingCode(res.TLS, local.ID, s.fingerprint, p.Nonce, info.ID, p.Peer.Fingerprint, reply.Nonce, reply.ID)
		if err != nil {
			return
		}
		_ = s.editPairings(func(data *persisted) error {
			current, ok := data.Pairings[id]
			if !ok || current.State != "starting" {
				return engine.ErrConflict
			}
			current.Peer.ID, current.Peer.Name, current.Peer.CanSend, current.Peer.CanReceive = info.ID, info.Name, true, true
			current.RemoteID, current.Code, current.State = reply.ID, code, "comparing"
			data.Pairings[id] = current
			return nil
		})
		return
	}
	if p.RemoteID == "" {
		return
	}
	action := "poll"
	if p.State == "cancelled" {
		action = "cancel"
	} else if p.Granted {
		action = "ack"
	} else if p.LocalConfirmed {
		action = "confirm"
	}
	var reply pairingRemoteReply
	if err := requestJSON(ctx, client, "POST", base+"pair/v2/"+action, "", pairingRemoteInput{ID: p.RemoteID, Secret: p.Secret, ReturnToken: p.ReturnToken}, &reply); err != nil {
		return
	}
	_ = s.editPairings(func(data *persisted) error {
		current, ok := data.Pairings[id]
		if !ok || current.State != p.State {
			return engine.ErrConflict
		}
		current.RemoteConfirmed = reply.Confirmed
		if reply.State == "denied" || reply.State == "cancelled" {
			current.State = reply.State
		}
		if current.LocalConfirmed && reply.Confirmed && pairingHex(reply.Token, 24) && !current.Granted {
			if _, exists := data.Devices[current.Peer.ID]; exists {
				return engine.ErrConflict
			}
			d := current.Peer
			d.Paired = true
			d.AutoAccept = current.Automatic
			data.Devices[d.ID] = deviceRecord{Device: d, TokenHash: digest([]byte(current.ReturnToken)), OutboundToken: reply.Token}
			current.Granted, current.State = true, "finishing"
		}
		if p.Granted && reply.State == "paired" {
			current.State = "paired"
		}
		if action == "cancel" {
			current.RemoteID = ""
		}
		data.Pairings[id] = current
		return nil
	})
}
