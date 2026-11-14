// Package peer implements AirLink protocol 3: one long-lived, mutually
// authenticated TLS 1.3 connection per pair of paired devices. Control
// messages (JSON) and file data share the connection as length-prefixed
// frames; several transfers interleave fairly on separate streams.
package peer

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// ALPN identifies protocol 3 on the shared TLS port. HTTP/1.1 on the same port
// serves only device information and the comparison-code pairing.
const ALPN = "airlink/3"

// ProtocolVersion is announced in the hello message and in discovery.
const ProtocolVersion = 3

const (
	frameControl byte = 1
	frameData    byte = 2

	headerSize = 12
	// MaxControlPayload bounds one JSON control message.
	MaxControlPayload = 1 << 20
	// MaxDataPayload bounds one data frame. Senders use at most this size.
	MaxDataPayload = 1 << 20
)

var errFrame = errors.New("invalid protocol frame")

type header struct {
	kind   byte
	stream uint32
	length uint32
}

// Wire layout: kind(1) reserved(3, zero) stream(4) length(4), big endian.
func putHeader(b []byte, kind byte, stream uint32, length int) {
	b[0] = kind
	b[1], b[2], b[3] = 0, 0, 0
	binary.BigEndian.PutUint32(b[4:8], stream)
	binary.BigEndian.PutUint32(b[8:12], uint32(length))
}

func readHeader(r io.Reader, b []byte) (header, error) {
	if _, err := io.ReadFull(r, b[:headerSize]); err != nil {
		return header{}, err
	}
	return parseHeader(b[:headerSize])
}

func parseHeader(b []byte) (header, error) {
	if len(b) < headerSize {
		return header{}, errFrame
	}
	h := header{kind: b[0], stream: binary.BigEndian.Uint32(b[4:8]), length: binary.BigEndian.Uint32(b[8:12])}
	if b[1] != 0 || b[2] != 0 || b[3] != 0 {
		return header{}, fmt.Errorf("%w: reserved bits set", errFrame)
	}
	switch h.kind {
	case frameControl:
		if h.length == 0 || h.length > MaxControlPayload {
			return header{}, fmt.Errorf("%w: control length %d", errFrame, h.length)
		}
	case frameData:
		if h.stream == 0 || h.length == 0 || h.length > MaxDataPayload {
			return header{}, fmt.Errorf("%w: data frame stream %d length %d", errFrame, h.stream, h.length)
		}
	default:
		return header{}, fmt.Errorf("%w: kind %d", errFrame, h.kind)
	}
	return h, nil
}
