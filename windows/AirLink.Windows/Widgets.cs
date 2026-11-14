using System;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Media;
using System.Windows.Media.Animation;
using System.Windows.Shapes;
using AirLink.Core;

namespace AirLink.Windows;

/// <summary>A computer in the AirDrop-style grid: drop files on it or click it.</summary>
internal sealed class DeviceTile : StackPanel
{
    public enum Kind { Paired, Nearby, Add, Searching }
    public Kind TileKind { get; }
    public event Action? Activated;
    public event Action<string[]>? FilesDropped;
    private readonly double size;
    private readonly Ellipse circle = new();
    private readonly TextBlock glyph;
    private readonly Path ring = new() { StrokeThickness = 4, StrokeStartLineCap = PenLineCap.Round, StrokeEndLineCap = PenLineCap.Round, IsHitTestVisible = false };
    private readonly Ellipse track = new() { StrokeThickness = 4, IsHitTestVisible = false, Opacity = 0.25 };
    private readonly RotateTransform spin = new();
    private readonly TextBlock name = Theme.Text("", 14, true), status = Theme.Text("", 12, secondary: true);
    private readonly ScaleTransform scale = new(1, 1);
    private bool reachable = true, busy;
    private double? progress;

    public DeviceTile(Kind kind, double diameter = 92)
    {
        TileKind = kind; size = diameter;
        Width = diameter + 54; Margin = new Thickness(0, 0, 12, 18); Focusable = true; Cursor = Cursors.Hand; Background = Brushes.Transparent;
        var host = new Grid { Width = diameter + 14, Height = diameter + 14, HorizontalAlignment = HorizontalAlignment.Center, RenderTransformOrigin = new Point(0.5, 0.5), RenderTransform = scale };
        circle.Width = circle.Height = diameter;
        circle.StrokeThickness = kind == Kind.Paired ? 1 : 1.5;
        if (kind == Kind.Paired)
        {
            circle.SetResourceReference(Shape.FillProperty, "CardBackgroundFillColorDefaultBrush");
            circle.SetResourceReference(Shape.StrokeProperty, "CardStrokeColorDefaultBrush");
        }
        else
        {
            circle.StrokeDashArray = [3, 2.5];
            circle.SetResourceReference(Shape.FillProperty, "SubtleFillColorSecondaryBrush");
            circle.SetResourceReference(Shape.StrokeProperty, "TextFillColorTertiaryBrush");
        }
        glyph = Theme.Glyph("", diameter * 0.40, kind == Kind.Paired ? "AccentTextFillColorPrimaryBrush" : "TextFillColorSecondaryBrush");
        track.Width = track.Height = diameter + 12;
        track.SetResourceReference(Shape.StrokeProperty, "AccentFillColorDefaultBrush");
        ring.SetResourceReference(Shape.StrokeProperty, "AccentFillColorDefaultBrush");
        ring.RenderTransformOrigin = new Point(0.5, 0.5); ring.RenderTransform = spin;
        host.Children.Add(circle); host.Children.Add(glyph); host.Children.Add(track); host.Children.Add(ring);
        Children.Add(host);
        name.TextAlignment = TextAlignment.Center; name.Margin = new Thickness(0, 8, 0, 0); name.MaxHeight = 40; name.TextTrimming = TextTrimming.CharacterEllipsis;
        status.TextAlignment = TextAlignment.Center; status.TextWrapping = TextWrapping.NoWrap; status.TextTrimming = TextTrimming.CharacterEllipsis;
        Children.Add(name); Children.Add(status);
        MouseLeftButtonUp += (_, e) => { Activated?.Invoke(); e.Handled = true; };
        KeyDown += (_, e) => { if (e.Key is Key.Enter or Key.Space) { Activated?.Invoke(); e.Handled = true; } };
        MouseEnter += (_, _) => Hover(1.04); MouseLeave += (_, _) => Hover(1);
        if (kind == Kind.Paired)
        {
            AllowDrop = true;
            DragEnter += (_, e) => { var ok = e.Data.GetDataPresent(DataFormats.FileDrop); e.Effects = ok ? DragDropEffects.Copy : DragDropEffects.None; if (ok) Target(true); e.Handled = true; };
            DragOver += (_, e) => { e.Effects = e.Data.GetDataPresent(DataFormats.FileDrop) ? DragDropEffects.Copy : DragDropEffects.None; e.Handled = true; };
            DragLeave += (_, _) => Target(false);
            Drop += (_, e) => { Target(false); if (e.Data.GetData(DataFormats.FileDrop) is string[] paths && paths.Length > 0) FilesDropped?.Invoke(paths); e.Handled = true; };
        }
    }

