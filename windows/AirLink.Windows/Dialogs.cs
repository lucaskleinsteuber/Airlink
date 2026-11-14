using System;
using System.Collections.Generic;
using System.Globalization;
using System.IO;
using System.Linq;
using System.Text.Json;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Documents;
using AirLink.Core;
using Microsoft.Win32;

namespace AirLink.Windows;

internal abstract class AirDialog : Window
{
    protected readonly Controller c;
    protected readonly StackPanel body = new() { Margin = new Thickness(28, 22, 28, 22) };
    protected AirDialog(Window owner, Controller controller, string title, double width = 480)
    {
        c = controller; Owner = owner; Title = title; Width = width; SizeToContent = SizeToContent.Height; MaxHeight = SystemParameters.WorkArea.Height - 40;
        ResizeMode = ResizeMode.NoResize; ShowInTaskbar = false; WindowStartupLocation = WindowStartupLocation.CenterOwner; FontSize = 14;
        Content = new ScrollViewer { Content = body, VerticalScrollBarVisibility = ScrollBarVisibility.Auto };
    }
    internal static StackPanel Row(params UIElement[] children)
    {
        var row = new StackPanel { Orientation = Orientation.Horizontal, HorizontalAlignment = HorizontalAlignment.Right, Margin = new Thickness(0, 18, 0, 0) };
        foreach (var child in children) row.Children.Add(child);
        return row;
    }
    protected UIElement Step(int number, string text)
    {
        var row = new DockPanel { Margin = new Thickness(0, 0, 0, 10) };
        var badge = new Border { Width = 24, Height = 24, CornerRadius = new CornerRadius(12), Margin = new Thickness(0, 0, 12, 0), VerticalAlignment = VerticalAlignment.Top, Child = Theme.Glyph(number.ToString(CultureInfo.InvariantCulture), 13, "TextOnAccentFillColorPrimaryBrush") };
        ((TextBlock)badge.Child).FontFamily = new System.Windows.Media.FontFamily("Segoe UI Variable, Segoe UI"); ((TextBlock)badge.Child).FontWeight = FontWeights.SemiBold;
        badge.SetResourceReference(Border.BackgroundProperty, "AccentFillColorDefaultBrush");
        row.Children.Add(badge); row.Children.Add(Theme.Text(text));
        return row;
    }
}

/// <summary>Both computers show the same code; each side confirms that it matches.</summary>
internal sealed class PairingDialog : AirDialog
{
    public string SessionId { get; }
    private readonly MainWindow main;
    private readonly TextBlock link = Theme.Glyph("• • •", 16, "TextFillColorTertiaryBrush"), peerIcon = Theme.Glyph(Theme.Laptop, 30, "AccentTextFillColorPrimaryBrush");
    private readonly TextBlock title = Theme.Text("", 20, true), message = Theme.Text("", 14, secondary: true), code = Theme.Text("", 40, true), hint = Theme.Text("", 12, secondary: true);
    private readonly CheckBox automatic = new() { IsChecked = true, HorizontalAlignment = HorizontalAlignment.Center, Margin = new Thickness(0, 14, 0, 6) };
    private readonly Button cancel, confirm, close;
    private bool confirming;
    public bool IsOpenSession => c.Status?.PairingSessions.FirstOrDefault(p => p.Id == SessionId)?.IsOpen == true;

    public PairingDialog(MainWindow owner, Controller controller, string id) : base(owner, controller, "AirLink", 480)
    {
        main = owner; SessionId = id;
        var icons = new StackPanel { Orientation = Orientation.Horizontal, HorizontalAlignment = HorizontalAlignment.Center, Margin = new Thickness(0, 4, 0, 16) };
        icons.Children.Add(Circle(Theme.Glyph(System.Windows.Forms.SystemInformation.PowerStatus.BatteryChargeStatus.HasFlag(System.Windows.Forms.BatteryChargeStatus.NoSystemBattery) ? Theme.Desktop : Theme.Laptop, 30, "AccentTextFillColorPrimaryBrush")));
        link.Margin = new Thickness(18, 0, 18, 0); icons.Children.Add(link);
        icons.Children.Add(Circle(peerIcon));
        body.Children.Add(icons);
        foreach (var text in new[] { title, message, hint }) text.TextAlignment = TextAlignment.Center;
        code.TextAlignment = TextAlignment.Center; code.Margin = new Thickness(0, 16, 0, 0); code.FontFamily = new System.Windows.Media.FontFamily("Segoe UI Variable Display, Segoe UI");
        message.Margin = new Thickness(0, 6, 0, 0);
        body.Children.Add(title); body.Children.Add(message); body.Children.Add(code); body.Children.Add(automatic); body.Children.Add(hint);
        cancel = Theme.Button("", () => { main.Run(() => c.CancelPairingAsync(SessionId)); Close(); });
        cancel.IsCancel = true;
        confirm = Theme.Button("", () => { confirming = true; Refresh(); main.Run(() => c.ConfirmPairingAsync(SessionId, automatic.IsChecked == true)); }, accent: true);
        confirm.IsDefault = true;
        close = Theme.Button("", Close, accent: true);
        var buttons = Row(cancel, confirm, close); buttons.HorizontalAlignment = HorizontalAlignment.Center;
        body.Children.Add(buttons);
        Refresh();
    }

