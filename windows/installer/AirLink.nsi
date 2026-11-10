# -*- coding: utf-8 -*-
; Headless-built, per-user installer. Packaged application code is never executed
; by compilation. All filesystem deletion is limited to explicit owned files.
Unicode true
RequestExecutionLevel user
ManifestDPIAware true
CRCCheck force
SetCompressor /SOLID lzma
SetDatablockOptimize on

!include "MUI2.nsh"
!include "LogicLib.nsh"

!ifndef AIRLINK_VERSION
  !error "AIRLINK_VERSION is required"
!endif
!ifndef AIRLINK_ARCH
  !error "AIRLINK_ARCH must be x64 or arm64"
!endif
!ifndef AIRLINK_PAYLOAD
  !error "AIRLINK_PAYLOAD must identify the self-contained native AirLink.exe"
!endif
!ifndef AIRLINK_OUTPUT
  !error "AIRLINK_OUTPUT is required"
!endif
!ifndef AIRLINK_ICON
  !error "AIRLINK_ICON is required"
!endif
!if "${AIRLINK_ARCH}" == "x64"
  !define AIRLINK_MACHINE 0x8664
!else if "${AIRLINK_ARCH}" == "arm64"
  !define AIRLINK_MACHINE 0xaa64
!else
  !error "Unsupported AIRLINK_ARCH; use x64 or arm64"
!endif

!define UNINSTALL_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\AirLink"
!define DESKTOP_MUTEX "Local\lucasfd3.AirLink.Desktop"

Name "AirLink ${AIRLINK_VERSION}"
OutFile "${AIRLINK_OUTPUT}"
InstallDir "$LOCALAPPDATA\Programs\AirLink"
BrandingText "AirLink"
Icon "${AIRLINK_ICON}"
UninstallIcon "${AIRLINK_ICON}"
!ifndef AIRLINK_NUMERIC_VERSION
  !define AIRLINK_NUMERIC_VERSION "${AIRLINK_VERSION}.0"
!endif
VIProductVersion "${AIRLINK_NUMERIC_VERSION}"
VIAddVersionKey "ProductName" "AirLink"
VIAddVersionKey "FileDescription" "AirLink per-user setup"
VIAddVersionKey "FileVersion" "${AIRLINK_VERSION}"
VIAddVersionKey "ProductVersion" "${AIRLINK_VERSION}"
VIAddVersionKey "CompanyName" "Lucas Kleinsteuber"
VIAddVersionKey "LegalCopyright" "Copyright (c) 2026 Lucas Kleinsteuber"

Var DesktopMutex
Var StagedPayload
Var StagedUninstaller

!define MUI_ABORTWARNING
!define MUI_CUSTOMFUNCTION_ABORT InstallerUserAbort
!define MUI_CUSTOMFUNCTION_UNABORT un.UserAbort
!define MUI_WELCOMEPAGE_TITLE "$(WelcomeTitle)"
!define MUI_WELCOMEPAGE_TEXT "$(WelcomeText)"
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_TITLE "$(FinishTitle)"
!define MUI_FINISHPAGE_TEXT "$(FinishText)"
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_FUNCTION LaunchAirLink
!define MUI_FINISHPAGE_RUN_TEXT "$(LaunchText)"
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_UNPAGE_FINISH

; German is the fallback; NSIS chooses the matching Windows UI language.
!insertmacro MUI_LANGUAGE "German"
!insertmacro MUI_LANGUAGE "English"
LangString WelcomeTitle ${LANG_GERMAN} "AirLink installieren"
LangString WelcomeTitle ${LANG_ENGLISH} "Install AirLink"
LangString WelcomeText ${LANG_GERMAN} "AirLink wird für dein Benutzerkonto installiert.$\r$\n$\r$\nSpeicherort: $LOCALAPPDATA\Programs\AirLink$\r$\n$\r$\nDu findest AirLink anschließend im Startmenü und unter Installierte Apps. Vorhandene Einstellungen, Kopplungen und empfangene Dateien bleiben erhalten."
LangString WelcomeText ${LANG_ENGLISH} "AirLink will be installed for your user account.$\r$\n$\r$\nLocation: $LOCALAPPDATA\Programs\AirLink$\r$\n$\r$\nAirLink will appear in the Start menu and Installed apps. Existing settings, pairings and received files are preserved."
LangString FinishTitle ${LANG_GERMAN} "AirLink ist bereit"
LangString FinishTitle ${LANG_ENGLISH} "AirLink is ready"
LangString FinishText ${LANG_GERMAN} "AirLink wurde installiert. Du kannst die App jetzt oder später über das Startmenü öffnen."
LangString FinishText ${LANG_ENGLISH} "AirLink has been installed. Open it now or later from the Start menu."
LangString LaunchText ${LANG_GERMAN} "AirLink öffnen"
LangString LaunchText ${LANG_ENGLISH} "Open AirLink"
LangString RunningText ${LANG_GERMAN} "AirLink läuft noch. Beende AirLink über das Symbol im Infobereich und wähle Erneut versuchen.$\r$\n$\r$\nEin geschlossenes Hauptfenster allein beendet die App nicht. Laufende Übertragungen werden nicht erzwungen beendet."
LangString RunningText ${LANG_ENGLISH} "AirLink is still running. Quit AirLink from its system-tray icon, then choose Retry.$\r$\n$\r$\nClosing the main window does not quit the app. Active transfers will not be forcibly stopped."
LangString ArchitectureText ${LANG_GERMAN} "Dieses Installationsprogramm ist für Windows ${AIRLINK_ARCH}. Verwende die passende x64- oder ARM64-Ausgabe auf einem unterstützten Windows-System."
LangString ArchitectureText ${LANG_ENGLISH} "This installer is for Windows ${AIRLINK_ARCH}. Use the matching x64 or ARM64 edition on a supported Windows system."
LangString InstallError ${LANG_GERMAN} "AirLink konnte nicht vollständig installiert werden. Prüfe den freien Speicherplatz und die Schreibrechte und versuche es erneut. Eine vorhandene App wird erst nach vollständiger Vorbereitung der neuen Datei ersetzt."
LangString InstallError ${LANG_ENGLISH} "AirLink could not be completely installed. Check available disk space and write access, then try again. An existing app is replaced only after the new file has been fully staged."
LangString UninstallError ${LANG_GERMAN} "AirLink konnte nicht vollständig entfernt werden. Beende die App und versuche es erneut. Einstellungen und empfangene Dateien bleiben erhalten."
LangString UninstallError ${LANG_ENGLISH} "AirLink could not be completely removed. Quit the app and try again. Settings and received files are preserved."
LangString MutexError ${LANG_GERMAN} "AirLink konnte nicht sicher für die Installation oder Entfernung gesperrt werden. Versuche es erneut."
LangString MutexError ${LANG_ENGLISH} "AirLink could not be safely locked for installation or removal. Please try again."

