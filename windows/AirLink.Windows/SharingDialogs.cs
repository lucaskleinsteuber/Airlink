using System;
using System.Collections.Generic;
using System.ComponentModel;
using System.Globalization;
using System.IO;
using System.Linq;
using System.Text;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using AirLink.Core;
using Microsoft.Win32;
using SharedItem = AirLink.Core.Share;

namespace AirLink.Windows;

internal static class SharingDialogs
{
    public static void Share(Window owner, Controller c, IReadOnlyList<string>? paths = null, Transfer? transfer = null)
    {
        var form = new Panel(owner, "Quick Share");
        form.Help(c.T("Erstelle einen Link für Dateien im lokalen Netzwerk. Die Originale bleiben an ihrem Platz. Empfänger benötigen das Zertifikat dieses PCs.",
            "Create a link to files on your local network. Originals stay in place. Recipients need to trust this PC’s certificate."));
        var chosen = paths?.Distinct(StringComparer.OrdinalIgnoreCase).ToList() ?? [];
        var selectedTransfer = transfer;
        var creation = new StackPanel(); form.Add(creation);
        var source = Theme.Text("", 13, secondary: true); creation.Children.Add(source);
        void ShowSource() => source.Text = selectedTransfer is not null
            ? c.T("Aus Übertragung: ", "From transfer: ") + selectedTransfer.Title
            : chosen.Count == 0 ? c.T("Noch keine Dateien ausgewählt", "No files selected yet") : string.Join(", ", chosen.Take(3).Select(p => Path.GetFileName(p.TrimEnd(Path.DirectorySeparatorChar)))) + (chosen.Count > 3 ? " + " + (chosen.Count - 3) : "");
        ShowSource();
        var pickers = new WrapPanel { Margin = new Thickness(0, 10, 0, 4) };
        pickers.Children.Add(Theme.Button(c.T("Dateien wählen …", "Choose files …"), () =>
        {
            var picker = new OpenFileDialog { Multiselect = true, CheckFileExists = true, Title = c.T("Dateien freigeben", "Share files") };
            if (picker.ShowDialog(form.Window) == true) { selectedTransfer = null; chosen = chosen.Concat(picker.FileNames).Distinct(StringComparer.OrdinalIgnoreCase).ToList(); ShowSource(); }
        }));
        pickers.Children.Add(Theme.Button(c.T("Ordner wählen …", "Choose folders …"), () =>
        {
            var picker = new OpenFolderDialog { Multiselect = true, Title = c.T("Ordner freigeben", "Share folders") };
            if (picker.ShowDialog(form.Window) == true) { selectedTransfer = null; chosen = chosen.Concat(picker.FolderNames).Distinct(StringComparer.OrdinalIgnoreCase).ToList(); ShowSource(); }
        }));
        pickers.Children.Add(Theme.Button(c.T("Auswahl leeren", "Clear selection"), () => { selectedTransfer = null; chosen.Clear(); ShowSource(); }));
        creation.Children.Add(pickers);
        var name = Input(creation, c.T("Name (optional)", "Name (optional)"));
        Caption(creation, c.T("Passwort (optional)", "Password (optional)"));
        var password = new PasswordBox { Padding = new Thickness(8), MaxLength = 1024, Background = Theme.Surface, Foreground = Theme.Foreground, BorderBrush = Theme.Line };
        creation.Children.Add(password);
        Caption(creation, c.T("Gültigkeit", "Expires"));
        var expires = new ComboBox { ItemsSource = new[] { c.T("Nach 24 Stunden", "After 24 hours"), c.T("Nach einer Stunde", "After one hour"), c.T("Nach sieben Tagen", "After seven days"), c.T("Ohne Ablaufdatum", "Never"), c.T("Datum und Uhrzeit …", "Date and time …") }, SelectedIndex = 0 };
        creation.Children.Add(expires);
        var date = new DatePicker { SelectedDate = DateTime.Today.AddDays(1), Margin = new Thickness(0, 8, 8, 0), Width = 170 };
        var time = new TextBox { Text = DateTime.Now.ToString("HH:mm", CultureInfo.InvariantCulture), Width = 85, Margin = new Thickness(0, 8, 0, 0) };
        time.ToolTip = c.T("Uhrzeit HH:mm", "Time HH:mm");
        var custom = new WrapPanel { Visibility = Visibility.Collapsed }; custom.Children.Add(date); custom.Children.Add(time); creation.Children.Add(custom);
        expires.SelectionChanged += (_, _) => custom.Visibility = expires.SelectedIndex == 4 ? Visibility.Visible : Visibility.Collapsed;
        var limit = Input(creation, c.T("Downloadlimit (0 = unbegrenzt)", "Download limit (0 = unlimited)"), "0");
        var create = Theme.Button(c.T("Link erstellen", "Create link"), () => { }, true);
        create.Margin = new Thickness(0, 15, 0, 0); creation.Children.Add(create);
        var created = new StackPanel { Visibility = Visibility.Collapsed, Margin = new Thickness(0, 12, 0, 0) }; form.Add(created);
        var listHeading = Theme.Text(c.T("Deine Freigaben", "Your shares"), 18, true); listHeading.Margin = new Thickness(0, 22, 0, 10); form.Add(listHeading);
        var shares = new StackPanel(); form.Add(shares);
        string listKey = "";
        void Render()
        {
            create.IsEnabled = c.Ready && !form.Busy;
            var current = c.Status?.Shares.OrderByDescending(s => s.CreatedAt).ToArray() ?? [];
            var key = c.Ready + ":" + string.Join('|', current.Select(s => s.Id + ":" + s.Revoked + ":" + s.Downloads + ":" + s.Url));
            if (listKey == key && shares.Children.Count > 0) return;
            listKey = key; shares.Children.Clear();
            if (current.Length == 0) shares.Children.Add(Theme.Text(c.T("Noch keine Freigaben", "No shares yet"), 13, secondary: true));
            foreach (var share in current)
            {
                var body = new StackPanel();
                body.Children.Add(Theme.Text(share.Name, 15, true));
                body.Children.Add(Theme.Text(c.Bytes(share.TotalBytes) + " · " + share.Downloads + (share.MaxDownloads > 0 ? " / " + share.MaxDownloads : "") + c.T(" Downloads", " downloads") + (share.PasswordProtected ? c.T(" · Passwortgeschützt", " · Password protected") : ""), 12, secondary: true));
                if (DateTimeOffset.TryParse(share.ExpiresAt, CultureInfo.InvariantCulture, DateTimeStyles.None, out var expiry))
                    body.Children.Add(Theme.Text(c.T("Gültig bis: ", "Expires: ") + expiry.ToLocalTime().ToString("g", CultureInfo.CurrentCulture), 12, secondary: true));
                if (share.Revoked) body.Children.Add(Theme.Text(c.T("Widerrufen", "Revoked"), 12, secondary: true));
                else
                {
                    var buttons = new WrapPanel { Margin = new Thickness(0, 10, 0, 0) };
                    buttons.Children.Add(Theme.Button(c.T("Link anzeigen", "Show link"), () => _ = form.Execute(() =>
                    {
                        ShowCreated(share); return Task.CompletedTask;
                    })));
                    var revoke = Theme.Button(c.T("Widerrufen …", "Revoke …"), () => _ = form.Execute(async () =>
                    {
                        if (MessageBox.Show(form.Window, c.T("Diese Freigabe widerrufen? Laufende Downloads werden beendet.", "Revoke this share? Active downloads will stop."), "Quick Share", MessageBoxButton.YesNo, MessageBoxImage.Question) != MessageBoxResult.Yes) return;
                        await c.RequestAsync("share.revoke", new { id = share.Id }); await c.RefreshAsync();
                    }));
                    revoke.IsEnabled = c.Ready; buttons.Children.Add(revoke);
                    body.Children.Add(buttons);
                }
                var card = Theme.Card(body); card.Margin = new Thickness(0, 0, 0, 10); shares.Children.Add(card);
            }
        }
        void ShowCreated(SharedItem share)
        {
            creation.Visibility = Visibility.Collapsed; created.Visibility = Visibility.Visible; created.Children.Clear();
            created.Children.Add(Theme.Text(share.Name, 18, true));
            var latest = c.Status?.Shares.FirstOrDefault(s => s.Id == share.Id) ?? share;
            if (latest.Revoked) created.Children.Add(Theme.Text(c.T("Diese Freigabe wurde widerrufen.", "This share was revoked."), 13, secondary: true));
            else if (ValidLink(latest.Url))
            {
                var link = new TextBox { Text = latest.Url, IsReadOnly = true, TextWrapping = TextWrapping.Wrap, Margin = new Thickness(0, 10, 0, 10) }; created.Children.Add(link);
                created.Children.Add(Theme.Button(c.T("Vollständigen Link kopieren", "Copy complete link"), () => _ = form.Execute(() =>
                {
                    var fresh = c.Status?.Shares.FirstOrDefault(s => s.Id == share.Id) ?? share;
                    if (fresh.Revoked || !ValidLink(fresh.Url)) throw new InvalidOperationException(c.T("Die Freigabe ist nicht mehr verfügbar.", "The share is no longer available."));
                    System.Windows.Clipboard.SetText(fresh.Url!); form.Notice(c.T("Link kopiert", "Link copied")); return Task.CompletedTask;
                }), true));
            }
            else created.Children.Add(Theme.Text(c.T("Der Link ist noch nicht verfügbar. Aktualisiere die Freigaben.", "The link is not available yet. Refresh your shares."), 13, secondary: true));
            var note = Theme.Text(c.T("Der vollständige Link enthält den Zugriffsschlüssel. Ein Passwort wird getrennt weitergegeben. Ablaufdatum, Widerruf und Downloadlimit werden vom bereitstellenden PC geprüft.",
                "The complete link includes the access key. Share any password separately. The hosting PC enforces expiry, revocation and the download limit."), 12, secondary: true);
            note.Margin = new Thickness(0, 12, 0, 12); created.Children.Add(note);
            created.Children.Add(Theme.Button(c.T("Neue Freigabe", "New share"), () => { created.Visibility = Visibility.Collapsed; creation.Visibility = Visibility.Visible; }));
        }
        create.Click += async (_, _) => await form.Execute(async () =>
        {
            if (selectedTransfer is null && chosen.Count == 0) throw new ArgumentException(c.T("Wähle zuerst Dateien oder Ordner aus.", "Choose files or folders first."));
            if (selectedTransfer is not null && c.Status?.Transfers.FirstOrDefault(t => t.Id == selectedTransfer.Id)?.State != "completed")
                throw new InvalidOperationException(c.T("Diese Übertragung ist nicht mehr vollständig verfügbar.", "This completed transfer is no longer available."));
            if (!long.TryParse(limit.Text.Trim(), NumberStyles.None, CultureInfo.InvariantCulture, out var maximum) || maximum < 0)
                throw new ArgumentException(c.T("Das Downloadlimit muss 0 oder eine positive ganze Zahl sein.", "The download limit must be 0 or a positive whole number."));
            if (UTF8Count(name.Text.Trim()) > 200) throw new ArgumentException(c.T("Der Name darf höchstens 200 UTF-8 Bytes enthalten.", "The name may contain at most 200 UTF-8 bytes."));
            if (UTF8Count(password.Password) > 1024) throw new ArgumentException(c.T("Das Passwort darf höchstens 1024 UTF-8 Bytes enthalten.", "The password may contain at most 1024 UTF-8 bytes."));
            DateTimeOffset? expiry = expires.SelectedIndex switch { 0 => DateTimeOffset.UtcNow.AddDays(1), 1 => DateTimeOffset.UtcNow.AddHours(1), 2 => DateTimeOffset.UtcNow.AddDays(7), _ => null };
            if (expires.SelectedIndex == 4)
            {
                if (date.SelectedDate is null || !TimeOnly.TryParseExact(time.Text.Trim(), "HH:mm", CultureInfo.InvariantCulture, DateTimeStyles.None, out var clock))
                    throw new ArgumentException(c.T("Bitte Datum und Uhrzeit im Format HH:mm eingeben.", "Enter a date and a time in HH:mm format."));
                var local = DateTime.SpecifyKind(date.SelectedDate.Value.Date + clock.ToTimeSpan(), DateTimeKind.Local);
                if (TimeZoneInfo.Local.IsInvalidTime(local)) throw new ArgumentException(c.T("Diese lokale Uhrzeit existiert wegen der Zeitumstellung nicht.", "This local time does not exist because of daylight saving time."));
                expiry = new DateTimeOffset(local);
                if (expiry <= DateTimeOffset.Now) throw new ArgumentException(c.T("Das Ablaufdatum muss in der Zukunft liegen.", "Expiry must be in the future."));
            }
            var request = new { name = name.Text.Trim(), paths = selectedTransfer is null ? chosen.ToArray() : null, transferId = selectedTransfer?.Id, password = password.Password, expiresAt = expiry?.ToUniversalTime().ToString("O", CultureInfo.InvariantCulture), maxDownloads = maximum };
            var result = await c.RequestAsync<SharedItem>("share.create", request);
            password.Clear(); await c.RefreshAsync(); ShowCreated(result);
        });
        form.Action(c.T("Aktualisieren", "Refresh"), async () => { await c.RefreshAsync(); Render(); });
        form.AfterOperation = Render;
        PropertyChangedEventHandler changed = (_, _) => Render(); c.PropertyChanged += changed;
        form.Window.Closed += (_, _) => { c.PropertyChanged -= changed; c.ObserveTool("shares", false); password.Clear(); };
        c.ObserveTool("shares", true); Render(); form.Show();
    }

