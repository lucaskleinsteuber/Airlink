using System.Globalization;
using System.Text.Json;
using AirLink.Core;

internal static class SettingsDraftTests
{
    private static void Check(bool condition, string message) { if (!condition) throw new InvalidOperationException(message); }
    public static Task MergeOnlyEditedFields()
    {
        var baseline = JsonSerializer.Deserialize<HostSettings>("""
        {"name":"Windows laptop","receiveDirectory":"/previous","appearance":"system","notifications":true,"sendLimitBytesPerSecond":0,"futureSetting":{"value":1}}
        """, ProtocolJson.Options)!;
        var draft = new SettingsDraft(baseline);
        draft.Values.Name = "  Work laptop  "; draft.Values.Language = "en";
        var latest = ProtocolJson.Clone(baseline);
        latest.ReceiveDirectory = "/changed-by-setup"; latest.SendLimitBytesPerSecond = 3_000_000;
        latest.Notifications = false; latest.Shortcut.Enabled = false;
        latest.Additional!["futureSetting"] = JsonSerializer.SerializeToElement(new { value = 2 });
        var merged = draft.Merge(latest);
        Check(merged.Name == "Work laptop" && merged.Language == "en", "Explicit edits were not applied");
        Check(merged.ReceiveDirectory == latest.ReceiveDirectory && merged.SendLimitBytesPerSecond == 3_000_000 && !merged.Notifications, "Untouched changes from a newer settings snapshot were overwritten");
        Check(!merged.Shortcut.Enabled, "Unexposed shortcut fields were reset");
        Check(merged.Additional!["futureSetting"].GetProperty("value").GetInt32() == 2, "New additive settings were lost");
        Check(latest.Name == "Windows laptop" && latest.Language != "en" && baseline.ReceiveDirectory == "/previous", "Merging mutated the source or baseline");
        draft.Values.Name = baseline.Name; draft.Values.Language = baseline.Language;
        Check(!draft.HasChanges && draft.Merge(latest).ReceiveDirectory == latest.ReceiveDirectory, "Reverted edits still overwrote current settings");
        return Task.CompletedTask;
    }
    public static Task DraftResetAndLimitBoundaries()
    {
        var original = new HostSettings { Name = "Saved", ReceiveDirectory = "/received" };
        var draft = new SettingsDraft(original); draft.Values.ReceiveDirectory = "/draft";
        Check(draft.HasChanges && original.ReceiveDirectory == "/received", "The edit did not remain isolated");
        draft.Reset(original); Check(!draft.HasChanges && draft.Values.ReceiveDirectory == "/received", "Discard did not restore the saved snapshot");
        draft.Values.Language = "en";
        var acknowledged = draft.Merge(original); draft.Reset(acknowledged);
        draft.Values.Notifications = false;
        Check(draft.HasChanges && draft.Merge(acknowledged).Language == "en", "A second edit after save used the previous edit object");
        var english = CultureInfo.GetCultureInfo("en-US"); var german = CultureInfo.GetCultureInfo("de-DE");
        foreach (var bytes in new long[] { 0, 1024, 1_500_000, 1L << 40 })
        {
            Check(SettingsDraft.TryParseLimit(SettingsDraft.FormatLimit(bytes, english), out var parsed, english) && parsed == bytes, "Limit formatting changed exact bytes");
        }
        Check(SettingsDraft.TryParseLimit("1,5", out var localized, german) && localized == 1_500_000, "Localized decimal limit was rejected");
        foreach (var invalid in new[] { "-1", "NaN", "0.001", "0.0010245", "1099511.627777", "" })
            Check(!SettingsDraft.TryParseLimit(invalid, out _, english), "Unsafe or sub-byte speed limit accepted");
        return Task.CompletedTask;
    }
    public static Task SetupReviewPreservesOtherPreferences()
    {
        var source = new HostSettings { Name = "Before", ReceiveDirectory = Path.GetTempPath(), Appearance = "dark", Notifications = true, SendLimitBytesPerSecond = 2_000_000, Shortcut = new() { Enabled = false } };
        var custom = Path.Combine(Path.GetTempPath(), "Chosen destination");
        var result = SetupDefaults.Complete(source, "  Laptop  ", custom, false);
        Check(result.SetupCompleted && result.Name == "Laptop" && result.ReceiveDirectory == custom && !result.Notifications, "Review did not commit explicit selections");
        Check(result.Appearance == "dark" && result.SendLimitBytesPerSecond == 2_000_000 && !result.Shortcut.Enabled, "Setup reset unrelated saved preferences");
        Check(!source.SetupCompleted && source.Name == "Before" && source.Notifications, "Review modified settings before confirmation");
        Check(SetupDefaults.Complete(source, "Laptop", custom).Notifications, "Unspecified notification choice reset an existing preference");
        try { SetupDefaults.Complete(source, "Laptop", "relative-folder", false); throw new InvalidOperationException("Relative destination accepted"); } catch (ArgumentException) { }
        return Task.CompletedTask;
    }
    public static Task RebaseDisplayedFieldsWithoutLosingTyping()
    {
        var original = new HostSettings { Name = "Laptop", ReceiveDirectory = "/before", Notifications = true };
        var clean = new SettingsDraft(original);
        var latest = ProtocolJson.Clone(original); latest.Name = "Renamed elsewhere"; latest.ReceiveDirectory = "/setup-folder";
        Check(clean.Rebase(latest) && clean.Values.Name == latest.Name && clean.Values.ReceiveDirectory == latest.ReceiveDirectory && !clean.HasChanges, "Clean displayed settings did not follow a newer snapshot");
        Check(!clean.Rebase(latest), "Identical polling rebuilt the edit state");
        var dirty = new SettingsDraft(original); dirty.Values.Name = ""; dirty.Values.Notifications = false;
        latest.SendLimitBytesPerSecond = 1_500_000; latest.Appearance = "dark";
        Check(dirty.Rebase(latest) && dirty.Values.Name == "" && !dirty.Values.Notifications, "Rebase normalized or replaced an in-progress user edit");
        Check(dirty.Values.ReceiveDirectory == "/setup-folder" && dirty.Values.SendLimitBytesPerSecond == 1_500_000 && dirty.Values.Appearance == "dark", "Unedited displayed preferences remained stale");
        dirty.Values.Name = "Typed name";
        var merged = dirty.Merge(latest);
        Check(merged.Name == "Typed name" && !merged.Notifications && merged.Appearance == "dark", "Saving after rebase lost an edit or a newer setting");
        return Task.CompletedTask;
    }
}
