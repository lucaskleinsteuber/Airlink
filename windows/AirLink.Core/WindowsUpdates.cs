using System.Net;
using System.Reflection;
using System.Runtime.InteropServices;
using System.Text;
using System.Text.Json;
using System.Text.RegularExpressions;
using NetSparkleUpdater;
using NetSparkleUpdater.AppCastHandlers;
using NetSparkleUpdater.Enums;
using NetSparkleUpdater.Interfaces;
using NetSparkleUpdater.SignatureVerifiers;

namespace AirLink.Core;

public sealed record WindowsUpdateChannel(string Architecture, Uri Feed, string PublicKey)
{
    public static WindowsUpdateChannel Load(Architecture architecture)
    {
        using var stream = typeof(WindowsUpdateChannel).Assembly.GetManifestResourceStream("AirLink.Core.UpdateSigningConfiguration")
            ?? throw new InvalidDataException("The update signing configuration is missing.");
        return Read(stream, architecture);
    }

    public static WindowsUpdateChannel Read(Stream stream, Architecture architecture)
    {
        var name = architecture switch { System.Runtime.InteropServices.Architecture.X64 => "x64", System.Runtime.InteropServices.Architecture.Arm64 => "arm64", _ => throw new PlatformNotSupportedException("Updates require native x64 or ARM64 Windows.") };
        using var document = JsonDocument.Parse(stream);
        var root = document.RootElement;
        if (root.GetProperty("schemaVersion").GetInt32() != 1 || root.GetProperty("algorithm").GetString() != "Ed25519") throw new InvalidDataException("The update signing configuration is unsupported.");
        var key = root.GetProperty("publicKey").GetString() ?? "";
        if (!StrictUpdateSignatures.IsBase64(key, 32)) throw new InvalidDataException("The update verification key is invalid.");
        var expected = new Uri("https://raw.githubusercontent.com/lucasfd3/Airlink/main/updates/windows-" + name + ".xml");
        if (root.GetProperty("feeds").GetProperty("windows-" + name).GetString() != expected.AbsoluteUri) throw new InvalidDataException("The Windows update feed is unexpected.");
        return new(name, expected, key);
    }
}

/// <summary>Every operation gets a fresh upstream verifier; missing or malformed signatures always fail closed.</summary>
public sealed class StrictUpdateSignatures(string publicKey) : ISignatureVerifier
{
    public SecurityMode SecurityMode
    {
        get => SecurityMode.Strict;
        set { if (value != SecurityMode.Strict) throw new InvalidOperationException("Update signatures cannot be disabled."); }
    }
    public bool RejectedSignature { get; private set; }
    public bool HasValidKeyInformation() => IsBase64(publicKey, 32);
    public void ResetFailure() => RejectedSignature = false;
    public static bool IsBase64(string value, int bytes)
    {
        try { return !string.IsNullOrWhiteSpace(value) && Convert.FromBase64String(value).Length == bytes; }
        catch (FormatException) { return false; }
    }
    private ValidationResult Verify(string signature, Func<Ed25519Checker, ValidationResult> operation)
    {
        var result = ValidationResult.Invalid;
        if (HasValidKeyInformation() && IsBase64(signature, 64))
        {
            try { result = operation(new Ed25519Checker(SecurityMode.Strict, publicKey, publicKeyFile: null, readFileBeingVerifiedInChunks: true, chunkSize: 1024 * 1024)); }
            catch (Exception ex) when (ex is ArgumentException or FormatException or IOException or UnauthorizedAccessException) { }
        }
        if (result != ValidationResult.Valid) RejectedSignature = true;
        return result;
    }
    public ValidationResult VerifySignature(string signature, byte[] dataToVerify) => Verify(signature, checker => checker.VerifySignature(signature, dataToVerify));
    public ValidationResult VerifySignatureOfFile(string signature, string binaryPath) => Verify(signature, checker => checker.VerifySignatureOfFile(signature, binaryPath));
    public ValidationResult VerifySignatureOfString(string signature, string data) => VerifySignature(signature, Encoding.UTF8.GetBytes(data));
}

public enum UpdateFeedFailure { None, Unavailable, InvalidDocument }

