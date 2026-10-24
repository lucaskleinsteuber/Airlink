package model

const Version = "0.1.0"
const ChunkSize int64 = 8 * 1024 * 1024

type File struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Modified  int64  `json:"modified,omitempty"`
	Directory bool   `json:"directory,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	Received  int64  `json:"received"`
	Completed bool   `json:"completed"`
}
type Transfer struct {
	ID             string  `json:"id"`
	DeviceID       string  `json:"deviceId"`
	DeviceName     string  `json:"deviceName"`
	Direction      string  `json:"direction"`
	State          string  `json:"state"`
	Files          []File  `json:"files"`
	TotalBytes     int64   `json:"totalBytes"`
	CompletedBytes int64   `json:"completedBytes"`
	BytesPerSecond float64 `json:"bytesPerSecond"`
	CreatedAt      string  `json:"createdAt"`
	UpdatedAt      string  `json:"updatedAt"`
	Error          string  `json:"error,omitempty"`
	Destination    string  `json:"destination,omitempty"`
}
type Device struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Address     string `json:"address,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Paired      bool   `json:"paired"`
	Online      bool   `json:"online"`
	CanSend     bool   `json:"canSend"`
	CanReceive  bool   `json:"canReceive"`
	AutoAccept  bool   `json:"autoAccept"`
	LastSeen    string `json:"lastSeen,omitempty"`
}
type PairRequest struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"createdAt"`
}
type Settings struct {
	Name             string `json:"name"`
	ReceiveDirectory string `json:"receiveDirectory"`
	Appearance       string `json:"appearance"`
	Language         string `json:"language"`
	Notifications    bool   `json:"notifications"`
}
type Status struct {
	Version         string        `json:"version"`
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	URLs            []string      `json:"urls"`
	SetupURLs       []string      `json:"setupURLs"`
	Fingerprint     string        `json:"fingerprint"`
	CertificatePath string        `json:"certificatePath,omitempty"`
	ChunkSize       int64         `json:"chunkSize"`
	Devices         []Device      `json:"devices"`
	Transfers       []Transfer    `json:"transfers"`
	PairRequests    []PairRequest `json:"pairRequests"`
	Settings        Settings      `json:"settings"`
}
type Offer struct {
	Files []File `json:"files"`
}
