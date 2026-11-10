# Runs real silent setup executables in an explicitly disposable Windows user.
# The 0.0.0 baseline is a test fixture, not a historical AirLink release.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$InstallerPath,
    [Parameter(Mandatory = $true)][string]$PayloadPath,
    [Parameter(Mandatory = $true)][ValidatePattern('^\d+\.\d+\.\d+(?:-beta\.[1-9]\d*)?$')][string]$ExpectedVersion,
    [Parameter(Mandatory = $true)][string]$BaselineInstallerPath,
    [Parameter(Mandatory = $true)][string]$BaselinePayloadPath,
    [ValidatePattern('^\d+\.\d+\.\d+(?:-beta\.[1-9]\d*)?$')][string]$BaselineVersion = '0.0.0',
    [string]$ReportPath
)

Set-StrictMode -Version 3.0
$ErrorActionPreference = 'Stop'

if ($env:OS -ne 'Windows_NT') {
    throw 'Installer acceptance requires Windows; no setup executable was started.'
}
if ($env:GITHUB_ACTIONS -ne 'true' -and $env:AIRLINK_ALLOW_INSTALLER_TEST -ne '1') {
    throw 'Use a disposable Windows CI user. Explicit opt-in requires GITHUB_ACTIONS=true or AIRLINK_ALLOW_INSTALLER_TEST=1.'
}
function Compare-ReleaseVersion([string]$Left, [string]$Right) {
    $a = $Left.Split('-', 2); $b = $Right.Split('-', 2)
    $core = ([Version]$a[0]).CompareTo([Version]$b[0])
    if ($core -ne 0) { return $core }
    if ($a.Count -eq 1 -and $b.Count -eq 1) { return 0 }
    if ($a.Count -eq 1) { return 1 }
    if ($b.Count -eq 1) { return -1 }
    return ([System.Numerics.BigInteger]::Parse($a[1].Substring(5))).CompareTo([System.Numerics.BigInteger]::Parse($b[1].Substring(5)))
}
if ((Compare-ReleaseVersion $ExpectedVersion $BaselineVersion) -le 0) {
    throw 'The release version must be newer than the explicit baseline fixture version.'
}
foreach ($pair in @(@('0.12.0-beta.1', '0.12.0-beta.2'), @('0.12.0-beta.2', '0.12.0-beta.10'), @('0.12.0-beta.10', '0.12.0'))) {
    if ((Compare-ReleaseVersion $pair[0] $pair[1]) -ge 0) { throw 'Installer beta ordering is incorrect.' }
}
if (-not [Environment]::Is64BitOperatingSystem) {
    throw 'This acceptance test requires a 64-bit Windows runner.'
}
if ([Runtime.InteropServices.RuntimeInformation]::OSArchitecture -ne [Runtime.InteropServices.Architecture]::X64) {
    throw 'This installer acceptance job requires native x64 Windows; ARM64 runtime acceptance is separate.'
}

function Resolve-InputFile([string]$Path) {
    $item = Get-Item -LiteralPath $Path
    if ($item.PSIsContainer) { throw 'An installer or payload input is a directory.' }
    return $item.FullName
}

function Assert-True([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw $Message }
}

function Assert-PathEqual([string]$Actual, [string]$Expected, [string]$Message) {
    Assert-True (-not [string]::IsNullOrWhiteSpace($Actual)) $Message
    $left = [IO.Path]::GetFullPath($Actual).TrimEnd([char[]]'\/')
    $right = [IO.Path]::GetFullPath($Expected).TrimEnd([char[]]'\/')
    Assert-True ([string]::Equals($left, $right, [StringComparison]::OrdinalIgnoreCase)) $Message
}

