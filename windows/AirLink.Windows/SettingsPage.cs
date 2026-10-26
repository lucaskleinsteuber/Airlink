using System;
using System.Collections.Generic;
using System.IO;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using AirLink.Core;
using Microsoft.Win32;

namespace AirLink.Windows;

internal sealed class SettingsPageState
{
    public SettingsDraft? Draft;
    public int Category;
    public bool Saving;
    public string Notice = "";
    public bool Failed;
    public string SendLimit = "0", ReceiveLimit = "0";
    public bool LimitsValid => SettingsDraft.TryParseLimit(SendLimit, out _) && SettingsDraft.TryParseLimit(ReceiveLimit, out _);
    public bool IsDirty => Draft?.HasChanges == true || !LimitsValid;
    public void Reset(HostSettings settings)
    {
        if (Draft is null) Draft = new(settings); else Draft.Reset(settings);
        SendLimit = SettingsDraft.FormatLimit(settings.SendLimitBytesPerSecond);
        ReceiveLimit = SettingsDraft.FormatLimit(settings.ReceiveLimitBytesPerSecond);
    }
}

/// <summary>The page and its edit state survive navigation and ordinary status polls.</summary>
internal sealed class SettingsPage : Grid
{
    private readonly MainWindow owner;
    private readonly Controller c;
    private readonly SettingsPageState state;
    private readonly Action changed, reopenSetup;
    private readonly List<FrameworkElement> sections = [];
    private readonly List<Button> categories = [];
    private StackPanel categoryBar = new(), content = new();
    private TextBlock status = Theme.Text("", 12, secondary: true);
    private Button save = null!, discard = null!;
    private TextBox? nameInput, folderInput, sendInput, receiveInput;
    private ComboBox? appearanceInput, languageInput;
    private CheckBox? notificationsInput, duplicatesInput;
    private TextBlock? sortingHelp;
    private bool applying;

