using System.Diagnostics;
using System.Reflection;
using System.Runtime.InteropServices;
using System.Security.Cryptography;
using System.Net;
using System.Net.Http.Json;
using System.Net.Security;
using System.Security.Cryptography.X509Certificates;
using System.Text;
using System.Text.Json;
using AirLink.Core;

internal static class Program
{
    private static async Task<int> Main(string[] arguments)
    {
        if (arguments.Length >= 2 && arguments[0] == "--fake-helper") return await FakeHelper.RunAsync(arguments);
        if (arguments.Length > 0 && arguments[0] == "--inspect-native") return NativePackageInspection.Run(arguments);
        if (arguments.Length == 4 && arguments[0] == "--verify-update-release") return UpdateTests.VerifyRelease(arguments[1], arguments[2], arguments[3]);
        (string Name, Func<Task> Run)[] cases = [
            ("compact desktop models preserve code identity, settings drafts and active history", DesktopProtocolTests.Run),
            ("settings preserve existing permissions and unknown additive fields", SettingsRoundTrip),
            ("setup is explicit and preserves custom destinations", SetupTests.ExplicitDefaults),
            ("settings edits merge only changed fields into the newest snapshot", SettingsDraftTests.MergeOnlyEditedFields),
            ("settings drafts reset safely and limits preserve exact localized byte values", SettingsDraftTests.DraftResetAndLimitBoundaries),
            ("setup review commits explicit choices without resetting saved preferences", SettingsDraftTests.SetupReviewPreservesOtherPreferences),
            ("settings rebase follows unedited fields without replacing in-progress input", SettingsDraftTests.RebaseDisplayedFieldsWithoutLosingTyping),
            ("updater uses native channels and rejects downgrades or other architectures", UpdateTests.NativeChannels),
            ("actual NetSparkle Ed25519 checks reject tampered unsigned or truncated updates", UpdateTests.SignatureIntegrity),
            ("signed feeds fail closed and private feed requests send no credentials", UpdateTests.SignedFeed),
            ("installer handoff uses an explicit hidden shell and literal user paths", UpdateTests.InstallerHandoff),
            ("old defaults and 100 GiB progress remain exact", DefaultsAndLargeProgress),
            ("pairing completion follows certificate identity and validates local connection origins", PairingIdentity),
            ("fingerprint confirmation belongs to the exact displayed peer identity", ExperienceTests.ComparisonIdentity),
            ("history removal keeps a captured terminal selection and rejects newly active entries", ExperienceTests.HistorySelection),
            ("history removal stops at request failure or cancellation without widening the selection", ExperienceTests.HistoryFailureBoundaries),
            ("a forced status refresh prevents older polls from restoring removed entries", ExperienceTests.StatusRefreshOrdering),
            ("automatic receive consent applies only to the offered sender", ReceiveConsent),
            ("received display entries follow published sorting and folder boundaries", ReceivedDisplayEntries),
            ("send recipient defaults never redirect a pending or explicit draft", SendRecipientDefaults),
            ("send snapshots preserve later choices and reject stale acknowledgments", SendSnapshotLifetime),
            ("direct send offers only the explicit drop batch", DirectSendBatch),
            ("failed direct send remains available for an explicit retry", FailedDirectSend),
            ("lost offer replies reuse their key only for an unchanged explicit retry", LostOfferReply),
            ("send eligibility and invalid batches leave the draft intact", SendEligibility),
            ("desktop notices announce new attention without replaying old results", DesktopNoticeTests.Transitions),
            ("recent results keep an old transfer's new completion visible", DesktopNoticeTests.RecentResults),
            ("private transport correlates out-of-order replies and reports remote errors", CorrelatedReplies),
            ("helper fixture readiness is independent of the command deadline", StartupReadiness),
            ("request timeout and explicit cancellation preserve a live connection", TimeoutAndCancellation),
            ("long filesystem commands do not inherit the status timeout", LongOperations),
            ("immediate startup failure includes its diagnostic and can retry", StartupFailureAndRetry),
            ("unexpected service crash fails pending work and allows a fresh session", CrashAndRetry),
            ("large or malformed reply fails the session without unbounded input", InvalidOutput),
            ("oversized requests leave the connection usable", OversizedRequest),
            ("diagnostics are bounded and redact credentials and private keys", BoundedDiagnostics),
            ("shutdown waits for exit and remains bounded for ACK and no-ACK hangs", BoundedShutdown),
            ("updater shutdown never terminates a helper that has not exited", GracefulUpdateShutdown),
            ("a blocked stdin writer cannot prevent shutdown", BlockedInput),
            ("previous process EOF cannot clear the restarted connection", PreviousSessionIsolation),
            ("embedded helper extraction verifies reuse and repairs only its private cache", HelperExtraction),
            ("concurrent helper extraction never exposes a partial executable", ConcurrentHelperExtraction),
            ("actual helper preserves private status settings and source data", ActualHelper)
        ];
        var failed = 0;
        var passed = 0;
        var skipped = 0;
        foreach (var test in cases)
        {
            try { await test.Run(); Console.WriteLine("PASS " + test.Name); passed++; }
            catch (SkippedException ex) { Console.WriteLine("SKIP " + test.Name + ": " + ex.Message); skipped++; }
            catch (Exception ex) { Console.Error.WriteLine("FAIL " + test.Name + "\n" + ex); failed++; }
        }
        Console.WriteLine($"AirLink.Core background tests: {passed} passed, {skipped} skipped, {failed} failed.");
        return failed == 0 ? 0 : 1;
    }

    private sealed class SkippedException(string message) : Exception(message);
    private static void Require(bool condition, string message) { if (!condition) throw new InvalidOperationException(message); }
    private static async Task<T> Throws<T>(Func<Task> action) where T : Exception
    {
        try { await action(); }
        catch (T ex) { return ex; }
        throw new InvalidOperationException("Expected " + typeof(T).Name);
    }

    private sealed class Fixture : IAsyncDisposable
    {
        public string Root { get; } = Path.Combine(Path.GetTempPath(), "AirLink-Windows-Core-" + Guid.NewGuid().ToString("N"));
        public string Data => Path.Combine(Root, "state");
        public string Received => Path.Combine(Root, "received");
        public string ModePath => Path.Combine(Data, "test-mode");
        public string ReadyPath => Path.Combine(Data, "test-ready");
        public Fixture() { Directory.CreateDirectory(Data); }
        public void Mode(string mode) => File.WriteAllText(ModePath, mode);
        public ValueTask DisposeAsync() { Directory.Delete(Root, recursive: true); return ValueTask.CompletedTask; }
    }

