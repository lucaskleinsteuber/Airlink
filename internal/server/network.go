package server

import (
	"context"
	"net"
	"net/http"
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

type sourceReader struct {
	context.Context
	*os.File
	server             *Server
	transferID, fileID string
}

func (r *sourceReader) Read(p []byte) (int, error) {
	if err := r.Context.Err(); err != nil {
		return 0, err
	}
	if err := r.server.engine.ValidateSource(r.transferID, r.fileID, r.File); err != nil {
		return 0, err
	}
	n, err := r.File.Read(p)
	if check := r.server.engine.ValidateSource(r.transferID, r.fileID, r.File); check != nil {
		return 0, check
	}
	return n, err
}

type sourcePolicyWriter struct {
	http.ResponseWriter
	ctx      context.Context
	validate func() error
}

func (w *sourcePolicyWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *sourcePolicyWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if w.validate != nil {
		if err := w.validate(); err != nil {
			return 0, err
		}
	}
	return w.ResponseWriter.Write(p)
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
