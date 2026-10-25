namespace AirLink.Core;

public enum DesktopNoticeKind { Pairing, IncomingTransfer, DuplicateDecision, CompletedTransfer, Clipboard }
public sealed record DesktopNotice(DesktopNoticeKind Kind, string Id, string DeviceName, string Title);

/// <summary>Tracks observations, including when notifications are disabled, without replaying old receipts at launch.</summary>
public sealed class DesktopNoticeTracker
{
    private HostStatus? previous;
    private readonly HashSet<string> pairs = [];
    private readonly HashSet<string> completed = [];
    private readonly HashSet<string> clipboard = [];
    private readonly HashSet<string> decisions = [];

    public IReadOnlyList<DesktopNotice> Observe(HostStatus next)
    {
        List<DesktopNotice> notices = [];
        foreach (var request in next.PairRequests)
            if (pairs.Add(request.Id)) notices.Add(new(DesktopNoticeKind.Pairing, request.Id, request.Name, ""));
        foreach (var transfer in next.Transfers)
        {
            var prior = previous?.Transfers.FirstOrDefault(t => t.Id == transfer.Id);
            if (transfer.Direction == "receive" && transfer.State == "offered" && prior?.State != "offered")
                notices.Add(new(DesktopNoticeKind.IncomingTransfer, transfer.Id, transfer.DeviceName, transfer.Title));
            if (transfer.Direction == "receive" && transfer.State == "waitingDuplicate")
                foreach (var file in transfer.Files.Where(f => f.DuplicateState == "pending"))
                    if (decisions.Add(transfer.Id + ":" + file.Id)) notices.Add(new(DesktopNoticeKind.DuplicateDecision, transfer.Id, transfer.DeviceName, file.Path));
            if (transfer.Direction == "receive" && transfer.State == "completed" && completed.Add(transfer.Id) && previous is not null && NativeExperience.ReceivedDisplayEntries(transfer).Count > 0)
                notices.Add(new(DesktopNoticeKind.CompletedTransfer, transfer.Id, transfer.DeviceName, transfer.Title));
        }
        foreach (var message in next.Clipboard.Where(m => m.Direction == "receive"))
            if (clipboard.Add(message.Id) && previous is not null) notices.Add(new(DesktopNoticeKind.Clipboard, message.Id, message.DeviceName, ""));
        previous = next;
        return next.Settings.Notifications ? notices : [];
    }
}
