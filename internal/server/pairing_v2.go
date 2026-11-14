package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
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
	"strconv"
	"strings"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
	"github.com/lucaskleinsteuber/Airlink/internal/peer"
	"github.com/lucaskleinsteuber/Airlink/internal/transfer"
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
	Problem         string       `json:"problem,omitempty"`
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
	return model.PairingSession{ID: p.ID, PeerID: p.Peer.ID, PeerName: p.Peer.Name, State: state, ComparisonCode: code, ExpiresAt: p.Expires.UTC().Format(time.RFC3339), LocalConfirmed: p.LocalConfirmed, RemoteConfirmed: p.RemoteConfirmed, AutomaticReceive: p.Automatic, Error: p.Error,
		Problem: p.Problem, Initiator: p.Initiator, Platform: p.Peer.Platform, Form: p.Peer.Form}
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
	s.changed()
	return nil
}

func trimPairings(p *persisted) {
	for id, r := range p.Pairings {
		if time.Since(r.Expires) > 5*time.Minute && (r.State == "paired" || r.State == "cancelled" || r.State == "denied" || !r.Granted) {
			delete(p.Pairings, id)
		}
	}
}

// Requests from other devices are accepted only while AirLink is open on this
// computer: the apps renew this window while their main window is visible
// and close it when it is hidden.
const pairingWindow = 2 * time.Minute

const pairingClosedHint = "Open AirLink on the other computer to continue."

// Stable pairing problems; the apps explain them in the user's language.
const (
	problemClosed      = "closed"
	problemUnreachable = "unreachable"
	problemOutdated    = "outdated"
	problemPaired      = "paired"
	problemIdentity    = "identity"
	problemSelf        = "self"
)

// peerURLFromAddress turns what a person types (an IP address, address:port,
// a host name or a full https URL) into the device URL used for pairing.
func peerURLFromAddress(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" || strings.ContainsAny(v, " /?#@") && !strings.HasPrefix(strings.ToLower(v), "https://") {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(v), "https://") {
		return strings.TrimRight(v, "/")
	}
	host, port := v, "43871"
	if ip := net.ParseIP(strings.Trim(v, "[]")); ip != nil {
		host = ip.String()
	} else if h, p, err := net.SplitHostPort(v); err == nil {
		host, port = strings.Trim(h, "[]"), p
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return ""
	}
	if net.ParseIP(host) == nil && !strings.Contains(host, ".") && !strings.EqualFold(host, "localhost") {
		// A bare computer name is reached via multicast DNS.
		host += ".local"
	}
	return "https://" + net.JoinHostPort(host, port)
}

// probeIdentity reads the identity of a device that was entered by address.
// The chain must be valid; the comparison code then proves that both sides
// see the same TLS connection, exactly as for discovered devices.
func probeIdentity(ctx context.Context, base string) (string, error) {
	var seen string
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, InsecureSkipVerify: true, VerifyConnection: func(c tls.ConnectionState) error {
			fp, err := peer.VerifyChain(c.PeerCertificates, x509.ExtKeyUsageServerAuth)
			seen = fp
			return err
		}},
		DialContext:         (&net.Dialer{Timeout: 4 * time.Second}).DialContext,
		TLSHandshakeTimeout: 4 * time.Second,
	}}
	defer client.CloseIdleConnections()
	var info struct {
		Fingerprint string `json:"fingerprint"`
	}
	if err := requestJSON(ctx, client, "GET", base+"info", "", nil, &info); err != nil {
		return "", err
	}
	if seen == "" || info.Fingerprint != seen {
		return "", fmt.Errorf("%w: announced identity differs from the connection", errPeerIdentity)
	}
	return seen, nil
}

// setPairingProblem records why an outgoing request does not progress.
func (s *Server) setPairingProblem(id, problem, message string, deny bool) {
	_ = s.editPairings(func(data *persisted) error {
		current, ok := data.Pairings[id]
		if !ok || current.State != "starting" {
			return nil
		}
		current.Problem, current.Error = problem, message
		if deny {
			current.State = "denied"
		}
		data.Pairings[id] = current
		return nil
	})
}

