using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Reflection;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Documents;
using System.Windows.Interop;
using System.Windows.Media;
using System.Windows.Media.Imaging;
using AirLink.Core;
using Microsoft.Win32;

namespace AirLink.Windows;

/// <summary>The main window: welcome on first start, otherwise the computers
/// as round tiles and what happened recently.</summary>
internal sealed class MainWindow : Window
{
    private readonly Controller c;
    private readonly WindowsUpdater updater;
    private readonly Grid pages = new();
    private readonly StackPanel startup = new() { VerticalAlignment = VerticalAlignment.Center, HorizontalAlignment = HorizontalAlignment.Center, MaxWidth = 460 };
    private ScrollViewer welcome = null!, home = null!;
    private readonly StackPanel banners = new(), recentList = new();
    private readonly WrapPanel tiles = new();
    private readonly TextBlock homeTitle = Theme.Text("", 24, true), homeSubtitle = Theme.Text("", 14, secondary: true);
    private TextBlock ipLink = null!;
    private FrameworkElement recentArea = null!;
    private Button sendButton = null!, historyButton = null!;
    private TextBox welcomeName = null!;
    private TextBlock welcomeFolder = null!;
    private string welcomePath = "";
    private CheckBox welcomeAutostart = null!, welcomeNotify = null!;
    private bool welcomeFilled, quitting, rendering, builtEnglish;
    private List<string> tileKeys = [], rowOrder = [], bannerKeys = [];
    private readonly Dictionary<string, DeviceTile> tileMap = [];
    private readonly Dictionary<string, TransferRow> rowMap = [];
    private readonly HashSet<string> shownPairings = [];
    private PairingDialog? pairing;
    internal AddDeviceDialog? AddDialog { get; set; }
    internal Window? SettingsWindow { get; set; }

    public MainWindow(Controller controller)
    {
        c = controller; updater = new WindowsUpdater(this, c); Title = "AirLink";
        var area = SystemParameters.WorkArea;
        Width = Math.Min(720, area.Width); Height = Math.Min(700, area.Height); MinWidth = Math.Min(560, area.Width); MinHeight = Math.Min(500, area.Height);
        WindowStartupLocation = WindowStartupLocation.CenterScreen; FontSize = 14;
        using (var icon = Assembly.GetExecutingAssembly().GetManifestResourceStream("AirLink.Windows.Icon")) if (icon is not null) Icon = BitmapFrame.Create(icon, BitmapCreateOptions.None, BitmapCacheOption.OnLoad);
        builtEnglish = c.English;
        Build();
        c.PropertyChanged += (_, _) => Render();
        SourceInitialized += (_, _) => HwndSource.FromHwnd(new WindowInteropHelper(this).Handle)?.AddHook(WindowMessage);
        Closing += (_, e) => { if (!quitting && !c.Demo) { e.Cancel = true; Hide(); } };
        Closed += (_, _) => updater.Dispose();
        IsVisibleChanged += (_, _) => c.SetPairingWindow(IsVisible && WindowState != WindowState.Minimized);
        StateChanged += (_, _) => c.SetPairingWindow(IsVisible && WindowState != WindowState.Minimized);
        Loaded += async (_, _) =>
        {
            // After a start at sign-in the service already runs; opening the
            // window must not restart it.
            if (c.Demo || c.Ready || c.Starting) { c.SetPairingWindow(IsVisible); return; }
            await c.StartAsync(); c.SetPairingWindow(IsVisible); _ = c.FallbackRefreshAsync();
        };
        Render(); updater.StartSchedule();
    }

    private IntPtr WindowMessage(IntPtr window, int message, IntPtr w, IntPtr l, ref bool handled)
    {
        if ((uint)message == Program.ShowMessage) { ShowMain(); handled = true; }
        return IntPtr.Zero;
    }

