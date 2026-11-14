package server

import (
	"net"
)

// localOnlyListener drops connections that do not come from the local
// network before any TLS processing.
type localOnlyListener struct {
	net.Listener
	allowed func(net.IP) bool
}

func (l localOnlyListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if addr, ok := conn.RemoteAddr().(*net.TCPAddr); ok && l.allowed(addr.IP) {
			return conn, nil
		}
		_ = conn.Close()
	}
}

// localNetworkAddress allows loopback, private (RFC 1918, IPv6 ULA) and
// link-local addresses, and global addresses inside the subnet of one of this
// computer's interfaces (for example IPv6 addresses assigned by the router).
func localNetworkAddress(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return true
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.Contains(ip) {
			ones, bits := n.Mask.Size()
			// A /0 or host route would accept the whole internet.
			if ones > 0 && ones < bits {
				return true
			}
		}
	}
	return false
}