    public SettingsPage(MainWindow owner, Controller c, SettingsPageState state, Action changed, Action reopenSetup)
    {
        this.owner = owner; this.c = c; this.state = state; this.changed = changed; this.reopenSetup = reopenSetup;
        if (state.Draft is null && c.Status is not null) state.Reset(c.Status.Settings);
        Build();
    }
    public void Enter()
    {
        if (!state.Saving && !state.IsDirty && c.Status is not null) { state.Reset(c.Status.Settings); Build(); }
    }
    public void UpdateAvailability()
    {
        if (state.Draft is null && c.Status is not null) { state.Reset(c.Status.Settings); Build(); }
        else if (!state.Saving && c.Status is not null && state.Draft?.Rebase(c.Status.Settings) == true) ApplyRebasedValues();
        RefreshFooter();
    }
    private void ApplyRebasedValues()
    {
        var values = state.Draft!.Values;
        applying = true;
        try
        {
            if (nameInput is not null && nameInput.Text != values.Name) nameInput.Text = values.Name;
            if (folderInput is not null && folderInput.Text != values.ReceiveDirectory) folderInput.Text = values.ReceiveDirectory;
            if (appearanceInput is not null) appearanceInput.SelectedIndex = Math.Max(0, Array.IndexOf(new[] { "system", "light", "dark" }, values.Appearance));
            if (languageInput is not null) languageInput.SelectedIndex = Math.Max(0, Array.IndexOf(new[] { "system", "de", "en" }, values.Language));
            if (notificationsInput is not null) notificationsInput.IsChecked = values.Notifications;
            if (duplicatesInput is not null) duplicatesInput.IsChecked = values.Organization.DetectDuplicates;
            if (SettingsDraft.TryParseLimit(state.SendLimit, out var send) && send != values.SendLimitBytesPerSecond)
            {
                state.SendLimit = SettingsDraft.FormatLimit(values.SendLimitBytesPerSecond);
                if (sendInput is not null) sendInput.Text = state.SendLimit;
            }
            if (SettingsDraft.TryParseLimit(state.ReceiveLimit, out var receive) && receive != values.ReceiveLimitBytesPerSecond)
            {
                state.ReceiveLimit = SettingsDraft.FormatLimit(values.ReceiveLimitBytesPerSecond);
                if (receiveInput is not null) receiveInput.Text = state.ReceiveLimit;
            }
            if (sortingHelp is not null) sortingHelp.Text = SortingDescription(values.Organization.SortEnabled);
        }
        finally { applying = false; }
    }
    private void Build()
    {
        Children.Clear(); RowDefinitions.Clear(); sections.Clear(); categories.Clear();
        RowDefinitions.Add(new() { Height = GridLength.Auto }); RowDefinitions.Add(new()); RowDefinitions.Add(new() { Height = GridLength.Auto });
        var header = new StackPanel { Margin = new Thickness(34, 28, 34, 20) };
        header.Children.Add(Theme.Text(c.T("Einstellungen", "Settings"), 28, true));
        header.Children.Add(Theme.Text(c.T("Dein Computer, empfangene Dateien und Übertragungen.", "Your computer, received files and transfers."), 14, secondary: true)); Children.Add(header);
        var middle = new Grid { Margin = new Thickness(34, 0, 22, 0) };
        middle.ColumnDefinitions.Add(new() { Width = new GridLength(156) }); middle.ColumnDefinitions.Add(new()); SetRow(middle, 1); Children.Add(middle);
        categoryBar = new StackPanel { Margin = new Thickness(0, 0, 18, 0) }; middle.Children.Add(categoryBar);
        content = new StackPanel { Margin = new Thickness(0, 0, 12, 22) };
        var scroll = new ScrollViewer { Content = content, VerticalScrollBarVisibility = ScrollBarVisibility.Auto, HorizontalScrollBarVisibility = ScrollBarVisibility.Disabled };
        SetColumn(scroll, 1); middle.Children.Add(scroll);
        if (state.Draft is null) content.Children.Add(Theme.Text(c.T("AirLink wird gestartet …", "Starting AirLink …"), secondary: true));
        else
        {
            AddCategory(c.T("Allgemein", "General"), General());
            AddCategory(c.T("Empfangen", "Receiving"), Receiving());
            AddCategory(c.T("Übertragung", "Transfers"), Transfers());
            AddCategory(c.T("Info", "About"), About());
            SelectCategory(Math.Clamp(state.Category, 0, sections.Count - 1));
        }
        var footer = new Grid { Margin = new Thickness(34, 16, 34, 22) };
        footer.ColumnDefinitions.Add(new()); footer.ColumnDefinitions.Add(new() { Width = GridLength.Auto }); footer.ColumnDefinitions.Add(new() { Width = GridLength.Auto });
        status = Theme.Text("", 12, secondary: true); status.VerticalAlignment = VerticalAlignment.Center; status.Margin = new Thickness(0, 0, 12, 0); footer.Children.Add(status);
        discard = Theme.Button(c.T("Verwerfen", "Discard"), () =>
        {
            if (state.Saving || c.Status is null) return;
            state.Reset(c.Status.Settings); state.Failed = false; state.Notice = c.T("Änderungen verworfen", "Changes discarded"); Build();
        }); SetColumn(discard, 1); footer.Children.Add(discard);
        save = Theme.Button(c.T("Speichern", "Save"), () => _ = SaveAsync(), true); save.Margin = new Thickness(0); SetColumn(save, 2); footer.Children.Add(save);
        SetRow(footer, 2); Children.Add(footer); RefreshFooter();
    }
    private void AddCategory(string name, FrameworkElement view)
    {
        var index = sections.Count; sections.Add(view); content.Children.Add(view);
        var button = Theme.Button(name, () => SelectCategory(index));
        button.HorizontalContentAlignment = HorizontalAlignment.Left; button.Margin = new Thickness(0, 0, 0, 6); button.BorderBrush = Brushes.Transparent;
        categories.Add(button); categoryBar.Children.Add(button);
    }
    private void SelectCategory(int index)
    {
        state.Category = index;
        for (var i = 0; i < sections.Count; i++) { sections[i].Visibility = i == index ? Visibility.Visible : Visibility.Collapsed; categories[i].Background = i == index ? Theme.SoftAccent : Brushes.Transparent; categories[i].FontWeight = i == index ? FontWeights.SemiBold : FontWeights.Normal; }
    }
    private static StackPanel Section(string title, string description)
    {
        var panel = new StackPanel(); panel.Children.Add(Theme.Text(title, 20, true));
        var help = Theme.Text(description, 13, secondary: true); help.Margin = new Thickness(0, 5, 0, 20); panel.Children.Add(help); return panel;
    }
    private TextBox Input(StackPanel panel, string label, string text, Action<string> update)
    {
        var caption = Theme.Text(label, 13, true); caption.Margin = new Thickness(0, 12, 0, 6); panel.Children.Add(caption);
        var field = new TextBox { Text = text }; field.TextChanged += (_, _) => { if (!applying) { update(field.Text); Edited(); } }; panel.Children.Add(field); return field;
    }
    private CheckBox Toggle(StackPanel panel, string title, bool value, Action<bool> update)
    {
        var box = new CheckBox { Content = Theme.Text(title, 13), IsChecked = value, Margin = new Thickness(0, 12, 0, 8) };
        box.Checked += (_, _) => { if (!applying) { update(true); Edited(); } }; box.Unchecked += (_, _) => { if (!applying) { update(false); Edited(); } }; panel.Children.Add(box); return box;
    }
    private ComboBox Choice(StackPanel panel, string title, string[] labels, string[] values, string value, Action<string> update)
    {
        var caption = Theme.Text(title, 13, true); caption.Margin = new Thickness(0, 16, 0, 6); panel.Children.Add(caption);
        var choice = new ComboBox { ItemsSource = labels, SelectedIndex = Math.Max(0, Array.IndexOf(values, value)) };
        choice.SelectionChanged += (_, _) => { if (!applying && choice.SelectedIndex >= 0) { update(values[choice.SelectedIndex]); Edited(); } }; panel.Children.Add(choice); return choice;
    }
    private FrameworkElement General()
    {
        var values = state.Draft!.Values;
        var panel = Section(c.T("Allgemein", "General"), c.T("So erscheint AirLink auf diesem Computer.", "How AirLink appears on this computer."));
        nameInput = Input(panel, c.T("Gerätename", "Device name"), values.Name, value => state.Draft!.Values.Name = value); nameInput.MaxLength = 120;
        panel.Children.Add(Theme.Text(c.T("Diesen Namen sehen deine anderen Geräte.", "Your other devices see this name."), 12, secondary: true));
        appearanceInput = Choice(panel, c.T("Darstellung", "Appearance"), [c.T("System", "System"), c.T("Hell", "Light"), c.T("Dunkel", "Dark")], ["system", "light", "dark"], values.Appearance, value => state.Draft!.Values.Appearance = value);
        languageInput = Choice(panel, c.T("Sprache", "Language"), [c.T("System", "System"), "Deutsch", "English"], ["system", "de", "en"], values.Language, value => state.Draft!.Values.Language = value);
        notificationsInput = Toggle(panel, c.T("Bei Anfragen und empfangenen Dateien benachrichtigen", "Notify about requests and received files"), values.Notifications, value => state.Draft!.Values.Notifications = value);
        panel.Children.Add(Theme.Text(c.T("Beim Schließen läuft AirLink im Infobereich weiter. Mit „Beenden“ werden Übertragungen gesichert.", "Closing the window keeps AirLink running in the notification area. Quit checkpoints transfers."), 12, secondary: true));
        return panel;
    }
    private FrameworkElement Receiving()
    {
        var values = state.Draft!.Values;
        var panel = Section(c.T("Empfangen", "Receiving"), c.T("Lege fest, wo neue Dateien ankommen.", "Choose where new files arrive."));
        var folder = Input(panel, c.T("Empfangsordner", "Receive folder"), values.ReceiveDirectory, value => state.Draft!.Values.ReceiveDirectory = value); folderInput = folder;
        var choose = Theme.Button(c.T("Ordner auswählen …", "Choose folder …"), () =>
        {
            var picker = new OpenFolderDialog { Title = c.T("Empfangsordner wählen", "Choose receive folder") };
            if (Directory.Exists(folder.Text)) picker.InitialDirectory = folder.Text;
            if (picker.ShowDialog(owner) == true) folder.Text = picker.FolderName;
        }); choose.HorizontalAlignment = HorizontalAlignment.Left; choose.Margin = new Thickness(0, 10, 0, 12); panel.Children.Add(choose);
        panel.Children.Add(Theme.Text(c.T("Neue Dateien landen direkt hier. Gleiche Namen erhalten eine Nummer. Bereits laufende und frühere Übertragungen behalten ihren Speicherort.", "New files arrive directly here. Duplicate names receive a number. Active and earlier transfers keep their saved location."), 12, secondary: true));
        duplicatesInput = Toggle(panel, c.T("Identische Inhalte beim Empfang erkennen", "Detect identical content when receiving"), values.Organization.DetectDuplicates, value => state.Draft!.Values.Organization.DetectDuplicates = value);
        panel.Children.Add(Theme.Text(c.T("AirLink fragt, bevor eine identische Datei übersprungen wird. Vorhandene Dateien bleiben erhalten.", "AirLink asks before skipping identical files. Existing files are preserved."), 12, secondary: true));
        sortingHelp = Theme.Text(SortingDescription(values.Organization.SortEnabled), 12, secondary: true); sortingHelp.Margin = new Thickness(0, 16, 0, 16); panel.Children.Add(sortingHelp);
        var setup = Theme.Button(c.T("Einrichtung erneut öffnen …", "Open setup again …"), () =>
        {
            if (state.IsDirty && MessageBox.Show(owner, c.T("Ungespeicherte Änderungen verwerfen und die Einrichtung öffnen?", "Discard unsaved changes and open setup?"), c.T("Einrichtung öffnen", "Open setup"), MessageBoxButton.YesNo, MessageBoxImage.Question) != MessageBoxResult.Yes) return;
            if (c.Status is not null) state.Reset(c.Status.Settings);
            reopenSetup();
            if (c.Status is not null) state.Reset(c.Status.Settings);
            state.Notice = ""; Build();
        }); setup.HorizontalAlignment = HorizontalAlignment.Left; panel.Children.Add(setup);
        return panel;
    }
    private FrameworkElement Transfers()
    {
        var panel = Section(c.T("Übertragung", "Transfers"), c.T("Ohne Limit nutzt AirLink die verfügbare Verbindung.", "Without limits, AirLink uses the available connection."));
        sendInput = Input(panel, c.T("Senden begrenzen · MB/s", "Upload limit · MB/s"), state.SendLimit, value => { state.SendLimit = value; if (SettingsDraft.TryParseLimit(value, out var limit)) state.Draft!.Values.SendLimitBytesPerSecond = limit; });
        receiveInput = Input(panel, c.T("Empfangen begrenzen · MB/s", "Download limit · MB/s"), state.ReceiveLimit, value => { state.ReceiveLimit = value; if (SettingsDraft.TryParseLimit(value, out var limit)) state.Draft!.Values.ReceiveLimitBytesPerSecond = limit; });
        panel.Children.Add(Theme.Text(c.T("0 bedeutet unbegrenzt. Limits gelten gemeinsam für Dateien, Freigaben und Browserübertragungen pro Richtung.", "0 means unlimited. Each directional limit is shared by files, shares and browser transfers."), 12, secondary: true));
        var note = Theme.Text(c.T("Pausierte Übertragungen setzen beim Fortsetzen an bestätigten Blöcken fort. Die Geschwindigkeit hängt auch von WLAN und Datenträgern beider Geräte ab.", "Paused transfers resume from confirmed chunks. Speed also depends on Wi-Fi and storage on both devices."), 12, secondary: true); note.Margin = new Thickness(0, 22, 0, 0); panel.Children.Add(note);
        return panel;
    }
    private string SortingDescription(bool enabled) => enabled
        ? c.T("Deine Sortierregeln sind aktiv und bleiben erhalten. Den Regeleditor findest du in der Weboberfläche.", "Your sorting rules are active and preserved. The rule editor is available in the web interface.")
        : c.T("Automatische Sortierung ist ausgeschaltet. Sortierregeln können in der Weboberfläche eingerichtet werden.", "Automatic sorting is off. Sorting rules can be configured in the web interface.");
    private FrameworkElement About()
    {
        var panel = Section("AirLink " + c.Version, c.T("Dateien im lokalen Netzwerk teilen.", "Share files on your local network."));
        panel.Children.Add(Theme.Text(c.T("Private Entwicklungsversion", "Private development build"), 16, true));
        panel.Children.Add(Theme.Text(c.T("Neue Versionen lädst du angemeldet aus den privaten GitHub-Releases herunter und installierst sie selbst.", "Download new versions from the private GitHub releases while signed in, then install them manually."), 13, secondary: true));
        var releases = Theme.Button(c.T("Downloads auf GitHub öffnen", "Open GitHub downloads"), () => _ = c.RunAsync(() => { Shell.OpenUrl("https://github.com/lucasfd3/Airlink/releases"); return System.Threading.Tasks.Task.CompletedTask; })); releases.HorizontalAlignment = HorizontalAlignment.Left; releases.Margin = new Thickness(0, 16, 0, 8); panel.Children.Add(releases);
        var check = Theme.Button(c.T("Nach Updates suchen …", "Check for updates …"), () => _ = owner.CheckForUpdatesAsync()); check.HorizontalAlignment = HorizontalAlignment.Left; panel.Children.Add(check);
        panel.Children.Add(Theme.Text(c.T("Die Prüfung startet nur auf Anfrage. Der private GitHub-Feed kann ohne Anmeldung von der App nicht abgerufen werden.", "The check starts only on request. The app cannot access the private GitHub feed without authentication."), 12, secondary: true));
        var licenses = Theme.Button(c.T("Lizenzen", "Licenses"), () => NativeLicenses.Show(owner, c)); licenses.HorizontalAlignment = HorizontalAlignment.Left; licenses.Margin = new Thickness(0, 20, 0, 0); panel.Children.Add(licenses);
        return panel;
    }
    private void Edited() { state.Notice = ""; state.Failed = false; RefreshFooter(); }
    private void RefreshFooter()
    {
        if (save is null) return;
        content.IsEnabled = !state.Saving; categoryBar.IsEnabled = !state.Saving;
        for (var i = 0; i < sections.Count; i++) sections[i].IsEnabled = !state.Saving && (i == 3 || c.Ready);
        save.IsEnabled = c.Ready && !state.Saving && state.Draft is not null && state.IsDirty && state.LimitsValid;
        discard.IsEnabled = c.Ready && !state.Saving && state.IsDirty;
        save.Content = state.Saving ? c.T("Wird gespeichert …", "Saving …") : c.T("Speichern", "Save");
        status.Text = state.Saving ? c.T("Änderungen werden gespeichert …", "Saving changes …") : !c.Ready ? c.T("Warte auf die Verbindung zum Dienst.", "Waiting for the local service.") : state.Notice.Length > 0 ? state.Notice : !state.LimitsValid ? c.T("Bitte gültige Limits eingeben. 0 bedeutet unbegrenzt.", "Enter valid limits. 0 means unlimited.") : state.IsDirty ? c.T("Ungespeicherte Änderungen", "Unsaved changes") : c.T("Alles gespeichert", "All changes saved");
        status.Foreground = state.Failed || !state.LimitsValid ? Theme.Color(Theme.Dark ? "#FFB5AE" : "#B1372A") : Theme.Secondary;
    }
    private async System.Threading.Tasks.Task SaveAsync()
    {
        if (state.Saving || state.Draft is null || !c.Ready || !state.LimitsValid) return;
        state.Saving = true; state.Notice = ""; state.Failed = false; RefreshFooter();
        try
        {
            SetupWindow.ValidateName(state.Draft.Values.Name, c);
            var merged = state.Draft.Merge(c.Status!.Settings);
            if (!Path.IsPathFullyQualified(merged.ReceiveDirectory)) throw new ArgumentException(c.T("Bitte einen vollständigen Empfangsordner wählen.", "Choose an absolute receive folder."));
            var saved = await c.SaveSettingsAsync(merged);
            state.Reset(saved); state.Notice = c.T("Gespeichert", "Saved");
        }
        catch (Exception error) { state.Failed = true; state.Notice = error.Message; }
        finally { state.Saving = false; Build(); changed(); }
    }
}
