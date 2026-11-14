using System.Globalization;

namespace AirLink.Core;

/// <summary>What a computer tile shows under its name.</summary>
public sealed record DeviceActivity(string Label, double? Progress, bool Busy);

/// <summary>Presentation rules shared by the tiles, "Recent" and the history.</summary>
public static class DeviceExperience
{
    public static DeviceActivity Activity(Device device, IEnumerable<Transfer> transfers, IEnumerable<PairingSession> pairings, bool preparing, bool english)
    {
        string T(string de, string en) => english ? en : de;
        if (!device.Paired)
        {
            if (pairings.Any(p => p.IsOpen && (p.PeerId == device.Id || p.PeerName == device.Name))) return new(T("Verbindet …", "Connecting …"), null, true);
            return device.Outdated ? new(T("Braucht Update", "Needs update"), null, false) : new(T("Zum Verbinden klicken", "Click to connect"), null, false);
        }
        if (preparing) return new(T("Wird vorbereitet …", "Preparing …"), null, true);
        var mine = transfers.Where(t => t.DeviceId == device.Id && t.IsActive).ToList();
        if (mine.Count > 0)
        {
            double total = mine.Sum(t => (double)Math.Max(0, t.TotalBytes)), done = mine.Sum(t => t.Progress * Math.Max(0, t.TotalBytes));
            var fraction = total > 0 ? Math.Min(1, done / total) : 0;
            var percent = (int)Math.Floor(fraction * 100);
            if (mine.FirstOrDefault(t => t.State == "running") is { } running)
                return new(running.Direction == "send" ? T($"Sendet … {percent} %", $"Sending … {percent}%") : T($"Empfängt … {percent} %", $"Receiving … {percent}%"), fraction, true);
            var first = mine[0];
            return first.State switch
            {
                "paused" => new(T("Angehalten", "Paused"), fraction, false),
                "offered" => first.Direction == "send" ? new(T("Wartet auf Annahme", "Waiting for approval"), null, true) : new(T("Möchte senden", "Wants to send"), null, false),
                _ => new(device.Online ? T("Wartet …", "Waiting …") : T("Wartet auf Verbindung", "Waiting to connect"), fraction > 0 ? fraction : null, true)
            };
        }
        return new(device.Online ? T("Bereit", "Ready") : T("Nicht erreichbar", "Not reachable"), null, false);
    }

    /// <summary>"Notizen.pdf", "Fotos" for a folder, or "a.txt und 2 weitere".</summary>
    public static string Describe(Transfer transfer, bool english)
    {
        var name = transfer.Title.Split('/').Last();
        if (transfer.Files.Count == 0) return name;
        var first = transfer.Files[0];
        if (first.Directory)
        {
            var folder = first.Path + "/";
            return transfer.Files.Skip(1).All(f => f.Path.StartsWith(folder, StringComparison.Ordinal)) ? name : english ? name + " and more" : name + " und weitere";
        }
        var count = transfer.FileCount > 0 ? transfer.FileCount : transfer.Files.Count;
        return count <= 1 ? name : english ? $"{name} and {count - 1} more" : $"{name} und {count - 1} weitere";
    }

    /// <summary>Active transfers first, then the newest finished ones.</summary>
    public static IReadOnlyList<Transfer> Recent(IEnumerable<Transfer> transfers, int limit = 5)
    {
        var all = transfers.ToList();
        var active = all.Where(t => t.IsActive).OrderByDescending(t => t.UpdatedAt, StringComparer.Ordinal).ThenByDescending(t => t.Id, StringComparer.Ordinal).ToList();
        var done = all.Where(t => !t.IsActive).OrderByDescending(t => t.UpdatedAt, StringComparer.Ordinal).ThenByDescending(t => t.Id, StringComparer.Ordinal);
        return [.. active, .. done.Take(Math.Max(0, limit - active.Count))];
    }

    public static string RelativeTime(string iso, bool english)
    {
        if (!DateTimeOffset.TryParse(iso, CultureInfo.InvariantCulture, DateTimeStyles.AssumeUniversal, out var when)) return "";
        var seconds = Math.Max(0, (DateTimeOffset.UtcNow - when).TotalSeconds);
        if (seconds < 60) return english ? "just now" : "gerade eben";
        if (seconds < 3600) return english ? $"{(int)(seconds / 60)} min ago" : $"vor {(int)(seconds / 60)} Min.";
        if (seconds < 86400) return english ? $"{(int)(seconds / 3600)} h ago" : $"vor {(int)(seconds / 3600)} Std.";
        return when.ToLocalTime().ToString("d", CultureInfo.GetCultureInfo(english ? "en-US" : "de-DE"));
    }

    /// <summary>Segoe Fluent Icons glyph for a computer.</summary>
    public static string Glyph(string? platform, string? form) => form == "laptop" ? "" : "";
}
