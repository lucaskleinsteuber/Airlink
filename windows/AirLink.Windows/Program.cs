using System;
using System.Drawing;
using System.IO;
using System.IO.Pipes;
using System.Linq;
using System.Reflection;
using System.Runtime.InteropServices;
using System.Text.Json;
using System.Threading;
using System.Threading.Tasks;
using System.Windows;
using AirLink.Core;
using Forms = System.Windows.Forms;

namespace AirLink.Windows;

internal static class Program
{
    internal static readonly uint ShowMessage = RegisterWindowMessage("lucasfd3.AirLink.ShowMainWindow");
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] private static extern uint RegisterWindowMessage(string name);
    [DllImport("user32.dll")] private static extern bool PostMessage(IntPtr window, uint message, IntPtr w, IntPtr l);
    [DllImport("shell32.dll", CharSet = CharSet.Unicode)] private static extern int SetCurrentProcessExplicitAppUserModelID(string id);
    private static string PipeName => "lucasfd3.AirLink." + System.Diagnostics.Process.GetCurrentProcess().SessionId;

    [STAThread]
    public static void Main(string[] args)
    {
        var background = args.Contains("--background");
        var demo = args.Contains("--demo");
        var snapshotIndex = Array.IndexOf(args, "--snapshot");
        var paths = args.Where(a => !a.StartsWith("--", StringComparison.Ordinal) && Path.IsPathFullyQualified(a)).ToArray();
        if (demo) { RunDemo(snapshotIndex >= 0 && snapshotIndex + 1 < args.Length ? args[snapshotIndex + 1] : null); return; }
        using var singleton = new Mutex(true, @"Local\lucasfd3.AirLink.Desktop", out var created);
        var version = Assembly.GetExecutingAssembly().GetName().Version?.ToString() ?? "0.13.0.0";
        var sessionPath = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "AirLink", "desktop-session-" + System.Diagnostics.Process.GetCurrentProcess().SessionId + ".json");
        if (!created)
        {
            if (paths.Length > 0 && Forward(paths)) return;
            PostMessage(new IntPtr(0xffff), ShowMessage, IntPtr.Zero, IntPtr.Zero);
            if (!DesktopSession.SameVersionRunning(sessionPath, version))
            {
                var english = System.Globalization.CultureInfo.CurrentUICulture.TwoLetterISOLanguageName != "de";
                MessageBox.Show(english ? "AirLink is already running. To open this update, quit the existing app using its icon in the notification area next to the clock, then open the new version again. Your devices and files are preserved." : "AirLink läuft bereits. Beende die bisherige App über ihr Symbol im Infobereich neben der Uhr und öffne danach die neue Version erneut. Deine Geräte und Dateien bleiben erhalten.", "AirLink", MessageBoxButton.OK, MessageBoxImage.Information);
            }
            return;
        }
        using var registration = DesktopSession.Register(sessionPath, version);
        SetCurrentProcessExplicitAppUserModelID("lucasfd3.AirLink");
        var application = new Application { ShutdownMode = ShutdownMode.OnExplicitShutdown };
        Theme.Apply(application);
        var controller = new Controller(application.Dispatcher);
        var window = new MainWindow(controller);
        using var tray = new Forms.NotifyIcon { Text = "AirLink", Visible = true };
        using var iconStream = Assembly.GetExecutingAssembly().GetManifestResourceStream("AirLink.Windows.Icon");
        using var icon = iconStream is null ? null : new Icon(iconStream);
        tray.Icon = icon ?? SystemIcons.Application;
        var menu = new Forms.ContextMenuStrip();
        void BuildMenu()
        {
            menu.Items.Clear();
            menu.Items.Add(controller.T("AirLink öffnen", "Open AirLink"), null, (_, _) => window.ShowMain());
            menu.Items.Add(controller.T("Empfangsordner öffnen", "Open receive folder"), null, (_, _) => application.Dispatcher.InvokeAsync(() => window.Run(() => { controller.OpenReceiveFolder(); return Task.CompletedTask; })));
            menu.Items.Add(new Forms.ToolStripSeparator());
            menu.Items.Add(controller.T("Beenden", "Quit"), null, async (_, _) => await window.QuitAsync());
        }
        BuildMenu();
        menu.Opening += (_, _) => BuildMenu();
        tray.ContextMenuStrip = menu;
        tray.MouseClick += (_, e) => { if (e.Button == Forms.MouseButtons.Left) window.ShowMain(); };
        DesktopNotice? recentNotice = null;
        tray.BalloonTipClicked += (_, _) => application.Dispatcher.InvokeAsync(async () =>
        {
            if (recentNotice?.Kind == DesktopNoticeKind.CompletedTransfer && controller.Status?.Transfers.FirstOrDefault(t => t.Id == recentNotice.Id) is { } transfer)
                await window.RunAsync(() => controller.RevealAsync(transfer));
            else if (recentNotice?.Kind == DesktopNoticeKind.Update) await window.CheckForUpdatesAsync();
            else window.ShowMain();
        });
        controller.NoticeRaised += notice =>
        {
            recentNotice = notice;
            var title = notice.Kind switch
            {
                DesktopNoticeKind.Update => controller.T("Neue AirLink-Version verfügbar", "New AirLink version available"),
                DesktopNoticeKind.Pairing => controller.T("Verbindungsanfrage", "Connection request"),
                DesktopNoticeKind.IncomingTransfer => controller.T($"{notice.DeviceName} möchte Dateien senden", $"{notice.DeviceName} wants to send files"),
                DesktopNoticeKind.Clipboard => controller.T($"Text von {notice.DeviceName}", $"Text from {notice.DeviceName}"),
                _ => controller.T($"Von {notice.DeviceName} empfangen", $"Received from {notice.DeviceName}")
            };
            var body = notice.Kind == DesktopNoticeKind.CompletedTransfer ? notice.Title + controller.T(" · Klicken zum Zeigen", " · Click to show")
                : notice.Kind == DesktopNoticeKind.Pairing ? controller.T($"{notice.DeviceName} möchte sich mit diesem PC verbinden.", $"{notice.DeviceName} wants to connect to this PC.")
                : notice.Title.Length > 0 ? notice.Title : controller.T("Zum Öffnen klicken", "Click to open");
            if (body.Length > 240) body = body[..237] + "…";
            tray.ShowBalloonTip(5000, title, body, Forms.ToolTipIcon.Info);
        };
        using var lifetime = new CancellationTokenSource();
        _ = ListenAsync(window, lifetime.Token);
        application.SessionEnding += (_, _) => { _ = controller.StopAsync(); };
        application.DispatcherUnhandledException += (_, e) => { e.Handled = true; window.ReportError(e.Exception.Message); };
        window.Loaded += (_, _) => { if (paths.Length > 0) application.Dispatcher.InvokeAsync(async () => { await Task.Delay(1500); window.AcceptPaths(paths); }); };
        if (!background) window.Show();
        if (background) { var helper = new System.Windows.Interop.WindowInteropHelper(window); helper.EnsureHandle(); _ = application.Dispatcher.InvokeAsync(async () => await StartHiddenAsync(window, controller)); }
        application.Run();
        lifetime.Cancel();
        tray.Visible = false;
        singleton.ReleaseMutex();
    }

    /// <summary>Started at sign-in: run the service without showing the window.</summary>
    private static async Task StartHiddenAsync(MainWindow window, Controller controller)
    {
        await controller.StartAsync();
        _ = controller.FallbackRefreshAsync();
    }

    /// <summary>Paths from Explorer's "Send to" reach the running app over a
    /// pipe that only this user can open.</summary>
    private static bool Forward(string[] paths)
    {
        try
        {
            using var pipe = new NamedPipeClientStream(".", PipeName, PipeDirection.Out, PipeOptions.CurrentUserOnly);
            pipe.Connect(2000);
            using var writer = new StreamWriter(pipe);
            writer.WriteLine(JsonSerializer.Serialize(paths));
            writer.Flush();
            return true;
        }
        catch (Exception) { return false; }
    }

    private static async Task ListenAsync(MainWindow window, CancellationToken ct)
    {
        while (!ct.IsCancellationRequested)
        {
            try
            {
                await using var pipe = new NamedPipeServerStream(PipeName, PipeDirection.In, 1, PipeTransmissionMode.Byte, PipeOptions.Asynchronous | PipeOptions.CurrentUserOnly);
                await pipe.WaitForConnectionAsync(ct);
                using var reader = new StreamReader(pipe);
                var line = await reader.ReadLineAsync(ct);
                if (line is null || line.Length > 2 * 1024 * 1024) continue;
                var paths = (JsonSerializer.Deserialize<string[]>(line) ?? []).Where(Path.IsPathFullyQualified).ToArray();
                await window.Dispatcher.InvokeAsync(() => window.AcceptPaths(paths));
            }
            catch (OperationCanceledException) { return; }
            catch (Exception) { await Task.Delay(500, CancellationToken.None); }
        }
    }

    private static void RunDemo(string? snapshot)
    {
        var application = new Application { ShutdownMode = snapshot is null ? ShutdownMode.OnMainWindowClose : ShutdownMode.OnExplicitShutdown };
        Theme.Apply(application);
        var english = Environment.GetEnvironmentVariable("AIRLINK_DEMO_LANGUAGE") == "en";
        var controller = new Controller(application.Dispatcher, demo: true);
        controller.ShowDemo(DemoData.Status("home", english));
        var window = new MainWindow(controller);
        if (snapshot is null) { application.Run(window); return; }
        window.Left = -20000; window.Top = 0; window.ShowActivated = false;
        window.Loaded += (_, _) => application.Dispatcher.InvokeAsync(() => { window.Snapshot(snapshot); application.Shutdown(); }, System.Windows.Threading.DispatcherPriority.ApplicationIdle);
        window.Show();
        application.Run();
    }
}