    private static string FakeExecutable => Environment.ProcessPath ?? throw new InvalidOperationException("No process executable");
    private static DaemonClient FakeClient(TimeSpan? timeout = null, int maxReply = 1024 * 1024, int maxRequest = 2 * 1024 * 1024)
    {
        List<string> prefix = [];
        if (Path.GetFileNameWithoutExtension(FakeExecutable).Equals("dotnet", StringComparison.OrdinalIgnoreCase)) prefix.Add(Assembly.GetExecutingAssembly().Location);
        prefix.AddRange(["--fake-helper", "controlled"]);
        return new DaemonClient(new DaemonClientOptions
        {
            PrefixArguments = prefix, DisableDiscovery = true,
            RequestTimeout = timeout ?? TimeSpan.FromSeconds(2),
            GracefulStopTimeout = TimeSpan.FromMilliseconds(250),
            TerminateTimeout = TimeSpan.FromMilliseconds(150), KillTimeout = TimeSpan.FromSeconds(1),
            MaximumReplyCharacters = maxReply, MaximumRequestBytes = maxRequest
        });
    }

    private static async Task Ready(DaemonClient client, Fixture fixture)
    {
        // Process creation/JIT/antivirus scheduling is not the command under
        // test. Wait for this fake process's explicit startup marker while
        // retaining the short request deadline for the protocol assertions.
        if (!client.IsRunning) File.Delete(fixture.ReadyPath);
        await client.StartAsync(FakeExecutable, fixture.Data, fixture.Received);
        var startup = Stopwatch.StartNew();
        while (!File.Exists(fixture.ReadyPath))
        {
            Require(client.IsRunning, "Fake helper exited before startup readiness");
            if (startup.Elapsed > TimeSpan.FromSeconds(30)) throw new TimeoutException("Fake helper startup readiness timed out");
            await Task.Delay(10);
        }
        var result = await client.RequestAsync<HostStatus>("status");
        Require(result.Id == "fake-host" && client.IsRunning, "Helper did not become ready");
    }

    private static async Task StartupReadiness()
    {
        await using var fixture = new Fixture(); fixture.Mode("delayed-ready");
        await using var client = FakeClient(TimeSpan.FromMilliseconds(500));
        await Ready(client, fixture);
        await Throws<TimeoutException>(async () => { await client.RequestAsync("very-slow"); });
        Require((await client.RequestAsync<HostStatus>("status")).Id == "fake-host", "Startup synchronization changed the live request deadline");
    }

    private static Task SettingsRoundTrip()
    {
        const string json = """{"name":"Kept","receiveDirectory":"C:\\Users\\example\\Downloads\\AirLink","appearance":"dark","language":"de","notifications":false,"sendLimitBytesPerSecond":32768,"receiveLimitBytesPerSecond":65536,"shortcut":{"enabled":false,"keyCode":40,"modifiers":256,"futureShortcut":true},"organization":{"detectDuplicates":true,"sortEnabled":false,"rules":[{"id":"docs","extensions":["pdf"],"folder":"Documents","futureRule":"kept"}],"futureOrganization":7},"futureTop":{"value":42}}""";
        var original = JsonSerializer.Deserialize<HostSettings>(json, ProtocolJson.Options)!;
        var copy = ProtocolJson.Clone(original); copy.Notifications = true;
        using var saved = JsonDocument.Parse(JsonSerializer.Serialize(copy, ProtocolJson.Options));
        Require(saved.RootElement.GetProperty("futureTop").GetProperty("value").GetInt32() == 42, "Unknown setting lost");
        Require(copy.Organization.DetectDuplicates && !copy.Organization.SortEnabled && !copy.Shortcut.Enabled, "Existing preferences reset");
        Require(saved.RootElement.GetProperty("organization").GetProperty("rules")[0].GetProperty("futureRule").GetString() == "kept", "Nested setting lost");
        Require(!original.Notifications && copy.Notifications, "Clone shared mutable settings");
        return Task.CompletedTask;
    }

    private static Task DefaultsAndLargeProgress()
    {
        var settings = JsonSerializer.Deserialize<HostSettings>("{}", ProtocolJson.Options)!;
        Require(!settings.Organization.DetectDuplicates && !settings.Organization.SortEnabled && settings.Organization.Rules.Count == 0, "Organization default enabled");
        var device = JsonSerializer.Deserialize<Device>("{\"id\":\"legacy\"}", ProtocolJson.Options)!;
        Require(!device.AutoAccept && !device.CanClipboard && !device.CanRelay, "New permission silently enabled");
        var transfer = new Transfer { State = "running", TotalBytes = 100L * 1024 * 1024 * 1024, CompletedBytes = 40L * 1024 * 1024 * 1024, SkippedBytes = 10L * 1024 * 1024 * 1024 };
        Require(transfer.Progress == 0.5 && transfer.CompletedBytes == 42_949_672_960L, "64-bit byte/progress loss");
        transfer.CompletedBytes = long.MaxValue; transfer.SkippedBytes = long.MaxValue;
        Require(transfer.Progress == 1, "Large progress overflow");
        return Task.CompletedTask;
    }

    private static Task PairingIdentity()
    {
        var fingerprint = new string('a', 64);
        var draft = new ConnectionDraft(" https://192.168.1.20:43871/ ", string.Join(":", Enumerable.Repeat("AA", 32)));
        Require(draft.IsValid && draft.NormalizedFingerprint == fingerprint && !draft.AutomaticReceive, "Draft normalization enabled trust or lost fingerprint");
        var spoofed = new Device { Id = "wrong", Address = draft.NormalizedAddress, Kind = "engine", Paired = true, Fingerprint = new string('b', 64) };
        var actual = new Device { Id = "actual", Address = "https://changed-address:43871", Kind = "engine", Paired = true, Fingerprint = fingerprint };
        Require(draft.PairedDevice([spoofed, actual])?.Id == "actual", "An address match replaced certificate identity");
        Require(draft.PairedDevice([new Device { Id = "browser", Paired = true, Kind = "browser", Fingerprint = fingerprint }, new Device { Id = "nearby", Kind = "engine", Fingerprint = fingerprint }]) == null,
            "Unpaired or browser device completed engine pairing");
        foreach (var invalid in new[] { "http://host:43871", "https://user:password@host", "https://host/path", "https://host/?token=secret", "https://host/#token", "https://" })
            Require(!new ConnectionDraft(invalid, fingerprint).IsValid, "Unsafe pairing origin accepted: " + invalid);
        Require(new ConnectionDraft("https://[fe80::1]:43871", fingerprint).IsValid && !new ConnectionDraft("https://host", new string('g', 64)).IsValid,
            "IPv6 origin or fingerprint validation failed");
        return Task.CompletedTask;
    }

