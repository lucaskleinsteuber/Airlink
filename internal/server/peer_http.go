package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

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
	requestPath := raw
	if u, err := url.Parse(raw); err == nil {
		requestPath = u.Path
	}
	if !strings.HasSuffix(requestPath, "/complete") && !strings.HasSuffix(requestPath, "/prepare") {
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
