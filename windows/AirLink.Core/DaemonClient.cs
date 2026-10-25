using System.Collections.Concurrent;
using System.Diagnostics;
using System.Runtime.InteropServices;
using System.Text;
using System.Text.Json;
using System.Threading.Channels;

namespace AirLink.Core;

public sealed class DaemonClientOptions
{
    public TimeSpan RequestTimeout { get; init; } = TimeSpan.FromSeconds(20);
    public TimeSpan GracefulStopTimeout { get; init; } = TimeSpan.FromSeconds(10);
    public TimeSpan TerminateTimeout { get; init; } = TimeSpan.FromSeconds(3);
    public TimeSpan KillTimeout { get; init; } = TimeSpan.FromSeconds(2);
    public int MaximumRequestBytes { get; init; } = 2 * 1024 * 1024;
    public int MaximumReplyCharacters { get; init; } = 16 * 1024 * 1024;
    public bool DisableDiscovery { get; init; }
    public IReadOnlyList<string> PrefixArguments { get; init; } = [];
}

public sealed class DaemonExitedEventArgs(Guid sessionId, bool unexpected, string diagnostic, int? exitCode) : EventArgs
{
    public Guid SessionId { get; } = sessionId;
    public bool Unexpected { get; } = unexpected;
    public string Diagnostic { get; } = diagnostic;
    public int? ExitCode { get; } = exitCode;
}

public sealed class DaemonException(string message) : IOException(message);

/// <summary>Owns one private helper session. No network administrator access or browser is required.</summary>
public sealed class DaemonClient : IAsyncDisposable
{
    private sealed class Session(Process process)
    {
        public Guid Id { get; } = Guid.NewGuid();
        public Process Process { get; } = process;
        public ConcurrentDictionary<string, TaskCompletionSource<JsonElement>> Pending { get; } = new();
        public Channel<string> Writes { get; } = Channel.CreateUnbounded<string>(new UnboundedChannelOptions { SingleReader = true, SingleWriter = false });
        public DiagnosticBuffer Diagnostics { get; } = new();
        public Task Writer { get; set; } = Task.CompletedTask;
        public Task Reader { get; set; } = Task.CompletedTask;
        public Task Errors { get; set; } = Task.CompletedTask;
        public Task Observer { get; set; } = Task.CompletedTask;
        public volatile bool Stopping;
        public volatile bool Faulted;
        public int FailureReported;
    }

    private readonly DaemonClientOptions options;
    private readonly SemaphoreSlim lifecycle = new(1, 1);
    private readonly object stateLock = new();
    private Session? current;
    private string lastDiagnostic = "";
    private bool disposed;

    public DaemonClient(DaemonClientOptions? options = null)
    {
        this.options = options ?? new();
        if (this.options.RequestTimeout <= TimeSpan.Zero || this.options.GracefulStopTimeout <= TimeSpan.Zero || this.options.TerminateTimeout <= TimeSpan.Zero || this.options.KillTimeout <= TimeSpan.Zero || this.options.MaximumRequestBytes < 1024 || this.options.MaximumReplyCharacters < 1024)
            throw new ArgumentOutOfRangeException(nameof(options), "Helper timeouts and frame limits must be positive.");
    }

    public event EventHandler<DaemonExitedEventArgs>? Exited;
    public Guid? SessionId { get { lock (stateLock) return current?.Id; } }
    public string DiagnosticTail { get { lock (stateLock) return current?.Diagnostics.Tail ?? lastDiagnostic; } }
    public bool IsRunning
    {
        get { lock (stateLock) return current is { Stopping: false, Faulted: false } session && !HasExited(session.Process); }
    }