    private void Build()
    {
        var root = new DockPanel();
        var bar = new Grid { Margin = new Thickness(12, 10, 12, 4) };
        bar.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        bar.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        bar.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        sendButton = Theme.Button(c.T("Dateien senden …", "Send files …"), () => _ = PickAndSendAsync(null), glyph: Theme.Send);
        bar.Children.Add(sendButton);
        var right = new StackPanel { Orientation = Orientation.Horizontal };
        historyButton = Theme.Button(c.T("Verlauf", "History"), () => Dialogs.History(this, c), glyph: Theme.History);
        right.Children.Add(historyButton);
        right.Children.Add(Theme.Button(c.T("Hilfe", "Help"), () => Dialogs.Help(this, c), glyph: Theme.Help));
        right.Children.Add(Theme.Button(c.T("Einstellungen", "Settings"), () => Dialogs.Settings(this, c), glyph: Theme.Settings));
        Grid.SetColumn(right, 2); bar.Children.Add(right);
        DockPanel.SetDock(bar, Dock.Top); root.Children.Add(bar);
        pages.Children.Clear();
        pages.Children.Add(startup);
        pages.Children.Add(welcome = BuildWelcome());
        pages.Children.Add(home = BuildHome());
        root.Children.Add(pages);
        Content = root;
        tileKeys = []; rowOrder = []; bannerKeys = []; tileMap.Clear(); rowMap.Clear(); welcomeFilled = false;
    }

    private ScrollViewer BuildWelcome()
    {
        var column = new StackPanel { MaxWidth = 540, Margin = new Thickness(28, 20, 28, 28) };
        var logo = new Image { Width = 96, Height = 96, Margin = new Thickness(0, 10, 0, 14) };
        using (var stream = Assembly.GetExecutingAssembly().GetManifestResourceStream("AirLink.Windows.Icon")) if (stream is not null) logo.Source = BitmapFrame.Create(stream, BitmapCreateOptions.None, BitmapCacheOption.OnLoad);
        column.Children.Add(logo);
        var title = Theme.Text(c.T("Willkommen bei AirLink", "Welcome to AirLink"), 28, true); title.TextAlignment = TextAlignment.Center; column.Children.Add(title);
        var intro = Theme.Text(c.T("Schick Dateien direkt zwischen deinen Computern – Windows, Mac und Linux. Schnell, verschlüsselt und nur in deinem Netzwerk.", "Send files directly between your computers – Windows, Mac and Linux. Fast, encrypted and only on your network."), 14, secondary: true);
        intro.TextAlignment = TextAlignment.Center; intro.Margin = new Thickness(0, 8, 0, 22); column.Children.Add(intro);
        var form = new StackPanel { Margin = new Thickness(18) };
        form.Children.Add(Theme.Text(c.T("Name dieses PCs", "Name of this PC"), 13, true));
        welcomeName = new TextBox { MaxLength = 120, Margin = new Thickness(0, 6, 0, 16) }; form.Children.Add(welcomeName);
        form.Children.Add(Theme.Text(c.T("Empfangene Dateien", "Received files"), 13, true));
        var folderRow = new DockPanel { Margin = new Thickness(0, 6, 0, 16) };
        var change = Theme.Button(c.T("Ändern …", "Change …"), () =>
        {
            var dialog = new OpenFolderDialog { Title = c.T("Wohin sollen empfangene Dateien?", "Where should received files go?"), InitialDirectory = welcomePath };
            if (dialog.ShowDialog(this) == true) { welcomePath = dialog.FolderName; welcomeFolder.Text = welcomePath; }
        });
        DockPanel.SetDock(change, Dock.Right); folderRow.Children.Add(change);
        welcomeFolder = Theme.Text("", 14, secondary: true); welcomeFolder.VerticalAlignment = VerticalAlignment.Center; welcomeFolder.TextTrimming = TextTrimming.CharacterEllipsis; folderRow.Children.Add(welcomeFolder);
        form.Children.Add(folderRow);
        welcomeAutostart = new CheckBox { Content = c.T("Beim Anmelden starten", "Open at sign-in"), IsChecked = true };
        form.Children.Add(welcomeAutostart);
        var hintA = Theme.Text(c.T("Damit dich deine anderen Computer jederzeit erreichen.", "So your other computers can always reach you."), 12, secondary: true); hintA.Margin = new Thickness(28, 0, 0, 10); form.Children.Add(hintA);
        welcomeNotify = new CheckBox { Content = c.T("Mitteilungen", "Notifications"), IsChecked = true };
        form.Children.Add(welcomeNotify);
        var hintB = Theme.Text(c.T("Wenn Dateien ankommen oder jemand sich verbinden möchte.", "When files arrive or someone wants to connect."), 12, secondary: true); hintB.Margin = new Thickness(28, 0, 0, 0); form.Children.Add(hintB);
        column.Children.Add(Theme.Card(form));
        var go = Theme.Button(c.T("Los geht’s", "Get started"), () => _ = FinishWelcomeAsync(), accent: true);
        go.MinWidth = 200; go.HorizontalAlignment = HorizontalAlignment.Center; go.Margin = new Thickness(0, 22, 0, 0); go.Padding = new Thickness(18, 9, 18, 9); go.IsDefault = true;
        column.Children.Add(go);
        return new ScrollViewer { Content = column, VerticalScrollBarVisibility = ScrollBarVisibility.Auto };
    }