    public static void Clipboard(Window owner, Controller c)
    {
        var form = new Panel(owner, c.T("Zwischenablage", "Clipboard"));
        form.Help(c.T("Text wird nur durch deine Aktion eingefügt, gesendet oder kopiert. Nachrichten bleiben bis zu einer Stunde in der laufenden Sitzung erhalten.",
            "Text is pasted, sent or copied only after your action. Messages remain in the current session for up to one hour."));
        Caption(form.Body, c.T("Empfänger", "Recipient"));
        var target = new ComboBox { DisplayMemberPath = "Name", SelectedValuePath = "Id" }; form.Add(target);
        var permissions = Theme.Text("", 12, secondary: true); permissions.Margin = new Thickness(0, 8, 0, 8); form.Add(permissions);
        var draft = new TextBox { AcceptsReturn = true, TextWrapping = TextWrapping.Wrap, VerticalScrollBarVisibility = ScrollBarVisibility.Auto, MinHeight = 125, MaxHeight = 180, MaxLength = 65536 };
        form.Add(draft);
        var count = Theme.Text("", 12, secondary: true); count.Margin = new Thickness(0, 8, 0, 8); form.Add(count);
        var buttons = new WrapPanel(); form.Add(buttons);
        buttons.Children.Add(Theme.Button(c.T("Einfügen", "Paste"), () => _ = form.Execute(() =>
        {
            if (!System.Windows.Clipboard.ContainsText()) { form.Notice(c.T("Die Zwischenablage enthält keinen Text.", "The clipboard contains no text.")); return Task.CompletedTask; }
            var text = System.Windows.Clipboard.GetText();
            if (UTF8Count(text) > 65536) throw new ArgumentException(c.T("Der Text ist größer als 64 KiB. Wähle einen kürzeren Text.", "This text exceeds 64 KiB. Choose shorter text."));
            draft.Text = text; return Task.CompletedTask;
        })));
        var send = Theme.Button(c.T("Senden", "Send"), () => { }, true); buttons.Children.Add(send);
        var heading = Theme.Text(c.T("Textnachrichten", "Text messages"), 18, true); heading.Margin = new Thickness(0, 22, 0, 10); form.Add(heading);
        var messages = new StackPanel(); form.Add(messages);
        string targetsKey = "", messagesKey = ""; bool updating = false;
        void Render()
        {
            if (updating) return;
            updating = true;
            try
            {
                var devices = c.Status?.Devices.Where(d => d.Paired && d.CanClipboard).OrderBy(d => d.Name).ToArray() ?? [];
                var key = string.Join('|', devices.Select(d => d.Id + ":" + d.Name));
                if (key != targetsKey)
                {
                    var selected = target.SelectedValue as string ?? c.SelectedDeviceId;
                    targetsKey = key; target.ItemsSource = devices;
                    target.SelectedValue = devices.Any(d => d.Id == selected) ? selected : devices.Length == 1 ? devices[0].Id : null;
                }
                permissions.Text = devices.Length == 0 ? c.T("Aktiviere Zwischenablage in den Berechtigungen eines gekoppelten Geräts. Zwischen Computern muss sie auf beiden Seiten erlaubt sein.", "Enable clipboard in a paired device’s permissions. Between computers, both sides need to allow it.") : c.T("Zwischen Computern muss die Zwischenablage auf beiden Seiten freigegeben sein.", "Between computers, clipboard permission is required on both sides.");
                var length = UTF8Count(draft.Text); count.Text = length == int.MaxValue ? c.T("Der Text enthält ungültige Unicode-Zeichen.", "This text contains invalid Unicode characters.") : length + " / 65536 UTF-8 bytes";
                send.IsEnabled = c.Ready && !form.Busy && length is > 0 and <= 65536 && devices.Any(d => d.Id == target.SelectedValue as string);
                var current = c.Status?.Clipboard.AsEnumerable().Reverse().ToArray() ?? [];
                var next = string.Join('|', current.Select(m => m.Id));
                if (messagesKey == next && messages.Children.Count > 0) return;
                messagesKey = next; messages.Children.Clear();
                if (current.Length == 0) messages.Children.Add(Theme.Text(c.T("Noch keine Textnachrichten", "No text messages yet"), 13, secondary: true));
                foreach (var message in current)
                {
                    var body = new StackPanel(); body.Children.Add(Theme.Text((message.Direction == "receive" ? c.T("Von ", "From ") : c.T("An ", "To ")) + message.DeviceName, 13, true));
                    var text = new TextBox { Text = message.Text, IsReadOnly = true, AcceptsReturn = true, TextWrapping = TextWrapping.Wrap, MaxHeight = 125, VerticalScrollBarVisibility = ScrollBarVisibility.Auto, Margin = new Thickness(0, 8, 0, 10) }; body.Children.Add(text);
                    var actions = new WrapPanel();
                    actions.Children.Add(Theme.Button(c.T("Kopieren", "Copy"), () => _ = form.Execute(() => { System.Windows.Clipboard.SetText(message.Text); form.Notice(c.T("Text kopiert", "Text copied")); return Task.CompletedTask; })));
                    actions.Children.Add(Theme.Button(c.T("Verwerfen", "Dismiss"), () => _ = form.Execute(async () => { await c.RequestAsync("clipboard.dismiss", new { id = message.Id }); await c.RefreshAsync(); })));
                    body.Children.Add(actions); var card = Theme.Card(body); card.Margin = new Thickness(0, 0, 0, 10); messages.Children.Add(card);
                }
            }
            finally { updating = false; }
        }
        send.Click += async (_, _) => await form.Execute(async () =>
        {
            var deviceId = target.SelectedValue as string; var text = draft.Text;
            if (c.Status?.Devices.Any(d => d.Id == deviceId && d.Paired && d.CanClipboard) != true)
                throw new InvalidOperationException(c.T("Wähle ein Gerät mit freigegebener Zwischenablage.", "Choose a device with clipboard permission."));
            if (UTF8Count(text) is not (> 0 and <= 65536)) throw new ArgumentException(c.T("Der Text muss 1 bis 65536 UTF-8 Bytes enthalten.", "Text must contain 1–65536 UTF-8 bytes."));
            await c.RequestAsync<ClipboardMessage>("clipboard.send", new { deviceId, text });
            if (draft.Text == text) draft.Clear(); await c.RefreshAsync(); form.Notice(c.T("Text gesendet", "Text sent"));
        });
        draft.TextChanged += (_, _) => Render(); target.SelectionChanged += (_, _) => Render();
        form.Action(c.T("Aktualisieren", "Refresh"), async () => { await c.RefreshAsync(); Render(); });
        form.AfterOperation = Render;
        PropertyChangedEventHandler changed = (_, _) => Render(); c.PropertyChanged += changed;
        form.Window.Closed += (_, _) => { c.PropertyChanged -= changed; c.ObserveTool("clipboard", false); };
        c.ObserveTool("clipboard", true); Render(); form.Show();
    }

