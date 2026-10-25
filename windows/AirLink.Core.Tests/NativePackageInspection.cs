using System.Reflection.Metadata;
using System.Reflection.PortableExecutable;
using System.Security.Cryptography;
using System.Text;

internal static class NativePackageInspection
{
    public static int Run(string[] arguments)
    {
        try
        {
            if (arguments.Length != 3) throw new ArgumentException("Usage: --inspect-native <AirLink.dll> <matching-Go-helper.exe>");
            using var input = File.OpenRead(arguments[1]);
            using var image = new PEReader(input);
            if (!image.HasMetadata || image.PEHeaders.CorHeader is not { } header) throw new InvalidDataException("Missing managed PE metadata.");
            var metadata = image.GetMetadataReader();
            if (!metadata.IsAssembly) throw new InvalidDataException("The native client payload is not an assembly.");
            var assembly = metadata.GetAssemblyDefinition();
            if (metadata.GetString(assembly.Name) != "AirLink" || assembly.Version != new Version(0, 9, 0, 0)) throw new InvalidDataException("Wrong native client identity or assembly version; expected AirLink 0.9.0.0.");
            var directory = header.ResourcesDirectory;
            if (directory.RelativeVirtualAddress <= 0 || directory.Size <= 0) throw new InvalidDataException("Missing managed resource directory.");
            var block = image.GetSectionData(directory.RelativeVirtualAddress);
            var resources = new Dictionary<string, byte[]>(StringComparer.Ordinal);
            foreach (var handle in metadata.ManifestResources)
            {
                var resource = metadata.GetManifestResource(handle);
                if (!resource.Implementation.IsNil) continue;
                var name = metadata.GetString(resource.Name);
                var offset = checked((int)resource.Offset);
                if (offset < 0 || offset > directory.Size - sizeof(uint)) throw new InvalidDataException("Invalid embedded resource offset: " + name);
                var reader = block.GetReader(offset, directory.Size - offset);
                var length = reader.ReadUInt32();
                if (length > int.MaxValue || length > reader.RemainingBytes) throw new InvalidDataException("Invalid embedded resource length: " + name);
                if (!resources.TryAdd(name, reader.ReadBytes((int)length))) throw new InvalidDataException("Duplicate embedded resource: " + name);
            }
            byte[] Require(string name)
            {
                if (!resources.TryGetValue(name, out var bytes) || bytes.Length == 0) throw new InvalidDataException("Missing embedded resource: " + name);
                return bytes;
            }
            var helper = Require("AirLink.Windows.Helper");
            var expected = SHA256.HashData(File.ReadAllBytes(arguments[2]));
            if (!CryptographicOperations.FixedTimeEquals(SHA256.HashData(helper), expected)) throw new InvalidDataException("Embedded Go helper does not match the architecture-specific publish input.");
            var license = Encoding.UTF8.GetString(Require("AirLink.Windows.License"));
            if (!license.Contains("MIT License", StringComparison.Ordinal) || !license.Contains("Copyright", StringComparison.Ordinal) || !license.Contains("Permission is hereby granted", StringComparison.Ordinal)) throw new InvalidDataException("Embedded AirLink license is incomplete.");
            var notices = Encoding.UTF8.GetString(Require("AirLink.Windows.Notices"));
            if (notices.Length < 4096 || !notices.Contains("Microsoft.NETCore.App.Runtime.win-", StringComparison.OrdinalIgnoreCase) || !notices.Contains("Microsoft.WindowsDesktop.App.Runtime.win-", StringComparison.OrdinalIgnoreCase) || !notices.Contains("THIRD-PARTY-NOTICES", StringComparison.Ordinal)) throw new InvalidDataException("Embedded runtime notices are incomplete.");
            var icon = Require("AirLink.Windows.Icon");
            if (icon.Length < 6 || icon[0] != 0 || icon[1] != 0 || icon[2] != 1 || icon[3] != 0 || icon[4] == 0) throw new InvalidDataException("Embedded app icon is not a multi-image Windows ICO.");
            foreach (var entry in new[] { "AirLink.Windows.Helper", "AirLink.Windows.License", "AirLink.Windows.Notices", "AirLink.Windows.Icon" })
                Console.WriteLine("Embedded resource verified: " + entry + " (" + resources[entry].Length + " bytes)");
            Console.WriteLine("Native managed payload verified without loading WPF or executing Windows code: AirLink " + assembly.Version);
            return 0;
        }
        catch (Exception error)
        {
            Console.Error.WriteLine("Native package inspection failed: " + error.Message);
            return 1;
        }
    }
}
