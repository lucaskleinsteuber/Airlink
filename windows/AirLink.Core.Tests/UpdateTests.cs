using System.Net;
using System.Diagnostics;
using System.Runtime.InteropServices;
using System.Security.Cryptography;
using System.Text;
using AirLink.Core;
using NetSparkleUpdater;
using NetSparkleUpdater.AppCastHandlers;
using NetSparkleUpdater.Enums;

internal static class UpdateTests
{
    public static int VerifyRelease(string architecture, string feedPath, string payloadPath)
    {
        try
        {
            var native = architecture switch { "x64" => Architecture.X64, "arm64" => Architecture.Arm64, _ => throw new InvalidDataException("Unsupported fixture architecture") };
            var channel = WindowsUpdateChannel.Load(native);
            var verifier = new StrictUpdateSignatures(channel.PublicKey);
            var bytes = File.ReadAllBytes(feedPath);
            Require(!bytes.AsSpan().StartsWith(new byte[] { 0xef, 0xbb, 0xbf }), "The feed byte contract forbids a BOM");
            Require(verifier.VerifySignature(File.ReadAllText(feedPath + ".signature").Trim(), bytes) == ValidationResult.Valid, "The production-key feed signature is invalid");
            var parsed = new XMLAppCastGenerator().DeserializeAppCast(new UTF8Encoding(false, true).GetString(bytes));
            Require(parsed.Items.Count == 1, "Exactly one signed release item is required");
            var item = parsed.Items[0];
            Require(new WindowsUpdateFilter(architecture).Accepts(item), "The signed release item is incompatible with this native channel");
            Require(new FileInfo(payloadPath).Length == item.UpdateSize, "The signed installer length differs");
            Require(verifier.VerifySignatureOfFile(item.DownloadSignature ?? "", payloadPath) == ValidationResult.Valid, "The production-key installer signature is invalid");
            Console.WriteLine("PASS actual NetSparkle production-key feed and payload verification for windows-" + architecture);
            return 0;
        }
        catch (Exception e)
        {
            Console.Error.WriteLine("FAIL signed update release verification (" + e.GetType().Name + ")");
            return 1;
        }
    }

    private static void Require(bool condition, string message) { if (!condition) throw new InvalidOperationException(message); }
    private static (string PublicKey, byte[] PrivateKey) EphemeralKey()
    {
        Chaos.NaCl.Ed25519.KeyPairFromSeed(out var publicKey, out var privateKey, RandomNumberGenerator.GetBytes(32));
        return (Convert.ToBase64String(publicKey), privateKey);
    }
    private static string Sign(byte[] bytes, byte[] key) => Convert.ToBase64String(Chaos.NaCl.Ed25519.Sign(bytes, key));

    public static Task NativeChannels()
    {
        var x64 = WindowsUpdateChannel.Load(Architecture.X64);
        var arm = WindowsUpdateChannel.Load(Architecture.Arm64);
        Require(x64.Architecture == "x64" && arm.Architecture == "arm64" && x64.Feed != arm.Feed && x64.PublicKey == arm.PublicKey, "Native update channels or common signing key differ");
        try { WindowsUpdateChannel.Load(Architecture.X86); throw new InvalidOperationException("An unsupported native architecture received an update channel"); }
        catch (PlatformNotSupportedException) { }
        var filter = new WindowsUpdateFilter("x64");
        AppCastItem Item(string version, string arch) => new() { Version = version, OperatingSystem = "windows", UpdateSize = 1024, DownloadSignature = Convert.ToBase64String(new byte[64]), DownloadLink = "https://github.com/lucaskleinsteuber/Airlink/releases/download/v" + version + "/AirLink-" + version + "-windows-" + arch + "-setup.exe" };
        var latest = Item("0.11.0", "x64");
        var candidates = filter.GetFilteredAppCastItems(SemVerLike.Parse("0.9.0"), [Item("0.8.1", "x64"), Item("0.12.0", "arm64"), Item("0.10.0", "x64"), latest]).ToArray();
        Require(candidates.Length == 1 && ReferenceEquals(candidates[0], latest), "Update selection accepted a downgrade, another architecture or the wrong newest release");
        var beta = Item("0.12.0-beta.1", "x64"); beta.Version = "0.12.0.14"; beta.ShortVersion = "0.12.0-beta.1";
        Require(!new WindowsUpdateFilter("x64", "0.12.0.14").GetFilteredAppCastItems(SemVerLike.Parse("0.12.0-beta.1"), [beta]).Any(), "The installed beta was offered again when the library reported the informational version");
        Require(filter.Accepts(beta), "A signed native beta with numeric OS version was rejected");
        var beta10 = Item("0.12.0-beta.10", "x64"); beta10.Version = "0.12.0.23"; beta10.ShortVersion = "0.12.0-beta.10";
        var beta2 = Item("0.12.0-beta.2", "x64"); beta2.Version = "0.12.0.15"; beta2.ShortVersion = "0.12.0-beta.2";
        var ordered = filter.GetFilteredAppCastItems(SemVerLike.Parse("0.12.0.14"), [beta, beta2, beta10]).Single();
        Require(ReferenceEquals(ordered, beta10), "Beta ordering used text ordering or offered the installed beta");
        var stable = Item("0.12.0", "x64"); stable.Version = "0.12.0.24"; stable.ShortVersion = "0.12.0";
        Require(filter.Accepts(stable) && ReferenceEquals(filter.GetFilteredAppCastItems(SemVerLike.Parse("0.12.0.23"), [beta10, stable]).Single(), stable), "The final version did not replace the last beta");
        beta.Version = "0.13.0.14"; Require(!filter.Accepts(beta), "The numeric version was not bound to the displayed product version");
        latest.DownloadLink = latest.DownloadLink!.Replace("https://", "http://");
        Require(!filter.Accepts(latest), "An unsigned transport URL was accepted");
        latest = Item("0.11.0", "x64"); latest.DownloadSignature = "";
        Require(!filter.Accepts(latest), "An unsigned payload was offered");
        return Task.CompletedTask;
    }

