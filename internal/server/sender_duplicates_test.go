package server

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func TestNativeSenderContinuesPastConfirmedSkipAfterAnotherDuplicateDecision(t *testing.T) {
	for _, secondAction := range []string{"keep", "skip"} {
		t.Run(secondAction, func(t *testing.T) {
			source, target := integrationHost(t), integrationHost(t)
			token := randomID()
			from := model.Device{ID: source.Status().ID, Name: "Source", Kind: "engine", CanSend: true, CanReceive: true, AutoAccept: true, Paired: true}
			to := model.Device{ID: target.Status().ID, Name: "Target", Kind: "engine", Address: hostURL(target), Fingerprint: target.fingerprint, CanSend: true, CanReceive: true, Paired: true}
			if err := target.store.put(deviceRecord{Device: from, TokenHash: digest([]byte(token))}); err != nil {
				t.Fatal(err)
			}
			if err := source.store.put(deviceRecord{Device: to, OutboundToken: token}); err != nil {
				t.Fatal(err)
			}
			firstData := bytes.Repeat([]byte("first duplicate content\n"), 4096)
			secondData := bytes.Repeat([]byte("second distinct duplicate content\n"), 3072)
			firstOriginal := historyReceived(t, target, from, "original-first.bin", firstData)
			secondOriginal := historyReceived(t, target, from, "original-second.bin", secondData)
			organizationSettings(t, target, model.Organization{DetectDuplicates: true})
			baseline := target.statistics("").ReceivedBytes
			sourceDirectory := t.TempDir()
			firstPath := filepath.Join(sourceDirectory, "a-new-first.bin")
			secondPath := filepath.Join(sourceDirectory, "b-new-second.bin")
			for path, data := range map[string][]byte{firstPath: firstData, secondPath: secondData} {
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			sent, err := source.engine.Offer(to, []string{firstPath, secondPath})
			if err != nil {
				t.Fatal(err)
			}
			if len(sent.Files) != 2 || sent.Files[0].Path != filepath.Base(firstPath) || sent.Files[1].Path != filepath.Base(secondPath) {
				t.Fatal("unexpected sender manifest", sent.Files)
			}
			firstID, secondID := sent.Files[0].ID, sent.Files[1].ID
			deadline := time.Now().Add(20 * time.Second)
			var received model.Transfer
			findIncoming := func() model.Transfer {
				t.Helper()
				for _, transfer := range target.engine.Transfers(from.ID) {
					if len(transfer.Files) == 2 && transfer.Files[0].ID == firstID {
						return transfer
					}
				}
				return model.Transfer{}
			}
			waitFor := func(description string, ready func(model.Transfer, model.Transfer) bool) {
				t.Helper()
				for time.Now().Before(deadline) {
					local, getErr := source.engine.Get(sent.ID)
					if getErr != nil {
						t.Fatal(getErr)
					}
					received = findIncoming()
					if local.State == "failed" || local.State == "cancelled" || received.State == "failed" || received.State == "cancelled" {
						t.Fatalf("transfer stopped while %s: source=%+v target=%+v", description, local, received)
					}
					if ready(local, received) {
						return
					}
					time.Sleep(20 * time.Millisecond)
				}
				t.Fatalf("timed out while %s: source=%+v target=%+v", description, source.engine.Transfers(""), target.engine.Transfers(""))
			}
			pending := func(transfer model.Transfer, fileID string) bool {
				if transfer.State != "waitingDuplicate" {
					return false
				}
				for _, file := range transfer.Files {
					if file.ID == fileID {
						return file.DuplicateState == "pending" && len(file.DuplicateCandidates) > 0
					}
				}
				return false
			}
			resolve := func(fileID, action string) {
				t.Helper()
				for _, file := range received.Files {
					if file.ID == fileID && len(file.DuplicateCandidates) > 0 {
						if _, err := target.engine.ResolveDuplicate(context.Background(), received.ID, fileID, action, file.DuplicateCandidates[0].ID); err != nil {
							t.Fatal(err)
						}
						return
					}
				}
				t.Fatal("missing pending duplicate", fileID, received)
			}
			waitFor("waiting for the first duplicate", func(_, remote model.Transfer) bool { return pending(remote, firstID) })
			resolve(firstID, "skip")
			// Do not resolve the second file until the sender has recorded the first
			// Skip and polled the second pending decision in a later outer pass.
			// Continuing must revisit the first file without trying to reopen it.
			waitFor("waiting for a second decision after the first confirmed Skip", func(local, remote model.Transfer) bool {
				return local.State == "waitingDuplicate" && local.Files[0].Skipped && local.Files[0].Completed && !local.Files[1].Completed && pending(remote, secondID)
			})
			resolve(secondID, secondAction)
			waitFor("finishing the second file", func(local, remote model.Transfer) bool {
				return local.State == "completed" && remote.State == "completed"
			})
			actual := int64(0)
			avoided := int64(len(firstData) + len(secondData))
			skippedFiles := int64(2)
			if secondAction == "keep" {
				actual = int64(len(secondData))
				avoided = int64(len(firstData))
				skippedFiles = 1
			}
			completed, _ := source.engine.Get(sent.ID)
			for _, transfer := range []model.Transfer{completed, received} {
				if transfer.Error != "" || transfer.CompletedBytes != actual || transfer.SkippedBytes != avoided || transfer.TotalBytes != actual+avoided || !transfer.Files[0].Skipped || transfer.Files[1].Skipped != (secondAction == "skip") {
					t.Fatal("incorrect confirmed/avoided accounting", transfer)
				}
				for _, file := range transfer.Files {
					if !file.Completed || file.SHA256 == "" {
						t.Fatal("file was not confirmed", file)
					}
				}
			}
			outStats, inStats := source.statistics(""), target.statistics("")
			if outStats.SentBytes != actual || inStats.ReceivedBytes != baseline+actual || outStats.SkippedBytes != avoided || inStats.SkippedBytes != avoided || outStats.SkippedFiles != skippedFiles || inStats.SkippedFiles != skippedFiles || outStats.TrafficSentBytes != actual || inStats.TrafficReceivedBytes != actual {
				t.Fatal("incorrect transfer/network statistics", outStats, inStats)
			}
			if _, err := os.Stat(filepath.Join(received.Destination, filepath.Base(firstPath))); !os.IsNotExist(err) {
				t.Fatal("first skipped file created a copy", err)
			}
			secondDestination := filepath.Join(received.Destination, filepath.Base(secondPath))
			if secondAction == "keep" {
				if data, err := os.ReadFile(secondDestination); err != nil || !bytes.Equal(data, secondData) {
					t.Fatal("kept second file has incorrect contents", err)
				}
			} else if _, err := os.Stat(secondDestination); !os.IsNotExist(err) {
				t.Fatal("second skipped file created a copy", err)
			}
			for path, expected := range map[string][]byte{
				firstPath: firstData, secondPath: secondData,
				filepath.Join(firstOriginal.Destination, "original-first.bin"):   firstData,
				filepath.Join(secondOriginal.Destination, "original-second.bin"): secondData,
			} {
				if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, expected) {
					t.Fatal("original file changed", path, err)
				}
			}
		})
	}
}
