package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
)

func TestPermanentPeerHTTPFailures(t *testing.T) {
	for _, code := range []int{400, 401, 403, 404, 413, 422} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			err := fmt.Errorf("sending file: %w", &url.Error{Op: "POST", URL: "https://peer.local/api/v1/transfers", Err: &peerHTTPError{Code: code, Message: "attention required"}})
			if !permanentPeerError(err) {
				t.Fatalf("peer rejection %d would retry forever: %v", code, err)
			}
		})
	}
	for _, code := range []int{307, 408, 409, 429, 500, 502, 503, 504, 599} {
		t.Run(fmt.Sprintf("retry-%d", code), func(t *testing.T) {
			if permanentPeerError(fmt.Errorf("request: %w", &peerHTTPError{Code: code, Message: "try later"})) {
				t.Fatalf("temporary/pending peer status %d became permanent", code)
			}
		})
	}
}

func TestTemporaryPeerTransportFailures(t *testing.T) {
	for _, err := range []error{
		nil,
		context.Canceled,
		context.DeadlineExceeded,
		&net.DNSError{Err: "no such host", Name: "peer.local", IsNotFound: true},
		&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED},
		&url.Error{Op: "PUT", URL: "https://peer.local", Err: &net.OpError{Op: "read", Net: "tcp", Err: context.DeadlineExceeded}},
		io.ErrUnexpectedEOF,
		errors.New("certificate expired: a message alone is not an identity error"),
	} {
		if permanentPeerError(err) {
			t.Fatalf("transient or untyped error must remain retryable: %v", err)
		}
	}
}

func peerTLSFixture(t *testing.T) (tls.Certificate, string, tls.ConnectionState) {
	t.Helper()
	cert, fingerprint, _, err := identity(t.TempDir(), "Peer failure test")
	if err != nil {
		t.Fatal(err)
	}
	state := tls.ConnectionState{ServerName: "localhost"}
	for _, der := range cert.Certificate {
		parsed, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		state.PeerCertificates = append(state.PeerCertificates, parsed)
	}
	return cert, fingerprint, state
}

func TestPinnedVerificationFailuresRequireAttention(t *testing.T) {
	_, fingerprint, state := peerTLSFixture(t)
	_, otherFingerprint, other := peerTLSFixture(t)
	verify := func(fingerprint string, state tls.ConnectionState) error {
		return pinnedClient(fingerprint).Transport.(*http.Transport).TLSClientConfig.VerifyConnection(state)
	}
	if err := verify(fingerprint, state); err != nil {
		t.Fatal("correct pinned chain rejected", err)
	}
	for name, input := range map[string]struct {
		fingerprint string
		state       tls.ConnectionState
	}{
		"changed identity": {otherFingerprint, state},
		"missing identity": {fingerprint, tls.ConnectionState{ServerName: "localhost", PeerCertificates: state.PeerCertificates[:1]}},
		"invalid hostname": {fingerprint, tls.ConnectionState{ServerName: "not-the-paired-host.invalid", PeerCertificates: state.PeerCertificates}},
		"untrusted chain with matching presented root": {otherFingerprint, tls.ConnectionState{ServerName: "localhost", PeerCertificates: []*x509.Certificate{state.PeerCertificates[0], other.PeerCertificates[1]}}},
	} {
		t.Run(name, func(t *testing.T) {
			err := verify(input.fingerprint, input.state)
			if err == nil || !errors.Is(err, errPeerIdentity) {
				t.Fatalf("TLS verification failure lost identity sentinel: %v", err)
			}
			wrapped := fmt.Errorf("sending offer: %w", &url.Error{Op: "POST", URL: "https://peer.local", Err: err})
			if !permanentPeerError(wrapped) {
				t.Fatal("wrapped identity failure would reconnect forever", wrapped)
			}
			if name == "invalid hostname" {
				var cause x509.HostnameError
				if !errors.As(wrapped, &cause) {
					t.Fatal("underlying hostname verification error was discarded", wrapped)
				}
			}
			if name == "untrusted chain with matching presented root" {
				var cause x509.UnknownAuthorityError
				if !errors.As(wrapped, &cause) {
					t.Fatal("underlying chain verification error was discarded", wrapped)
				}
			}
		})
	}
}

func TestPinnedHTTPSFailureClassifiesBeforeSendingCredentials(t *testing.T) {
	cert, fingerprint, _ := peerTLSFixture(t)
	var requests atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("authorized probe lost its credentials")
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}}
	server.StartTLS()
	defer server.Close()

	wrong := pinnedClient(strings.Repeat("0", 64))
	defer wrong.CloseIdleConnections()
	err := requestJSON(context.Background(), wrong, http.MethodPost, server.URL, "test-token", map[string]string{"file": "private-name"}, nil)
	var requestErr *url.Error
	if err == nil || !errors.As(err, &requestErr) || !errors.Is(err, errPeerIdentity) || !permanentPeerError(err) {
		t.Fatalf("real HTTPS pin failure was not classified through transport wrapping: %v", err)
	}
	if requests.Load() != 0 {
		t.Fatal("an untrusted peer received an HTTP request")
	}
	trusted := pinnedClient(fingerprint)
	defer trusted.CloseIdleConnections()
	var result struct{ OK bool }
	if err := requestJSON(context.Background(), trusted, http.MethodPost, server.URL, "test-token", map[string]string{"file": "private-name"}, &result); err != nil || !result.OK {
		t.Fatal("valid pinned HTTPS request failed", result, err)
	}
	if requests.Load() != 1 {
		t.Fatal("expected exactly one request to the verified peer", requests.Load())
	}
}
