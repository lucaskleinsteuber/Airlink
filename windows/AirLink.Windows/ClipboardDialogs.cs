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

namespace AirLink.Windows;

internal static class ClipboardDialogs
{
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
