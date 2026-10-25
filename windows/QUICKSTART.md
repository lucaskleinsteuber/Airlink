# AirLink 0.7 for Windows

Requires a supported 64-bit Windows 10 or Windows 11 installation (x64 or ARM64).

1. Extract the complete ZIP to a folder of your choice and open **AirLink.exe**. No terminal, browser, separate .NET installation or administrator account is needed.
2. Keep both computers on the same local network. Pair the other computer once if it is not already saved: select it, compare the fingerprint shown directly there, and approve the request on that computer. A discovered address is only a connection hint.
3. **Drop files or folders directly onto that paired computer in the device list.** That batch is offered immediately. You can also select the computer and use the named central drop area or **Send files…** / **Send folders…**.
4. If no suitable recipient is selected, files stay prepared. Choose a computer and click **Send**. An explicitly selected offline computer retains its queued offer until reachable. The receiver still approves unless its local automatic-receive policy permits the sender.
5. After receipt, choose **Show file** / **Open folder** in the recent-transfer result to reach the actual saved content.

An empty, idle draft can choose its sole online authorized desktop automatically. An existing recipient or pending selection is never redirected by discovery. A direct drop sends only its batch, preserving unrelated prepared files. Files added or reselected during preparation remain for a later send. If preparation is busy, finish that offer before dropping another batch; a failed offer keeps its sources for an explicit retry.

**Ctrl+O** opens file selection while AirLink has focus; **Ctrl+Enter** sends a prepared selection. These are window shortcuts, not global shortcuts. Double-clicking a paired desktop opens file selection for it.

**Updating:** quit the old native AirLink through its notification-area menu before replacing the executable. If an earlier console service is still running, stop it once with **Ctrl+C**. The native app and old service use the same `%APPDATA%\AirLink` metadata folder, preserving identity, settings, pairings and history. Received files stay outside the app folder, normally under `Downloads\AirLink`; the folder is visible and changeable in Settings. Two services cannot use the same metadata directory at once.

Automatic receiving is optional and applies to one trusted sender. Your choice on Windows controls files arriving on Windows; the Mac has its own independent choice. Ordinary pairing does not silently enable either side.

**Quick Share** creates local links from selected files/folders or eligible completed transfers. Set an optional password, expiry and download limit, copy the complete link, and revoke it from the share list. The host and original source files must stay available; recipients still need a reachable local network and trust in this computer’s certificate. Links do not create internet access.

**Clipboard** supports explicit Paste/Edit/Send/Copy/Dismiss. Enable clipboard permission for the paired device; desktop peers need permission on both sides. Receiving text never reads or replaces the system clipboard automatically. Text is bounded to 64 KiB UTF-8 and remains in memory for up to one hour and 100 messages.

Closing the main window keeps AirLink running in the notification area. When notifications are enabled, requests, pending choices, text and new completions can draw your attention there. Use its menu to reopen AirLink, open the receive folder or quit. Native browser-relay administration, detailed statistics and sorting-rule editing remain further interface work; existing web/macOS controls remain available.

These are private prerelease builds. The executable is not Authenticode signed. Only run the package from the private AirLink repository. Cross-building and background tests do not establish Windows interaction, tray/Explorer behavior, real multicast discovery or physical transfer acceptance; those checks remain manual.

## Deutsch

ZIP vollständig entpacken und **AirLink.exe** öffnen. Beim Update die bisherige native App zuerst über den Infobereich beenden. Falls noch der alte Konsolendienst läuft, diesen einmal mit **Strg+C** beenden. Die separate Identität, Kopplungen, Einstellungen und empfangenen Dateien bleiben erhalten.

Mac und Windows müssen im selben erreichbaren lokalen Netzwerk sein. Bereits gespeicherte Geräte bleiben gekoppelt. Ein neues Gerät einmal verbinden, den Fingerabdruck direkt vergleichen und die Anfrage dort bestätigen.

**Dateien oder Ordner direkt auf den gekoppelten Computer in der Geräteliste ziehen.** Alternativ den Computer wählen und auf die mit seinem Namen beschriftete Fläche ziehen oder **Dateien senden …** verwenden. Dieser Stapel wird direkt angeboten. Ohne geeigneten Empfänger bleibt die Auswahl vorbereitet: Gerät wählen, dann **Senden** beziehungsweise **Strg+Enter**. **Strg+O** öffnet die Dateiauswahl im AirLink-Fenster. Ein offline ausgewählter Computer behält sein Angebot in der Warteschlange.

Nach dem Empfang führt **Datei zeigen** oder **Ordner öffnen** direkt zum Ergebnis. Automatisches Annehmen lässt sich ausdrücklich pro vertrautem Gerät einschalten und gilt jeweils nur auf dem Empfänger. **Quick Share** erstellt lokale Freigabelinks mit optionalem Passwort, Ablauf und Downloadlimit. **Zwischenablage** sendet und kopiert Text nur durch deine ausdrückliche Aktion.

Das Schließen des Fensters lässt AirLink im Infobereich weiterlaufen. Zum vollständigen Beenden dort **Beenden / Quit** wählen. Die neue Bedienung, Benachrichtigungen und Geräteerkennung sind auf dem echten Windows-Laptop noch manuell zu prüfen; es gibt keine UI-Automation.
