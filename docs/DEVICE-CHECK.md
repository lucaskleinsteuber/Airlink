# Manueller Gerätetest: Mac, Windows, iPhone und iPad

Dieser Leitfaden ist für deinen Mac, den Windows-Laptop, dein iPhone und dein iPad. Der erste Mac→Windows-Transfer mit 0.5 wurde von dir als erfolgreich und schnell gemeldet. Die Bedienung mit Konsole, Link, Bestätigungen und schwer auffindbarem Empfangsordner war dabei unzureichend. Größe, exakte Prüfsummen und Wiederaufnahme wurden für diesen Lauf nicht dokumentiert. Die folgenden Fälle, besonders der vereinfachte native Desktop-Ablauf von 0.7, sind **noch nicht als abgeschlossen dokumentiert**. Du bedienst die Geräte selbst; es gibt keine UI-Automation. Verwende zunächst kleine, eigens angelegte Testdateien. Große Dateien im echten WLAN/LAN folgen danach.

Nutze auf Mac und Windows dieselbe zu prüfende AirLink-Version. Notiere Version, Betriebssystem und Browser. Alle Geräte müssen im selben erreichbaren lokalen Netzwerk sein; ein Gäste-WLAN kann Verbindungen zwischen Geräten sperren. Lass AirLink auf dem Mac und die Windows-App während der jeweiligen Tests laufen. Installation und Start sind in der [README](../README.md#choose-how-to-run-it) beschrieben.

## 1. Mac ↔ Windows: Dateien, Ordner und Wiederaufnahme

Lege auf dem Mac einen kleinen Testordner an, zum Beispiel im Terminal:

```sh
mkdir -p "$HOME/Desktop/AirLink-Test/Leer"
printf 'AirLink Test 1\n' > "$HOME/Desktop/AirLink-Test/Grüße.txt"
dd if=/dev/urandom of="$HOME/Desktop/AirLink-Test/Test-16MiB.bin" bs=1048576 count=16
shasum -a256 "$HOME/Desktop/AirLink-Test/Grüße.txt"
shasum -a256 "$HOME/Desktop/AirLink-Test/Test-16MiB.bin"
```

- [ ] Lade `AirLink-0.9.0-windows-x64-setup.exe` für einen Intel-/AMD-Laptop beziehungsweise die ARM64-Setup-EXE für Windows auf ARM und installiere AirLink. Prüfe den festen Speicherort `%LOCALAPPDATA%\Programs\AirLink`, den Startmenüeintrag und den Eintrag unter **Installierte Apps**. Nach erfolgreichem Start über das Startmenü kann die heruntergeladene Setup-Datei entfernt werden. Beende beim Update die alte native App bei Aufforderung über ihr Menü im Infobereich; Kopplungen und Dateien müssen erhalten bleiben.
- [ ] Wenn die alte AirLink-Konsole noch läuft, beende sie einmal mit **Strg+C** und wähle in der App **Erneut starten**. Die vorhandenen Daten unter `%APPDATA%\AirLink` bleiben erhalten. Prüfe, ob der schon gekoppelte Mac vorhanden ist; kopple ihn nicht vorsorglich erneut.
- [ ] Durchlaufe auf beiden Computern die Einrichtung. Vergib **Mein MacBook** beziehungsweise **Laptop Windows** und wähle **Downloads**. Schließe die Einrichtung einmal vorzeitig: sie darf dadurch nicht als fertig gespeichert sein. Nach erfolgreichem Abschluss und Neustart müssen Name und Zielordner erhalten sein. Prüfe, dass der Name auf dem anderen Gerät aktualisiert wird.
- [ ] Ziehe den Windows-Seitenleistenteiler breiter und schmaler. Normale Gerätenamen müssen in die Seitenleiste passen; längere Namen sollen über zwei Zeilen und vollständig im Tooltip erreichbar bleiben. Prüfe **Geräte**, **Übertragungen** und **Einstellungen**, ohne dass die Alltagssicht eine Wand aus Berechtigungen und Zusatzfunktionen wird.
- [ ] Sende eine einzelne kleine Datei und prüfe, dass sie direkt im gewählten Downloads-Ordner liegt. Sende sie erneut: die vorhandene Datei muss unverändert bleiben, die neue erhält einen eindeutigen Namenszusatz. Wiederhole mit einem ganzen Ordner, der bereits im Ziel vorhanden ist: der neue Ordner darf nicht mit dem alten zusammengeführt werden.
- [ ] Prüfe auf einer übernommenen Installation den vorherigen benutzerdefinierten Zielordner, vorhandene alte Dateien und eine ältere unterbrochene Übertragung. Die Einrichtung darf keine alten Dateien verschieben; bereits gespeicherte Transferziele müssen beim Fortsetzen erhalten bleiben. Wenn Windows Downloads umgeleitet hat, muss die Einrichtung den tatsächlichen Zielordner vorschlagen.
- [ ] Falls noch keine Kopplung besteht, wähle den entdeckten Computer oder **Gerät verbinden**. Vergleiche den Zertifikatsfingerabdruck direkt auf beiden Computern und bestätige die Anfrage am anderen Gerät. Nach „Kopplung angefragt“ muss die App noch auf diese Bestätigung warten; die Anfrage allein ist kein Erfolg.
- [ ] Sind genau ein geeigneter, gekoppelter Desktop online und noch keine Dateien oder Empfänger ausgewählt, darf dieser als Standard erscheinen. Wähle anschließend einen anderen Empfänger oder bereite Dateien ohne Empfänger vor: neue Statusmeldungen dürfen diese Auswahl nicht auf ein anderes Gerät umleiten.
- [ ] Ziehe zuerst die Textdatei direkt auf den gekoppelten Windows-Computer in der Mac-Geräteliste. Ziehe danach den ganzen Ordner auf die große Fläche, deren Beschriftung den Windows-Empfänger nennt. Der jeweilige Stapel soll ohne zusätzlichen Senden-Klick angeboten werden. Bestätige das Angebot auf dem Empfänger. Klicke nach Abschluss direkt auf **Datei zeigen** beziehungsweise **Ordner öffnen**, statt im Empfangsordner zu suchen. Prüfe Inhalt, Namen, Unterordner und den leeren Ordner `Leer`.
- [ ] Aktiviere auf Windows für den Mac ausdrücklich **Von diesem Gerät automatisch empfangen**. Sende eine zweite kleine Datei: In dieser Richtung soll keine neue Annahme nötig sein. Windows→Mac bleibt weiter bestätigungspflichtig, bis du die unabhängige Einstellung auch auf dem Mac aktivierst. Prüfe den gespeicherten Zustand nach einem Neustart und schalte die Option wieder aus, wenn du Bestätigungen behalten möchtest.
- [ ] Teste auf Windows **Dateien senden …** bei gewähltem Mac: Nach der Dateiauswahl soll genau dieser Stapel angeboten werden. Ohne geeigneten Empfänger müssen Dateien vorbereitet bleiben, bis du ein Gerät wählst und **Senden** beziehungsweise **Strg+Enter** verwendest. **Strg+O** öffnet die Dateiauswahl im AirLink-Fenster.
- [ ] Bereite einen Stapel vor und füge während der Vorbereitung neue Dateien hinzu beziehungsweise entferne eine Datei und wähle sie erneut. Die Bestätigung des ersten Stapels darf spätere oder erneut ausgewählte Einträge nicht entfernen. Ein zweites direktes Ablegen während laufender Vorbereitung muss sichtbar erklärt werden. Bei einem verlorenen lokalen Angebotsergebnis darf ein unveränderter Wiederholversuch keinen zweiten Auftrag erzeugen; neue Empfänger oder eine geänderte Auswahl benötigen ein neues Angebot. Prüfe auch den ausdrücklichen Versand an einen bekannten, vorübergehend offline befindlichen Computer und dessen spätere Wiederaufnahme.
- [ ] Schließe das Windows-Hauptfenster während eines ausreichend langen Transfers. Öffne es über das Tray-Symbol wieder; die Übertragung soll weiterlaufen. Prüfe separat aktivierte Hinweise für Kopplung, Annahme, Duplikate, Zwischenablagentext und einen sehr schnellen Abschluss sowie den Zugriff auf das echte Ergebnis. Nach Abschluss soll der Eintrag mit **Datei zeigen** im Bereich der letzten Übertragungen auffindbar bleiben. Beende AirLink abschließend ausdrücklich über **Beenden** und prüfe den nächsten normalen Start.
- [ ] Berechne auf Windows für jede empfangene Datei die SHA-256-Prüfsumme. Ersetze den Beispielpfad durch den tatsächlichen Empfangspfad:

```powershell
Get-FileHash -Algorithm SHA256 -LiteralPath "C:\tatsaechlicher\Empfangspfad\Test-16MiB.bin"
Get-FileHash -Algorithm SHA256 -LiteralPath "C:\tatsaechlicher\Empfangspfad\Grüße.txt"
```

- [ ] Vergleiche jeweils alle 64 Zeichen mit dem Mac-Ergebnis. Groß-/Kleinschreibung der Hex-Zeichen spielt keine Rolle. Wähle auf Windows den Mac, ziehe den empfangenen Ordner in die App oder verwende die Ordnerauswahl, und sende ihn zurück an den Mac und prüfe dort erneut mit `shasum -a256` und dem tatsächlichen Rückempfangspfad.
- [ ] Teste bei einem ausreichend langen Transfer **Pause → Fortsetzen**. Unterbrich danach kurz die Netzwerkverbindung eines Laptops und stelle sie wieder her. AirLink soll vom bestätigten Stand weiterarbeiten; vergleiche die fertige Datei erneut per SHA-256. Kleine Dateien können dafür zu schnell sein: Bei Bedarf vorübergehend ein niedriges Geschwindigkeitslimit einstellen und anschließend den vorherigen Wert wiederherstellen.

## Übertragungstempo: der gemeldete 300-MB-Fall

- [ ] Wiederhole eine einzelne Datei um 300 MB auf dem echten Mac/Windows-Paar in beiden Richtungen. Notiere genaue Bytezahl, App-Versionen, Verbindung (WLAN/Ethernet), Speichermedium und eventuell gesetzte Send-/Empfangslimits. Miss getrennt Vorbereitung, eigentliche Nutzdatenübertragung und abschließende Prüfung; vergleiche die fertigen SHA-256-Werte.
- [ ] Wiederhole unter denselben Bedingungen und protokolliere Auffälligkeiten, ohne lokale Loopback-Werte als WLAN- oder Windows-Nachweis zu verwenden. Ein schneller Internetdownload allein isoliert die Ursache im lokalen Sende-/Speicherpfad nicht. Eine Ursache bleibt offen, solange die Messung sie nicht belegt.

## Windows: Freigabelink, Text und Geräteerkennung

- [ ] Öffne **Quick Share** in der Windows-App. Wähle eine kleine Datei, setze ein Testpasswort, einen Ablaufzeitpunkt und ein Downloadlimit. Kopiere den vollständigen Link und öffne ihn auf einem zweiten Gerät mit eingerichtetem Zertifikatsvertrauen. Prüfe Inhalt, Passwort, Sitzungszählung, Widerruf und Ablauf anhand der Anzeige des bereitstellenden PCs. Es wird keine Internetfreigabe erzeugt.
- [ ] Erlaube Zwischenablagentext auf beiden Desktopgeräten ausdrücklich. Öffne **Zwischenablage** auf Windows und verwende bewusst **Einfügen → Bearbeiten → Senden**. Auf dem Mac darf der Text erst durch **Kopieren** in dessen Systemzwischenablage gelangen. Wiederhole den Rückweg und **Verwerfen**. Teste anschließend ausgeschaltete Berechtigungen und einen zu großen Text.
- [ ] Lass beide Geräte über mehrere 15-Sekunden-Erkennungsfenster geöffnet. Unterbrich die Verbindung eines Geräts und prüfe den Status nach mehr als 45 Sekunden, die spätere Rückkehr und eine gegebenenfalls geänderte Adresse. Gespeicherte Kopplungen dürfen dabei nicht gelöscht werden. Das tatsächliche WLAN/LAN-Verhalten ist durch Hintergrundtests noch nicht bewiesen.

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

Notiere pro Fall Geräte/Versionen und CPU-Architektur, Richtung, Dateigröße, beobachtete Meldung, Verhalten nach Rückkehr, tatsächlichen Speicherort und beide Prüfsummen. Ein x64-Laptop bestätigt keine ARM64-Laufzeit. Ein fehlgeschlagener oder noch nicht ausgeführter Schritt bleibt offen. Melde die konkrete Stelle und den angezeigten Fehler; wiederholtes Koppeln oder Löschen der Anwendungsdaten ist kein allgemeiner Reparaturschritt.

Wenn die kleinen Rundwege funktionieren, folgt eine große einzelne Testdatei über das **echte lokale Netzwerk**, mit ausreichend freiem Speicher und Prüfsummenvergleich. Für die große Desktop-Abnahme ist eine einzelne Datei von mindestens **100 GB** erforderlich. Ein **lokaler, kopfloser 100-GiB-Lauf mit zwei Helferprozessen** prüft andere Bedingungen und schließt diese physische Abnahme nicht ab. Auf den Mobilgeräten notierst du zusätzlich die tatsächlich getestete Größe und Grenzen des Browsers und freien Speichers.

Linux und Android bleiben eigenständige offene Abnahmefälle; diese vier verfügbaren Geräte ersetzen sie nicht. Weitere Kriterien, etwa Ruhemodus, Speicher voll, native Mac-Funktionen und Signierung/Notarisierung, stehen in [RELEASE-READINESS.md](RELEASE-READINESS.md). Tatsächlich abgeschlossene Nachweise gehören in [TESTING.md](TESTING.md). Daraus wird kein fester 1.0-Termin abgeleitet.