    private static Border Circle(UIElement child)
    {
        var border = new Border { Width = 64, Height = 64, CornerRadius = new CornerRadius(32), BorderThickness = new Thickness(1), Child = child };
        border.SetResourceReference(Border.BackgroundProperty, "CardBackgroundFillColorDefaultBrush");
        border.SetResourceReference(Border.BorderBrushProperty, "CardStrokeColorDefaultBrush");
        return border;
    }

    public void Refresh()
    {
        var s = c.Status?.PairingSessions.FirstOrDefault(p => p.Id == SessionId);
        var state = s?.State ?? "";
        var name = s?.PeerName ?? "";
        var peer = name.Length == 0 ? c.T("dem anderen Computer", "the other computer") : name;
        var peerTitle = name.Length == 0 ? c.T("Anderer Computer", "Other computer") : name;
        peerIcon.Text = DeviceExperience.Glyph(s?.Platform, s?.Form);
        var comparing = state == "comparing" && s?.LocalConfirmed == false;
        if (!comparing) confirming = false;
        var open = s?.IsOpen == true;
        code.Text = s?.FormattedCode ?? ""; code.Visibility = state == "comparing" ? Visibility.Visible : Visibility.Collapsed; code.Opacity = comparing ? 1 : 0.5;
        automatic.Content = c.T($"Dateien von {peerTitle} ohne Nachfrage annehmen", $"Accept files from {peerTitle} without asking");
        automatic.Visibility = hint.Visibility = comparing ? Visibility.Visible : Visibility.Collapsed;
        hint.Text = c.T("Bestätige nur, wenn beide Codes gleich sind. So weißt du sicher, dass du mit deinem eigenen Computer verbunden bist.", "Confirm only if both codes match. That way you know for sure it is your own computer.");
        link.Text = state == "paired" ? Theme.Check : "• • •"; link.FontFamily = state == "paired" ? Theme.Icons : new System.Windows.Media.FontFamily("Segoe UI");
        if (s is null) { title.Text = c.T("Anfrage abgelaufen", "Request expired"); message.Text = c.T("Diese Anfrage gibt es nicht mehr. Starte das Verbinden einfach noch einmal.", "This request no longer exists. Simply start connecting again."); }
        else if (state == "paired") { title.Text = c.T("Verbunden!", "Connected!"); message.Text = c.T($"Zieh jetzt Dateien auf {peerTitle} – oder klick den Computer an, um Dateien auszuwählen.", $"Now drag files onto {peerTitle}, or click it to choose files."); }
        else if (state == "starting")
        {
            title.Text = c.T($"Verbinden mit {peerTitle}", $"Connecting to {peerTitle}");
            message.Text = s.Problem switch
            {
                "closed" => c.T($"Öffne AirLink auf {peer}. Sobald es dort offen ist, geht es hier von selbst weiter.", $"Open AirLink on {peer}. As soon as it is open there, this continues by itself."),
                "unreachable" => c.T($"{peerTitle} antwortet nicht. Ist AirLink dort geöffnet und seid ihr im selben Netzwerk? Falls dort eine Firewall aktiv ist (oft unter Linux): Klick stattdessen auf {peer} auf diesen PC – so herum klappt es.", $"{peerTitle} does not answer. Is AirLink open there and are you on the same network? If a firewall is active there (common on Linux), click this PC on {peer} instead – that direction works."),
                _ => c.T($"Verbindung zu {peer} wird aufgebaut …", $"Contacting {peer} …")
            };
        }
        else if (state is "comparing" or "finishing")
        {
            title.Text = s.Initiator ? c.T($"Mit {peerTitle} verbinden", $"Connect to {peerTitle}") : c.T($"{peerTitle} möchte sich verbinden", $"{peerTitle} wants to connect");
            message.Text = comparing ? c.T($"Steht auf {peer} derselbe Code?", $"Does {peer} show the same code?") : state == "comparing" ? c.T($"Bestätige jetzt auch auf {peer}.", $"Now confirm on {peer} too.") : c.T("Verbindung wird gespeichert …", "Saving the connection …");
        }
        else
        {
            title.Text = c.T("Verbinden hat nicht geklappt", "Could not connect");
            message.Text = s.Problem switch
            {
                "outdated" => c.T($"{peerTitle} hat eine ältere AirLink-Version. Aktualisiere AirLink dort auf 0.13 oder neuer.", $"{peerTitle} runs an older AirLink. Update AirLink there to 0.13 or later."),
                "paired" => c.T("Ihr seid bereits verbunden.", "You are already connected."),
                "self" => c.T("Das ist die Adresse dieses PCs. Gib die Adresse des anderen Computers ein.", "That is the address of this PC. Enter the other computer's address."),
                "identity" => c.T("Der Computer hat sich anders ausgewiesen als erwartet. Wähle ihn noch einmal aus der Liste.", "The computer identified itself differently than expected. Choose it from the list again."),
                _ => state is "cancelled" or "denied" ? c.T("Das Verbinden wurde abgebrochen.", "Connecting was cancelled.") : c.T("Die Anfrage ist abgelaufen. Versuch es einfach noch einmal.", "The request expired. Simply try again.")
            };
        }
        cancel.Content = comparing && s?.Initiator == false ? c.T("Ablehnen", "Decline") : c.T("Abbrechen", "Cancel");
        cancel.Visibility = open && !(s!.LocalConfirmed && state != "comparing") ? Visibility.Visible : Visibility.Collapsed;
        confirm.Content = c.T("Code stimmt", "Code matches");
        confirm.Visibility = comparing ? Visibility.Visible : Visibility.Collapsed;
        confirm.IsEnabled = comparing && !confirming && s!.CanConfirm;
        close.Content = state == "paired" ? c.T("Fertig", "Done") : c.T("Schließen", "Close");
        close.Visibility = open ? Visibility.Collapsed : Visibility.Visible;
    }
}

