using System.Security.Cryptography;

namespace AirLink.Core;

public static class HelperExtractor
{
    /// <summary>Materializes only the app's verified embedded helper; never user transfer content.</summary>
    public static async Task<string> ExtractVerifiedAsync(Func<Stream> openResource, string cacheRoot, string expectedSha256, CancellationToken ct = default)
    {
        ArgumentNullException.ThrowIfNull(openResource);
        if (!Path.IsPathFullyQualified(cacheRoot)) throw new ArgumentException("The private helper cache must use an absolute path.", nameof(cacheRoot));
        if (expectedSha256.Length != 64 || expectedSha256.Any(c => !Uri.IsHexDigit(c))) throw new ArgumentException("A complete helper SHA-256 is required.", nameof(expectedSha256));
        var expected = expectedSha256.ToLowerInvariant();
        Directory.CreateDirectory(cacheRoot); RejectLink(cacheRoot);
        var directory = Path.Combine(cacheRoot, expected);
        Directory.CreateDirectory(directory); RejectLink(directory);
        var destination = Path.Combine(directory, "airlink.exe");
        if (File.Exists(destination))
        {
            RejectLink(destination);
            if (await MatchesAsync(destination, expected, ct).ConfigureAwait(false)) return destination;
        }
        var temporary = Path.Combine(directory, ".helper-" + Guid.NewGuid().ToString("N") + ".tmp");
        try
        {
            using var hash = IncrementalHash.CreateHash(HashAlgorithmName.SHA256);
            await using (var input = openResource())
            await using (var output = new FileStream(temporary, FileMode.CreateNew, FileAccess.Write, FileShare.None, 64 * 1024, FileOptions.Asynchronous | FileOptions.SequentialScan))
            {
                var buffer = new byte[64 * 1024];
                while (true)
                {
                    var count = await input.ReadAsync(buffer, ct).ConfigureAwait(false);
                    if (count == 0) break;
                    hash.AppendData(buffer.AsSpan(0, count));
                    await output.WriteAsync(buffer.AsMemory(0, count), ct).ConfigureAwait(false);
                }
                await output.FlushAsync(ct).ConfigureAwait(false);
                output.Flush(flushToDisk: true);
            }
            if (!Convert.ToHexString(hash.GetHashAndReset()).Equals(expected, StringComparison.OrdinalIgnoreCase)) throw new DaemonException("The bundled AirLink service failed its integrity check. Download the complete app again.");
            ct.ThrowIfCancellationRequested();
            RejectLink(cacheRoot); RejectLink(directory);
            if (File.Exists(destination)) RejectLink(destination);
            try { File.Move(temporary, destination, overwrite: true); }
            catch (Exception error) when ((error is IOException or UnauthorizedAccessException) && File.Exists(destination))
            {
                // Another launch may have installed the same immutable hash.
                // Reuse only a fully verified winner, never a partial file.
                RejectLink(destination);
                if (!await MatchesAsync(destination, expected, ct).ConfigureAwait(false)) throw;
            }
            RejectLink(destination);
            if (!await MatchesAsync(destination, expected, ct).ConfigureAwait(false)) throw new DaemonException("The AirLink service cache changed during extraction. Retry the app.");
            return destination;
        }
        finally { try { File.Delete(temporary); } catch (Exception error) when (error is IOException or UnauthorizedAccessException) { } }
    }

    private static void RejectLink(string path)
    {
        if ((File.GetAttributes(path) & FileAttributes.ReparsePoint) != 0) throw new DaemonException("The private AirLink service cache contains an unexpected link.");
    }

    private static async Task<bool> MatchesAsync(string path, string expected, CancellationToken ct)
    {
        await using var file = new FileStream(path, FileMode.Open, FileAccess.Read, FileShare.Read | FileShare.Delete, 64 * 1024, FileOptions.Asynchronous | FileOptions.SequentialScan);
        var actual = await SHA256.HashDataAsync(file, ct).ConfigureAwait(false);
        return Convert.ToHexString(actual).Equals(expected, StringComparison.OrdinalIgnoreCase);
    }
}
