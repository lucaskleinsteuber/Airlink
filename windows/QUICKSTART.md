# AirLink 0.8 for Windows

Requires a supported 64-bit Windows 10 or Windows 11 installation. Download the **x64 EXE** for an Intel/AMD computer or the **ARM64 EXE** for Windows on ARM. Run the downloaded EXE directly; the matching service and runtime are included. No ZIP extraction, .NET installation, terminal or administrator account is needed for ordinary use.

## First launch

1. Give this computer a name you will recognize on the Mac, for example **Laptop Windows**.
2. Choose the folder for received files. **Downloads** is the normal choice. Finish setup to save these decisions.
3. Choose your Mac in the device sidebar. For a new pairing, compare its fingerprint directly on the Mac and approve the request there. Existing pairings remain saved.
4. Drop a file or folder onto that paired computer, or use its named drop area and **Choose files** / **Choose folder**. That batch is offered immediately. With no recipient selected, a prepared selection waits for your explicit Send.

New files arrive directly in the chosen folder: `Downloads/report.pdf`, for example. If that name already exists, AirLink chooses a distinct name such as `report (2).pdf`. A sent folder keeps its contents together; a conflicting top-level folder gets a new name rather than merging into the existing folder. Earlier files and transfers keep their saved destinations. **Show file** / **Open folder** opens the actual verified result.

The sidebar can be widened by dragging its divider. Device names wrap onto two lines, with the full name in their tooltip. **Transfers** shows active and recent work. **Settings** contains this PC's name, receive folder, appearance, language and notifications; rate limits and duplicate detection are under Advanced settings. Device-specific permissions are in the selected device's **···** menu. **Quick Share**, **Clipboard** and phone connection details are under **More tools**.

Automatic receiving is an explicit local choice for a trusted sender. Enabling it on Windows controls incoming Mac → Windows files; the Mac has its own independent choice. Ordinary pairing does not enable it automatically.

**Ctrl+O** chooses files while the send view has focus; **Ctrl+Enter** sends an already prepared selection. Direct drops pin their recipient and batch; unrelated prepared files are kept separate. An unchanged retry after a lost offer response recovers the existing retained transfer instead of creating another one.

## Updating and quitting

Quit the old native app through its notification-area menu before replacing/running the new EXE. If the legacy console service is still running, stop it once with **Ctrl+C**. The standard `%APPDATA%\AirLink` metadata directory retains identity, pairings, settings and history. During first setup after upgrading, the old conventional `Downloads\AirLink` destination is offered as a change to Downloads; confirm the destination you want. Custom folders remain selected. Already received files are not relocated.

Closing the window keeps AirLink running in the notification area so transfers can continue. Use **Beenden / Quit** there to stop the app and checkpoint work.

This is a private, unsigned prerelease. Only run the file obtained from the private AirLink repository. **Settings → Licenses** displays the bundled MIT license and runtime notices. Cross-building/resource checks do not prove the new UI, tray, Explorer, clipboard or real network performance on a Windows laptop; those checks are manual. Native relay administration, detailed statistics and sorting-rule editing remain further interface work.

## Deutsch

Lade die passende **EXE** herunter und öffne sie direkt. Beim Update die bisherige App zuerst über **Beenden / Quit** im Infobereich beenden. Ein altes Konsolenfenster bei Bedarf einmal mit **Strg+C** stoppen. Ein Entpacken ist nicht erforderlich.

Bei der Einrichtung vergibst du einen verständlichen **Gerätenamen**, zum Beispiel **Laptop Windows**, und wählst den **Zielordner**, normalerweise **Downloads**. Neue Dateien landen direkt dort; bei einem Namenskonflikt erhält die neue Datei einen Zusatz. Bereits empfangene Dateien und laufende ältere Übertragungen behalten ihren Speicherort.

Wähle den Mac in der breiten, verstellbaren Seitenleiste. Ein neues Gerät einmal koppeln, den Fingerabdruck direkt vergleichen und die Anfrage auf dem anderen Gerät bestätigen. Danach Dateien auf das gekoppelte Gerät oder seine beschriftete Ablagefläche ziehen. Die Übertragung startet für diesen Stapel; die Empfangsbestätigung richtet sich nach der ausdrücklich gespeicherten Einstellung des Empfängers.

**Einstellungen** enthält Namen und Zielordner. Die Geräteberechtigungen liegen im **···**-Menü des Geräts. **Übertragungen** zeigt Fortschritt und Ergebnisse; **Datei zeigen** beziehungsweise **Ordner öffnen** führt direkt zum Ergebnis. Quick Share, Zwischenablage und die Handy-Verbindung stehen unter **Weitere Funktionen**. Im Einstellungsbereich findest du auch **Lizenzen**.