    public static async Task InstallerHandoff()
    {
        var root = Path.Combine(Path.GetTempPath(), "AirLink updater ! % & Ω " + Guid.NewGuid().ToString("N"));
        Directory.CreateDirectory(root);
        var script = Path.Combine(root, Guid.NewGuid().ToString() + ".cmd");
        var harmless = Path.Combine(root, "harmless ! % & Ω.cmd");
        var marker = Path.Combine(root, "marker ! % & Ω.txt");
        try
        {
            File.WriteAllText(script, "placeholder");
            // This harmless background fixture is the only executable command in
            // the handoff test. The AirLink app and Setup are never launched.
            File.WriteAllText(harmless, "@echo off\r\nsetlocal DisableDelayedExpansion\r\necho done>\"" + marker.Replace("%", "%%") + "\"\r\nexit /b 0\r\n", new UTF8Encoding(false));
            var system = OperatingSystem.IsWindows() ? Environment.SystemDirectory : Path.Combine(root, "system");
            var process = new ProcessStartInfo(script);
            WindowsUpdateInstaller.Prepare(process, harmless, int.MaxValue, system);
            Require(process.FileName == Path.Combine(system, "cmd.exe") && !process.UseShellExecute && process.CreateNoWindow, "The waiter was passed directly to CreateProcess or could open a console");
            Require(process.Environment[WindowsUpdateInstaller.WaiterEnvironmentVariable] == script && !process.Arguments.Contains(script, StringComparison.Ordinal), "A special-character script path was placed directly in the shell template");
            var content = File.ReadAllText(script);
            Require(content.Contains("DisableDelayedExpansion") && !content.Contains("ENABLEDELAYEDEXPANSION", StringComparison.OrdinalIgnoreCase) && content.Contains("%%") && !content.Contains("taskkill", StringComparison.OrdinalIgnoreCase), "The waiter expands literal paths or could terminate the app");
            try { WindowsUpdateInstaller.BuildWaiter("bad\"path.exe", 1, system); throw new InvalidOperationException("A command-delimiter path was accepted"); }
            catch (InvalidDataException) { }
            if (OperatingSystem.IsWindows())
            {
                using var running = Process.Start(process) ?? throw new IOException("The harmless waiter did not start");
                await running.WaitForExitAsync().WaitAsync(TimeSpan.FromSeconds(15));
                Require(running.ExitCode == 0 && File.Exists(marker), "The actual background Windows waiter did not preserve literal special-character paths");
            }
        }
        finally { Directory.Delete(root, recursive: true); }
    }

