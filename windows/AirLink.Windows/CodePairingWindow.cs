using System;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using System.Windows.Threading;
using AirLink.Core;

namespace AirLink.Windows;

internal sealed class CodePairingWindow : Window
{
    private readonly Controller c;
    private readonly string id;
    private readonly TextBlock peer, code, state;
    private readonly CheckBox automatic, matches;
    private readonly Button confirm;
    private readonly DispatcherTimer timer = new() { Interval = TimeSpan.FromMilliseconds(700) };
    private bool fetching, closed;
    public CodePairingWindow(Window owner, Controller controller, string sessionId)
    {
        c = controller; id = sessionId; Owner = owner; Title = c.T("Gerät bestätigen", "Confirm device");
        Width = 550; SizeToContent = SizeToContent.Height; WindowStartupLocation = WindowStartupLocation.CenterOwner;
        Background = Theme.Background; Foreground = Theme.Foreground; ShowInTaskbar = false;
        var body = new StackPanel { Margin = new Thickness(30) };
        peer = Theme.Text("", 23, true); body.Children.Add(peer);
        body.Children.Add(Theme.Text(c.T("Vergleiche den Code direkt auf beiden Computern. Bestätige nur, wenn beide Anzeigen übereinstimmen.", "Compare the code directly on both computers. Confirm only if both displays match."), 14, secondary: true));
        code = Theme.Text("", 36, true); code.FontFamily = new FontFamily("Consolas"); code.Margin = new Thickness(0, 24, 0, 20); body.Children.Add(code);
        matches = new CheckBox { Content = c.T("Beide Codes stimmen überein", "Both codes match"), Margin = new Thickness(0, 8, 0, 14) }; body.Children.Add(matches);
        automatic = new CheckBox { Content = Theme.Text(c.T("Dateien von diesem Gerät automatisch annehmen", "Automatically accept files from this device"), 13), Margin = new Thickness(0, 4, 0, 15) }; body.Children.Add(automatic);
        state = Theme.Text("", 13, secondary: true); body.Children.Add(state);
        var actions = new WrapPanel { Margin = new Thickness(0, 20, 0, 0) };
        confirm = Theme.Button(c.T("Code bestätigen", "Confirm code"), () => _ = Confirm(), true); confirm.IsEnabled = false;
        actions.Children.Add(confirm);
        actions.Children.Add(Theme.Button(c.T("Kopplung abbrechen", "Cancel pairing"), () => _ = c.RunAsync(async () => { await c.CommandAsync("pairing.cancel", id); Close(); })));
        actions.Children.Add(Theme.Button(c.T("Schließen", "Close"), Close)); body.Children.Add(actions);
        Content = body;
        matches.Checked += (_, _) => _ = Refresh(); matches.Unchecked += (_, _) => confirm.IsEnabled = false;
        timer.Tick += async (_, _) => await Refresh();
        Loaded += async (_, _) => { timer.Start(); await Refresh(); };
        Closed += (_, _) => { closed = true; timer.Stop(); };
    }
    private async Task Refresh()
    {
        if (fetching || closed) return; fetching = true;
        try {
            var session = await c.RequestAsync<PairingSession>("pairing.status", new { id });
            if (closed) return;
            peer.Text = session.PeerName; code.Text = session.FormattedCode;
            confirm.IsEnabled = session.CanConfirm && matches.IsChecked == true;
            automatic.IsEnabled = matches.IsEnabled = !session.LocalConfirmed && session.State == "comparing";
            state.Text = session.Error ?? (session.State == "paired" ? c.T("Gerät verbunden. Du kannst Dateien senden.", "Device paired. You can send files.") : session.State is "cancelled" or "denied" or "expired" ? c.T("Die Kopplung wurde beendet. Starte sie bei Bedarf erneut.", "Pairing ended. Start again if needed.") : session.LocalConfirmed ? c.T("Warte auf die Bestätigung am anderen Computer …", "Waiting for confirmation on the other computer …") : c.T("Bestätigung ist auf beiden Computern erforderlich.", "Confirmation is required on both computers."));
            if (session.State == "paired") { c.SelectedDeviceId = session.PeerId; timer.Stop(); await c.RefreshAsync(true); }
        } catch (Exception e) { if (!closed) state.Text = e.Message; }
        finally { fetching = false; }
    }
    private async Task Confirm()
    {
        confirm.IsEnabled = false;
        try { await c.ConfirmPairingAsync(id, automatic.IsChecked == true); await Refresh(); }
        catch (Exception e) { state.Text = e.Message; await Refresh(); }
    }
}
