namespace AirLink.Core;

/// <summary>Only the newest status request may publish state or end the refresh.</summary>
public sealed class StatusRefreshGate
{
    private readonly object sync = new();
    private long generation;
    private bool inFlight;
    public long? Begin(bool force = false)
    {
        lock (sync)
        {
            if (inFlight && !force) return null;
            inFlight = true;
            return ++generation;
        }
    }
    public bool IsCurrent(long ticket) { lock (sync) return generation == ticket; }
    public void Complete(long ticket) { lock (sync) { if (generation == ticket) inFlight = false; } }
    public void Invalidate() { lock (sync) { generation++; inFlight = false; } }
}
