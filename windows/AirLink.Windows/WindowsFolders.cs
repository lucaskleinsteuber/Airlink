using System;
using System.IO;
using System.Runtime.InteropServices;

namespace AirLink.Windows;

internal static class WindowsFolders
{
    // Current user's real Downloads location, including Shell folder redirection.
    // https://learn.microsoft.com/windows/win32/shell/knownfolderid
    [DllImport("shell32.dll", CharSet = CharSet.Unicode)]
    private static extern int SHGetKnownFolderPath(in Guid id, uint flags, IntPtr token, out IntPtr path);
    public static string Downloads()
    {
        var pointer = IntPtr.Zero;
        try
        {
            var id = new Guid("374DE290-123F-4565-9164-39C4925E467B");
            if (SHGetKnownFolderPath(in id, 0, IntPtr.Zero, out pointer) == 0 && Marshal.PtrToStringUni(pointer) is { Length: > 0 } path) return path;
        }
        finally { if (pointer != IntPtr.Zero) Marshal.FreeCoTaskMem(pointer); }
        return Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), "Downloads");
    }
}
