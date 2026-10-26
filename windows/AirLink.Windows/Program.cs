using System;
using System.ComponentModel;
using System.Drawing;
using System.IO;
using System.Linq;
using System.Reflection;
using System.Runtime.InteropServices;
using System.Threading;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Interop;
using AirLink.Core;
using Forms = System.Windows.Forms;

namespace AirLink.Windows;

internal static class Program
{
    internal static readonly uint ShowMessage = RegisterWindowMessage("lucasfd3.AirLink.ShowMainWindow");
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] private static extern uint RegisterWindowMessage(string name);
    [DllImport("user32.dll")] private static extern bool PostMessage(IntPtr window, uint message, IntPtr w, IntPtr l);
    [DllImport("shell32.dll", CharSet = CharSet.Unicode)] private static extern int SetCurrentProcessExplicitAppUserModelID(string id);

    [STAThread]
    public static void Main()
    {
        using var singleton = new Mutex(true, @"Local\lucasfd3.AirLink.Desktop", out var created);
        var version = Assembly.GetExecutingAssembly().GetName().Version?.ToString() ?? "0.10.0.0";
        var sessionPath = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "AirLink", "desktop-session-" + System.Diagnostics.Process.GetCurrentProcess().SessionId + ".json");
        if (!created)
        {
            PostMessage(new IntPtr(0xffff), ShowMessage, IntPtr.Zero, IntPtr.Zero);
            if (!DesktopSession.SameVersionRunning(sessionPath, version))
            {
                var english = System.Globalization.CultureInfo.CurrentUICulture.TwoLetterISOLanguageName == "en";
                MessageBox.Show(english ? "AirLink is already running. To open this update, quit the existing app using its system-tray menu next to the clock, then open the new EXE again. Your devices and files are preserved." : "AirLink läuft bereits. Beende die bisherige App über ihr Symbol im Infobereich neben der Uhr und öffne danach die neue EXE erneut. Deine Geräte und Dateien bleiben erhalten.", english ? "Open the AirLink update" : "AirLink-Update öffnen", MessageBoxButton.OK, MessageBoxImage.Information);
            }
            return;
        }
        using var registration = DesktopSession.Register(sessionPath, version);
        SetCurrentProcessExplicitAppUserModelID("lucasfd3.AirLink");
        var application = new System.Windows.Application { ShutdownMode = ShutdownMode.OnExplicitShutdown };
        Theme.Apply("system");
        var controller = new Controller(application.Dispatcher);
        var window = new MainWindow(controller);
        using var tray = new Forms.NotifyIcon { Text = "AirLink", Visible = true };
        using var iconStream = Assembly.GetExecutingAssembly().GetManifestResourceStream("AirLink.Windows.Icon");
        using var icon = iconStream is null ? null : new Icon(iconStream);
        tray.Icon = icon ?? SystemIcons.Application;
        var menu = new Forms.ContextMenuStrip();
        menu.Items.Add("AirLink öffnen / Open AirLink", null, (_, _) => window.ShowMain());
        menu.Items.Add("Empfangsordner / Receive folder", null, (_, _) => application.Dispatcher.InvokeAsync(() => controller.RunAsync(() => { controller.OpenReceiveFolder(); return Task.CompletedTask; })));
        menu.Items.Add("Nach Updates suchen … / Check for updates …", null, (_, _) => application.Dispatcher.InvokeAsync(() => window.CheckForUpdatesAsync()));
        menu.Items.Add(new Forms.ToolStripSeparator());
        menu.Items.Add("Beenden / Quit", null, async (_, _) => await window.QuitAsync());
        tray.ContextMenuStrip = menu;
        tray.DoubleClick += (_, _) => window.ShowMain();
        DesktopNotice? recentNotice = null;
        tray.BalloonTipClicked += (_, _) => application.Dispatcher.InvokeAsync(async () =>
        {
            if (recentNotice?.Kind == DesktopNoticeKind.CompletedTransfer && controller.Status?.Transfers.FirstOrDefault(t => t.Id == recentNotice.Id) is { } transfer)
                await controller.RunAsync(() => controller.RevealAsync(transfer));
            else window.ShowMain();
        });
        controller.NoticeRaised += notice =>
        {
            recentNotice = notice;
            var title = notice.Kind switch
            {
                DesktopNoticeKind.Pairing => controller.T("Verbindungsanfrage", "Connection request"),
                DesktopNoticeKind.IncomingTransfer => controller.T("Dateien warten auf Annahme", "Files waiting for acceptance"),
                DesktopNoticeKind.DuplicateDecision => controller.T("Duplikat benötigt deine Entscheidung", "Duplicate needs your decision"),
                DesktopNoticeKind.Clipboard => controller.T("Text empfangen", "Text received"),
                _ => controller.T("Datei empfangen", "File received")
            };
            var body = notice.DeviceName + (notice.Title.Length > 0 ? " · " + notice.Title : "") + controller.T(" · Zum Öffnen klicken", " · Click to open");
            if (body.Length > 240) body = body[..237] + "…";
            tray.ShowBalloonTip(5000, title, body, Forms.ToolTipIcon.Info);
        };
        application.SessionEnding += (_, _) => { _ = controller.StopAsync(); };
        application.DispatcherUnhandledException += (_, e) => { e.Handled = true; window.ReportError(e.Exception.Message); };
        application.Run(window);
        tray.Visible = false;
        singleton.ReleaseMutex();
    }
}