    private static Task ReceiveConsent()
    {
        var transfer = new Transfer { Direction = "receive", State = "offered", DeviceId = "sender" };
        var sender = new Device { Id = "sender", Paired = true, CanSend = true, CanReceive = false };
        Require(NativeExperience.CanAcceptAndTrust(transfer, sender), "Sender's unrelated receive permission blocked local consent");
        Require(!NativeExperience.CanAcceptAndTrust(transfer, new Device { Id = "other", Paired = true, CanSend = true }) &&
            !NativeExperience.CanAcceptAndTrust(transfer, new Device { Id = "sender", CanSend = true }) &&
            !NativeExperience.CanAcceptAndTrust(transfer, new Device { Id = "sender", Paired = true, CanReceive = true }), "Trust crossed sender or permission boundary");
        transfer.Direction = "send"; Require(!NativeExperience.CanAcceptAndTrust(transfer, sender), "Outgoing offer granted incoming trust");
        transfer.Direction = "receive"; transfer.State = "completed";
        Require(!NativeExperience.CanAcceptAndTrust(transfer, sender) && !NativeExperience.CanAcceptAndTrust(transfer, null), "Completed or missing sender granted consent");
        return Task.CompletedTask;
    }

    private static Task ReceivedDisplayEntries()
    {
        var transfer = new Transfer { Direction = "receive", State = "completed", Files = [
            new() { Id = "folder", Path = "Folder", Directory = true, Completed = true },
            new() { Id = "nested-folder", Path = "Folder/Empty", Directory = true, Completed = true },
            new() { Id = "child", Path = "Folder/child.txt", Completed = true },
            new() { Id = "sorted", Path = "Folder/sorted.pdf", DestinationPath = "Documents/sorted.pdf", Completed = true },
            new() { Id = "collision", Path = "Folder-like.txt", Completed = true },
            new() { Id = "skipped", Path = "skipped.txt", Completed = true, Skipped = true },
            new() { Id = "incomplete", Path = "incomplete.txt" },
            new() { Id = "invalid", Path = "../escape.txt", Completed = true }
        ] };
        Require(NativeExperience.ReceivedDisplayEntries(transfer).Select(file => file.Id).SequenceEqual(["folder", "sorted", "collision"]),
            "Display IDs did not match published folder/sorting boundaries");
        transfer.Files = [new() { Id = "empty", Path = "Empty", Directory = true, Completed = true }];
        Require(NativeExperience.ReceivedDisplayEntries(transfer).Single().Id == "empty", "Valid empty folder result was hidden");
        transfer.Files.Add(new TransferFile { Id = "all-skipped", Path = "skipped.txt", Completed = true, Skipped = true });
        Require(NativeExperience.ReceivedDisplayEntries(transfer).Count == 0, "All-skipped receive redirected into structural folders");
        transfer.Files[1].Skipped = false; transfer.Files[1].Completed = false;
        Require(NativeExperience.ReceivedDisplayEntries(transfer).Count == 0, "Unpublished regular payload exposed a structural folder result");
        transfer.Direction = "send"; Require(NativeExperience.ReceivedDisplayEntries(transfer).Count == 0, "Outgoing files exposed as received results");
        return Task.CompletedTask;
    }

    private static async Task CorrelatedReplies()
    {
        await using var fixture = new Fixture(); await using var client = FakeClient(); await Ready(client, fixture);
        var slow = client.RequestAsync<JsonElement>("slow", new { marker = "slow" });
        var fast = client.RequestAsync<JsonElement>("echo", new { marker = "fast" });
        Require((await fast).GetProperty("marker").GetString() == "fast" && !slow.IsCompleted, "Replies were serialized or mismatched");
        Require((await slow).GetProperty("marker").GetString() == "slow", "Slow reply mismatch");
        var error = await Throws<DaemonException>(async () => { await client.RequestAsync("reject"); });
        Require(error.Message.Contains("recipient approval"), "Remote error lost");
        Require((await client.RequestAsync<HostStatus>("status")).Id == "fake-host", "Remote error killed connection");
    }

    private static Device Recipient(string id, bool online = true) => new() { Id = id, Kind = "engine", Paired = true, CanReceive = true, Online = online };
    private static string SourcePath(string name) => Path.Combine(Path.GetTempPath(), "AirLink-draft-model", name);

    private static Task SendRecipientDefaults()
    {
        var a = Recipient("A"); var b = Recipient("B");
        var browser = new Device { Id = "browser", Kind = "browser", Paired = true, CanReceive = true, Online = true };
        var denied = Recipient("denied"); denied.CanReceive = false;
        var unpaired = Recipient("nearby"); unpaired.Paired = false;
        var draft = new TransferDraft();
        draft.RefreshDevices([browser, denied, unpaired, a]);
        Require(draft.SelectedDeviceId == "A" && !draft.HasExplicitTarget, "A sole online authorized desktop was not chosen");
        a.Online = false; draft.RefreshDevices([a, b]);
        Require(draft.SelectedDeviceId == "A", "Refresh silently redirected a shown recipient when it went offline");
        draft.SelectDevice("B"); draft.RefreshDevices([a]);
        Require(draft.SelectedDeviceId == "B" && draft.HasExplicitTarget, "A missing selected peer was silently replaced");

        var ambiguous = new TransferDraft(); a.Online = true;
        ambiguous.RefreshDevices([a, b]); Require(ambiguous.SelectedDeviceId is null, "Ambiguous devices were silently chosen");
        ambiguous.AddPaths([SourcePath("awaiting-choice.txt")]); ambiguous.RefreshDevices([a]);
        Require(ambiguous.SelectedDeviceId is null, "Discovery changed a pending draft's recipient");
        var offline = new TransferDraft(); a.Online = false; offline.RefreshDevices([a]);
        Require(offline.SelectedDeviceId is null, "Offline peer was automatically selected");
        a.Online = true; offline.RefreshDevices([a], allowAutomaticSelection: false);
        Require(offline.SelectedDeviceId is null, "Automatic selection ignored caller's explicit gate");
        offline.RefreshDevices([a]); Require(offline.SelectedDeviceId == "A", "Delayed online discovery did not select an empty draft");
        return Task.CompletedTask;
    }

