using System;
using System.Collections.Generic;
using System.Globalization;
using System.Linq;
using System.Runtime.InteropServices;
using System.Threading.Tasks;
using System.Windows;
using AirLink.Core;
using NetSparkleUpdater;
using NetSparkleUpdater.Configurations;
using NetSparkleUpdater.Enums;

namespace AirLink.Windows;

/// <summary>Created lazily by an explicit menu action. No update loop, background timer or startup request.</summary>
internal sealed class WindowsUpdater(MainWindow owner, Controller controller) : IDisposable
{
    private SparkleUpdater? updater;
    private WindowsUpdateFeed? feed;
    private StrictUpdateSignatures? signatures;
    private bool checking;
    private readonly List<Window> windows = [];

    public async Task CheckAsync()
    {
        owner.ShowMain();
        if (checking || owner.UpdatePrepared) return;
        var existing = windows.LastOrDefault(window => window.IsVisible);
        if (existing is not null) { existing.Activate(); return; }
        checking = true;
        try
        {
            NetSparkleUpdater.Properties.Resources.Culture = CultureInfo.GetCultureInfo(controller.English ? "en" : "de");
            EnsureUpdater();
            feed!.ResetFailure(); signatures!.ResetFailure();
            await updater!.CheckForUpdatesAtUserRequest();
        }
        catch (Exception)
        {
            ShowUnavailable();
        }
        finally { checking = false; }
    }

    private void EnsureUpdater()
    {
        if (updater is not null) return;
        var channel = WindowsUpdateChannel.Load(RuntimeInformation.OSArchitecture);
        signatures = new StrictUpdateSignatures(channel.PublicKey);
        feed = new WindowsUpdateFeed(channel.Feed);
        var filter = new WindowsUpdateFilter(channel.Architecture);
        var ui = new UpdateUI(this)
        {
            HideReleaseNotes = true,
            HideSkipButton = true,
            HideRemindMeLaterButton = true,
            ProcessWindowAfterInit = (window, _) =>
            {
                window.Owner = owner;
                windows.Add(window);
                window.Closed += (_, _) => windows.Remove(window);
            }
        };
        updater = new SafeSparkleUpdater(channel.Feed.AbsoluteUri, signatures, ReportLaunchFailure)
        {
            UIFactory = ui,
            AppCastDataDownloader = feed,
            RelaunchAfterUpdate = false,
            CustomInstallerArguments = "",
            // In NetSparkle this means: start a waiter, invoke our graceful quit,
            // and run Setup only after this process exits. No taskkill is used.
            ShouldKillParentProcessWhenStartingInstaller = true
        };
        updater.Configuration = new DefaultConfiguration(updater.Configuration.AssemblyAccessor);
        updater.AppCastHelper.AppCastFilter = filter;
        updater.PreparingToExitAsync += async (_, args) =>
        {
            // NetSparkle treats an escaping event exception as permission to exit,
            // so explicitly retain cancellation unless all preparation succeeds.
            args.Cancel = true;
            try { args.Cancel = !await owner.Dispatcher.InvokeAsync(owner.PrepareForUpdateAsync).Task.Unwrap(); }
            catch (Exception)
            {
                // Do not let even a closing-dispatcher/reporting failure escape:
                // upstream otherwise treats an event exception as permission.
                try { await owner.Dispatcher.InvokeAsync(() => owner.ReportError(controller.T("Das Update wurde abgebrochen. AirLink konnte nicht sicher beendet werden.", "The update was cancelled because AirLink could not close safely."))); }
                catch (Exception) { }
            }
        };
        updater.InstallerProcessAboutToStart += (process, path) =>
        {
            try
            {
                var item = filter.Selected;
                if (!owner.UpdatePrepared || item is null || signatures.VerifySignatureOfFile(item.DownloadSignature ?? "", path) != ValidationResult.Valid) return false;
                WindowsUpdateInstaller.Prepare(process.StartInfo, path, Environment.ProcessId, Environment.SystemDirectory);
                return true;
            }
            catch (Exception) { return false; }
        };
        updater.CloseApplicationAsync += async () => await owner.Dispatcher.InvokeAsync(owner.FinishUpdateQuit).Task;
        updater.InstallUpdateFailed += (_, _) =>
        {
            ReportLaunchFailure();
            return false; // NetSparkle3.1.0 declares bool; this result is unused.
        };
    }

    private void ReportLaunchFailure()
    {
        try
        {
            owner.Dispatcher.InvokeAsync(() =>
            {
                owner.CancelUpdatePreparation();
                owner.ReportError(controller.T("Das Update konnte nicht gestartet werden. Öffne AirLink erneut oder wähle Dienst erneut starten.", "The update could not start. Reopen AirLink or choose Retry service."));
            });
        }
        catch (Exception) { }
    }

    private sealed class SafeSparkleUpdater(string url, StrictUpdateSignatures verifier, Action failed) : SparkleUpdater(url, verifier)
    {
        protected override async Task RunDownloadedInstaller(string downloadFilePath)
        {
            // Upstream starts the waiter from an async-void UI callback without
            // catching Process.Start failures. Restore the app on any handoff error.
            try { await base.RunDownloadedInstaller(downloadFilePath); }
            catch (Exception) { failed(); }
        }
    }

    private void ShowUnavailable()
    {
        if (!owner.Dispatcher.CheckAccess()) { owner.Dispatcher.Invoke(ShowUnavailable); return; }
        var message = signatures?.RejectedSignature == true || feed?.Failure == UpdateFeedFailure.InvalidDocument
            ? controller.T("Das Update konnte nicht sicher verifiziert werden. Es wurde nichts installiert. Versuche es später erneut.", "The update could not be verified securely. Nothing was installed. Try again later.")
            : controller.T("Updates sind derzeit nicht abrufbar. Prüfe die Internetverbindung. Bei einem privaten Repository ist der signierte Update-Feed ohne Anmeldung nicht öffentlich erreichbar; die installierte Version bleibt unverändert.", "Updates are currently unavailable. Check the internet connection. A private repository's signed update feed cannot be accessed publicly without signing in; your installed version is unchanged.");
        MessageBox.Show(owner, message, controller.T("AirLink-Updates", "AirLink updates"), MessageBoxButton.OK, MessageBoxImage.Information);
    }

    private sealed class UpdateUI(WindowsUpdater updates) : NetSparkleUpdater.UI.WPF.UIFactory
    {
        public override void ShowCannotDownloadAppcast(string? appcastUrl) => updates.ShowUnavailable();
        public override void ShowDownloadErrorMessage(string message, string? appcastUrl) => updates.ShowUnavailable();
    }

    public void Dispose() { updater?.Dispose(); feed?.Dispose(); }
}