/// <summary>How to add a computer, the computers nearby and the address fallback.</summary>
internal sealed class AddDeviceDialog : AirDialog
{
    private readonly MainWindow main;
    private readonly StackPanel nearby = new() { Margin = new Thickness(12, 8, 12, 8) };
    private readonly TextBox address = new() { Margin = new Thickness(0, 0, 8, 0) };
    private readonly TextBlock issue = Theme.Text("", 13), own = Theme.Text("", 12, secondary: true);
    private string key = "";

    public AddDeviceDialog(MainWindow owner, Controller controller, bool byAddress) : base(owner, controller, controller.T("Computer hinzufügen", "Add a computer"), 520)
    {
        main = owner;
        body.Children.Add(Theme.Text(c.T("Computer hinzufügen", "Add a computer"), 20, true));
        body.Children.Add(new Border { Height = 14 });
        body.Children.Add(Step(1, c.T("Installiere AirLink auf dem anderen Computer und öffne es.", "Install AirLink on the other computer and open it.")));
        body.Children.Add(Step(2, c.T("Beide Computer sind im selben Netzwerk (WLAN oder Kabel).", "Both computers are on the same network (Wi-Fi or cable).")));
        body.Children.Add(Step(3, c.T("Klick den Computer hier an und vergleicht den Code.", "Click the computer here and compare the code.")));
        var header = Theme.Text(c.T("In der Nähe", "Nearby"), 15, true); header.Margin = new Thickness(0, 8, 0, 8); body.Children.Add(header);
        body.Children.Add(Theme.Card(nearby));
        var expander = new Expander { Header = c.T("Nicht gefunden? Per IP-Adresse verbinden", "Not found? Connect by IP address"), IsExpanded = byAddress, Margin = new Thickness(0, 16, 0, 0) };
        var inner = new StackPanel { Margin = new Thickness(0, 8, 0, 0) };
        inner.Children.Add(Theme.Text(c.T("Gib die IP-Adresse des anderen Computers ein. AirLink zeigt sie dort unter Hilfe.", "Enter the other computer's IP address. AirLink shows it there under Help."), 13, secondary: true));
        var row = new DockPanel { Margin = new Thickness(0, 8, 0, 6) };
        var go = Theme.Button(c.T("Verbinden", "Connect"), Connect, accent: true);
        DockPanel.SetDock(go, Dock.Right); row.Children.Add(go); row.Children.Add(address);
        address.KeyDown += (_, e) => { if (e.Key == System.Windows.Input.Key.Enter) Connect(); };
        inner.Children.Add(row);
        issue.SetResourceReference(TextBlock.ForegroundProperty, "SystemFillColorCriticalBrush"); inner.Children.Add(issue);
        inner.Children.Add(own);
        expander.Content = inner; body.Children.Add(expander);
        body.Children.Add(Row(Theme.Button(c.T("Fertig", "Done"), Close)));
        Closed += (_, _) => { if (main.AddDialog == this) main.AddDialog = null; };
        Refresh();
        if (byAddress) Loaded += (_, _) => address.Focus();
    }

    private void Connect()
    {
        var value = address.Text.Trim();
        if (value.Length == 0) return;
        issue.Text = "";
        _ = Run();
        async System.Threading.Tasks.Task Run()
        {
            try { var id = await c.ConnectAddressAsync(value); Close(); main.ShowPairing(id); }
            catch (Exception e) { issue.Text = e.Message; }
        }
    }

    public void Refresh()
    {
        own.Text = c.LocalAddresses.FirstOrDefault() is { } ip ? c.T("Die Adresse dieses PCs: ", "This PC's address: ") + ip : "";
        var devices = c.NearbyDevices;
        var next = string.Join("|", devices.Select(d => d.Id + d.Name + d.Outdated));
        if (next == key && nearby.Children.Count > 0) return;
        key = next; nearby.Children.Clear();
        if (devices.Count == 0) { nearby.Children.Add(Theme.Text(c.T("Suche nach Computern mit AirLink …", "Looking for computers with AirLink …"), 14, secondary: true)); return; }
        foreach (var d in devices)
        {
            var row = new DockPanel { Margin = new Thickness(0, 4, 0, 4) };
            var connect = Theme.Button(c.T("Verbinden", "Connect"), () => main.Connect(d)); connect.IsEnabled = !d.Outdated;
            DockPanel.SetDock(connect, Dock.Right); row.Children.Add(connect);
            var icon = Theme.Glyph(DeviceExperience.Glyph(d.Platform, d.Form), 22, "AccentTextFillColorPrimaryBrush"); icon.Margin = new Thickness(0, 0, 12, 0);
            DockPanel.SetDock(icon, Dock.Left); row.Children.Add(icon);
            var texts = new StackPanel { VerticalAlignment = VerticalAlignment.Center };
            texts.Children.Add(Theme.Text(d.Name, 14, true));
            if (d.Outdated) texts.Children.Add(Theme.Text(c.T("Braucht AirLink 0.13 oder neuer", "Needs AirLink 0.13 or later"), 12, secondary: true));
            row.Children.Add(texts);
            nearby.Children.Add(row);
        }
    }
}