    private static Task SendSnapshotLifetime()
    {
        var a = Recipient("A"); var b = Recipient("B");
        var first = SourcePath("first.txt"); var second = SourcePath("second.txt"); var later = SourcePath("later.txt");
        var draft = new TransferDraft(); draft.SelectDevice("A");
        draft.AddPaths([first, second, first]);
        Require(draft.Entries.Count == 2, "Repeated paths created duplicate selected files");
        var submittedSecond = draft.Entries.Single(entry => entry.Path == second);
        var submission = draft.TryBegin([a, b], serviceReady: true)!;
        Require(submission.DeviceId == "A" && submission.Paths.SequenceEqual([first, second]) && draft.IsSending, "Offer did not capture intended device and sources");
        Require(draft.TryBegin([a, b], true) is null, "Repeated Send created another offer");
        draft.AddPaths([later]); draft.Entries.Remove(submittedSecond); draft.AddPaths([second]); draft.SelectDevice("B");
        var reselectedSecond = draft.Entries.Single(entry => entry.Path == second);
        Require(reselectedSecond.Id != submittedSecond.Id, "A reselected path reused an in-flight entry identity");
        Require(submission.DeviceId == "A" && submission.Paths.SequenceEqual([first, second]), "Later choices mutated an in-flight offer");
        Require(draft.FinishSuccess(submission), "Current offer acknowledgment was rejected");
        Require(draft.Entries.Select(entry => entry.Path).SequenceEqual([later, second]) && draft.SelectedDeviceId == "B" && !draft.IsSending,
            "Successful acknowledgment removed later or reselected files, or redirected a later recipient choice");

        var next = draft.TryBegin([a, b], true)!;
        Require(next.DeviceId == "B" && !draft.FinishSuccess(submission) && draft.IsSending, "Late acknowledgment cleared a newer offer");
        var other = new TransferDraft(); other.SelectDevice("A"); other.AddPaths([SourcePath("foreign.txt")]);
        var foreign = other.TryBegin([a], true)!;
        Require(!draft.Fail(foreign) && !draft.FinishSuccess(foreign) && draft.IsSending, "Foreign offer token changed current state");
        Require(draft.Fail(next) && draft.Entries.Count == 2 && !draft.IsSending, "Failure discarded retryable selected files");
        return Task.CompletedTask;
    }

    private static Task DirectSendBatch()
    {
        var a = Recipient("A"); var b = Recipient("B");
        var kept = SourcePath("manual-selection.txt"); var shared = SourcePath("also-dropped.txt");
        var direct = SourcePath("direct.txt"); var later = SourcePath("after-drop.txt"); var busyPath = SourcePath("busy.txt");
        var draft = new TransferDraft(); draft.SelectDevice("A"); draft.AddPaths([kept, shared]);
        var submission = draft.TryBeginDirect("B", [shared, direct, direct], [a, b], true)!;
        Require(submission.TargetId == "B" && submission.Paths.SequenceEqual([shared, direct]) && draft.SelectedDeviceId == "B", "Direct drop consumed the whole old draft or wrong recipient");
        draft.AddPaths([later]);
        Require(draft.TryBeginDirect("A", [busyPath], [a, b], true) is null && !draft.Entries.Any(entry => entry.Path == busyPath),
            "A busy direct-send was silently accepted or appended to a different pending send");
        Require(draft.FinishSuccess(submission) && draft.Entries.Select(entry => entry.Path).SequenceEqual([kept, later]),
            "Direct-send acknowledgment discarded unrelated selections");
        return Task.CompletedTask;
    }

    private static Task FailedDirectSend()
    {
        var recipient = Recipient("offline", online: false);
        var first = SourcePath("retry-file.txt"); var second = SourcePath("retry-folder");
        var draft = new TransferDraft();
        var submission = draft.TryBeginDirect(recipient.Id, [first, second], [recipient], true)!;
        Require(submission.Paths.Count == 2 && draft.SelectedDeviceId == recipient.Id, "Explicit offline offer could not be queued for its intended device");
        Require(draft.Fail(submission) && !draft.IsSending && draft.Entries.Select(entry => entry.Path).SequenceEqual([first, second]), "Failed drop lost its sources");
        var retry = draft.TryBegin([recipient], true)!;
        Require(retry.Token == submission.Token && retry.DeviceId == recipient.Id && retry.Paths.SequenceEqual(submission.Paths), "Explicit retry changed offer key, recipient or sources");
        Require(!draft.FinishSuccess(submission) && !draft.Fail(submission) && draft.IsSending, "A late local result from the prior attempt cleared its active retry");
        Require(draft.FinishSuccess(retry) && draft.Entries.Count == 0, "Successful retry did not clear exactly its own files");
        return Task.CompletedTask;
    }

    private static Task LostOfferReply()
    {
        var a = Recipient("A"); var b = Recipient("B");
        var first = SourcePath("possibly-committed.txt"); var second = SourcePath("later.txt");
        var draft = new TransferDraft(); draft.SelectDevice(a.Id); draft.AddPaths([first]);
        var original = draft.TryBegin([a, b], true)!; draft.Fail(original);
        var retried = draft.TryBegin([a, b], true)!;
        Require(retried.Token == original.Token, "Retry after a possibly lost ACK would create another persisted offer");
        draft.Fail(retried); draft.AddPaths([second]);
        var changedEntries = draft.TryBegin([a, b], true)!;
        Require(changedEntries.Token != original.Token, "Adding a source reused a key for another request payload");
        draft.Fail(changedEntries); draft.SelectDevice(b.Id);
        var changedDevice = draft.TryBegin([a, b], true)!;
        Require(changedDevice.Token != changedEntries.Token && changedDevice.DeviceId == b.Id, "A changed recipient reused another recipient's key");
        draft.Fail(changedDevice); draft.Entries.Remove(draft.Entries[0]); draft.AddPaths([first]);
        var reordered = draft.TryBegin([a, b], true)!;
        Require(reordered.Token != changedDevice.Token && reordered.Paths.SequenceEqual([second, first]), "Reordered/reselected entries reused the old key");
        draft.FinishSuccess(reordered); draft.AddPaths([second, first]);
        var explicitNewSend = draft.TryBegin([a, b], true)!;
        Require(explicitNewSend.Token != reordered.Token, "An explicitly new send of the same files reused a completed offer key");

        var direct = new TransferDraft(); var drop = direct.TryBeginDirect(a.Id, [first], [a, b], true)!;
        direct.Fail(drop);
        var sameDrop = direct.TryBeginDirect(a.Id, [first], [a, b], true)!;
        Require(sameDrop.Token == drop.Token, "Retrying an identical failed direct drop lost its persisted offer key");
        return Task.CompletedTask;
    }

