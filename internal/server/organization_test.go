package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func organizationSettings(t *testing.T, s *Server, o model.Organization) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"organization": o})
	if _, e := s.Control("settings.update", b); e != nil {
		t.Fatal(e)
	}
}

func TestOrganizationPrivateDecisionSortedSharesAndAccurateStatistics(t *testing.T) {
	s := integrationHost(t)
	d, token := integrationDevice(t, s, true)
	_, foreignToken := integrationDevice(t, s, true)
	if o := s.Status().Settings.Organization; o.DetectDuplicates || o.SortEnabled || o.Rules == nil {
		t.Fatal("organization must start disabled", o)
	}
	data := []byte("A different filename is still identical content.")
	original := historyReceived(t, s, d, "first.txt", data)
	organizationSettings(t, s, model.Organization{DetectDuplicates: true, SortEnabled: true, Rules: []model.SortRule{{ID: "docs", Extensions: []string{".TXT"}, Folder: "Documents"}}})
	// Old clients omit the new field; their settings writes must preserve it.
	if _, e := s.Control("settings.update", json.RawMessage(`{"name":"Preserved organization"}`)); e != nil {
		t.Fatal(e)
	}
	if !s.Status().Settings.Organization.DetectDuplicates {
		t.Fatal("old settings update reset policy")
	}
	fresh := []byte("new unrelated content")
	files := []model.File{{ID: "duplicate", Path: "renamed.txt", Size: int64(len(data)), DestinationPath: "attacker/path", Skipped: true, DuplicateCandidates: []model.DuplicateCandidate{{ID: "forged", Path: "/private/forged"}}}, {ID: "fresh", Path: "fresh.txt", Size: int64(len(fresh))}}
	var tr model.Transfer
	base := hostURL(s) + "/api/v1/transfers"
	client := pinnedClient(s.fingerprint)
	if e := requestJSON(context.Background(), client, "POST", base, token, model.Offer{Files: files}, &tr); e != nil {
		t.Fatal(e)
	}
	for _, f := range tr.Files {
		if f.DestinationPath != "" || len(f.DuplicateCandidates) > 0 || f.Skipped {
			t.Fatal("untrusted private file fields accepted", f)
		}
	}
	prep := base + "/" + tr.ID + "/files/duplicate/prepare"
	if e := requestJSON(context.Background(), client, "POST", prep, foreignToken, map[string]string{"sha256": digest(data)}, nil); e == nil {
		t.Fatal("other device prepared transfer")
	}
	var p struct {
		Decision string         `json:"decision"`
		Transfer model.Transfer `json:"transfer"`
	}
	if e := requestJSON(context.Background(), client, "POST", prep, token, map[string]string{"sha256": digest(data)}, &p); e != nil || p.Decision != "pending" {
		t.Fatal(p, e)
	}
	local, _ := s.engine.Get(tr.ID)
	if local.State != "waitingDuplicate" || len(local.Files[0].DuplicateCandidates) != 1 || local.Files[0].DestinationPath != "Documents/renamed.txt" {
		t.Fatal(local)
	}
	candidate := local.Files[0].DuplicateCandidates[0]
	for _, v := range []any{p.Transfer, s.guestStatus(d), s.history(d.ID, historyFilter{})} {
		b, _ := json.Marshal(v)
		if bytes.Contains(b, []byte("duplicateCandidates")) || bytes.Contains(b, []byte("destinationPath")) || bytes.Contains(b, []byte(original.Destination)) || bytes.Contains(b, []byte(candidate.ID)) {
			t.Fatal("private path/identity leaked", string(b))
		}
	}
	// Public actions never route to local duplicate resolution/reveal RPCs.
	if e := requestJSON(context.Background(), client, "POST", base+"/"+tr.ID+"/duplicate.resolve", token, map[string]string{"action": "skip"}, nil); e == nil {
		t.Fatal("guest resolved its own duplicate")
	}
	b, _ := json.Marshal(map[string]string{"candidateId": candidate.ID})
	shown, e := s.Control("duplicate.path", b)
	if e != nil || !strings.Contains(shown.(map[string]string)["path"], "first.txt") {
		t.Fatal(shown, e)
	}
	b, _ = json.Marshal(map[string]string{"transferId": tr.ID, "fileId": "duplicate", "action": "skip", "candidateId": candidate.ID})
	if _, e = s.Control("duplicate.resolve", b); e != nil {
		t.Fatal(e)
	}
	if e = requestJSON(context.Background(), client, "POST", prep, token, map[string]string{"sha256": digest(data)}, &p); e != nil || p.Decision != "skip" {
		t.Fatal(p, e)
	}
	if uploadBlock(t, s, token, tr.ID, "fresh", 0, fresh) != 200 {
		t.Fatal("fresh upload failed")
	}
	if e = requestJSON(context.Background(), client, "POST", base+"/"+tr.ID+"/files/fresh/complete", token, map[string]string{"sha256": digest(fresh)}, &tr); e != nil {
		t.Fatal(e)
	}
	local, _ = s.engine.Get(tr.ID)
	if local.State != "completed" || local.CompletedBytes != int64(len(fresh)) || local.SkippedBytes != int64(len(data)) || local.Files[0].Received != 0 {
		t.Fatal("skip inflated actual progress", local)
	}
	if _, e = os.Stat(filepath.Join(local.Destination, "Documents", "renamed.txt")); !os.IsNotExist(e) {
		t.Fatal("skip produced a payload/placeholder", e)
	}
	if got, e := os.ReadFile(filepath.Join(original.Destination, "first.txt")); e != nil || !bytes.Equal(got, data) {
		t.Fatal("original modified", e)
	}
	stats := s.statistics("")
	if stats.SkippedFiles != 1 || stats.SkippedBytes != int64(len(data)) || stats.ReceivedBytes != int64(len(data)+len(fresh)) || stats.TrafficReceivedBytes != int64(len(fresh)) {
		t.Fatal(stats)
	}
	share, e := s.createShare("local", model.ShareCreate{TransferID: local.ID})
	if e != nil || len(share.Files) != 1 || share.Files[0].Path != "fresh.txt" {
		t.Fatal("share included missing skipped file", share, e)
	}
	b, _ = json.Marshal(share)
	if bytes.Contains(b, []byte("destinationPath")) || bytes.Contains(b, []byte("duplicateCandidates")) {
		t.Fatal("share leaked organization metadata")
	}
	f, _, e := s.shares.Open(share.ID, share.Files[0].ID)
	if e != nil {
		t.Fatal("share did not follow sorted destination", e)
	}
	got, _ := io.ReadAll(f)
	f.Close()
	if !bytes.Equal(got, fresh) {
		t.Fatal("share exposed wrong original")
	}
}

