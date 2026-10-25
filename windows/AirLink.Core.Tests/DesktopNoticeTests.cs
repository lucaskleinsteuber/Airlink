using AirLink.Core;

internal static class DesktopNoticeTests
{
    public static Task RecentResults()
    {
        var items = Enumerable.Range(0, 24).Select(i => new Transfer { Id = "recent" + i, State = "completed", CreatedAt = "2026-10-25T08:00:00Z", UpdatedAt = "2026-10-25T09:00:00Z" }).ToList();
        items.Add(new() { Id = "resumed-old", State = "completed", CreatedAt = "2025-01-01T00:00:00Z", UpdatedAt = "2026-10-25T10:00:00Z" });
        items.Add(new() { Id = "running", State = "running", UpdatedAt = "2026-10-25T11:00:00Z" });
        var recent = NativeExperience.RecentTransfers(items);
        if (recent.Count != 20 || recent[0].Id != "resumed-old" || recent.Any(t => t.IsActive)) throw new InvalidOperationException("A just-completed old transfer disappeared behind older results");
        return Task.CompletedTask;
    }
    public static Task Transitions()
    {
        static Transfer Receipt(string id, string state) => new() { Id = id, Direction = "receive", State = state, Files = [new() { Id = "file", Path = "file.txt", Completed = state == "completed" }] };
        static void Check(bool valid, string message) { if (!valid) throw new InvalidOperationException(message); }
        var tracker = new DesktopNoticeTracker();
        Check(tracker.Observe(new() { Transfers = [Receipt("historical", "completed")] }).Count == 0, "Startup replayed an old completed receive");
        var fast = tracker.Observe(new() { Transfers = [Receipt("historical", "completed"), Receipt("fast", "completed")] });
        Check(fast.Count == 1 && fast[0].Kind == DesktopNoticeKind.CompletedTransfer && fast[0].Id == "fast", "Fast auto-accepted completion was lost between polls");
        Check(tracker.Observe(new() { Transfers = [Receipt("fast", "completed")] }).Count == 0, "Completed receipt repeated");
        var attention = new HostStatus { PairRequests = [new() { Id = "pair", Name = "Mac" }], Transfers = [Receipt("offer", "offered")], Clipboard = [new() { Id = "text", Direction = "receive", Text = "Private message never included in notice" }] };
        var requests = tracker.Observe(attention);
        Check(requests.Select(n => n.Kind).ToHashSet().SetEquals([DesktopNoticeKind.Pairing, DesktopNoticeKind.IncomingTransfer, DesktopNoticeKind.Clipboard]), "A waiting action was not announced");
        Check(requests.All(n => !n.Title.Contains("Private message")), "Clipboard content leaked into notification");
        Check(tracker.Observe(attention).Count == 0, "Repeated status generated repeated attention");
        var duplicate = Receipt("duplicate", "waitingDuplicate"); duplicate.Files[0].DuplicateState = "pending";
        Check(tracker.Observe(new() { Transfers = [duplicate] }).Single().Kind == DesktopNoticeKind.DuplicateDecision, "Missing duplicate decision attention");
        Check(tracker.Observe(new() { Transfers = [duplicate] }).Count == 0, "Duplicate decision repeated");
        var silent = new HostStatus { Settings = new() { Notifications = false }, Transfers = [Receipt("silent", "completed")] };
        Check(tracker.Observe(silent).Count == 0, "Disabled notifications still emitted");
        silent.Settings.Notifications = true;
        Check(tracker.Observe(silent).Count == 0, "Enabling notifications replayed a silent completion");
        var skipped = Receipt("skipped", "completed"); skipped.Files[0].Skipped = true;
        Check(tracker.Observe(new() { Transfers = [skipped] }).Count == 0, "All-skipped payload offered a nonexistent receipt");
        return Task.CompletedTask;
    }
}