!macro MutexFunctions PREFIX
Function ${PREFIX}AcquireDesktopMutex
  retry:
    System::Call 'kernel32::SetLastError(i 0)'
    System::Call 'kernel32::CreateMutexW(p 0, i 1, w "${DESKTOP_MUTEX}") p.r0 ?e'
    Pop $1
    StrCmp $0 0 mutex_error
    StrCmp $1 183 already_running
    StrCpy $DesktopMutex $0
    Return
  already_running:
    System::Call 'kernel32::CloseHandle(p r0)'
    IfSilent busy
    MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "$(RunningText)" /SD IDCANCEL IDRETRY retry
  busy:
    SetErrorLevel 1618
    Quit
  mutex_error:
    IfSilent +2
      MessageBox MB_OK|MB_ICONSTOP "$(MutexError)"
    SetErrorLevel 1603
    Quit
FunctionEnd

Function ${PREFIX}ReleaseDesktopMutex
  StrCmp $DesktopMutex "" done
  StrCmp $DesktopMutex 0 done
  System::Call 'kernel32::ReleaseMutex(p $DesktopMutex)'
  System::Call 'kernel32::CloseHandle(p $DesktopMutex)'
  StrCpy $DesktopMutex 0
  done:
FunctionEnd
!macroend
!insertmacro MutexFunctions ""
!insertmacro MutexFunctions "un."

Function CheckArchitecture
  ; IsWow64Process2 reports the true OS architecture even for this x86 bootstrap
  ; under ARM64/x64 emulation. The native payload must match that architecture.
  System::Call 'kernel32::GetModuleHandleW(w "kernel32.dll") p.r0'
  System::Call 'kernel32::GetProcAddress(p r0, m "IsWow64Process2") p.r1'
  StrCmp $1 0 unsupported
  System::Call '::$1(p -1, *i 0.r2, *i 0.r3) i.r4'
  StrCmp $4 0 unsupported
  IntOp $3 $3 & 0xffff
  IntCmp $3 ${AIRLINK_MACHINE} supported unsupported unsupported
  unsupported:
    IfSilent +2
      MessageBox MB_OK|MB_ICONSTOP "$(ArchitectureText)"
    SetErrorLevel 1633
    Quit
  supported:
FunctionEnd

Function .onInit
  SetShellVarContext current
  SetRegView 64
  ; Deliberately ignore /D and historical locations: this installer manages only
  ; one fixed per-user application directory, never a downloaded portable copy.
  StrCpy $INSTDIR "$LOCALAPPDATA\Programs\AirLink"
  ReadRegDWORD $0 HKCU "${UNINSTALL_KEY}" "InstallerLanguage"
  ${If} $0 == ${LANG_GERMAN}
  ${OrIf} $0 == ${LANG_ENGLISH}
    StrCpy $LANGUAGE $0
  ${EndIf}
  Call CheckArchitecture
  Call AcquireDesktopMutex
FunctionEnd

Function CleanupStaging
  ${If} $StagedPayload != ""
    Delete "$StagedPayload"
    StrCpy $StagedPayload ""
  ${EndIf}
  ${If} $StagedUninstaller != ""
    Delete "$StagedUninstaller"
    StrCpy $StagedUninstaller ""
  ${EndIf}
FunctionEnd

