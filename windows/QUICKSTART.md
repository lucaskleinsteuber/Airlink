# AirLink for Windows

Requires a supported 64-bit Windows 10 or Windows 11 installation (x64 or ARM64).

1. Extract the complete ZIP to a folder of your choice.
2. Open **AirLink.exe**. No terminal, browser, .NET installation, or administrator account is needed.
3. Keep your devices on the same local network. Select your Mac in AirLink's device list and connect it once. Compare the fingerprint shown on the Mac before approving.
4. Drop files or folders into the window, choose the destination device, and press Send. On completion, use **Show file** / **Open folder** to reach the result directly.

**Switching from the old console service:** if the earlier AirLink console is still running, close it once with Ctrl+C before opening this app. Existing settings, pairings, transfer history, and received files are preserved. The native app and old service use the same `%APPDATA%\AirLink` metadata folder. Do not run both at the same time.

Automatic receiving is optional and applies to one trusted sender. Enable it only for a device you recognize. Your choice on Windows controls files arriving on Windows; the Mac has its own choice for files arriving on the Mac.

Closing the main window keeps AirLink running in the notification area so transfers can continue. Use **Quit AirLink** from its notification-area menu to exit. Received files live outside the app folder, normally in your `Downloads\AirLink` folder. The receiving folder is shown in the app and can be changed in Settings.

These are private prerelease builds. The executable is not Authenticode signed. Windows may show an unknown-publisher warning; only run the asset downloaded from the private AirLink repository and compare the published SHA-256 checksum if needed. Real-device testing is still required before a stable 1.0 release.

The native Windows window covers device pairing, sending and receiving, transfer controls, duplicate decisions, received-file actions, and basic settings. Advanced share-link, clipboard, browser-relay, statistics, and sorting-rule management remain available through the existing web interface; they do not yet have full native Windows controls.

All transfers stay in the local network. No account, cloud service, or internet connection is required after extraction. The web interface remains available for phones and tablets.

## Deutsch

ZIP vollständig entpacken und **AirLink.exe** öffnen. Ein Terminal ist für den normalen Betrieb nicht erforderlich. Falls noch der alte Konsolendienst läuft, diesen einmal mit **Strg+C** beenden. Geräte, Einstellungen und empfangene Dateien bleiben erhalten.

Mac und Windows müssen im selben lokalen Netzwerk sein. Das Gerät einmal verbinden, den Fingerabdruck vergleichen und die Kopplung bestätigen. Danach Dateien oder Ordner in das Fenster ziehen und senden. **Datei zeigen** oder **Ordner öffnen** führt nach dem Empfang direkt zum Ergebnis. Automatisches Annehmen lässt sich ausdrücklich pro vertrautem Gerät einschalten.

Das Schließen des Fensters lässt AirLink im Infobereich weiterlaufen. Zum vollständigen Beenden im Menü des Infobereichssymbols **AirLink beenden** wählen.
