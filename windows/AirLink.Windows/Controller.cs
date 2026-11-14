using System;
using System.Collections.Generic;
using System.ComponentModel;
using System.Globalization;
using System.IO;
using System.Linq;
using System.Reflection;
using System.Runtime.CompilerServices;
using System.Runtime.InteropServices;
using System.Security.Cryptography;
using System.Text.Json;
using System.Threading;
using System.Threading.Tasks;
using System.Windows.Threading;
using AirLink.Core;
using Microsoft.Win32;

namespace AirLink.Windows;

/// <summary>State and actions of the app. The service pushes "status" events;
/// a slow refresh only catches what has no event.</summary>
internal sealed class Controller : INotifyPropertyChanged
{
    private readonly Dispatcher dispatcher;
    private readonly DaemonClient client = new();
    private readonly CancellationTokenSource lifetime = new();
    private bool starting, stopping, refreshAgain;
    private readonly StatusRefreshGate refreshes = new();
    private string? helperPath;
    private readonly DesktopNoticeTracker notices = new();
    private string? serviceError;
    private bool pairingOpen;
    private Timer? pairingTimer;
    public event PropertyChangedEventHandler? PropertyChanged;
    public event Action<DesktopNotice>? NoticeRaised;
    public HostStatus? Status { get; private set; }
    public bool Ready { get; private set; }
    public bool Demo { get; }
    public bool Starting => starting;
    public bool SetupComplete => Status?.Settings.SetupCompleted == true;
    public string? ServiceError => serviceError;
    public HashSet<string> Preparing { get; } = [];
    public Dictionary<string, string> ClipboardTexts { get; } = [];
    public bool English => (Status?.Settings.Language ?? "system") switch
    {
        "en" => true, "de" => false,
        _ => CultureInfo.CurrentUICulture.TwoLetterISOLanguageName != "de"
    };
    public string T(string de, string en) => English ? en : de;
    public string DataDirectory => Environment.GetEnvironmentVariable("AIRLINK_DATA_DIR") is { Length: > 0 } custom && Path.IsPathFullyQualified(custom) ? custom
        : Path.Combine(Environment.GetEnvironmentVariable("APPDATA") ?? Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData), "AirLink");
    public int ActiveCount => Status?.Transfers.Count(t => t.IsActive) ?? 0;
    public string Version => Status?.Version ?? Assembly.GetExecutingAssembly().GetName().Version?.ToString(3) ?? "";
    public IReadOnlyList<Device> PairedDevices => (Status?.Devices ?? []).Where(d => d.Paired && d.Kind == "engine")
        .OrderByDescending(d => d.Online).ThenBy(d => d.Name, StringComparer.CurrentCultureIgnoreCase).ToList();
    public IReadOnlyList<Device> NearbyDevices => (Status?.Devices ?? []).Where(d => !d.Paired && d.Kind == "engine" && !string.IsNullOrEmpty(d.Address))
        .OrderBy(d => d.Name, StringComparer.CurrentCultureIgnoreCase).ToList();
    public IEnumerable<string> LocalAddresses => (Status?.Urls ?? []).Select(u => Uri.TryCreate(u, UriKind.Absolute, out var uri) ? uri.Host : "")
        .Where(h => h.Length > 0 && h != "localhost" && !h.Contains(':') && !h.StartsWith('['));

    public Controller(Dispatcher dispatcher, bool demo = false)
    {
        this.dispatcher = dispatcher;
        Demo = demo;
        client.EventReceived += name => { if (name == "status") dispatcher.BeginInvoke(() => _ = RefreshAsync()); };
        client.Exited += (_, e) => dispatcher.BeginInvoke(() =>
        {
            if (!e.Unexpected || (client.SessionId is { } current && e.SessionId != current) || (starting && client.SessionId is null)) return;
            Ready = false;
            serviceError = T("Der AirLink-Dienst wurde beendet.", "The AirLink service stopped.") + (string.IsNullOrWhiteSpace(e.Diagnostic) ? "" : "\n" + e.Diagnostic);
            Changed();
        });
    }

    public void ShowDemo(HostStatus status) { Status = status; Ready = true; serviceError = null; Changed(); }

    public async Task StartAsync(string? receiveOverride = null)
    {
        if (Demo || starting || stopping) return;
        refreshes.Invalidate();
        starting = true; Ready = false; serviceError = null; Changed();
        try
        {
            if (client.IsRunning && !await client.StopAsync()) throw new IOException(T("Der bisherige Dienst läuft noch.", "The previous service is still running."));
            helperPath ??= await ExtractHelperAsync();
            Directory.CreateDirectory(DataDirectory);
            await client.StartAsync(helperPath, DataDirectory, receiveOverride, lifetime.Token);
            await RefreshAsync(force: true);
            if (!Ready) throw new IOException(serviceError ?? T("Der Dienst antwortet noch nicht.", "The service is not responding."));
            SetPairingWindow(pairingOpen, force: true);
        }
        catch (Exception e) { serviceError = Friendly(e); Ready = false; }
        finally { starting = false; Changed(); }
    }

    private static async Task<string> ExtractHelperAsync()
    {
        var assembly = Assembly.GetExecutingAssembly();
        var version = assembly.GetName().Version?.ToString(4) ?? "0.13.0.0";
        var cache = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "AirLink", "Runtime", version,
            RuntimeInformation.ProcessArchitecture.ToString().ToLowerInvariant());
        Stream Open() => assembly.GetManifestResourceStream("AirLink.Windows.Helper") ?? throw new FileNotFoundException("The bundled AirLink service is missing. Download the complete app.");
        using var resource = Open();
        var expected = Convert.ToHexStringLower(SHA256.HashData(resource));
        return await HelperExtractor.ExtractVerifiedAsync(Open, cache, expected);
    }

    /// <summary>Safety net only; changes arrive as events.</summary>
    public async Task FallbackRefreshAsync()
    {
        using var timer = new PeriodicTimer(TimeSpan.FromSeconds(15));
        try
        {
            while (await timer.WaitForNextTickAsync(lifetime.Token))
                if (client.IsRunning && !starting && !stopping) await RefreshAsync();
        }
        catch (OperationCanceledException) { }
    }

    public async Task RefreshAsync(bool force = false)
    {
        if (stopping || Demo) return;
        var ticket = refreshes.Begin(force);
        if (ticket is null) { refreshAgain = true; return; }
        try
        {
            var next = await client.RequestAsync<HostStatus>("status.summary", ct: lifetime.Token);
            if (!refreshes.IsCurrent(ticket.Value)) return;
            Status = next; Ready = true; serviceError = null;
            foreach (var notice in notices.Observe(next)) NoticeRaised?.Invoke(notice);
            if (next.Clipboard.Any(m => m.Direction == "receive" && !ClipboardTexts.ContainsKey(m.Id))) _ = LoadClipboardAsync();
            Changed();
        }
        catch (Exception e)
        {
            if (!refreshes.IsCurrent(ticket.Value)) return;
            serviceError = Friendly(e);
            if (!client.IsRunning) Ready = false;
            Changed();
        }
        finally
        {
            refreshes.Complete(ticket.Value);
            if (refreshAgain && !stopping) { refreshAgain = false; _ = RefreshAsync(); }
        }
    }

    private async Task LoadClipboardAsync()
    {
        try
        {
            var messages = (await client.RequestAsync("clipboard.list", ct: lifetime.Token)).Deserialize<List<ClipboardMessage>>(ProtocolJson.Options) ?? [];
            foreach (var message in messages.Where(m => m.Direction == "receive")) ClipboardTexts.TryAdd(message.Id, message.Text);
            Changed(nameof(ClipboardTexts));
        }
        catch (Exception) { }
    }

    /// <summary>Other computers may start pairing only while the window is open.</summary>
    public void SetPairingWindow(bool open, bool force = false)
    {
        if (open == pairingOpen && !force) return;
        pairingOpen = open;
        pairingTimer?.Dispose(); pairingTimer = null;
        if (Demo || !Ready) return;
        if (open)
            pairingTimer = new Timer(_ => dispatcher.BeginInvoke(async () => { try { if (Ready) await client.RequestAsync("pairing.listen", ct: lifetime.Token); } catch (Exception) { } }), null, TimeSpan.Zero, TimeSpan.FromSeconds(60));
        else
            _ = Task.Run(async () => { try { await client.RequestAsync("pairing.listen", new { close = true }, lifetime.Token); } catch (Exception) { } });
    }

    public async Task<bool> StopAsync()
    {
        if (Demo) return true;
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
            if (stopped) { Ready = false; Changed(); }
            return stopped;
        }
        finally { stopping = false; }
    }
    public void CompleteUpdateShutdown() => lifetime.Cancel();

    public async Task<T> RequestAsync<T>(string method, object? parameters = null)
    {
        RequireReady();
        return await client.RequestAsync<T>(method, parameters, lifetime.Token);
    }
    public async Task<JsonElement> RequestAsync(string method, object? parameters = null)
    {
        RequireReady();
        return await client.RequestAsync(method, parameters, lifetime.Token);
    }
    public async Task CommandAsync(string method, string id)
    {
        await RequestAsync(method, new { id });
        await RefreshAsync(force: true);
    }

    /// <summary>Sends files or folders to a paired computer right away.</summary>
    public async Task SendAsync(IEnumerable<string> paths, Device device)
    {
        RequireReady();
        var files = paths.Select(Path.GetFullPath).Distinct(StringComparer.OrdinalIgnoreCase).ToArray();
        if (files.Length == 0) return;
        if (files.Any(p => !File.Exists(p) && !Directory.Exists(p)))
            throw new IOException(T("Eine ausgewählte Datei ist nicht mehr da. Bitte noch einmal auswählen.", "A selected file is no longer there. Please choose it again."));
        if (!TransferDraft.CanSendTo(device))
            throw new IOException(T($"{device.Name} darf von diesem PC keine Dateien empfangen. Du kannst das in den Einstellungen unter „Geräte“ ändern.", $"{device.Name} may not receive files from this PC. You can change this in Settings under Devices."));
        Preparing.Add(device.Id); Changed();
        try
        {
            var job = await client.RequestAsync<Preparation>("preparation.start", new { deviceId = device.Id, paths = files, requestId = Guid.NewGuid().ToString("N") }, lifetime.Token);
            while (job.State == "preparing")
            {
                await Task.Delay(120, lifetime.Token);
                job = await client.RequestAsync<Preparation>("preparation.status", new { id = job.Id }, lifetime.Token);
            }
            if (job.State != "completed") throw new IOException(job.Error ?? T("Das Senden wurde abgebrochen.", "Sending was cancelled."));
        }
        finally { Preparing.Remove(device.Id); await RefreshAsync(force: true); Changed(); }
    }

    public async Task<string> ConnectAsync(Device device)
    {
        if (device.Outdated) throw new IOException(T($"{device.Name} hat eine ältere AirLink-Version. Aktualisiere AirLink dort auf 0.13 oder neuer.", $"{device.Name} runs an older AirLink. Update AirLink there to 0.13 or later."));
        var session = await RequestAsync<PairingSession>("pairing.start", new { url = device.Address, fingerprint = device.Fingerprint, name = device.Name, platform = device.Platform, form = device.Form });
        await RefreshAsync(force: true);
        return session.Id;
    }
    public async Task<string> ConnectAddressAsync(string address)
    {
        PairingSession session;
        try { session = await RequestAsync<PairingSession>("pairing.start", new { address }); }
        catch (DaemonException) { throw new IOException(T("Diese Adresse sieht nicht richtig aus. Gib die IP-Adresse ein, die AirLink auf dem anderen Computer unter „Hilfe“ anzeigt, z. B. 192.168.178.22.", "This address does not look right. Enter the IP address AirLink shows under Help on the other computer, for example 192.168.178.22.")); }
        await RefreshAsync(force: true);
        return session.Id;
    }
    public async Task ConfirmPairingAsync(string id, bool automaticReceive) { await RequestAsync("pairing.confirm", new { id, automaticReceive }); await RefreshAsync(true); }
    public async Task CancelPairingAsync(string id) { try { await RequestAsync("pairing.cancel", new { id }); } catch (Exception) { } await RefreshAsync(true); }

    public async Task SaveDeviceAsync(Device device) { await RequestAsync("device.update", device); await RefreshAsync(force: true); }
    public async Task UpdateSettingsAsync(Action<HostSettings> change)
    {
        RequireReady();
        var next = ProtocolJson.Clone(Status!.Settings);
        change(next);
        await client.RequestAsync<HostSettings>("settings.update", next, lifetime.Token);
        await RefreshAsync(force: true);
    }

    public async Task SendClipboardAsync(Device device, string text)
    {
        if (string.IsNullOrEmpty(text) || System.Text.Encoding.UTF8.GetByteCount(text) > 64 * 1024)
            throw new IOException(T("In der Zwischenablage ist kein passender Text. Kopiere zuerst einen Text (Strg+C).", "There is no suitable text on the clipboard. Copy some text first (Ctrl+C)."));
        if (!device.CanClipboard) throw new IOException(T($"Zwischenablage teilen ist für {device.Name} ausgeschaltet.", $"Clipboard sharing is off for {device.Name}."));
        await RequestAsync("clipboard.send", new { deviceId = device.Id, text });
    }
    public async Task DismissClipboardAsync(string id)
    {
        ClipboardTexts.Remove(id);
        try { await RequestAsync("clipboard.dismiss", new { id }); } catch (Exception) { }
        await RefreshAsync(force: true);
    }

    public async Task RevealAsync(Transfer transfer)
    {
        var result = await RequestAsync<ReceivedResult>("transfer.received", new { id = transfer.Id });
        var items = result.Items.Where(i => File.Exists(i.Path) || Directory.Exists(i.Path)).ToList();
        if (items.Count >= 1) Shell.Reveal(items[0].Path);
        else Shell.OpenFolder(result.Directory);
    }
    public void UpdateAvailable(string version) => NoticeRaised?.Invoke(new(DesktopNoticeKind.Update, version, "AirLink", version));
    public void OpenReceiveFolder() { if (Status is not null) Shell.OpenFolder(Status.Settings.ReceiveDirectory); }

    private const string RunKey = @"Software\Microsoft\Windows\CurrentVersion\Run";
    public bool Autostart
    {
        get { try { using var key = Registry.CurrentUser.OpenSubKey(RunKey); return key?.GetValue("AirLink") is string; } catch (Exception) { return false; } }
        set
        {
            if (Demo) return;
            using var key = Registry.CurrentUser.CreateSubKey(RunKey);
            if (value) key.SetValue("AirLink", "\"" + Environment.ProcessPath + "\" --background");
            else key.DeleteValue("AirLink", false);
            Changed();
        }
    }

    public string Bytes(long count)
    {
        double value = Math.Max(0, count); string[] units = ["B", "KB", "MB", "GB", "TB", "PB"];
        var unit = 0; while (value >= 1000 && unit < units.Length - 1) { value /= 1000; unit++; }
        return value.ToString(unit == 0 ? "0" : "0.#", CultureInfo.GetCultureInfo(English ? "en-US" : "de-DE")) + " " + units[unit];
    }

    private string Friendly(Exception e)
    {
        var message = e.Message;
        if (message.Contains("already running with this data directory", StringComparison.OrdinalIgnoreCase))
            return T("AirLink läuft bereits. Beende die andere AirLink-App und versuch es noch einmal.", "AirLink is already running. Quit the other AirLink app and try again.");
        return message;
    }
    private void RequireReady()
    {
        if (Demo) throw new IOException(T("Das ist eine Vorschau mit Beispieldaten.", "This is a preview with sample data."));
        if (!Ready || starting || stopping) throw new IOException(T("AirLink startet noch. Einen Moment bitte.", "AirLink is still starting. One moment please."));
    }
    private void Changed([CallerMemberName] string? property = null) => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(property));
}
