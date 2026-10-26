using AirLink.Core;

internal static class ExperienceTests
{
    private static void Check(bool condition, string message) { if (!condition) throw new InvalidOperationException(message); }

    public static Task StatusRefreshOrdering()
    {
        var gate = new StatusRefreshGate();
        var oldPoll = gate.Begin() ?? throw new InvalidOperationException("Initial poll did not start");
        Check(gate.Begin() is null, "Ordinary polling duplicated in-flight work");
        var afterRemoval = gate.Begin(force: true) ?? throw new InvalidOperationException("Post-removal status was skipped behind an old poll");
        Check(!gate.IsCurrent(oldPoll) && gate.IsCurrent(afterRemoval), "An older result could restore deleted history");
        gate.Complete(oldPoll);
        Check(gate.Begin() is null, "An older finally cleared the newer in-flight request");
        gate.Complete(afterRemoval);
        var later = gate.Begin() ?? throw new InvalidOperationException("Polling did not resume after fresh state");
        Check(!gate.IsCurrent(afterRemoval) && gate.IsCurrent(later), "A late previous error could replace the current state");
        gate.Invalidate();
        Check(!gate.IsCurrent(later) && gate.Begin() is not null, "Restart accepted an old helper's status");
        return Task.CompletedTask;
    }

    public static Task ComparisonIdentity()
    {
        var draft = new ConnectionDraft("https://peer.local:43871/", new string('a', 64));
        var verification = new ConnectionVerification();
        Check(!verification.Matches(draft) && !draft.AutomaticReceive, "Discovery implicitly verified a peer or enabled automatic receipt");
        verification.Confirm(new("https://peer.local", "invalid"));
        Check(!verification.Matches(new("https://peer.local", "invalid")), "An invalid fingerprint could be confirmed");
        verification.Confirm(draft);
        Check(verification.Matches(draft with { Address = " https://peer.local:43871 ", Fingerprint = string.Join(":", Enumerable.Repeat("AA", 32)) }), "Equivalent formatting lost a valid identity comparison");
        Check(draft.ComparisonFingerprint == string.Join(" ", Enumerable.Repeat("aaaaaaaa", 8)), "The comparison display omitted or altered fingerprint characters");
        Check(!verification.Matches(draft with { Address = "https://other.local:43871" }), "Address editing reused another peer's comparison");
        Check(!verification.Matches(draft with { Fingerprint = new string('b', 64) }), "A changed certificate reused old consent");
        Check(verification.Matches(draft with { AutomaticReceive = true }), "A separate explicit receive choice altered certificate identity");
        verification.Reset();
        Check(!verification.Matches(draft), "Returning to selection retained comparison consent");
        return Task.CompletedTask;
    }

    public static async Task HistorySelection()
    {
        var complete = new Transfer { Id = "sent", Direction = "send", State = "completed" };
        var failed = new Transfer { Id = "retry", State = "failed" };
        var cancelled = new Transfer { Id = "cancelled", State = "cancelled" };
        var selection = new List<Transfer> { complete, failed, cancelled, complete,
            new() { Id = "active", State = "running" }, new() { Id = "paused", State = "paused" },
            new() { Id = "reconnecting", State = "reconnecting" }, new() { Id = "future-state", State = "unknown" },
            new() { Id = "relay-state", State = "delivered" } };
        var plan = new HistoryRemovalPlan(selection);
        Check(plan.Ids.SequenceEqual(["sent", "retry", "cancelled"]), "Removal included active, unknown or relay states, or repeated IDs");
        selection.Add(new() { Id = "finished-after-click", State = "completed" });
        List<string> requested = [];
        try
        {
            await plan.ExecuteAsync(() => selection, id =>
            {
                requested.Add(id); failed.State = "running";
                return Task.CompletedTask;
            });
            throw new InvalidOperationException("Resumed failed transfer was removed");
        }
        catch (HistoryRemovalConflictException e) { Check(e.TransferId == "retry", "Wrong conflicting transfer identity"); }
        Check(requested.SequenceEqual(["sent"]), "Removal continued after a conflict or included a later completion");
        Check(complete.State == "completed" && cancelled.State == "cancelled", "Planning mutated transfer records");
    }

    public static async Task HistoryFailureBoundaries()
    {
        var records = new List<Transfer> { new() { Id = "already-gone", State = "completed" }, new() { Id = "first", State = "failed" }, new() { Id = "second", State = "cancelled" } };
        var plan = new HistoryRemovalPlan(records);
        records.RemoveAt(0);
        List<string> requested = [];
        try
        {
            await plan.ExecuteAsync(() => records, id => { requested.Add(id); throw new IOException("Synthetic persistence failure"); });
            throw new InvalidOperationException("Removal swallowed a failure");
        }
        catch (IOException) { }
        Check(requested.SequenceEqual(["first"]), "A missing entry was retried or work continued after failure");
        using var cancellation = new CancellationTokenSource(); cancellation.Cancel(); requested.Clear();
        try
        {
            await plan.ExecuteAsync(() => records, id => { requested.Add(id); return Task.CompletedTask; }, cancellation.Token);
            throw new InvalidOperationException("Removal ignored cancellation");
        }
        catch (OperationCanceledException) { }
        Check(requested.Count == 0 && records.Count == 2, "Cancellation mutated or removed an entry");
        var count = await plan.ExecuteAsync(() => records, id => { requested.Add(id); return Task.CompletedTask; });
        Check(count == 2 && requested.SequenceEqual(["first", "second"]), "Terminal selection was not processed exactly once in order");
    }
}
