package model

const Version = "0.12.0-beta.1"
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
	Title          string  `json:"title,omitempty"`
	FileCount      int     `json:"fileCount,omitempty"`
	Summary        bool    `json:"summary,omitempty"`
	HasContent     bool    `json:"hasContent,omitempty"`
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
	AutomaticUpdateChecks      bool         `json:"automaticUpdateChecks"`
	SendLimitBytesPerSecond    int64        `json:"sendLimitBytesPerSecond"`
	ReceiveLimitBytesPerSecond int64        `json:"receiveLimitBytesPerSecond"`
	Shortcut                   Shortcut     `json:"shortcut"`
	Organization               Organization `json:"organization"`
}
type Status struct {
	Summary             bool               `json:"summary,omitempty"`
	Capabilities        []string           `json:"capabilities,omitempty"`
	ActiveTransferCount int                `json:"activeTransferCount,omitempty"`
	ActiveRelayCount    int                `json:"activeRelayCount,omitempty"`
	Preparations        []Preparation      `json:"preparations,omitempty"`
	PairingSessions     []PairingSession   `json:"pairingSessions,omitempty"`
	Version             string             `json:"version"`
	ID                  string             `json:"id"`
	Name                string             `json:"name"`
	URLs                []string           `json:"urls"`
	SetupURLs           []string           `json:"setupURLs"`
	Fingerprint         string             `json:"fingerprint"`
	CertificatePath     string             `json:"certificatePath,omitempty"`
	ChunkSize           int64              `json:"chunkSize"`
	Devices             []Device           `json:"devices"`
	Transfers           []Transfer         `json:"transfers"`
	PairRequests        []PairRequest      `json:"pairRequests"`
	Settings            Settings           `json:"settings"`
	Shares              []Share            `json:"shares"`
	Clipboard           []ClipboardMessage `json:"clipboard"`
	ClipboardAllowed    bool               `json:"clipboardAllowed"`
	CurrentDeviceID     string             `json:"currentDeviceId"`
	RelayDevices        []Device           `json:"relayDevices"`
	Relays              []Relay            `json:"relays"`
	Statistics          Statistics         `json:"statistics"`
}

type Preparation struct {
	ID         string `json:"id"`
	RequestID  string `json:"requestId"`
	DeviceID   string `json:"deviceId"`
	State      string `json:"state"`
	TransferID string `json:"transferId,omitempty"`
	Error      string `json:"error,omitempty"`
}

type PairingSession struct {
	ID               string `json:"id"`
	PeerID           string `json:"peerId"`
	PeerName         string `json:"peerName"`
	State            string `json:"state"`
	ComparisonCode   string `json:"comparisonCode,omitempty"`
	ExpiresAt        string `json:"expiresAt"`
	LocalConfirmed   bool   `json:"localConfirmed"`
	RemoteConfirmed  bool   `json:"remoteConfirmed"`
	AutomaticReceive bool   `json:"automaticReceive"`
	Error            string `json:"error,omitempty"`
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
