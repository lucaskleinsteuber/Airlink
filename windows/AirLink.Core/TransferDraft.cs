using System.Collections.ObjectModel;

namespace AirLink.Core;

/// <summary>A selection entry has an identity independent of its path and any earlier send.</summary>
public sealed class SendEntry(string path)
{
    public Guid Id { get; } = Guid.NewGuid();
    public string Path { get; } = path;
    public string Title
    {
        get
        {
            var name = System.IO.Path.GetFileName(System.IO.Path.TrimEndingDirectorySeparator(Path));
            return string.IsNullOrEmpty(name) ? Path : name;
        }
    }
}

/// <summary>An immutable offer snapshot; its recipient never follows later UI selection changes.</summary>
public sealed class SendSubmission
{
    internal SendSubmission(string deviceId, IEnumerable<SendEntry> entries, Guid? token = null)
    {
        Token = token ?? Guid.NewGuid();
        DeviceId = deviceId;
        Entries = Array.AsReadOnly(entries.ToArray());
        Paths = Array.AsReadOnly(Entries.Select(entry => entry.Path).ToArray());
    }

    public Guid Token { get; }
    public string DeviceId { get; }
    public string TargetId => DeviceId;
    public IReadOnlyList<SendEntry> Entries { get; }
    public IReadOnlyList<string> Paths { get; }
}

/// <summary>
/// Native Windows send state. The caller validates actual source availability before
/// adding paths; the helper remains authoritative for filesystem and peer permissions.
/// Use on the owning UI thread, without any dependency on WPF or native dialogs.
/// </summary>
public sealed class TransferDraft
{
    private SendSubmission? active;
    private SendSubmission? retryable;
    public ObservableCollection<SendEntry> Entries { get; } = [];
    public string? SelectedDeviceId { get; private set; }
    public bool HasExplicitTarget { get; private set; }
    public bool IsSending => active is not null;

    public static bool CanSendTo(Device? device) => device is { Paired: true, CanReceive: true, Kind: "engine" };

    public void SelectDevice(string? deviceId)
    {
        SelectedDeviceId = string.IsNullOrWhiteSpace(deviceId) ? null : deviceId;
        HasExplicitTarget = SelectedDeviceId is not null;
    }

    /// <summary>
    /// Only a fresh, empty draft can choose its sole online desktop automatically.
    /// Once a recipient is shown, a refresh never redirects it to a different device.
    /// An offline paired desktop may still receive an explicitly queued offer.
    /// </summary>
    public void RefreshDevices(IEnumerable<Device> devices, bool allowAutomaticSelection = true)
    {
        ArgumentNullException.ThrowIfNull(devices);
        if (!allowAutomaticSelection || SelectedDeviceId is not null || HasExplicitTarget || Entries.Count > 0 || IsSending) return;
        var eligible = devices.Where(device => CanSendTo(device) && device.Online).Take(2).ToArray();
        if (eligible.Length == 1) SelectedDeviceId = eligible[0].Id;
    }

    /// <summary>Return only the entries corresponding to this batch, preserving other selected files.</summary>
    public IReadOnlyList<SendEntry> AddPaths(IEnumerable<string> paths)
    {
        var normalized = NormalizePaths(paths);
        return AddNormalized(normalized);
    }

    public SendSubmission? TryBegin(IEnumerable<Device> devices, bool serviceReady)
    {
        ArgumentNullException.ThrowIfNull(devices);
        if (!serviceReady || IsSending || Entries.Count == 0 ||
            !CanSendTo(devices.FirstOrDefault(device => device.Id == SelectedDeviceId))) return null;
        return Begin(SelectedDeviceId!, Entries);
    }

    /// <summary>
    /// A drop on a device is an explicit offer for that batch alone. Busy, unavailable,
    /// and unauthorized starts leave the draft untouched so callers can report failure.
    /// </summary>
    public SendSubmission? TryBeginDirect(string deviceId, IEnumerable<string> paths, IEnumerable<Device> devices, bool serviceReady)
    {
        ArgumentNullException.ThrowIfNull(devices);
        if (!serviceReady || IsSending || !CanSendTo(devices.FirstOrDefault(device => device.Id == deviceId))) return null;
        var normalized = NormalizePaths(paths);
        if (normalized.Length == 0) return null;
        var batch = AddNormalized(normalized);
        SelectDevice(deviceId);
        return Begin(deviceId, batch);
    }

    public bool FinishSuccess(SendSubmission submission)
    {
        if (!ReferenceEquals(active, submission)) return false;
        var completed = submission.Entries.Select(entry => entry.Id).ToHashSet();
        foreach (var entry in Entries.Where(entry => completed.Contains(entry.Id)).ToArray()) Entries.Remove(entry);
        active = null;
        retryable = null;
        return true;
    }

    public bool Fail(SendSubmission submission)
    {
        if (!ReferenceEquals(active, submission)) return false;
        retryable = active;
        active = null;
        return true;
    }

    private SendSubmission Begin(string deviceId, IEnumerable<SendEntry> entries)
    {
        var batch = entries.ToArray();
        // A failed local request may have been committed before its reply was lost.
        // Reuse the persisted offer key only for exactly the same recipient/entries.
        var requestId = retryable?.DeviceId == deviceId && retryable.Entries.Select(entry => entry.Id).SequenceEqual(batch.Select(entry => entry.Id))
            ? retryable.Token : (Guid?)null;
        // Each attempt still has its own object identity: a late local failure from
        // an earlier attempt must not clear an active retry sharing the wire key.
        active = new SendSubmission(deviceId, batch, requestId);
        retryable = null;
        return active;
    }

    private IReadOnlyList<SendEntry> AddNormalized(IEnumerable<string> paths)
    {
        var batch = new List<SendEntry>();
        foreach (var path in paths)
        {
            var entry = Entries.FirstOrDefault(candidate => StringComparer.OrdinalIgnoreCase.Equals(candidate.Path, path));
            if (entry is null) { entry = new SendEntry(path); Entries.Add(entry); }
            batch.Add(entry);
        }
        return batch.AsReadOnly();
    }

    private static string[] NormalizePaths(IEnumerable<string> paths)
    {
        ArgumentNullException.ThrowIfNull(paths);
        // Validate the entire batch before mutating the visible draft.
        return paths.Select(path =>
        {
            if (string.IsNullOrWhiteSpace(path) || !System.IO.Path.IsPathFullyQualified(path))
                throw new ArgumentException("Send paths must be absolute.", nameof(paths));
            return System.IO.Path.TrimEndingDirectorySeparator(System.IO.Path.GetFullPath(path));
        }).Distinct(StringComparer.OrdinalIgnoreCase).ToArray();
    }
}
