using System;
using System.Collections.Generic;
using System.ComponentModel;
using System.Linq;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Interop;
using System.Windows.Media;
using AirLink.Core;
using Microsoft.Win32;

namespace AirLink.Windows;

internal sealed class MainWindow : Window
{
    private readonly Controller c;
    private readonly Dictionary<string, ListBoxItem> deviceItems = [];
    private readonly Dictionary<string, TransferRow> transferRows = [];
    private readonly Dictionary<string, Border> pairRows = [];
    private ListBox devices = null!, selection = null!;
    private StackPanel requests = null!, transfers = null!;
    private TextBlock host = null!, target = null!, targetDetail = null!, service = null!, notice = null!, error = null!, empty = null!;
    private CheckBox automatic = null!;
    private Button send = null!, pair = null!, manage = null!, retry = null!, recover = null!;
    private bool rendering, quitting;
    private string? appearance, language;
    private string selectedKey = "";

    public MainWindow(Controller controller)
    {
        c = controller;
        Title = "AirLink"; Width = 1080; Height = 780; MinWidth = 860; MinHeight = 640;
        WindowStartupLocation = WindowStartupLocation.CenterScreen;
        FontFamily = new FontFamily("Segoe UI Variable, Segoe UI"); FontSize = 14;
        AllowDrop = true;
        DragOver += (_, e) => { e.Effects = e.Data.GetDataPresent(DataFormats.FileDrop) ? DragDropEffects.Copy : DragDropEffects.None; e.Handled = true; };
        Drop += (_, e) => { if (e.Data.GetData(DataFormats.FileDrop) is string[] paths) _ = c.RunAsync(() => { c.AddPaths(paths); return Task.CompletedTask; }); e.Handled = true; };
        c.PropertyChanged += (_, _) => Render();
        c.Selection.CollectionChanged += (_, _) => Render();
        SourceInitialized += (_, _) => HwndSource.FromHwnd(new WindowInteropHelper(this).Handle)?.AddHook(WindowMessage);
        Closing += (_, e) => { if (!quitting) { e.Cancel = true; Hide(); } };
        Loaded += async (_, _) => { await c.StartAsync(); _ = c.PollAsync(); };
        Build(); Render();
    }

    private IntPtr WindowMessage(IntPtr window, int message, IntPtr w, IntPtr l, ref bool handled)
    {
        if ((uint)message == Program.ShowMessage) { ShowMain(); handled = true; }
        return IntPtr.Zero;
    }
    public void ShowMain() { Show(); if (WindowState == WindowState.Minimized) WindowState = WindowState.Normal; Activate(); }
    public void ReportError(string message) { error.Text = message; error.Visibility = Visibility.Visible; }

    public async Task QuitAsync(bool confirmActive = true)
    {
        if (quitting) return;
        if (confirmActive && c.ActiveCount > 0 && MessageBox.Show(this,
            c.T("Aktive Übertragungen werden gesichert und beim nächsten Start fortgesetzt. AirLink beenden?", "Active transfers will be saved and resumed next time. Quit AirLink?"),
            "AirLink", MessageBoxButton.YesNo, MessageBoxImage.Question) != MessageBoxResult.Yes) return;
        quitting = true;
        try
        {
            if (!await c.StopAsync()) { quitting = false; ReportError(c.T("Der Dienst läuft noch. Bitte erneut beenden.", "The service is still running. Please try quitting again.")); return; }
            Application.Current.Shutdown();
        }
        catch (Exception e) { quitting = false; ReportError(e.Message); }
    }