    private static int UTF8Count(string text)
    {
        try { return new UTF8Encoding(false, true).GetByteCount(text); }
        catch (EncoderFallbackException) { return int.MaxValue; }
    }
    private static bool ValidLink(string? text) => Uri.TryCreate(text, UriKind.Absolute, out var uri) && uri.Scheme == "https" && uri.Fragment.Length > 1 && uri.UserInfo.Length == 0;
    private static void Caption(StackPanel parent, string caption)
    {
        var label = Theme.Text(caption, 13, true); label.Margin = new Thickness(0, 12, 0, 5); parent.Children.Add(label);
    }
    private static TextBox Input(StackPanel parent, string caption, string value = "")
    {
        Caption(parent, caption); var input = new TextBox { Text = value }; parent.Children.Add(input); return input;
    }

    private sealed class Panel
    {
        public Window Window { get; }
        public StackPanel Body { get; } = new();
        public bool Busy { get; private set; }
        public Action? AfterOperation { get; set; }
        private readonly WrapPanel actions = new() { Margin = new Thickness(0, 18, 0, 0) };
        private readonly TextBlock error = Theme.Text("", 13);
        private readonly TextBlock notice = Theme.Text("", 13, secondary: true);
        private readonly ProgressBar progress = new() { IsIndeterminate = true, Height = 3, Visibility = Visibility.Collapsed, Margin = new Thickness(0, 12, 0, 0) };
        public Panel(Window owner, string title)
        {
            Window = new Window { Owner = owner, Title = title, Width = 680, MinWidth = 560, MaxHeight = Math.Max(400, Math.Min(850, SystemParameters.WorkArea.Height - 80)), MinHeight = 300,
                SizeToContent = SizeToContent.Height, WindowStartupLocation = WindowStartupLocation.CenterOwner, ShowInTaskbar = false, Background = Theme.Background, Foreground = Theme.Foreground, FontFamily = owner.FontFamily, FontSize = 14 };
            var root = new StackPanel { Margin = new Thickness(24) }; root.Children.Add(Theme.Text(title, 24, true));
            Body.Margin = new Thickness(0, 14, 0, 0); root.Children.Add(Body);
            error.Foreground = Theme.Color(Theme.Dark ? "#FFB3A9" : "#AF3125");
            error.Margin = notice.Margin = new Thickness(0, 12, 0, 0); error.Visibility = notice.Visibility = Visibility.Collapsed;
            root.Children.Add(error); root.Children.Add(notice); root.Children.Add(progress); root.Children.Add(actions);
            var close = Theme.Button("OK", () => Window.Close()); close.IsCancel = true; actions.Children.Add(close);
            Window.Content = new ScrollViewer { Content = root, VerticalScrollBarVisibility = ScrollBarVisibility.Auto, HorizontalScrollBarVisibility = ScrollBarVisibility.Disabled };
            Window.Closing += (_, e) => { if (Busy) e.Cancel = true; };
        }
        public void Add(UIElement view) => Body.Children.Add(view);
        public void Help(string text) { var label = Theme.Text(text, 13, secondary: true); label.Margin = new Thickness(0, 4, 0, 8); Add(label); }
        public void Action(string title, Func<Task> action) => actions.Children.Insert(0, Theme.Button(title, () => _ = Execute(action)));
        public void Notice(string message) { notice.Text = message; notice.Visibility = Visibility.Visible; }
        public async Task Execute(Func<Task> action)
        {
            if (Busy) return;
            Busy = true; Body.IsEnabled = false; actions.IsEnabled = false; progress.Visibility = Visibility.Visible;
            error.Text = ""; error.Visibility = notice.Visibility = Visibility.Collapsed;
            try { await action(); }
            catch (Exception e) { error.Text = e.Message; error.Visibility = Visibility.Visible; }
            finally { Busy = false; Body.IsEnabled = true; actions.IsEnabled = true; progress.Visibility = Visibility.Collapsed; AfterOperation?.Invoke(); }
        }
        public void Show() => Window.ShowDialog();
    }
}