internal static class Dialogs
{
    public static void AddDevice(MainWindow owner, Controller c, bool byAddress)
    {
        if (owner.AddDialog is { } existing) { existing.Activate(); return; }
        owner.AddDialog = new AddDeviceDialog(owner, c, byAddress);
        owner.AddDialog.Show();
    }

    public static void SendChooser(MainWindow owner, Controller c, string[] paths)
    {
        var window = new ChooserWindow(owner, c, c.T("Wohin senden?", "Send to?"));
        var body = window.Body;
        var title = Theme.Text(c.T("Wohin senden?", "Send to?"), 20, true); title.TextAlignment = TextAlignment.Center; body.Children.Add(title);
        var first = Path.GetFileName(paths[0]);
        var summary = Theme.Text(paths.Length == 1 ? first : c.T($"{first} und {paths.Length - 1} weitere", $"{first} and {paths.Length - 1} more"), 14, secondary: true);
        summary.TextAlignment = TextAlignment.Center; summary.Margin = new Thickness(0, 4, 0, 18); body.Children.Add(summary);
        var tiles = new WrapPanel { HorizontalAlignment = HorizontalAlignment.Center };
        foreach (var d in c.PairedDevices.Where(d => d.CanReceive))
        {
            var tile = new DeviceTile(DeviceTile.Kind.Paired, 72);
            var activity = DeviceExperience.Activity(d, c.Status?.Transfers ?? [], [], false, c.English);
            tile.SetContent(DeviceExperience.Glyph(d.Platform, d.Form), d.Name, activity.Label, null, false, d.Online);
            var id = d.Id;
            tile.Activated += () => { window.Close(); owner.SendTo(paths, owner.Device(id)); };
            tiles.Children.Add(tile);
        }
        body.Children.Add(tiles);
        var cancel = Theme.Button(c.T("Abbrechen", "Cancel"), window.Close); cancel.IsCancel = true; cancel.HorizontalAlignment = HorizontalAlignment.Center;
        body.Children.Add(cancel);
        window.Show();
    }

    private sealed class ChooserWindow(Window owner, Controller c, string title) : AirDialog(owner, c, title, 460)
    {
        public StackPanel Body => body;
    }

    public static void RemoveDevice(MainWindow owner, Controller c, Device device)
    {
        if (MessageBox.Show(owner, c.T("Ihr könnt euch danach keine Dateien mehr schicken, bis ihr euch neu verbindet. Bereits gesendete Dateien bleiben erhalten.", "You can no longer send each other files until you connect again. Files already sent stay where they are."),
                c.T($"{device.Name} entfernen?", $"Remove {device.Name}?"), MessageBoxButton.OKCancel, MessageBoxImage.Question) != MessageBoxResult.OK) return;
        owner.Run(() => c.CommandAsync("device.revoke", device.Id));
    }

    private static CheckBox Toggle(MainWindow owner, Controller c, string deviceId, string title, Func<Device, bool> get, Action<Device, bool> set)
    {
        var device = owner.Device(deviceId);
        var box = new CheckBox { Content = title, IsChecked = device is not null && get(device), Margin = new Thickness(0, 4, 0, 4) };
        box.Click += (_, _) =>
        {
            if (owner.Device(deviceId) is not { } current) return;
            var copy = ProtocolJson.Clone(current); set(copy, box.IsChecked == true);
            owner.Run(() => c.SaveDeviceAsync(copy));
        };
        return box;
    }

    private static void DevicePermissions(MainWindow owner, Controller c, string id, StackPanel panel)
    {
        panel.Children.Add(Toggle(owner, c, id, c.T("Dateien ohne Nachfrage annehmen", "Accept files without asking"), d => d.AutoAccept, (d, v) => d.AutoAccept = v));
        panel.Children.Add(Toggle(owner, c, id, c.T("Zwischenablage teilen", "Share clipboard"), d => d.CanClipboard, (d, v) => d.CanClipboard = v));
        panel.Children.Add(Toggle(owner, c, id, c.T("Darf mir Dateien senden", "May send me files"), d => d.CanSend, (d, v) => d.CanSend = v));
        panel.Children.Add(Toggle(owner, c, id, c.T("Darf Dateien von mir empfangen", "May receive my files"), d => d.CanReceive, (d, v) => d.CanReceive = v));
    }

    public static void DeviceSettings(MainWindow owner, Controller c, string id)
    {
        if (owner.Device(id) is not { } device) return;
        var window = new ChooserWindow(owner, c, device.Name);
        var head = new StackPanel { Orientation = Orientation.Horizontal, Margin = new Thickness(0, 0, 0, 16) };
        var icon = Theme.Glyph(DeviceExperience.Glyph(device.Platform, device.Form), 30, "AccentTextFillColorPrimaryBrush"); icon.Margin = new Thickness(0, 0, 14, 0); head.Children.Add(icon);
        var names = new StackPanel(); names.Children.Add(Theme.Text(device.Name, 20, true)); names.Children.Add(Theme.Text(device.Online ? c.T("Verbunden", "Connected") : c.T("Gerade nicht erreichbar", "Not reachable right now"), 13, secondary: true));
        head.Children.Add(names); window.Body.Children.Add(head);
        DevicePermissions(owner, c, id, window.Body);
        var row = new DockPanel { Margin = new Thickness(0, 18, 0, 0) };
        var remove = Theme.Button(c.T("Entfernen …", "Remove …"), () => { window.Close(); RemoveDevice(owner, c, device); });
        DockPanel.SetDock(remove, Dock.Left); row.Children.Add(remove);
        var done = Theme.Button(c.T("Fertig", "Done"), window.Close, accent: true); done.HorizontalAlignment = HorizontalAlignment.Right; row.Children.Add(done);
        window.Body.Children.Add(row);
        window.Show();
    }

