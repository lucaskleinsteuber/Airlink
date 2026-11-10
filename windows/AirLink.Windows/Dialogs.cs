using System;
using System.Linq;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using AirLink.Core;
using Microsoft.Win32;
using RemoteDevice = AirLink.Core.Device;

namespace AirLink.Windows;

internal static class Dialogs
{
    public static void Pair(Window owner, Controller c, RemoteDevice? seed)
    {
        new PairingWindow(owner, c, seed).ShowDialog();
    }

    public static void TransferDetails(Window owner, Controller c, Transfer transfer)
    {
        var form = new Form(owner, transfer.Title, 650);
        form.Help(c.StateText(transfer.State));
        if (transfer.Problem is { } advice) {
            form.Help(c.T(advice.TitleDE, advice.TitleEN));
            form.Help(c.T(advice.RecoveryDE, advice.RecoveryEN));
        }
        if (!string.IsNullOrWhiteSpace(transfer.Error)) form.Add(new Expander {
            Header = c.T("Technische Details", "Technical details"),
            Content = new TextBox { Text = transfer.Error, IsReadOnly = true, TextWrapping = TextWrapping.Wrap, BorderThickness = new Thickness(0), Margin = new Thickness(0, 8, 0, 0) },
            Margin = new Thickness(0, 8, 0, 12)
        });
        var files = new StackPanel(); form.Add(files);
        var offset = 0; var loading = false;
        async Task Load() {
            if (loading || offset < 0) return; loading = true;
            try {
                var page = await c.RequestAsync<TransferFilePage>("transfer.files", new { id = transfer.Id, offset, limit = 50 });
                foreach (var file in page.Files) {
                    files.Children.Add(Theme.Text(file.Path, 13, true));
                    files.Children.Add(Theme.Text(c.Bytes(file.Size), 12, secondary: true));
                    if (file.DuplicateState == "pending") files.Children.Add(Theme.Button(c.T("Duplikat prüfen", "Review duplicate"), () => Duplicate(form.Window, c, transfer, file)));
                }
                offset = page.NextOffset;
            } finally { loading = false; }
        }
        form.Action(c.T("Weitere Dateien laden", "Load more files"), async () => { await Load(); return false; });
        form.Window.Loaded += async (_, _) => await form.Execute(async () => { await Load(); return false; });
        form.Show();
    }

    public static void Device(Window owner, Controller c, RemoteDevice device)
    {
        var copy = ProtocolJson.Clone(device);
        var form = new Form(owner, device.Name);
        form.Help(c.T("Lege fest, was dieses Gerät über AirLink darf. Automatische Annahme gilt nur für Dateien, die auf diesem PC eingehen.",
            "Choose what this device may do through AirLink. Automatic acceptance applies only to files received on this PC."));
        var send = form.Check(c.T("Darf Dateien an diesen PC senden", "May send files to this PC"), copy.CanSend);
        var receive = form.Check(c.T("Darf Dateien von diesem PC empfangen", "May receive files from this PC"), copy.CanReceive);
        var automatic = form.Check(c.T("Eingehende Dateien automatisch annehmen", "Automatically accept incoming files"), copy.AutoAccept);
        automatic.IsEnabled = send.IsChecked == true;
        send.Checked += (_, _) => automatic.IsEnabled = true;
        send.Unchecked += (_, _) => { automatic.IsChecked = false; automatic.IsEnabled = false; };
        var clipboard = form.Check(c.T("Darf Zwischenablagentext austauschen", "May exchange clipboard text"), copy.CanClipboard);
        var relay = copy.Kind == "browser" ? form.Check(c.T("Darf Browserübertragungen vermitteln", "May take part in browser relay transfers"), copy.CanRelay) : null;
        if (!string.IsNullOrEmpty(copy.Fingerprint))
            form.ReadOnly(c.T("Gespeicherter Fingerabdruck", "Saved fingerprint"), copy.Fingerprint);
        form.Commit(c.T("Speichern", "Save"), async () =>
        {
            copy.CanSend = send.IsChecked == true; copy.CanReceive = receive.IsChecked == true;
            copy.AutoAccept = copy.CanSend && automatic.IsChecked == true;
            copy.CanClipboard = clipboard.IsChecked == true; if (relay is not null) copy.CanRelay = relay.IsChecked == true;
            await c.SaveDeviceAsync(copy);
        });
        form.Action(c.T("Kopplung widerrufen", "Revoke pairing"), async () =>
        {
            if (MessageBox.Show(form.Window,
                c.T("Das Gerät muss danach erneut gekoppelt werden. Aktive Übertragungen können unterbrochen werden.", "The device will need to pair again. Active transfers may be interrupted."),
                c.T("Kopplung widerrufen?", "Revoke pairing?"), MessageBoxButton.YesNo, MessageBoxImage.Question) != MessageBoxResult.Yes) return false;
            await c.CommandAsync("device.revoke", copy.Id);
            return true;
        });
        form.Show();
    }

