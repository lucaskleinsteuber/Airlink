package server

import (
	"errors"
	"net/http"
)

// errPeerIdentity marks a failure that reconnecting cannot repair without
// inspecting the peer's identity or certificate. Preserve the underlying TLS
// verification error as well so callers can inspect its concrete cause.
var errPeerIdentity = errors.New("peer TLS identity verification failed")

// permanentPeerError distinguishes peer rejections that require attention
// from unavailable networks, throttling, pending decisions and server faults.
func permanentPeerError(err error) bool {
	if errors.Is(err, errPeerIdentity) {
		return true
	}
	var status *peerHTTPError
	if !errors.As(err, &status) {
		return false
	}
	switch status.Code {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return true
	default:
		return false
	}
}