    public static Task SignatureIntegrity()
    {
        var key = EphemeralKey();
        var bytes = RandomNumberGenerator.GetBytes(2 * 1024 * 1024 + 13);
        var signature = Sign(bytes, key.PrivateKey);
        var verifier = new StrictUpdateSignatures(key.PublicKey);
        Require(verifier.VerifySignature(signature, bytes) == ValidationResult.Valid, "A standard Ed25519 signature was not accepted by the actual NetSparkle verifier");
        bytes[0] ^= 1;
        Require(verifier.VerifySignature(signature, bytes) == ValidationResult.Invalid, "Tampered bytes were accepted");
        bytes[0] ^= 1;
        Require(verifier.VerifySignature(signature, bytes) == ValidationResult.Valid, "A prior invalid verification contaminated later checks");
        foreach (var invalid in new[] { "", "not-base64", Convert.ToBase64String(new byte[63]) }) Require(verifier.VerifySignature(invalid, bytes) == ValidationResult.Invalid, "A missing or malformed signature was accepted");
        Require(new StrictUpdateSignatures(EphemeralKey().PublicKey).VerifySignature(signature, bytes) == ValidationResult.Invalid, "Another signing key was accepted");
        try { verifier.SecurityMode = SecurityMode.Unsafe; throw new InvalidOperationException("Signature policy was downgraded"); }
        catch (InvalidOperationException e) when (e.Message == "Update signatures cannot be disabled.") { }
        var path = Path.Combine(Path.GetTempPath(), "AirLink-update-signature-" + Guid.NewGuid().ToString("N"));
        try
        {
            File.WriteAllBytes(path, bytes);
            Require(verifier.VerifySignatureOfFile(signature, path) == ValidationResult.Valid, "A signed file failed chunked verification");
            using (var file = File.OpenWrite(path)) file.SetLength(bytes.Length - 1);
            Require(verifier.VerifySignatureOfFile(signature, path) == ValidationResult.Invalid, "A truncated installer was accepted");
        }
        finally { File.Delete(path); CryptographicOperations.ZeroMemory(key.PrivateKey); }
        return Task.CompletedTask;
    }

    public static async Task SignedFeed()
    {
        var key = EphemeralKey();
        var channel = WindowsUpdateChannel.Load(Architecture.X64);
        var xml = Encoding.UTF8.GetBytes("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<rss version=\"2.0\"><channel><title>AirLink</title></channel></rss>\n");
        var signature = Encoding.UTF8.GetBytes(Sign(xml, key.PrivateKey) + "\n");
        var handler = new FeedHandler(xml, signature);
        using var downloader = new WindowsUpdateFeed(channel.Feed, handler);
        var verifier = new StrictUpdateSignatures(key.PublicKey);
        var helper = new AppCastHelper();
        helper.SetupAppCastHelper(downloader, channel.Feed.AbsoluteUri, "0.9.0", verifier, null);
        Require(await helper.DownloadAppCast() == Encoding.UTF8.GetString(xml), "The actual NetSparkle helper rejected a valid detached feed signature");
        handler.Body = Encoding.UTF8.GetBytes(Encoding.UTF8.GetString(xml).Replace("AirLink", "Changed"));
        Require(await helper.DownloadAppCast() is null && verifier.RejectedSignature, "A tampered feed was not rejected before parsing");
        handler.Status = HttpStatusCode.NotFound;
        Require(await helper.DownloadAppCast() is null && downloader.Failure == UpdateFeedFailure.Unavailable, "A private or missing feed was treated as usable update metadata");
        var requests = handler.Requests;
        try { await downloader.DownloadAndGetAppCastDataAsync(channel.Feed.AbsoluteUri + ".dsa"); throw new InvalidOperationException("A legacy signature fallback was fetched"); }
        catch (InvalidDataException) { }
        Require(handler.Requests == requests, "A rejected legacy URL performed a network request");
        handler.Status = HttpStatusCode.OK; handler.Body = new byte[1024 * 1024 + 1];
        try { await downloader.DownloadAndGetAppCastDataAsync(channel.Feed.AbsoluteUri); throw new InvalidOperationException("Oversized metadata was accepted"); }
        catch (InvalidDataException) { }
        handler.Body = [0xef, 0xbb, 0xbf, 0x61];
        try { await downloader.DownloadAndGetAppCastDataAsync(channel.Feed.AbsoluteUri); throw new InvalidOperationException("BOM metadata changed the signed-byte contract"); }
        catch (InvalidDataException) { }
        CryptographicOperations.ZeroMemory(key.PrivateKey);
    }

    private sealed class FeedHandler(byte[] body, byte[] signature) : HttpMessageHandler
    {
        public byte[] Body { get; set; } = body;
        public HttpStatusCode Status { get; set; } = HttpStatusCode.OK;
        public int Requests { get; private set; }
        protected override Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken cancellationToken)
        {
            Requests++;
            Require(request.Headers.Authorization is null && !request.Headers.Contains("Cookie"), "A public update request sent authentication or cookies");
            Require(request.RequestUri?.Scheme == "https", "Update metadata used insecure transport");
            return Task.FromResult(new HttpResponseMessage(Status) { Content = new ByteArrayContent(request.RequestUri!.AbsoluteUri.EndsWith(".signature", StringComparison.Ordinal) ? signature : Body) });
        }
    }
}
