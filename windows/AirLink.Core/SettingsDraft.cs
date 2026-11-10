using System.Globalization;

namespace AirLink.Core;

/// <summary>A settings edit applies only fields changed from its baseline.</summary>
public sealed class SettingsDraft
{
    private HostSettings baseline;
    public HostSettings Values { get; private set; }
    public SettingsDraft(HostSettings settings) { baseline = ProtocolJson.Clone(settings); Values = ProtocolJson.Clone(settings); }
    public bool HasChanges => Values.Name != baseline.Name || Values.ReceiveDirectory != baseline.ReceiveDirectory ||
        Values.Appearance != baseline.Appearance || Values.Language != baseline.Language || Values.Notifications != baseline.Notifications || Values.AutomaticUpdateChecks != baseline.AutomaticUpdateChecks ||
        Values.SendLimitBytesPerSecond != baseline.SendLimitBytesPerSecond || Values.ReceiveLimitBytesPerSecond != baseline.ReceiveLimitBytesPerSecond ||
        Values.Organization.DetectDuplicates != baseline.Organization.DetectDuplicates;
    public void Reset(HostSettings settings) { baseline = ProtocolJson.Clone(settings); Values = ProtocolJson.Clone(settings); }
    public HostSettings Merge(HostSettings latest) => Merge(latest, normalizeName: true);
    public bool Rebase(HostSettings latest)
    {
        if (SameDisplayedFields(baseline, latest)) return false;
        var rebased = Merge(latest, normalizeName: false);
        baseline = ProtocolJson.Clone(latest); Values = rebased; return true;
    }
    private static bool SameDisplayedFields(HostSettings a, HostSettings b) => a.Name == b.Name && a.ReceiveDirectory == b.ReceiveDirectory &&
        a.Appearance == b.Appearance && a.Language == b.Language && a.Notifications == b.Notifications && a.AutomaticUpdateChecks == b.AutomaticUpdateChecks &&
        a.SendLimitBytesPerSecond == b.SendLimitBytesPerSecond && a.ReceiveLimitBytesPerSecond == b.ReceiveLimitBytesPerSecond &&
        a.Organization.DetectDuplicates == b.Organization.DetectDuplicates && a.Organization.SortEnabled == b.Organization.SortEnabled;
    private HostSettings Merge(HostSettings latest, bool normalizeName)
    {
        var result = ProtocolJson.Clone(latest);
        if (Values.Name != baseline.Name) result.Name = normalizeName ? SetupDefaults.NormalizeName(Values.Name) : Values.Name;
        if (Values.ReceiveDirectory != baseline.ReceiveDirectory) result.ReceiveDirectory = normalizeName ? Values.ReceiveDirectory.Trim() : Values.ReceiveDirectory;
        if (Values.Appearance != baseline.Appearance) result.Appearance = Values.Appearance;
        if (Values.Language != baseline.Language) result.Language = Values.Language;
        if (Values.Notifications != baseline.Notifications) result.Notifications = Values.Notifications;
        if (Values.AutomaticUpdateChecks != baseline.AutomaticUpdateChecks) result.AutomaticUpdateChecks = Values.AutomaticUpdateChecks;
        if (Values.SendLimitBytesPerSecond != baseline.SendLimitBytesPerSecond) result.SendLimitBytesPerSecond = Values.SendLimitBytesPerSecond;
        if (Values.ReceiveLimitBytesPerSecond != baseline.ReceiveLimitBytesPerSecond) result.ReceiveLimitBytesPerSecond = Values.ReceiveLimitBytesPerSecond;
        if (Values.Organization.DetectDuplicates != baseline.Organization.DetectDuplicates) result.Organization.DetectDuplicates = Values.Organization.DetectDuplicates;
        return result;
    }
    public static string FormatLimit(long bytes, CultureInfo? culture = null) => (bytes / 1_000_000m).ToString("0.######", culture ?? CultureInfo.CurrentCulture);
    public static bool TryParseLimit(string text, out long bytes, CultureInfo? culture = null)
    {
        bytes = 0;
        if (!decimal.TryParse(text.Trim(), NumberStyles.Number, culture ?? CultureInfo.CurrentCulture, out var amount) || amount < 0 || amount > (1L << 40) / 1_000_000m) return false;
        var value = amount * 1_000_000m;
        if (value != decimal.Truncate(value) || value is > 0 and < 1024) return false;
        bytes = (long)value; return true;
    }
}