    public static void Settings(MainWindow owner, Controller c)
    {
        if (owner.SettingsWindow is { } existing) { existing.Activate(); return; }
        if (c.Status is null) return;
        var window = new Window { Owner = owner, Title = c.T("AirLink-Einstellungen", "AirLink settings"), Width = 580, Height = 560, WindowStartupLocation = WindowStartupLocation.CenterOwner, ShowInTaskbar = false, FontSize = 14 };
        owner.SettingsWindow = window; window.Closed += (_, _) => owner.SettingsWindow = null;
        var tabs = new TabControl { Margin = new Thickness(12) };
        StackPanel Page(string header)
        {
            var panel = new StackPanel { Margin = new Thickness(18) };
            tabs.Items.Add(new TabItem { Header = header, Content = new ScrollViewer { Content = panel, VerticalScrollBarVisibility = ScrollBarVisibility.Auto } });
            return panel;
        }
        TextBlock Label(string text) { var t = Theme.Text(text, 13, true); t.Margin = new Thickness(0, 12, 0, 6); return t; }
        var s = c.Status.Settings;

        var general = Page(c.T("Allgemein", "General"));
        general.Children.Add(Label(c.T("Name dieses PCs", "Name of this PC")));
        var name = new TextBox { Text = s.Name, MaxLength = 120 };
        name.LostFocus += (_, _) => { var value = name.Text.Trim(); if (value.Length > 0 && value != c.Status?.Settings.Name) owner.Run(() => c.UpdateSettingsAsync(x => x.Name = value)); };
        general.Children.Add(name);
        general.Children.Add(Label(c.T("Empfangene Dateien", "Received files")));
        var folder = new DockPanel();
        var folderText = Theme.Text(s.ReceiveDirectory, 14, secondary: true); folderText.TextTrimming = TextTrimming.CharacterEllipsis; folderText.VerticalAlignment = VerticalAlignment.Center;
        var change = Theme.Button(c.T("Ändern …", "Change …"), () =>
        {
            var dialog = new OpenFolderDialog { Title = c.T("Wohin sollen empfangene Dateien?", "Where should received files go?"), InitialDirectory = c.Status?.Settings.ReceiveDirectory };
            if (dialog.ShowDialog(window) == true) { var chosen = dialog.FolderName; folderText.Text = chosen; owner.Run(() => c.UpdateSettingsAsync(x => x.ReceiveDirectory = chosen)); }
        });
        var open = Theme.Button(c.T("Öffnen", "Open"), () => owner.Run(() => { c.OpenReceiveFolder(); return System.Threading.Tasks.Task.CompletedTask; }));
        DockPanel.SetDock(open, Dock.Right); DockPanel.SetDock(change, Dock.Right); folder.Children.Add(open); folder.Children.Add(change); folder.Children.Add(folderText);
        general.Children.Add(folder);
        var autostart = new CheckBox { Content = c.T("Beim Anmelden starten", "Open at sign-in"), IsChecked = c.Autostart, Margin = new Thickness(0, 18, 0, 4) };
        autostart.Click += (_, _) => { try { c.Autostart = autostart.IsChecked == true; } catch (Exception e) { owner.ReportError(e.Message); } };
        general.Children.Add(autostart);
        var notify = new CheckBox { Content = c.T("Mitteilungen", "Notifications"), IsChecked = s.Notifications, Margin = new Thickness(0, 4, 0, 4) };
        notify.Click += (_, _) => { var on = notify.IsChecked == true; owner.Run(() => c.UpdateSettingsAsync(x => x.Notifications = on)); };
        general.Children.Add(notify);
        general.Children.Add(Label(c.T("Sprache", "Language")));
        var language = new ComboBox { Width = 220, HorizontalAlignment = HorizontalAlignment.Left };
        foreach (var (title, value) in new[] { (c.T("Wie Windows", "Same as Windows"), "system"), ("Deutsch", "de"), ("English", "en") }) language.Items.Add(new ComboBoxItem { Content = title, Tag = value });
        language.SelectedIndex = s.Language switch { "de" => 1, "en" => 2, _ => 0 };
        language.SelectionChanged += (_, _) => { if (language.SelectedItem is ComboBoxItem { Tag: string value }) owner.Run(() => c.UpdateSettingsAsync(x => x.Language = value)); };
        general.Children.Add(language);
        var note = Theme.Text(c.T("Nach dem Schließen des Fensters läuft AirLink im Infobereich neben der Uhr weiter, damit dich deine Computer erreichen.", "After you close the window, AirLink keeps running in the notification area so your computers can reach you."), 12, secondary: true);
        note.Margin = new Thickness(0, 18, 0, 0); general.Children.Add(note);

        var devices = Page(c.T("Geräte", "Devices"));
        if (c.PairedDevices.Count == 0) devices.Children.Add(Theme.Text(c.T("Noch kein Computer verbunden. Öffne AirLink auf deinem anderen Computer und klick ihn im AirLink-Fenster an.", "No computer connected yet. Open AirLink on your other computer and click it in the AirLink window."), 14, secondary: true));
        foreach (var d in c.PairedDevices)
        {
            var panel = new StackPanel { Margin = new Thickness(16, 12, 16, 12) };
            panel.Children.Add(Theme.Text(d.Name + (d.Online ? "" : c.T(" · nicht erreichbar", " · not reachable")), 15, true));
            DevicePermissions(owner, c, d.Id, panel);
            var remove = Theme.Button(c.T("Entfernen …", "Remove …"), () => RemoveDevice(owner, c, d)); remove.HorizontalAlignment = HorizontalAlignment.Left; remove.Margin = new Thickness(0, 8, 0, 0);
            panel.Children.Add(remove);
            var card = Theme.Card(panel); card.Margin = new Thickness(0, 0, 0, 12); devices.Children.Add(card);
        }

        var advanced = Page(c.T("Erweitert", "Advanced"));
        UIElement Limit(string title, Func<HostSettings, long> get, Action<HostSettings, long> set)
        {
            var row = new StackPanel { Orientation = Orientation.Horizontal, Margin = new Thickness(0, 4, 0, 4) };
            var label = Theme.Text(title); label.Width = 200; label.VerticalAlignment = VerticalAlignment.Center; row.Children.Add(label);
            var value = get(c.Status!.Settings);
            var box = new TextBox { Width = 110, Text = value > 0 ? (value / 1e6).ToString("0.##", CultureInfo.CurrentCulture) : "" };
            box.LostFocus += (_, _) =>
            {
                var raw = box.Text.Trim().Replace(',', '.');
                long bytes = 0;
                if (raw.Length > 0) { if (!double.TryParse(raw, NumberStyles.Float, CultureInfo.InvariantCulture, out var mb) || mb <= 0 || mb > 1e6) return; bytes = Math.Max(1024, (long)(mb * 1e6)); }
                owner.Run(() => c.UpdateSettingsAsync(x => set(x, bytes)));
            };
            row.Children.Add(box); var unit = Theme.Text("MB/s", 14, secondary: true); unit.Margin = new Thickness(8, 0, 0, 0); unit.VerticalAlignment = VerticalAlignment.Center; row.Children.Add(unit);
            return row;
        }
        advanced.Children.Add(Label(c.T("Geschwindigkeit (leer = unbegrenzt)", "Speed (empty = unlimited)")));
        advanced.Children.Add(Limit(c.T("Senden begrenzen", "Limit sending"), x => x.SendLimitBytesPerSecond, (x, v) => x.SendLimitBytesPerSecond = v));
        advanced.Children.Add(Limit(c.T("Empfangen begrenzen", "Limit receiving"), x => x.ReceiveLimitBytesPerSecond, (x, v) => x.ReceiveLimitBytesPerSecond = v));
        advanced.Children.Add(Label(c.T("Updates", "Updates") + " · " + c.Version));
        var checks = new CheckBox { Content = c.T("Automatisch nach Updates suchen", "Check for updates automatically"), IsChecked = s.AutomaticUpdateChecks };
        checks.Click += (_, _) => { var on = checks.IsChecked == true; owner.Run(() => c.UpdateSettingsAsync(x => x.AutomaticUpdateChecks = on)); };
        advanced.Children.Add(checks);
        var check = Theme.Button(c.T("Jetzt nach Updates suchen …", "Check now …"), () => _ = owner.CheckForUpdatesAsync()); check.HorizontalAlignment = HorizontalAlignment.Left; check.Margin = new Thickness(0, 8, 0, 0);
        advanced.Children.Add(check);
        advanced.Children.Add(Label(c.T("Verbindung", "Connection")));
        var addressText = Theme.Text(c.T("Adresse dieses PCs: ", "This PC's address: ") + string.Join(", ", c.LocalAddresses)); advanced.Children.Add(addressText);
        var fingerprint = new TextBox { Text = c.Status.Fingerprint, IsReadOnly = true, TextWrapping = TextWrapping.Wrap, FontFamily = new System.Windows.Media.FontFamily("Consolas"), Margin = new Thickness(0, 6, 0, 0) };
        advanced.Children.Add(fingerprint);
        var buttons = new StackPanel { Orientation = Orientation.Horizontal, Margin = new Thickness(0, 14, 0, 0) };
        buttons.Children.Add(Theme.Button(c.T("Diagnosebericht sichern …", "Save diagnostic report …"), () => owner.Run(async () =>
        {
            var data = await c.RequestAsync("diagnostics.export");
            var dialog = new SaveFileDialog { FileName = "AirLink-Diagnose.json", Filter = "JSON (*.json)|*.json" };
            if (dialog.ShowDialog(window) == true) await File.WriteAllTextAsync(dialog.FileName, JsonSerializer.Serialize(data, new JsonSerializerOptions { WriteIndented = true }));
        })));
        buttons.Children.Add(Theme.Button(c.T("Lizenzen", "Licenses"), () => NativeLicenses.Show(window, c)));
        advanced.Children.Add(buttons);
        window.Content = tabs;
        window.Show();
    }