function Read-UninstallEntry([Microsoft.Win32.RegistryHive]$Hive, [Microsoft.Win32.RegistryView]$View = [Microsoft.Win32.RegistryView]::Registry64) {
    $base = [Microsoft.Win32.RegistryKey]::OpenBaseKey($Hive, $View)
    $key = $null
    try {
        $key = $base.OpenSubKey($script:UninstallKey)
        if ($null -eq $key) { return $null }
        $values = @{}
        foreach ($name in $key.GetValueNames()) {
            $values[$name] = $key.GetValue($name, $null, [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
        }
        return $values
    }
    finally {
        if ($null -ne $key) { $key.Dispose() }
        $base.Dispose()
    }
}

function Assert-NoMachineRegistration {
    foreach ($view in @([Microsoft.Win32.RegistryView]::Registry32, [Microsoft.Win32.RegistryView]::Registry64)) {
        Assert-True ($null -eq (Read-UninstallEntry ([Microsoft.Win32.RegistryHive]::LocalMachine) $view)) 'Setup created a machine-wide uninstall registration.'
    }
}

function Assert-NoDesktopProcess {
    $processes = @(Get-Process -Name 'AirLink' -ErrorAction SilentlyContinue)
    Assert-True ($processes.Count -eq 0) 'An AirLink process is running; this test never stops desktop processes.'
}

function Invoke-SilentExecutable([string]$Path, [int]$ExpectedExitCode, [string]$Step, [switch]$UninstallerInPlace) {
    # Start-Process -Wait also waits for descendant processes on Windows. This
    # matters for NSIS's temporary uninstaller copy. /S must never launch AirLink.
    $watch = [Diagnostics.Stopwatch]::StartNew()
    $arguments = '/S'
    if ($UninstallerInPlace) {
        # NSIS's original uninstaller process otherwise self-relocates before
        # un.onInit and cannot propagate the temporary child's busy exit code.
        # _?= consumes the remaining command line, including spaces. Use it only
        # when no deletion can occur because this test holds the desktop mutex.
        $arguments += ' _?=' + $script:InstallDirectory
    }
    $process = Start-Process -FilePath $Path -ArgumentList $arguments -PassThru -Wait -WindowStyle Hidden
    $process.Refresh()
    $code = $process.ExitCode
    $process.Dispose()
    $watch.Stop()
    $script:Results.Add([ordered]@{ step = $Step; exitCode = $code; elapsedMilliseconds = $watch.ElapsedMilliseconds })
    Assert-True ($code -eq $ExpectedExitCode) ($Step + ': unexpected exit code; expected ' + $ExpectedExitCode + ', received ' + $code + '.')
    Assert-NoDesktopProcess
}

function With-DesktopMutex([scriptblock]$Body) {
    $created = $false
    $mutex = [Threading.Mutex]::new($true, $script:DesktopMutexName, [ref]$created)
    try {
        Assert-True $created 'The desktop mutex already exists; this user is not isolated.'
        & $Body
    }
    finally {
        if ($created) { $mutex.ReleaseMutex() }
        $mutex.Dispose()
    }
}

function Assert-Markers {
    foreach ($marker in $script:Markers) {
        Assert-True (Test-Path -LiteralPath $marker.path -PathType Leaf) ('A preserved ' + $marker.label + ' marker is missing.')
        Assert-True ((Get-FileHash -LiteralPath $marker.path -Algorithm SHA256).Hash -eq $marker.hash) ('A preserved ' + $marker.label + ' marker changed.')
    }
}

function Assert-Installation([string]$Version, [string]$PayloadHash) {
    Assert-True (Test-Path -LiteralPath $script:InstalledExe -PathType Leaf) 'The installed AirLink executable is missing.'
    Assert-True ((Get-FileHash -LiteralPath $script:InstalledExe -Algorithm SHA256).Hash -eq $PayloadHash) 'The installed payload differs from the independently hashed input.'
    Assert-True (Test-Path -LiteralPath $script:Uninstaller -PathType Leaf) 'The installed uninstaller is missing.'
    Assert-True (Test-Path -LiteralPath $script:Shortcut -PathType Leaf) 'The current-user Start menu shortcut is missing.'

    $shell = New-Object -ComObject WScript.Shell
    $link = $null
    try {
        $link = $shell.CreateShortcut($script:Shortcut)
        Assert-PathEqual $link.TargetPath $script:InstalledExe 'The Start menu shortcut targets another executable.'
        Assert-PathEqual $link.WorkingDirectory $script:InstallDirectory 'The Start menu shortcut has an unexpected working directory.'
    }
    finally {
        if ($null -ne $link) { [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($link) }
        [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($shell)
    }

    $entry = Read-UninstallEntry ([Microsoft.Win32.RegistryHive]::CurrentUser)
    Assert-True ($null -ne $entry) 'The current-user uninstall registration is missing.'
    Assert-True ($entry['DisplayName'] -eq 'AirLink') 'The uninstall display name is incorrect.'
    Assert-True ($entry['DisplayVersion'] -eq $Version) 'The uninstall version is incorrect.'
    Assert-True ($entry['Publisher'] -eq 'lucasfd3') 'The uninstall publisher is incorrect.'
    Assert-PathEqual $entry['InstallLocation'] $script:InstallDirectory 'The registered install location is incorrect.'
    Assert-True ($entry['UninstallString'] -eq ('"' + $script:Uninstaller + '"')) 'The registered uninstaller command is incorrect or unquoted.'
    Assert-True ($entry['QuietUninstallString'] -eq ('"' + $script:Uninstaller + '" /S')) 'The registered silent uninstaller command is incorrect.'
    Assert-NoMachineRegistration
    Assert-Markers
    Assert-NoDesktopProcess
}

function Wait-ForUninstall {
    $deadline = [DateTime]::UtcNow.AddSeconds(20)
    do {
        $complete = -not (Test-Path -LiteralPath $script:InstalledExe) -and
            -not (Test-Path -LiteralPath $script:Uninstaller) -and
            -not (Test-Path -LiteralPath $script:Shortcut) -and
            $null -eq (Read-UninstallEntry ([Microsoft.Win32.RegistryHive]::CurrentUser))
        if ($complete) { return }
        Start-Sleep -Milliseconds 100
    } while ([DateTime]::UtcNow -lt $deadline)
    throw 'Silent uninstall did not remove its owned executable, uninstaller, shortcut and registration.'
}

function Remove-EmptyFixtureDirectory([string]$Path) {
    if ((Test-Path -LiteralPath $Path -PathType Container) -and @(Get-ChildItem -LiteralPath $Path -Force).Count -eq 0) {
        [IO.Directory]::Delete($Path, $false)
    }
}

$InstallerPath = Resolve-InputFile $InstallerPath
$PayloadPath = Resolve-InputFile $PayloadPath
$BaselineInstallerPath = Resolve-InputFile $BaselineInstallerPath
$BaselinePayloadPath = Resolve-InputFile $BaselinePayloadPath
$releaseHash = (Get-FileHash -LiteralPath $PayloadPath -Algorithm SHA256).Hash
$baselineHash = (Get-FileHash -LiteralPath $BaselinePayloadPath -Algorithm SHA256).Hash
Assert-True ($releaseHash -ne $baselineHash) 'The baseline fixture and release payload must differ to prove replacement.'

$local = [Environment]::GetFolderPath([Environment+SpecialFolder]::LocalApplicationData)
$roaming = [Environment]::GetFolderPath([Environment+SpecialFolder]::ApplicationData)
$programs = [Environment]::GetFolderPath([Environment+SpecialFolder]::Programs)
Assert-True (-not [string]::IsNullOrWhiteSpace($local) -and -not [string]::IsNullOrWhiteSpace($roaming) -and -not [string]::IsNullOrWhiteSpace($programs)) 'Required current-user folders are unavailable.'
$script:InstallDirectory = Join-Path $local 'Programs\AirLink'
$script:InstalledExe = Join-Path $script:InstallDirectory 'AirLink.exe'
$script:Uninstaller = Join-Path $script:InstallDirectory 'Uninstall.exe'
$script:Shortcut = Join-Path $programs 'AirLink.lnk'
$script:UninstallKey = 'Software\Microsoft\Windows\CurrentVersion\Uninstall\AirLink'
$script:DesktopMutexName = 'Local\lucasfd3.AirLink.Desktop'
$roamingState = Join-Path $roaming 'AirLink'
$localState = Join-Path $local 'AirLink'

# Opt-in alone is insufficient: never operate on an existing user's app, data,
# shortcut or registration, including a machine-wide installation.
foreach ($path in @($script:InstallDirectory, $roamingState, $localState, $script:Shortcut)) {
    Assert-True (-not (Test-Path -LiteralPath $path)) 'Existing AirLink installation or user data detected; use a fresh disposable Windows user.'
}
foreach ($hive in @([Microsoft.Win32.RegistryHive]::CurrentUser, [Microsoft.Win32.RegistryHive]::LocalMachine)) {
    foreach ($view in @([Microsoft.Win32.RegistryView]::Registry32, [Microsoft.Win32.RegistryView]::Registry64)) {
        Assert-True ($null -eq (Read-UninstallEntry $hive $view)) 'An existing AirLink uninstall registration was found; refusing to modify it.'
    }
}
Assert-NoDesktopProcess
With-DesktopMutex { }

$userShellFolders = Get-ItemProperty -LiteralPath 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Explorer\User Shell Folders'
$downloadsValue = $userShellFolders.'{374DE290-123F-4565-9164-39C4925E467B}'
Assert-True (-not [string]::IsNullOrWhiteSpace($downloadsValue)) 'The current-user Downloads known folder is unavailable.'
$downloads = [Environment]::ExpandEnvironmentVariables($downloadsValue)
Assert-True (Test-Path -LiteralPath $downloads -PathType Container) 'The Downloads folder must already exist; this test does not relocate it.'

$fixtureId = [Guid]::NewGuid().ToString('N')
$script:Markers = [Collections.Generic.List[object]]::new()
$script:Results = [Collections.Generic.List[object]]::new()
$success = $false
$failure = $null
try {
    foreach ($item in @(
        @{ root = $roamingState; label = 'roaming application data' },
        @{ root = $localState; label = 'local runtime cache' },
        @{ root = $downloads; label = 'download' }
    )) {
        [void][IO.Directory]::CreateDirectory($item.root)
        $marker = Join-Path $item.root ('installer-preserve-' + $fixtureId + '.txt')
        [IO.File]::WriteAllText($marker, 'AirLink disposable installer acceptance marker ' + $fixtureId)
        $script:Markers.Add(@{ path = $marker; label = $item.label; hash = (Get-FileHash -LiteralPath $marker -Algorithm SHA256).Hash })
    }

    Invoke-SilentExecutable $BaselineInstallerPath 0 'install synthetic baseline'
    Assert-Installation $BaselineVersion $baselineHash

    $unknownFile = Join-Path $script:InstallDirectory ('unknown-user-file-' + $fixtureId + '.txt')
    [IO.File]::WriteAllText($unknownFile, 'Unknown install-directory file must survive upgrade and uninstall ' + $fixtureId)
    $script:Markers.Add(@{ path = $unknownFile; label = 'unknown install-directory file'; hash = (Get-FileHash -LiteralPath $unknownFile -Algorithm SHA256).Hash })

    With-DesktopMutex {
        Invoke-SilentExecutable $InstallerPath 1618 'reject upgrade while desktop mutex is held'
    }
    Assert-Installation $BaselineVersion $baselineHash

    $entriesBeforeFailure = @(Get-ChildItem -LiteralPath $script:InstallDirectory -Force | ForEach-Object { $_.Name } | Sort-Object)
    $payloadLock = [IO.File]::Open($script:InstalledExe, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
    try {
        Invoke-SilentExecutable $InstallerPath 1603 'reject replacement of a locked installed payload'
        Assert-Installation $BaselineVersion $baselineHash
    }
    finally {
        $payloadLock.Dispose()
    }
    $entriesAfterFailure = @(Get-ChildItem -LiteralPath $script:InstallDirectory -Force | ForEach-Object { $_.Name } | Sort-Object)
    Assert-True (@(Compare-Object -ReferenceObject $entriesBeforeFailure -DifferenceObject $entriesAfterFailure).Count -eq 0) 'A failed upgrade left unexpected staging files or removed installation files.'

    Invoke-SilentExecutable $InstallerPath 0 'upgrade baseline to release'
    Assert-Installation $ExpectedVersion $releaseHash

    With-DesktopMutex {
        Invoke-SilentExecutable $script:Uninstaller 1618 'reject uninstall while desktop mutex is held' -UninstallerInPlace
    }
    Assert-Installation $ExpectedVersion $releaseHash

    Invoke-SilentExecutable $script:Uninstaller 0 'uninstall release'
    Wait-ForUninstall
    Assert-Markers
    Assert-NoDesktopProcess
    Assert-NoMachineRegistration
    $success = $true
}
catch {
    $failure = $_.Exception.Message
    throw
}
finally {
    # On failure, retain evidence and never attempt a second destructive action.
    # On success, remove only this script's known marker files and empty roots.
    if ($success) {
        foreach ($marker in $script:Markers) { [IO.File]::Delete($marker.path) }
        foreach ($directory in @($script:InstallDirectory, $roamingState, $localState)) {
            Remove-EmptyFixtureDirectory $directory
        }
    }
    $report = [ordered]@{
        test = 'real silent per-user Windows setup lifecycle'
        success = $success
        baselineVersion = $BaselineVersion
        releaseVersion = $ExpectedVersion
        baselinePayloadSHA256 = $baselineHash
        releasePayloadSHA256 = $releaseHash
        steps = @($script:Results.ToArray())
        failure = $failure
        scope = 'Disposable Windows user, actual setup install/upgrade/uninstall, no app launch or GUI automation. Does not prove desktop or ARM64 runtime behavior.'
    }
    if (-not [string]::IsNullOrWhiteSpace($ReportPath)) {
        $parent = Split-Path -Parent ([IO.Path]::GetFullPath($ReportPath))
        [void][IO.Directory]::CreateDirectory($parent)
        $report | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $ReportPath -Encoding UTF8
    }
}

Write-Output 'Windows silent installer lifecycle passed: install, blocked running-app changes, upgrade, uninstall and user-file preservation.'
