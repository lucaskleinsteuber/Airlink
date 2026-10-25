namespace AirLink.Core;

/// <summary>The address is a hint; the paired certificate identifies the approved device.</summary>
public sealed record ConnectionDraft(string Address, string Fingerprint, bool AutomaticReceive = false)
{
    public string NormalizedFingerprint => NormalizeFingerprint(Fingerprint);
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

public static class NativeExperience
{
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