    public static void History(MainWindow owner, Controller c)
    {
        if (!c.Ready) return;
        var window = new Window { Owner = owner, Title = c.T("Verlauf", "History"), Width = 640, Height = 580, WindowStartupLocation = WindowStartupLocation.CenterOwner, FontSize = 14 };
        var root = new DockPanel { Margin = new Thickness(16) };
        var top = new DockPanel { Margin = new Thickness(0, 0, 0, 10) };
        var filter = new ComboBox { Width = 220 };
        filter.Items.Add(new ComboBoxItem { Content = c.T("Alle Computer", "All computers"), Tag = "" });
        foreach (var d in c.PairedDevices) filter.Items.Add(new ComboBoxItem { Content = d.Name, Tag = d.Id });
        filter.SelectedIndex = 0;
        var clear = Theme.Button(c.T("Verlauf leeren …", "Clear history …"), () => { });
        DockPanel.SetDock(clear, Dock.Right); top.Children.Add(clear); top.Children.Add(filter);
        DockPanel.SetDock(top, Dock.Top); root.Children.Add(top);
        var list = new StackPanel();
        var more = Theme.Button(c.T("Ältere laden", "Load older"), () => { }); more.HorizontalAlignment = HorizontalAlignment.Center; more.Margin = new Thickness(0, 10, 0, 0);
        var content = new StackPanel(); content.Children.Add(Theme.Card(list)); content.Children.Add(more);
        root.Children.Add(new ScrollViewer { Content = content, VerticalScrollBarVisibility = ScrollBarVisibility.Auto });
        window.Content = root;
        string? cursor = null;
        async System.Threading.Tasks.Task Load(bool append)
        {
            var device = (filter.SelectedItem as ComboBoxItem)?.Tag as string;
            var page = await c.RequestAsync<TransferPage>("history.page", new { limit = 50, cursor = append ? cursor : null, deviceId = string.IsNullOrEmpty(device) ? null : device });
            if (!append) list.Children.Clear();
            foreach (var t in page.Transfers)
            {
                if (list.Children.Count > 0) list.Children.Add(new Separator { Margin = new Thickness(60, 0, 0, 0) });
                var row = new TransferRow(c); row.Update(t); row.ActionRequested += owner.TransferAction; list.Children.Add(row);
            }
            if (list.Children.Count == 0) { var empty = Theme.Text(c.T("Noch keine Übertragungen. Was du sendest und empfängst, erscheint hier.", "No transfers yet. What you send and receive appears here."), 14, secondary: true); empty.Margin = new Thickness(16); list.Children.Add(empty); }
            cursor = page.NextCursor;
            more.Visibility = string.IsNullOrEmpty(cursor) ? Visibility.Collapsed : Visibility.Visible;
        }
        more.Click += (_, _) => owner.Run(() => Load(true));
        filter.SelectionChanged += (_, _) => owner.Run(() => Load(false));
        clear.Click += (_, _) =>
        {
            if (MessageBox.Show(window, c.T("Nur die Liste wird geleert. Deine Dateien bleiben, wo sie sind.", "Only the list is cleared. Your files stay where they are."), c.T("Verlauf leeren?", "Clear history?"), MessageBoxButton.OKCancel, MessageBoxImage.Question) != MessageBoxResult.OK) return;
            owner.Run(async () => { await c.RequestAsync("history.clear"); await Load(false); await c.RefreshAsync(true); });
        };
        owner.Run(() => Load(false));
        window.Show();
    }