    private ScrollViewer BuildHome()
    {
        var column = new StackPanel { Margin = new Thickness(28, 12, 28, 24), MaxWidth = 860 };
        column.Children.Add(banners);
        column.Children.Add(homeTitle);
        homeSubtitle.Margin = new Thickness(0, 4, 0, 18); column.Children.Add(homeSubtitle);
        column.Children.Add(tiles);
        ipLink = new TextBlock { Margin = new Thickness(0, 0, 0, 8) };
        var link = new Hyperlink(new Run(c.T("Nicht gefunden? Per IP-Adresse verbinden …", "Not found? Connect by IP address …")));
        link.Click += (_, _) => Dialogs.AddDevice(this, c, byAddress: true);
        ipLink.Inlines.Add(link); column.Children.Add(ipLink);
        var recent = new StackPanel { Margin = new Thickness(0, 14, 0, 0) };
        var header = new DockPanel { Margin = new Thickness(0, 0, 0, 8) };
        var all = new TextBlock(); var allLink = new Hyperlink(new Run(c.T("Alle anzeigen", "Show all"))); allLink.Click += (_, _) => Dialogs.History(this, c); all.Inlines.Add(allLink);
        DockPanel.SetDock(all, Dock.Right); header.Children.Add(all);
        header.Children.Add(Theme.Text(c.T("Zuletzt", "Recent"), 16, true));
        recent.Children.Add(header);
        recent.Children.Add(Theme.Card(recentList));
        column.Children.Add(recentArea = recent);
        return new ScrollViewer { Content = column, VerticalScrollBarVisibility = ScrollBarVisibility.Auto };
    }

