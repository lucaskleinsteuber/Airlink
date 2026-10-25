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
    private StackPanel requests = null!, transfers = null!, recentTransfers = null!;
    private Expander history = null!;
    private Border dropArea = null!;
    private TextBlock host = null!, target = null!, targetDetail = null!, service = null!, notice = null!, error = null!, empty = null!, dropTitle = null!, selectionTitle = null!;
    private CheckBox automatic = null!;
    private Button send = null!, pair = null!, manage = null!, retry = null!, recover = null!, dismiss = null!, pickFiles = null!, pickFolder = null!, share = null!;
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
        DragOver += (_, e) => { e.Effects = e.Data.GetDataPresent(DataFormats.FileDrop) && InsideDropArea(e.OriginalSource as DependencyObject) && c.Ready && !c.Sending ? DragDropEffects.Copy : DragDropEffects.None; e.Handled = true; };
        Drop += (_, e) => { if (InsideDropArea(e.OriginalSource as DependencyObject) && e.Data.GetData(DataFormats.FileDrop) is string[] paths) _ = c.RunAsync(() => c.SendPathsAsync(paths)); e.Handled = true; };
        PreviewKeyDown += (_, e) =>
        {
            if (Keyboard.Modifiers != ModifierKeys.Control) return;
            if (e.Key == Key.O) { PickFiles(); e.Handled = true; }
            if (e.Key == Key.Enter) { _ = c.RunAsync(c.SendAsync); e.Handled = true; }
        };
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
    private bool InsideDropArea(DependencyObject? source)
    {
        while (source is not null)
        {
            if (ReferenceEquals(source, dropArea)) return true;
            source = source is System.Windows.Media.Visual || source is System.Windows.Media.Media3D.Visual3D ? VisualTreeHelper.GetParent(source) : (source as FrameworkContentElement)?.Parent;
        }
        return false;
    }
    public void ShowMain() { Show(); if (WindowState == WindowState.Minimized) WindowState = WindowState.Normal; Activate(); }
    public void ReportError(string message) => c.ReportError(message);

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
        bottom.Children.Add(Theme.Button("Quick Share", () => SharingDialogs.Share(this, c)));
        bottom.Children.Add(Theme.Button(c.T("Zwischenablage", "Clipboard"), () => SharingDialogs.Clipboard(this, c)));
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
        dismiss = Theme.Button(c.T("Meldung schließen", "Dismiss message"), c.DismissError); page.Children.Add(dismiss);
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
        dropTitle = Theme.Text("", 19, true); drop.Children.Add(dropTitle);
        var hint = Theme.Text(c.T("Die Originale bleiben an ihrem Platz. Übertragung verschlüsselt im lokalen Netzwerk.", "Originals stay in place. Encrypted transfer over your local network."), 12, secondary: true);
        hint.Margin = new Thickness(0, 7, 0, 14); drop.Children.Add(hint);
        pickFiles = Theme.Button(c.T("Dateien senden …", "Send files …"), PickFiles, true);
        pickFolder = Theme.Button(c.T("Ordner senden …", "Send folders …"), PickFolder);
        drop.Children.Add(Horizontal(pickFiles, pickFolder));
        selectionTitle = Theme.Text(c.T("Vorbereitete Auswahl", "Prepared selection"), 13, true); selectionTitle.Margin = new Thickness(0, 14, 0, 0); drop.Children.Add(selectionTitle);
        selection = new ListBox { MaxHeight = 130, BorderThickness = new Thickness(0), Margin = new Thickness(0, 12, 0, 8), HorizontalContentAlignment = HorizontalAlignment.Stretch };
        drop.Children.Add(selection);
        send = Action(c.T("Senden", "Send"), c.SendAsync, true);
        share = Theme.Button(c.T("Freigabelink …", "Share link …"), () => SharingDialogs.Share(this, c, paths: c.Selection.Select(p => p.Path).ToArray()));
        drop.Children.Add(Horizontal(send, share, Theme.Button(c.T("Auswahl leeren", "Clear selection"), () => c.Selection.Clear())));
        var shortcut = Theme.Text(c.T("Strg+O: Dateien wählen · Strg+Enter: vorbereitete Auswahl senden", "Ctrl+O: choose files · Ctrl+Enter: send prepared selection"), 11, secondary: true); shortcut.Margin = new Thickness(0, 12, 0, 0); drop.Children.Add(shortcut);
        dropArea = Theme.Card(drop, 22); dropArea.Margin = new Thickness(0, 16, 0, 22); dropArea.BorderBrush = Theme.Accent; page.Children.Add(dropArea);
        page.Children.Add(Theme.Text(c.T("Übertragungen", "Transfers"), 20, true));
        empty = Theme.Text(c.T("Noch keine Übertragungen. Wähle links ein Gerät aus und lege Dateien ab.", "No transfers yet. Select a device on the left and drop some files."), 13, secondary: true);
        empty.Margin = new Thickness(0, 12, 0, 0); page.Children.Add(empty);
        transfers = new StackPanel(); page.Children.Add(transfers);
        recentTransfers = new StackPanel();
        history = new Expander { Content = recentTransfers, IsExpanded = true, Foreground = Theme.Foreground, Margin = new Thickness(0, 20, 0, 0) }; page.Children.Add(history);
        var scroll = new ScrollViewer { Content = page, VerticalScrollBarVisibility = ScrollBarVisibility.Auto, HorizontalScrollBarVisibility = ScrollBarVisibility.Disabled };
        Grid.SetColumn(scroll, 1); layout.Children.Add(scroll); Content = layout;
    }

    private void PickFiles()
    {
        if (!c.Ready || c.Sending) return;
        var recipient = c.SelectedDeviceId;
        var picker = new OpenFileDialog { Multiselect = true, Title = c.T("Dateien senden", "Send files"), CheckFileExists = true };
        if (picker.ShowDialog(this) == true) _ = c.RunAsync(() => c.SendPathsAsync(picker.FileNames, recipient, useCurrentSelection: false));
    }
    private void PickFolder()
    {
        if (!c.Ready || c.Sending) return;
        var recipient = c.SelectedDeviceId;
        var picker = new OpenFolderDialog { Multiselect = true, Title = c.T("Ordner senden", "Send folders") };
        if (picker.ShowDialog(this) == true) _ = c.RunAsync(() => c.SendPathsAsync(picker.FolderNames, recipient, useCurrentSelection: false));
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
            dismiss.Visibility = c.HasActionError ? Visibility.Visible : Visibility.Collapsed;
            notice.Text = c.Notice ?? ""; notice.Visibility = string.IsNullOrEmpty(c.Notice) ? Visibility.Collapsed : Visibility.Visible;
            retry.Visibility = recover.Visibility = !c.Ready && !c.Starting ? Visibility.Visible : Visibility.Collapsed;
            var peers = c.Status?.Devices ?? [];
            foreach (var gone in deviceItems.Keys.Where(id => peers.All(d => d.Id != id)).ToArray()) { devices.Items.Remove(deviceItems[gone]); deviceItems.Remove(gone); }
            foreach (var d in peers.OrderByDescending(d => d.Paired).ThenBy(d => d.Name))
            {
                if (!deviceItems.TryGetValue(d.Id, out var row))
                {
                    row = new ListBoxItem { Tag = d.Id, Padding = new Thickness(8, 10, 8, 10), HorizontalContentAlignment = HorizontalAlignment.Stretch };
                    var deviceId = d.Id;
                    row.AllowDrop = true;
                    row.ToolTip = c.T("Dateien hier ablegen, um direkt an dieses Gerät zu senden. Doppelklick: Dateien wählen.", "Drop files here to send directly to this device. Double-click to choose files.");
                    row.DragOver += (_, e) => { e.Effects = e.Data.GetDataPresent(DataFormats.FileDrop) && c.Ready && !c.Sending && TransferDraft.CanSendTo(c.Status?.Devices.FirstOrDefault(p => p.Id == deviceId)) ? DragDropEffects.Copy : DragDropEffects.None; e.Handled = true; };
                    row.Drop += (_, e) =>
                    {
                        if (e.Data.GetData(DataFormats.FileDrop) is string[] paths && TransferDraft.CanSendTo(c.Status?.Devices.FirstOrDefault(p => p.Id == deviceId))) _ = c.RunAsync(() => c.SendPathsAsync(paths, deviceId));
                        e.Handled = true;
                    };
                    row.MouseDoubleClick += (_, e) => { c.SelectedDeviceId = deviceId; if (TransferDraft.CanSendTo(c.SelectedDevice)) PickFiles(); else if (c.SelectedDevice is { Paired: true } paired) Dialogs.Device(this, c, paired); else Dialogs.Pair(this, c, c.SelectedDevice); e.Handled = true; };
                    deviceItems.Add(d.Id, row); devices.Items.Add(row);
                }
                row.Content = Theme.Text(d.Name + "\n" + c.T(d.Paired ? d.Online ? "Verbunden" : "Vertraut · offline" : "In der Nähe · nicht gekoppelt", d.Paired ? d.Online ? "Connected" : "Trusted · offline" : "Nearby · not paired"), 13);
                row.IsSelected = d.Id == c.SelectedDeviceId;
            }
            var selected = c.SelectedDevice;
            target.Text = selected is null ? c.T("Wohin möchtest du senden?", "Where do you want to send?") : c.T("Senden an ", "Send to ") + selected.Name;
            targetDetail.Text = selected is null ? c.T("Wähle ein Gerät links aus oder ziehe Dateien direkt auf ein gekoppeltes Gerät.", "Choose a device on the left or drop files directly on a paired device.") : selected.Paired ? !TransferDraft.CanSendTo(selected) ? c.T("Für direkte Übertragungen ein Desktopgerät mit Empfangsrecht wählen.", "Choose a desktop device with receiving permission for direct transfers.") : c.T(selected.Online ? "Dateien ablegen oder auswählen — die Übertragung startet direkt.":"Offline · die Übertragung wartet in der Warteschlange, bis das Gerät erreichbar ist.", selected.Online ? "Drop or choose files — the transfer starts immediately." : "Offline · transfers wait in the queue until the device is reachable.") : c.T("Einmal verbinden und auf dem anderen Gerät bestätigen.", "Connect once and approve on the other device.");
            dropTitle.Text = TransferDraft.CanSendTo(selected) ? c.T("Hier ablegen → an ", "Drop here → send to ") + selected!.Name : c.T("Dateien ablegen · danach Empfänger wählen", "Drop files · then choose a recipient");
            pair.Visibility = selected is { Paired: false } ? Visibility.Visible : Visibility.Collapsed;
            pair.IsEnabled = c.Ready;
            manage.Visibility = automatic.Visibility = selected is { Paired: true } ? Visibility.Visible : Visibility.Collapsed;
            manage.IsEnabled = automatic.IsEnabled = c.Ready;
            automatic.IsChecked = selected?.AutoAccept == true;
            send.IsEnabled = c.Ready && !c.Sending && c.Selection.Count > 0 && TransferDraft.CanSendTo(selected);
            send.Content = c.Sending ? c.T("Wird vorbereitet …", "Preparing …") : selected is { Online: false } ? c.T("In Warteschlange", "Queue transfer") : selected is not null ? c.T("An ", "Send to ") + selected.Name + c.T(" senden", "") : c.T("Senden", "Send");
            pickFiles.IsEnabled = pickFolder.IsEnabled = c.Ready && !c.Sending;
            share.IsEnabled = c.Ready && c.Selection.Count > 0;
            selectionTitle.Visibility = send.Visibility = share.Visibility = c.Selection.Count > 0 ? Visibility.Visible : Visibility.Collapsed;
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
            var allJobs = c.Status?.Transfers ?? [];
            var jobs = allJobs.Where(t => t.IsActive).OrderByDescending(t => t.CreatedAt).Concat(NativeExperience.RecentTransfers(allJobs)).ToArray();
            empty.Visibility = jobs.All(t => !t.IsActive) ? Visibility.Visible : Visibility.Collapsed;
            empty.Text = c.T("Keine aktiven Übertragungen. Dateien auf ein Gerät oder die Ablagefläche ziehen.", "No active transfers. Drop files on a device or the drop area.");
            var terminalCount = allJobs.Count(t => !t.IsActive);
            history.Header = c.T("Letzte Übertragungen", "Recent transfers") + " · " + Math.Min(20, terminalCount) + (terminalCount > 20 ? c.T(" von ", " of ") + terminalCount : "");
            history.Visibility = allJobs.Any(t => !t.IsActive) ? Visibility.Visible : Visibility.Collapsed;
            foreach (var gone in transferRows.Keys.Where(id => jobs.All(t => t.Id != id)).ToArray()) { transfers.Children.Remove(transferRows[gone]); recentTransfers.Children.Remove(transferRows[gone]); transferRows.Remove(gone); }
            var activeIndex = 0; var recentIndex = 0;
            foreach (var job in jobs)
            {
                if (!transferRows.TryGetValue(job.Id, out var row)) { row = new TransferRow(this, c); transferRows.Add(job.Id, row); }
                var parent = job.IsActive ? transfers : recentTransfers; var index = job.IsActive ? activeIndex++ : recentIndex++;
                if (row.Parent != parent) { (row.Parent as Panel)?.Children.Remove(row); parent.Children.Insert(index, row); }
                else if (parent.Children.IndexOf(row) != index) { parent.Children.Remove(row); parent.Children.Insert(index, row); }
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
        if (t.State == "completed" && t.Files.Any(f => !f.Directory && !f.Skipped))
            actions.Children.Add(Theme.Button(c.T("Freigabelink …", "Share link …"), () => SharingDialogs.Share(owner, c, transfer: current)));
        actions.IsEnabled = files.IsEnabled = c.Ready;
    }
}
