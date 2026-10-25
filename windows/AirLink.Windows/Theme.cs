using System;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using Microsoft.Win32;

namespace AirLink.Windows;

internal static class Theme
{
    public static bool Dark { get; private set; }
    public static Brush Background => Color(Dark ? "#17191D" : "#FFFFFF");
    public static Brush Sidebar => Color(Dark ? "#202227" : "#F4F5F7");
    public static Brush Surface => Color(Dark ? "#25282E" : "#F8F9FB");
    public static Brush Foreground => Color(Dark ? "#F5F6F8" : "#20242C");
    public static Brush Secondary => Color(Dark ? "#ADB4BF" : "#687180");
    public static Brush Line => Color(Dark ? "#363B44" : "#E5E8ED");
    public static Brush Accent => Color(Dark ? "#79B1FF" : "#0876E8");
    public static Brush SoftAccent => Color(Dark ? "#263B57" : "#E5F0FF");
    public static Brush Color(string value) => (SolidColorBrush)new BrushConverter().ConvertFromString(value)!;
    private static System.Windows.Data.Binding Parent(string property) => new(property) { RelativeSource = new System.Windows.Data.RelativeSource(System.Windows.Data.RelativeSourceMode.TemplatedParent) };

    public static void Apply(string appearance)
    {
        bool systemDark = false;
        try { systemDark = (int?)Registry.GetValue(@"HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize", "AppsUseLightTheme", 1) == 0; }
        catch (Exception) { }
        Dark = appearance == "dark" || appearance == "system" && systemDark;
        var resources = Application.Current.Resources;
        var button = new Style(typeof(Button));
        button.Setters.Add(new Setter(Control.PaddingProperty, new Thickness(15, 9, 15, 9)));
        button.Setters.Add(new Setter(Control.MarginProperty, new Thickness(0, 0, 8, 0)));
        button.Setters.Add(new Setter(Control.MinHeightProperty, 38.0));
        button.Setters.Add(new Setter(Control.FontSizeProperty, 14.0));
        button.Setters.Add(new Setter(Control.ForegroundProperty, Foreground));
        button.Setters.Add(new Setter(Control.BackgroundProperty, Surface));
        button.Setters.Add(new Setter(Control.BorderBrushProperty, Line));
        button.Setters.Add(new Setter(Control.CursorProperty, System.Windows.Input.Cursors.Hand));
        var shell = new FrameworkElementFactory(typeof(Border)); shell.Name = "ButtonShell";
        shell.SetValue(Border.CornerRadiusProperty, new CornerRadius(9));
        shell.SetBinding(Border.BackgroundProperty, Parent("Background")); shell.SetBinding(Border.BorderBrushProperty, Parent("BorderBrush"));
        shell.SetValue(Border.BorderThicknessProperty, new Thickness(1)); shell.SetBinding(Border.PaddingProperty, Parent("Padding"));
        var content = new FrameworkElementFactory(typeof(ContentPresenter));
        content.SetBinding(ContentPresenter.HorizontalAlignmentProperty, Parent("HorizontalContentAlignment"));
        content.SetValue(ContentPresenter.VerticalAlignmentProperty, VerticalAlignment.Center);
        content.SetValue(ContentPresenter.RecognizesAccessKeyProperty, true); shell.AppendChild(content);
        var template = new ControlTemplate(typeof(Button)) { VisualTree = shell };
        var focus = new Trigger { Property = UIElement.IsKeyboardFocusedProperty, Value = true };
        focus.Setters.Add(new Setter(Border.BorderBrushProperty, Accent, "ButtonShell")); template.Triggers.Add(focus);
        button.Setters.Add(new Setter(Control.TemplateProperty, template));
        button.Setters.Add(new Setter(Control.HorizontalContentAlignmentProperty, HorizontalAlignment.Center));
        var hover = new Trigger { Property = UIElement.IsMouseOverProperty, Value = true }; hover.Setters.Add(new Setter(UIElement.OpacityProperty, .8)); button.Triggers.Add(hover);
        var disabled = new Trigger { Property = UIElement.IsEnabledProperty, Value = false }; disabled.Setters.Add(new Setter(UIElement.OpacityProperty, .4)); button.Triggers.Add(disabled);
        resources[typeof(Button)] = button;
        foreach (var kind in new[] { typeof(TextBox), typeof(ComboBox), typeof(ListBox) })
        {
            var style = new Style(kind);
            style.Setters.Add(new Setter(Control.ForegroundProperty, Foreground)); style.Setters.Add(new Setter(Control.BackgroundProperty, Background));
            style.Setters.Add(new Setter(Control.BorderBrushProperty, Line)); style.Setters.Add(new Setter(Control.BorderThicknessProperty, new Thickness(1)));
            style.Setters.Add(new Setter(Control.PaddingProperty, new Thickness(10))); style.Setters.Add(new Setter(Control.FontSizeProperty, 14.0));
            resources[kind] = style;
        }
        var checkbox = new Style(typeof(CheckBox)); checkbox.Setters.Add(new Setter(Control.ForegroundProperty, Foreground));
        checkbox.Setters.Add(new Setter(Control.MarginProperty, new Thickness(0, 9, 0, 9))); resources[typeof(CheckBox)] = checkbox;
        var item = new Style(typeof(ListBoxItem));
        item.Setters.Add(new Setter(Control.HorizontalContentAlignmentProperty, HorizontalAlignment.Stretch));
        item.Setters.Add(new Setter(Control.PaddingProperty, new Thickness(12, 11, 12, 11)));
        item.Setters.Add(new Setter(Control.MarginProperty, new Thickness(0, 2, 0, 2)));
        item.Setters.Add(new Setter(Control.ForegroundProperty, Foreground)); item.Setters.Add(new Setter(Control.BackgroundProperty, Brushes.Transparent));
        var frame = new FrameworkElementFactory(typeof(Border)); frame.Name = "ItemFrame"; frame.SetValue(Border.CornerRadiusProperty, new CornerRadius(11));
        frame.SetBinding(Border.BackgroundProperty, Parent("Background")); frame.SetBinding(Border.PaddingProperty, Parent("Padding"));
        var presenter = new FrameworkElementFactory(typeof(ContentPresenter)); presenter.SetValue(ContentPresenter.HorizontalAlignmentProperty, HorizontalAlignment.Stretch); frame.AppendChild(presenter);
        item.Setters.Add(new Setter(Control.TemplateProperty, new ControlTemplate(typeof(ListBoxItem)) { VisualTree = frame }));
        var selected = new Trigger { Property = ListBoxItem.IsSelectedProperty, Value = true }; selected.Setters.Add(new Setter(Control.BackgroundProperty, SoftAccent)); item.Triggers.Add(selected);
        var over = new Trigger { Property = UIElement.IsMouseOverProperty, Value = true }; over.Setters.Add(new Setter(Control.BackgroundProperty, SoftAccent)); item.Triggers.Add(over);
        resources[typeof(ListBoxItem)] = item;
    }