    public void Render()
    {
        if (rendering) return;
        rendering = true;
        try
        {
            if (c.English != builtEnglish) { builtEnglish = c.English; Build(); }
            startup.Visibility = welcome.Visibility = home.Visibility = Visibility.Collapsed;
            if (!c.Ready || c.Status is null) { RenderStartup(); return; }
            sendButton.IsEnabled = historyButton.IsEnabled = c.SetupComplete;
            if (!c.SetupComplete)
            {
                welcome.Visibility = Visibility.Visible;
                if (!welcomeFilled)
                {
                    welcomeFilled = true;
                    var name = c.Status.Settings.Name;
                    welcomeName.Text = string.IsNullOrWhiteSpace(name) || name is "AirLink" or "Windows" ? Environment.MachineName : name;
                    welcomePath = string.IsNullOrEmpty(c.Status.Settings.ReceiveDirectory) ? WindowsFolders.Downloads() : c.Status.Settings.ReceiveDirectory;
                    welcomeFolder.Text = welcomePath;
                }
                return;
            }
            home.Visibility = Visibility.Visible;
            var none = c.PairedDevices.Count == 0;
            homeTitle.Text = none ? c.T("Verbinde deinen anderen Computer", "Connect your other computer") : c.T("Zieh Dateien auf einen Computer", "Drag files onto a computer");
            homeSubtitle.Text = none ? c.T("Öffne AirLink auf dem anderen Computer. Er erscheint dann hier – klick ihn an und vergleicht einen Code. Das musst du nur einmal machen.", "Open AirLink on the other computer. It then appears here – click it and compare a code. You only do this once.")
                                     : c.T("Oder klick ihn an, um Dateien auszuwählen. Alles bleibt in deinem Netzwerk.", "Or click it to choose files. Everything stays on your network.");
            ipLink.Visibility = none ? Visibility.Visible : Visibility.Collapsed;
            RenderBanners(); RenderTiles(); RenderRecent(); OpenPairingRequests();
            pairing?.Refresh();
            AddDialog?.Refresh();
        }
        finally { rendering = false; }
    }

    private void RenderStartup()
    {
        startup.Visibility = Visibility.Visible;
        startup.Children.Clear();
        var failed = c.ServiceError is not null && !c.Starting;
        var title = Theme.Text(failed ? c.T("AirLink konnte nicht starten", "AirLink could not start") : c.T("AirLink startet …", "Starting AirLink …"), 22, true);
        title.TextAlignment = TextAlignment.Center; startup.Children.Add(title);
        if (!failed) { startup.Children.Add(new ProgressBar { IsIndeterminate = true, Width = 200, Margin = new Thickness(0, 16, 0, 0) }); return; }
        var detail = Theme.Text(c.T("Ein Neustart von AirLink hilft meistens.", "Restarting AirLink usually helps.") + "\n\n" + c.ServiceError, 13, secondary: true);
        detail.TextAlignment = TextAlignment.Center; detail.Margin = new Thickness(0, 10, 0, 16); startup.Children.Add(detail);
        var row = new StackPanel { Orientation = Orientation.Horizontal, HorizontalAlignment = HorizontalAlignment.Center };
        row.Children.Add(Theme.Button(c.T("Anderen Empfangsordner wählen …", "Choose another receive folder …"), () =>
        {
            var dialog = new OpenFolderDialog { Title = c.T("Wohin sollen empfangene Dateien?", "Where should received files go?") };
            if (dialog.ShowDialog(this) == true) _ = c.StartAsync(dialog.FolderName);
        }));
        row.Children.Add(Theme.Button(c.T("Noch einmal versuchen", "Try again"), () => _ = c.StartAsync(), accent: true));
        startup.Children.Add(row);
    }

