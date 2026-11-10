using System.Text.RegularExpressions;
namespace AirLink.Core;

public sealed class PairingSession {
 public string Id { get; set; } = "";
 public string PeerId { get; set; } = "";
 public string PeerName { get; set; } = "";
 public string State { get; set; } = "";
 public string? ComparisonCode { get; set; }
 public string ExpiresAt { get; set; } = "";
 public bool LocalConfirmed { get; set; }
 public bool RemoteConfirmed { get; set; }
 public bool AutomaticReceive { get; set; }
 public string? Error { get; set; }
 public bool CanConfirm => State == "comparing" && !LocalConfirmed && Regex.IsMatch(ComparisonCode ?? "", @"^[0-9]{8}$");
 public string FormattedCode => Regex.IsMatch(ComparisonCode ?? "", @"^[0-9]{8}$") ? ComparisonCode![..4] + " " + ComparisonCode[4..] : "";
}
public sealed class Preparation {
 public string Id { get; set; } = "";
 public string State { get; set; } = "";
 public string? TransferId { get; set; }
 public string? Error { get; set; }
}
public sealed class TransferPage {
 public List<Transfer> Transfers { get; set; } = [];
 public string? NextCursor { get; set; }
}
public sealed class TransferFilePage {
 public List<TransferFile> Files { get; set; } = [];
 public int Total { get; set; }
 public int NextOffset { get; set; } = -1;
}
public static class DesktopTransferSelection {
 public static IReadOnlyList<Transfer> ForDevice(IEnumerable<Transfer> transfers, string? deviceId) {
  var matching = transfers.Where(t => t.DeviceId == deviceId).ToArray();
  return matching.Where(t => t.IsActive).OrderByDescending(t => t.CreatedAt).Concat(NativeExperience.RecentTransfers(matching, 3)).ToArray();
 }
 public static List<Transfer> MergeHistory(IEnumerable<Transfer> history, IEnumerable<Transfer> summary) {
  var rows = history.ToDictionary(t => t.Id);
  foreach (var item in summary) if ((item.IsActive || rows.ContainsKey(item.Id)) && (!rows.TryGetValue(item.Id,out var known) || string.CompareOrdinal(TimestampKey(item.UpdatedAt),TimestampKey(known.UpdatedAt)) >= 0)) rows[item.Id] = item;
  return rows.Values.OrderByDescending(t => t.IsActive).ThenByDescending(t => t.CreatedAt).ThenBy(t => t.Id, StringComparer.Ordinal).ToList();
 }
 private static string TimestampKey(string raw) => raw.EndsWith('Z') && raw.Length >= 20 && raw[19] is '.' or 'Z' ? raw[..19] + (raw[19] == '.' ? raw[20..^1] : "").PadRight(9,'0')[..9] : raw;
 public static TimeSpan? Remaining(Transfer transfer) {
  if (transfer.State != "running" || !double.IsFinite(transfer.BytesPerSecond) || transfer.BytesPerSecond <= 0) return null;
  var seconds = Math.Ceiling(Math.Max(0, (double)transfer.TotalBytes - transfer.CompletedBytes - transfer.SkippedBytes) / transfer.BytesPerSecond);
  return seconds is >= 0 and <= 31536000 ? TimeSpan.FromSeconds(seconds) : null;
 }
}
public static class UpdateCheckSchedule {
 public static bool IsDue(bool enabled, DateTimeOffset? last, DateTimeOffset now) => enabled && (last is null || now - last >= TimeSpan.FromHours(24));
}
