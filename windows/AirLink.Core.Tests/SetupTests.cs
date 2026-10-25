using System.Text.Json;
using AirLink.Core;

internal static class SetupTests
{
    public static Task ExplicitDefaults()
    {
        var old = JsonSerializer.Deserialize<HostSettings>("{}", ProtocolJson.Options)!;
        if (old.SetupCompleted) throw new InvalidOperationException("Legacy settings skipped user setup");
        old.SetupCompleted = true; old.Name = SetupDefaults.NormalizeName("  Laptop Windows  ");
        if (!ProtocolJson.Clone(old).SetupCompleted || old.Name != "Laptop Windows") throw new InvalidOperationException("Confirmed setup did not survive settings roundtrip");
        var downloads = Path.Combine(Path.GetTempPath(), "Downloads");
        var custom = Path.Combine(Path.GetTempPath(), "Custom", "AirLink");
        if (SetupDefaults.SuggestReceiveDirectory(Path.Combine(downloads, "AirLink"), downloads) != downloads || SetupDefaults.SuggestReceiveDirectory(custom, downloads) != custom) throw new InvalidOperationException("Setup changed a custom destination or retained legacy nested default");
        foreach (var invalid in new[] { "\n\t", "Laptop\nWindows", string.Concat(Enumerable.Repeat("😀", 31)) })
        {
            try { SetupDefaults.NormalizeName(invalid); } catch (ArgumentException) { continue; }
            throw new InvalidOperationException("Invalid setup name accepted");
        }
        var root = Path.Combine(Path.GetTempPath(), "AirLink-session-test-" + Guid.NewGuid().ToString("N"));
        var sessionPath = Path.Combine(root, "session.json");
        try
        {
            using (DesktopSession.Register(sessionPath, "0.8.1.0"))
            {
                if (!DesktopSession.SameVersionRunning(sessionPath, "0.8.1.0") || DesktopSession.SameVersionRunning(sessionPath, "0.7.0.0")) throw new InvalidOperationException("Existing-version launch was not distinguished");
            }
            if (DesktopSession.SameVersionRunning(sessionPath, "0.8.1.0")) throw new InvalidOperationException("Exited desktop session still appears active");
        }
        finally { if (Directory.Exists(root)) Directory.Delete(root, true); }
        return Task.CompletedTask;
    }
}
