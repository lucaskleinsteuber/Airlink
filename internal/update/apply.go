package update

import "crypto/ed25519"

type ApplyOptions struct {
	ParentPID        int
	ParentExecutable string
	Target           string
	Package          string
	Feed             string
	SignatureFile    string
	InstalledVersion string
	PublicKey        ed25519.PublicKey
	Ready            func() error
}