    private void RenderBanners()
    {
        var offers = (c.Status?.Transfers ?? []).Where(t => t.Direction == "receive" && t.State == "offered").Take(2).ToList();
        var clips = (c.Status?.Clipboard ?? []).Where(m => m.Direction == "receive" && c.ClipboardTexts.ContainsKey(m.Id)).OrderByDescending(m => m.CreatedAt, StringComparer.Ordinal).ToList();
        var keys = offers.Select(t => "o:" + t.Id).Concat(clips.Take(1).Select(m => "c:" + m.Id + ":" + clips.Count)).ToList();
        if (keys.SequenceEqual(bannerKeys)) return;
        bannerKeys = keys; banners.Children.Clear();
        foreach (var t in offers)
        {
            var id = t.Id;
            var banner = new Banner(Theme.Download, c.T($"{t.DeviceName} möchte dir etwas senden", $"{t.DeviceName} wants to send you something"), DeviceExperience.Describe(t, c.English) + " · " + c.Bytes(t.TotalBytes));
            banner.Actions.Children.Add(Theme.Button(c.T("Ablehnen", "Decline"), () => Run(() => c.CommandAsync("transfer.cancel", id))));
            banner.Actions.Children.Add(Theme.Button(c.T("Annehmen", "Accept"), () => Run(() => c.CommandAsync("transfer.accept", id)), accent: true));
            banners.Children.Add(banner);
        }
        if (clips.FirstOrDefault() is { } clip)
        {
            var id = clip.Id;
            var heading = c.T($"Text von {clip.DeviceName}", $"Text from {clip.DeviceName}") + (clips.Count > 1 ? c.T($" (+{clips.Count - 1} weitere)", $" (+{clips.Count - 1} more)") : "");
            var banner = new Banner(Theme.Paste, heading, c.ClipboardTexts.GetValueOrDefault(id, ""));
            banner.Actions.Children.Add(Theme.IconButton(Theme.Close, c.T("Verwerfen", "Dismiss"), () => Run(() => c.DismissClipboardAsync(id))));
            banner.Actions.Children.Add(Theme.Button(c.T("Kopieren", "Copy"), () =>
            {
                try { Clipboard.SetText(c.ClipboardTexts.GetValueOrDefault(id, "")); } catch (Exception) { }
                Run(() => c.DismissClipboardAsync(id));
            }, accent: true));
            banners.Children.Add(banner);
        }
    }

    private void RenderTiles()
    {
        var paired = c.PairedDevices; var nearby = c.NearbyDevices;
        var keys = paired.Select(d => "p:" + d.Id).Concat(nearby.Select(d => "n:" + d.Id)).ToList();
        if (paired.Count == 0 && nearby.Count == 0) keys.Add("search");
        keys.Add("add");
        if (!keys.SequenceEqual(tileKeys))
        {
            tileKeys = keys; tileMap.Clear(); tiles.Children.Clear();
            foreach (var key in keys)
            {
                var id = key.Length > 2 ? key[2..] : "";
                var kind = key.StartsWith("p:") ? DeviceTile.Kind.Paired : key.StartsWith("n:") ? DeviceTile.Kind.Nearby : key == "search" ? DeviceTile.Kind.Searching : DeviceTile.Kind.Add;
                var tile = new DeviceTile(kind);
                switch (kind)
                {
                    case DeviceTile.Kind.Paired:
                        tile.Activated += () => _ = PickAndSendAsync(Device(id));
                        tile.FilesDropped += paths => SendTo(paths, Device(id));
                        tile.ContextMenu = DeviceMenu(id);
                        break;
                    case DeviceTile.Kind.Nearby:
                        tile.Activated += () => Connect(Device(id));
                        break;
                    default:
                        tile.Activated += () => Dialogs.AddDevice(this, c, byAddress: false);
                        break;
                }
                tiles.Children.Add(tile); tileMap[key] = tile;
            }
        }
        var transfers = c.Status?.Transfers ?? []; var pairings = c.Status?.PairingSessions ?? [];
        foreach (var d in paired.Concat(nearby))
        {
            if (!tileMap.TryGetValue((d.Paired ? "p:" : "n:") + d.Id, out var tile)) continue;
            var activity = DeviceExperience.Activity(d, transfers, pairings, c.Preparing.Contains(d.Id), c.English);
            tile.SetContent(DeviceExperience.Glyph(d.Platform, d.Form), d.Name, activity.Label, activity.Progress, activity.Busy, !d.Paired || d.Online);
            tile.ToolTip = d.Paired ? c.T($"Klicken, um Dateien an {d.Name} zu senden – oder Dateien hierher ziehen.", $"Click to send files to {d.Name}, or drag files here.")
                                    : c.T($"Klicken, um dich mit {d.Name} zu verbinden. Ihr vergleicht dann einen Code.", $"Click to connect to {d.Name}. You then compare a code.");
        }
        if (tileMap.TryGetValue("search", out var search)) search.SetContent(Theme.Search, c.T("Suche …", "Searching …"), c.T("Im Netzwerk", "On your network"), null, false, true);
        if (tileMap.TryGetValue("add", out var add)) { add.SetContent(Theme.Add, c.T("Hinzufügen", "Add"), c.T("Anderer Computer", "Another computer"), null, false, true); add.ToolTip = c.T("Einen anderen Computer hinzufügen", "Add another computer"); }
    }