func TestPublicFileRedactionDoesNotMutatePrivateSnapshot(t *testing.T) {
	private := model.Transfer{Destination: "/host", Files: []model.File{{DestinationPath: "Documents/a", DuplicateCandidates: []model.DuplicateCandidate{{ID: "private", Path: "/host/old"}}}}}
	public := publicTransfer(private)
	if public.Destination != "" || public.Files[0].DestinationPath != "" || len(public.Files[0].DuplicateCandidates) > 0 {
		t.Fatal(public)
	}
	if private.Files[0].DestinationPath == "" || len(private.Files[0].DuplicateCandidates) != 1 {
		t.Fatal("redaction mutated engine-owned metadata")
	}
}

func TestLegacyCompletionWaitsForHostAndKeepPublishesWithoutSource(t *testing.T) {
	for _, action := range []string{"keep", "skip"} {
		t.Run(action, func(t *testing.T) {
			s := integrationHost(t)
			d, token := integrationDevice(t, s, true)
			data := []byte("legacy sender omits preflight")
			historyReceived(t, s, d, "original.txt", data)
			organizationSettings(t, s, model.Organization{DetectDuplicates: true})
			var tr model.Transfer
			base := hostURL(s) + "/api/v1/transfers"
			client := pinnedClient(s.fingerprint)
			if e := requestJSON(context.Background(), client, "POST", base, token, model.Offer{Files: []model.File{{ID: "legacy", Path: "renamed.txt", Size: int64(len(data))}}}, &tr); e != nil {
				t.Fatal(e)
			}
			if uploadBlock(t, s, token, tr.ID, "legacy", 0, data) != 200 {
				t.Fatal("upload failed")
			}
			err := requestJSON(context.Background(), client, "POST", base+"/"+tr.ID+"/files/legacy/complete", token, map[string]string{"sha256": digest(data)}, nil)
			status, ok := err.(*peerHTTPError)
			if !ok || status.Code != 409 {
				t.Fatal("legacy sender received premature success", err)
			}
			local, _ := s.engine.Get(tr.ID)
			if local.State != "waitingDuplicate" {
				t.Fatal(local)
			}
			if _, e := s.engine.ResolveDuplicate(context.Background(), tr.ID, "legacy", action, local.Files[0].DuplicateCandidates[0].ID); e != nil {
				t.Fatal(e)
			}
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				local, _ = s.engine.Get(tr.ID)
				if local.State == "completed" {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if local.State != "completed" || local.CompletedBytes != int64(len(data)) || local.SkippedBytes != 0 {
				t.Fatal(local)
			}
			got, e := os.ReadFile(filepath.Join(local.Destination, "renamed.txt"))
			if action == "keep" {
				if e != nil || !bytes.Equal(got, data) {
					t.Fatal("verified staging not published", e)
				}
			} else {
				if !os.IsNotExist(e) {
					t.Fatal("skip published a new copy", e)
				}
				if _, e := s.createShare("local", model.ShareCreate{TransferID: local.ID}); e == nil {
					t.Fatal("all-skipped offer shared an indexed existing file")
				}
			}
		})
	}
}

func TestNativeSenderPropagatesSkipWithoutPayloadOrFalseProgress(t *testing.T) {
	source, target := integrationHost(t), integrationHost(t)
	token := randomID()
	from := model.Device{ID: source.Status().ID, Name: "Source", Kind: "engine", CanSend: true, CanReceive: true, AutoAccept: true, Paired: true}
	to := model.Device{ID: target.Status().ID, Name: "Target", Kind: "engine", Address: hostURL(target), Fingerprint: target.fingerprint, CanSend: true, CanReceive: true, Paired: true}
	if e := target.store.put(deviceRecord{Device: from, TokenHash: digest([]byte(token))}); e != nil {
		t.Fatal(e)
	}
	if e := source.store.put(deviceRecord{Device: to, OutboundToken: token}); e != nil {
		t.Fatal(e)
	}
	data := bytes.Repeat([]byte("same-content-different-name"), 40000)
	historyReceived(t, target, from, "original.bin", data)
	organizationSettings(t, target, model.Organization{DetectDuplicates: true})
	path := filepath.Join(t.TempDir(), "new-name.bin")
	if e := os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	sent, e := source.engine.Offer(to, []string{path})
	if e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(15 * time.Second)
	resolved := false
	for time.Now().Before(deadline) {
		for _, tr := range target.engine.Transfers(from.ID) {
			if tr.State == "waitingDuplicate" && !resolved {
				_, e = target.engine.ResolveDuplicate(context.Background(), tr.ID, tr.Files[0].ID, "skip", tr.Files[0].DuplicateCandidates[0].ID)
				if e != nil {
					t.Fatal(e)
				}
				resolved = true
			}
		}
		tr, _ := source.engine.Get(sent.ID)
		if tr.State == "failed" {
			t.Fatal(tr.Error)
		}
		if tr.State == "completed" {
			if !resolved || !tr.Files[0].Skipped || tr.CompletedBytes != 0 || tr.SkippedBytes != int64(len(data)) || source.statistics("").TrafficSentBytes != 0 || target.statistics("").TrafficReceivedBytes != 0 {
				t.Fatal(tr, source.statistics(""), target.statistics(""))
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("sender failed to finish after explicit receiver Skip", source.engine.Transfers(""), target.engine.Transfers(""))
}

func TestPrepareCompatibilityFallsBackOnlyForAbsentRoute(t *testing.T) {
	for _, code := range []int{404, 403, 409} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }))
			defer h.Close()
			decision, _, err := prepareRemoteFile(context.Background(), h.Client(), h.URL+"/prepare", "token", strings.Repeat("a", 64), "transfer", "file")
			if code == 404 {
				if err != nil || decision != "upload" {
					t.Fatal(decision, err)
				}
			} else if err == nil {
				t.Fatal("permission/conflict error silently bypassed")
			}
		})
	}
}

