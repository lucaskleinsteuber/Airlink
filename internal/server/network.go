package server

import (
	"context"
	"net"
	"net/url"
	"os"
	"time"
)

func (s *Server) currentURLs(original []string) []string {
	if len(original) == 0 {
		return []string{}
	}
	last, _ := url.Parse(original[len(original)-1])
	out := []string{}
	for _, ip := range localIPs() {
		if !ip.IsLoopback() {
			out = append(out, last.Scheme+"://"+net.JoinHostPort(ip.String(), last.Port())+last.Path)
		}
	}
	return append(out, last.String())
}
func (s *Server) addressForPeer(raw string) string {
	u, e := url.Parse(raw)
	if e != nil {
		return s.currentURLs(s.urls)[0]
	}
	remote := u.Host
	if u.Port() == "" {
		remote = net.JoinHostPort(u.Hostname(), "443")
	}
	c, e := net.DialTimeout("udp", remote, 3*time.Second)
	if e != nil {
		return s.currentURLs(s.urls)[0]
	}
	defer c.Close()
	ip := c.LocalAddr().(*net.UDPAddr).IP
	if ip.IsLoopback() {
		return s.urls[len(s.urls)-1]
	}
	own, _ := url.Parse(s.urls[len(s.urls)-1])
	return "https://" + net.JoinHostPort(ip.String(), own.Port())
}

type cancelReader struct {
	context.Context
	*os.File
}

func (r *cancelReader) Read(b []byte) (int, error) {
	if e := r.Context.Err(); e != nil {
		return 0, e
	}
	return r.File.Read(b)
}
func (s *Server) cancelDeviceRequests(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cancel := range s.active[id] {
		cancel()
	}
}

func (s *Server) stopSender(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel := s.senderCancels[id]; cancel != nil {
		cancel()
	}
}
