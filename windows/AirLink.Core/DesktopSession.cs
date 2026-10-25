using System.Diagnostics;
using System.Text.Json;

namespace AirLink.Core;

/// <summary>Local version hint for an already running desktop window, never an authorization credential.</summary>
public sealed class DesktopSession : IDisposable
{
    private sealed record Owner(int ProcessId, string Version);
    private readonly string path;
    private readonly Owner owner;
    private DesktopSession(string path, string version) { this.path = path; owner = new(Environment.ProcessId, version); }
    public static DesktopSession? Register(string path, string version)
    {
        try
        {
            Directory.CreateDirectory(Path.GetDirectoryName(path)!);
            var session = new DesktopSession(path, version);
            File.WriteAllText(path, JsonSerializer.Serialize(session.owner));
            return session;
        }
        catch (IOException) { return null; }
        catch (UnauthorizedAccessException) { return null; }
    }
    public static bool SameVersionRunning(string path, string version)
    {
        try
        {
            if (new FileInfo(path).Length > 4096) return false;
            var owner = JsonSerializer.Deserialize<Owner>(File.ReadAllText(path));
            if (owner is null || owner.Version != version) return false;
            using var process = Process.GetProcessById(owner.ProcessId);
            return !process.HasExited;
        }
        catch (IOException) { return false; }
        catch (UnauthorizedAccessException) { return false; }
        catch (JsonException) { return false; }
        catch (ArgumentException) { return false; }
        catch (System.ComponentModel.Win32Exception) { return false; }
    }
    public void Dispose()
    {
        try
        {
            if (JsonSerializer.Deserialize<Owner>(File.ReadAllText(path)) == owner) File.Delete(path);
        }
        catch (IOException) { }
        catch (UnauthorizedAccessException) { }
        catch (JsonException) { }
    }
}
