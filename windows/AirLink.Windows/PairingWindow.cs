using System;
using System.ComponentModel;
using System.Linq;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using AirLink.Core;

namespace AirLink.Windows;

/// <summary>
/// Chooses the other computer. Every path, including manual address entry,
/// continues with the mutual comparison code; discovery never grants trust.
/// </summary>
internal sealed class PairingWindow : Window
{
    private enum Step { Choose, Manual }
    private readonly Controller c;
    private readonly StackPanel body = new(), actions = new() { Orientation = Orientation.Horizontal, HorizontalAlignment = HorizontalAlignment.Right };
    private readonly TextBlock subtitle = Theme.Text("", 13, secondary: true), error = Theme.Text("", 13);
    private readonly Button back, next, close;
    private StackPanel? nearby;
    private string nearbyKey = "";
    private Step step;
    private ConnectionDraft draft = new("", "");
    private bool busy;

    public PairingWindow(Window owner, Controller controller, Device? seed)
    {
        c = controller; Owner = owner; Title = c.T("Gerät hinzufügen", "Add device");
        Width = Math.Min(620, SystemParameters.WorkArea.Width - 32);
        MaxHeight = Math.Max(320, Math.Min(840, SystemParameters.WorkArea.Height - 40));
        SizeToContent = SizeToContent.Height; MinHeight = 320;
        WindowStartupLocation = WindowStartupLocation.CenterOwner; ShowInTaskbar = false;
        Background = Theme.Background; Foreground = Theme.Foreground;
        FontFamily = new FontFamily("Segoe UI Variable, Segoe UI"); FontSize = 14;
        var root = new StackPanel { Margin = new Thickness(28) };
        root.Children.Add(Theme.Text(Title, 25, true)); subtitle.Margin = new Thickness(0, 7, 0, 22); root.Children.Add(subtitle);
        root.Children.Add(body); error.Foreground = Theme.Color(Theme.Dark ? "#FFB5AE" : "#B1372A"); error.Margin = new Thickness(0, 14, 0, 0); root.Children.Add(error);
        back = Theme.Button(c.T("Zurück", "Back"), () => { step = Step.Choose; Render(); });
        close = Theme.Button(c.T("Schließen", "Close"), Close); close.IsCancel = true;
        next = Theme.Button(c.T("Verbindung anfragen", "Request connection"), () => _ = StartAsync(draft), true); next.IsDefault = true;
        actions.Margin = new Thickness(0, 24, 0, 0); actions.Children.Add(back); actions.Children.Add(close); actions.Children.Add(next); root.Children.Add(actions);
        Content = new ScrollViewer { Content = root, VerticalScrollBarVisibility = ScrollBarVisibility.Auto, HorizontalScrollBarVisibility = ScrollBarVisibility.Disabled };
        c.PropertyChanged += StatusChanged;
        Closing += (_, e) => { if (busy) e.Cancel = true; };
        Closed += (_, _) => c.PropertyChanged -= StatusChanged;
        Render();
        if (seed is { Paired: false, Kind: "engine" }) Loaded += (_, _) => _ = StartAsync(new ConnectionDraft(seed.Address ?? "", seed.Fingerprint ?? ""));
    }