    private static Task SendEligibility()
    {
        var desktop = Recipient("desktop");
        var browser = Recipient("browser"); browser.Kind = "browser";
        var unpaired = Recipient("unpaired"); unpaired.Paired = false;
        var denied = Recipient("denied"); denied.CanReceive = false;
        var draft = new TransferDraft(); var source = SourcePath("source.txt");
        foreach (var device in new[] { browser, unpaired, denied })
            Require(!TransferDraft.CanSendTo(device) && draft.TryBeginDirect(device.Id, [source], [device], true) is null, "Unauthorized or browser recipient accepted native direct-send");
        Require(draft.TryBeginDirect(desktop.Id, [source], [desktop], false) is null && draft.Entries.Count == 0 && draft.SelectedDeviceId is null,
            "Unready service accepted a direct-drop batch");
        try { draft.AddPaths([source, "relative.txt"]); throw new InvalidOperationException("Relative source accepted"); }
        catch (ArgumentException) { Require(draft.Entries.Count == 0, "Invalid batch partially changed the draft"); }
        Require(draft.TryBeginDirect(desktop.Id, [], [desktop], true) is null && draft.SelectedDeviceId is null, "Empty direct batch changed selected recipient");
        draft.SelectDevice("missing"); draft.AddPaths([source]);
        Require(draft.TryBegin([desktop], true) is null && draft.SelectedDeviceId == "missing" && draft.Entries.Count == 1,
            "Missing selected recipient fell back to an unrelated available device");
        draft.SelectDevice(desktop.Id); desktop.Online = false;
        Require(TransferDraft.CanSendTo(desktop) && draft.TryBegin([desktop], true) is not null, "Paired offline desktop lost explicit persistent queue support");
        return Task.CompletedTask;
    }

    private static async Task TimeoutAndCancellation()
    {
        await using var fixture = new Fixture(); await using var client = FakeClient(TimeSpan.FromMilliseconds(500)); await Ready(client, fixture);
        await Throws<TimeoutException>(async () => { await client.RequestAsync("very-slow"); });
        using var cancelled = new CancellationTokenSource(50);
        await Throws<OperationCanceledException>(async () => { await client.RequestAsync("offer", null, cancelled.Token); });
        Require(client.IsRunning && (await client.RequestAsync<HostStatus>("status")).Id == "fake-host", "Timeout damaged healthy session");
    }

    private static async Task LongOperations()
    {
        await using var fixture = new Fixture(); await using var client = FakeClient(TimeSpan.FromMilliseconds(500)); await Ready(client, fixture);
        foreach (var method in new[] { "offer", "share.create", "duplicate.resolve" })
        {
            var task = client.RequestAsync(method);
            Require((await client.RequestAsync<HostStatus>("status")).Id == "fake-host", "Status blocked by long request");
            Require((await task).GetProperty("ok").GetBoolean(), "Long request inherited short timeout");
        }
    }

    private static async Task StartupFailureAndRetry()
    {
        await using var fixture = new Fixture(); await using var client = FakeClient();
        fixture.Mode("immediate-exit"); await client.StartAsync(FakeExecutable, fixture.Data);
        var failure = await Throws<DaemonException>(async () => { await client.RequestAsync("status"); });
        Require(failure.Message.Contains("RECEIVE_DIRECTORY_UNAVAILABLE"), "Startup diagnostic lost: " + failure.Message);
        fixture.Mode("normal"); await Ready(client, fixture);
        Require(Directory.Exists(fixture.Data), "Retry discarded data directory");
    }

    private static async Task CrashAndRetry()
    {
        await using var fixture = new Fixture(); await using var client = FakeClient(); await Ready(client, fixture);
        var exited = new TaskCompletionSource<DaemonExitedEventArgs>(TaskCreationOptions.RunContinuationsAsynchronously);
        client.Exited += (_, args) => { if (args.Unexpected) exited.TrySetResult(args); };
        await Throws<DaemonException>(async () => { await client.RequestAsync("crash"); });
        var old = await exited.Task.WaitAsync(TimeSpan.FromSeconds(2));
        Require(old.Diagnostic.Contains("INTENTIONAL_CRASH") && !client.IsRunning, "Crash reason or lifecycle state lost");
        await Ready(client, fixture);
        Require(client.SessionId != old.SessionId, "Restart reused failed session identity");
    }

    private static async Task InvalidOutput()
    {
        foreach (var mode in new[] { "oversized", "malformed" })
        {
            await using var fixture = new Fixture(); await using var client = FakeClient(maxReply: 4096); await Ready(client, fixture);
            await Throws<DaemonException>(async () => { await client.RequestAsync(mode); });
            Require(!client.IsRunning, "Invalid helper output retained ready state");
            await Ready(client, fixture);
        }
    }

    private static async Task OversizedRequest()
    {
        await using var fixture = new Fixture(); await using var client = FakeClient(maxRequest: 4096); await Ready(client, fixture);
        await Throws<DaemonException>(async () => { await client.RequestAsync("echo", new { text = new string('x', 5000) }); });
        Require((await client.RequestAsync<HostStatus>("status")).Id == "fake-host", "Oversized frame broke session");
    }

    private static async Task BoundedDiagnostics()
    {
        await using var fixture = new Fixture(); fixture.Mode("diagnostics"); await using var client = FakeClient(); await Ready(client, fixture);
        var deadline = DateTime.UtcNow + TimeSpan.FromSeconds(2);
        while (!client.DiagnosticTail.Contains("AFTER_DIAGNOSTICS") && DateTime.UtcNow < deadline) await Task.Delay(5);
        var tail = client.DiagnosticTail;
        Require(tail.Contains("AFTER_DIAGNOSTICS") && Encoding.UTF8.GetByteCount(tail) <= 8192, "Diagnostic bound/end lost");
        foreach (var secret in new[] { "TOKEN_SECRET", "PASSWORD_SECRET", "BEARER_SECRET", "PRIVATE_KEY_CONTENT", "LONG_SECRET" }) Require(!tail.Contains(secret), "Diagnostic exposed " + secret);
    }

    private static async Task BoundedShutdown()
    {
        foreach (var mode in new[] { "hang-ack", "hang-no-ack" })
        {
            await using var fixture = new Fixture(); fixture.Mode(mode); await using var client = FakeClient(); await Ready(client, fixture);
            var watch = Stopwatch.StartNew();
            Require(await client.StopAsync() && !client.IsRunning, "Owned hung process was left running");
            Require(watch.Elapsed < TimeSpan.FromSeconds(3), "Shutdown exceeded bounded stages");
        }
    }

    private static async Task GracefulUpdateShutdown()
    {
        foreach (var mode in new[] { "hang-ack", "hang-no-ack" })
        {
            await using var fixture = new Fixture(); fixture.Mode(mode); await using var client = FakeClient(); await Ready(client, fixture);
            var session = client.SessionId;
            Require(!await client.StopGracefullyAsync(), "Updater shutdown falsely reported a checkpointed helper exit");
            Require(client.IsRunning && client.SessionId == session && (await client.RequestAsync<HostStatus>("status")).Id == "fake-host", "Updater shutdown killed, closed the pipe of, or replaced a live helper");
        }
        await using var normalFixture = new Fixture(); await using var normal = FakeClient(); await Ready(normal, normalFixture);
        Require(await normal.StopGracefullyAsync() && !normal.IsRunning, "An ordinarily exiting helper could not close gracefully for an update");
    }

