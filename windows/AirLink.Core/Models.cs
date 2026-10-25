using System.Text.Json;
using System.Text.Json.Serialization;

namespace AirLink.Core;

public static class ProtocolJson
{
    public static JsonSerializerOptions Options { get; } = new(JsonSerializerDefaults.Web)
    {
        PropertyNameCaseInsensitive = true,
        DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull
    };

    public static T Clone<T>(T value) => JsonSerializer.Deserialize<T>(JsonSerializer.Serialize(value, Options), Options)
        ?? throw new JsonException("Unable to copy the AirLink settings.");
}

public sealed class HostStatus
{
    public string Version { get; set; } = "";
    public string Id { get; set; } = "";
    public string Name { get; set; } = "";
    public List<string> Urls { get; set; } = [];
    public List<string> SetupUrls { get; set; } = [];
    public string Fingerprint { get; set; } = "";
    public string? CertificatePath { get; set; }
    public long ChunkSize { get; set; }
    public List<Device> Devices { get; set; } = [];
    public List<Transfer> Transfers { get; set; } = [];
    public List<PairRequest> PairRequests { get; set; } = [];
    public HostSettings Settings { get; set; } = new();
    public List<Share> Shares { get; set; } = [];
    public List<ClipboardMessage> Clipboard { get; set; } = [];
    public List<Relay> Relays { get; set; } = [];
    public Statistics Statistics { get; set; } = new();
    public bool ClipboardAllowed { get; set; }
    public string CurrentDeviceId { get; set; } = "";
    public List<Device> RelayDevices { get; set; } = [];
}

public sealed class HostSettings
{
    public bool SetupCompleted { get; set; }
    public string Name { get; set; } = "";
    public string ReceiveDirectory { get; set; } = "";
    public string Appearance { get; set; } = "system";
    public string Language { get; set; } = "system";
    public bool Notifications { get; set; } = true;
    public long SendLimitBytesPerSecond { get; set; }
    public long ReceiveLimitBytesPerSecond { get; set; }
    public Shortcut Shortcut { get; set; } = new();
    public Organization Organization { get; set; } = new();
    [JsonExtensionData] public Dictionary<string, JsonElement>? Additional { get; set; }
}

public sealed class Shortcut
{
    public bool Enabled { get; set; } = true;
    public uint KeyCode { get; set; } = 37;
    public uint Modifiers { get; set; } = 768;
    [JsonExtensionData] public Dictionary<string, JsonElement>? Additional { get; set; }
}

public sealed class Organization
{
    public bool DetectDuplicates { get; set; }
    public bool SortEnabled { get; set; }
    public List<SortRule> Rules { get; set; } = [];
    [JsonExtensionData] public Dictionary<string, JsonElement>? Additional { get; set; }
}

public sealed class SortRule
{
    public string Id { get; set; } = "";
    public List<string> Extensions { get; set; } = [];
    public string Folder { get; set; } = "";
    [JsonExtensionData] public Dictionary<string, JsonElement>? Additional { get; set; }
}

public sealed class Device
{
    public string Id { get; set; } = "";
    public string Name { get; set; } = "";
    public string Kind { get; set; } = "";
    public string? Address { get; set; }
    public string? Fingerprint { get; set; }
    public bool Paired { get; set; }
    public bool Online { get; set; }
    public bool CanSend { get; set; }
    public bool CanReceive { get; set; }
    public bool CanClipboard { get; set; }
    public bool CanRelay { get; set; }
    public bool AutoAccept { get; set; }
    public string? LastSeen { get; set; }
    [JsonExtensionData] public Dictionary<string, JsonElement>? Additional { get; set; }
}

public sealed class PairRequest
{
    public string Id { get; set; } = "";
    public string Name { get; set; } = "";
    public string Kind { get; set; } = "";
    public string CreatedAt { get; set; } = "";
}

