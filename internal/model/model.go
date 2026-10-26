package model

const Version = "0.11.0"
const ChunkSize int64 = 8 * 1024 * 1024

type File struct {
	ID                  string               `json:"id"`
	Path                string               `json:"path"`
	Size                int64                `json:"size"`
	Modified            int64                `json:"modified,omitempty"`
	Directory           bool                 `json:"directory,omitempty"`
	SHA256              string               `json:"sha256,omitempty"`
	Received            int64                `json:"received"`
	Completed           bool                 `json:"completed"`
	DestinationPath     string               `json:"destinationPath,omitempty"`
	Skipped             bool                 `json:"skipped,omitempty"`
	DuplicateState      string               `json:"duplicateState,omitempty"`
	DuplicateCandidates []DuplicateCandidate `json:"duplicateCandidates,omitempty"`
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
	SkippedBytes   int64   `json:"skippedBytes"`
}
type Device struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Address      string `json:"address,omitempty"`
	Fingerprint  string `json:"fingerprint,omitempty"`
	Paired       bool   `json:"paired"`
	Online       bool   `json:"online"`
	CanSend      bool   `json:"canSend"`
	CanReceive   bool   `json:"canReceive"`
	CanClipboard bool   `json:"canClipboard"`
	AutoAccept   bool   `json:"autoAccept"`
	CanRelay     bool   `json:"canRelay"`
	LastSeen     string `json:"lastSeen,omitempty"`
}
type PairRequest struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"createdAt"`
}
type Settings struct {
	SetupCompleted             bool         `json:"setupCompleted"`
	Name                       string       `json:"name"`
	ReceiveDirectory           string       `json:"receiveDirectory"`
	Appearance                 string       `json:"appearance"`
	Language                   string       `json:"language"`
	Notifications              bool         `json:"notifications"`
	SendLimitBytesPerSecond    int64        `json:"sendLimitBytesPerSecond"`
	ReceiveLimitBytesPerSecond int64        `json:"receiveLimitBytesPerSecond"`
	Shortcut                   Shortcut     `json:"shortcut"`
	Organization               Organization `json:"organization"`
}
type Status struct {
	Version          string             `json:"version"`
	ID               string             `json:"id"`
	Name             string             `json:"name"`
	URLs             []string           `json:"urls"`
	SetupURLs        []string           `json:"setupURLs"`
	Fingerprint      string             `json:"fingerprint"`
	CertificatePath  string             `json:"certificatePath,omitempty"`
	ChunkSize        int64              `json:"chunkSize"`
	Devices          []Device           `json:"devices"`
	Transfers        []Transfer         `json:"transfers"`
	PairRequests     []PairRequest      `json:"pairRequests"`
	Settings         Settings           `json:"settings"`
	Shares           []Share            `json:"shares"`
	Clipboard        []ClipboardMessage `json:"clipboard"`
	ClipboardAllowed bool               `json:"clipboardAllowed"`
	CurrentDeviceID  string             `json:"currentDeviceId"`
	RelayDevices     []Device           `json:"relayDevices"`
	Relays           []Relay            `json:"relays"`
	Statistics       Statistics         `json:"statistics"`
}
type Offer struct {
	Files []File `json:"files"`
}

type Share struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	URL               string `json:"url,omitempty"`
	Files             []File `json:"files"`
	TotalBytes        int64  `json:"totalBytes"`
	CreatedAt         string `json:"createdAt"`
	ExpiresAt         string `json:"expiresAt,omitempty"`
	MaxDownloads      int64  `json:"maxDownloads"`
	Downloads         int64  `json:"downloads"`
	PasswordProtected bool   `json:"passwordProtected"`
	Revoked           bool   `json:"revoked"`
}
type ShareCreate struct {
	Name         string   `json:"name"`
	Paths        []string `json:"paths,omitempty"`
	TransferID   string   `json:"transferId,omitempty"`
	Password     string   `json:"password,omitempty"`
	ExpiresAt    string   `json:"expiresAt,omitempty"`
	MaxDownloads int64    `json:"maxDownloads"`
}
type ClipboardMessage struct {
	ID         string `json:"id"`
	DeviceID   string `json:"deviceId"`
	DeviceName string `json:"deviceName"`
	Direction  string `json:"direction"`
	Text       string `json:"text"`
	CreatedAt  string `json:"createdAt"`
}

type Shortcut struct {
	Enabled   bool   `json:"enabled"`
	KeyCode   uint32 `json:"keyCode"`
	Modifiers uint32 `json:"modifiers"`
}
type Relay struct {
	ID             string  `json:"id"`
	SenderID       string  `json:"senderId"`
	SenderName     string  `json:"senderName"`
	ReceiverID     string  `json:"receiverId"`
	ReceiverName   string  `json:"receiverName"`
	State          string  `json:"state"`
	Files          []File  `json:"files"`
	TotalBytes     int64   `json:"totalBytes"`
	DeliveredBytes int64   `json:"deliveredBytes"`
	BytesPerSecond float64 `json:"bytesPerSecond"`
	CreatedAt      string  `json:"createdAt"`
	UpdatedAt      string  `json:"updatedAt"`
	Error          string  `json:"error,omitempty"`
}
type RelayCreate struct {
	ReceiverID string `json:"receiverId"`
	Files      []File `json:"files"`
}
type RelayRequest struct {
	RequestID string `json:"requestId"`
	FileID    string `json:"fileId"`
	Index     int64  `json:"index"`
}
type History struct {
	Transfers []Transfer `json:"transfers"`
	Relays    []Relay    `json:"relays"`
}
type Statistics struct {
	SentBytes            int64           `json:"sentBytes"`
	ReceivedBytes        int64           `json:"receivedBytes"`
	CompletedTransfers   int64           `json:"completedTransfers"`
	FailedTransfers      int64           `json:"failedTransfers"`
	CancelledTransfers   int64           `json:"cancelledTransfers"`
	ActiveTransfers      int64           `json:"activeTransfers"`
	RelayedBytes         int64           `json:"relayedBytes"`
	RelayDeliveries      int64           `json:"relayDeliveries"`
	TrafficSentBytes     int64           `json:"trafficSentBytes"`
	TrafficReceivedBytes int64           `json:"trafficReceivedBytes"`
	SkippedFiles         int64           `json:"skippedFiles"`
	SkippedBytes         int64           `json:"skippedBytes"`
	Days                 []DayStatistics `json:"days"`
}

type Organization struct {
	DetectDuplicates bool       `json:"detectDuplicates"`
	SortEnabled      bool       `json:"sortEnabled"`
	Rules            []SortRule `json:"rules"`
}
type SortRule struct {
	ID         string   `json:"id"`
	Extensions []string `json:"extensions"`
	Folder     string   `json:"folder"`
}
type DuplicateCandidate struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}
type DayStatistics struct {
	Date               string `json:"date"`
	SentBytes          int64  `json:"sentBytes"`
	ReceivedBytes      int64  `json:"receivedBytes"`
	RelayedBytes       int64  `json:"relayedBytes"`
	CompletedTransfers int64  `json:"completedTransfers"`
}
