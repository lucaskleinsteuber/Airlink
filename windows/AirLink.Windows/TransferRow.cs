using System;
using System.Linq;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using AirLink.Core;

namespace AirLink.Windows;

internal sealed class TransferRow : Border
{
    private readonly MainWindow owner;
    private readonly Controller c;
    private readonly TextBlock title, state, detail, problem;
    private readonly ProgressBar progress;
    private readonly WrapPanel actions;
    private readonly StackPanel files;
    private string actionKey = "";
    private Transfer current = new();
    public TransferRow(MainWindow owner, Controller controller)
    {
        this.owner = owner; c = controller; Background = Theme.Surface; BorderBrush = Theme.Line; BorderThickness = new Thickness(1); CornerRadius = new CornerRadius(13); Padding = new Thickness(18); Margin = new Thickness(0, 11, 0, 0);
        var body = new StackPanel(); title = Theme.Text("", 15, true); body.Children.Add(title);
        state = Theme.Text("", 12, secondary: true); state.Margin = new Thickness(0, 3, 0, 12); body.Children.Add(state);
        progress = new ProgressBar { Minimum = 0, Maximum = 1, Height = 4, Foreground = Theme.Accent, Background = Theme.Line, BorderThickness = new Thickness(0) }; body.Children.Add(progress);
        detail = Theme.Text("", 12, secondary: true); detail.Margin = new Thickness(0, 8, 0, 8); body.Children.Add(detail);
        problem = Theme.Text("", 12); problem.Foreground = Theme.Color(Theme.Dark ? "#FFB5AE" : "#B1372A"); problem.Margin = new Thickness(0, 0, 0, 10); body.Children.Add(problem);
        actions = new WrapPanel(); body.Children.Add(actions); files = new StackPanel(); body.Children.Add(files); Child = body;
    }
    private Button Action(string label, Func<Task> task, bool primary = false) => Theme.Button(label, () => _ = c.RunAsync(task), primary);
    private async Task ShowResult()
    {
        var result = await c.GetReceivedAsync(current.Id);
        if (result.Items.Count == 1)
        {
            if (result.Items[0].Directory) Shell.OpenFolder(result.Items[0].Path);
            else Shell.Reveal(result.Items[0].Path);
        }
        else Shell.OpenFolder(result.Directory);
    }
    public void Update(Transfer t)
    {
        current = t; title.Text = t.Title;
        state.Text = (t.Direction == "receive" ? c.T("Von ", "From ") : c.T("An ", "To ")) + t.DeviceName + " · " + c.StateText(t.State);
        progress.Value = t.Progress; progress.Visibility = t.IsActive ? Visibility.Visible : Visibility.Collapsed;
        var speed = double.IsFinite(t.BytesPerSecond) && t.BytesPerSecond > 0 ? t.BytesPerSecond : 0;
        detail.Text = t.State == "completed" ? c.Bytes(t.CompletedBytes) + (t.SkippedBytes > 0 ? " · " + c.Bytes(t.SkippedBytes) + c.T(" übersprungen", " skipped") : "") : c.Bytes(t.CompletedBytes) + " / " + c.Bytes(t.TotalBytes) + (speed > 0 && t.State == "running" ? " · " + c.Bytes((long)Math.Min(speed, long.MaxValue)) + "/s" : "");
        problem.Text = t.Problem is { } advice ? c.T(advice.TitleDE, advice.TitleEN) + "\n" + c.T(advice.RecoveryDE, advice.RecoveryEN) : t.Error ?? ""; problem.Visibility = string.IsNullOrWhiteSpace(t.Error) ? Visibility.Collapsed : Visibility.Visible;
        if (DesktopTransferSelection.Remaining(t) is { } left) detail.Text += " · " + (left.TotalMinutes >= 1 ? Math.Ceiling(left.TotalMinutes) + c.T(" Min. übrig", " min left") : Math.Ceiling(left.TotalSeconds) + c.T(" Sek. übrig", " sec left"));
        var key = t.Retryable + ":" + t.HasContent + ":" + t.State + ":" + c.Ready + ":" + c.RemovingHistory + ":" + string.Join('|', t.Files.Select(f => f.Id + f.Completed + f.Skipped + f.DuplicateState));
        if (key == actionKey) return; actionKey = key; actions.Children.Clear(); files.Children.Clear();
        ContextMenu = null;
        if (t.Retryable) actions.Children.Add(Action(c.T("Erneut versuchen", "Try again"), () => c.CommandAsync("transfer.resume", current.Id)));
        if (t.State == "offered" && t.Direction == "receive")
        {
            var trust = new CheckBox { Content = c.T("Für dieses Gerät künftig automatisch", "Automatically receive from this device in future"), Margin = new Thickness(0, 8, 0, 8) };
            trust.Visibility = NativeExperience.CanAcceptAndTrust(t, c.Status?.Devices.FirstOrDefault(d => d.Id == t.DeviceId)) ? Visibility.Visible : Visibility.Collapsed;
            actions.Children.Add(Action(c.T("Annehmen", "Accept"), () => c.CommandAsync(trust.IsChecked == true ? "transfer.acceptTrusted" : "transfer.accept", current.Id), true)); files.Children.Add(trust);
        }
        if (t.State is "running" or "queued" or "reconnecting" or "checkingDuplicate" or "waitingDuplicate") actions.Children.Add(Action(c.T("Pause", "Pause"), () => c.CommandAsync("transfer.pause", current.Id)));
        if (t.State is "paused" or "reconnecting") actions.Children.Add(Action(c.T("Fortsetzen", "Resume"), () => c.CommandAsync("transfer.resume", current.Id), true));
        if (t.IsActive) actions.Children.Add(Action(c.T("Abbrechen", "Cancel"), () => c.CommandAsync("transfer.cancel", current.Id)));
        actions.Children.Add(Theme.Button(c.T("Details", "Details"), () => Dialogs.TransferDetails(owner, c, current)));
        foreach (var file in t.Files.Where(f => !t.Summary && f.DuplicateState == "pending"))
            files.Children.Add(Theme.Button(file.Path + c.T(" · Duplikat prüfen", " · Review duplicate"), () => { var fresh = current.Files.FirstOrDefault(f => f.Id == file.Id); if (fresh is not null) Dialogs.Duplicate(owner, c, current, fresh); }));
        if (t.Direction == "receive" && t.State == "completed")
        {
            var received = NativeExperience.ReceivedDisplayEntries(t);
            if (t.HasReceivedContent)
            {
                actions.Children.Add(Action(received.Count == 1 && !received[0].Directory ? c.T("Datei zeigen", "Show file") : c.T("Ordner öffnen", "Open folder"), ShowResult, true));
                if (!t.Summary && received.Count > 1)
                {
                    var entries = new StackPanel();
                    foreach (var file in received.Take(20)) entries.Children.Add(Action(file.Path + " ↗", () => c.RevealAsync(current, file.Id)));
                    files.Children.Add(new Expander { Header = c.T("Dateien anzeigen", "Show files"), Content = entries, Foreground = Theme.Secondary, Margin = new Thickness(0, 12, 0, 0) });
                }
            }
            else files.Children.Add(Theme.Text(c.T("Identische Dateien übersprungen", "Identical files skipped"), 12, secondary: true));
        }
        if (t.State == "completed" && t.Files.Any(f => !f.Directory && !f.Skipped))
        {
            var menu = new ContextMenu(); var share = new MenuItem { Header = c.T("Freigabelink erstellen …", "Create share link …") };
            share.Click += (_, _) => SharingDialogs.Share(owner, c, transfer: current); menu.Items.Add(share); ContextMenu = menu;
        }
        if (NativeExperience.CanRemoveFromHistory(t))
        {
            var remove = Action(c.T("Aus Liste entfernen", "Remove from list"), () => c.RemoveHistoryAsync(new[] { ProtocolJson.Clone(current) }));
            remove.ToolTip = c.T("Entfernt nur diesen Verlaufseintrag. Die Dateien bleiben erhalten.", "Removes this history entry only. The files are preserved.");
            remove.IsEnabled = c.Ready && !c.RemovingHistory; actions.Children.Add(remove);
        }
        actions.IsEnabled = files.IsEnabled = c.Ready;
    }
}