func (s *Server) delayPairing(id string, d time.Duration) {
	s.mu.Lock()
	if s.pairingNext == nil {
		s.pairingNext = map[string]time.Time{}
	}
	s.pairingNext[id] = time.Now().Add(d)
	s.mu.Unlock()
}

func (s *Server) pairingControl(method string, params json.RawMessage) (any, error) {
	if method == "pairing.start" {
		// A discovered device comes with URL and fingerprint; an address typed
		// by the user has no fingerprint yet: it is read over TLS and then
		// confirmed by comparing the code on both screens.
		var in struct {
			URL         string `json:"url"`
			Address     string `json:"address"`
			Fingerprint string `json:"fingerprint"`
			Name        string `json:"name"`
			Platform    string `json:"platform"`
			Form        string `json:"form"`
		}
		if json.Unmarshal(params, &in) != nil {
			return nil, transfer.ErrInvalid
		}
		if in.URL == "" {
			in.URL = peerURLFromAddress(in.Address)
		}
		if !validPeerURL(in.URL) {
			return nil, transfer.ErrInvalid
		}
		in.URL = strings.TrimRight(in.URL, "/")
		in.Fingerprint = strings.ToLower(strings.TrimSpace(in.Fingerprint))
		if in.Fingerprint != "" && !pairingHex(in.Fingerprint, 32) {
			return nil, transfer.ErrInvalid
		}
		if !validDiscoveryName(in.Name) {
			in.Name = ""
		}
		if !validPlatform(in.Platform) || !validForm(in.Form) {
			in.Platform, in.Form = "", ""
		}
		id := randomID()
		err := s.editPairings(func(p *persisted) error {
			trimPairings(p)
			for _, r := range p.Pairings {
				if r.Initiator && r.Peer.Address == in.URL && (in.Fingerprint == "" || r.Peer.Fingerprint == in.Fingerprint) && (r.State == "starting" || r.State == "comparing" || r.State == "finishing") && time.Now().Before(r.Expires) {
					id = r.ID
					return nil
				}
			}
			if len(p.Pairings) >= 64 {
				return errors.New("pairing queue is full")
			}
			for _, r := range p.Devices {
				if in.Fingerprint != "" && r.Device.Fingerprint == in.Fingerprint {
					return errors.New("device is already trusted")
				}
			}
			p.Pairings[id] = pairingRecord{ID: id, Initiator: true, Peer: model.Device{Kind: "engine", Address: in.URL, Fingerprint: in.Fingerprint, Name: in.Name, Platform: in.Platform, Form: in.Form}, Nonce: randomID(), Secret: randomID(), ReturnToken: randomID(), Expires: time.Now().Add(5 * time.Minute), State: "starting"}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return s.store.snapshot().Pairings[id].snapshot(), nil
	}
	if method == "pairing.listen" {
		// The local user opened "Add device": accept requests from other
		// devices for a limited time only.
		var v struct {
			Seconds int  `json:"seconds"`
			Close   bool `json:"close"`
		}
		_ = json.Unmarshal(params, &v)
		if v.Close {
			s.mu.Lock()
			s.pairingOpenUntil = time.Time{}
			s.mu.Unlock()
			return map[string]string{"openUntil": ""}, nil
		}
		if v.Seconds <= 0 {
			v.Seconds = int(pairingWindow / time.Second)
		}
		until := time.Now().Add(time.Duration(min(max(v.Seconds, 30), 600)) * time.Second)
		s.mu.Lock()
		s.pairingOpenUntil = until
		s.mu.Unlock()
		return map[string]string{"openUntil": until.UTC().Format(time.RFC3339)}, nil
	}
	var in struct {
		ID        string `json:"id"`
		Automatic bool   `json:"automaticReceive"`
	}
	if json.Unmarshal(params, &in) != nil {
		return nil, transfer.ErrInvalid
	}
	if method == "pairing.status" {
		r, ok := s.store.snapshot().Pairings[in.ID]
		if !ok {
			return nil, transfer.ErrNotFound
		}
		return r.snapshot(), nil
	}
	err := s.editPairings(func(p *persisted) error {
		r, ok := p.Pairings[in.ID]
		if !ok {
			return transfer.ErrNotFound
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
	Platform          string `json:"platform,omitempty"`
	Form              string `json:"form,omitempty"`
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
	if !now.Before(s.pairingOpenUntil) {
		// Not counted against the attempt limit: the requesting computer
		// keeps asking until AirLink is opened here, and must then proceed
		// at once instead of waiting for a rate window.
		s.mu.Unlock()
		fail(w, http.StatusForbidden, errors.New("pairing is not open on this device"))
		return
	}
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
	if !pairingHex(in.ID, 24) || !validDiscoveryName(in.Name) || !validPeerURL(in.Address) || !pairingHex(in.Fingerprint, 32) || !pairingHex(in.Nonce, 24) || !pairingHex(in.Secret, 24) || !pairingHex(in.AttemptID, 24) || !validPlatform(in.Platform) || !validForm(in.Form) {
		fail(w, 400, transfer.ErrInvalid)
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
		p.Pairings[reply.ID] = pairingRecord{ID: reply.ID, Peer: model.Device{ID: in.ID, Name: in.Name, Kind: "engine", Address: in.Address, Fingerprint: in.Fingerprint, CanSend: true, CanReceive: true, CanClipboard: true, Platform: in.Platform, Form: in.Form}, RemoteID: in.AttemptID, Nonce: reply.Nonce, SecretHash: digest([]byte(in.Secret)), Code: code, Expires: now.Add(5 * time.Minute), State: "comparing"}
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
	granted := ""
	err := s.editPairings(func(p *persisted) error {
		pair, ok := p.Pairings[in.ID]
		if !ok || pair.Initiator || !pairingHex(in.Secret, 24) || !hmac.Equal([]byte(pair.SecretHash), []byte(digest([]byte(in.Secret)))) {
			return transfer.ErrNotFound
		}
		if !pair.Granted && time.Now().After(pair.Expires) {
			return transfer.ErrNotFound
		}
		if pair.State == "cancelled" || pair.State == "denied" {
			reply.State = pair.State
			return nil
		}
		switch action {
		case "cancel":
			if pair.Granted {
				return transfer.ErrConflict
			}
			pair.State = "cancelled"
		case "confirm":
			if !pairingHex(in.ReturnToken, 24) {
				return transfer.ErrInvalid
			}
			if pair.ReturnToken != "" && pair.ReturnToken != in.ReturnToken {
				return transfer.ErrConflict
			}
			pair.RemoteConfirmed, pair.ReturnToken = true, in.ReturnToken
		case "poll":
		case "ack":
			if !pair.Granted {
				return transfer.ErrConflict
			}
			if _, exists := p.Devices[pair.Peer.ID]; !exists {
				return transfer.ErrNotFound
			}
			pair.State = "paired"
		default:
			return transfer.ErrInvalid
		}
		if pair.LocalConfirmed && pair.RemoteConfirmed && !pair.Granted && pair.State != "cancelled" {
			if _, exists := p.Devices[pair.Peer.ID]; exists {
				return transfer.ErrConflict
			}
			pair.Granted, pair.State, pair.GrantToken = true, "finishing", randomID()
			d := pair.Peer
			d.Paired = true
			d.AutoAccept = pair.Automatic
			p.Devices[d.ID] = deviceRecord{Device: d, TokenHash: digest([]byte(pair.GrantToken)), OutboundToken: pair.ReturnToken}
			granted = d.ID
		}
		if pair.Granted {
			rec, exists := p.Devices[pair.Peer.ID]
			if !exists || rec.TokenHash != digest([]byte(pair.GrantToken)) {
				return transfer.ErrNotFound
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
	if granted != "" {
		s.requestConnection(granted)
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
			if s.pairingWorkers[id] || time.Now().Before(s.pairingNext[id]) {
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
	base := strings.TrimRight(p.Peer.Address, "/") + "/api/v1/"
	local := s.store.snapshot()
	if p.State == "starting" && p.Peer.Fingerprint == "" {
		fp, err := probeIdentity(ctx, base)
		if err != nil {
			s.setPairingProblem(id, problemUnreachable, "The other computer cannot be reached at this address.", false)
			s.delayPairing(id, 2*time.Second)
			return
		}
		_ = s.editPairings(func(data *persisted) error {
			current, ok := data.Pairings[id]
			if !ok || current.State != "starting" || current.Peer.Fingerprint != "" {
				return nil
			}
			current.Peer.Fingerprint, current.Problem, current.Error = fp, "", ""
			if fp == s.fingerprint {
				current.State, current.Problem, current.Error = "denied", problemSelf, "This is the address of this computer."
			}
			for _, r := range data.Devices {
				if r.Device.Fingerprint == fp {
					current.State, current.Problem, current.Error = "denied", problemPaired, "This computer is already connected."
				}
			}
			data.Pairings[id] = current
			return nil
		})
		return
	}
	client := pinnedClient(p.Peer.Fingerprint)
	defer client.CloseIdleConnections()
	if p.State == "starting" {
		var info struct {
			ID, Name     string
			Capabilities []string
			Platform     string
			Form         string
		}
		if err := requestJSON(ctx, client, "GET", base+"info", "", nil, &info); err != nil {
			if errors.Is(err, errPeerIdentity) {
				s.setPairingProblem(id, problemIdentity, "The other computer presented a different identity. Choose it again from the list.", true)
				return
			}
			s.setPairingProblem(id, problemUnreachable, "The other computer cannot be reached. A firewall there may block AirLink.", false)
			s.delayPairing(id, 2*time.Second)
			return
		}
		capable := false
		for _, v := range info.Capabilities {
			capable = capable || v == "protocol-3"
		}
		if !capable || !pairingHex(info.ID, 24) || info.ID == local.ID {
			problem, message := problemOutdated, "The other computer needs AirLink 0.13 or later."
			if info.ID == local.ID {
				problem, message = problemSelf, "This is the address of this computer."
			}
			s.setPairingProblem(id, problem, message, true)
			return
		}
		platform, form := localDevice()
		input := pairingBeginInput{ID: local.ID, Name: local.Settings.Name, Address: s.addressForPeer(p.Peer.Address), Fingerprint: s.fingerprint, TargetID: info.ID, TargetFingerprint: p.Peer.Fingerprint, AttemptID: p.ID, Nonce: p.Nonce, Secret: p.Secret, Platform: platform, Form: form}
		body, _ := json.Marshal(input)
		req, err := http.NewRequestWithContext(ctx, "POST", base+"pair/v2/start", bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			s.setPairingProblem(id, problemUnreachable, "The other computer cannot be reached. A firewall there may block AirLink.", false)
			s.delayPairing(id, 2*time.Second)
			return
		}
		content, readErr := io.ReadAll(io.LimitReader(res.Body, 16385))
		res.Body.Close()
		var reply pairingBeginReply
		if res.StatusCode == http.StatusForbidden {
			// Keep asking until AirLink is opened on the other computer.
			if p.Problem != problemClosed {
				s.setPairingProblem(id, problemClosed, pairingClosedHint, false)
			}
			s.delayPairing(id, time.Second)
			return
		}
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
				return transfer.ErrConflict
			}
			current.Peer.ID, current.Peer.Name, current.Peer.CanSend, current.Peer.CanReceive, current.Peer.CanClipboard = info.ID, info.Name, true, true, true
			if validPlatform(info.Platform) && validForm(info.Form) && info.Platform != "" {
				current.Peer.Platform, current.Peer.Form = info.Platform, info.Form
			}
			current.RemoteID, current.Code, current.State, current.Error, current.Problem = reply.ID, code, "comparing", "", ""
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
	granted := ""
	defer func() {
		if granted != "" {
			s.requestConnection(granted)
		}
	}()
	_ = s.editPairings(func(data *persisted) error {
		current, ok := data.Pairings[id]
		if !ok || current.State != p.State {
			return transfer.ErrConflict
		}
		current.RemoteConfirmed = reply.Confirmed
		if reply.State == "denied" || reply.State == "cancelled" {
			current.State = reply.State
		}
		if current.LocalConfirmed && reply.Confirmed && pairingHex(reply.Token, 24) && !current.Granted {
			if _, exists := data.Devices[current.Peer.ID]; exists {
				return transfer.ErrConflict
			}
			d := current.Peer
			d.Paired = true
			d.AutoAccept = current.Automatic
			data.Devices[d.ID] = deviceRecord{Device: d, TokenHash: digest([]byte(current.ReturnToken)), OutboundToken: reply.Token}
			current.Granted, current.State = true, "finishing"
			granted = d.ID
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
