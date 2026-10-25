# AirLink 0.9.0 for Windows

Requires a supported 64-bit Windows 10 or Windows 11 installation. Download **`AirLink-0.9.0-windows-x64-setup.exe`** for an Intel/AMD computer or **`AirLink-0.9.0-windows-arm64-setup.exe`** for Windows on ARM manually from the private [AirLink releases](https://github.com/lucasfd3/Airlink/releases), signed in with a GitHub account that has repository access.

## Install

1. Open the downloaded **Setup** EXE. If AirLink is running, choose **Quit** in its notification-area menu, then retry Setup. Closing the main window alone keeps the app running. Stop an old console service once with **Ctrl+C** if needed.
2. Follow Setup. It installs for your account in **`%LOCALAPPDATA%\Programs\AirLink`** and adds **AirLink** to the Start menu and Windows **Installed apps**. No administrator account, ZIP extraction, separate .NET installation or terminal is required; the matching service and runtime are included.
3. Choose **Open AirLink** when Setup finishes, or open **AirLink** from the Start menu.

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

For development updates, download the newer **Setup** EXE manually from the private Releases page using an authorized GitHub account, then run it. The in-app update action cannot access this private repository anonymously; an unavailable feed does not mean your version is current. Making the repository or update delivery public later requires a new owner decision. Quit the old native app through its notification-area menu if Setup asks, then retry. After confirming the installed app opens from the Start menu, you can manually delete any older downloaded portable AirLink EXE.

The standard `%APPDATA%\AirLink` metadata directory retains identity, pairings, settings and history. `%LOCALAPPDATA%\AirLink` runtime caches and received files are also preserved. During first setup after upgrading, the old conventional `Downloads\AirLink` destination is offered as a change to Downloads; confirm the destination you want. Custom folders remain selected. Already received files are not relocated.

Closing the window keeps AirLink running in the notification area so transfers can continue. Use **Beenden / Quit** there to stop the app and checkpoint work.

To uninstall, open **Windows Settings → Apps → Installed apps → AirLink → Uninstall**. Quit AirLink through its notification-area menu if asked. Uninstallation removes the installed app, Start menu shortcut and uninstall registration. It preserves the application data and caches listed above, received files and other files placed in the installation directory; that directory remains when it contains other files.

This is an unsigned development preview in the private AirLink repository. Only run Setup obtained from that repository's releases. **Settings → Licenses** displays the bundled MIT license and runtime notices. Cross-building/resource checks do not prove installer execution, the new UI, tray, Explorer, clipboard or real network performance on a Windows laptop. The provided installer acceptance test runs silently in a disposable Windows user; completed-run evidence is recorded separately in [TESTING.md](../docs/TESTING.md), and it is not executed on macOS. Native relay administration, detailed statistics and sorting-rule editing remain further interface work.

## Deutsch

Lade **`AirLink-0.9.0-windows-x64-setup.exe`** für einen Intel-/AMD-Rechner oder **`AirLink-0.9.0-windows-arm64-setup.exe`** für Windows auf ARM manuell aus den privaten [AirLink-Releases](https://github.com/lucasfd3/Airlink/releases) herunter. Melde dich dafür bei GitHub mit einem Konto an, das Zugriff auf das Repository hat, und öffne danach **Setup**. Falls AirLink noch läuft, über **Beenden / Quit** im Infobereich beenden und im Setup erneut versuchen. Nur das Hauptfenster zu schließen beendet die App nicht. Ein altes Konsolenfenster bei Bedarf einmal mit **Strg+C** stoppen.

Setup installiert AirLink für dein Benutzerkonto unter **`%LOCALAPPDATA%\Programs\AirLink`** und legt Einträge im **Startmenü** und unter **Installierte Apps** an. Ein Administratorkonto, Entpacken, eine separate .NET-Installation und ein Terminal sind nicht erforderlich. Öffne AirLink nach der Installation über die Abschlussseite oder das Startmenü.

Bei der Einrichtung vergibst du einen verständlichen **Gerätenamen**, zum Beispiel **Laptop Windows**, und wählst den **Zielordner**, normalerweise **Downloads**. Neue Dateien landen direkt dort; bei einem Namenskonflikt erhält die neue Datei einen Zusatz. Bereits empfangene Dateien und laufende ältere Übertragungen behalten ihren Speicherort.

Wähle den Mac in der breiten, verstellbaren Seitenleiste. Ein neues Gerät einmal koppeln, den Fingerabdruck direkt vergleichen und die Anfrage auf dem anderen Gerät bestätigen. Danach Dateien auf das gekoppelte Gerät oder seine beschriftete Ablagefläche ziehen. Die Übertragung startet für diesen Stapel; die Empfangsbestätigung richtet sich nach der ausdrücklich gespeicherten Einstellung des Empfängers.

**Einstellungen** enthält Namen und Zielordner. Die Geräteberechtigungen liegen im **···**-Menü des Geräts. **Übertragungen** zeigt Fortschritt und Ergebnisse; **Datei zeigen** beziehungsweise **Ordner öffnen** führt direkt zum Ergebnis. Quick Share, Zwischenablage und die Handy-Verbindung stehen unter **Weitere Funktionen**. Im Einstellungsbereich findest du auch **Lizenzen**.

Neuere Entwicklungsstände manuell aus den privaten Releases mit einem berechtigten GitHub-Konto herunterladen und das neue **Setup** ausführen. Die Updateprüfung in der App kann das private Repository nicht anonym abrufen; ein nicht erreichbarer Feed bedeutet nicht, dass deine Version aktuell ist. Eine spätere öffentliche Bereitstellung erfordert eine neue Entscheidung des Besitzers. Sobald die installierte App aus dem Startmenü erfolgreich geöffnet wurde, kannst du eine ältere heruntergeladene portable AirLink-EXE manuell löschen. Einstellungen, Identität, Kopplungen und Verlauf unter `%APPDATA%\AirLink`, die Laufzeitdateien unter `%LOCALAPPDATA%\AirLink` und bereits empfangene Dateien bleiben erhalten.

Zum Entfernen **Windows-Einstellungen → Apps → Installierte Apps → AirLink → Deinstallieren** wählen. Die Deinstallation entfernt die installierte App und ihre Startmenü- und Deinstallationseinträge. App-Daten, Laufzeitdateien, empfangene Dateien und andere Dateien im Installationsordner bleiben erhalten. Enthält der Installationsordner solche anderen Dateien, bleibt auch der Ordner bestehen.

Dies ist eine private Vorabversion ohne Authenticode-Signatur. Verwende nur Setup aus den Releases des privaten AirLink-Repositorys. Hintergrundtests und Paketprüfungen ersetzen keine Abnahme auf einem echten Windows-Gerät; die tatsächlich ausgeführten Prüfungen stehen in [TESTING.md](../docs/TESTING.md).
