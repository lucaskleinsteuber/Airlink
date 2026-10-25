using System;
using System.Diagnostics;
using System.IO;
using System.Runtime.InteropServices;

namespace AirLink.Windows;

internal static class Shell
{
    [DllImport("shell32.dll", CharSet = CharSet.Unicode)]
    private static extern int SHParseDisplayName(string name, IntPtr bindContext, out IntPtr item, uint attributes, out uint actualAttributes);
    [DllImport("shell32.dll")]
    private static extern int SHOpenFolderAndSelectItems(IntPtr item, uint count, IntPtr children, uint flags);

    public static void Reveal(string path)
    {
        if (!Path.IsPathFullyQualified(path) || !(File.Exists(path) || Directory.Exists(path))) throw new IOException("Die empfangene Datei ist nicht mehr verfügbar. / The received file is no longer available.");
        Marshal.ThrowExceptionForHR(SHParseDisplayName(path, IntPtr.Zero, out var item, 0, out _));
        try { Marshal.ThrowExceptionForHR(SHOpenFolderAndSelectItems(item, 0, IntPtr.Zero, 0)); }
        finally { Marshal.FreeCoTaskMem(item); }
    }

    public static void OpenFolder(string path)
    {
        if (!Path.IsPathFullyQualified(path) || !Directory.Exists(path)) throw new IOException("Der Empfangsordner ist nicht verfügbar. / The receive folder is unavailable.");
        var start = new ProcessStartInfo("explorer.exe") { UseShellExecute = true };
        start.ArgumentList.Add(path);
        Process.Start(start);
    }
}