    public static void Duplicate(Window owner, Controller c, Transfer transfer, TransferFile file)
    {
        var form = new Form(owner, c.T("Identische Datei gefunden", "Identical file found"), 620);
        form.Help(file.Path);
        form.Help(c.T("Eine Datei mit demselben Inhalt ist bereits vorhanden. Behalte eine zusätzliche Kopie oder überspringe diese Datei. Vorhandene Dateien werden nicht gelöscht.",
            "A file with the same content already exists. Keep an additional copy or skip this file. Existing files will be preserved."));
        bool Pending()
        {
            var current = c.Status?.Transfers.FirstOrDefault(t => t.Id == transfer.Id);
            return current?.State == "waitingDuplicate";
        }
        async Task Resolve(string action, string? candidate)
        {
            if (!Pending()) throw new InvalidOperationException(c.T("Diese Entscheidung ist nicht mehr offen. Schließe das Fenster und prüfe die Übertragung.",
                "This decision is no longer pending. Close this window and check the transfer."));
            await c.ResolveAsync(transfer, file, action, candidate);
        }
        form.Commit(c.T("Zusätzliche Kopie behalten", "Keep an additional copy"), () => Resolve("keep", null));
        foreach (var candidate in file.DuplicateCandidates)
        {
            var row = new StackPanel { Margin = new Thickness(0, 10, 0, 2) };
            row.Children.Add(Theme.Text(c.T("Vorhandene Datei", "Existing file") + " · " + c.Bytes(candidate.Size), bold: true));
            if (!string.IsNullOrWhiteSpace(candidate.Path)) row.Children.Add(Theme.Text(candidate.Path, 12, secondary: true));
            var actions = new WrapPanel { Margin = new Thickness(0, 8, 0, 0) };
            var reveal = Theme.Button(c.T("Im Explorer zeigen", "Show in Explorer"), () => { });
            reveal.Click += async (_, _) => await form.Execute(async () => { await c.RevealCandidateAsync(candidate.Id); return false; });
            var skip = Theme.Button(c.T("Empfangene Datei überspringen", "Skip incoming file"), () => { });
            skip.Click += async (_, _) => await form.Execute(async () => { await Resolve("skip", candidate.Id); return true; });
            actions.Children.Add(reveal); actions.Children.Add(skip); row.Children.Add(actions); form.Add(Theme.Card(row));
        }
        form.Show();
    }

    public static void ConnectionInfo(Window owner, Controller c)
    {
        if (c.Status is null) return;
        var form = new Form(owner, c.T("Dieses Gerät", "This device"), 650);
        form.Help(c.Status.Name);
        form.Help(c.T("Vergleiche diese Angaben beim Verbinden eines Computers. Über „Gerät hinzufügen“ wählst du den anderen Computer im Netzwerk aus.",
            "Compare these details when connecting a computer. Use Add device to choose the other computer on your network."));
        foreach (var url in c.Status.Urls)
            CopyField(form, c, c.T("HTTPS-Adresse", "HTTPS address"), url);
        CopyField(form, c, c.T("Fingerabdruck dieses PCs", "This PC's fingerprint"), new ConnectionDraft("", c.Status.Fingerprint).ComparisonFingerprint);
        var browser = new StackPanel();
        browser.Children.Add(Theme.Text(c.T("iPhone, iPad oder Android", "iPhone, iPad or Android"), 15, true));
        browser.Children.Add(Theme.Text(c.T("Öffne auf dem Handy oder Tablet eine dieser Adressen im selben Netzwerk. Die Seite führt durch die Zertifikatsfreigabe. Vergleiche dabei den Fingerabdruck mit diesem PC.",
            "Open one of these addresses on your phone or tablet on the same network. The page guides you through certificate trust. Compare the fingerprint with this PC."), 13, secondary: true));
        foreach (var url in c.Status.SetupUrls)
        {
            var value = url;
            browser.Children.Add(new TextBox { Text = value, IsReadOnly = true, TextWrapping = TextWrapping.Wrap, Margin = new Thickness(0, 12, 0, 6) });
            browser.Children.Add(Theme.Button(c.T("Einrichtungsadresse kopieren", "Copy setup address"), () => Clipboard.SetText(value)));
        }
        form.Add(new Expander { Header = c.T("Handy oder Tablet verbinden", "Connect phone or tablet"), Content = browser, Foreground = Theme.Foreground, Margin = new Thickness(0, 22, 0, 0) });
        form.Show();
    }