    public static TextBlock Text(string text, double size = 14, bool bold = false, bool secondary = false) => new()
    {
        Text = text, FontSize = size, LineHeight = size * 1.4, FontWeight = bold ? FontWeights.SemiBold : FontWeights.Normal,
        Foreground = secondary ? Secondary : Foreground, TextWrapping = TextWrapping.Wrap
    };
    public static Border Card(UIElement child, int padding = 20) => new() { Child = child, Padding = new Thickness(padding), Background = Surface, BorderBrush = Line, BorderThickness = new Thickness(1), CornerRadius = new CornerRadius(14) };
    public static Button Button(string title, Action action, bool primary = false)
    {
        var button = new Button { Content = title };
        if (primary) { button.Background = Accent; button.Foreground = Brushes.White; button.BorderBrush = Accent; }
        button.Click += (_, _) => action(); return button;
    }
    public static UIElement Icon(string kind, double size = 22, Brush? brush = null)
    {
        var geometry = kind switch
        {
            "settings" => "M16,3A13,13 0 1 0 16,29A13,13 0 1 0 16,3 M16,11A5,5 0 1 0 16,21A5,5 0 1 0 16,11 M16,0V5 M16,27V32 M0,16H5 M27,16H32",
            "folder" => "M3,9V27H29V7H17L13,3H3Z M3,11H29",
            "history" => "M7,6A13,13 0 1 1 3,17 M2,3V11H10 M16,8V17L22,21",
            "link" => "M13,10L17,6A7,7 0 0 1 27,16L23,20 M19,22L15,26A7,7 0 0 1 5,16L9,12 M10,22L22,10",
            "clipboard" => "M10,5H4V29H28V5H22 M11,2H21V8H11Z M10,15H22 M10,21H19",
            "send" => "M3,4L29,16L3,28L8,16Z M8,16H27",
            _ => "M3,4H29V23H3Z M11,29H21 M16,23V29"
        };
        var path = new System.Windows.Shapes.Path { Data = Geometry.Parse(geometry), Stroke = brush ?? Secondary, StrokeThickness = 1.7, StrokeLineJoin = PenLineJoin.Round, StrokeStartLineCap = PenLineCap.Round, StrokeEndLineCap = PenLineCap.Round };
        return new Viewbox { Width = size, Height = size, Child = new Canvas { Width = 32, Height = 32, Children = { path } } };
    }
}