Function LaunchAirLink
  IfSilent done
  Call ReleaseDesktopMutex
  SetOutPath "$INSTDIR"
  Exec '"$INSTDIR\AirLink.exe"'
  done:
FunctionEnd

Function .onGUIEnd
  Call CleanupStaging
  Call ReleaseDesktopMutex
FunctionEnd

Function InstallerUserAbort
  SetErrorLevel 1602
  Call CleanupStaging
  Call ReleaseDesktopMutex
FunctionEnd

Function .onInstFailed
  SetErrorLevel 1603
  Call CleanupStaging
  Call ReleaseDesktopMutex
FunctionEnd

Section "AirLink" InstallSection
  StrCpy $INSTDIR "$LOCALAPPDATA\Programs\AirLink"
  ClearErrors
  SetOutPath "$INSTDIR"
  IfErrors failed
  ; GetTempFileName creates unique files inside the destination volume. Only
  ; these exact temporary paths are cleaned; no wildcard/recursive deletion.
  GetTempFileName $StagedPayload "$INSTDIR"
  IfErrors failed
  GetTempFileName $StagedUninstaller "$INSTDIR"
  IfErrors failed
  SetOverwrite on
  ClearErrors
  File "/oname=$StagedPayload" "${AIRLINK_PAYLOAD}"
  IfErrors failed
  WriteUninstaller "$StagedUninstaller"
  IfErrors failed
  ; Both files are fully written (and compressed payload CRC checked) before
  ; replacement. Same-volume MoveFileEx does not copy/truncate the old target.
  ; Commit the backward-compatible uninstaller first, keeping the old app if
  ; the final atomic payload replacement fails. No old app backup is retained.
  System::Call 'kernel32::MoveFileExW(w "$StagedUninstaller", w "$INSTDIR\Uninstall.exe", i 9) i.r0'
  StrCmp $0 0 failed
  StrCpy $StagedUninstaller ""
  System::Call 'kernel32::MoveFileExW(w "$StagedPayload", w "$INSTDIR\AirLink.exe", i 9) i.r0'
  StrCmp $0 0 failed
  StrCpy $StagedPayload ""
  ClearErrors
  CreateShortCut "$SMPROGRAMS\AirLink.lnk" "$INSTDIR\AirLink.exe" "" "$INSTDIR\AirLink.exe" 0
  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayName" "AirLink"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayVersion" "${AIRLINK_VERSION}"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "Publisher" "lucasfd3"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayIcon" '$\"$INSTDIR\AirLink.exe$\",0'
  WriteRegStr HKCU "${UNINSTALL_KEY}" "UninstallString" '$\"$INSTDIR\Uninstall.exe$\"'
  WriteRegStr HKCU "${UNINSTALL_KEY}" "QuietUninstallString" '$\"$INSTDIR\Uninstall.exe$\" /S'
  WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoRepair" 1
  WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallerLanguage" $LANGUAGE
  IfErrors failed
  Call ReleaseDesktopMutex
  SetErrorLevel 0
  Goto done
  failed:
    Call CleanupStaging
    Call ReleaseDesktopMutex
    IfSilent +2
      MessageBox MB_OK|MB_ICONSTOP "$(InstallError)"
    SetErrorLevel 1603
    Abort
  done:
SectionEnd

Function un.onInit
  SetShellVarContext current
  SetRegView 64
  StrCpy $INSTDIR "$LOCALAPPDATA\Programs\AirLink"
  ReadRegDWORD $0 HKCU "${UNINSTALL_KEY}" "InstallerLanguage"
  ${If} $0 == ${LANG_GERMAN}
  ${OrIf} $0 == ${LANG_ENGLISH}
    StrCpy $LANGUAGE $0
  ${EndIf}
  Call un.AcquireDesktopMutex
FunctionEnd

Function un.onGUIEnd
  Call un.ReleaseDesktopMutex
FunctionEnd

Function un.UserAbort
  SetErrorLevel 1602
  Call un.ReleaseDesktopMutex
FunctionEnd

Function un.onUninstFailed
  SetErrorLevel 1603
  Call un.ReleaseDesktopMutex
FunctionEnd

Section "Uninstall"
  StrCpy $INSTDIR "$LOCALAPPDATA\Programs\AirLink"
  ClearErrors
  Delete "$INSTDIR\AirLink.exe"
  IfErrors failed
  Delete "$SMPROGRAMS\AirLink.lnk"
  DeleteRegKey HKCU "${UNINSTALL_KEY}"
  IfErrors failed
  ; NSIS normally executes its uninstaller from a temporary copy. _?= is the
  ; documented caller-managed alternative when a deterministic exit is needed.
  Delete "$INSTDIR\Uninstall.exe"
  ; Never remove runtime/cache/received data or unknown installation-dir files.
  SetOutPath "$TEMP"
  RMDir "$INSTDIR"
  Call un.ReleaseDesktopMutex
  SetErrorLevel 0
  Goto done
  failed:
    Call un.ReleaseDesktopMutex
    IfSilent +2
      MessageBox MB_OK|MB_ICONSTOP "$(UninstallError)"
    SetErrorLevel 1603
    Abort
  done:
SectionEnd
