using System;
using System.IO;
using System.Linq;
using System.Text;
using System.Text.RegularExpressions;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using AirLink.Core;
using Microsoft.Win32;

namespace AirLink.Windows;

internal sealed class SetupWindow : Window
{
    private readonly Controller c;
    private readonly StackPanel nameStep = new(), folderStep = new(), reviewStep = new();
    private readonly TextBox name, folder;
    private readonly TextBlock title, explanation, progress, error, summary;
    private readonly TextBlock reviewName, reviewFolder;
    private readonly CheckBox notifications, updates;
    private readonly Button back, next;
    private bool busy;
    private int step;
    public SetupWindow(Window owner, Controller controller)
    {
        c = controller; Owner = owner; Title = c.T("AirLink einrichten", "Set up AirLink");
        Width = Math.Min(600, SystemParameters.WorkArea.Width - 32); MinWidth = Math.Min(560, Width); MaxWidth = 700;
        MaxHeight = Math.Max(320, SystemParameters.WorkArea.Height - 40);
        SizeToContent = SizeToContent.Height; ResizeMode = ResizeMode.NoResize; WindowStartupLocation = WindowStartupLocation.CenterOwner; ShowInTaskbar = false;
        Background = Theme.Background; Foreground = Theme.Foreground; FontFamily = owner.FontFamily; FontSize = 14;
        var body = new StackPanel { Margin = new Thickness(34) };
        progress = Theme.Text("", 12, true, true); body.Children.Add(progress);
        title = Theme.Text("", 28, true); title.Margin = new Thickness(0, 12, 0, 8); body.Children.Add(title);
        explanation = Theme.Text("", 14, secondary: true); explanation.Margin = new Thickness(0, 0, 0, 24); body.Children.Add(explanation);
        nameStep.Children.Add(Theme.Text(c.T("Name dieses Computers", "This computer's name"), 14, true));
        name = new TextBox { Text = SuggestedName(c), MaxLength = 120, Margin = new Thickness(0, 8, 0, 10), FontSize = 17 }; nameStep.Children.Add(name);
        nameStep.Children.Add(Theme.Text(c.T("Zum Beispiel „Laptop Windows“. Den Namen kannst du später in den Einstellungen ändern.", "For example “Windows laptop”. You can change this later in Settings."), 12, secondary: true));
        body.Children.Add(nameStep);
        folderStep.Children.Add(Theme.Text(c.T("Zielordner für empfangene Dateien", "Receive folder"), 14, true));
        folder = new TextBox { Text = c.SuggestedReceiveDirectory, Margin = new Thickness(0, 8, 0, 10) }; folderStep.Children.Add(folder);
        var choose = Theme.Button(c.T("Ordner auswählen …", "Choose folder …"), () =>
        {
            var picker = new OpenFolderDialog { Title = c.T("Zielordner auswählen", "Choose receive folder") };
            if (Directory.Exists(folder.Text)) picker.InitialDirectory = folder.Text;
            if (picker.ShowDialog(this) == true) folder.Text = picker.FolderName;
        }); choose.HorizontalAlignment = HorizontalAlignment.Left; folderStep.Children.Add(choose);
        var help = Theme.Text(c.T("Empfangene Dateien liegen direkt hier, ohne zusätzliche AirLink-Ordner. Bereits vorhandene Dateien bleiben erhalten.", "Received files are saved directly here, without extra AirLink folders. Existing files are preserved."), 13, secondary: true); help.Margin = new Thickness(0, 16, 0, 16); folderStep.Children.Add(help);
        summary = Theme.Text("", 14, true); folderStep.Children.Add(summary); body.Children.Add(folderStep);
        reviewName = Theme.Text("", 17, true); reviewFolder = Theme.Text("", 13, secondary: true);
        var review = new StackPanel(); review.Children.Add(Theme.Text(c.T("Dieser Computer", "This computer"), 12, secondary: true)); review.Children.Add(reviewName);
        var folderCaption = Theme.Text(c.T("Empfangsordner", "Receive folder"), 12, secondary: true); folderCaption.Margin = new Thickness(0, 16, 0, 4); review.Children.Add(folderCaption); review.Children.Add(reviewFolder);
        reviewStep.Children.Add(Theme.Card(review));
        notifications = new CheckBox { Content = Theme.Text(c.T("Bei Anfragen und empfangenen Dateien benachrichtigen", "Notify about requests and received files"), 13), IsChecked = c.SetupComplete && c.Status?.Settings.Notifications == true, Margin = new Thickness(0, 18, 0, 8) };
        reviewStep.Children.Add(notifications);
        updates = new CheckBox { Content = Theme.Text(c.T("Über neue Betas informieren", "Notify me about new betas"), 13), IsChecked = c.Status?.Settings.AutomaticUpdateChecks == true, Margin = new Thickness(0, 8, 0, 8) }; reviewStep.Children.Add(updates);
        reviewStep.Children.Add(Theme.Text(c.T("Optional. Du kannst diese Einstellung später unter Allgemein ändern.", "Optional. You can change this later under General."), 12, secondary: true));
        var nextStep = Theme.Text(c.T("Als Nächstes: AirLink auf dem anderen Gerät öffnen und „Gerät hinzufügen“ wählen. Nach einmaligem Bestätigen bleibt die Verbindung gespeichert.", "Next: open AirLink on the other device and choose Add a device. After approving once, the connection stays saved."), 13, secondary: true); nextStep.Margin = new Thickness(0, 20, 0, 0); reviewStep.Children.Add(nextStep); body.Children.Add(reviewStep);
        error = Theme.Text("", 13); error.Foreground = Theme.Color(Theme.Dark ? "#FFB5AE" : "#B1372A"); error.Margin = new Thickness(0, 16, 0, 0); body.Children.Add(error);
        var actions = new Grid { Margin = new Thickness(0, 28, 0, 0) }; actions.ColumnDefinitions.Add(new ColumnDefinition()); actions.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        back = Theme.Button(c.T("Zurück", "Back"), () => { step = Math.Max(0, step - 1); error.Text = ""; Render(); }); actions.Children.Add(back); back.HorizontalAlignment = HorizontalAlignment.Left;
        next = Theme.Button("", () => _ = ContinueAsync(), true); next.IsDefault = true; Grid.SetColumn(next, 1); next.Margin = new Thickness(0); actions.Children.Add(next); body.Children.Add(actions);
        Content = new ScrollViewer { Content = body, VerticalScrollBarVisibility = ScrollBarVisibility.Auto, HorizontalScrollBarVisibility = ScrollBarVisibility.Disabled };
        Closing += (_, e) => { if (busy) e.Cancel = true; }; Render(); Loaded += (_, _) => { name.Focus(); name.SelectAll(); };
    }
    private void Render()
    {
        nameStep.Visibility = step == 0 ? Visibility.Visible : Visibility.Collapsed; folderStep.Visibility = step == 1 ? Visibility.Visible : Visibility.Collapsed; reviewStep.Visibility = step == 2 ? Visibility.Visible : Visibility.Collapsed;
        progress.Text = c.T("Einrichtung", "Setup") + " · " + (step + 1) + " / 3";
        title.Text = step switch { 0 => c.T("Willkommen bei AirLink", "Welcome to AirLink"), 1 => c.T("Alles an deinem Platz", "Everything in its place"), _ => c.T("Bereit zum Verbinden", "Ready to connect") };
        explanation.Text = step switch { 0 => c.T("Gib diesem Computer einen Namen, den du auf deinen anderen Geräten sofort erkennst.", "Give this computer a name you will recognize on your other devices."), 1 => c.T("Wähle, wo AirLink empfangene Dateien speichern soll.", "Choose where AirLink should save received files."), _ => c.T("Prüfe deine Auswahl. Erst beim Abschließen wird die Einrichtung gespeichert.", "Review your choices. Setup is saved only when you finish.") };
        summary.Text = name.Text.Trim();
        reviewName.Text = name.Text.Trim(); reviewFolder.Text = folder.Text.Trim();
        back.Visibility = step == 0 ? Visibility.Hidden : Visibility.Visible; next.Content = busy ? c.T("Wird gespeichert …", "Saving …") : step < 2 ? c.T("Weiter", "Continue") : c.T("Einrichtung abschließen", "Finish setup");
        back.IsEnabled = next.IsEnabled = nameStep.IsEnabled = folderStep.IsEnabled = reviewStep.IsEnabled = !busy;
    }
    private async Task ContinueAsync()
    {
        if (busy) return; error.Text = "";
        try
        {
            ValidateName(name.Text, c);
            if (step == 0) { step = 1; Render(); folder.Focus(); return; }
            if (!Path.IsPathFullyQualified(folder.Text.Trim())) throw new ArgumentException(c.T("Wähle einen vollständigen Zielordner.", "Choose an absolute receive folder."));
            if (step == 1) { step = 2; Render(); return; }
            busy = true; Render();
            await c.CompleteSetupAsync(name.Text.Trim(), folder.Text.Trim(), notifications.IsChecked == true, updates.IsChecked == true);
            if (!c.SetupComplete) throw new InvalidOperationException(c.T("Die Einrichtung konnte noch nicht bestätigt werden. Bitte erneut versuchen.", "Setup has not been confirmed. Please try again."));
            busy = false; DialogResult = true;
        }
        catch (Exception e) { error.Text = e.Message; }
        finally { busy = false; Render(); }
    }
    public static void ValidateName(string value, Controller c)
    {
        try { _ = SetupDefaults.NormalizeName(value); }
        catch (ArgumentException) { throw new ArgumentException(c.T("Gib einen Gerätenamen mit höchstens 120 UTF-8 Bytes ein.", "Enter a device name containing at most 120 UTF-8 bytes.")); }
    }
    private static string SuggestedName(Controller c)
    {
        var current = c.Status?.Settings.Name?.Trim() ?? "";
        if (current.StartsWith("LAPTOP-", StringComparison.OrdinalIgnoreCase)) return c.T("Laptop Windows", "Windows laptop");
        var generated = current.Length == 0 || current.StartsWith("DESKTOP-", StringComparison.OrdinalIgnoreCase) || current.EndsWith(".fritz.box", StringComparison.OrdinalIgnoreCase) || Regex.IsMatch(current, "^[0-9a-fA-F-]{20,}");
        return generated ? c.T("Windows-PC", "Windows PC") : current;
    }
}
