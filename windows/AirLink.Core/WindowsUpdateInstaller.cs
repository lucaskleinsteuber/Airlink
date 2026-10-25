using System.Diagnostics;
using System.Text;

namespace AirLink.Core;

/// <summary>Adapts NetSparkle's waiter to Windows CreateProcess and literal user paths, without terminating the desktop.</summary>
public static class WindowsUpdateInstaller
{
    public const string WaiterEnvironmentVariable = "AIRLINK_UPDATE_WAITER";

    public static void Prepare(ProcessStartInfo process, string installerPath, int parentProcessId, string systemDirectory)
    {
        var script = Path.GetFullPath(process.FileName);
        if (!script.EndsWith(".cmd", StringComparison.OrdinalIgnoreCase) || !Guid.TryParse(Path.GetFileNameWithoutExtension(script), out _) || !File.Exists(script))
            throw new InvalidDataException("Unexpected update waiter path.");
        ValidatePath(script);
        var contents = BuildWaiter(installerPath, parentProcessId, systemDirectory);
        File.WriteAllText(script, contents, new UTF8Encoding(false));
        process.FileName = Path.Combine(systemDirectory, "cmd.exe");
        process.ArgumentList.Clear();
        // Expand the environment variable exactly once; percent/bang characters
        // inside its value are literal, rather than a second command template.
        process.Environment[WaiterEnvironmentVariable] = script;
        process.Arguments = "/d /v:off /s /c \"\"%" + WaiterEnvironmentVariable + "%\"\"";
        process.UseShellExecute = false;
        process.CreateNoWindow = true;
        process.WindowStyle = ProcessWindowStyle.Hidden;
    }

    public static string BuildWaiter(string installerPath, int parentProcessId, string systemDirectory)
    {
        if (parentProcessId <= 0 || string.IsNullOrWhiteSpace(systemDirectory)) throw new InvalidDataException("Invalid update waiter configuration.");
        static string Literal(string path) { ValidatePath(path); return path.Replace("%", "%%", StringComparison.Ordinal); }
        var installer = Literal(installerPath);
        var tasklist = Literal(Path.Combine(systemDirectory, "tasklist.exe"));
        var findstr = Literal(Path.Combine(systemDirectory, "findstr.exe"));
        var ping = Literal(Path.Combine(systemDirectory, "ping.exe"));
        var chcp = Literal(Path.Combine(systemDirectory, "chcp.com"));
        // No parenthesized counter block or delayed expansion: ! and doubled %
        // remain literal even in profile/cache paths. Only a fixed numeric PID
        // enters the filter. Ping is loopback-only and supplies a background wait.
        return string.Join("\r\n", new[]
        {
            "@echo off", "setlocal DisableDelayedExpansion", "\"" + chcp + "\" 65001 >nul",
            "set airlink_update_wait_count=0", ":airlink_update_wait",
            "\"" + tasklist + "\" /fi \"PID eq " + parentProcessId + "\" /nh | \"" + findstr + "\" /r /c:\"\\<" + parentProcessId + "\\>\" >nul",
            "if errorlevel 1 goto airlink_update_install",
            "set /a airlink_update_wait_count+=1 >nul", "if %airlink_update_wait_count% GEQ 90 goto airlink_update_timeout",
            "\"" + ping + "\" -n 2 -w 1000 127.0.0.1 >nul", "goto airlink_update_wait",
            ":airlink_update_install", "\"" + installer + "\"", "set airlink_update_exit=%errorlevel%",
            "del \"%~f0\" >nul 2>&1", "endlocal & exit /b %airlink_update_exit%",
            ":airlink_update_timeout", "del \"%~f0\" >nul 2>&1", "endlocal & exit /b 1603", ""
        });
    }

    private static void ValidatePath(string path)
    {
        if (string.IsNullOrWhiteSpace(path) || path.Any(character => character == '"' || char.IsControl(character)))
            throw new InvalidDataException("An update path contains unsupported characters.");
    }
}
