package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRecoveryAdviceNeverIncludesRawTechnicalData(t *testing.T) {
	for raw, code := range map[string]string{"write /home/" + "sensitive-test-person/file.txt: no space left on device": "storage_full", "storage_full: Receiving disk is full": "storage_full", "file_access: File access is unavailable": "file_access", "source changed sensitive-test-person": "source_changed", "Receiving device permission revoked": "device_permission", "read-only file system": "file_access", "file integrity check failed": "integrity", "destination replaced sensitive-test-person": "destination", "dial tcp sensitive-test-person: network unreachable": "connection", "unrecognized sensitive-test-person": "unknown"} {
		p := DescribeTransferError(raw)
		if p == nil || p.Code != code {
			t.Fatalf("wrong problem class for %q", code)
		}
		body, _ := json.Marshal(p)
		if strings.Contains(string(body), "sensitive-test-person") || p.TitleDE == "" || p.TitleEN == "" || p.RecoveryDE == "" || p.RecoveryEN == "" {
			t.Fatal("unsafe/incomplete recovery advice")
		}
	}
	if DescribeTransferError("") != nil {
		t.Fatal("empty error creates problem")
	}
	if CanRetryTransfer(Transfer{State: "failed", Direction: "send", Error: "Receiver stopped transfer: disk full"}) {
		t.Fatal("a terminal receiver cannot be resumed by retrying its sender")
	}
}

func TestOnlyRecoverableFailedSendersCanRetry(t *testing.T) {
	for _, state := range []string{"completed", "cancelled", "failed", "running"} {
		for _, direction := range []string{"send", "receive"} {
			for _, reason := range []string{"disk full", "source changed", "file integrity check failed", "connection timeout"} {
				got := CanRetryTransfer(Transfer{State: state, Direction: direction, Error: reason})
				want := state == "failed" && direction == "send" && (reason == "disk full" || reason == "connection timeout")
				if got != want {
					t.Fatal("unsafe retry eligibility", state, direction, reason)
				}
			}
		}
	}
}