/// <summary>Unauthenticated, bounded UTF-8 feed reads; no cookies, private-repository token, fallback signature or redirect.</summary>
public sealed class WindowsUpdateFeed : IAppCastDataDownloader, IDisposable
{
    private static readonly UTF8Encoding Encoding = new(false, true);
    private readonly HttpClient http;
    private readonly Uri feed;
    public UpdateFeedFailure Failure { get; private set; }
    public WindowsUpdateFeed(Uri feed, HttpMessageHandler? handler = null)
    {
        this.feed = feed;
        http = new HttpClient(handler ?? new HttpClientHandler { AllowAutoRedirect = false, UseCookies = false, UseDefaultCredentials = false }) { Timeout = TimeSpan.FromSeconds(20) };
    }
    public void ResetFailure() => Failure = UpdateFeedFailure.None;
    public Encoding GetAppCastEncoding() => Encoding;
    public string DownloadAndGetAppCastData(string url) => DownloadAndGetAppCastDataAsync(url).GetAwaiter().GetResult();
    public async Task<string> DownloadAndGetAppCastDataAsync(string url)
    {
        // AppCastHelper attempts a legacy .dsa fallback. It is deliberately not a publication input here.
        if (url != feed.AbsoluteUri && url != feed.AbsoluteUri + ".signature") throw new InvalidDataException("Unexpected update metadata URL.");
        try
        {
            using var timeout = new CancellationTokenSource(TimeSpan.FromSeconds(20));
            using var response = await http.GetAsync(url, HttpCompletionOption.ResponseHeadersRead, timeout.Token).ConfigureAwait(false);
            if (!response.IsSuccessStatusCode) throw new HttpRequestException("The signed update feed is unavailable.", null, response.StatusCode);
            var limit = url.EndsWith(".signature", StringComparison.Ordinal) ? 4096 : 1024 * 1024;
            if (response.Content.Headers.ContentLength > limit) throw new InvalidDataException("Update metadata exceeds its size limit.");
            await using var body = await response.Content.ReadAsStreamAsync(timeout.Token).ConfigureAwait(false);
            using var collected = new MemoryStream();
            var buffer = new byte[8192];
            int count;
            while ((count = await body.ReadAsync(buffer, timeout.Token).ConfigureAwait(false)) != 0)
            {
                if (collected.Length + count > limit) throw new InvalidDataException("Update metadata exceeds its size limit.");
                collected.Write(buffer, 0, count);
            }
            var bytes = collected.ToArray();
            if (bytes.AsSpan().StartsWith(new byte[] { 0xef, 0xbb, 0xbf })) throw new InvalidDataException("Update metadata must be UTF-8 without a byte-order mark.");
            return Encoding.GetString(bytes);
        }
        catch (Exception ex) when (ex is HttpRequestException or OperationCanceledException or IOException or DecoderFallbackException)
        {
            Failure = ex is InvalidDataException or DecoderFallbackException ? UpdateFeedFailure.InvalidDocument : UpdateFeedFailure.Unavailable;
            throw;
        }
    }
    public void Dispose() => http.Dispose();
}

public sealed class WindowsUpdateFilter(string architecture) : IAppCastFilter
{
    private static readonly Regex ReleaseVersion = new(@"^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$", RegexOptions.CultureInvariant);
    public AppCastItem? Selected { get; private set; }
    public IEnumerable<AppCastItem> GetFilteredAppCastItems(SemVerLike installed, IEnumerable<AppCastItem> items)
    {
        Selected = items.Where(item => Accepts(item) && item.SemVerLikeVersion.CompareTo(installed) > 0).OrderByDescending(item => item.SemVerLikeVersion).FirstOrDefault();
        return Selected is null ? [] : [Selected];
    }
    public bool Accepts(AppCastItem item)
    {
        if (architecture is not ("x64" or "arm64") || item.Version is null || !ReleaseVersion.IsMatch(item.Version) || !System.Version.TryParse(item.Version, out _)) return false;
        if (item.OperatingSystem != "windows" || item.UpdateSize <= 0 || item.UpdateSize > 1024L * 1024 * 1024 || !StrictUpdateSignatures.IsBase64(item.DownloadSignature ?? "", 64)) return false;
        var expected = "https://github.com/lucasfd3/Airlink/releases/download/v" + item.Version + "/AirLink-" + item.Version + "-windows-" + architecture + "-setup.exe";
        return item.DownloadLink == expected;
    }
}
