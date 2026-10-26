using System;
using System.Collections.Generic;
using System.Globalization;
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
    private enum Page { Devices, Transfers, Settings }
    private readonly Controller c;
    private readonly WindowsUpdater updater;
    private readonly Dictionary<string, ListBoxItem> deviceItems = [];
    private readonly Dictionary<string, TransferRow> transferRows = [];
    private readonly Dictionary<string, Border> pairRows = [];
    private ListBox devices = null!, selection = null!;
    private StackPanel requests = null!, transfers = null!, recent = null!, queue = null!;
    private Grid pages = null!;
    private StackPanel globalStatus = null!;
    private FrameworkElement devicePage = null!, transferPage = null!, settingsPage = null!;
    private StackPanel deviceQueueHost = null!, transferQueueHost = null!;
    private TextBlock host = null!, target = null!, targetDetail = null!, service = null!, notice = null!, error = null!, empty = null!, dropTitle = null!, selectionTitle = null!, receivePath = null!, recentTitle = null!;
    private Border dropArea = null!, hero = null!, welcome = null!, trustedCard = null!;
    private CheckBox automaticReceive = null!;
    private Button cleanHistory = null!;
    private bool savingTrust;
    private string? savingTrustDevice;
    private bool savingTrustValue;
    private Button send = null!, connect = null!, manage = null!, retry = null!, recover = null!, dismiss = null!, pickFiles = null!, pickFolder = null!, setup = null!, transferNav = null!, settingsNav = null!, clear = null!, attention = null!, browserShare = null!;
    private Page page;
    private bool rendering, quitting, setupOpen;
    private string? appearance, language;
    private string selectedKey = "";
    private double sidebarWidth = 320;

    public MainWindow(Controller controller)
    {
        c = controller; updater = new WindowsUpdater(this, c); Title = "AirLink";
        var workArea = SystemParameters.WorkArea;
        Width = Math.Min(1120, workArea.Width); Height = Math.Min(800, workArea.Height);
        MinWidth = Math.Min(940, workArea.Width); MinHeight = Math.Min(650, workArea.Height);
        WindowStartupLocation = WindowStartupLocation.CenterScreen;
        FontFamily = new FontFamily("Segoe UI Variable, Segoe UI"); FontSize = 14; AllowDrop = true;
        DragOver += (_, e) => { e.Effects = e.Data.GetDataPresent(DataFormats.FileDrop) && InsideDropArea(e.OriginalSource as DependencyObject) && c.Ready && c.SetupComplete && !c.Sending ? DragDropEffects.Copy : DragDropEffects.None; e.Handled = true; };
        Drop += (_, e) => { if (InsideDropArea(e.OriginalSource as DependencyObject) && e.Data.GetData(DataFormats.FileDrop) is string[] paths) _ = c.RunAsync(() => c.SendPathsAsync(paths)); e.Handled = true; };
        PreviewKeyDown += (_, e) =>
        {
            if (page != Page.Devices || Keyboard.Modifiers != ModifierKeys.Control) return;
            if (e.Key == Key.O) { PickFiles(); e.Handled = true; }
            if (e.Key == Key.Enter) { _ = c.RunAsync(c.SendAsync); e.Handled = true; }
        };
        c.PropertyChanged += (_, _) => Render(); c.Selection.CollectionChanged += (_, _) => Render();
        SourceInitialized += (_, _) => HwndSource.FromHwnd(new WindowInteropHelper(this).Handle)?.AddHook(WindowMessage);
        Closing += (_, e) => { if (!quitting) { e.Cancel = true; Hide(); } };
        Closed += (_, _) => updater.Dispose();
        Loaded += async (_, _) => { await c.StartAsync(); _ = c.PollAsync(); if (c.SetupRequired) OpenSetup(); };
        Build(); Render();
    }
    private IntPtr WindowMessage(IntPtr window, int message, IntPtr w, IntPtr l, ref bool handled)
    {
        if ((uint)message == Program.ShowMessage) { ShowMain(); handled = true; } return IntPtr.Zero;
    }
    private bool InsideDropArea(DependencyObject? source)
    {
        if (page != Page.Devices) return false;
        while (source is not null)
        {
            if (ReferenceEquals(source, dropArea)) return true;
            source = source is Visual || source is System.Windows.Media.Media3D.Visual3D ? VisualTreeHelper.GetParent(source) : (source as FrameworkContentElement)?.Parent;
        }
        return false;
    }
    public void ShowMain() { Show(); if (WindowState == WindowState.Minimized) WindowState = WindowState.Normal; Activate(); }
    public void ReportError(string message) => c.ReportError(message);
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
                ReportError(c.T("Das Update wurde abgebrochen: Der Dienst konnte nicht sicher beendet werden. Laufende Prozesse wurden nicht erzwungen beendet.", "Update cancelled: the service could not close safely. No running process was forcibly stopped."));
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
        if (confirmActive && c.ActiveCount > 0 && MessageBox.Show(this, c.T("Übertragungen werden gesichert und beim nächsten Start fortgesetzt. AirLink beenden?", "Transfers will be saved and resumed next time. Quit AirLink?"), "AirLink", MessageBoxButton.YesNo, MessageBoxImage.Question) != MessageBoxResult.Yes) return;
        quitting = true;
        try
        {
            if (!await c.StopAsync()) { quitting = false; ReportError(c.T("Der Dienst läuft noch. Bitte erneut beenden.", "The service is still running. Please try quitting again.")); return; }
            Application.Current.Shutdown();
        }
        catch (Exception e) { quitting = false; ReportError(e.Message); }
    }
    private void OpenSetup()
    {
        if (setupOpen || !c.Ready) return; setupOpen = true;
        try { new SetupWindow(this, c).ShowDialog(); } finally { setupOpen = false; Render(); }
    }
    private void AfterSetup(Action action)
    {
        if (!c.Ready) return;
        if (!c.SetupComplete) OpenSetup();
        if (c.SetupComplete) action();
    }
    private void PairDevice(Device? seed) => AfterSetup(() => Dialogs.Pair(this, c, seed));
    private Button Action(string title, Func<Task> action, bool primary = false) => Theme.Button(title, () => _ = c.RunAsync(action), primary);
    private static StackPanel Row(params UIElement[] children)
    {
        var panel = new StackPanel { Orientation = Orientation.Horizontal }; foreach (var child in children) panel.Children.Add(child); return panel;
    }
    private static WrapPanel Wrap(params UIElement[] children)
    {
        var panel = new WrapPanel(); foreach (var child in children) panel.Children.Add(child); return panel;
    }
    private Button Navigation(string title, string icon, Action action)
    {
        var button = Theme.Button(title, action); button.HorizontalContentAlignment = HorizontalAlignment.Left;
        button.Margin = new Thickness(0, 2, 0, 2); button.Padding = new Thickness(12, 9, 12, 9); button.Background = Brushes.Transparent; button.BorderBrush = Brushes.Transparent;
        var graphic = Theme.Icon(icon, 19); var label = Theme.Text(title); label.Margin = new Thickness(12, 0, 0, 0); button.Content = Row(graphic, label); return button;
    }
    private void Navigate(Page next)
    {
        page = next;
        if (next == Page.Settings) { pages.Children.Remove(settingsPage); settingsPage = BuildSettings(); pages.Children.Add(settingsPage); }
        Render();
    }
    private void Build()
    {
        appearance = c.Status?.Settings.Appearance ?? "system"; language = c.English ? "en" : "de";
        Theme.Apply(appearance); Background = Theme.Background; Foreground = Theme.Foreground;
        deviceItems.Clear(); transferRows.Clear(); pairRows.Clear(); selectedKey = "";
        var layout = new Grid(); var sidebarColumn = new ColumnDefinition { Width = new GridLength(sidebarWidth), MinWidth = 280, MaxWidth = 480 };
        layout.ColumnDefinitions.Add(sidebarColumn); layout.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(8) }); layout.ColumnDefinitions.Add(new ColumnDefinition());
        var sidebar = new DockPanel { Background = Theme.Sidebar, LastChildFill = true };
        var top = new StackPanel { Margin = new Thickness(22, 24, 18, 14) };
        var brand = Theme.Text("AirLink", 24, true); brand.Margin = new Thickness(4, 0, 0, 22); top.Children.Add(brand);
        top.Children.Add(Navigation(c.T("Geräte", "Devices"), "computer", () => Navigate(Page.Devices)));
        var devicesHeading = new DockPanel { Margin = new Thickness(4, 15, 0, 0) }; var add = Theme.Button("+", () => PairDevice(null)); add.ToolTip = c.T("Gerät verbinden", "Connect device"); add.Margin = new Thickness(0); add.Padding = new Thickness(10, 2, 10, 2); add.MinHeight = 28;
        add.Background = Brushes.Transparent; add.BorderBrush = Brushes.Transparent; DockPanel.SetDock(add, Dock.Right); devicesHeading.Children.Add(add); devicesHeading.Children.Add(Theme.Text(c.T("Vertraute Geräte & Netzwerk", "Trusted devices & network"), 12, true, true)); top.Children.Add(devicesHeading);
        DockPanel.SetDock(top, Dock.Top); sidebar.Children.Add(top);
        var bottom = new StackPanel { Margin = new Thickness(14, 10, 14, 18) };
        transferNav = Navigation(c.T("Übertragungen", "Transfers"), "history", () => Navigate(Page.Transfers)); bottom.Children.Add(transferNav);
        settingsNav = Navigation(c.T("Einstellungen", "Settings"), "settings", () => Navigate(Page.Settings)); bottom.Children.Add(settingsNav);
        bottom.Children.Add(Navigation(c.T("Dieses Gerät", "This device"), "computer", () => AfterSetup(() => Dialogs.ConnectionInfo(this, c))));
        var tools = new StackPanel(); tools.Children.Add(Navigation("Quick Share", "link", () => AfterSetup(() => SharingDialogs.Share(this, c)))); tools.Children.Add(Navigation(c.T("Zwischenablage", "Clipboard"), "clipboard", () => AfterSetup(() => SharingDialogs.Clipboard(this, c))));
        tools.Children.Add(Navigation(c.T("Handy verbinden", "Connect phone"), "link", () => AfterSetup(() => Dialogs.ConnectionInfo(this, c))));
        bottom.Children.Add(new Expander { Header = Theme.Text(c.T("Weitere Funktionen", "More tools"), 12, secondary: true), Content = tools, Foreground = Theme.Secondary, Margin = new Thickness(12, 12, 0, 18) });
        var local = new Grid(); local.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(36) }); local.ColumnDefinitions.Add(new ColumnDefinition()); local.Children.Add(Theme.Icon("computer", 24));
        var localText = new StackPanel(); host = Theme.Text("", 14, true); host.MaxHeight = 42; host.ToolTip = host.Text; localText.Children.Add(host); localText.Children.Add(Theme.Text(c.T("Dieser Computer", "This computer"), 11, secondary: true)); Grid.SetColumn(localText, 1); local.Children.Add(localText);
        var profile = Theme.Button("", () => Navigate(Page.Settings)); profile.Content = local; profile.Background = Brushes.Transparent; profile.BorderBrush = Theme.Line; profile.HorizontalContentAlignment = HorizontalAlignment.Stretch; profile.Margin = new Thickness(0); bottom.Children.Add(profile);
        DockPanel.SetDock(bottom, Dock.Bottom); sidebar.Children.Add(bottom);
        devices = new ListBox { BorderThickness = new Thickness(0), Padding = new Thickness(0), Margin = new Thickness(12, 0, 12, 0), Background = Brushes.Transparent, HorizontalContentAlignment = HorizontalAlignment.Stretch };
        ScrollViewer.SetHorizontalScrollBarVisibility(devices, ScrollBarVisibility.Disabled);
        devices.SelectionChanged += (_, _) => { if (rendering) return; if ((devices.SelectedItem as ListBoxItem)?.Tag is string id) { c.SelectedDeviceId = id; Navigate(Page.Devices); } };
        sidebar.Children.Add(devices); layout.Children.Add(sidebar);
        var divider = new Border { Background = Theme.Line, Width = 1, HorizontalAlignment = HorizontalAlignment.Center }; Grid.SetColumn(divider, 1); layout.Children.Add(divider);
        var splitter = new GridSplitter { Width = 8, Background = Brushes.Transparent, HorizontalAlignment = HorizontalAlignment.Right, VerticalAlignment = VerticalAlignment.Stretch, ResizeDirection = GridResizeDirection.Columns, ResizeBehavior = GridResizeBehavior.PreviousAndNext };
        splitter.DragCompleted += (_, _) => sidebarWidth = sidebarColumn.ActualWidth; Grid.SetColumn(splitter, 1); layout.Children.Add(splitter);
        var main = new DockPanel(); Grid.SetColumn(main, 2); layout.Children.Add(main);
        globalStatus = new StackPanel { Margin = new Thickness(34, 0, 34, 0) }; DockPanel.SetDock(globalStatus, Dock.Top); main.Children.Add(globalStatus);
        pages = new Grid(); main.Children.Add(pages);
        devicePage = BuildDevicePage(); transferPage = BuildTransferPage(); settingsPage = BuildSettings(); pages.Children.Add(devicePage); pages.Children.Add(transferPage); pages.Children.Add(settingsPage); Content = layout;
    }
    private FrameworkElement BuildDevicePage()
    {
        var body = new StackPanel { Margin = new Thickness(34, 28, 34, 30) };
        var header = new Grid(); header.ColumnDefinitions.Add(new ColumnDefinition()); header.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var identity = new StackPanel(); target = Theme.Text("", 28, true); identity.Children.Add(target); targetDetail = Theme.Text("", 13, secondary: true); targetDetail.Margin = new Thickness(0, 5, 0, 0); identity.Children.Add(targetDetail); header.Children.Add(identity);
        manage = Theme.Button(c.T("Geräteeinstellungen", "Device settings"), () => { if (c.SelectedDevice is { } d) Dialogs.Device(this, c, d); }); manage.Margin = new Thickness(16, 0, 0, 0); manage.VerticalAlignment = VerticalAlignment.Top; Grid.SetColumn(manage, 1); header.Children.Add(manage);
        hero = new Border { Child = header, Margin = new Thickness(0, 0, 0, 22) }; body.Children.Add(hero);
        var trust = new StackPanel(); trust.Children.Add(Theme.Text(c.T("Vertrautes Gerät", "Trusted device"), 14, true));
        var trustHelp = Theme.Text(c.T("Die Verbindung ist gespeichert. Du kannst Dateien direkt senden, sobald das Gerät erreichbar ist.", "The connection is saved. You can send files directly when the device is reachable."), 12, secondary: true); trustHelp.Margin = new Thickness(0, 5, 0, 12); trust.Children.Add(trustHelp);
        automaticReceive = new CheckBox { Content = Theme.Text(c.T("Von diesem Gerät automatisch empfangen", "Automatically receive from this device"), 13) };
        automaticReceive.Click += async (_, _) =>
        {
            if (rendering || savingTrust || c.SelectedDevice is not { } device) return;
            var id = device.Id; var automatic = automaticReceive.IsChecked == true;
            savingTrust = true; savingTrustDevice = id; savingTrustValue = automatic; Render();
            try { await c.RunAsync(() => c.SetAutomaticReceiveAsync(id, automatic)); }
            finally { savingTrust = false; savingTrustDevice = null; Render(); }
        };
        trust.Children.Add(automaticReceive);
        var automaticHelp = Theme.Text(c.T("Gilt nur für den Empfang auf diesem PC. Die Einstellung des anderen Computers bestimmt, ob er deine Dateien automatisch annimmt.", "Applies to receiving on this PC. The other computer's setting decides whether it accepts your files automatically."), 12, secondary: true); automaticHelp.Margin = new Thickness(0, 7, 0, 0); trust.Children.Add(automaticHelp);
        trustedCard = Theme.Card(trust); trustedCard.Margin = new Thickness(0, 0, 0, 20); body.Children.Add(trustedCard);
        var welcomeBody = new StackPanel(); var welcomeIcon = Theme.Icon("send", 48, Theme.Accent); welcomeBody.Children.Add(welcomeIcon);
        var welcomeTitle = Theme.Text(c.T("Deine Geräte. Ein direkter Weg.", "Your devices. One direct connection."), 25, true); welcomeTitle.Margin = new Thickness(0, 18, 0, 9); welcomeBody.Children.Add(welcomeTitle);
        welcomeBody.Children.Add(Theme.Text(c.T("Öffne AirLink auf dem anderen Computer. Wähle ihn links aus und ziehe eine Datei hinein.", "Open AirLink on the other computer. Choose it on the left and drop a file."), 14, secondary: true));
        connect = Theme.Button(c.T("Gerät verbinden", "Connect device"), () => PairDevice(c.SelectedDevice is { Paired: false } d ? d : null), true); connect.Margin = new Thickness(0, 20, 0, 0); connect.HorizontalAlignment = HorizontalAlignment.Left; welcomeBody.Children.Add(connect);
        welcome = Theme.Card(welcomeBody, 28); welcome.Margin = new Thickness(0, 0, 0, 20); body.Children.Add(welcome);
        browserShare = Theme.Button(c.T("Dateien per Freigabelink teilen …", "Share files by link …"), () => AfterSetup(() => SharingDialogs.Share(this, c)), true); browserShare.HorizontalAlignment = HorizontalAlignment.Left; browserShare.Margin = new Thickness(0, 0, 0, 20); body.Children.Add(browserShare);
        setup = Theme.Button(c.T("Einrichtung abschließen", "Finish setup"), OpenSetup, true); setup.Margin = new Thickness(0, 20, 0, 18); setup.HorizontalAlignment = HorizontalAlignment.Left; globalStatus.Children.Add(setup);
        service = Theme.Text("", 12, secondary: true); service.Margin = new Thickness(0, 16, 0, 10); globalStatus.Children.Add(service);
        error = Theme.Text("", 13); error.Foreground = Theme.Color(Theme.Dark ? "#FFB5AE" : "#B1372A"); globalStatus.Children.Add(error);
        dismiss = Theme.Button(c.T("Meldung schließen", "Dismiss"), c.DismissError); dismiss.Margin = new Thickness(0, 8, 0, 12); dismiss.HorizontalAlignment = HorizontalAlignment.Left; globalStatus.Children.Add(dismiss);
        retry = Action(c.T("Erneut starten", "Retry"), async () => { await c.StartAsync(); if (c.SetupRequired) OpenSetup(); }); retry.HorizontalAlignment = HorizontalAlignment.Left; retry.Margin = new Thickness(0, 8, 0, 15); globalStatus.Children.Add(retry);
        recover = Action(c.T("Empfangsordner ändern und starten …", "Choose receive folder and retry …"), async () =>
        {
            var picker = new OpenFolderDialog { Title = c.T("Erreichbaren Empfangsordner wählen", "Choose an available receive folder") };
            if (picker.ShowDialog(this) == true) { await c.StartAsync(picker.FolderName); if (c.SetupRequired) OpenSetup(); }
        });
        recover.HorizontalAlignment = HorizontalAlignment.Left; recover.Margin = new Thickness(0, 0, 0, 15); globalStatus.Children.Add(recover);
        notice = Theme.Text("", 13, secondary: true); notice.Margin = new Thickness(0, 0, 0, 10); globalStatus.Children.Add(notice);
        attention = Theme.Button("", () => Navigate(Page.Devices)); attention.Margin = new Thickness(0, 0, 0, 10); attention.HorizontalAlignment = HorizontalAlignment.Left; globalStatus.Children.Add(attention);
        requests = new StackPanel(); body.Children.Add(requests);
        var drop = new StackPanel { HorizontalAlignment = HorizontalAlignment.Stretch };
        var icon = Theme.Icon("send", 38, Theme.Accent); icon.SetValue(FrameworkElement.HorizontalAlignmentProperty, HorizontalAlignment.Center); drop.Children.Add(icon);
        dropTitle = Theme.Text("", 21, true); dropTitle.TextAlignment = TextAlignment.Center; dropTitle.Margin = new Thickness(0, 16, 0, 7); drop.Children.Add(dropTitle);
        var hint = Theme.Text(c.T("Dateien und Ordner hier ablegen", "Drop files and folders here"), 14, secondary: true); hint.TextAlignment = TextAlignment.Center; drop.Children.Add(hint);
        pickFiles = Theme.Button(c.T("Dateien auswählen", "Choose files"), PickFiles, true); pickFolder = Theme.Button(c.T("Ordner auswählen", "Choose folder"), PickFolder);
        var choose = Wrap(pickFiles, pickFolder); choose.HorizontalAlignment = HorizontalAlignment.Center; choose.Margin = new Thickness(0, 22, 0, 0); drop.Children.Add(choose);
        dropArea = Theme.Card(drop, 30); dropArea.Margin = new Thickness(0, 5, 0, 17); body.Children.Add(dropArea);
        var receiving = Action("", () => { c.OpenReceiveFolder(); return Task.CompletedTask; }); receiving.Background = Brushes.Transparent; receiving.BorderBrush = Brushes.Transparent; receiving.HorizontalContentAlignment = HorizontalAlignment.Left; receiving.Padding = new Thickness(0, 5, 0, 5);
        receivePath = Theme.Text("", 12, secondary: true); receivePath.TextTrimming = TextTrimming.CharacterEllipsis; receivePath.TextWrapping = TextWrapping.NoWrap; receiving.Content = receivePath; body.Children.Add(receiving);
        selectionTitle = Theme.Text(c.T("Zum Senden vorbereitet", "Ready to send"), 14, true); selectionTitle.Margin = new Thickness(0, 20, 0, 5); body.Children.Add(selectionTitle);
        selection = new ListBox { MaxHeight = 150, BorderThickness = new Thickness(0), Padding = new Thickness(0), Background = Brushes.Transparent, HorizontalContentAlignment = HorizontalAlignment.Stretch }; body.Children.Add(selection);
        send = Action(c.T("Senden", "Send"), c.SendAsync, true); clear = Theme.Button(c.T("Auswahl leeren", "Clear"), () => c.Selection.Clear()); body.Children.Add(Wrap(send, clear));
        deviceQueueHost = new StackPanel { Margin = new Thickness(0, 28, 0, 0) }; body.Children.Add(deviceQueueHost);
        return new ScrollViewer { Content = body, VerticalScrollBarVisibility = ScrollBarVisibility.Auto, HorizontalScrollBarVisibility = ScrollBarVisibility.Disabled };
    }
    private FrameworkElement BuildTransferPage()
    {
        var body = new StackPanel { Margin = new Thickness(34, 28, 34, 30) }; body.Children.Add(Theme.Text(c.T("Übertragungen", "Transfers"), 28, true));
        var subtitle = Theme.Text(c.T("Aktuelle Übertragungen und dein Verlauf. Fertige Einträge kannst du entfernen; die Dateien bleiben erhalten.", "Active transfers and your history. You can remove finished entries; the files are preserved."), 14, secondary: true); subtitle.Margin = new Thickness(0, 6, 0, 20); body.Children.Add(subtitle);
        transferQueueHost = new StackPanel(); body.Children.Add(transferQueueHost);
        queue = new StackPanel(); queue.Children.Add(Theme.Text(c.T("Aktuell", "Active"), 15, true));
        empty = Theme.Text(c.T("Keine aktiven Übertragungen", "No active transfers"), 13, secondary: true); empty.Margin = new Thickness(0, 10, 0, 10); queue.Children.Add(empty);
        transfers = new StackPanel(); queue.Children.Add(transfers);
        var recentHeader = new DockPanel { Margin = new Thickness(0, 23, 0, 0) };
        cleanHistory = Theme.Button(c.T("Fertige Einträge entfernen …", "Remove finished entries …"), RemoveFinishedHistory);
        cleanHistory.Margin = new Thickness(12, 0, 0, 0); cleanHistory.FontSize = 12; DockPanel.SetDock(cleanHistory, Dock.Right); recentHeader.Children.Add(cleanHistory);
        recentTitle = Theme.Text(c.T("Zuletzt", "Recent"), 15, true); recentTitle.VerticalAlignment = VerticalAlignment.Center; recentHeader.Children.Add(recentTitle); queue.Children.Add(recentHeader);
        recent = new StackPanel(); queue.Children.Add(recent);
        return new ScrollViewer { Content = body, VerticalScrollBarVisibility = ScrollBarVisibility.Auto, HorizontalScrollBarVisibility = ScrollBarVisibility.Disabled };
    }
    private void RemoveFinishedHistory()
    {
        var captured = (c.Status?.Transfers ?? []).Where(NativeExperience.CanRemoveFromHistory).Select(ProtocolJson.Clone).ToArray();
        if (!c.Ready || c.RemovingHistory || captured.Length == 0) return;
        if (MessageBox.Show(this,
            c.T($"{captured.Length} fertige Einträge aus der Liste entfernen? Dazu gehören auch ältere Einträge. Gesendete und empfangene Dateien bleiben erhalten. Laufende Übertragungen werden nicht entfernt.",
                $"Remove {captured.Length} finished entries from the list, including older entries? Sent and received files are preserved. Active transfers will remain."),
            c.T("Liste aufräumen", "Clean up list"), MessageBoxButton.YesNo, MessageBoxImage.Question) == MessageBoxResult.Yes)
            _ = c.RunAsync(() => c.RemoveHistoryAsync(captured));
    }
    private FrameworkElement BuildSettings()
    {
        var body = new StackPanel { Margin = new Thickness(34, 28, 34, 30) }; body.Children.Add(Theme.Text(c.T("Einstellungen", "Settings"), 28, true));
        var description = Theme.Text(c.T("Dein Computer und der Ort für empfangene Dateien.", "Your computer and where received files are saved."), 14, secondary: true); description.Margin = new Thickness(0, 6, 0, 24); body.Children.Add(description);
        var updates = new StackPanel(); updates.Children.Add(Theme.Text("AirLink " + c.Version, 17, true));
        var check = Theme.Button(c.T("Nach Updates suchen …", "Check for updates …"), () => _ = CheckForUpdatesAsync()); check.HorizontalAlignment = HorizontalAlignment.Left; check.Margin = new Thickness(0, 12, 0, 8); updates.Children.Add(check);
        updates.Children.Add(Theme.Text(c.T("Prüft nur auf deine Anfrage. In der privaten Testphase ist der Update-Feed möglicherweise nicht öffentlich erreichbar.", "Checks only when requested. During private testing, the update feed may not be publicly accessible."), 12, secondary: true));
        var updateCard = Theme.Card(updates); updateCard.Margin = new Thickness(0, 0, 0, 20); body.Children.Add(updateCard);
        if (c.Status is null) { body.Children.Add(Theme.Text(c.T("AirLink wird gestartet …", "Starting AirLink …"), secondary: true)); return new ScrollViewer { Content = body }; }
        var edited = ProtocolJson.Clone(c.Status.Settings);
        static TextBox Input(StackPanel panel, string label, string value)
        {
            var caption = Theme.Text(label, 13, true); caption.Margin = new Thickness(0, 15, 0, 6); panel.Children.Add(caption); var input = new TextBox { Text = value }; panel.Children.Add(input); return input;
        }
        var general = new StackPanel(); general.Children.Add(Theme.Text(c.T("Dieser Computer", "This computer"), 17, true));
        var name = Input(general, c.T("Gerätename", "Device name"), edited.Name); name.MaxLength = 120;
        general.Children.Add(Theme.Text(c.T("So sehen andere Geräte diesen Computer, zum Beispiel „Laptop Windows“.", "Other devices see this name, for example “Windows laptop”."), 12, secondary: true)); body.Children.Add(Theme.Card(general));
        var receiving = new StackPanel(); receiving.Children.Add(Theme.Text(c.T("Empfang", "Receiving"), 17, true));
        var folder = Input(receiving, c.T("Zielordner", "Save received files to"), edited.ReceiveDirectory);
        var choose = Theme.Button(c.T("Ordner ändern …", "Change folder …"), () => { var picker = new OpenFolderDialog { Title = c.T("Zielordner wählen", "Choose receive folder") }; if (System.IO.Directory.Exists(folder.Text)) picker.InitialDirectory = folder.Text; if (picker.ShowDialog(this) == true) folder.Text = picker.FolderName; }); choose.Margin = new Thickness(0, 10, 0, 8); choose.HorizontalAlignment = HorizontalAlignment.Left; receiving.Children.Add(choose);
        receiving.Children.Add(Theme.Text(c.T("Dateien landen direkt in diesem Ordner. Bestehende Dateien werden nicht überschrieben.", "Files are saved directly in this folder. Existing files are preserved."), 12, secondary: true));
        var receiveCard = Theme.Card(receiving); receiveCard.Margin = new Thickness(0, 16, 0, 0); body.Children.Add(receiveCard);
        var personal = new StackPanel(); personal.Children.Add(Theme.Text(c.T("Allgemein", "General"), 17, true));
        var appearanceField = new ComboBox { ItemsSource = new[] { c.T("Systemdarstellung", "System appearance"), c.T("Hell", "Light"), c.T("Dunkel", "Dark") }, SelectedIndex = Math.Max(0, Array.IndexOf(new[] { "system", "light", "dark" }, edited.Appearance)), Margin = new Thickness(0, 14, 0, 8) }; personal.Children.Add(appearanceField);
        var languageField = new ComboBox { ItemsSource = new[] { c.T("Systemsprache", "System language"), "Deutsch", "English" }, SelectedIndex = Math.Max(0, Array.IndexOf(new[] { "system", "de", "en" }, edited.Language)) }; personal.Children.Add(languageField);
        var notifications = new CheckBox { Content = c.T("Bei Empfang und Anfragen benachrichtigen", "Notify about received files and requests"), IsChecked = edited.Notifications, Margin = new Thickness(0, 14, 0, 0) }; personal.Children.Add(notifications);
        var personalCard = Theme.Card(personal); personalCard.Margin = new Thickness(0, 16, 0, 0); body.Children.Add(personalCard);
        var errorLabel = Theme.Text("", 13); errorLabel.Foreground = Theme.Color(Theme.Dark ? "#FFB5AE" : "#B1372A"); errorLabel.Margin = new Thickness(0, 14, 0, 10); body.Children.Add(errorLabel);
        var saving = false; var save = Theme.Button(c.T("Änderungen speichern", "Save changes"), () => { }, true); save.HorizontalAlignment = HorizontalAlignment.Left;
        save.Click += async (_, _) =>
        {
            if (saving) return; saving = true; save.IsEnabled = false; save.Content = c.T("Wird gespeichert …", "Saving …"); general.IsEnabled = receiving.IsEnabled = personal.IsEnabled = false; errorLabel.Text = "";
            try
            {
                SetupWindow.ValidateName(name.Text, c);
                if (!System.IO.Path.IsPathFullyQualified(folder.Text.Trim())) throw new ArgumentException(c.T("Bitte einen vollständigen Zielordner wählen.", "Choose an absolute receive folder."));
                edited.Name = name.Text.Trim(); edited.ReceiveDirectory = folder.Text.Trim(); edited.Appearance = new[] { "system", "light", "dark" }[appearanceField.SelectedIndex]; edited.Language = new[] { "system", "de", "en" }[languageField.SelectedIndex]; edited.Notifications = notifications.IsChecked == true;
                await c.SaveSettingsAsync(edited); errorLabel.Foreground = Theme.Accent; errorLabel.Text = c.T("Gespeichert", "Saved");
            }
            catch (Exception e) { errorLabel.Foreground = Theme.Color(Theme.Dark ? "#FFB5AE" : "#B1372A"); errorLabel.Text = e.Message; }
            finally { saving = false; save.IsEnabled = true; save.Content = c.T("Änderungen speichern", "Save changes"); general.IsEnabled = receiving.IsEnabled = personal.IsEnabled = true; }
        };
        body.Children.Add(save);
        var advanced = Theme.Button(c.T("Erweiterte Einstellungen …", "Advanced settings …"), () => { if (!saving) Dialogs.Settings(this, c); }); advanced.HorizontalAlignment = HorizontalAlignment.Left; advanced.Margin = new Thickness(0, 20, 0, 0); body.Children.Add(advanced);
        var about = new WrapPanel { Margin = new Thickness(0, 28, 0, 0) }; about.Children.Add(Theme.Text("AirLink " + c.Version, 12, secondary: true));
        var license = Theme.Button(c.T("Lizenzen", "Licenses"), () => NativeLicenses.Show(this, c)); license.Margin = new Thickness(18, -8, 0, 0); about.Children.Add(license); body.Children.Add(about);
        return new ScrollViewer { Content = body, VerticalScrollBarVisibility = ScrollBarVisibility.Auto, HorizontalScrollBarVisibility = ScrollBarVisibility.Disabled };
    }
    private void PickFiles()
    {
        if (!c.Ready || !c.SetupComplete || c.Sending) return; var recipient = c.SelectedDeviceId;
        var picker = new OpenFileDialog { Multiselect = true, Title = c.T("Dateien senden", "Send files"), CheckFileExists = true };
        if (picker.ShowDialog(this) == true) _ = c.RunAsync(() => c.SendPathsAsync(picker.FileNames, recipient, useCurrentSelection: false));
    }
    private void PickFolder()
    {
        if (!c.Ready || !c.SetupComplete || c.Sending) return; var recipient = c.SelectedDeviceId;
        var picker = new OpenFolderDialog { Multiselect = true, Title = c.T("Ordner senden", "Send folders") };
        if (picker.ShowDialog(this) == true) _ = c.RunAsync(() => c.SendPathsAsync(picker.FolderNames, recipient, useCurrentSelection: false));
    }
    private void Render()
    {
        if (rendering) return; rendering = true;
        try
        {
            if (appearance != (c.Status?.Settings.Appearance ?? "system") || language != (c.English ? "en" : "de")) Build();
            host.Text = c.Status?.Name ?? Environment.MachineName; host.ToolTip = host.Text;
            devicePage.Visibility = page == Page.Devices ? Visibility.Visible : Visibility.Collapsed; transferPage.Visibility = page == Page.Transfers ? Visibility.Visible : Visibility.Collapsed; settingsPage.Visibility = page == Page.Settings ? Visibility.Visible : Visibility.Collapsed;
            transferNav.Background = page == Page.Transfers ? Theme.SoftAccent : Brushes.Transparent; settingsNav.Background = page == Page.Settings ? Theme.SoftAccent : Brushes.Transparent;
            if (queue.Parent is Panel old && old != (page == Page.Transfers ? transferQueueHost : deviceQueueHost)) old.Children.Remove(queue);
            var holder = page == Page.Transfers ? transferQueueHost : deviceQueueHost; if (queue.Parent is null) holder.Children.Add(queue);
            service.Text = c.Starting ? c.T("AirLink wird gestartet …", "Starting AirLink …") : !c.Ready ? c.T("AirLink ist nicht bereit", "AirLink is unavailable") : "";
            service.Visibility = string.IsNullOrEmpty(service.Text) ? Visibility.Collapsed : Visibility.Visible;
            error.Text = c.Error ?? ""; error.Visibility = string.IsNullOrEmpty(c.Error) ? Visibility.Collapsed : Visibility.Visible; dismiss.Visibility = c.HasActionError ? Visibility.Visible : Visibility.Collapsed;
            notice.Text = c.Notice ?? ""; notice.Visibility = string.IsNullOrEmpty(c.Notice) || page != Page.Devices ? Visibility.Collapsed : Visibility.Visible;
            var waiting = (c.Status?.PairRequests.Count ?? 0) + (c.Status?.Transfers.Count(t => t.Direction == "receive" && t.State is "offered" or "waitingDuplicate") ?? 0);
            attention.Visibility = page != Page.Devices && waiting > 0 ? Visibility.Visible : Visibility.Collapsed; attention.Content = waiting + c.T(" Anfrage(n) anzeigen", " request(s) need attention"); retry.Visibility = recover.Visibility = !c.Ready && !c.Starting ? Visibility.Visible : Visibility.Collapsed;
            setup.Visibility = c.SetupRequired ? Visibility.Visible : Visibility.Collapsed;
            var peers = c.Status?.Devices ?? [];
            foreach (var gone in deviceItems.Keys.Where(id => peers.All(d => d.Id != id)).ToArray()) { devices.Items.Remove(deviceItems[gone]); deviceItems.Remove(gone); }
            var deviceIndex = 0;
            foreach (var d in peers.OrderByDescending(d => d.Paired).ThenByDescending(d => d.Online).ThenBy(d => d.Name))
            {
                if (!deviceItems.TryGetValue(d.Id, out var item))
                {
                    var tile = new DeviceTile(); item = new ListBoxItem { Tag = d.Id, Content = tile, AllowDrop = true }; deviceItems.Add(d.Id, item);
                    var id = d.Id;
                    item.DragOver += (_, e) => { e.Effects = e.Data.GetDataPresent(DataFormats.FileDrop) && c.Ready && c.SetupComplete && !c.Sending && TransferDraft.CanSendTo(c.Status?.Devices.FirstOrDefault(p => p.Id == id)) ? DragDropEffects.Copy : DragDropEffects.None; e.Handled = true; };
                    item.Drop += (_, e) => { if (e.Data.GetData(DataFormats.FileDrop) is string[] paths && TransferDraft.CanSendTo(c.Status?.Devices.FirstOrDefault(p => p.Id == id))) { Navigate(Page.Devices); _ = c.RunAsync(() => c.SendPathsAsync(paths, id)); } e.Handled = true; };
                    item.MouseDoubleClick += (_, e) => { c.SelectedDeviceId = id; Navigate(Page.Devices); if (TransferDraft.CanSendTo(c.SelectedDevice)) PickFiles(); else if (c.SelectedDevice is { Paired: true } paired) Dialogs.Device(this, c, paired); else PairDevice(c.SelectedDevice); e.Handled = true; };
                }
                ((DeviceTile)item.Content).Update(d, c); item.ToolTip = d.Name;
                if (!devices.Items.Contains(item)) devices.Items.Insert(deviceIndex, item); else if (devices.Items.IndexOf(item) != deviceIndex) { devices.Items.Remove(item); devices.Items.Insert(deviceIndex, item); }
                item.IsSelected = page == Page.Devices && d.Id == c.SelectedDeviceId; deviceIndex++;
            }
            var selected = c.SelectedDevice; var canSend = TransferDraft.CanSendTo(selected);
            hero.Visibility = selected is null ? Visibility.Collapsed : Visibility.Visible; welcome.Visibility = selected is null || !selected.Paired ? Visibility.Visible : Visibility.Collapsed;
            target.Text = selected?.Name ?? ""; targetDetail.Text = selected is null ? "" : !selected.Paired ? c.T("Noch nicht verbunden", "Not paired yet") : selected.Kind == "browser" ? c.T("Im Browser verbunden · per Link teilen", "Connected through browser · share by link") : !canSend ? c.T("Empfang nicht freigegeben", "Receiving is not allowed") : selected.Online ? c.T("Verbunden · bereit zum Senden", "Connected · ready to send") : c.T("Offline · Sendungen warten auf die Verbindung", "Offline · transfers wait for a connection");
            manage.Visibility = selected is { Paired: true } ? Visibility.Visible : Visibility.Collapsed; manage.IsEnabled = c.Ready && c.SetupComplete; connect.IsEnabled = c.Ready && c.SetupComplete;
            trustedCard.Visibility = selected is { Paired: true } ? Visibility.Visible : Visibility.Collapsed;
            automaticReceive.IsChecked = savingTrust && selected?.Id == savingTrustDevice ? savingTrustValue : selected?.AutoAccept == true;
            automaticReceive.IsEnabled = c.Ready && !savingTrust && selected is { Paired: true, CanSend: true };
            browserShare.Visibility = c.SetupComplete && selected is { Paired: true, Kind: "browser" } ? Visibility.Visible : Visibility.Collapsed;
            dropArea.Visibility = c.SetupComplete && (selected is null || selected is { Paired: true, Kind: "engine" }) ? Visibility.Visible : Visibility.Collapsed;
            dropTitle.Text = canSend ? c.T("An ", "Send to ") + selected!.Name + c.T(" senden", "") : c.T("Dateien zum Senden ablegen", "Drop files to send");
            pickFiles.IsEnabled = pickFolder.IsEnabled = c.Ready && c.SetupComplete && !c.Sending;
            receivePath.Text = c.T("Empfang: ", "Received files: ") + (c.Status?.Settings.ReceiveDirectory ?? ""); receivePath.ToolTip = c.Status?.Settings.ReceiveDirectory;
            send.IsEnabled = c.Ready && c.SetupComplete && !c.Sending && c.Selection.Count > 0 && canSend; send.Content = c.Sending ? c.T("Wird vorbereitet …", "Preparing …") : canSend ? c.T("An ", "Send to ") + selected!.Name + c.T(" senden", "") : c.T("Senden", "Send");
            selectionTitle.Visibility = selection.Visibility = send.Visibility = clear.Visibility = c.Selection.Count > 0 ? Visibility.Visible : Visibility.Collapsed;
            var selectionKey = string.Join('|', c.Selection.Select(i => i.Id));
            if (selectionKey != selectedKey)
            {
                selectedKey = selectionKey; selection.Items.Clear();
                foreach (var entry in c.Selection)
                {
                    var line = new DockPanel(); var remove = Theme.Button("×", () => c.Selection.Remove(entry)); remove.ToolTip = c.T("Entfernen", "Remove"); remove.Margin = new Thickness(8, 0, 0, 0); DockPanel.SetDock(remove, Dock.Right); line.Children.Add(remove);
                    var text = Theme.Text(entry.Title, 13); text.ToolTip = entry.Path; line.Children.Add(text); selection.Items.Add(line);
                }
            }
            var pending = c.Status?.PairRequests ?? [];
            foreach (var gone in pairRows.Keys.Where(id => pending.All(p => p.Id != id)).ToArray()) { requests.Children.Remove(pairRows[gone]); pairRows.Remove(gone); }
            foreach (var request in pending.Where(p => !pairRows.ContainsKey(p.Id)))
            {
                var body = new StackPanel(); body.Children.Add(Theme.Text(request.Name + c.T(" möchte sich verbinden", " wants to connect"), 16, true));
                body.Children.Add(Theme.Text(c.T("Bestätige nur eine Anfrage, die du selbst auf deinem anderen Gerät gestartet hast.", "Approve only a request you started on your other device."), 12, secondary: true));
                var trust = new CheckBox { Content = c.T("Von diesem Gerät künftig automatisch empfangen", "Automatically receive from this device in future") }; body.Children.Add(trust);
                body.Children.Add(Wrap(Action(c.T("Verbinden", "Connect"), () => c.CommandAsync(trust.IsChecked == true ? "pair.approveTrusted" : "pair.approve", request.Id), true), Action(c.T("Ablehnen", "Decline"), () => c.CommandAsync("pair.deny", request.Id))));
                var card = Theme.Card(body); card.Margin = new Thickness(0, 0, 0, 16); pairRows.Add(request.Id, card); requests.Children.Add(card);
            }
            var all = c.Status?.Transfers ?? []; var terminal = NativeExperience.RecentTransfers(all, page == Page.Transfers ? 50 : 3); var jobs = all.Where(t => t.IsActive).OrderByDescending(t => t.CreatedAt).Concat(terminal).ToArray();
            empty.Visibility = jobs.Any(t => t.IsActive) ? Visibility.Collapsed : Visibility.Visible; recentTitle.Visibility = terminal.Count == 0 ? Visibility.Collapsed : Visibility.Visible;
            cleanHistory.Visibility = all.Any(NativeExperience.CanRemoveFromHistory) ? Visibility.Visible : Visibility.Collapsed;
            cleanHistory.IsEnabled = c.Ready && !c.RemovingHistory;
            recentTitle.Text = c.T("Zuletzt", "Recent") + (page == Page.Transfers && all.Count(t => !t.IsActive) > terminal.Count ? " · " + terminal.Count + c.T(" von ", " of ") + all.Count(t => !t.IsActive) : "");
            foreach (var gone in transferRows.Keys.Where(id => jobs.All(t => t.Id != id)).ToArray()) { transfers.Children.Remove(transferRows[gone]); recent.Children.Remove(transferRows[gone]); transferRows.Remove(gone); }
            int activeAt = 0, recentAt = 0;
            foreach (var job in jobs)
            {
                if (!transferRows.TryGetValue(job.Id, out var row)) { row = new TransferRow(this, c); transferRows.Add(job.Id, row); }
                var parent = job.IsActive ? transfers : recent; var index = job.IsActive ? activeAt++ : recentAt++;
                if (row.Parent != parent) { (row.Parent as Panel)?.Children.Remove(row); parent.Children.Insert(index, row); } else if (parent.Children.IndexOf(row) != index) { parent.Children.Remove(row); parent.Children.Insert(index, row); }
                row.Update(job);
            }
        }
        finally { rendering = false; }
    }
}

internal sealed class DeviceTile : Grid
{
    private readonly TextBlock name = Theme.Text("", 14, true), status = Theme.Text("", 12, secondary: true);
    public DeviceTile()
    {
        ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(42) }); ColumnDefinitions.Add(new ColumnDefinition());
        var icon = Theme.Icon("computer", 25); icon.SetValue(VerticalAlignmentProperty, VerticalAlignment.Center); Children.Add(icon);
        var text = new StackPanel { MinWidth = 0 }; name.MaxHeight = 42; name.TextTrimming = TextTrimming.CharacterEllipsis; text.Children.Add(name); text.Children.Add(status); SetColumn(text, 1); Children.Add(text);
    }
    public void Update(Device device, Controller c)
    {
        name.Text = device.Name; name.ToolTip = device.Name;
        status.Text = device.Paired ? device.Online ? c.T("Vertraut · verbunden", "Trusted · connected") : c.T("Vertraut · offline", "Trusted · offline") : c.T("In der Nähe · verbinden", "Nearby · connect");
        status.Foreground = device.Paired && device.Online ? Theme.Accent : Theme.Secondary;
    }
}