    private Button Action(string title, Func<Task> action, bool primary = false) => Theme.Button(title, () => _ = c.RunAsync(action), primary);
    private static StackPanel Horizontal(params UIElement[] children)
    {
        var panel = new StackPanel { Orientation = Orientation.Horizontal };
        foreach (var child in children) panel.Children.Add(child);
        return panel;
    }
    private void Build()
    {
        appearance = c.Status?.Settings.Appearance ?? "system";
        language = c.English ? "en" : "de";
        Theme.Apply(appearance); Background = Theme.Background; Foreground = Theme.Foreground;
        deviceItems.Clear(); transferRows.Clear(); pairRows.Clear(); selectedKey = "";
        var layout = new Grid(); layout.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(246) }); layout.ColumnDefinitions.Add(new ColumnDefinition());
        var sidebar = new DockPanel { Margin = new Thickness(20, 26, 14, 20) };
        var heading = new StackPanel();
        heading.Children.Add(Theme.Text("AirLink", 28, true));
        heading.Children.Add(Theme.Text(c.T("Direkt. Lokal. Verbunden.", "Direct. Local. Connected."), 12, secondary: true));
        host = Theme.Text("", 12, secondary: true); host.Margin = new Thickness(0, 20, 0, 15); heading.Children.Add(host);
        heading.Children.Add(Theme.Text(c.T("GERÄTE", "DEVICES"), 11, true, true));
        DockPanel.SetDock(heading, Dock.Top); sidebar.Children.Add(heading);
        var bottom = new StackPanel { Margin = new Thickness(0, 14, 0, 0) };
        bottom.Children.Add(Theme.Button(c.T("Gerät verbinden …", "Connect device …"), () => Dialogs.Pair(this, c, c.SelectedDevice is { Paired: false } d ? d : null)));
        bottom.Children.Add(Theme.Button(c.T("Handy / Browser verbinden", "Connect phone / browser"), () => Dialogs.ConnectionInfo(this, c)));
        bottom.Children.Add(Theme.Button(c.T("Einstellungen", "Settings"), () => Dialogs.Settings(this, c)));
        bottom.Children.Add(Theme.Text("AirLink " + c.Version, 11, secondary: true));
        DockPanel.SetDock(bottom, Dock.Bottom); sidebar.Children.Add(bottom);
        devices = new ListBox { BorderThickness = new Thickness(0), Background = Brushes.Transparent, Margin = new Thickness(0, 10, 0, 0), HorizontalContentAlignment = HorizontalAlignment.Stretch };
        devices.SelectionChanged += (_, _) =>
        {
            if (rendering) return;
            c.SelectedDeviceId = (devices.SelectedItem as ListBoxItem)?.Tag as string; Render();
        };
        sidebar.Children.Add(devices); layout.Children.Add(sidebar);
        var page = new StackPanel { Margin = new Thickness(16, 26, 28, 28) };
        var toolbar = new DockPanel();
        var receive = Action(c.T("Empfangsordner öffnen", "Open receive folder"), () => { c.OpenReceiveFolder(); return Task.CompletedTask; });
        DockPanel.SetDock(receive, Dock.Right); toolbar.Children.Add(receive); toolbar.Children.Add(Theme.Text(c.T("Dateien übertragen", "Transfer files"), 24, true)); page.Children.Add(toolbar);
        service = Theme.Text("", 12, secondary: true); service.Margin = new Thickness(0, 8, 0, 8); page.Children.Add(service);
        error = Theme.Text("", 13); error.Foreground = Theme.Color(Theme.Dark ? "#FFB3A9" : "#AF3125"); error.Margin = new Thickness(0, 5, 0, 10); page.Children.Add(error);
        notice = Theme.Text("", 13, secondary: true); notice.Margin = new Thickness(0, 4, 0, 10); page.Children.Add(notice);
        retry = Action(c.T("Erneut starten", "Retry service"), () => c.StartAsync());
        recover = Action(c.T("Anderen Empfangsordner wählen …", "Choose another receive folder …"), async () =>
        {
            var picker = new OpenFolderDialog { Title = c.T("Empfangsordner", "Receive folder") };
            if (picker.ShowDialog(this) == true) await c.StartAsync(picker.FolderName);
        });
        page.Children.Add(Horizontal(retry, recover));
        requests = new StackPanel { Margin = new Thickness(0, 8, 0, 8) }; page.Children.Add(requests);
        var destination = new StackPanel();
        target = Theme.Text("", 18, true); destination.Children.Add(target);
        targetDetail = Theme.Text("", 12, secondary: true); targetDetail.Margin = new Thickness(0, 5, 0, 5); destination.Children.Add(targetDetail);
        pair = Theme.Button(c.T("Verbinden …", "Connect …"), () => Dialogs.Pair(this, c, c.SelectedDevice));
        manage = Theme.Button(c.T("Gerät verwalten …", "Manage device …"), () => { if (c.SelectedDevice is { } d) Dialogs.Device(this, c, d); });
        destination.Children.Add(Horizontal(pair, manage));
        automatic = new CheckBox { Content = c.T("Von diesem Gerät künftig automatisch empfangen", "Automatically receive from this device in future") };
        automatic.Click += (_, _) =>
        {
            if (c.SelectedDevice is not { Paired: true } current) return;
            var edited = ProtocolJson.Clone(current); edited.AutoAccept = automatic.IsChecked == true;
            _ = c.RunAsync(() => c.SaveDeviceAsync(edited));
        };
        destination.Children.Add(automatic); page.Children.Add(Theme.Card(destination));
        var drop = new StackPanel { HorizontalAlignment = HorizontalAlignment.Stretch };
        drop.Children.Add(Theme.Text(c.T("Dateien oder Ordner hier ablegen", "Drop files or folders here"), 19, true));
        var hint = Theme.Text(c.T("Die Originale bleiben an ihrem Platz. Übertragung verschlüsselt im lokalen Netzwerk.", "Originals stay in place. Encrypted transfer over your local network."), 12, secondary: true);
        hint.Margin = new Thickness(0, 7, 0, 14); drop.Children.Add(hint);
        drop.Children.Add(Horizontal(Theme.Button(c.T("Dateien wählen …", "Choose files …"), PickFiles), Theme.Button(c.T("Ordner wählen …", "Choose folder …"), PickFolder)));
        selection = new ListBox { MaxHeight = 130, BorderThickness = new Thickness(0), Margin = new Thickness(0, 12, 0, 8), HorizontalContentAlignment = HorizontalAlignment.Stretch };
        drop.Children.Add(selection);
        send = Action(c.T("Senden", "Send"), c.SendAsync, true);
        drop.Children.Add(Horizontal(send, Theme.Button(c.T("Auswahl leeren", "Clear selection"), () => c.Selection.Clear())));
        var dropCard = Theme.Card(drop, 22); dropCard.Margin = new Thickness(0, 16, 0, 22); dropCard.BorderBrush = Theme.Accent; page.Children.Add(dropCard);
        page.Children.Add(Theme.Text(c.T("Übertragungen", "Transfers"), 20, true));
        empty = Theme.Text(c.T("Noch keine Übertragungen. Wähle links ein Gerät aus und lege Dateien ab.", "No transfers yet. Select a device on the left and drop some files."), 13, secondary: true);
        empty.Margin = new Thickness(0, 12, 0, 0); page.Children.Add(empty);
        transfers = new StackPanel(); page.Children.Add(transfers);
        var scroll = new ScrollViewer { Content = page, VerticalScrollBarVisibility = ScrollBarVisibility.Auto, HorizontalScrollBarVisibility = ScrollBarVisibility.Disabled };
        Grid.SetColumn(scroll, 1); layout.Children.Add(scroll); Content = layout;
    }

    private void PickFiles()
    {
        var picker = new OpenFileDialog { Multiselect = true, Title = c.T("Dateien senden", "Send files"), CheckFileExists = true };
        if (picker.ShowDialog(this) == true) _ = c.RunAsync(() => { c.AddPaths(picker.FileNames); return Task.CompletedTask; });
    }
    private void PickFolder()
    {
        var picker = new OpenFolderDialog { Multiselect = true, Title = c.T("Ordner senden", "Send folders") };
        if (picker.ShowDialog(this) == true) _ = c.RunAsync(() => { c.AddPaths(picker.FolderNames); return Task.CompletedTask; });
    }

    private void Render()
    {
        if (rendering) return;
        rendering = true;
        try
        {
            if (appearance != (c.Status?.Settings.Appearance ?? "system") || language != (c.English ? "en" : "de")) Build();
            host.Text = c.Status?.Name ?? Environment.MachineName;
            service.Text = c.Ready ? c.T("Bereit · lokal verschlüsselt · Fenster schließen lässt AirLink im Infobereich weiterlaufen", "Ready · locally encrypted · closing the window keeps AirLink running in the system tray") : c.Starting ? c.T("AirLink wird gestartet …", "Starting AirLink …") : c.T("Dienst nicht bereit", "Service not ready");
            error.Text = c.Error ?? ""; error.Visibility = string.IsNullOrEmpty(c.Error) ? Visibility.Collapsed : Visibility.Visible;
            notice.Text = c.Notice ?? ""; notice.Visibility = string.IsNullOrEmpty(c.Notice) ? Visibility.Collapsed : Visibility.Visible;
            retry.Visibility = recover.Visibility = !c.Ready && !c.Starting ? Visibility.Visible : Visibility.Collapsed;
            var peers = c.Status?.Devices ?? [];
            foreach (var gone in deviceItems.Keys.Where(id => peers.All(d => d.Id != id)).ToArray()) { devices.Items.Remove(deviceItems[gone]); deviceItems.Remove(gone); }
            foreach (var d in peers.OrderByDescending(d => d.Paired).ThenBy(d => d.Name))
            {
                if (!deviceItems.TryGetValue(d.Id, out var row))
                {
                    row = new ListBoxItem { Tag = d.Id, Padding = new Thickness(8, 10, 8, 10), HorizontalContentAlignment = HorizontalAlignment.Stretch };
                    deviceItems.Add(d.Id, row); devices.Items.Add(row);
                }
                row.Content = Theme.Text(d.Name + "\n" + c.T(d.Paired ? d.Online ? "Verbunden" : "Vertraut · offline" : "In der Nähe · nicht gekoppelt", d.Paired ? d.Online ? "Connected" : "Trusted · offline" : "Nearby · not paired"), 13);
                row.IsSelected = d.Id == c.SelectedDeviceId;
            }
            var selected = c.SelectedDevice;
            target.Text = selected is null ? c.T("Gerät auswählen", "Choose a device") : selected.Name;
            targetDetail.Text = selected is null ? c.T("Geräte im gleichen Netzwerk erscheinen links automatisch.", "Devices on the same network appear on the left automatically.") : selected.Paired ? c.T(selected.Online ? "Verbunden und gekoppelt" : "Vertraut · zurzeit nicht erreichbar", selected.Online ? "Connected and paired" : "Trusted · currently unavailable") : c.T("Einmal verbinden und auf dem anderen Gerät bestätigen.", "Connect once and approve on the other device.");
            pair.Visibility = selected is { Paired: false } ? Visibility.Visible : Visibility.Collapsed;
            pair.IsEnabled = c.Ready;
            manage.Visibility = automatic.Visibility = selected is { Paired: true } ? Visibility.Visible : Visibility.Collapsed;
            manage.IsEnabled = automatic.IsEnabled = c.Ready;
            automatic.IsChecked = selected?.AutoAccept == true;
            send.IsEnabled = c.Ready && !c.Sending && c.Selection.Count > 0 && selected is { Paired: true, CanReceive: true };
            send.Content = c.Sending ? c.T("Wird vorbereitet …", "Preparing …") : c.T("Senden", "Send");
            var key = string.Join('|', c.Selection.Select(i => i.Id));
            if (key != selectedKey)
            {
                selectedKey = key; selection.Items.Clear();
                foreach (var item in c.Selection)
                {
                    var line = new DockPanel(); var remove = Theme.Button("×", () => c.Selection.Remove(item));
                    remove.ToolTip = c.T("Aus Auswahl entfernen", "Remove from selection"); DockPanel.SetDock(remove, Dock.Right); line.Children.Add(remove);
                    var label = Theme.Text(item.Title, 13); label.ToolTip = item.Path; line.Children.Add(label); selection.Items.Add(line);
                }
            }
            selection.Visibility = c.Selection.Count == 0 ? Visibility.Collapsed : Visibility.Visible;
            var pending = c.Status?.PairRequests ?? [];
            foreach (var gone in pairRows.Keys.Where(id => pending.All(p => p.Id != id)).ToArray()) { requests.Children.Remove(pairRows[gone]); pairRows.Remove(gone); }
            foreach (var request in pending.Where(p => !pairRows.ContainsKey(p.Id)))
            {
                var body = new StackPanel(); body.Children.Add(Theme.Text(request.Name + c.T(" möchte sich verbinden", " wants to connect"), 16, true));
                body.Children.Add(Theme.Text(c.T("Nur bestätigen, wenn du die Kopplung auf deinem anderen Gerät gestartet hast.", "Approve only if you started pairing on your other device."), 12, secondary: true));
                var trust = new CheckBox { Content = c.T("Von diesem Gerät künftig automatisch empfangen", "Automatically receive from this device in future") }; body.Children.Add(trust);
                body.Children.Add(Horizontal(Action(c.T("Verbinden", "Connect"), () => c.CommandAsync(trust.IsChecked == true ? "pair.approveTrusted" : "pair.approve", request.Id), true), Action(c.T("Ablehnen", "Decline"), () => c.CommandAsync("pair.deny", request.Id))));
                var card = Theme.Card(body); card.Margin = new Thickness(0, 8, 0, 0); pairRows.Add(request.Id, card); requests.Children.Add(card);
            }
            var jobs = c.Status?.Transfers ?? [];
            empty.Visibility = jobs.Count == 0 ? Visibility.Visible : Visibility.Collapsed;
            foreach (var gone in transferRows.Keys.Where(id => jobs.All(t => t.Id != id)).ToArray()) { transfers.Children.Remove(transferRows[gone]); transferRows.Remove(gone); }
            foreach (var job in jobs.OrderByDescending(t => t.IsActive).ThenByDescending(t => t.CreatedAt))
            {
                if (!transferRows.TryGetValue(job.Id, out var row)) { row = new TransferRow(this, c); transferRows.Add(job.Id, row); transfers.Children.Add(row); }
                row.Update(job);
            }
        }
        finally { rendering = false; }
    }
}

