using System;
using System.Globalization;
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
        var form = new Form(owner, c.T("Gerät verbinden", "Connect a device"));
        form.Help(c.T("Vergleiche den Fingerabdruck mit der Anzeige in AirLink auf dem anderen Gerät. Bestätige anschließend die Anfrage dort.",
            "Compare this fingerprint with AirLink on the other device. Then approve the request there."));
        var address = form.Input(c.T("HTTPS-Adresse", "HTTPS address"), seed?.Address ?? "https://");
        var fingerprint = form.Input(c.T("Fingerabdruck (SHA-256)", "Fingerprint (SHA-256)"), seed?.Fingerprint ?? "");
        fingerprint.FontFamily = new System.Windows.Media.FontFamily("Consolas");
        var verified = form.Check(c.T("Ich habe den Fingerabdruck am anderen Gerät verglichen.", "I compared the fingerprint on the other device."));
        var automatic = form.Check(c.T("Dateien von diesem Gerät künftig automatisch annehmen", "Automatically accept future files from this device"));
        form.Help(c.T("Diese Freigabe gilt für den Empfang auf diesem PC. Die Empfangsrechte des anderen Geräts werden dort festgelegt.",
            "This permission applies to receiving on this PC. Receiving permissions on the other device are set there."));
        form.Commit(c.T("Verbindung anfragen", "Request connection"), async () =>
        {
            if (verified.IsChecked != true)
                throw new InvalidOperationException(c.T("Bitte zuerst den Fingerabdruck am anderen Gerät vergleichen.", "First compare the fingerprint on the other device."));
            await c.ConnectAsync(address.Text, fingerprint.Text, automatic.IsChecked == true);
        });
        form.Show();
    }

    public static void Settings(Window owner, Controller c)
    {
        if (c.Status is null) return;
        var settings = ProtocolJson.Clone(c.Status.Settings);
        var form = new Form(owner, c.T("Einstellungen", "Settings"), 600);
        var name = form.Input(c.T("Name dieses PCs", "This PC's name"), settings.Name);
        var directory = form.Input(c.T("Empfangsordner", "Receive folder"), settings.ReceiveDirectory);
        var choose = Theme.Button(c.T("Ordner wählen …", "Choose folder …"), () =>
        {
            var picker = new OpenFolderDialog { Title = c.T("Empfangsordner wählen", "Choose receive folder"), Multiselect = false };
            if (System.IO.Directory.Exists(directory.Text)) picker.InitialDirectory = directory.Text;
            if (picker.ShowDialog(form.Window) == true) directory.Text = picker.FolderName;
        });
        form.Add(choose);
        var appearance = form.Options(c.T("Darstellung", "Appearance"),
            [c.T("System", "System"), c.T("Hell", "Light"), c.T("Dunkel", "Dark")],
            Array.IndexOf(new[] { "system", "light", "dark" }, settings.Appearance));
        var language = form.Options(c.T("Sprache", "Language"),
            [c.T("System", "System"), "Deutsch", "English"],
            Array.IndexOf(new[] { "system", "de", "en" }, settings.Language));
        var notifications = form.Check(c.T("Bei empfangenen Dateien benachrichtigen", "Notify when files are received"), settings.Notifications);
        var duplicates = form.Check(c.T("Empfangene Dateien auf identische Inhalte prüfen", "Check received files for identical content"), settings.Organization.DetectDuplicates);
        var sendLimit = form.Input(c.T("Sendelimit in MB/s (0 = unbegrenzt)", "Upload limit in MB/s (0 = unlimited)"), Limit(settings.SendLimitBytesPerSecond));
        var receiveLimit = form.Input(c.T("Empfangslimit in MB/s (0 = unbegrenzt)", "Download limit in MB/s (0 = unlimited)"), Limit(settings.ReceiveLimitBytesPerSecond));
        if (settings.Organization.SortEnabled)
            form.Help(c.T("Deine bestehenden Sortierregeln sind aktiv und bleiben erhalten. Der Regeleditor ist derzeit in der Weboberfläche verfügbar.",
                "Your existing sorting rules are active and will be preserved. The rule editor is currently available in the web interface."));
        form.Commit(c.T("Speichern", "Save"), async () =>
        {
            if (string.IsNullOrWhiteSpace(name.Text)) throw new ArgumentException(c.T("Bitte einen Gerätenamen eingeben.", "Enter a device name."));
            if (!System.IO.Path.IsPathFullyQualified(directory.Text.Trim()))
                throw new ArgumentException(c.T("Bitte einen vollständigen Empfangspfad wählen.", "Choose an absolute receive folder path."));
            settings.Name = name.Text.Trim(); settings.ReceiveDirectory = directory.Text.Trim();
            settings.Appearance = new[] { "system", "light", "dark" }[Math.Max(0, appearance.SelectedIndex)];
            settings.Language = new[] { "system", "de", "en" }[Math.Max(0, language.SelectedIndex)];
            settings.Notifications = notifications.IsChecked == true;
            settings.Organization.DetectDuplicates = duplicates.IsChecked == true;
            settings.SendLimitBytesPerSecond = ParseLimit(sendLimit.Text, c);
            settings.ReceiveLimitBytesPerSecond = ParseLimit(receiveLimit.Text, c);
            await c.SaveSettingsAsync(settings);
        });
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
        var clipboard = form.Check(c.T("Darf Zwischenablagentext austauschen", "May exchange clipboard text"), copy.CanClipboard);
        var relay = form.Check(c.T("Darf Browserübertragungen vermitteln", "May take part in browser relay transfers"), copy.CanRelay);
        if (!string.IsNullOrEmpty(copy.Fingerprint))
            form.ReadOnly(c.T("Gespeicherter Fingerabdruck", "Saved fingerprint"), copy.Fingerprint);
        form.Commit(c.T("Speichern", "Save"), async () =>
        {
            copy.CanSend = send.IsChecked == true; copy.CanReceive = receive.IsChecked == true;
            copy.AutoAccept = automatic.IsChecked == true;
            copy.CanClipboard = clipboard.IsChecked == true; copy.CanRelay = relay.IsChecked == true;
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
            return current?.State == "waitingDuplicate" && current.Files.Any(f => f.Id == file.Id && f.DuplicateState == "pending");
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
        var form = new Form(owner, c.T("Verbindungsdaten", "Connection details"), 650);
        form.Help(c.T("Desktopgeräte verbinden sich über die Geräteauswahl. Für iPhone, iPad und andere Browsergeräte öffne eine Einrichtungsadresse im selben lokalen Netzwerk.",
            "Desktop devices connect from the device list. For iPhone, iPad and other browser devices, open a setup address on the same local network."));
        foreach (var url in c.Status.SetupUrls)
            CopyField(form, c, c.T("Browser einrichten", "Set up browser"), url);
        foreach (var url in c.Status.Urls)
            CopyField(form, c, c.T("HTTPS-Adresse", "HTTPS address"), url);
        CopyField(form, c, c.T("Fingerabdruck dieses PCs", "This PC's fingerprint"), c.Status.Fingerprint);
        form.Help(c.T("Vergleiche den Fingerabdruck bei der ersten Verbindung. Die Einrichtungsseite erklärt die nötige Zertifikatsfreigabe für den Browser.",
            "Compare the fingerprint on first connection. The setup page explains the certificate trust steps for the browser."));
        form.Show();
    }

    private static void CopyField(Form form, Controller c, string label, string value)
    {
        form.ReadOnly(label, value);
        form.Action(c.T("Kopieren", "Copy"), () => { Clipboard.SetText(value); return Task.FromResult(false); });
    }

    private static string Limit(long bytes) => (bytes / 1_000_000m).ToString("0.######", CultureInfo.CurrentCulture);
    private static long ParseLimit(string input, Controller c)
    {
        if (!decimal.TryParse(input.Trim(), NumberStyles.Number, CultureInfo.CurrentCulture, out var amount) || amount < 0 || amount > long.MaxValue / 1_000_000m)
            throw new ArgumentException(c.T("Geschwindigkeitslimits müssen eine positive Zahl oder 0 sein.", "Speed limits must be a positive number or 0."));
        return checked((long)(amount * 1_000_000m));
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
                Owner = owner, Title = title, Width = width, MaxHeight = 830, MinHeight = 250,
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
