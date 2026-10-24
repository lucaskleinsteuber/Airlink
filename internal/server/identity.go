package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func randomID() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func atomicFile(path string, data []byte) error {
	f, err := os.OpenFile(path+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(path + ".tmp")
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}
func localIPs() []net.IP {
	ips := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && !n.IP.IsLoopback() && n.IP.IsGlobalUnicast() {
			ips = append(ips, n.IP)
		}
	}
	sort.SliceStable(ips, func(i, j int) bool {
		score := func(ip net.IP) int {
			if ip.IsPrivate() && ip.To4() != nil {
				return 0
			}
			if ip.IsPrivate() {
				return 1
			}
			if ip.IsLoopback() {
				return 3
			}
			return 2
		}
		return score(ips[i]) < score(ips[j])
	})
	return ips
}
func identity(dir, name string) (tls.Certificate, string, []byte, error) {
	certPath := filepath.Join(dir, "certificate.pem")
	keyPath := filepath.Join(dir, "identity-key.pem")
	certPEM, certErr := os.ReadFile(certPath)
	keyPEM, keyErr := os.ReadFile(keyPath)
	var ca *x509.Certificate
	var key *ecdsa.PrivateKey
	if certErr == nil && keyErr == nil {
		block, _ := pem.Decode(certPEM)
		kb, _ := pem.Decode(keyPEM)
		if block == nil || kb == nil {
			return tls.Certificate{}, "", nil, fmt.Errorf("invalid saved identity")
		}
		var err error
		ca, err = x509.ParseCertificate(block.Bytes)
		if err != nil {
			return tls.Certificate{}, "", nil, err
		}
		parsed, err := x509.ParsePKCS8PrivateKey(kb.Bytes)
		if err != nil {
			return tls.Certificate{}, "", nil, err
		}
		var ok bool
		key, ok = parsed.(*ecdsa.PrivateKey)
		if !ok {
			return tls.Certificate{}, "", nil, fmt.Errorf("invalid identity key")
		}
	} else if os.IsNotExist(certErr) && os.IsNotExist(keyErr) {
		var err error
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return tls.Certificate{}, "", nil, err
		}
		serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		tmpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "AirLink " + name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, MaxPathLenZero: true}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		if err != nil {
			return tls.Certificate{}, "", nil, err
		}
		ca, _ = x509.ParseCertificate(der)
		certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
		kb, _ := x509.MarshalPKCS8PrivateKey(key)
		keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb})
		if err = atomicFile(keyPath, keyPEM); err != nil {
			return tls.Certificate{}, "", nil, err
		}
		if err = atomicFile(certPath, certPEM); err != nil {
			return tls.Certificate{}, "", nil, err
		}
	} else {
		return tls.Certificate{}, "", nil, fmt.Errorf("identity incomplete: certificate=%v key=%v", certErr, keyErr)
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	host, _ := os.Hostname()
	leaf := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "AirLink"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(0, 3, 0), DNSNames: []string{"localhost", host, strings.TrimSuffix(host, ".local") + ".local"}, IPAddresses: localIPs(), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, "", nil, err
	}
	chain := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), certPEM...)
	pair, err := tls.X509KeyPair(chain, keyPEM)
	return pair, digest(ca.Raw), certPEM, err
}

// The TLS root is pinned out of band during pairing. Verification still checks
// the leaf chain, hostname, expiry and key usages; it never blindly skips TLS.
func pinnedClient(fingerprint string) *http.Client {
	fp := strings.ToLower(strings.ReplaceAll(fingerprint, ":", ""))
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, InsecureSkipVerify: true, VerifyConnection: func(c tls.ConnectionState) error {
		if len(c.PeerCertificates) < 2 {
			return fmt.Errorf("%w: peer omitted identity certificate", errPeerIdentity)
		}
		root := c.PeerCertificates[len(c.PeerCertificates)-1]
		if digest(root.Raw) != fp {
			return fmt.Errorf("%w: device identity changed: pairing required", errPeerIdentity)
		}
		roots := x509.NewCertPool()
		roots.AddCert(root)
		inter := x509.NewCertPool()
		for _, cert := range c.PeerCertificates[1 : len(c.PeerCertificates)-1] {
			inter.AddCert(cert)
		}
		_, err := c.PeerCertificates[0].Verify(x509.VerifyOptions{Roots: roots, Intermediates: inter, DNSName: c.ServerName})
		if err != nil {
			return fmt.Errorf("%w: %w", errPeerIdentity, err)
		}
		return nil
	}}, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: 10 * time.Second, IdleConnTimeout: 30 * time.Second, MaxIdleConnsPerHost: 4}}
}