    public void SetContent(string symbol, string title, string state, double? fraction, bool working, bool enabled)
    {
        glyph.Text = symbol; name.Text = title; status.Text = state;
        status.SetResourceReference(TextBlock.ForegroundProperty, working ? "AccentTextFillColorPrimaryBrush" : "TextFillColorSecondaryBrush");
        reachable = enabled; Opacity = enabled ? 1 : 0.6;
        System.Windows.Automation.AutomationProperties.SetName(this, title + ", " + state);
        if (progress == fraction && busy == working) return;
        progress = fraction; busy = working;
        track.Visibility = fraction is null ? Visibility.Collapsed : Visibility.Visible;
        spin.BeginAnimation(RotateTransform.AngleProperty, null);
        if (fraction is { } value) ring.Data = Arc(Math.Max(0.02, value));
        else if (working)
        {
            ring.Data = Arc(0.22);
            spin.BeginAnimation(RotateTransform.AngleProperty, new DoubleAnimation(0, 360, TimeSpan.FromSeconds(1)) { RepeatBehavior = RepeatBehavior.Forever });
        }
        else ring.Data = null;
    }

    private Geometry Arc(double fraction)
    {
        var radius = (size + 12) / 2; var center = new Point(radius + 1, radius + 1);
        var angle = Math.Min(359.9, fraction * 360) * Math.PI / 180;
        var start = new Point(center.X, center.Y - radius);
        var end = new Point(center.X + radius * Math.Sin(angle), center.Y - radius * Math.Cos(angle));
        var figure = new PathFigure { StartPoint = start, IsClosed = false };
        figure.Segments.Add(new ArcSegment(end, new Size(radius, radius), 0, fraction > 0.5, SweepDirection.Clockwise, true));
        var geometry = new PathGeometry([figure]);
        geometry.Freeze();
        return geometry;
    }

    private void Hover(double value)
    {
        var animation = new DoubleAnimation(value, TimeSpan.FromMilliseconds(140));
        scale.BeginAnimation(ScaleTransform.ScaleXProperty, animation);
        scale.BeginAnimation(ScaleTransform.ScaleYProperty, animation);
    }
    private void Target(bool on)
    {
        if (on) circle.SetResourceReference(Shape.StrokeProperty, "AccentFillColorDefaultBrush");
        else circle.SetResourceReference(Shape.StrokeProperty, "CardStrokeColorDefaultBrush");
        circle.StrokeThickness = on ? 3 : 1;
        Hover(on ? 1.1 : 1);
    }
}

/// <summary>One transfer in "Recent" and in the history.</summary>
internal sealed class TransferRow : Grid
{
    public event Action<string, string>? ActionRequested; // kind, transfer id
    private readonly Controller c;
    private readonly TextBlock icon = Theme.Glyph("", 24, "TextFillColorSecondaryBrush"), badge = Theme.Glyph("", 11, "TextOnAccentFillColorPrimaryBrush");
    private readonly Border badgeBack = new() { Width = 16, Height = 16, CornerRadius = new CornerRadius(8), HorizontalAlignment = HorizontalAlignment.Right, VerticalAlignment = VerticalAlignment.Bottom, Margin = new Thickness(0, 0, -4, -2) };
    private readonly TextBlock title = Theme.Text("", 14, true), detail = Theme.Text("", 12, secondary: true);
    private readonly ProgressBar bar = new() { Height = 4, Minimum = 0, Maximum = 1, Margin = new Thickness(0, 4, 0, 4) };
    private readonly StackPanel actions = new() { Orientation = Orientation.Horizontal, VerticalAlignment = VerticalAlignment.Center };
    public string Id { get; private set; } = "";

