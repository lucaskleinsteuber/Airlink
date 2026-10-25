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
    private bool refreshing, starting, sending;
    private string? helperPath;
    private string? pendingPeerFingerprint;
    private DateTimeOffset pendingPeerAt;
    private readonly HashSet<string> announced = [];
    public event PropertyChangedEventHandler? PropertyChanged;
    public event Action<Transfer>? Received;
    public HostStatus? Status { get; private set; }
    public bool Ready { get; private set; }
    public bool Starting => starting;
    public bool Sending => sending;
    public string? Error { get; private set; }
    public string? Notice { get; private set; }
    public string? SelectedDeviceId { get; set; }
    public ObservableCollection<SelectionItem> Selection { get; } = [];
    public bool English => (Status?.Settings.Language ?? "system") switch
    {
        "en" => true, "de" => false,
        _ => CultureInfo.CurrentUICulture.TwoLetterISOLanguageName == "en"
    };
    public string T(string de, string en) => English ? en : de;
    public string DataDirectory => Path.Combine(Environment.GetEnvironmentVariable("APPDATA") ?? Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData), "AirLink");
    public Device? SelectedDevice => Status?.Devices.FirstOrDefault(d => d.Id == SelectedDeviceId);
    public int ActiveCount => (Status?.Transfers.Count(t => t.IsActive) ?? 0) + (Status?.Relays.Count(r => r.State is not ("delivered" or "cancelled" or "failed")) ?? 0);
    public string Version => Status?.Version ?? "0.6.0";

    public Controller(Dispatcher dispatcher)
    {
        this.dispatcher = dispatcher;
        client.Exited += (_, e) => dispatcher.BeginInvoke(() =>
        {
            if (!e.Unexpected || (client.SessionId is { } current && e.SessionId != current) || (starting && client.SessionId is null)) return;
            Ready = false;
            Error = T("Der AirLink-Dienst wurde beendet. Du kannst ihn mit denselben Daten erneut starten.", "The AirLink service stopped. Retry to keep using the same data.") +
                (string.IsNullOrWhiteSpace(e.Diagnostic) ? "" : "\n" + e.Diagnostic);
            Changed();
        });
    }

    public async Task StartAsync(string? receiveOverride = null)
    {
        if (starting) return;
        starting = true; Ready = false; Error = null; Changed();
        try
        {
            if (client.IsRunning && !await client.StopAsync()) throw new IOException(T("Der bisherige Dienst läuft noch.", "The previous service is still running."));
            helperPath ??= await ExtractHelperAsync();
            Directory.CreateDirectory(DataDirectory);
            await client.StartAsync(helperPath, DataDirectory, receiveOverride, lifetime.Token);
            await RefreshAsync();
            if (!Ready) throw new IOException(Error ?? T("Der Dienst antwortet noch nicht.", "The service is not responding."));
        }
        catch (Exception e) { Error = Friendly(e); Ready = false; }
        finally { starting = false; Changed(); }
    }

    private static async Task<string> ExtractHelperAsync()
    {
        var assembly = Assembly.GetExecutingAssembly();
        var version = assembly.GetName().Version?.ToString(3) ?? "0.6.0";
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
                if (client.IsRunning && !starting) await RefreshAsync();
        }
        catch (OperationCanceledException) { }
    }

    public async Task RefreshAsync()
    {
        if (refreshing) return;
        refreshing = true;
        try
        {
            var next = await client.RequestAsync<HostStatus>("status", ct: lifetime.Token);
            var previous = Status;
            Status = next; Ready = true; Error = null;
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
            foreach (var transfer in next.Transfers.Where(t => t.Direction == "receive" && t.State == "completed"))
                if (previous?.Transfers.Any(t => t.Id == transfer.Id && t.State != "completed") == true && announced.Add(transfer.Id)) Received?.Invoke(transfer);
            Changed();
        }
        catch (Exception e)
        {
            Error = Friendly(e);
            if (!client.IsRunning) Ready = false;
            Changed();
        }
        finally { refreshing = false; }
    }

    public async Task<bool> StopAsync()
    {
        var stopped = await client.StopAsync();
        if (stopped) { lifetime.Cancel(); Ready = false; Changed(); }
        return stopped;
    }

    public async Task RunAsync(Func<Task> action)
    {
        try { Error = null; await action(); }
        catch (Exception e) { Error = Friendly(e); }
        Changed();
    }

    public async Task CommandAsync(string method, string id)
    {
        RequireReady();
        await client.RequestAsync(method, new { id }, lifetime.Token);
        await RefreshAsync();
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
        foreach (var path in paths)
        {
            var full = Path.GetFullPath(path);
            if (!(File.Exists(full) || Directory.Exists(full))) continue;
            if (!Selection.Any(p => string.Equals(p.Path, full, StringComparison.OrdinalIgnoreCase))) Selection.Add(new SelectionItem(full));
        }
        Changed();
    }

    public async Task SendAsync()
    {
        RequireReady();
        var device = SelectedDevice;
        if (sending || device is not { Paired: true, CanReceive: true } || Selection.Count == 0) return;
        var submitted = Selection.ToArray();
        var deviceId = device.Id;
        sending = true; Changed();
        try
        {
            await client.RequestAsync<Transfer>("offer", new { deviceId, paths = submitted.Select(p => p.Path).ToArray() }, lifetime.Token);
            foreach (var item in submitted) Selection.Remove(item);
            Notice = T("Übertragung angelegt.", "Transfer created.");
            await RefreshAsync();
        }
        finally { sending = false; Changed(); }
    }

    public async Task SaveSettingsAsync(HostSettings settings)
    {
        RequireReady();
        await client.RequestAsync<HostSettings>("settings.update", settings, lifetime.Token);
        await RefreshAsync();
    }

    public async Task SaveDeviceAsync(Device device)
    {
        RequireReady();
        await client.RequestAsync("device.update", device, lifetime.Token);
        await RefreshAsync();
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
        await RefreshAsync();
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
    private void RequireReady() { if (!Ready) throw new IOException(T("Der AirLink-Dienst ist nicht bereit. Bitte erneut starten.", "The AirLink service is not ready. Please retry.")); }
    private void Changed([CallerMemberName] string? property = null) => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(property));
}

internal sealed record SelectionItem(string Path)
{
    public Guid Id { get; } = Guid.NewGuid();
    public string Title => System.IO.Path.GetFileName(Path.TrimEnd(System.IO.Path.DirectorySeparatorChar));
}
