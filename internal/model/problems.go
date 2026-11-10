package model

import "strings"

// Only a sender can safely retry these failures by reconciling the receiver's
// durable checkpoints. Changed sources, integrity failures and cancellation
// require a new explicit operation instead.
func CanRetryTransfer(t Transfer) bool {
	if t.State != "failed" || t.Direction != "send" || strings.HasPrefix(t.Error, "Receiver stopped transfer:") {
		return false
	}
	p := DescribeTransferError(t.Error)
	if p == nil {
		return false
	}
	switch p.Code {
	case "storage_full", "file_access", "destination", "connection", "device_permission":
		return true
	}
	return false
}

// TransferProblem contains only fixed product text. Technical errors and paths
// remain local details and are never interpolated into diagnostic reports.
type TransferProblem struct {
	Code       string `json:"code"`
	TitleDE    string `json:"titleDE"`
	TitleEN    string `json:"titleEN"`
	RecoveryDE string `json:"recoveryDE"`
	RecoveryEN string `json:"recoveryEN"`
}

func DescribeTransferError(raw string) *TransferProblem {
	if raw == "" {
		return nil
	}
	s := strings.ToLower(raw)
	p := TransferProblem{Code: "unknown", TitleDE: "Übertragung benötigt Aufmerksamkeit", TitleEN: "Transfer needs attention", RecoveryDE: "Öffne die Details. Prüfe Quelle, Empfangsordner und Verbindung auf beiden Geräten.", RecoveryEN: "Open details. Check the source, destination and connection on both devices."}
	switch {
	case strings.HasPrefix(s, "storage_full:"), strings.Contains(s, "no space left"), strings.Contains(s, "disk full"), strings.Contains(s, "disk is full"), strings.Contains(s, "not enough space"), strings.Contains(s, "insufficient disk"):
		p = TransferProblem{"storage_full", "Empfangsdatenträger ist voll", "Receiving disk is full", "Schaffe auf dem empfangenden Gerät Speicherplatz. Eine unterbrochene Übertragung kann danach fortgesetzt werden; einen fehlgeschlagenen Auftrag erneut senden.", "Free space on the receiving device. Resume an interrupted transfer afterwards; send a failed job again."}
	case strings.Contains(s, "source changed"), strings.Contains(s, "source file"), strings.Contains(s, "original file"):
		p = TransferProblem{"source_changed", "Quelldatei ist verändert oder nicht verfügbar", "Source changed or is unavailable", "Prüfe die Originaldateien und wähle sie für eine neue Sendung erneut aus.", "Check the original files and select them again for a new send."}
	case strings.Contains(s, "permission revoked"), strings.Contains(s, "device cannot"), strings.Contains(s, "not paired"):
		p = TransferProblem{"device_permission", "Geräteberechtigung fehlt", "Device permission is missing", "Prüfe die Freigaben für dieses Gerät. Nach einer aufgehobenen Kopplung beide Geräte erneut verbinden.", "Check this device's permissions. Pair both devices again after trust was revoked."}
	case strings.HasPrefix(s, "file_access:"), strings.Contains(s, "permission denied"), strings.Contains(s, "not permitted"), strings.Contains(s, "access is denied"), strings.Contains(s, "read-only"):
		p = TransferProblem{"file_access", "Dateizugriff ist nicht möglich", "File access is unavailable", "Prüfe die Lese- und Schreibrechte. Wähle für neue Übertragungen bei Bedarf einen anderen Empfangsordner; laufende Aufträge behalten ihren bisherigen Zielordner.", "Check read/write access. Choose another receiving folder for new transfers if needed; existing jobs keep their original destination."}
	case strings.Contains(s, "integrity"), strings.Contains(s, "checksum"), strings.Contains(s, "hash mismatch"):
		p = TransferProblem{"integrity", "Dateiprüfung fehlgeschlagen", "File verification failed", "Prüfe die Originaldatei und sende sie erneut. Unvollständige Dateien werden nicht als fertiges Ergebnis veröffentlicht.", "Check the original file and send it again. Incomplete files are not published as finished results."}
	case strings.Contains(s, "destination"), strings.Contains(s, "no such file"), strings.Contains(s, "cannot find the path"):
		p = TransferProblem{"destination", "Empfangsordner prüfen", "Check the receiving folder", "Stelle den bisherigen Empfangsordner wieder bereit. Eine Ordneränderung in den Einstellungen gilt für neue Übertragungen.", "Make the original receiving folder available again. Changing the folder in settings applies to new transfers."}
	case strings.Contains(s, "connection"), strings.Contains(s, "timeout"), strings.Contains(s, "deadline"), strings.Contains(s, "network"), strings.Contains(s, "dial tcp"):
		p = TransferProblem{"connection", "Verbindung ist unterbrochen", "Connection interrupted", "Lass AirLink auf beiden Geräten geöffnet und prüfe das lokale Netzwerk. Unterbrochene Aufträge verbinden sich automatisch erneut; Fortsetzen versucht es sofort.", "Keep AirLink open on both devices and check the local network. Interrupted jobs reconnect automatically; Resume retries immediately."}
	}
	return &p
}
