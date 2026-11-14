using System;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;

namespace AirLink.Windows;

/// <summary>Windows 11 Fluent look: system accent, light/dark and Mica come
/// from WPF's built-in Fluent theme; symbols from Segoe Fluent Icons.</summary>
internal static class Theme
{
    public static readonly FontFamily Icons = new("Segoe Fluent Icons, Segoe MDL2 Assets");
    public const string Send = "", History = "", Help = "", Settings = "", Add = "", Laptop = "", Desktop = "",
        Search = "", Pause = "", Play = "", Close = "", Check = "", Folder = "", Document = "", Paste = "",
        Download = "", Upload = "", Delete = "", Photo = "", Video = "", Music = "", Archive = "", Link = "";

    public static void Apply(Application application)
    {
#pragma warning disable WPF0001
        application.ThemeMode = ThemeMode.System;
#pragma warning restore WPF0001
    }

    public static T Brush<T>(this T element, DependencyProperty property, string key) where T : FrameworkElement
    {
        element.SetResourceReference(property, key);
        return element;
    }

    public static TextBlock Text(string text, double size = 14, bool bold = false, bool secondary = false)
    {
        var block = new TextBlock { Text = text, FontSize = size, FontWeight = bold ? FontWeights.SemiBold : FontWeights.Normal, TextWrapping = TextWrapping.Wrap };
        block.SetResourceReference(TextBlock.ForegroundProperty, secondary ? "TextFillColorSecondaryBrush" : "TextFillColorPrimaryBrush");
        return block;
    }

    public static TextBlock Glyph(string glyph, double size, string brush = "TextFillColorPrimaryBrush")
    {
        var block = new TextBlock { Text = glyph, FontFamily = Icons, FontSize = size, HorizontalAlignment = HorizontalAlignment.Center, VerticalAlignment = VerticalAlignment.Center };
        block.SetResourceReference(TextBlock.ForegroundProperty, brush);
        return block;
    }

    public static Button Button(string title, Action action, bool accent = false, string? glyph = null)
    {
        var button = new Button { Padding = new Thickness(14, 6, 14, 6), Margin = new Thickness(4, 0, 0, 0) };
        if (glyph is null) button.Content = title;
        else
        {
            var row = new StackPanel { Orientation = Orientation.Horizontal };
            row.Children.Add(new TextBlock { Text = glyph, FontFamily = Icons, FontSize = 15, VerticalAlignment = VerticalAlignment.Center, Margin = new Thickness(0, 0, 8, 0) });
            row.Children.Add(new TextBlock { Text = title, VerticalAlignment = VerticalAlignment.Center });
            button.Content = row;
        }
        if (accent) button.SetResourceReference(FrameworkElement.StyleProperty, "AccentButtonStyle");
        button.Click += (_, _) => action();
        return button;
    }

    /// <summary>A borderless button with only a symbol, for rows and toolbars.</summary>
    public static Button IconButton(string glyph, string tooltip, Action action)
    {
        var button = new Button { Content = new TextBlock { Text = glyph, FontFamily = Icons, FontSize = 14 }, ToolTip = tooltip, Padding = new Thickness(8), Margin = new Thickness(2, 0, 0, 0), Background = Brushes.Transparent, BorderThickness = new Thickness(0) };
        System.Windows.Automation.AutomationProperties.SetName(button, tooltip);
        button.Click += (_, _) => action();
        return button;
    }

    public static Border Card(UIElement child, Thickness? padding = null)
    {
        var border = new Border { Child = child, CornerRadius = new CornerRadius(8), BorderThickness = new Thickness(1), Padding = padding ?? new Thickness(0) };
        border.SetResourceReference(Border.BackgroundProperty, "CardBackgroundFillColorDefaultBrush");
        border.SetResourceReference(Border.BorderBrushProperty, "CardStrokeColorDefaultBrush");
        return border;
    }

    public static string FileGlyph(Core.Transfer transfer)
    {
        if (transfer.Files.Count == 0) return Document;
        var first = transfer.Files[0];
        if (first.Directory) return Folder;
        return System.IO.Path.GetExtension(first.Path).ToLowerInvariant() switch
        {
            ".jpg" or ".jpeg" or ".png" or ".heic" or ".gif" or ".webp" or ".bmp" or ".tif" or ".tiff" or ".raw" => Photo,
            ".mov" or ".mp4" or ".mkv" or ".avi" or ".webm" or ".m4v" => Video,
            ".mp3" or ".m4a" or ".wav" or ".flac" or ".aac" or ".ogg" => Music,
            ".zip" or ".7z" or ".rar" or ".tar" or ".gz" or ".xz" or ".zst" or ".dmg" or ".iso" => Archive,
            _ => Document
        };
    }
}
