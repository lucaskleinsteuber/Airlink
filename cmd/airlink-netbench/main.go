// Command airlink-netbench measures raw TLS 1.3 throughput between two
// computers. It is a development tool: it establishes the network ceiling that
// AirLink transfers are compared against. It is not shipped in app packages.
//
//	airlink-netbench server -listen :43871
//	airlink-netbench client -connect 192.168.178.22:43871 -seconds 10 -streams 1 -mode upload
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"os"
	"sync"
	"time"
)

const bufferSize = 1 << 20

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: airlink-netbench server|client [flags]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "server":
		flags := flag.NewFlagSet("server", flag.ExitOnError)
		listen := flags.String("listen", ":43871", "TCP listen address")
		once := flags.Int("connections", 0, "exit after this many connections (0 = run until killed)")
		_ = flags.Parse(os.Args[2:])
		if err := serve(*listen, *once); err != nil {
			log.Fatal(err)
		}
	case "client":
		flags := flag.NewFlagSet("client", flag.ExitOnError)
		connect := flags.String("connect", "", "server host:port")
		seconds := flags.Float64("seconds", 10, "measurement duration")
		streams := flags.Int("streams", 1, "parallel TLS connections")
		mode := flags.String("mode", "upload", "upload (client sends) or download (server sends)")
		_ = flags.Parse(os.Args[2:])
		if *connect == "" || *streams < 1 || *seconds <= 0 || (*mode != "upload" && *mode != "download") {
			flags.Usage()
			os.Exit(2)
		}
		result, err := run(*connect, *mode, *streams, time.Duration(*seconds*float64(time.Second)))
		if err != nil {
			log.Fatal(err)
		}
		_ = json.NewEncoder(os.Stdout).Encode(result)
	default:
		fmt.Fprintln(os.Stderr, "usage: airlink-netbench server|client [flags]")
		os.Exit(2)
	}
}

func certificate() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "airlink-netbench"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

// Each connection starts with one mode byte. Upload: the server counts bytes
// until EOF and replies with byte count and nanoseconds measured from the first
// payload byte. Download: the server sends for the requested duration.
func serve(address string, limit int) error {
	cert, err := certificate()
	if err != nil {
		return err
	}
	listener, err := tls.Listen("tcp", address, &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}})
	if err != nil {
		return err
	}
	defer listener.Close()
	log.Printf("listening on %s", listener.Addr())
	var wg sync.WaitGroup
	for accepted := 0; limit == 0 || accepted < limit; accepted++ {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer conn.Close()
			if err := handle(conn); err != nil && !errors.Is(err, io.EOF) {
				log.Print(err)
			}
		}()
	}
	wg.Wait()
	return nil
}

func handle(conn net.Conn) error {
	header := make([]byte, 9)
	if _, err := io.ReadFull(conn, header); err != nil {
		return err
	}
	buffer := make([]byte, bufferSize)
	switch header[0] {
	case 'U':
		var total int64
		var first time.Time
		for {
			n, err := conn.Read(buffer)
			if n > 0 && first.IsZero() {
				first = time.Now()
			}
			total += int64(n)
			if err != nil {
				if !errors.Is(err, io.EOF) {
					return err
				}
				break
			}
		}
		reply := make([]byte, 16)
		binary.BigEndian.PutUint64(reply[:8], uint64(total))
		binary.BigEndian.PutUint64(reply[8:], uint64(time.Since(first).Nanoseconds()))
		_, err := conn.Write(reply)
		return err
	case 'D':
		duration := time.Duration(binary.BigEndian.Uint64(header[1:]))
		_, _ = rand.Read(buffer)
		deadline := time.Now().Add(duration)
		for time.Now().Before(deadline) {
			if _, err := conn.Write(buffer); err != nil {
				return err
			}
		}
		if tlsConn, ok := conn.(*tls.Conn); ok {
			return tlsConn.CloseWrite()
		}
		return nil
	default:
		return fmt.Errorf("unknown mode %q", header[0])
	}
}

type streamResult struct {
	Bytes   int64   `json:"bytes"`
	Seconds float64 `json:"seconds"`
}
type result struct {
	Mode          string         `json:"mode"`
	Streams       int            `json:"streams"`
	Bytes         int64          `json:"bytes"`
	Seconds       float64        `json:"seconds"`
	MbitPerSecond float64        `json:"mbitPerSecond"`
	MiBPerSecond  float64        `json:"mibPerSecond"`
	PerStream     []streamResult `json:"perStream"`
	HandshakeMS   float64        `json:"handshakeMs"`
}

func run(address, mode string, streams int, duration time.Duration) (result, error) {
	out := result{Mode: mode, Streams: streams, PerStream: make([]streamResult, streams)}
	errs := make([]error, streams)
	handshakes := make([]time.Duration, streams)
	var wg sync.WaitGroup
	for i := range streams {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out.PerStream[i], handshakes[i], errs[i] = stream(address, mode, duration)
		}()
	}
	wg.Wait()
	var longest float64
	for i, r := range out.PerStream {
		if errs[i] != nil {
			return out, errs[i]
		}
		out.Bytes += r.Bytes
		longest = max(longest, r.Seconds)
		out.HandshakeMS = max(out.HandshakeMS, float64(handshakes[i].Microseconds())/1000)
	}
	out.Seconds = longest
	if longest > 0 {
		out.MbitPerSecond = float64(out.Bytes) * 8 / longest / 1e6
		out.MiBPerSecond = float64(out.Bytes) / longest / (1 << 20)
	}
	return out, nil
}

func stream(address, mode string, duration time.Duration) (streamResult, time.Duration, error) {
	start := time.Now()
	// Raw link measurement only: the ephemeral server certificate is not an
	// identity. AirLink itself always pins the paired device identity.
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", address, &tls.Config{MinVersion: tls.VersionTLS13, InsecureSkipVerify: true})
	if err != nil {
		return streamResult{}, 0, err
	}
	defer conn.Close()
	handshake := time.Since(start)
	header := make([]byte, 9)
	buffer := make([]byte, bufferSize)
	if mode == "upload" {
		header[0] = 'U'
		if _, err = conn.Write(header); err != nil {
			return streamResult{}, handshake, err
		}
		_, _ = rand.Read(buffer)
		deadline := time.Now().Add(duration)
		for time.Now().Before(deadline) {
			if _, err = conn.Write(buffer); err != nil {
				return streamResult{}, handshake, err
			}
		}
		if err = conn.CloseWrite(); err != nil {
			return streamResult{}, handshake, err
		}
		reply := make([]byte, 16)
		if _, err = io.ReadFull(conn, reply); err != nil {
			return streamResult{}, handshake, err
		}
		return streamResult{Bytes: int64(binary.BigEndian.Uint64(reply[:8])), Seconds: time.Duration(binary.BigEndian.Uint64(reply[8:])).Seconds()}, handshake, nil
	}
	header[0] = 'D'
	binary.BigEndian.PutUint64(header[1:], uint64(duration))
	if _, err = conn.Write(header); err != nil {
		return streamResult{}, handshake, err
	}
	var total int64
	var first time.Time
	for {
		n, readErr := conn.Read(buffer)
		if n > 0 && first.IsZero() {
			first = time.Now()
		}
		total += int64(n)
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return streamResult{}, handshake, readErr
			}
			break
		}
	}
	return streamResult{Bytes: total, Seconds: time.Since(first).Seconds()}, handshake, nil
}