internal sealed class TransferRow : Border
{
    private readonly MainWindow owner;
    private readonly Controller c;
    private readonly TextBlock title, state, detail, problem;
    private readonly ProgressBar progress;
    private readonly StackPanel actions, files;
    private string actionKey = "";
    private Transfer current = new();
    public TransferRow(MainWindow owner, Controller controller)
    {
        this.owner = owner; c = controller;
        Background = Theme.Surface; BorderBrush = Theme.Line; BorderThickness = new Thickness(1); CornerRadius = new CornerRadius(12); Padding = new Thickness(16); Margin = new Thickness(0, 12, 0, 0);
        var body = new StackPanel(); title = Theme.Text("", 16, true); body.Children.Add(title);
        state = Theme.Text("", 12, secondary: true); state.Margin = new Thickness(0, 4, 0, 10); body.Children.Add(state);
        progress = new ProgressBar { Minimum = 0, Maximum = 1, Height = 5, Foreground = Theme.Accent, Background = Theme.Line, BorderThickness = new Thickness(0) }; body.Children.Add(progress);
        detail = Theme.Text("", 12, secondary: true); detail.Margin = new Thickness(0, 7, 0, 8); body.Children.Add(detail);
        problem = Theme.Text("", 12); problem.Margin = new Thickness(0, 0, 0, 8); body.Children.Add(problem);
        actions = new StackPanel { Orientation = Orientation.Horizontal }; body.Children.Add(actions);
        files = new StackPanel(); body.Children.Add(files); Child = body;
    }
    private Button Action(string label, Func<Task> task, bool primary = false) => Theme.Button(label, () => _ = c.RunAsync(task), primary);
    public void Update(Transfer t)
    {
        current = t;
        title.Text = t.Title + (t.Files.Count > 1 ? c.T(" · ", " · ") + t.Files.Count + c.T(" Einträge", " entries") : "");
        state.Text = (t.Direction == "receive" ? c.T("Von ", "From ") : c.T("An ", "To ")) + t.DeviceName + " · " + c.StateText(t.State);
        progress.Value = t.Progress;
        var speed = double.IsFinite(t.BytesPerSecond) && t.BytesPerSecond > 0 ? t.BytesPerSecond : 0;
        var remaining = t.TotalBytes - t.CompletedBytes - t.SkippedBytes;
        var seconds = speed > 0 ? Math.Max(0, remaining / speed) : 0;
        detail.Text = c.Bytes(t.CompletedBytes) + " / " + c.Bytes(t.TotalBytes) + (speed > 0 && t.State == "running" ? " · " + c.Bytes((long)Math.Min(speed, long.MaxValue)) + "/s" + (seconds < 86400 ? " · ~" + Math.Max(1, (int)Math.Ceiling(seconds / 60)) + c.T(" Min.", " min") : "") : "") + (t.SkippedBytes > 0 ? " · " + c.Bytes(t.SkippedBytes) + c.T(" übersprungen", " skipped") : "");
        problem.Text = t.Error ?? ""; problem.Visibility = string.IsNullOrWhiteSpace(t.Error) ? Visibility.Collapsed : Visibility.Visible;
        // Keep action controls stable while the user chooses trust or a duplicate.
        var key = t.State + ":" + c.Ready + ":" + string.Join('|', t.Files.Select(f => f.Id + f.Completed + f.Skipped + f.DuplicateState));
        if (key == actionKey) return;
        actionKey = key; actions.Children.Clear(); files.Children.Clear();
        if (t.State == "offered" && t.Direction == "receive")
        {
            var trust = new CheckBox { Content = c.T("Künftig automatisch", "Automatically in future"), Margin = new Thickness(0, 8, 12, 8) };
            trust.Visibility = NativeExperience.CanAcceptAndTrust(t, c.Status?.Devices.FirstOrDefault(d => d.Id == t.DeviceId)) ? Visibility.Visible : Visibility.Collapsed;
            actions.Children.Add(Action(c.T("Annehmen", "Accept"), () => c.CommandAsync(trust.IsChecked == true ? "transfer.acceptTrusted" : "transfer.accept", current.Id), true)); actions.Children.Add(trust);
        }
        if (t.State is "running" or "queued" or "reconnecting" or "checkingDuplicate" or "waitingDuplicate") actions.Children.Add(Action(c.T("Pause", "Pause"), () => c.CommandAsync("transfer.pause", current.Id)));
        if (t.State is "paused" or "reconnecting") actions.Children.Add(Action(c.T("Fortsetzen", "Resume"), () => c.CommandAsync("transfer.resume", current.Id), true));
        if (t.IsActive) actions.Children.Add(Action(c.T("Abbrechen", "Cancel"), () => c.CommandAsync("transfer.cancel", current.Id)));
        foreach (var file in t.Files.Where(f => f.DuplicateState == "pending"))
            files.Children.Add(Theme.Button(file.Path + c.T(" · Duplikat entscheiden …", " · Resolve duplicate …"), () => { var fresh = current.Files.FirstOrDefault(f => f.Id == file.Id); if (fresh is not null) Dialogs.Duplicate(owner, c, current, fresh); }));
        if (t.Direction == "receive" && t.State == "completed")
        {
            var received = NativeExperience.ReceivedDisplayEntries(t);
            if (received.Count > 0)
            {
                actions.Children.Add(Action(c.T("Datei zeigen", "Show received files"), () => c.RevealAsync(current), true));
                actions.Children.Add(Action(c.T("Ordner öffnen", "Open folder"), () => c.RevealAsync(current, openFolder: true)));
                foreach (var file in received.Take(20))
                    files.Children.Add(Action(file.Path + " ↗", () => c.RevealAsync(current, file.Id)));
                if (t.Destination is { Length: > 0 }) files.Children.Add(Theme.Text(t.Destination, 11, secondary: true));
            }
            else files.Children.Add(Theme.Text(c.T("Alle Dateien wurden als Duplikate übersprungen.", "All files were skipped as duplicates."), 12, secondary: true));
        }
        actions.IsEnabled = files.IsEnabled = c.Ready;
    }
}
