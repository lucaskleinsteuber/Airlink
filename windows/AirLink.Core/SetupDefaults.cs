using System.Text;

namespace AirLink.Core;

public static class SetupDefaults
{
    public static string SuggestReceiveDirectory(string? saved, string downloads)
    {
        if (string.IsNullOrWhiteSpace(saved)) return downloads;
        var comparer = OperatingSystem.IsWindows() ? StringComparer.OrdinalIgnoreCase : StringComparer.Ordinal;
        var current = Path.TrimEndingDirectorySeparator(Path.GetFullPath(saved));
        var legacy = Path.Combine(Path.GetFullPath(downloads), "AirLink");
        return comparer.Equals(current, legacy) ? downloads : saved;
    }

    public static string NormalizeName(string name)
    {
        name = name.Trim();
        if (name.Length == 0 || name.Any(char.IsControl) || new UTF8Encoding(false, true).GetByteCount(name) > 120)
            throw new ArgumentException("Use a device name between 1 and 120 UTF-8 bytes, without control characters.");
        return name;
    }
}