    private ContextMenu DeviceMenu(string id)
    {
        var menu = new ContextMenu();
        void Item(string title, Action action) { var item = new MenuItem { Header = title }; item.Click += (_, _) => action(); menu.Items.Add(item); }
        Item(c.T("Dateien senden …", "Send files …"), () => _ = PickAndSendAsync(Device(id)));
        Item(c.T("Ordner senden …", "Send folder …"), () => _ = PickAndSendAsync(Device(id), folder: true));
        Item(c.T("Zwischenablage senden", "Send clipboard"), () =>
        {
            var text = ""; try { text = Clipboard.GetText(); } catch (Exception) { }
            Run(() => c.SendClipboardAsync(Device(id) ?? throw new IOException(c.T("Dieser Computer ist nicht mehr verbunden.", "This computer is no longer connected.")), text));
        });
        menu.Items.Add(new Separator());
        Item(c.T("Einstellungen für dieses Gerät …", "Settings for this device …"), () => { if (Device(id) is { } d) Dialogs.DeviceSettings(this, c, d.Id); });
        Item(c.T("Entfernen …", "Remove …"), () => { if (Device(id) is { } d) Dialogs.RemoveDevice(this, c, d); });
        return menu;
    }

    private void RenderRecent()
    {
        var items = DeviceExperience.Recent(c.Status?.Transfers ?? []);
        var order = items.Select(t => t.Id).ToList();
        if (!order.SequenceEqual(rowOrder))
        {
            rowOrder = order; rowMap.Clear(); recentList.Children.Clear();
            for (var i = 0; i < order.Count; i++)
            {
                if (i > 0) recentList.Children.Add(new Separator { Margin = new Thickness(60, 0, 0, 0) });
                var row = new TransferRow(c);
                row.ActionRequested += TransferAction;
                recentList.Children.Add(row); rowMap[order[i]] = row;
            }
        }
        foreach (var t in items) if (rowMap.TryGetValue(t.Id, out var row)) row.Update(t);
        recentArea.Visibility = items.Count == 0 ? Visibility.Collapsed : Visibility.Visible;
    }

    internal void TransferAction(string kind, string id)
    {
        var transfer = c.Status?.Transfers.FirstOrDefault(t => t.Id == id);
        switch (kind)
        {
            case "reveal": if (transfer is not null) Run(() => c.RevealAsync(transfer)); break;
            case "retry" or "resume": Run(() => c.CommandAsync("transfer.resume", id)); break;
            case "pause": Run(() => c.CommandAsync("transfer.pause", id)); break;
            case "cancel": Run(() => c.CommandAsync("transfer.cancel", id)); break;
            case "accept": Run(() => c.CommandAsync("transfer.accept", id)); break;
        }
    }

    private void OpenPairingRequests()
    {
        foreach (var request in (c.Status?.PairingSessions ?? []).Where(p => !p.Initiator && p.CanConfirm))
        {
            if (!shownPairings.Add(request.Id)) continue;
            if (pairing is null || !pairing.IsOpenSession) ShowPairing(request.Id);
            ShowMain();
        }
    }

    internal void ShowPairing(string id)
    {
        if (pairing is { } existing && existing.SessionId == id) { existing.Activate(); return; }
        pairing?.Close();
        pairing = new PairingDialog(this, c, id);
        pairing.Closed += (_, _) => { if (pairing?.SessionId == id) pairing = null; };
        pairing.Show();
    }

    internal Device? Device(string id) => c.Status?.Devices.FirstOrDefault(d => d.Id == id);