    public static void Help(MainWindow owner, Controller c)
    {
        var window = new ChooserWindow(owner, c, c.T("AirLink-Hilfe", "AirLink help"));
        var body = window.Body; window.Width = 540;
        body.Children.Add(Theme.Text(c.T("So funktioniert AirLink", "How AirLink works"), 20, true));
        body.Children.Add(new Border { Height = 12 });
        foreach (var (number, text) in new[] {
            (1, c.T("Installiere AirLink auf deinen Computern – Windows, Mac oder Linux.", "Install AirLink on your computers – Windows, Mac or Linux.")),
            (2, c.T("Verbinde sie einmal: Computer anklicken, Code vergleichen, bestätigen.", "Connect them once: click the computer, compare the code, confirm.")),
            (3, c.T("Zieh Dateien auf einen Computer. Sie landen dort direkt im Download-Ordner.", "Drag files onto a computer. They arrive directly in its Downloads folder.")) })
        {
            var row = new DockPanel { Margin = new Thickness(0, 0, 0, 10) };
            var badge = Theme.Text(number + ".", 14, true); badge.Width = 24; row.Children.Add(badge); row.Children.Add(Theme.Text(text)); body.Children.Add(row);
        }
        var trouble = Theme.Text(c.T("Probleme lösen", "Troubleshooting"), 16, true); trouble.Margin = new Thickness(0, 10, 0, 8); body.Children.Add(trouble);
        void Item(string title, string text)
        {
            var expander = new Expander { Header = title, Margin = new Thickness(0, 0, 0, 6) };
            var content = new TextBox { Text = text, IsReadOnly = true, TextWrapping = TextWrapping.Wrap, BorderThickness = new Thickness(0), Background = System.Windows.Media.Brushes.Transparent, Margin = new Thickness(0, 6, 0, 6) };
            expander.Content = content; body.Children.Add(expander);
        }
        Item(c.T("Der andere Computer erscheint nicht", "The other computer does not appear"), c.T("AirLink muss auf beiden Computern geöffnet sein, und beide müssen im selben Netzwerk sein. Gastnetze und manche Router trennen Geräte voneinander – dann hilft „Per IP-Adresse verbinden“ unter „Hinzufügen“. Erlaube AirLink beim ersten Start in der Windows-Firewall für private Netzwerke.", "AirLink must be open on both computers, and both must be on the same network. Guest networks and some routers keep devices apart – then use “Connect by IP address” under “Add”. Allow AirLink in Windows Firewall for private networks when asked on first start."));
        Item(c.T("Verbinden klappt nicht (Firewall)", "Connecting does not work (firewall)"), c.T("Ist auf dem anderen Computer eine Firewall aktiv (oft unter Linux), starte das Verbinden dort – so herum klappt es, und danach funktioniert das Senden in beide Richtungen.", "If a firewall is active on the other computer (common on Linux), start connecting there instead – that direction works, and afterwards sending works both ways."));
        Item(c.T("Eine Übertragung hängt", "A transfer is stuck"), c.T("AirLink macht automatisch weiter, sobald beide Computer wieder verbunden sind – auch nach einem Neustart. Was schon angekommen ist, wird nicht noch einmal gesendet.", "AirLink continues automatically as soon as both computers are connected again – even after a restart. What already arrived is not sent again."));
        Item(c.T("Senden aus dem Explorer", "Sending from Explorer"), c.T("Rechtsklick auf Dateien → Senden an → AirLink (unter Windows 11 erst „Weitere Optionen anzeigen“).", "Right-click files → Send to → AirLink (on Windows 11 first “Show more options”)."));
        if (c.LocalAddresses.FirstOrDefault() is { } ip) { var address = Theme.Text(c.T("Adresse dieses PCs: ", "This PC's address: ") + ip, 14, true); address.Margin = new Thickness(0, 10, 0, 0); body.Children.Add(address); }
        body.Children.Add(AirDialog.Row(Theme.Button(c.T("Empfangsordner öffnen", "Open receive folder"), () => owner.Run(() => { c.OpenReceiveFolder(); return System.Threading.Tasks.Task.CompletedTask; })), Theme.Button(c.T("Fertig", "Done"), window.Close, accent: true)));
        window.Show();
    }
}

