package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lucasfd3/Airlink/internal/model"
)

type pairInput struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	ID          string `json:"id,omitempty"`
	Address     string `json:"address,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

func (s *Server) pairRequest(w http.ResponseWriter, r *http.Request) {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	now := time.Now()
	s.mu.Lock()
	recent := []time.Time{}
	for _, t := range s.rate[ip] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= 10 {
		s.mu.Unlock()
		fail(w, 429, errors.New("too many pairing requests; wait one minute"))
		return
	}
	s.rate[ip] = append(recent, now)
	for id, p := range s.pending {
		if now.After(p.Expires) {
			delete(s.pending, id)
		}
	}
	if len(s.pending) >= 64 {
		s.mu.Unlock()
		fail(w, 429, errors.New("pairing queue full"))
		return
	}
	s.mu.Unlock()
	var v pairInput
	if !decode(w, r, &v) {
		return
	}
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" || len(v.Name) > 120 || (v.Kind != "browser" && v.Kind != "engine") {
		fail(w, 400, errors.New("invalid device name or kind"))
		return
	}
	if v.Kind == "engine" {
		if !validPeerURL(v.Address) || len(v.Fingerprint) != 64 || len(v.ID) != 48 {
			fail(w, 400, errors.New("invalid engine identity"))
			return
		}
	} else {
		v.ID = randomID()
		v.Address = ""
		v.Fingerprint = ""
	}
	if v.ID == s.store.snapshot().ID {
		fail(w, 400, errors.New("cannot pair this device with itself"))
		return
	}
	if _, exists := s.store.snapshot().Devices[v.ID]; exists {
		fail(w, 409, errors.New("device already paired; revoke its old pairing before reconnecting"))
		return
	}
	id, poll := randomID(), randomID()
	d := model.Device{ID: v.ID, Name: v.Name, Kind: v.Kind, Address: v.Address, Fingerprint: v.Fingerprint, CanSend: true, CanReceive: true, Paired: true, Online: true, LastSeen: now.UTC().Format(time.RFC3339)}
	p := &pendingPair{PairRequest: model.PairRequest{ID: id, Name: v.Name, Kind: v.Kind, CreatedAt: now.UTC().Format(time.RFC3339)}, PollHash: digest([]byte(poll)), State: "pending", Device: d, Expires: now.Add(5 * time.Minute)}
	s.mu.Lock()
	s.pending[id] = p
	s.mu.Unlock()
	respond(w, 202, map[string]string{"requestId": id, "pollToken": poll})
}
func (s *Server) pairPoll(w http.ResponseWriter, r *http.Request) {
	var v struct {
		ID    string `json:"requestId"`
		Token string `json:"pollToken"`
	}
	if !decode(w, r, &v) {
		return
	}
	s.mu.Lock()
	p, ok := s.pending[v.ID]
	if !ok || time.Now().After(p.Expires) || p.PollHash != digest([]byte(v.Token)) {
		s.mu.Unlock()
		fail(w, 404, errors.New("pairing request expired"))
		return
	}
	state, token, id, kind := p.State, p.Token, p.Device.ID, p.Device.Kind
	s.mu.Unlock()
	out := map[string]string{"state": state}
	if state == "approved" {
		out["deviceId"] = id
		if kind == "browser" {
			http.SetCookie(w, &http.Cookie{Name: "airlink_session", Value: token, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 30 * 24 * 3600})
		} else {
			out["token"] = token
		}
	}
	respond(w, 200, out)
}
func (s *Server) approvePair(id string, approve bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pending[id]
	if !ok || time.Now().After(p.Expires) {
		return errors.New("pairing request expired")
	}
	if p.State != "pending" {
		return errors.New("pairing already handled")
	}
	if !approve {
		p.State = "denied"
		return nil
	}
	if _, exists := s.store.snapshot().Devices[p.Device.ID]; exists {
		return errors.New("device is already paired; revoke it before pairing again")
	}
	token := randomID()
	record := deviceRecord{Device: p.Device, TokenHash: digest([]byte(token))}
	if err := s.store.put(record); err != nil {
		return err
	}
	p.Token = token
	p.State = "approved"
	return nil
}
func validPeerURL(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return false
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast()
	}
	return strings.HasSuffix(strings.ToLower(host), ".local") || host == "localhost"
}

type peerHTTPError struct {
	Code    int
	Message string
}

func (e *peerHTTPError) Error() string { return fmt.Sprintf("peer HTTP %d: %s", e.Code, e.Message) }

func requestJSON(ctx context.Context, client *http.Client, method, raw, token string, value any, out any) error {
	if !strings.HasSuffix(raw, "/complete") && !strings.HasSuffix(raw, "/prepare") {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	var body io.Reader
	if value != nil {
		b, e := json.Marshal(value)
		if e != nil {
			return e
		}
		body = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(ctx, method, raw, body)
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, e := client.Do(req)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return &peerHTTPError{Code: res.StatusCode, Message: string(b)}
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(out)
	}
	return nil
}
func (s *Server) connectPeer(raw, fp string) (string, error) {
	raw = strings.TrimRight(raw, "/")
	fp = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(fp), ":", ""))
	if !validPeerURL(raw) || len(fp) != 64 {
		return "", errors.New("enter a local HTTPS address and its 64-character identity fingerprint")
	}
	client := pinnedClient(fp)
	var info struct{ ID, Name, Fingerprint string }
	ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
	defer cancel()
	if err := requestJSON(ctx, client, "GET", raw+"/api/v1/info", "", nil, &info); err != nil {
		return "", err
	}
	if info.ID == s.store.snapshot().ID {
		return "", errors.New("cannot pair this device with itself")
	}
	p := s.store.snapshot()
	var response struct {
		ID    string `json:"requestId"`
		Token string `json:"pollToken"`
	}
	if _, exists := p.Devices[info.ID]; exists {
		return "", errors.New("device already paired; revoke it before pairing again")
	}
	input := pairInput{Name: p.Settings.Name, Kind: "engine", ID: p.ID, Address: s.addressForPeer(raw), Fingerprint: s.fingerprint}
	if err := requestJSON(ctx, client, "POST", raw+"/api/v1/pair/request", "", input, &response); err != nil {
		return "", err
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		deadline := time.NewTimer(5 * time.Minute)
		defer deadline.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-deadline.C:
				return
			case <-tick.C:
			}
			var result struct{ State, Token string }
			if e := requestJSON(s.ctx, client, "POST", raw+"/api/v1/pair/poll", "", map[string]string{"requestId": response.ID, "pollToken": response.Token}, &result); e != nil {
				continue
			}
			if result.State == "denied" {
				return
			}
			if result.State != "approved" {
				continue
			}
			incoming := randomID()
			d := model.Device{ID: info.ID, Name: info.Name, Kind: "engine", Address: raw, Fingerprint: fp, Paired: true, Online: true, CanSend: true, CanReceive: true, LastSeen: time.Now().UTC().Format(time.RFC3339)}
			record := deviceRecord{Device: d, TokenHash: digest([]byte(incoming)), OutboundToken: result.Token, PendingReturnToken: incoming}
			if err := s.store.put(record); err != nil {
				return
			}
			return
		}
	}()
	return response.ID, nil
}
func (s *Server) peerConfirm(w http.ResponseWriter, r *http.Request, d model.Device) {
	if d.Kind != "engine" {
		fail(w, 403, errors.New("engine pairing required"))
		return
	}
	var v struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &v) {
		return
	}
	if len(v.Token) != 48 {
		fail(w, 400, errors.New("invalid peer credential"))
		return
	}
	if err := s.store.edit(d.ID, func(record *deviceRecord) { record.OutboundToken = v.Token }); err != nil {
		fail(w, 500, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
