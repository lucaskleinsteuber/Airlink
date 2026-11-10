using System;
using System.ComponentModel;
using System.Linq;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using System.Windows.Threading;
using AirLink.Core;

namespace AirLink.Windows;

/// <summary>A native, explicit comparison flow. Discovery never grants trust.</summary>
internal sealed class PairingWindow : Window
{
    private enum Step { Choose, Manual, Compare, Waiting, Connected }
    private readonly Controller c;
    private readonly StackPanel body = new(), actions = new() { Orientation = Orientation.Horizontal, HorizontalAlignment = HorizontalAlignment.Right };
    private readonly TextBlock subtitle = Theme.Text("", 13, secondary: true), error = Theme.Text("", 13);
    private readonly Button back, next, close;
    private readonly ConnectionVerification verification = new();
    private readonly DispatcherTimer timer = new() { Interval = TimeSpan.FromSeconds(1) };
    private StackPanel? nearby;
    private string nearbyKey = "";
    private Step step;
    private ConnectionDraft draft = new("", "");
    private string peerName = "", pairedId = "";
    private DateTimeOffset requestedAt;
    private bool busy;
    private bool manuallyEntered;

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
        back = Theme.Button(c.T("Zurück", "Back"), Back);
        close = Theme.Button(c.T("Schließen", "Close"), Close); close.IsCancel = true;
        next = Theme.Button("", () => _ = ContinueAsync(), true); next.IsDefault = true;
        actions.Margin = new Thickness(0, 24, 0, 0); actions.Children.Add(back); actions.Children.Add(close); actions.Children.Add(next); root.Children.Add(actions);
        Content = new ScrollViewer { Content = root, VerticalScrollBarVisibility = ScrollBarVisibility.Auto, HorizontalScrollBarVisibility = ScrollBarVisibility.Disabled };
        c.PropertyChanged += StatusChanged; timer.Tick += (_, _) => UpdateWaiting(); timer.Start();
        Closing += (_, e) => { if (busy) e.Cancel = true; };
        Closed += (_, _) => { c.PropertyChanged -= StatusChanged; timer.Stop(); };
        if (seed is { Paired: false, Kind: "engine" }) Choose(seed); else Render();
    }

    private void StatusChanged(object? sender, PropertyChangedEventArgs e)
    {
        if (step == Step.Choose) RefreshNearby();
        if (step == Step.Waiting) UpdateWaiting();
        else if (step == Step.Compare && !busy && verification.Matches(draft) && draft.PairedDevice(c.Status?.Devices ?? []) is { } paired)
        {
            pairedId = paired.Id; step = Step.Connected; Render();
        }
        UpdateButtons();
    }
    private void Help(string value)
    {
        var label = Theme.Text(value, 13, secondary: true); label.Margin = new Thickness(0, 6, 0, 14); body.Children.Add(label);
    }
    private void Back()
    {
        verification.Reset(); error.Text = "";
        step = step == Step.Compare && manuallyEntered ? Step.Manual : Step.Choose;
        Render();
    }
    private async void Choose(Device device)
    {
        if (busy) return;
        busy = true; UpdateButtons();
        try {
            var session = await c.StartPairingAsync(device.Address ?? "");
            new CodePairingWindow(IsVisible ? this : Owner!, c, session.Id).ShowDialog();
            busy = false;
            if (c.Status?.Devices.Any(d => d.Id == session.PeerId && d.Paired) == true) Close();
        } catch (Exception e) { error.Text = e.Message + c.T(" Nutze bei einer älteren App den manuellen Fingerabdruck-Vergleich.", " For an older app, use the manual fingerprint comparison."); }
        finally { busy = false; UpdateButtons(); }
    }
    private void ChooseLegacy(Device device)
    {
        draft = new ConnectionDraft(device.Address ?? "", device.Fingerprint ?? ""); peerName = device.Name;
        manuallyEntered = !draft.IsValid; verification.Reset(); step = draft.IsValid ? Step.Compare : Step.Manual; error.Text = ""; Render();
    }
    private void Render()
    {
        body.Children.Clear(); nearby = null; error.Text = "";
        subtitle.Text = step switch
        {
            Step.Choose => c.T("1 · Computer auswählen", "1 · Choose a computer"),
            Step.Manual => c.T("1 · Manuell verbinden", "1 · Connect manually"),
            Step.Compare => c.T("2 · Gerät bestätigen", "2 · Confirm device"),
            Step.Waiting => c.T("3 · Am anderen Gerät bestätigen", "3 · Approve on the other device"),
            _ => c.T("Bereit zum Teilen", "Ready to share")
        };
        if (step == Step.Choose)
        {
            Help(c.T("Öffne AirLink auf dem anderen Computer. Geräte im selben Netzwerk erscheinen hier automatisch.", "Open AirLink on the other computer. Devices on the same network appear here automatically."));
            nearby = new StackPanel(); body.Children.Add(nearby); nearbyKey = ""; RefreshNearby();
            var manual = Theme.Button(c.T("Nicht gefunden? Manuell verbinden …", "Not found? Connect manually …"), () => { step = Step.Manual; manuallyEntered = true; draft = new("https://", ""); peerName = ""; Render(); });
            manual.HorizontalAlignment = HorizontalAlignment.Left; manual.Margin = new Thickness(0, 16, 0, 12); body.Children.Add(manual);
            var phone = Theme.Button(c.T("iPhone, iPad oder Android verbinden …", "Connect iPhone, iPad or Android …"), () => Dialogs.ConnectionInfo(this, c));
            phone.HorizontalAlignment = HorizontalAlignment.Left; body.Children.Add(phone);
        }
        else if (step == Step.Manual)
        {
            Help(c.T("Öffne die Verbindungsdaten in AirLink auf dem anderen Computer und übernimm seine HTTPS-Adresse und seinen vollständigen Fingerabdruck.", "Open connection details in AirLink on the other computer and enter its HTTPS address and complete fingerprint."));
            body.Children.Add(Theme.Text(c.T("HTTPS-Adresse", "HTTPS address"), 13, true));
            var address = new TextBox { Text = draft.Address, MaxLength = 2048, Margin = new Thickness(0, 6, 0, 15) }; body.Children.Add(address);
            body.Children.Add(Theme.Text(c.T("Fingerabdruck · SHA-256", "Fingerprint · SHA-256"), 13, true));
            var fingerprint = new TextBox { Text = draft.Fingerprint, MaxLength = 256, FontFamily = new FontFamily("Consolas"), TextWrapping = TextWrapping.Wrap, Margin = new Thickness(0, 6, 0, 0) }; body.Children.Add(fingerprint);
            void Changed(object sender, TextChangedEventArgs e) { draft = new(address.Text, fingerprint.Text); verification.Reset(); UpdateButtons(); }
            address.TextChanged += Changed; fingerprint.TextChanged += Changed;
        }
        else if (step == Step.Compare)
        {
            body.Children.Add(Theme.Text(string.IsNullOrWhiteSpace(peerName) ? draft.NormalizedAddress : peerName, 20, true));
            Help(c.T("Vergleiche diesen gesamten Fingerabdruck mit der Anzeige in AirLink auf dem anderen Computer. So weißt du, dass du das richtige Gerät verbindest.", "Compare this complete fingerprint with the one shown in AirLink on the other computer. This confirms you are connecting the intended device."));
            var fingerprint = new TextBox { Text = draft.ComparisonFingerprint, IsReadOnly = true, FontFamily = new FontFamily("Consolas"), FontSize = 15, TextWrapping = TextWrapping.Wrap, BorderThickness = new Thickness(0), Background = Theme.Surface, Padding = new Thickness(16) }; body.Children.Add(fingerprint);
            var verified = new CheckBox { Content = Theme.Text(c.T("Stimmt mit der Anzeige am anderen Computer überein", "Matches the display on the other computer"), 13), IsChecked = verification.Matches(draft), Margin = new Thickness(0, 16, 0, 15) };
            verified.Checked += (_, _) => { verification.Confirm(draft); UpdateButtons(); }; verified.Unchecked += (_, _) => { verification.Reset(); UpdateButtons(); }; body.Children.Add(verified);
            var automatic = new CheckBox { Content = Theme.Text(c.T("Von diesem Gerät künftig automatisch empfangen", "Automatically receive from this device in future"), 13), IsChecked = draft.AutomaticReceive };
            automatic.Checked += (_, _) => draft = draft with { AutomaticReceive = true }; automatic.Unchecked += (_, _) => draft = draft with { AutomaticReceive = false }; body.Children.Add(automatic);
            Help(c.T("Optional: Weitere Dateien dieses Geräts kommen auf diesem PC ohne erneute Annahme an. In den Geräteeinstellungen kannst du das jederzeit ändern.", "Optional: future files from this device arrive on this PC without another acceptance prompt. You can change this in device settings at any time."));
        }
        else if (step == Step.Waiting)
        {
            body.Children.Add(Theme.Text(c.T("Anfrage gesendet", "Request sent"), 20, true));
            Help(c.T("Bestätige die Verbindung jetzt in AirLink auf ", "Approve the connection now in AirLink on ") + (string.IsNullOrWhiteSpace(peerName) ? c.T("dem anderen Computer.", "the other computer.") : peerName + "."));
            body.Children.Add(new ProgressBar { IsIndeterminate = true, Height = 4, Margin = new Thickness(0, 10, 0, 12) });
            Help(c.T("Danach bleibt das Gerät als vertrautes Gerät gespeichert. Du kannst dieses Fenster schließen und im Hintergrund warten.", "The device will then stay saved as a trusted device. You can close this window and wait in the background."));
        }
        else
        {
            body.Children.Add(Theme.Text(c.T("Gerät verbunden", "Device connected"), 22, true));
            Help(c.T("Wähle es in der Seitenleiste und lege Dateien hinein. Du musst es beim nächsten Mal nicht erneut hinzufügen.", "Choose it in the sidebar and drop files to send. You do not need to add it again next time."));
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
            var button = Theme.Button("", () => Choose(peer)); button.Content = label; button.HorizontalContentAlignment = HorizontalAlignment.Left; button.Padding = new Thickness(18); button.Margin = new Thickness(0, 0, 0, 9); nearby.Children.Add(button);
        }
    }
    private void UpdateButtons()
    {
        body.IsEnabled = !busy; close.IsEnabled = !busy; back.IsEnabled = !busy;
        back.Visibility = step is Step.Manual or Step.Compare ? Visibility.Visible : Visibility.Collapsed;
        next.Visibility = step == Step.Choose ? Visibility.Collapsed : Visibility.Visible;
        next.Content = step switch { Step.Manual => c.T("Weiter", "Continue"), Step.Compare => busy ? c.T("Wird angefragt …", "Requesting …") : c.T("Verbindung anfragen", "Request connection"), Step.Waiting => c.T("Erneut anfragen", "Request again"), _ => c.T("Zum Gerät", "Go to device") };
        next.IsEnabled = !busy && c.Ready && (step switch { Step.Manual => draft.IsValid, Step.Compare => verification.Matches(draft), Step.Waiting => DateTimeOffset.UtcNow - requestedAt >= TimeSpan.FromMinutes(5), Step.Connected => true, _ => false });
        close.Content = step == Step.Waiting ? c.T("Im Hintergrund warten", "Wait in background") : c.T("Schließen", "Close");
    }
    private void UpdateWaiting()
    {
        if (step != Step.Waiting || busy) return;
        if (draft.PairedDevice(c.Status?.Devices ?? []) is { } paired)
        {
            pairedId = paired.Id; step = Step.Connected; Render();
        }
        else
        {
            if (DateTimeOffset.UtcNow - requestedAt >= TimeSpan.FromMinutes(5)) error.Text = c.T("Die Verbindung wurde noch nicht bestätigt. Prüfe das andere Gerät und frage bei Bedarf erneut an.", "The connection has not been confirmed. Check the other device and request it again if needed.");
            UpdateButtons();
        }
    }
    private async Task ContinueAsync()
    {
        if (busy) return;
        if (step == Step.Manual) { if (!draft.IsValid) return; verification.Reset(); step = Step.Compare; Render(); return; }
        if (step == Step.Connected) { c.SelectedDeviceId = pairedId; Close(); return; }
        if (step is not (Step.Compare or Step.Waiting) || !verification.Matches(draft)) return;
        if (step == Step.Waiting && DateTimeOffset.UtcNow - requestedAt < TimeSpan.FromMinutes(5)) return;
        var submitted = draft; busy = true; error.Text = ""; UpdateButtons();
        try { await c.ConnectAsync(submitted.Address, submitted.Fingerprint, submitted.AutomaticReceive); requestedAt = DateTimeOffset.UtcNow; step = Step.Waiting; Render(); }
        catch (Exception e) { error.Text = e.Message; }
        finally { busy = false; UpdateButtons(); UpdateWaiting(); }
    }
}
