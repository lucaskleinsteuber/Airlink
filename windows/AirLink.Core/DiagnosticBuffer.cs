using System.Text;
using System.Text.RegularExpressions;

namespace AirLink.Core;

internal sealed partial class DiagnosticBuffer
{
    private const int MaximumBytes = 8192;
    private readonly object sync = new();
    private readonly StringBuilder text = new();
    private bool privateKey;

    public void Append(string line)
    {
        lock (sync)
        {
            if (line.Contains("BEGIN", StringComparison.Ordinal) && line.Contains("PRIVATE KEY", StringComparison.Ordinal))
            {
                privateKey = true;
                text.AppendLine("[Private key omitted]");
            }
            else if (privateKey)
            {
                if (line.Contains("END", StringComparison.Ordinal) && line.Contains("PRIVATE KEY", StringComparison.Ordinal)) privateKey = false;
            }
            else text.AppendLine(Credentials().Replace(Bearer().Replace(line, "Bearer [redacted]"), "$1[redacted]"));
            while (Encoding.UTF8.GetByteCount(text.ToString()) > MaximumBytes && text.Length > 0)
            {
                var newline = text.ToString().IndexOf('\n');
                text.Remove(0, newline >= 0 ? newline + 1 : 1);
            }
        }
    }

    public string Tail { get { lock (sync) return text.ToString(); } }

    [GeneratedRegex("(?i)(\\\"?(?:password|token|pollToken|outboundToken|secret|authorization)\\\"?\\s*[:=]\\s*)(?:\\\"[^\\\"]*\\\"|\\S+)")]
    private static partial Regex Credentials();
    [GeneratedRegex(@"(?i)\bBearer\s+\S+")]
    private static partial Regex Bearer();
}
