# Manueller Gerätetest: Mac, Windows, iPhone und iPad

Dieser Leitfaden ist für deinen Mac, den Windows-Laptop, dein iPhone und dein iPad. Die Schritte sind **noch nicht als durchgeführt dokumentiert**. Du bedienst die Geräte selbst; es gibt keine UI-Automation. Verwende zunächst kleine, eigens angelegte Testdateien. Große Dateien im echten WLAN/LAN folgen danach.

Nutze auf Mac und Windows dieselbe zu prüfende AirLink-Version. Notiere Version, Betriebssystem und Browser. Alle Geräte müssen im selben erreichbaren lokalen Netzwerk sein; ein Gäste-WLAN kann Verbindungen zwischen Geräten sperren. Lass AirLink auf dem Mac und den Windows-Dienst während der jeweiligen Tests laufen. Installation und Start sind in der [README](../README.md#choose-how-to-run-it) beschrieben.

## 1. Mac ↔ Windows: Dateien, Ordner und Wiederaufnahme

Lege auf dem Mac einen kleinen Testordner an, zum Beispiel im Terminal:

```sh
mkdir -p "$HOME/Desktop/AirLink-Test/Leer"
printf 'AirLink Test 1\n' > "$HOME/Desktop/AirLink-Test/Grüße.txt"
dd if=/dev/urandom of="$HOME/Desktop/AirLink-Test/Test-16MiB.bin" bs=1048576 count=16
shasum -a256 "$HOME/Desktop/AirLink-Test/Grüße.txt"
shasum -a256 "$HOME/Desktop/AirLink-Test/Test-16MiB.bin"
```

- [ ] Starte den Windows-Dienst und öffne seine vollständige private Konsolenadresse auf dem Windows-Laptop. Kopple Mac und Windows über die Geräteverbindung. Vergleiche den Zertifikatsfingerabdruck direkt auf beiden Computern und bestätige die Anfrage.
- [ ] Sende zuerst die Textdatei, dann den ganzen Ordner vom Mac an Windows. Bestätige das Angebot auf dem Empfänger. Prüfe Inhalt, Namen, Unterordner und den leeren Ordner `Leer`.
- [ ] Berechne auf Windows für jede empfangene Datei die SHA-256-Prüfsumme. Ersetze den Beispielpfad durch den tatsächlichen Empfangspfad:

```powershell
Get-FileHash -Algorithm SHA256 -LiteralPath "C:\tatsaechlicher\Empfangspfad\Test-16MiB.bin"
Get-FileHash -Algorithm SHA256 -LiteralPath "C:\tatsaechlicher\Empfangspfad\Grüße.txt"
```

- [ ] Vergleiche jeweils alle 64 Zeichen mit dem Mac-Ergebnis. Groß-/Kleinschreibung der Hex-Zeichen spielt keine Rolle. Sende anschließend den empfangenen Ordner von Windows zurück an den Mac und prüfe dort erneut mit `shasum -a256` und dem tatsächlichen Rückempfangspfad.
- [ ] Teste bei einem ausreichend langen Transfer **Pause → Fortsetzen**. Unterbrich danach kurz die Netzwerkverbindung eines Laptops und stelle sie wieder her. AirLink soll vom bestätigten Stand weiterarbeiten; vergleiche die fertige Datei erneut per SHA-256. Kleine Dateien können dafür zu schnell sein: Bei Bedarf vorübergehend ein niedriges Geschwindigkeitslimit einstellen und anschließend den vorherigen Wert wiederherstellen.

## 2. Mac ↔ iPhone: Safari, Zertifikat und Datei-Rundweg

- [ ] Öffne auf dem Mac den **Setup-QR-Code** und scanne ihn mit dem iPhone. Die erste Einrichtungsseite ist HTTP und liefert nur das öffentliche Zertifikat und Hinweise. Vergleiche ihren SHA-256-Fingerabdruck mit der Anzeige direkt in AirLink auf dem Mac, bevor du Vertrauen einrichtest.
- [ ] Lade das iOS-Zertifikatsprofil. Installiere es unter **Einstellungen → Allgemein → VPN und Geräteverwaltung**. Aktiviere anschließend das volle Vertrauen für dieses Zertifikat unter **Allgemein → Info → Zertifikatsvertrauenseinstellungen**. Öffne danach den HTTPS-Link des Macs in Safari.
- [ ] Fordere in Safari die Kopplung an und bestätige das iPhone auf dem Mac. Ein QR-Code allein erteilt keinen Zugriff.
- [ ] Sende die kleine Textdatei und `Test-16MiB.bin` vom Mac an das gekoppelte iPhone. Lade die Dateien in Safari und kontrolliere sie in der App **Dateien** am tatsächlich gewählten Speicherort. Safari verwaltet das Speichern; AirLinks Hinweis „an den Downloadmanager übergeben“ ist noch kein Nachweis einer gespeicherten Datei.
- [ ] Wähle in der AirLink-Seite auf dem iPhone genau diese gespeicherten Dateien über den Dateiauswahldialog und sende sie an den Mac zurück. Bestätige dort das Angebot. Berechne auf dem Mac die Prüfsummen der zurückgesendeten Dateien und vergleiche sie mit den Originalen. Dafür ist keine zusätzliche Hash-App auf dem iPhone nötig.
- [ ] Lass Safari bei einem Upload zunächst sichtbar im Vordergrund. Wechsle danach kurz zu einer anderen App und zurück. Notiere, ob der Upload weiterläuft, wieder verbindet oder die unveränderten Dateien erneut ausgewählt werden müssen. iOS darf Hintergrundseiten anhalten; eine nötige erneute Auswahl ist keine Zusage dauerhaften Dateizugriffs.

Für einen empfangenen Ordner kannst du anschließend den ZIP-Download ausprobieren. Ein Browser-Ordnerpicker zeigt leere Ordner unter Umständen nicht an; daraus darf kein erfolgreicher Test der Leerordnerübertragung abgeleitet werden. ZIP-Downloads müssen nach einem Abbruch neu gestartet werden.

## 3. Mac ↔ iPad: derselbe Rundweg und Safari-Rückkehr

- [ ] Wiederhole auf dem iPad die Zertifikatsprüfung, Profilinstallation, volle Vertrauensfreigabe und Kopplung aus Schritt 2. Auch dieses Gerät erhält eine eigene Bestätigung auf dem Mac.
- [ ] Lade dieselben Testdateien herunter, sende die gespeicherten Dateien vom iPad zurück und vergleiche die Rückweg-Prüfsummen auf dem Mac.
- [ ] Starte einen längeren Upload, wechsle den Safari-Tab oder sperre das iPad kurz und kehre zurück. Prüfe Wiederverbindung, Fortschritt und gegebenenfalls die ausdrückliche erneute Auswahl der Originaldatei. Vergleiche nach Abschluss erneut die Prüfsumme auf dem Mac.
- [ ] Unterbrich einen **einzelnen Datei-Download** kurz über die Netzwerkverbindung. Wenn Safaris Downloadmanager **Fortsetzen** anbietet, verwende es. Wenn Safari nur einen Neustart zulässt, notiere genau das; AirLink kann die Fortsetzung des Browserdownloads nicht erzwingen. Prüfe die gespeicherte Datei wieder über den Rückweg zum Mac. Verwende für diesen Test kein ZIP, da ZIP-Downloads nicht fortsetzbar sind.

## Ergebnisse und nächster Schritt

Notiere pro Fall Geräte/Versionen, Richtung, Dateigröße, beobachtete Meldung, Verhalten nach Rückkehr und beide Prüfsummen. Ein fehlgeschlagener oder noch nicht ausgeführter Schritt bleibt offen. Melde die konkrete Stelle und den angezeigten Fehler; wiederholtes Koppeln oder Löschen der Anwendungsdaten ist kein allgemeiner Reparaturschritt.

Wenn die kleinen Rundwege funktionieren, folgt eine große einzelne Testdatei über das **echte lokale Netzwerk**, mit ausreichend freiem Speicher und Prüfsummenvergleich. Für die große Desktop-Abnahme ist eine einzelne Datei von mindestens **100 GB** erforderlich. Ein **lokaler, kopfloser 100-GiB-Lauf mit zwei Helferprozessen** prüft andere Bedingungen und schließt diese physische Abnahme nicht ab. Auf den Mobilgeräten notierst du zusätzlich die tatsächlich getestete Größe und Grenzen des Browsers und freien Speichers.

Linux und Android bleiben eigenständige offene Abnahmefälle; diese vier verfügbaren Geräte ersetzen sie nicht. Weitere Kriterien, etwa Ruhemodus, Speicher voll, native Mac-Funktionen und Signierung/Notarisierung, stehen in [RELEASE-READINESS.md](RELEASE-READINESS.md). Tatsächlich abgeschlossene Nachweise gehören in [TESTING.md](TESTING.md). Daraus wird kein fester 1.0-Termin abgeleitet.