    private static void CopyField(Form form, Controller c, string label, string value)
    {
        form.ReadOnly(label, value);
        form.Action(c.T("Kopieren", "Copy"), () => { Clipboard.SetText(value); return Task.FromResult(false); });
    }

    private sealed class Form
    {
        public Window Window { get; }
        private readonly StackPanel body = new();
        private readonly WrapPanel actions = new() { Margin = new Thickness(0, 16, 0, 0) };
        private readonly TextBlock error = Theme.Text("", 13);
        private bool busy;

        public Form(Window owner, string title, double width = 560)
        {
            Window = new Window
            {
                Owner = owner, Title = title, Width = Math.Min(width, SystemParameters.WorkArea.Width - 32), MaxHeight = Math.Max(320, Math.Min(830, SystemParameters.WorkArea.Height - 40)), MinHeight = 250,
                SizeToContent = SizeToContent.Height, WindowStartupLocation = WindowStartupLocation.CenterOwner,
                Background = Theme.Background, Foreground = Theme.Foreground, ShowInTaskbar = false,
                FontFamily = new System.Windows.Media.FontFamily("Segoe UI"), FontSize = 14
            };
            var root = new StackPanel { Margin = new Thickness(24) };
            root.Children.Add(Theme.Text(title, 22, true));
            body.Margin = new Thickness(0, 12, 0, 0); root.Children.Add(body);
            error.Foreground = Theme.Color(Theme.Dark ? "#FFB2AA" : "#B3261E");
            error.Margin = new Thickness(0, 14, 0, 0); error.Visibility = Visibility.Collapsed;
            root.Children.Add(error); root.Children.Add(actions);
            var close = Theme.Button("OK", () => Window.Close());
            close.IsCancel = true; actions.Children.Add(close);
            Window.Content = new ScrollViewer { Content = root, VerticalScrollBarVisibility = ScrollBarVisibility.Auto, HorizontalScrollBarVisibility = ScrollBarVisibility.Disabled };
            Window.Closing += (_, e) => { if (busy) e.Cancel = true; };
        }

        public void Add(UIElement item) => body.Children.Add(item);
        public void Help(string text)
        {
            var view = Theme.Text(text, 13, secondary: true); view.Margin = new Thickness(0, 4, 0, 10); Add(view);
        }
        public TextBox Input(string label, string text)
        {
            var caption = Theme.Text(label, 13, true); caption.Margin = new Thickness(0, 12, 0, 5); Add(caption);
            var field = new TextBox { Text = text, MinWidth = 180 }; Add(field); return field;
        }
        public void ReadOnly(string label, string value)
        {
            var field = Input(label, value); field.IsReadOnly = true; field.TextWrapping = TextWrapping.Wrap;
        }
        public CheckBox Check(string title, bool value = false)
        {
            var box = new CheckBox { Content = Theme.Text(title, 13), IsChecked = value }; Add(box); return box;
        }
        public ComboBox Options(string label, string[] choices, int selected)
        {
            var caption = Theme.Text(label, 13, true); caption.Margin = new Thickness(0, 12, 0, 5); Add(caption);
            var combo = new ComboBox { ItemsSource = choices, SelectedIndex = Math.Max(0, selected) }; Add(combo); return combo;
        }
        public void Commit(string label, Func<Task> action)
        {
            var button = Theme.Button(label, () => { }, true);
            button.Click += async (_, _) => await Execute(async () => { await action(); return true; });
            button.IsDefault = true; actions.Children.Insert(0, button);
        }
        public void Action(string label, Func<Task<bool>> action)
        {
            var button = Theme.Button(label, () => { });
            button.Click += async (_, _) => await Execute(action); actions.Children.Insert(Math.Max(0, actions.Children.Count - 1), button);
        }
        public async Task Execute(Func<Task<bool>> action)
        {
            if (busy) return;
            busy = true; body.IsEnabled = false; actions.IsEnabled = false;
            error.Text = ""; error.Visibility = Visibility.Collapsed;
            bool close = false;
            try { close = await action(); }
            catch (Exception e) { error.Text = e.Message; error.Visibility = Visibility.Visible; }
            finally { busy = false; body.IsEnabled = true; actions.IsEnabled = true; }
            if (close) Window.Close();
        }
        public void Show() => Window.ShowDialog();
    }
}