    internal void Connect(Device? device)
    {
        if (device is null) return;
        Run(async () => { var id = await c.ConnectAsync(device); AddDialog?.Close(); ShowPairing(id); });
    }

    internal void SendTo(IEnumerable<string> paths, Device? device)
    {
        if (device is null) return;
        Run(() => c.SendAsync(paths, device));
    }

    private async Task PickAndSendAsync(Device? device, bool folder = false)
    {
        var receivers = c.PairedDevices.Where(d => d.CanReceive).ToList();
        if (device is null)
        {
            if (receivers.Count == 0) { ReportError(c.T("Verbinde zuerst deinen anderen Computer. Danach kannst du Dateien senden.", "Connect your other computer first. Then you can send files.")); return; }
            if (receivers.Count == 1) device = receivers[0];
        }
        string[] paths;
        if (folder)
        {
            var dialog = new OpenFolderDialog { Title = device is null ? c.T("Ordner senden", "Send folder") : c.T($"An {device.Name} senden", $"Send to {device.Name}"), Multiselect = true };
            if (dialog.ShowDialog(this) != true) return;
            paths = dialog.FolderNames;
        }
        else
        {
            var dialog = new OpenFileDialog { Title = device is null ? c.T("Dateien senden", "Send files") : c.T($"An {device.Name} senden", $"Send to {device.Name}"), Multiselect = true };
            if (dialog.ShowDialog(this) != true) return;
            paths = dialog.FileNames;
        }
        if (device is not null) SendTo(paths, device);
        else Dialogs.SendChooser(this, c, paths);
        await Task.CompletedTask;
    }

    /// <summary>Files from Explorer's "Send to" or another start of AirLink.</summary>
    public void AcceptPaths(string[] paths)
    {
        if (paths.Length == 0) return;
        ShowMain();
        if (!c.Ready || !c.SetupComplete) { ReportError(c.T("AirLink ist noch nicht bereit. Sende die Dateien gleich noch einmal.", "AirLink is not ready yet. Send the files again in a moment.")); return; }
        var receivers = c.PairedDevices.Where(d => d.CanReceive).ToList();
        if (receivers.Count == 0) { ReportError(c.T("Verbinde zuerst deinen anderen Computer. Danach kannst du Dateien senden.", "Connect your other computer first. Then you can send files.")); return; }
        if (receivers.Count == 1) SendTo(paths, receivers[0]);
        else Dialogs.SendChooser(this, c, paths);
    }

    private async Task FinishWelcomeAsync()
    {
        var name = welcomeName.Text.Trim();
        if (name.Length == 0 || System.Text.Encoding.UTF8.GetByteCount(name) > 120) { ReportError(c.T("Bitte einen Namen eingeben.", "Please enter a name.")); return; }
        try { c.Autostart = welcomeAutostart.IsChecked == true; } catch (Exception) { }
        var folder = welcomePath; var notify = welcomeNotify.IsChecked == true;
        await RunAsync(() => c.UpdateSettingsAsync(s => { s.Name = name; if (Path.IsPathFullyQualified(folder)) s.ReceiveDirectory = folder; s.Notifications = notify; s.SetupCompleted = true; }));
    }

    internal void Run(Func<Task> action) => _ = RunAsync(action);
    internal async Task RunAsync(Func<Task> action)
    {
        try { await action(); }
        catch (Exception e) { ReportError(e.Message); }
    }

