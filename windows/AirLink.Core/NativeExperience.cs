namespace AirLink.Core;

/// <summary>The address is a hint; the paired certificate identifies the approved device.</summary>
public sealed record ConnectionDraft(string Address, string Fingerprint, bool AutomaticReceive = false)
{
    public string NormalizedFingerprint => NormalizeFingerprint(Fingerprint);
    public string ComparisonFingerprint => string.Join(" ", Enumerable.Range(0, (NormalizedFingerprint.Length + 7) / 8)
        .Select(index => NormalizedFingerprint.Substring(index * 8, Math.Min(8, NormalizedFingerprint.Length - index * 8))));
    public string NormalizedAddress => IsValid ? new Uri(Address.Trim().TrimEnd('/')).GetLeftPart(UriPartial.Authority) : Address.Trim().TrimEnd('/');
    public bool IsValid
    {
        get
        {
            var address = Address.Trim().TrimEnd('/');
            return Uri.TryCreate(address, UriKind.Absolute, out var uri) && uri.Scheme == "https" &&
                !string.IsNullOrEmpty(uri.Host) && uri.UserInfo.Length == 0 && uri.Query.Length == 0 &&
                uri.Fragment.Length == 0 && uri.AbsolutePath == "/" &&
                NormalizedFingerprint.Length == 64 && NormalizedFingerprint.All(Uri.IsHexDigit);
        }
    }
    public Device? PairedDevice(IEnumerable<Device> devices) => !IsValid ? null : devices.FirstOrDefault(device =>
        device.Paired && device.Kind == "engine" &&
        string.Equals(device.Fingerprint, NormalizedFingerprint, StringComparison.OrdinalIgnoreCase));

    private static string NormalizeFingerprint(string value) => string.Concat(value.Where(c => !char.IsWhiteSpace(c) && c != ':')).ToLowerInvariant();
}

/// <summary>Consent belongs to the displayed address and complete certificate identity.</summary>
public sealed class ConnectionVerification
{
    private string? address, fingerprint;
    public void Confirm(ConnectionDraft draft)
    {
        address = draft.IsValid ? draft.NormalizedAddress : null;
        fingerprint = draft.IsValid ? draft.NormalizedFingerprint : null;
    }
    public void Reset() { address = null; fingerprint = null; }
    public bool Matches(ConnectionDraft draft) => draft.IsValid && address == draft.NormalizedAddress && fingerprint == draft.NormalizedFingerprint;
}

public sealed class HistoryRemovalConflictException(string transferId) : InvalidOperationException("A transfer is active again and cannot be removed.")
{
    public string TransferId { get; } = transferId;
}

/// <summary>A fixed user selection, checked against fresh state before every request.</summary>
public sealed class HistoryRemovalPlan
{
    public IReadOnlyList<string> Ids { get; }
    public HistoryRemovalPlan(IEnumerable<Transfer> selection) => Ids = NativeExperience.HistoryRemovalIds(selection);
    public async Task<int> ExecuteAsync(Func<IReadOnlyList<Transfer>> currentTransfers, Func<string, Task> remove, CancellationToken cancellationToken = default)
    {
        var count = 0;
        foreach (var id in Ids)
        {
            cancellationToken.ThrowIfCancellationRequested();
            var current = currentTransfers().FirstOrDefault(t => t.Id == id);
            if (current is null) continue;
            if (!NativeExperience.CanRemoveFromHistory(current)) throw new HistoryRemovalConflictException(id);
            await remove(id);
            count++;
        }
        return count;
    }
}

public static class NativeExperience
{
    public static bool CanRemoveFromHistory(Transfer transfer) => transfer.State is "completed" or "failed" or "cancelled";

    /// <summary>Capture only the explicitly selected terminal file records before asynchronous work.</summary>
    public static IReadOnlyList<string> HistoryRemovalIds(IEnumerable<Transfer> transfers) => transfers
        .Where(CanRemoveFromHistory).Select(t => t.Id).Where(id => !string.IsNullOrEmpty(id)).Distinct(StringComparer.Ordinal).ToArray();
    public static IReadOnlyList<Transfer> RecentTransfers(IEnumerable<Transfer> transfers, int limit = 20) => transfers
        .Where(t => !t.IsActive)
        .OrderByDescending(t => DateTimeOffset.TryParse(string.IsNullOrEmpty(t.UpdatedAt) ? t.CreatedAt : t.UpdatedAt, out var time) ? time : DateTimeOffset.MinValue)
        .ThenBy(t => t.Id, StringComparer.Ordinal)
        .Take(Math.Clamp(limit, 0, 100)).ToArray();

    public static bool CanAcceptAndTrust(Transfer transfer, Device? device) =>
        transfer.Direction == "receive" && transfer.State == "offered" && device is { Paired: true, CanSend: true } && device.Id == transfer.DeviceId;

    /// <summary>Display identities only. The reveal action must still request the helper's validated published paths.</summary>
    public static IReadOnlyList<TransferFile> ReceivedDisplayEntries(Transfer transfer)
    {
        if (transfer.Direction != "receive" || transfer.State != "completed") return [];
        var regular = transfer.Files.Where(file => !file.Directory).ToArray();
        if (regular.Length > 0 && !regular.Any(file => file.Completed && !file.Skipped)) return [];
        var actual = transfer.Files.Where(file => file.Completed && !file.Skipped && ValidRelative(EffectivePath(file))).ToArray();
        var directories = actual.Where(file => file.Directory).Select(file => EffectivePath(file) + "/").ToArray();
        return actual.Where(file => !directories.Any(parent => EffectivePath(file).StartsWith(parent, StringComparison.Ordinal))).ToArray();
    }

    private static string EffectivePath(TransferFile file) => string.IsNullOrEmpty(file.DestinationPath) ? file.Path : file.DestinationPath;
    private static bool ValidRelative(string path) => !string.IsNullOrEmpty(path) && !path.Contains('\\') && !path.StartsWith('/') &&
        path.Split('/').All(part => part.Length > 0 && part is not ("." or ".."));
}