/// <summary>Sample states for --demo/--snapshot: neutral names, documentation addresses.</summary>
internal static class DemoData
{
    public static HostStatus Status(string scenario, bool english)
    {
        string Ago(int s) => DateTimeOffset.UtcNow.AddSeconds(-s).ToString("o", CultureInfo.InvariantCulture);
        var mac = new Device { Id = "demo-mac", Name = "MacBook Pro", Kind = "engine", Address = "https://192.0.2.10:43871", Paired = true, Online = true, CanSend = true, CanReceive = true, CanClipboard = true, AutoAccept = true, Platform = "macos", Form = "laptop" };
        var linux = new Device { Id = "demo-linux", Name = "Linux-PC", Kind = "engine", Address = "https://192.0.2.22:43871", Paired = true, Online = false, CanSend = true, CanReceive = true, AutoAccept = true, Platform = "linux", Form = "laptop" };
        var nearby = new Device { Id = "demo-air", Name = "MacBook Air", Kind = "engine", Address = "https://192.0.2.41:43871", Fingerprint = "cc", Platform = "macos", Form = "laptop" };
        var status = new HostStatus { Summary = true, Version = "0.13.0", Id = "demo-self", Name = "Windows-PC", Urls = ["https://192.0.2.30:43871"], Fingerprint = "dd",
            Settings = new HostSettings { SetupCompleted = scenario != "welcome", Name = "Windows-PC", ReceiveDirectory = WindowsFolders.Downloads(), Language = english ? "en" : "de" } };
        if (scenario is "welcome" or "empty") { status.Devices = [nearby]; return status; }
        status.Devices = scenario == "home" ? [mac, linux, nearby] : [mac, linux];
        status.Transfers = [
            new Transfer { Id = "t1", DeviceId = "demo-mac", DeviceName = "MacBook Pro", Direction = "send", State = "running", Files = [new() { Path = "Urlaub 2026.mov" }], TotalBytes = 2147483648, CompletedBytes = 1331439862, BytesPerSecond = 48234496, UpdatedAt = Ago(0), FileCount = 1, Summary = true },
            new Transfer { Id = "t2", DeviceId = "demo-mac", DeviceName = "MacBook Pro", Direction = "receive", State = "completed", Files = [new() { Path = "Notizen.pdf", Completed = true }], TotalBytes = 482133, CompletedBytes = 482133, UpdatedAt = Ago(240), FileCount = 1, Summary = true, HasContent = true },
            new Transfer { Id = "t3", DeviceId = "demo-linux", DeviceName = "Linux-PC", Direction = "send", State = "completed", Files = [new() { Path = "Fotos", Directory = true }, new() { Path = "Fotos/IMG_0001.jpg" }], TotalBytes = 734003200, CompletedBytes = 734003200, UpdatedAt = Ago(7200), FileCount = 214, Summary = true }];
        if (scenario == "home") status.Clipboard = [new ClipboardMessage { Id = "demo-clip", DeviceId = "demo-mac", DeviceName = "MacBook Pro", Direction = "receive", CreatedAt = Ago(20) }];
        else status.PairingSessions = [new PairingSession { Id = "demo-pair", PeerId = "demo-air", PeerName = "MacBook Air", State = "comparing", ComparisonCode = "48210937", Platform = "macos", Form = "laptop" }];
        return status;
    }
}