    private static async Task BlockedInput()
    {
        await using var fixture = new Fixture(); fixture.Mode("no-read"); await using var client = FakeClient(); await client.StartAsync(FakeExecutable, fixture.Data);
        var deadline = DateTime.UtcNow + TimeSpan.FromSeconds(3);
        while (!client.DiagnosticTail.Contains("NO_READ_READY") && DateTime.UtcNow < deadline) await Task.Delay(5);
        Require(client.DiagnosticTail.Contains("NO_READ_READY"), "Blocked input fake did not start");
        var blocked = client.RequestAsync("echo", new { text = new string('x', 1_000_000) });
        await Task.Delay(30);
        var watch = Stopwatch.StartNew();
        Require(await client.StopAsync(), "Full input pipe stranded helper");
        Require(watch.Elapsed < TimeSpan.FromSeconds(3), "Writer blocked cleanup");
        await Throws<DaemonException>(async () => { await blocked; });
    }

    private static async Task PreviousSessionIsolation()
    {
        await using var fixture = new Fixture(); await using var client = FakeClient();
        for (var cycle = 0; cycle < 4; cycle++)
        {
            await Ready(client, fixture); var previous = client.SessionId;
            Require(await client.StopAsync(), "Prior session did not exit");
            await Ready(client, fixture);
            await Task.Delay(50);
            Require(client.IsRunning && client.SessionId != previous && (await client.RequestAsync<HostStatus>("status")).Id == "fake-host", "Old callback cleared current session");
        }
    }

    private static async Task ActualHelper()
    {
        var helper = Environment.GetEnvironmentVariable("AIRLINK_TEST_HELPER");
        if (string.IsNullOrWhiteSpace(helper)) throw new SkippedException("Set AIRLINK_TEST_HELPER for the headless real service contract");
        await using var fixture = new Fixture(); await using var client = new DaemonClient(new DaemonClientOptions { DisableDiscovery = true });
        await client.StartAsync(Path.GetFullPath(helper), fixture.Data, fixture.Received);
        var before = await client.RequestAsync<HostStatus>("status");
        var expectedVersion = Environment.GetEnvironmentVariable("AIRLINK_TEST_VERSION") ?? typeof(HostStatus).Assembly.GetCustomAttribute<AssemblyInformationalVersionAttribute>()!.InformationalVersion.Split('+')[0];
        Require(before.Version == expectedVersion && before.Fingerprint.Length == 64 && before.ChunkSize == 8 * 1024 * 1024, "Unexpected service contract/version");
        var settings = ProtocolJson.Clone(before.Settings);
        settings.Appearance = "dark"; settings.Language = "en"; settings.Notifications = false;
        settings.ReceiveLimitBytesPerSecond = 65536;
        settings.Organization = new Organization { DetectDuplicates = true, Rules = [new SortRule { Id = "docs", Extensions = ["txt"], Folder = "Documents" }] };
        var saved = await client.RequestAsync<HostSettings>("settings.update", settings);
        Require(saved.ReceiveDirectory == fixture.Received && saved.Organization.DetectDuplicates && saved.ReceiveLimitBytesPerSecond == 65536, "Full settings contract lost values");
        var original = Path.Combine(fixture.Root, "original.txt"); await File.WriteAllTextAsync(original, "Preserve original test data");
        var share = await client.RequestAsync<Share>("share.create", new { paths = new[] { original }, maxDownloads = 2 });
        Require(share.TotalBytes > 0 && share.MaxDownloads == 2 && share.Url?.Contains('#') == true, "Share IPC contract failed");
        await client.RequestAsync("share.revoke", new { id = share.Id });
        Require(await File.ReadAllTextAsync(original) == "Preserve original test data", "Private request altered original");

        using var authority = X509Certificate2.CreateFromPem(await File.ReadAllTextAsync(before.CertificatePath!));
        Require(Convert.ToHexString(SHA256.HashData(authority.RawData)).Equals(before.Fingerprint, StringComparison.OrdinalIgnoreCase), "Private certificate identity mismatch");
        var cookies = new CookieContainer();
        using var handler = new HttpClientHandler { CookieContainer = cookies, UseProxy = false };
        handler.ServerCertificateCustomValidationCallback = (_, certificate, _, errors) =>
        {
            if (certificate == null || (errors & SslPolicyErrors.RemoteCertificateNameMismatch) != 0) return false;
            using var chain = new X509Chain();
            chain.ChainPolicy.TrustMode = X509ChainTrustMode.CustomRootTrust;
            chain.ChainPolicy.CustomTrustStore.Add(authority);
            chain.ChainPolicy.RevocationMode = X509RevocationMode.NoCheck;
            return chain.Build(certificate);
        };
        using var http = new HttpClient(handler) { BaseAddress = new Uri(before.Urls.Last()), Timeout = TimeSpan.FromSeconds(10) };
        async Task<TestHttpResponse> Send(string method, string path, byte[] body, Dictionary<string, string>? headers = null)
        {
            var uri = new Uri(http.BaseAddress!, path);
            if (OperatingSystem.IsMacOS())
            {
                // macOS .NET uses SecureTransport, whose TLS 1.3 limitation is unrelated
                // to the production Windows client (which uses private process pipes).
                headers ??= [];
                var cookie = cookies.GetCookieHeader(uri);
                if (cookie.Length != 0) headers["Cookie"] = cookie;
                var bridged = await NodeTls13Request(uri, method, body, before.CertificatePath!, headers);
                foreach (var value in bridged.SetCookies) cookies.SetCookies(uri, value);
                return bridged;
            }
            using var request = new HttpRequestMessage(new HttpMethod(method), uri) { Content = new ByteArrayContent(body) };
            foreach (var header in headers ?? [])
                if (!request.Headers.TryAddWithoutValidation(header.Key, header.Value)) request.Content.Headers.Add(header.Key, header.Value);
            using var response = await http.SendAsync(request);
            return new TestHttpResponse { Status = (int)response.StatusCode, BodyBase64 = Convert.ToBase64String(await response.Content.ReadAsByteArrayAsync()) };
        }
        async Task<T> Post<T>(string path, object payload)
        {
            var response = await Send("POST", path, JsonSerializer.SerializeToUtf8Bytes(payload, ProtocolJson.Options), new() { ["Content-Type"] = "application/json" });
            Require(response.Status is >= 200 and < 300, "Actual helper HTTP request failed: " + response.Status);
            return JsonSerializer.Deserialize<T>(Convert.FromBase64String(response.BodyBase64), ProtocolJson.Options)!;
        }
        var pair = await Post<PairingTicket>("/api/v1/pair/request", new { name = "Windows core browser fixture", kind = "browser" });
        await client.RequestAsync("pair.approve", new { id = pair.RequestId });
        _ = await Post<JsonElement>("/api/v1/pair/poll", new { requestId = pair.RequestId, pollToken = pair.PollToken });
        var incomingBytes = Encoding.UTF8.GetBytes("The native receive action returns the real published file.");
        var transfer = await Post<Transfer>("/api/v1/transfers", new { files = new[] { new { id = "received", path = "visible.txt", size = incomingBytes.Length } } });
        Require(transfer.State == "offered", "Ordinary pairing silently enabled auto receipt");
        var accepted = await client.RequestAsync<Transfer>("transfer.acceptTrusted", new { id = transfer.Id });
        Require(accepted.State == "running", "Explicit trusted receipt failed");
        var checksum = Convert.ToHexString(SHA256.HashData(incomingBytes)).ToLowerInvariant();
        var block = await Send("PUT", $"/api/v1/transfers/{transfer.Id}/files/received/chunks/0", incomingBytes, new() { ["X-Chunk-SHA256"] = checksum });
        Require(block.Status is >= 200 and < 300, "Actual helper rejected the verified chunk: " + block.Status);
        var completed = await Post<Transfer>($"/api/v1/transfers/{transfer.Id}/files/received/complete", new { sha256 = checksum });
        var received = await client.RequestAsync<ReceivedResult>("transfer.received", new { id = completed.Id });
        Require(received.Items.Count == 1 && !received.Items[0].Directory && (await File.ReadAllBytesAsync(received.Items[0].Path)).SequenceEqual(incomingBytes), "Native result reveal did not resolve the published file");
        var trusted = await Post<PairingTicket>("/api/v1/pair/request", new { name = "Trusted pairing fixture", kind = "browser" });
        await client.RequestAsync("pair.approveTrusted", new { id = trusted.RequestId });
        var permissions = await client.RequestAsync<HostStatus>("status");
        Require(permissions.Devices.Count == 2 && permissions.Devices.All(device => device.AutoAccept && !device.CanClipboard && !device.CanRelay), "Trusted receipt reset unrelated permissions");
        var offerRequest = new { deviceId = permissions.Devices[0].Id, paths = new[] { original }, requestId = Guid.NewGuid().ToString("N") };
        var firstOffer = await client.RequestAsync<Transfer>("offer", offerRequest);
        var repeatedOffer = await client.RequestAsync<Transfer>("offer", offerRequest);
        Require(firstOffer.Id == repeatedOffer.Id && firstOffer.Files.Select(file => file.Id).SequenceEqual(repeatedOffer.Files.Select(file => file.Id)),
            "Retrying a committed private offer created another transfer or file manifest");
        Require(await client.StopGracefullyAsync(), "Actual helper failed checkpointed updater shutdown without termination");
        await client.StartAsync(Path.GetFullPath(helper), fixture.Data, fixture.Received);
        var after = await client.RequestAsync<HostStatus>("status");
        Require(after.Id == before.Id && after.Fingerprint == before.Fingerprint && after.Settings.Organization.DetectDuplicates && after.Shares.Single().Revoked, "Restart reset identity/settings/shares");
        Require(after.Devices.Count == 2 && after.Devices.All(device => device.AutoAccept) && (await File.ReadAllBytesAsync(received.Items[0].Path)).SequenceEqual(incomingBytes), "Restart lost explicit trust or received data");
        var durableOffer = await client.RequestAsync<Transfer>("offer", offerRequest);
        Require(durableOffer.Id == firstOffer.Id && durableOffer.Files.Select(file => file.Id).SequenceEqual(firstOffer.Files.Select(file => file.Id)),
            "Service restart lost the committed private offer request key");
    }