    public async Task StartAsync(string helperPath, string dataDir, string? receiveOverride = null, CancellationToken ct = default)
    {
        await lifecycle.WaitAsync(ct).ConfigureAwait(false);
        try
        {
            ObjectDisposedException.ThrowIf(disposed, this);
            Session? previous;
            lock (stateLock) previous = current;
            if (previous is { Stopping: false, Faulted: false } && !HasExited(previous.Process)) return;
            if (previous != null)
            {
                if (!await StopSessionAsync(previous).ConfigureAwait(false)) throw new DaemonException("The previous AirLink service is still running. Retry after it has stopped.");
                Retire(previous);
            }
            ct.ThrowIfCancellationRequested();
            if (!Path.IsPathFullyQualified(helperPath) || !File.Exists(helperPath)) throw new DaemonException("The bundled AirLink service is missing. Extract the complete AirLink package and try again.");
            if (!Path.IsPathFullyQualified(dataDir)) throw new ArgumentException("Application data must use an absolute directory.", nameof(dataDir));
            Directory.CreateDirectory(dataDir);
            var start = new ProcessStartInfo(helperPath)
            {
                UseShellExecute = false,
                CreateNoWindow = true,
                RedirectStandardInput = true,
                RedirectStandardOutput = true,
                RedirectStandardError = true,
                StandardInputEncoding = new UTF8Encoding(false),
                StandardOutputEncoding = new UTF8Encoding(false),
                StandardErrorEncoding = new UTF8Encoding(false),
                WorkingDirectory = Path.GetDirectoryName(helperPath)!
            };
            foreach (var argument in options.PrefixArguments) start.ArgumentList.Add(argument);
            start.ArgumentList.Add("--stdio");
            start.ArgumentList.Add("--data-dir"); start.ArgumentList.Add(dataDir);
            if (!string.IsNullOrEmpty(receiveOverride))
            {
                if (!Path.IsPathFullyQualified(receiveOverride)) throw new ArgumentException("Receive directory must be absolute.", nameof(receiveOverride));
                start.ArgumentList.Add("--receive-dir"); start.ArgumentList.Add(receiveOverride);
            }
            if (options.DisableDiscovery) start.ArgumentList.Add("--no-discovery");
            var process = new Process { StartInfo = start };
            try { if (!process.Start()) throw new DaemonException("Unable to start the AirLink service."); }
            catch { process.Dispose(); throw; }
            var session = new Session(process);
            lock (stateLock) { current = session; lastDiagnostic = ""; }
            session.Errors = ReadDiagnosticsAsync(session);
            session.Reader = ReadRepliesAsync(session);
            session.Writer = WriteRequestsAsync(session);
            session.Observer = ObserveExitAsync(session);
        }
        finally { lifecycle.Release(); }
    }

    public async Task<T> RequestAsync<T>(string method, object? parameters = null, CancellationToken ct = default)
    {
        var element = await RequestAsync(method, parameters, ct).ConfigureAwait(false);
        return element.Deserialize<T>(ProtocolJson.Options) ?? throw new DaemonException("AirLink returned an empty response.");
    }

    public async Task<JsonElement> RequestAsync(string method, object? parameters = null, CancellationToken ct = default)
    {
        Session? session;
        lock (stateLock) session = current;
        if (session == null) throw new DaemonException("The AirLink service is not running. Choose Retry.");
        if (session.Faulted || session.Stopping || HasExited(session.Process)) throw await StoppedExceptionAsync(session).ConfigureAwait(false);
        return await RequestSessionAsync(session, method, parameters, ct).ConfigureAwait(false);
    }

    private async Task<JsonElement> RequestSessionAsync(Session session, string method, object? parameters, CancellationToken ct)
    {
        ct.ThrowIfCancellationRequested();
        var identifier = Guid.NewGuid().ToString("N");
        var line = JsonSerializer.Serialize(new { id = identifier, method, @params = parameters }, ProtocolJson.Options);
        if (Encoding.UTF8.GetByteCount(line) + 1 > options.MaximumRequestBytes) throw new DaemonException("Too many selected paths for one request. Select their containing folder instead.");
        var completion = new TaskCompletionSource<JsonElement>(TaskCreationOptions.RunContinuationsAsynchronously);
        if (!session.Pending.TryAdd(identifier, completion)) throw new InvalidOperationException("Duplicate request identifier.");
        using var timeout = method is "duplicate.resolve" or "offer" or "share.create" ? null : new CancellationTokenSource(options.RequestTimeout);
        using var linked = timeout == null ? CancellationTokenSource.CreateLinkedTokenSource(ct) : CancellationTokenSource.CreateLinkedTokenSource(ct, timeout.Token);
        using var registration = linked.Token.Register(() =>
        {
            if (!session.Pending.TryRemove(identifier, out var pending)) return;
            if (ct.IsCancellationRequested) pending.TrySetCanceled(ct);
            else pending.TrySetException(new TimeoutException("The AirLink service did not respond in time. The transfer may still be running."));
        });
        try
        {
            if (!session.Writes.Writer.TryWrite(line)) throw await StoppedExceptionAsync(session).ConfigureAwait(false);
            return await completion.Task.ConfigureAwait(false);
        }
        finally { session.Pending.TryRemove(identifier, out _); }
    }

    public async Task<bool> StopAsync(CancellationToken ct = default)
    {
        await lifecycle.WaitAsync(ct).ConfigureAwait(false);
        try
        {
            Session? session;
            lock (stateLock) session = current;
            if (session == null) return true;
            // Once cleanup starts it owns the process until exit or a bounded
            // failure result. Caller cancellation must not abandon a helper.
            var stopped = await StopSessionAsync(session).ConfigureAwait(false);
            if (stopped) Retire(session);
            return stopped;
        }
        finally { lifecycle.Release(); }
    }

