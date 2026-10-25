using System;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using Microsoft.Win32;

namespace AirLink.Windows;

internal static class Theme
{
    public static bool Dark { get; private set; }
    public static Brush Background => Color(Dark ? "#17191D" : "#F5F6F8");
    public static Brush Surface => Color(Dark ? "#23262C" : "#FFFFFF");
    public static Brush Foreground => Color(Dark ? "#F4F5F7" : "#20252E");
    public static Brush Secondary => Color(Dark ? "#AAB1BD" : "#626B78");
    public static Brush Line => Color(Dark ? "#3A3F48" : "#E0E4EA");
    public static Brush Accent => Color(Dark ? "#67A8FF" : "#1769D8");
    public static Brush SoftAccent => Color(Dark ? "#21344E" : "#EAF2FF");
    public static Brush Color(string value) => (SolidColorBrush)new BrushConverter().ConvertFromString(value)!;

    public static void Apply(string appearance)
    {
        bool systemDark = false;
        try { systemDark = (int?)Registry.GetValue(@"HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize", "AppsUseLightTheme", 1) == 0; }
        catch (Exception) { }
        Dark = appearance == "dark" || appearance == "system" && systemDark;
        var resources = System.Windows.Application.Current.Resources;
        resources["Surface"] = Surface; resources["Text"] = Foreground; resources["Secondary"] = Secondary;
        resources["Background"] = Background; resources["Line"] = Line; resources["Accent"] = Accent;
        var button = new Style(typeof(Button));
        button.Setters.Add(new Setter(Control.PaddingProperty, new Thickness(13, 8, 13, 8)));
        button.Setters.Add(new Setter(Control.MarginProperty, new Thickness(0, 0, 8, 0)));
        button.Setters.Add(new Setter(Control.FontSizeProperty, 13.0));
        button.Setters.Add(new Setter(Control.ForegroundProperty, Foreground));
        button.Setters.Add(new Setter(Control.BackgroundProperty, Surface));
        button.Setters.Add(new Setter(Control.BorderBrushProperty, Line));
        button.Setters.Add(new Setter(Control.CursorProperty, System.Windows.Input.Cursors.Hand));
        resources[typeof(Button)] = button;
        foreach (var kind in new[] { typeof(TextBox), typeof(ComboBox), typeof(ListBox) })
        {
            var style = new Style(kind);
            style.Setters.Add(new Setter(Control.ForegroundProperty, Foreground));
            style.Setters.Add(new Setter(Control.BackgroundProperty, Surface));
            style.Setters.Add(new Setter(Control.BorderBrushProperty, Line));
            style.Setters.Add(new Setter(Control.PaddingProperty, new Thickness(8)));
            resources[kind] = style;
        }
        var checkbox = new Style(typeof(CheckBox));
        checkbox.Setters.Add(new Setter(Control.ForegroundProperty, Foreground));
        checkbox.Setters.Add(new Setter(Control.MarginProperty, new Thickness(0, 8, 0, 8)));
        resources[typeof(CheckBox)] = checkbox;
    }

    public static TextBlock Text(string text, double size = 14, bool bold = false, bool secondary = false)
        => new() { Text = text, FontSize = size, FontWeight = bold ? FontWeights.SemiBold : FontWeights.Normal, Foreground = secondary ? Secondary : Foreground, TextWrapping = TextWrapping.Wrap };

    public static Border Card(UIElement child, int padding = 16) => new()
    {
        Child = child, Padding = new Thickness(padding), Background = Surface,
        BorderBrush = Line, BorderThickness = new Thickness(1), CornerRadius = new CornerRadius(12)
    };

    public static Button Button(string title, Action action, bool primary = false)
    {
        var button = new Button { Content = title };
        if (primary) { button.Background = Accent; button.Foreground = Brushes.White; button.BorderBrush = Accent; }
        button.Click += (_, _) => action();
        return button;
    }
}