    private sealed class TestHttpResponse
    {
        public int Status { get; set; }
        public string BodyBase64 { get; set; } = "";
        public List<string> SetCookies { get; set; } = [];
        public string Protocol { get; set; } = "";
    }

    private static async Task<TestHttpResponse> NodeTls13Request(Uri uri, string method, byte[] body, string caPath, Dictionary<string, string> headers)
    {
        using var deadline = new CancellationTokenSource(TimeSpan.FromSeconds(15));
        var start = new ProcessStartInfo(Environment.GetEnvironmentVariable("AIRLINK_TEST_NODE") ?? "node")
        {
            UseShellExecute = false, CreateNoWindow = true, RedirectStandardInput = true,
            RedirectStandardOutput = true, RedirectStandardError = true
        };
        start.ArgumentList.Add(Path.Combine(AppContext.BaseDirectory, "tls13-request.mjs"));
        using var process = Process.Start(start) ?? throw new IOException("Unable to start the test-only TLS 1.3 bridge");
        async Task<string> ReadBounded(StreamReader reader, int limit)
        {
            var output = new StringBuilder(); var buffer = new char[4096];
            int count;
            while ((count = await reader.ReadAsync(buffer.AsMemory(), deadline.Token)) != 0)
            {
                if (output.Length + count > limit) throw new IOException("Test TLS bridge output exceeded its bound");
                output.Append(buffer, 0, count);
            }
            return output.ToString();
        }
        var stdout = ReadBounded(process.StandardOutput, 1_500_000);
        var stderr = ReadBounded(process.StandardError, 4096);
        try
        {
            var input = JsonSerializer.Serialize(new { url = uri.AbsoluteUri, method, bodyBase64 = Convert.ToBase64String(body), caPath, headers }, ProtocolJson.Options);
            await process.StandardInput.WriteAsync(input.AsMemory(), deadline.Token);
            process.StandardInput.Close();
            await process.WaitForExitAsync(deadline.Token);
            var output = await stdout;
            _ = await stderr; // The fixture emits no credentials; never copy request details to a test failure.
            Require(process.ExitCode == 0, "Test-only TLS 1.3 bridge failed (exit " + process.ExitCode + ")");
            var response = JsonSerializer.Deserialize<TestHttpResponse>(output, ProtocolJson.Options) ?? throw new IOException("Missing test bridge response");
            Require(response.Protocol == "TLSv1.3", "Actual helper request did not use TLS 1.3");
            return response;
        }
        catch
        {
            if (!process.HasExited) { process.Kill(entireProcessTree: true); await process.WaitForExitAsync().WaitAsync(TimeSpan.FromSeconds(2)); }
            try { await Task.WhenAll(stdout, stderr); } catch { }
            throw;
        }
    }

    private sealed class PairingTicket
    {
        public string RequestId { get; set; } = "";
        public string PollToken { get; set; } = "";
    }