    /// <summary>Updater shutdown: wait for a checkpointed exit, without closing pipes or terminating a live helper on timeout.</summary>
    public async Task<bool> StopGracefullyAsync(CancellationToken ct = default)
    {
        await lifecycle.WaitAsync(ct).ConfigureAwait(false);
        try
        {
            Session? session;
            lock (stateLock) session = current;
            if (session == null) return true;
            session.Stopping = true;
            using var deadline = new CancellationTokenSource(options.GracefulStopTimeout);
            try { await RequestSessionAsync(session, "shutdown", null, deadline.Token).ConfigureAwait(false); }
            catch (Exception ex) when (ex is IOException or TimeoutException or OperationCanceledException or InvalidOperationException) { }
            if (await WaitForExitAsync(session.Process, deadline.Token).ConfigureAwait(false))
            {
                session.Writes.Writer.TryComplete();
                Retire(session);
                return true;
            }
            session.Stopping = false;
            return false;
        }
        finally { lifecycle.Release(); }
    }

    private async Task<bool> StopSessionAsync(Session session)
    {
        session.Stopping = true;
        if (!HasExited(session.Process))
        {
            using var graceful = new CancellationTokenSource(options.GracefulStopTimeout);
            try { await RequestSessionAsync(session, "shutdown", null, graceful.Token).ConfigureAwait(false); }
            catch (Exception ex) when (ex is IOException or TimeoutException or OperationCanceledException or InvalidOperationException) { }
            session.Writes.Writer.TryComplete();
            if (await WaitForExitAsync(session.Process, graceful.Token).ConfigureAwait(false)) return true;
            try
            {
                if (OperatingSystem.IsWindows()) session.Process.Kill();
                else _ = PosixKill(session.Process.Id, 15);
            }
            catch (Exception ex) when (ex is InvalidOperationException or System.ComponentModel.Win32Exception) { }
            using var terminate = new CancellationTokenSource(options.TerminateTimeout);
            if (await WaitForExitAsync(session.Process, terminate.Token).ConfigureAwait(false)) return true;
            try { session.Process.Kill(entireProcessTree: true); }
            catch (Exception ex) when (ex is InvalidOperationException or System.ComponentModel.Win32Exception or NotSupportedException) { }
            using var final = new CancellationTokenSource(options.KillTimeout);
            return await WaitForExitAsync(session.Process, final.Token).ConfigureAwait(false);
        }
        session.Writes.Writer.TryComplete();
        return true;
    }

    private static async Task<bool> WaitForExitAsync(Process process, CancellationToken ct)
    {
        if (HasExited(process)) return true;
        try { await process.WaitForExitAsync(ct).ConfigureAwait(false); return true; }
        catch (OperationCanceledException) { return HasExited(process); }
        catch (InvalidOperationException) { return true; }
    }

    private async Task WriteRequestsAsync(Session session)
    {
        try
        {
            await foreach (var line in session.Writes.Reader.ReadAllAsync().ConfigureAwait(false))
            {
                await session.Process.StandardInput.WriteLineAsync(line).ConfigureAwait(false);
                await session.Process.StandardInput.FlushAsync().ConfigureAwait(false);
            }
            session.Process.StandardInput.Close();
        }
        catch (Exception ex) when (ex is IOException or ObjectDisposedException or InvalidOperationException)
        {
            await ReportFailureAsync(session).ConfigureAwait(false);
        }
    }

    private async Task ReadRepliesAsync(Session session)
    {
        var buffer = new char[4096];
        var line = new StringBuilder();
        try
        {
            while (true)
            {
                var count = await session.Process.StandardOutput.ReadAsync(buffer).ConfigureAwait(false);
                if (count == 0) break;
                for (var index = 0; index < count; index++)
                {
                    var character = buffer[index];
                    if (character == '\n') { ConsumeReply(session, line.ToString()); line.Clear(); }
                    else
                    {
                        if (line.Length >= options.MaximumReplyCharacters) throw new DaemonException("The AirLink service response is too large. Restart the service and review its history.");
                        line.Append(character);
                    }
                }
            }
            if (line.Length != 0) ConsumeReply(session, line.ToString());
            await ReportFailureAsync(session).ConfigureAwait(false);
        }
        catch (Exception ex) when (ex is IOException or ObjectDisposedException or InvalidOperationException or JsonException)
        {
            if (ex is DaemonException) session.Diagnostics.Append(ex.Message);
            await ReportFailureAsync(session).ConfigureAwait(false);
        }
    }