    public TransferRow(Controller controller)
    {
        c = controller;
        Margin = new Thickness(14, 9, 10, 9);
        ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(46) });
        ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var picture = new Grid { Width = 32, Height = 32, VerticalAlignment = VerticalAlignment.Center, HorizontalAlignment = HorizontalAlignment.Left };
        picture.Children.Add(icon); badgeBack.Child = badge; picture.Children.Add(badgeBack);
        Children.Add(picture);
        title.TextWrapping = TextWrapping.NoWrap; title.TextTrimming = TextTrimming.CharacterEllipsis;
        var texts = new StackPanel { VerticalAlignment = VerticalAlignment.Center };
        texts.Children.Add(title); texts.Children.Add(bar); texts.Children.Add(detail);
        SetColumn(texts, 1); Children.Add(texts);
        SetColumn(actions, 2); Children.Add(actions);
    }

    public void Update(Transfer t)
    {
        Id = t.Id;
        var send = t.Direction == "send";
        icon.Text = Theme.FileGlyph(t);
        badge.Text = send ? Theme.Upload : Theme.Download;
        badgeBack.SetResourceReference(Border.BackgroundProperty, send ? "AccentFillColorDefaultBrush" : "SystemFillColorSuccessBrush");
        title.Text = DeviceExperience.Describe(t, c.English);
        var percent = (int)Math.Floor(t.Progress * 100);
        var peer = send ? c.T("An ", "To ") + t.DeviceName : c.T("Von ", "From ") + t.DeviceName;
        detail.SetResourceReference(TextBlock.ForegroundProperty, t.State == "failed" ? "SystemFillColorCriticalBrush" : "TextFillColorSecondaryBrush");
        detail.Text = t.State switch
        {
            "running" => peer + $" · {percent} % · " + c.Bytes((long)t.BytesPerSecond) + "/s" + (DesktopTransferSelection.Remaining(t) is { } left ? c.T(" · noch ", " · ") + Remaining(left) + c.T("", " left") : ""),
            "paused" => peer + " · " + c.T($"Angehalten bei {percent} %", $"Paused at {percent}%"),
            "offered" => send ? peer + " · " + c.T($"Wartet, bis {t.DeviceName} annimmt", $"Waiting for {t.DeviceName} to accept") : peer + " · " + c.Bytes(t.TotalBytes),
            "queued" or "reconnecting" or "verifying" => peer + " · " + c.T($"Wartet auf {t.DeviceName} … ({percent} %)", $"Waiting for {t.DeviceName} … ({percent}%)"),
            "failed" => t.Problem is { } problem ? c.T(problem.TitleDE + " " + problem.RecoveryDE, problem.TitleEN + " " + problem.RecoveryEN) : peer + " · " + c.T("Fehlgeschlagen", "Failed") + (string.IsNullOrEmpty(t.Error) ? "" : ": " + t.Error),
            "cancelled" => peer + " · " + c.T("Abgebrochen", "Cancelled"),
            _ => peer + " · " + c.Bytes(t.TotalBytes) + " · " + DeviceExperience.RelativeTime(t.UpdatedAt, c.English)
        };
        bar.Visibility = t.State == "running" ? Visibility.Visible : Visibility.Collapsed;
        bar.Value = t.Progress;
        actions.Children.Clear();
        switch (t.State)
        {
            case "running" or "queued" or "reconnecting" or "verifying":
                actions.Children.Add(Theme.IconButton(Theme.Pause, c.T("Anhalten", "Pause"), () => ActionRequested?.Invoke("pause", Id)));
                actions.Children.Add(Theme.IconButton(Theme.Close, c.T("Abbrechen", "Cancel"), () => ActionRequested?.Invoke("cancel", Id)));
                break;
            case "paused":
                actions.Children.Add(Theme.IconButton(Theme.Play, c.T("Fortsetzen", "Resume"), () => ActionRequested?.Invoke("resume", Id)));
                actions.Children.Add(Theme.IconButton(Theme.Close, c.T("Abbrechen", "Cancel"), () => ActionRequested?.Invoke("cancel", Id)));
                break;
            case "offered":
                if (send) actions.Children.Add(Theme.IconButton(Theme.Close, c.T("Abbrechen", "Cancel"), () => ActionRequested?.Invoke("cancel", Id)));
                else actions.Children.Add(Theme.Button(c.T("Annehmen", "Accept"), () => ActionRequested?.Invoke("accept", Id), accent: true));
                break;
            case "completed":
                if (!send && t.HasReceivedContent) actions.Children.Add(Theme.Button(c.T("Zeigen", "Show"), () => ActionRequested?.Invoke("reveal", Id)));
                else actions.Children.Add(Theme.Glyph(Theme.Check, 16, "SystemFillColorSuccessBrush"));
                break;
            case "failed":
                if (t.Retryable) actions.Children.Add(Theme.Button(c.T("Erneut versuchen", "Try again"), () => ActionRequested?.Invoke("retry", Id)));
                break;
        }
        System.Windows.Automation.AutomationProperties.SetName(this, title.Text + ", " + detail.Text);
    }

    private string Remaining(TimeSpan left) => left.TotalSeconds < 60 ? $"{(int)left.TotalSeconds} s" : left.TotalHours < 1 ? $"{(int)left.TotalMinutes} min" : $"{(int)left.TotalHours} h {left.Minutes} min";
}

/// <summary>A notice above the computers, like an InfoBar.</summary>
internal sealed class Banner : Border
{
    public StackPanel Actions { get; } = new() { Orientation = Orientation.Horizontal, VerticalAlignment = VerticalAlignment.Center };
    public Banner(string glyph, string heading, string text)
    {
        CornerRadius = new CornerRadius(8); BorderThickness = new Thickness(1); Padding = new Thickness(16, 12, 12, 12); Margin = new Thickness(0, 0, 0, 10);
        SetResourceReference(BackgroundProperty, "CardBackgroundFillColorDefaultBrush");
        SetResourceReference(BorderBrushProperty, "AccentFillColorDefaultBrush");
        var grid = new Grid();
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(40) });
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        grid.Children.Add(Theme.Glyph(glyph, 22, "AccentTextFillColorPrimaryBrush"));
        var texts = new StackPanel { VerticalAlignment = VerticalAlignment.Center };
        texts.Children.Add(Theme.Text(heading, 14, true));
        if (text.Length > 0) { var body = Theme.Text(text, 13, secondary: true); body.MaxHeight = 40; body.TextTrimming = TextTrimming.CharacterEllipsis; texts.Children.Add(body); }
        Grid.SetColumn(texts, 1); grid.Children.Add(texts);
        Grid.SetColumn(Actions, 2); grid.Children.Add(Actions);
        Child = grid;
    }
}