public sealed class Transfer
{
    public string Id { get; set; } = "";
    public string DeviceId { get; set; } = "";
    public string DeviceName { get; set; } = "";
    public string Direction { get; set; } = "";
    public string State { get; set; } = "";
    public List<TransferFile> Files { get; set; } = [];
    public long TotalBytes { get; set; }
    public long CompletedBytes { get; set; }
    public long SkippedBytes { get; set; }
    public double BytesPerSecond { get; set; }
    public string CreatedAt { get; set; } = "";
    public string UpdatedAt { get; set; } = "";
    public string? Error { get; set; }
    public string? Destination { get; set; }
    [JsonIgnore] public bool IsActive => State is "offered" or "queued" or "running" or "paused" or "reconnecting" or "verifying" or "checkingDuplicate" or "waitingDuplicate";
    [JsonIgnore] public double Progress => State == "completed" ? 1 : TotalBytes > 0 ? Math.Clamp((double)CompletedBytes / TotalBytes + (double)SkippedBytes / TotalBytes, 0, 1) : 0;
    [JsonIgnore] public string Title => Files.Count == 0 ? "AirLink" : Files[0].Path.Split('/').Last();
}

public sealed class TransferFile
{
    public string Id { get; set; } = "";
    public string Path { get; set; } = "";
    public long Size { get; set; }
    public long Modified { get; set; }
    public bool Directory { get; set; }
    public string? Sha256 { get; set; }
    public long Received { get; set; }
    public bool Completed { get; set; }
    public string? DestinationPath { get; set; }
    public bool Skipped { get; set; }
    public string? DuplicateState { get; set; }
    public List<DuplicateCandidate> DuplicateCandidates { get; set; } = [];
}

public sealed class DuplicateCandidate
{
    public string Id { get; set; } = "";
    public string Path { get; set; } = "";
    public long Size { get; set; }
    public string Sha256 { get; set; } = "";
}

public sealed class ReceivedResult
{
    public string Directory { get; set; } = "";
    public List<ReceivedItem> Items { get; set; } = [];
}

public sealed class ReceivedItem
{
    public string FileId { get; set; } = "";
    public string Path { get; set; } = "";
    public bool Directory { get; set; }
}

public sealed class Share
{
    public string Id { get; set; } = "";
    public string Name { get; set; } = "";
    public string? Url { get; set; }
    public List<TransferFile> Files { get; set; } = [];
    public long TotalBytes { get; set; }
    public string CreatedAt { get; set; } = "";
    public string? ExpiresAt { get; set; }
    public long MaxDownloads { get; set; }
    public long Downloads { get; set; }
    public bool PasswordProtected { get; set; }
    public bool Revoked { get; set; }
}

public sealed class ClipboardMessage
{
    public string Id { get; set; } = "";
    public string DeviceId { get; set; } = "";
    public string DeviceName { get; set; } = "";
    public string Direction { get; set; } = "";
    public string Text { get; set; } = "";
    public string CreatedAt { get; set; } = "";
}

public sealed class Relay
{
    public string Id { get; set; } = "";
    public string SenderId { get; set; } = "";
    public string SenderName { get; set; } = "";
    public string ReceiverId { get; set; } = "";
    public string ReceiverName { get; set; } = "";
    public string State { get; set; } = "";
    public List<TransferFile> Files { get; set; } = [];
    public long TotalBytes { get; set; }
    public long DeliveredBytes { get; set; }
    public double BytesPerSecond { get; set; }
    public string CreatedAt { get; set; } = "";
    public string UpdatedAt { get; set; } = "";
    public string? Error { get; set; }
}

public sealed class Statistics
{
    public long SentBytes { get; set; }
    public long ReceivedBytes { get; set; }
    public long CompletedTransfers { get; set; }
    public long FailedTransfers { get; set; }
    public long CancelledTransfers { get; set; }
    public long ActiveTransfers { get; set; }
    public long RelayedBytes { get; set; }
    public long RelayDeliveries { get; set; }
    public long TrafficSentBytes { get; set; }
    public long TrafficReceivedBytes { get; set; }
    public long SkippedFiles { get; set; }
    public long SkippedBytes { get; set; }
    public List<DayStatistics> Days { get; set; } = [];
}

public sealed class DayStatistics
{
    public string Date { get; set; } = "";
    public long SentBytes { get; set; }
    public long ReceivedBytes { get; set; }
    public long RelayedBytes { get; set; }
    public long CompletedTransfers { get; set; }
}
