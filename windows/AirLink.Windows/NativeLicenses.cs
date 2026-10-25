using System;
using System.IO;
using System.Reflection;
using System.Windows;
using System.Windows.Controls;

namespace AirLink.Windows;

internal static class NativeLicenses
{
    public static void Show(Window owner, Controller c)
    {
        var tabs = new TabControl { Background = Theme.Surface, Foreground = Theme.Foreground, BorderBrush = Theme.Line };
        foreach (var entry in new[] { ("AirLink · MIT", "AirLink.Windows.License"), (".NET · Third-party notices", "AirLink.Windows.Notices") })
        {
            using var stream = Assembly.GetExecutingAssembly().GetManifestResourceStream(entry.Item2);
            using var reader = stream is null ? null : new StreamReader(stream);
            var text = new TextBox { Text = reader?.ReadToEnd() ?? c.T("Die Lizenzinformationen fehlen in diesem Paket.", "This package is missing its license information."), IsReadOnly = true, AcceptsReturn = true, TextWrapping = TextWrapping.Wrap,
                VerticalScrollBarVisibility = ScrollBarVisibility.Auto, Background = Theme.Surface, Foreground = Theme.Foreground, BorderThickness = new Thickness(0), Padding = new Thickness(18) };
            tabs.Items.Add(new TabItem { Header = entry.Item1, Content = text });
        }
        new Window { Owner = owner, Title = c.T("Lizenzen", "Licenses") + " · AirLink " + c.Version, Content = tabs, Width = 760, Height = 600, MinWidth = 560, MinHeight = 360, WindowStartupLocation = WindowStartupLocation.CenterOwner, Background = Theme.Background, ShowInTaskbar = false }.ShowDialog();
    }
}