    private void StatusChanged(object? sender, PropertyChangedEventArgs e)
    {
        if (step == Step.Choose) RefreshNearby();
        UpdateButtons();
    }
    private void Help(string value)
    {
        var label = Theme.Text(value, 13, secondary: true); label.Margin = new Thickness(0, 6, 0, 14); body.Children.Add(label);
    }
    private async Task StartAsync(ConnectionDraft target)
    {
        if (busy) return;
        if (!target.IsValid) { error.Text = c.T("Eine HTTPS-Adresse und den Fingerabdruck mit 64 Zeichen eingeben.", "Enter an HTTPS address and the 64-character fingerprint."); return; }
        busy = true; error.Text = ""; UpdateButtons();
        try
        {
            var session = await c.StartPairingAsync(target.NormalizedAddress, target.NormalizedFingerprint);
            busy = false; UpdateButtons();
            new CodePairingWindow(IsVisible ? this : Owner!, c, session.Id).ShowDialog();
            var final = await c.RequestAsync<PairingSession>("pairing.status", new { id = session.Id });
            if (final.State == "paired") Close();
        }
        catch (Exception e) { error.Text = e.Message; }
        finally { busy = false; UpdateButtons(); }
    }
    private void Render()
    {
        body.Children.Clear(); nearby = null; error.Text = "";
        subtitle.Text = step == Step.Choose ? c.T("Computer auswählen", "Choose a computer") : c.T("Manuell verbinden", "Connect manually");
        if (step == Step.Choose)
        {
            Help(c.T("Öffne AirLink auf dem anderen Computer. Geräte im selben Netzwerk erscheinen hier automatisch.", "Open AirLink on the other computer. Devices on the same network appear here automatically."));
            nearby = new StackPanel(); body.Children.Add(nearby); nearbyKey = ""; RefreshNearby();
            var manual = Theme.Button(c.T("Nicht gefunden? Manuell verbinden …", "Not found? Connect manually …"), () => { step = Step.Manual; draft = new("https://", ""); Render(); });
            manual.HorizontalAlignment = HorizontalAlignment.Left; manual.Margin = new Thickness(0, 16, 0, 12); body.Children.Add(manual);
        }
        else
        {
            Help(c.T("Übernimm Adresse und Fingerabdruck aus „Dieses Gerät“ auf dem anderen Computer. Danach vergleicht ihr den Code auf beiden Bildschirmen.",
                "Copy the address and fingerprint from This device on the other computer. You then compare the code on both screens."));
            body.Children.Add(Theme.Text(c.T("HTTPS-Adresse", "HTTPS address"), 13, true));
            var address = new TextBox { Text = draft.Address, MaxLength = 2048, Margin = new Thickness(0, 6, 0, 15) }; body.Children.Add(address);
            body.Children.Add(Theme.Text(c.T("Fingerabdruck · SHA-256", "Fingerprint · SHA-256"), 13, true));
            var fingerprint = new TextBox { Text = draft.Fingerprint, MaxLength = 256, FontFamily = new FontFamily("Consolas"), TextWrapping = TextWrapping.Wrap, Margin = new Thickness(0, 6, 0, 0) }; body.Children.Add(fingerprint);
            void Changed(object sender, TextChangedEventArgs e) { draft = new(address.Text, fingerprint.Text); UpdateButtons(); }
            address.TextChanged += Changed; fingerprint.TextChanged += Changed;
        }
        UpdateButtons();
    }
    private void RefreshNearby()
    {
        if (nearby is null) return;
        var peers = (c.Status?.Devices ?? []).Where(d => !d.Paired && d.Kind == "engine" && !string.IsNullOrEmpty(d.Address)).OrderBy(d => d.Name, StringComparer.CurrentCultureIgnoreCase).ToArray();
        var key = string.Join('|', peers.Select(d => d.Id + d.Name + d.Address + d.Fingerprint));
        if (nearbyKey == key && nearby.Children.Count > 0) return; nearbyKey = key; nearby.Children.Clear();
        if (peers.Length == 0) nearby.Children.Add(Theme.Text(c.T("Noch kein anderer Computer gefunden. Beide Geräte müssen im selben Netzwerk sein und AirLink geöffnet haben.", "No other computer found yet. Both devices need to be on the same network with AirLink open."), 13, secondary: true));
        foreach (var peer in peers)
        {
            var label = new StackPanel(); label.Children.Add(Theme.Text(peer.Name, 16, true)); label.Children.Add(Theme.Text(c.T("Im Netzwerk gefunden · auswählen", "Found on your network · choose"), 12, secondary: true));
            var target = new ConnectionDraft(peer.Address ?? "", peer.Fingerprint ?? "");
            var button = Theme.Button("", () => _ = StartAsync(target)); button.Content = label; button.HorizontalContentAlignment = HorizontalAlignment.Left; button.Padding = new Thickness(18); button.Margin = new Thickness(0, 0, 0, 9); nearby.Children.Add(button);
        }
    }
    private void UpdateButtons()
    {
        body.IsEnabled = !busy; close.IsEnabled = !busy; back.IsEnabled = !busy;
        back.Visibility = next.Visibility = step == Step.Manual ? Visibility.Visible : Visibility.Collapsed;
        next.Content = busy ? c.T("Wird angefragt …", "Requesting …") : c.T("Verbindung anfragen", "Request connection");
        next.IsEnabled = !busy && c.Ready && draft.IsValid;
    }
}