    private static async Task HelperExtraction()
    {
        await using var fixture = new Fixture();
        var bytes = Encoding.UTF8.GetBytes("owned embedded helper bytes");
        var expected = Convert.ToHexString(SHA256.HashData(bytes));
        var cache = Path.Combine(fixture.Root, "runtime");
        var destination = await HelperExtractor.ExtractVerifiedAsync(() => new MemoryStream(bytes), cache, expected);
        Require((await File.ReadAllBytesAsync(destination)).SequenceEqual(bytes), "Embedded helper was not extracted");
        var again = await HelperExtractor.ExtractVerifiedAsync(() => throw new InvalidOperationException("Valid reuse must not reopen resource"), cache, expected);
        Require(again == destination, "Valid helper was not reused");
        await File.WriteAllTextAsync(destination, "corrupted cached executable");
        var repaired = await HelperExtractor.ExtractVerifiedAsync(() => new MemoryStream(bytes), cache, expected);
        Require(repaired == destination && (await File.ReadAllBytesAsync(destination)).SequenceEqual(bytes), "Cache corruption was reused");
        var otherHash = Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes("other source")));
        await Throws<DaemonException>(async () => { await HelperExtractor.ExtractVerifiedAsync(() => new MemoryStream(bytes), cache, otherHash); });
        Require(!File.Exists(Path.Combine(cache, otherHash.ToLowerInvariant(), "airlink.exe")), "Mismatched embedded content became executable");
        Require((await File.ReadAllBytesAsync(destination)).SequenceEqual(bytes), "Unrelated valid runtime was changed");
        var linkedCache = Path.Combine(fixture.Root, "linked");
        if (!OperatingSystem.IsWindows())
        {
            Directory.CreateSymbolicLink(linkedCache, cache);
            await Throws<DaemonException>(async () => { await HelperExtractor.ExtractVerifiedAsync(() => new MemoryStream(bytes), linkedCache, expected); });
        }
    }

    private static async Task ConcurrentHelperExtraction()
    {
        await using var fixture = new Fixture();
        var bytes = Enumerable.Range(0, 1024 * 256).Select(value => (byte)(value % 251)).ToArray();
        var expected = Convert.ToHexString(SHA256.HashData(bytes));
        var cache = Path.Combine(fixture.Root, "runtime");
        var results = await Task.WhenAll(Enumerable.Range(0, 8).Select(_ => HelperExtractor.ExtractVerifiedAsync(() => new MemoryStream(bytes), cache, expected)));
        Require(results.Distinct().Count() == 1 && (await File.ReadAllBytesAsync(results[0])).SequenceEqual(bytes), "Concurrent launch exposed incomplete helper");
        Require(!Directory.GetFiles(cache, "*.tmp", SearchOption.AllDirectories).Any(), "Partial helper files remained after success");
        using var cancelled = new CancellationTokenSource(); cancelled.Cancel();
        await Throws<OperationCanceledException>(async () => { await HelperExtractor.ExtractVerifiedAsync(() => new MemoryStream(bytes), cache, expected, cancelled.Token); });
    }
}

internal static class FakeHelper
{
    public static async Task<int> RunAsync(string[] arguments)
    {
        var dataIndex = Array.IndexOf(arguments, "--data-dir");
        var mode = arguments[1];
        if (mode == "controlled")
        {
            var path = Path.Combine(arguments[dataIndex + 1], "test-mode");
            mode = File.Exists(path) ? File.ReadAllText(path).Trim() : "normal";
        }
        if (mode == "immediate-exit") { Console.Error.WriteLine("RECEIVE_DIRECTORY_UNAVAILABLE"); return 18; }
        if (mode == "delayed-ready") await Task.Delay(1500);
        using var signal = !OperatingSystem.IsWindows() && mode.StartsWith("hang", StringComparison.Ordinal)
            ? PosixSignalRegistration.Create(PosixSignal.SIGTERM, context => context.Cancel = true) : null;
        if (mode == "no-read") { Console.Error.WriteLine("NO_READ_READY"); await Task.Delay(Timeout.Infinite); }
        if (mode == "diagnostics")
        {
            Console.Error.WriteLine("token=TOKEN_SECRET password=\"PASSWORD_SECRET\" Authorization: Bearer BEARER_SECRET");
            Console.Error.WriteLine("-----BEGIN PRIVATE KEY-----\nPRIVATE_KEY_CONTENT\n-----END PRIVATE KEY-----");
            Console.Error.WriteLine("LONG_SECRET" + new string('x', 50_000));
            Console.Error.WriteLine("AFTER_DIAGNOSTICS");
        }
        var write = new SemaphoreSlim(1, 1);
        async Task Reply(object id, object? result = null, string? error = null)
        {
            await write.WaitAsync();
            try { Console.WriteLine(JsonSerializer.Serialize(new { id, result, error }, ProtocolJson.Options)); await Console.Out.FlushAsync(); }
            finally { write.Release(); }
        }
        File.WriteAllText(Path.Combine(arguments[dataIndex + 1], "test-ready"), "ready");
        while (true)
        {
            var line = await Console.In.ReadLineAsync();
            if (line == null) { if (mode.StartsWith("hang", StringComparison.Ordinal)) await Task.Delay(Timeout.Infinite); return 0; }
            using var document = JsonDocument.Parse(line);
            var root = document.RootElement; var id = root.GetProperty("id").GetString()!; var method = root.GetProperty("method").GetString();
            switch (method)
            {
                case "shutdown":
                    if (mode != "hang-no-ack") await Reply(id, new { ok = true });
                    if (!mode.StartsWith("hang", StringComparison.Ordinal)) return 0;
                    break;
                case "status": await Reply(id, new { id = "fake-host", version = "0.9.0", name = "Fake service", settings = new HostSettings() }); break;
                case "echo": await Reply(id, root.GetProperty("params").Clone()); break;
                case "reject": await Reply(id, error: "This transfer still requires recipient approval."); break;
                case "slow":
                    var value = root.GetProperty("params").Clone();
                    _ = Task.Run(async () => { await Task.Delay(200); await Reply(id, value); }); break;
                case "very-slow": _ = Task.Run(async () => { await Task.Delay(1500); await Reply(id, new { ok = true }); }); break;
                case "offer": case "share.create": case "duplicate.resolve": _ = Task.Run(async () => { await Task.Delay(650); await Reply(id, new { ok = true }); }); break;
                case "crash": Console.Error.WriteLine("INTENTIONAL_CRASH"); return 35;
                case "oversized": Console.WriteLine(new string('x', 8192)); break;
                case "malformed": Console.WriteLine("not-json"); break;
                default: await Reply(id, new { ok = true }); break;
            }
        }
    }
}