    public void ShowMain() { Show(); if (WindowState == WindowState.Minimized) WindowState = WindowState.Normal; Activate(); }
    public void ReportError(string message)
    {
        if (c.Demo || string.IsNullOrWhiteSpace(message)) return;
        MessageBox.Show(this, message, c.T("Das hat nicht geklappt", "That did not work"), MessageBoxButton.OK, MessageBoxImage.Warning);
    }
    public Task CheckForUpdatesAsync() => updater.CheckAsync();
    internal bool UpdatePrepared { get; private set; }
    internal async Task<bool> PrepareForUpdateAsync()
    {
        if (quitting) return false;
        if (c.ActiveCount > 0 && MessageBox.Show(this, c.T("Für das Update wird AirLink beendet. Übertragungen werden gesichert und beim nächsten Start fortgesetzt. Jetzt fortfahren?", "AirLink will close for the update. Transfers will be saved and resumed next time. Continue?"), c.T("Update installieren", "Install update"), MessageBoxButton.YesNo, MessageBoxImage.Question) != MessageBoxResult.Yes) return false;
        IsEnabled = false;
        try
        {
            if (!await c.StopForUpdateAsync())
            {
                ReportError(c.T("Das Update wurde abgebrochen: Der Dienst konnte nicht sicher beendet werden.", "Update cancelled: the service could not close safely."));
                IsEnabled = true;
                return false;
            }
            UpdatePrepared = true;
            return true;
        }
        catch { IsEnabled = true; throw; }
    }
    internal void FinishUpdateQuit()
    {
        if (!UpdatePrepared) return;
        quitting = true;
        c.CompleteUpdateShutdown();
        Application.Current.Shutdown();
    }
    internal void CancelUpdatePreparation() { UpdatePrepared = false; IsEnabled = true; }
    public async Task QuitAsync(bool confirmActive = true)
    {
        if (quitting) return;
        if (confirmActive && c.ActiveCount > 0 && MessageBox.Show(this, c.T("AirLink beenden? Laufende Übertragungen werden angehalten und beim nächsten Start fortgesetzt.", "Quit AirLink? Running transfers pause and continue the next time AirLink starts."), "AirLink", MessageBoxButton.YesNo, MessageBoxImage.Question) != MessageBoxResult.Yes) return;
        quitting = true;
        try
        {
            if (!await c.StopAsync()) { quitting = false; ReportError(c.T("Der Dienst läuft noch. Bitte noch einmal beenden.", "The service is still running. Please try quitting again.")); return; }
            Application.Current.Shutdown();
        }
        catch { quitting = false; throw; }
    }

    /// <summary>Renders the main states with sample data into PNG files.</summary>
    internal void Snapshot(string directory)
    {
        Directory.CreateDirectory(directory);
        void Save(FrameworkElement element, string name)
        {
            element.UpdateLayout();
            var dpi = VisualTreeHelperDpi(element);
            var bitmap = new RenderTargetBitmap((int)Math.Ceiling(element.ActualWidth * dpi), (int)Math.Ceiling(element.ActualHeight * dpi), 96 * dpi, 96 * dpi, System.Windows.Media.PixelFormats.Pbgra32);
            bitmap.Render(element);
            var encoder = new PngBitmapEncoder(); encoder.Frames.Add(BitmapFrame.Create(bitmap));
            using var file = File.Create(Path.Combine(directory, name + ".png")); encoder.Save(file);
        }
        foreach (var scenario in new[] { "welcome", "empty", "home" })
        {
            c.ShowDemo(DemoData.Status(scenario, c.English));
            if (scenario == "home") c.ClipboardTexts["demo-clip"] = "https://example.com/notizen";
            bannerKeys = []; Render();
            Dispatcher.Invoke(() => { }, System.Windows.Threading.DispatcherPriority.ApplicationIdle);
            Save((FrameworkElement)Content, scenario);
        }
        c.ShowDemo(DemoData.Status("pairing", c.English)); Render();
        var dialog = new PairingDialog(this, c, "demo-pair") { Left = -20000 };
        dialog.Show(); Dispatcher.Invoke(() => { }, System.Windows.Threading.DispatcherPriority.ApplicationIdle);
        Save((FrameworkElement)dialog.Content, "pairing"); dialog.Close();
    }
    private static double VisualTreeHelperDpi(Visual visual) => System.Windows.Media.VisualTreeHelper.GetDpi(visual).DpiScaleX;
}