func TestDuplicateDecisionStdioKeepsStatusAndCancellationResponsive(t *testing.T) {
	s := integrationHost(t)
	d, _ := integrationDevice(t, s, true)
	payload := []byte("indexed content")
	historyReceived(t, s, d, "indexed.txt", payload)
	organizationSettings(t, s, model.Organization{DetectDuplicates: true})
	pending, e := s.engine.CreateIncoming(d, []model.File{{ID: "pending", Path: "another.txt", Size: int64(len(payload))}})
	if e != nil {
		t.Fatal(e)
	}
	if decision, e := s.engine.PrepareFile(context.Background(), pending.ID, "pending", digest(payload)); e != nil || decision != "pending" {
		t.Fatal(decision, e)
	}
	pending, _ = s.engine.Get(pending.ID)
	// Both receive slots are occupied, so Skip waits for bounded hash capacity.
	for _, id := range []string{"busy-one", "busy-two"} {
		if _, e = s.engine.CreateIncoming(d, []model.File{{ID: id, Path: id, Size: 1}}); e != nil {
			t.Fatal(e)
		}
	}
	in, input := io.Pipe()
	output, out := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- s.Stdio(in, out); out.Close() }()
	defer input.Close()
	defer output.Close()
	enc, dec := json.NewEncoder(input), json.NewDecoder(output)
	if e = enc.Encode(map[string]any{"id": 1, "method": "duplicate.resolve", "params": map[string]string{"transferId": pending.ID, "fileId": "pending", "action": "skip", "candidateId": pending.Files[0].DuplicateCandidates[0].ID}}); e != nil {
		t.Fatal(e)
	}
	if e = enc.Encode(map[string]any{"id": 2, "method": "status"}); e != nil {
		t.Fatal(e)
	}
	replies := make(chan map[string]any, 4)
	go func() {
		for {
			var v map[string]any
			if dec.Decode(&v) != nil {
				return
			}
			replies <- v
		}
	}()
	read := func() map[string]any {
		t.Helper()
		select {
		case v := <-replies:
			return v
		case <-time.After(3 * time.Second):
			t.Fatal("stdio blocked by duplicate resolution")
			return nil
		}
	}
	if v := read(); v["id"] != float64(2) || v["error"] != nil {
		t.Fatal("status did not overtake slow resolution", v)
	}
	if e = enc.Encode(map[string]any{"id": 3, "method": "transfer.cancel", "params": map[string]string{"id": pending.ID}}); e != nil {
		t.Fatal(e)
	}
	seen := map[float64]bool{}
	for len(seen) < 2 {
		v := read()
		id := v["id"].(float64)
		seen[id] = true
		if id == 1 && v["error"] == nil {
			t.Fatal("cancelled resolution succeeded", v)
		}
	}
	if !seen[1] || !seen[3] {
		t.Fatal(seen)
	}
	if e = enc.Encode(map[string]any{"id": 4, "method": "shutdown"}); e != nil {
		t.Fatal(e)
	}
	if v := read(); v["id"] != float64(4) {
		t.Fatal(v)
	}
	select {
	case e = <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stdio shutdown did not join hash workers")
	}
	if tr, _ := s.engine.Get(pending.ID); tr.State != "cancelled" {
		t.Fatal("stale resolution revived transfer", tr)
	}
}
