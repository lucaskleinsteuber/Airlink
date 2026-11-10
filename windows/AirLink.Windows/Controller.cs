using System.Text.Json;
using System;
using System.Collections.Generic;
using System.Collections.ObjectModel;
using System.ComponentModel;
using System.Globalization;
using System.IO;
using System.Linq;
using System.Reflection;
using System.Runtime.CompilerServices;
using System.Runtime.InteropServices;
using System.Security.Cryptography;
using System.Threading;
using System.Threading.Tasks;
using System.Windows.Threading;
using AirLink.Core;

namespace AirLink.Windows;

internal sealed class Controller : INotifyPropertyChanged
{
    private readonly Dispatcher dispatcher;
    private readonly DaemonClient client = new();
    private readonly CancellationTokenSource lifetime = new();
    private bool starting, stopping;
    private readonly StatusRefreshGate refreshes = new();
    private readonly TransferDraft draft = new();
    private string? helperPath;
    private string? pendingPeerFingerprint;
    private DateTimeOffset pendingPeerAt;
    private readonly DesktopNoticeTracker notices = new();
    private string? actionError, serviceError;
    public event PropertyChangedEventHandler? PropertyChanged;
    public event Action<DesktopNotice>? NoticeRaised;
    public HostStatus? Status { get; private set; }
    public bool Ready { get; private set; }
    public bool Starting => starting;
    public bool SetupComplete => Status?.Settings.SetupCompleted == true;
    public bool SetupRequired => Ready && !SetupComplete;
    public string SuggestedReceiveDirectory
    {
        get
        {
            var downloads = WindowsFolders.Downloads();
            var saved = Status?.Settings.ReceiveDirectory;
            var conventional = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), "Downloads");
            if (!SetupComplete && (string.Equals(saved, conventional, StringComparison.OrdinalIgnoreCase) || string.Equals(saved, Path.Combine(conventional, "AirLink"), StringComparison.OrdinalIgnoreCase))) return downloads;
            return SetupDefaults.SuggestReceiveDirectory(saved, downloads);
        }
    }
    public bool Sending => draft.IsSending;
    public bool RemovingHistory { get; private set; }
    public string? ActivePreparationId { get; private set; }
    public List<Transfer> History { get; private set; } = [];
    public string? HistoryCursor { get; private set; }
    public bool HistoryLoading { get; private set; }
    private long historyGeneration;
    private long toolGeneration;
    private readonly HashSet<string> observedTools = [];
    public string? Error => actionError ?? serviceError;
    public bool HasActionError => actionError is not null;
    public string? Notice { get; private set; }
    public string? SelectedDeviceId { get => draft.SelectedDeviceId; set { draft.SelectDevice(value); Changed(); } }
    public ObservableCollection<SendEntry> Selection => draft.Entries;
    public bool English => (Status?.Settings.Language ?? "system") switch
    {
        "en" => true, "de" => false,
        _ => CultureInfo.CurrentUICulture.TwoLetterISOLanguageName == "en"
    };
    public string T(string de, string en) => English ? en : de;
    public string DataDirectory => Path.Combine(Environment.GetEnvironmentVariable("APPDATA") ?? Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData), "AirLink");
    public Device? SelectedDevice => Status?.Devices.FirstOrDefault(d => d.Id == SelectedDeviceId);
    public int ActiveCount => Status is { Summary: true } summary ? summary.ActiveTransferCount + summary.ActiveRelayCount : (Status?.Transfers.Count(t => t.IsActive) ?? 0) + (Status?.Relays.Count(r => r.State is not ("delivered" or "cancelled" or "failed")) ?? 0);
    public string Version => Status?.Version ?? "0.12.0-beta.1";

    public Controller(Dispatcher dispatcher)
    {
        this.dispatcher = dispatcher;
        client.Exited += (_, e) => dispatcher.BeginInvoke(() =>
        {
            if (!e.Unexpected || (client.SessionId is { } current && e.SessionId != current) || (starting && client.SessionId is null)) return;
            Ready = false;
            serviceError = T("Der AirLink-Dienst wurde beendet. Du kannst ihn mit denselben Daten erneut starten.", "The AirLink service stopped. Retry to keep using the same data.") +
                (string.IsNullOrWhiteSpace(e.Diagnostic) ? "" : "\n" + e.Diagnostic);
            Changed();
        });
    }

    public async Task StartAsync(string? receiveOverride = null)
    {
        if (starting || stopping) return;
        refreshes.Invalidate();
        starting = true; Ready = false; serviceError = null; Changed();
        try
        {
            if (client.IsRunning && !await client.StopAsync()) throw new IOException(T("Der bisherige Dienst läuft noch.", "The previous service is still running."));
            helperPath ??= await ExtractHelperAsync();
            Directory.CreateDirectory(DataDirectory);
            await client.StartAsync(helperPath, DataDirectory, receiveOverride, lifetime.Token);
            await RefreshAsync();
            if (!Ready) throw new IOException(serviceError ?? T("Der Dienst antwortet noch nicht.", "The service is not responding."));
        }
        catch (Exception e) { serviceError = Friendly(e); Ready = false; }
        finally { starting = false; Changed(); }
    }

    private static async Task<string> ExtractHelperAsync()
    {
        var assembly = Assembly.GetExecutingAssembly();
        var version = assembly.GetName().Version?.ToString(4) ?? "0.12.0-beta.1";
        var cache = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "AirLink", "Runtime", version,
            RuntimeInformation.ProcessArchitecture.ToString().ToLowerInvariant());
        Stream Open() => assembly.GetManifestResourceStream("AirLink.Windows.Helper") ?? throw new FileNotFoundException("The bundled AirLink service is missing. Download the complete app.");
        using var resource = Open();
        var expected = Convert.ToHexStringLower(SHA256.HashData(resource));
        return await HelperExtractor.ExtractVerifiedAsync(Open, cache, expected);
    }

    public async Task PollAsync()
    {
        using var timer = new PeriodicTimer(TimeSpan.FromSeconds(1));
        try
        {
            while (await timer.WaitForNextTickAsync(lifetime.Token))
                if (client.IsRunning && !starting && !stopping) { await RefreshAsync(); await RunAsync(RefreshToolsAsync); }
        }
        catch (OperationCanceledException) { }
    }

    public async Task RefreshAsync(bool force = false)
    {
        if (stopping) return;
        var ticket = refreshes.Begin(force);
        if (ticket is null) return;
        try
        {
            var next = await client.RequestAsync<HostStatus>("status.summary", ct: lifetime.Token);
            if (!refreshes.IsCurrent(ticket.Value)) return;
            if (Status is { } previous) { next.Shares = previous.Shares; next.Statistics = previous.Statistics; if (observedTools.Contains("clipboard")) next.Clipboard = previous.Clipboard; }
            Status = next; Ready = true; serviceError = null;
            History = DesktopTransferSelection.MergeHistory(History, next.Transfers);
            draft.RefreshDevices(next.Devices);
            if (pendingPeerFingerprint is not null && next.Devices.FirstOrDefault(d => d.Paired && d.Kind == "engine" && string.Equals(d.Fingerprint, pendingPeerFingerprint, StringComparison.OrdinalIgnoreCase)) is { } paired)
            {
                pendingPeerFingerprint = null;
                if (SelectedDeviceId is null || SelectedDevice?.Fingerprint == paired.Fingerprint) SelectedDeviceId = paired.Id;
                Notice = T("Gerät verbunden. Du kannst Dateien senden.", "Device connected. You can send files.");
            }
            else if (pendingPeerFingerprint is not null && DateTimeOffset.UtcNow - pendingPeerAt >= TimeSpan.FromMinutes(5))
            {
                pendingPeerFingerprint = null;
                Notice = T("Die Kopplung wurde nicht bestätigt. Du kannst sie erneut anfragen.", "Pairing was not confirmed. You can request it again.");
            }
            foreach (var notice in notices.Observe(next)) NoticeRaised?.Invoke(notice);
            Changed();
        }
        catch (Exception e)
        {
            if (!refreshes.IsCurrent(ticket.Value)) return;
            serviceError = Friendly(e);
            if (!client.IsRunning) Ready = false;
            Changed();
        }
        finally { refreshes.Complete(ticket.Value); }
    }

    public async Task<bool> StopAsync()
    {
        if (stopping) return false;
        stopping = true; refreshes.Invalidate();
        try
        {
            var stopped = await client.StopAsync();
            if (stopped) { lifetime.Cancel(); Ready = false; Changed(); }
            return stopped;
        }
        finally { stopping = false; }
    }

    public async Task<bool> StopForUpdateAsync()
    {
        if (stopping) return false;
        stopping = true; refreshes.Invalidate();
        try
        {
            var stopped = await client.StopGracefullyAsync();
            // Keep the app lifetime alive until Setup's waiter actually starts. A
            // failed/cancelled installer launch can then retry the same helper data.
            if (stopped) { Ready = false; Changed(); }
            return stopped;
        }
        finally { stopping = false; }
    }
    public void CompleteUpdateShutdown() => lifetime.Cancel();

    public async Task RunAsync(Func<Task> action)
    {
        try { actionError = null; await action(); }
        catch (Exception e) { actionError = Friendly(e); }
        Changed();
    }
    public void DismissError() { actionError = null; Changed(); }
    public void ReportError(string message) { actionError = message; Changed(); }
    public async Task<T> RequestAsync<T>(string method, object? parameters = null)
    {
        RequireReady();
        var mutating = IsToolMutation(method); if (mutating) toolGeneration++;
        try { return await client.RequestAsync<T>(method, parameters, lifetime.Token); }
        finally { if (mutating) toolGeneration++; }
    }
    public async Task<System.Text.Json.JsonElement> RequestAsync(string method, object? parameters = null)
    {
        RequireReady();
        var mutating = IsToolMutation(method); if (mutating) toolGeneration++;
        try { return await client.RequestAsync(method, parameters, lifetime.Token); }
        finally { if (mutating) toolGeneration++; }
    }

    private static bool IsToolMutation(string method) => method is "share.create" or "share.revoke" or "clipboard.send" or "clipboard.dismiss";

    public async Task CommandAsync(string method, string id)
    {
        RequireReady();
        toolGeneration++; historyGeneration++;
        await client.RequestAsync(method, new { id }, lifetime.Token);
        if (method == "history.remove") History.RemoveAll(t => t.Id == id);
        await RefreshAsync(force: true);
    }

    public async Task RemoveHistoryAsync(IEnumerable<Transfer> selection)
    {
        RequireReady();
        if (RemovingHistory) return;
        var plan = new HistoryRemovalPlan(selection);
        if (plan.Ids.Count == 0) return;
        RemovingHistory = true; Changed();
        var removed = 0;
        try
        {
            historyGeneration++;
            removed = await plan.ExecuteAsync(() => DesktopTransferSelection.MergeHistory(History, Status?.Transfers ?? []), async id => { await client.RequestAsync("history.remove", new { id }, lifetime.Token); History.RemoveAll(t => t.Id == id); }, lifetime.Token);
            Notice = removed == 1 ? T("Eintrag entfernt. Deine Dateien bleiben erhalten.", "Entry removed. Your files are preserved.") :
                T($"{removed} Einträge entfernt. Deine Dateien bleiben erhalten.", $"{removed} entries removed. Your files are preserved.");
        }
        catch (HistoryRemovalConflictException)
        {
            throw new InvalidOperationException(T("Eine Übertragung ist wieder aktiv und bleibt in der Liste. Bereits entfernte Einträge bleiben entfernt.",
                "A transfer is active again and remains in the list. Entries already removed stay removed."));
        }
        finally { await RefreshAsync(force: true); RemovingHistory = false; Changed(); }
    }

    public void ObserveTool(string name, bool active) { toolGeneration++; if (active) { observedTools.Add(name); _ = RunAsync(RefreshToolsAsync); } else observedTools.Remove(name); }
    public async Task RefreshToolsAsync() {
        if (!Ready) return;
        var revision = ++toolGeneration;
        foreach (var name in observedTools.ToArray()) {
            var method = name == "shares" ? "share.list" : name == "clipboard" ? "clipboard.list" : "statistics.get";
            var data = await client.RequestAsync(method, ct: lifetime.Token);
            if (revision != toolGeneration || !observedTools.Contains(name) || Status is null) continue;
            if (name == "shares") Status.Shares = data.Deserialize<List<Share>>(ProtocolJson.Options) ?? [];
            else if (name == "clipboard") Status.Clipboard = data.Deserialize<List<ClipboardMessage>>(ProtocolJson.Options) ?? [];
            else Status.Statistics = data.Deserialize<Statistics>(ProtocolJson.Options) ?? new();
            Changed();
        }
    }
    public async Task LoadHistoryAsync(bool more = false) {
        if (HistoryLoading) return;
        HistoryLoading = true; var generation = ++historyGeneration; Changed();
        try {
            var page = await RequestAsync<TransferPage>("history.page", new { limit = 50, cursor = more ? HistoryCursor : null });
            if (generation != historyGeneration) return;
            History = DesktopTransferSelection.MergeHistory((more ? History : []).Concat(page.Transfers).DistinctBy(t => t.Id), Status?.Transfers ?? []);
            HistoryCursor = page.NextCursor;
        } finally { HistoryLoading = false; Changed(); }
    }
    public Task<PairingSession> StartPairingAsync(string url) => RequestAsync<PairingSession>("pairing.start", new { url });
    public async Task ConfirmPairingAsync(string id, bool automaticReceive) { await RequestAsync("pairing.confirm", new { id, automaticReceive }); await RefreshAsync(true); }
    public void UpdateAvailable(string version) { Notice = T("Neue Beta verfügbar: ", "New beta available: ") + version + T(" · Einstellungen → Updates", " · Settings → Updates"); NoticeRaised?.Invoke(new(DesktopNoticeKind.Update, version, "AirLink", version)); Changed(); }

    public async Task SetAutomaticReceiveAsync(string deviceId, bool automatic)
    {
        RequireReady();
        var current = Status?.Devices.FirstOrDefault(d => d.Id == deviceId);
        if (current is not { Paired: true, CanSend: true })
            throw new InvalidOperationException(T("Dieses Gerät muss verbunden sein und Dateien senden dürfen.", "This device must be paired and allowed to send files."));
        var copy = ProtocolJson.Clone(current); copy.AutoAccept = automatic;
        await SaveDeviceAsync(copy);
    }

    public async Task ConnectAsync(string address, string fingerprint, bool receiveAutomatically)
    {
        RequireReady();
        var draft = new ConnectionDraft(address, fingerprint, receiveAutomatically);
        if (!draft.IsValid)
            throw new ArgumentException(T("Eine HTTPS-Adresse und den geprüften Fingerabdruck mit 64 Zeichen eingeben.", "Enter an HTTPS address and the verified 64-character fingerprint."));
        var url = draft.NormalizedAddress;
        var fp = draft.NormalizedFingerprint;
        await client.RequestAsync(receiveAutomatically ? "peer.connectTrusted" : "peer.connect", new { url, fingerprint = fp }, lifetime.Token);
        pendingPeerFingerprint = fp;
        pendingPeerAt = DateTimeOffset.UtcNow;
        Notice = T("Kopplung angefragt. Bestätige sie am anderen Gerät.", "Pairing requested. Approve it on the other device.");
        Changed();
    }

    public void AddPaths(IEnumerable<string> paths)
    {
        draft.AddPaths(ExistingPaths(paths));
        Changed();
    }

    private string[] ExistingPaths(IEnumerable<string> paths)
    {
        var full = paths.Select(Path.GetFullPath).Distinct(StringComparer.OrdinalIgnoreCase).ToArray();
        if (full.Length == 0 || full.Any(p => !File.Exists(p) && !Directory.Exists(p)))
            throw new IOException(T("Eine ausgewählte Datei ist nicht mehr verfügbar. Bitte erneut auswählen.", "A selected file is no longer available. Please select it again."));
        return full;
    }

    public async Task SendPathsAsync(IEnumerable<string> paths, string? deviceId = null, bool useCurrentSelection = true)
    {
        RequireReady();
        RequireSetup();
        var files = ExistingPaths(paths);
        var target = deviceId ?? (useCurrentSelection ? SelectedDeviceId : null);
        if (Sending) throw new IOException(T("Die aktuelle Auswahl wird gerade vorbereitet. Bitte danach erneut ablegen.", "The current selection is being prepared. Please drop the next files when it is ready."));
        if (target is null || !TransferDraft.CanSendTo(Status?.Devices.FirstOrDefault(d => d.Id == target)))
        {
            draft.AddPaths(files);
            Notice = T("Dateien bereit. Wähle ein gekoppeltes Gerät und klicke auf Senden.", "Files ready. Choose a paired device and click Send.");
            Changed(); return;
        }
        var submitted = draft.TryBeginDirect(target, files, Status?.Devices ?? [], Ready);
        if (submitted is not null) await SubmitAsync(submitted);
    }

    public async Task SendAsync()
    {
        RequireReady();
        RequireSetup();
        var submitted = draft.TryBegin(Status?.Devices ?? [], Ready);
        if (submitted is not null) await SubmitAsync(submitted);
    }
    private async Task SubmitAsync(SendSubmission submitted)
    {
        Changed();
        try
        {
            var job = await client.RequestAsync<Preparation>("preparation.start", new { deviceId = submitted.DeviceId, paths = submitted.Paths, requestId = submitted.Token.ToString("N") }, lifetime.Token);
            ActivePreparationId = job.Id; Changed();
            while (job.State == "preparing") {
                await Task.Delay(150, lifetime.Token);
                job = await client.RequestAsync<Preparation>("preparation.status", new { id = job.Id }, lifetime.Token);
            }
            if (job.State != "completed") throw new IOException(job.Error ?? T("Vorbereitung abgebrochen. Deine Auswahl bleibt erhalten.", "Preparation cancelled. Your selection is preserved."));
            draft.FinishSuccess(submitted);
            Notice = T("Übertragung angelegt.", "Transfer created.");
            await RefreshAsync(force: true);
        }
        catch { draft.Fail(submitted); throw; }
        finally { ActivePreparationId = null; Changed(); }
    }

    public async Task<HostSettings> SaveSettingsAsync(HostSettings settings)
    {
        RequireReady();
        var saved = await client.RequestAsync<HostSettings>("settings.update", settings, lifetime.Token);
        await RefreshAsync(force: true);
        return saved;
    }
    public async Task CompleteSetupAsync(string name, string folder, bool? notifications = null, bool? automaticUpdateChecks = null)
    {
        RequireReady();
        try { name = SetupDefaults.NormalizeName(name); }
        catch (ArgumentException) { throw new ArgumentException(T("Bitte einen Gerätenamen mit höchstens 120 UTF-8 Bytes ohne Steuerzeichen eingeben.", "Enter a device name of at most 120 UTF-8 bytes without control characters.")); }
        if (!Path.IsPathFullyQualified(folder.Trim())) throw new ArgumentException(T("Bitte einen vollständigen Empfangsordner wählen.", "Choose an absolute receive folder."));
        var settings = SetupDefaults.Complete(Status!.Settings, name, folder, notifications);
        if (automaticUpdateChecks is not null) settings.AutomaticUpdateChecks = automaticUpdateChecks.Value;
        await SaveSettingsAsync(settings);
    }

    public async Task SaveDeviceAsync(Device device)
    {
        RequireReady();
        await client.RequestAsync("device.update", device, lifetime.Token);
        await RefreshAsync(force: true);
    }

    public async Task<ReceivedResult> GetReceivedAsync(string id)
    {
        RequireReady();
        return await client.RequestAsync<ReceivedResult>("transfer.received", new { id }, lifetime.Token);
    }

    public async Task RevealAsync(Transfer transfer, string? fileId = null, bool openFolder = false)
    {
        var result = await GetReceivedAsync(transfer.Id);
        if (openFolder) Shell.OpenFolder(result.Directory);
        else if (fileId is not null) Shell.Reveal(result.Items.FirstOrDefault(i => i.FileId == fileId)?.Path ?? throw new IOException(T("Die empfangene Datei ist nicht mehr verfügbar.", "The received file is no longer available.")));
        else if (result.Items.Count == 1) Shell.Reveal(result.Items[0].Path);
        else Shell.OpenFolder(result.Directory);
    }

    public async Task ResolveAsync(Transfer transfer, TransferFile file, string action, string? candidate = null)
    {
        RequireReady();
        await client.RequestAsync<Transfer>("duplicate.resolve", new { transferId = transfer.Id, fileId = file.Id, action, candidateId = candidate }, lifetime.Token);
        await RefreshAsync(force: true);
    }

    public async Task RevealCandidateAsync(string candidateId)
    {
        RequireReady();
        var result = await client.RequestAsync("duplicate.path", new { candidateId }, lifetime.Token);
        Shell.Reveal(result.GetProperty("path").GetString() ?? throw new IOException(T("Datei nicht mehr verfügbar.", "File is no longer available.")));
    }

    public void OpenReceiveFolder()
    {
        if (Status is not null) Shell.OpenFolder(Status.Settings.ReceiveDirectory);
    }

    public string Bytes(long count)
    {
        double value = Math.Max(0, count); string[] units = ["B", "KB", "MB", "GB", "TB", "PB"];
        var unit = 0; while (value >= 1000 && unit < units.Length - 1) { value /= 1000; unit++; }
        return value.ToString(unit == 0 ? "0" : "0.#", CultureInfo.GetCultureInfo(English ? "en-US" : "de-DE")) + " " + units[unit];
    }

    public string StateText(string state) => state switch
    {
        "offered" => T("Wartet auf Annahme", "Waiting for acceptance"),
        "queued" => T("In der Warteschlange", "Queued"),
        "running" => T("Wird übertragen", "Transferring"),
        "paused" => T("Pausiert", "Paused"),
        "reconnecting" => T("Verbindung unterbrochen · wird fortgesetzt", "Connection interrupted · reconnecting"),
        "verifying" => T("Datei wird geprüft", "Verifying file"),
        "checkingDuplicate" => T("Vorhandene Datei wird geprüft", "Checking existing file"),
        "waitingDuplicate" => T("Duplikat benötigt eine Entscheidung", "Duplicate needs a decision"),
        "completed" => T("Fertig", "Completed"), "failed" => T("Benötigt Aufmerksamkeit", "Needs attention"),
        "cancelled" => T("Abgebrochen", "Cancelled"), _ => state
    };

    private string Friendly(Exception e)
    {
        var message = e.Message;
        if (message.Contains("already running with this data directory", StringComparison.OrdinalIgnoreCase))
            return T("AirLink läuft bereits. Schließe die bisherige AirLink-Konsole mit Strg+C und wähle dann Erneut starten. Deine Geräte und Dateien bleiben erhalten.", "AirLink is already running. Stop the previous AirLink console with Ctrl+C, then retry. Your devices and files are preserved.");
        return message;
    }
    private void RequireReady() { if (!Ready || starting || stopping) throw new IOException(T("Der AirLink-Dienst ist nicht bereit. Bitte erneut starten.", "The AirLink service is not ready. Please retry.")); }
    private void RequireSetup() { if (!SetupComplete) throw new IOException(T("Lege zuerst deinen Gerätenamen und Empfangsordner in der Einrichtung fest.", "First choose your device name and receive folder in setup.")); }
    private void Changed([CallerMemberName] string? property = null) => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(property));
}