    private static void ConsumeReply(Session session, string line)
    {
        if (string.IsNullOrWhiteSpace(line)) return;
        using var document = JsonDocument.Parse(line);
        var root = document.RootElement;
        if (!root.TryGetProperty("id", out var id) || id.ValueKind != JsonValueKind.String) throw new JsonException("AirLink returned an invalid response identifier.");
        if (!session.Pending.TryRemove(id.GetString()!, out var completion)) return; // Timed-out/old reply.
        if (root.TryGetProperty("error", out var error) && error.ValueKind != JsonValueKind.Null)
        {
            completion.TrySetException(new DaemonException(error.ValueKind == JsonValueKind.String ? error.GetString()! : "The AirLink service rejected the operation."));
        }
        else if (root.TryGetProperty("result", out var result)) completion.TrySetResult(result.Clone());
        else completion.TrySetException(new DaemonException("AirLink returned an invalid response."));
    }

    private static async Task ReadDiagnosticsAsync(Session session)
    {
        var buffer = new char[2048];
        var line = new StringBuilder();
        var omitted = false;
        try
        {
            while (true)
            {
                var count = await session.Process.StandardError.ReadAsync(buffer).ConfigureAwait(false);
                if (count == 0) break;
                for (var index = 0; index < count; index++)
                {
                    var character = buffer[index];
                    if (character == '\n') { session.Diagnostics.Append(omitted ? "[Long diagnostic line omitted]" : line.ToString()); line.Clear(); omitted = false; }
                    else if (line.Length < 8192 && !omitted) line.Append(character);
                    else { line.Clear(); omitted = true; }
                }
            }
            if (line.Length > 0 || omitted) session.Diagnostics.Append(omitted ? "[Long diagnostic line omitted]" : line.ToString());
        }
        catch (Exception ex) when (ex is IOException or ObjectDisposedException or InvalidOperationException) { }
    }

    private async Task ObserveExitAsync(Session session)
    {
        try { await session.Process.WaitForExitAsync().ConfigureAwait(false); }
        catch (InvalidOperationException) { }
        await ReportFailureAsync(session).ConfigureAwait(false);
    }

    private async Task ReportFailureAsync(Session session)
    {
        session.Faulted = true;
        if (Interlocked.Exchange(ref session.FailureReported, 1) != 0) return;
        var exception = await StoppedExceptionAsync(session).ConfigureAwait(false);
        foreach (var pending in session.Pending)
            if (session.Pending.TryRemove(pending.Key, out var completion)) completion.TrySetException(exception);
        bool active;
        lock (stateLock) { active = ReferenceEquals(current, session); if (active) lastDiagnostic = session.Diagnostics.Tail; }
        if (!active) return;
        int? code = null;
        try { if (session.Process.HasExited) code = session.Process.ExitCode; } catch (InvalidOperationException) { }
        var args = new DaemonExitedEventArgs(session.Id, !session.Stopping, exception.Message, code);
        foreach (EventHandler<DaemonExitedEventArgs> handler in Exited?.GetInvocationList() ?? [])
        {
            try { handler(this, args); } catch { /* A presentation callback cannot strand pending work. */ }
        }
    }

    private static async Task<DaemonException> StoppedExceptionAsync(Session session)
    {
        // Startup can exit before stdin is read. Give its concurrently drained
        // diagnostics a short bounded opportunity to reach the error message.
        try { await session.Errors.WaitAsync(TimeSpan.FromMilliseconds(200)).ConfigureAwait(false); }
        catch (TimeoutException) { }
        var detail = session.Diagnostics.Tail.Trim();
        return new DaemonException(detail.Length == 0 ? "The AirLink service stopped. Choose Retry." : "The AirLink service stopped: " + detail);
    }

    private void Retire(Session session)
    {
        session.Writes.Writer.TryComplete();
        foreach (var pending in session.Pending)
            if (session.Pending.TryRemove(pending.Key, out var completion)) completion.TrySetException(new DaemonException("The AirLink service stopped."));
        lock (stateLock)
        {
            if (ReferenceEquals(current, session)) { lastDiagnostic = session.Diagnostics.Tail; current = null; }
        }
        // Closing the owned pipes also releases a blocked writer after exit.
        try { session.Process.StandardInput.BaseStream.Dispose(); } catch (InvalidOperationException) { }
        try { session.Process.StandardOutput.BaseStream.Dispose(); } catch (InvalidOperationException) { }
        try { session.Process.StandardError.BaseStream.Dispose(); } catch (InvalidOperationException) { }
        session.Process.Dispose();
    }

    private static bool HasExited(Process process)
    {
        try { return process.HasExited; }
        catch (InvalidOperationException) { return true; }
    }

    [DllImport("libc", EntryPoint = "kill", SetLastError = true)]
    private static extern int PosixKill(int pid, int signal);

    public async ValueTask DisposeAsync()
    {
        if (disposed) return;
        if (!await StopAsync().ConfigureAwait(false)) throw new DaemonException("The AirLink service has not stopped yet.");
        disposed = true;
    }
}
