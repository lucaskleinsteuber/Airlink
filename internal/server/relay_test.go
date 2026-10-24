package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucasfd3/Airlink/internal/model"
)

func relayBrowser(t *testing.T, s *Server, id string) (model.Device, string) {
	t.Helper()
	d, token := securityDevice(t, s, id)
	d.CanRelay = true
	d.LastSeen = time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.store.put(deviceRecord{Device: d, TokenHash: digest([]byte(token))}); err != nil {
		t.Fatal(err)
	}
	return d, token
}
func relayHTTPS(ctx context.Context, client *http.Client, method, endpoint, token string, body io.Reader, headers map[string]string) (*http.Response, error) {
	r, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Authorization", "Bearer "+token)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return client.Do(r)
}
func createHTTPRelay(t *testing.T, s *Server, client *http.Client, sourceToken string, target model.Device, files []model.File) model.Relay {
	t.Helper()
	var v model.Relay
	if err := requestJSON(context.Background(), client, "POST", securityURL(s)+"/api/v1/relays", sourceToken, model.RelayCreate{ReceiverID: target.ID, Files: files}, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func acceptHTTPRelay(t *testing.T, s *Server, client *http.Client, v model.Relay, receiverToken string) {
	t.Helper()
	if err := requestJSON(context.Background(), client, "POST", securityURL(s)+"/api/v1/relays/"+v.ID+"/accept", receiverToken, map[string]string{}, nil); err != nil {
		t.Fatal(err)
	}
}

type relayHTTPResult struct {
	Code   int
	Header http.Header
	Body   []byte
	Err    error
}

func relayDownloadAsync(s *Server, client *http.Client, v model.Relay, receiverToken, path string, headers map[string]string) <-chan relayHTTPResult {
	done := make(chan relayHTTPResult, 1)
	go func() {
		res, err := relayHTTPS(context.Background(), client, "GET", securityURL(s)+"/api/v1/relays/"+v.ID+path, receiverToken, nil, headers)
		if err != nil {
			done <- relayHTTPResult{Err: err}
			return
		}
		b, err := io.ReadAll(res.Body)
		res.Body.Close()
		done <- relayHTTPResult{Code: res.StatusCode, Header: res.Header, Body: b, Err: err}
	}()
	return done
}
func nextHTTPRelayRequest(ctx context.Context, s *Server, client *http.Client, v model.Relay, sourceToken string) (*model.RelayRequest, error) {
	for {
		var response struct {
			Request *model.RelayRequest `json:"request"`
		}
		if err := requestJSON(ctx, client, "GET", securityURL(s)+"/api/v1/relays/"+v.ID+"/request", sourceToken, nil, &response); err != nil {
			return nil, err
		}
		if response.Request != nil {
			return response.Request, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
}
func putHTTPRelayBlock(ctx context.Context, s *Server, client *http.Client, v model.Relay, sourceToken string, request model.RelayRequest, payload []byte) error {
	endpoint := fmt.Sprintf("%s/api/v1/relays/%s/files/%s/chunks/%d?requestId=%s", securityURL(s), v.ID, request.FileID, request.Index, request.RequestID)
	res, err := relayHTTPS(ctx, client, "PUT", endpoint, sourceToken, bytes.NewReader(payload), map[string]string{"X-Chunk-SHA256": digest(payload)})
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		return fmt.Errorf("source PUT %d: %s", res.StatusCode, body)
	}
	var ack struct {
		Delivered bool `json:"delivered"`
	}
	if err = json.Unmarshal(body, &ack); err != nil {
		return err
	}
	if !ack.Delivered {
		return errors.New("source ACK did not attest transport consumption")
	}
	return nil
}
func startRelaySource(s *Server, client *http.Client, v model.Relay, token string, payloads map[string][]byte) (context.CancelFunc, <-chan error) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		for {
			req, err := nextHTTPRelayRequest(ctx, s, client, v, token)
			if err != nil {
				done <- err
				return
			}
			payload, ok := payloads[req.FileID]
			if !ok {
				done <- fmt.Errorf("unknown source file %s", req.FileID)
				return
			}
			start := req.Index * model.ChunkSize
			end := min(start+model.ChunkSize, int64(len(payload)))
			if start < 0 || start >= end {
				done <- errors.New("invalid requested block")
				return
			}
			if err = putHTTPRelayBlock(ctx, s, client, v, token, *req, payload[start:end]); err != nil {
				done <- err
				return
			}
		}
	}()
	return cancel, done
}
func waitRelayResult(t *testing.T, done <-chan relayHTTPResult) relayHTTPResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(8 * time.Second):
		t.Fatal("relay download hung")
	}
	return relayHTTPResult{}
}
func waitRelayState(t *testing.T, s *Server, v model.Relay, state string) model.Relay {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got, err := s.relays.Get(v.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		if got.State == state {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("relay state=%s want=%s error=%s", got.State, state, got.Error)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRelayRealHTTPSRequiresReceiverApprovalAndStreamsWithoutDiskPayload(t *testing.T) {
	s := newSecurityServer(t)
	source, sourceToken := relayBrowser(t, s, "relay-source")
	target, targetToken := relayBrowser(t, s, "relay-target")
	_, otherToken := relayBrowser(t, s, "relay-other")
	client := pinnedClient(s.fingerprint)
	payload := bytes.Repeat([]byte("UNIQUE_RELAY_PAYLOAD_NEVER_ON_DISK_"), 9000)
	v := createHTTPRelay(t, s, client, sourceToken, target, []model.File{{ID: "original", Path: "original.bin", Size: int64(len(payload)), SHA256: digest(payload)}})
	if err := requestJSON(context.Background(), client, "POST", securityURL(s)+"/api/v1/relays/"+v.ID+"/accept", sourceToken, map[string]string{}, nil); err == nil {
		t.Fatal("source accepted its own relay")
	}
	if _, err := s.relayControl("relay.accept", json.RawMessage(`{"id":"`+v.ID+`"}`)); err == nil {
		t.Fatal("host silently accepted for recipient")
	}
	for _, token := range []string{sourceToken, otherToken} {
		res, err := relayHTTPS(context.Background(), client, "GET", securityURL(s)+"/api/v1/relays/"+v.ID+"/files/original/download", token, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode < 400 {
			t.Fatal("participant ACL bypass", res.StatusCode)
		}
	}
	acceptHTTPRelay(t, s, client, v, targetToken)
	cancel, sourceDone := startRelaySource(s, client, v, sourceToken, map[string][]byte{"original": payload})
	defer func() { cancel(); <-sourceDone }()
	result := waitRelayResult(t, relayDownloadAsync(s, client, v, targetToken, "/files/original/download", nil))
	if result.Err != nil || result.Code != 200 || !bytes.Equal(result.Body, payload) {
		t.Fatal("HTTPS relay failed", result.Code, result.Err, len(result.Body))
	}
	got := waitRelayState(t, s, v, "delivered")
	if got.DeliveredBytes != int64(len(payload)) {
		t.Fatal("wrong unique transport progress", got.DeliveredBytes)
	}
	// A retry after delivered still fetches the original from the live source.
	retry := waitRelayResult(t, relayDownloadAsync(s, client, v, targetToken, "/files/original/download", map[string]string{"Range": "bytes=3-7"}))
	if retry.Err != nil || retry.Code != 206 || !bytes.Equal(retry.Body, payload[3:8]) {
		t.Fatal("delivered Range retry failed", retry.Code, retry.Err)
	}
	got, _ = s.relays.Get(v.ID, source.ID)
	if got.DeliveredBytes != int64(len(payload)) {
		t.Fatal("retry double counted")
	}
	if len(s.engine.Transfers("")) != 0 {
		t.Fatal("relay silently became a stored upload")
	}
	if err := filepath.WalkDir(s.opts.DataDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if bytes.Contains(b, payload[:128]) {
			t.Fatal("relay payload found on disk", p)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	devices := s.relayDevices(source)
	for _, d := range devices {
		if d.Address != "" || d.Fingerprint != "" || d.LastSeen != "" {
			t.Fatal("device picker exposed private peer metadata")
		}
	}
}

func TestRelayHEADRangesAndEmptyFilesDoNotAskSourceUnnecessarily(t *testing.T) {
	s := newSecurityServer(t)
	_, sourceToken := relayBrowser(t, s, "relay-source")
	target, targetToken := relayBrowser(t, s, "relay-target")
	client := pinnedClient(s.fingerprint)
	payload := []byte("range source")
	v := createHTTPRelay(t, s, client, sourceToken, target, []model.File{{ID: "file", Path: "file.txt", Size: int64(len(payload)), SHA256: digest(payload)}})
	acceptHTTPRelay(t, s, client, v, targetToken)
	for _, input := range []struct {
		method, rangeValue string
		status             int
	}{{"HEAD", "", 200}, {"HEAD", "bytes=2-4", 206}, {"GET", "bytes=500-", 416}, {"GET", "bytes=0-1,3-4", 416}} {
		headers := map[string]string{}
		if input.rangeValue != "" {
			headers["Range"] = input.rangeValue
		}
		res, err := relayHTTPS(context.Background(), client, input.method, securityURL(s)+"/api/v1/relays/"+v.ID+"/files/file/download", targetToken, nil, headers)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != input.status {
			t.Fatal("range/HEAD mismatch", input, res.StatusCode)
		}
		if request, err := s.relays.NextRequest(v.ID, v.SenderID); err != nil || request != nil {
			t.Fatal("HEAD/invalid range asked source", err)
		}
	}
	empty := createHTTPRelay(t, s, client, sourceToken, target, []model.File{{ID: "zero", Path: "zero", SHA256: digest(nil)}})
	acceptHTTPRelay(t, s, client, empty, targetToken)
	invalid := waitRelayResult(t, relayDownloadAsync(s, client, empty, targetToken, "/files/zero/download", map[string]string{"Range": "bytes=0-"}))
	if invalid.Code != 416 {
		t.Fatal("empty invalid range", invalid.Code)
	}
	result := waitRelayResult(t, relayDownloadAsync(s, client, empty, targetToken, "/files/zero/download", nil))
	if result.Err != nil || result.Code != 200 || len(result.Body) != 0 {
		t.Fatal("empty download", result.Code, result.Err)
	}
	waitRelayState(t, s, empty, "delivered")
	if request, _ := s.relays.NextRequest(empty.ID, empty.SenderID); request != nil {
		t.Fatal("empty file asked source for bytes")
	}
}

func TestRelayFullSHAStopsBeforeLastBlockIsReleased(t *testing.T) {
	s := newSecurityServer(t)
	_, sourceToken := relayBrowser(t, s, "relay-source")
	target, targetToken := relayBrowser(t, s, "relay-target")
	client := pinnedClient(s.fingerprint)
	original := append(bytes.Repeat([]byte("a"), int(model.ChunkSize)), []byte("last bytes must be withheld")...)
	changed := append([]byte{}, original...)
	changed[len(changed)-1] = 'z'
	v := createHTTPRelay(t, s, client, sourceToken, target, []model.File{{ID: "file", Path: "changed.bin", Size: int64(len(original)), SHA256: digest(original)}})
	acceptHTTPRelay(t, s, client, v, targetToken)
	cancel, sourceDone := startRelaySource(s, client, v, sourceToken, map[string][]byte{"file": changed})
	defer func() { cancel(); <-sourceDone }()
	result := waitRelayResult(t, relayDownloadAsync(s, client, v, targetToken, "/files/file/download", nil))
	if result.Err == nil || int64(len(result.Body)) != model.ChunkSize {
		t.Fatal("unverified last block escaped", result.Err, len(result.Body))
	}
	got := waitRelayState(t, s, v, "failed")
	if got.DeliveredBytes != model.ChunkSize || got.Files[0].Completed {
		t.Fatal("failed relay claimed file completion", got)
	}
}

func TestRelayPauseInterruptsHTTPSAndRangeResumeUsesSourceAgain(t *testing.T) {
	s := newSecurityServer(t)
	_, sourceToken := relayBrowser(t, s, "relay-source")
	target, targetToken := relayBrowser(t, s, "relay-target")
	client := pinnedClient(s.fingerprint)
	payload := append(bytes.Repeat([]byte("a"), int(model.ChunkSize)), byte('b'))
	v := createHTTPRelay(t, s, client, sourceToken, target, []model.File{{ID: "file", Path: "resume.bin", Size: int64(len(payload)), SHA256: digest(payload)}})
	acceptHTTPRelay(t, s, client, v, targetToken)
	download := relayDownloadAsync(s, client, v, targetToken, "/files/file/download", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := nextHTTPRelayRequest(ctx, s, client, v, sourceToken)
	if err != nil {
		t.Fatal(err)
	}
	if err = putHTTPRelayBlock(ctx, s, client, v, sourceToken, *first, payload[:model.ChunkSize]); err != nil {
		t.Fatal(err)
	}
	second, err := nextHTTPRelayRequest(ctx, s, client, v, sourceToken)
	if err != nil || second.Index != 1 {
		t.Fatal("next source block missing", err)
	}
	if err = requestJSON(ctx, client, "POST", securityURL(s)+"/api/v1/relays/"+v.ID+"/pause", targetToken, map[string]string{}, nil); err != nil {
		t.Fatal(err)
	}
	interrupted := waitRelayResult(t, download)
	if interrupted.Err == nil || int64(len(interrupted.Body)) != model.ChunkSize {
		t.Fatal("pause did not interrupt transport", interrupted.Err, len(interrupted.Body))
	}
	if err = requestJSON(ctx, client, "POST", securityURL(s)+"/api/v1/relays/"+v.ID+"/resume", targetToken, map[string]string{}, nil); err != nil {
		t.Fatal(err)
	}
	retry := relayDownloadAsync(s, client, v, targetToken, "/files/file/download", map[string]string{"Range": fmt.Sprintf("bytes=%d-", model.ChunkSize)})
	request, err := nextHTTPRelayRequest(ctx, s, client, v, sourceToken)
	if err != nil || request.Index != 1 || request.RequestID == second.RequestID {
		t.Fatal("resume reused stale RAM request", err)
	}
	if err = putHTTPRelayBlock(ctx, s, client, v, sourceToken, *request, payload[model.ChunkSize:]); err != nil {
		t.Fatal(err)
	}
	result := waitRelayResult(t, retry)
	if result.Code != 206 || result.Err != nil || string(result.Body) != "b" {
		t.Fatal("range resume failed", result.Code, result.Err)
	}
	got := waitRelayState(t, s, v, "delivered")
	if got.DeliveredBytes != int64(len(payload)) {
		t.Fatal("resume unique bytes", got.DeliveredBytes)
	}
}

func TestRelayZIPPreservesPathsAndEmptyDirectories(t *testing.T) {
	s := newSecurityServer(t)
	_, sourceToken := relayBrowser(t, s, "relay-source")
	target, targetToken := relayBrowser(t, s, "relay-target")
	client := pinnedClient(s.fingerprint)
	payload := []byte("folder payload")
	v := createHTTPRelay(t, s, client, sourceToken, target, []model.File{{ID: "dir", Path: "folder", Directory: true}, {ID: "empty-dir", Path: "folder/empty", Directory: true}, {ID: "zero", Path: "folder/zero", SHA256: digest(nil)}, {ID: "file", Path: "folder/nested/file.txt", Size: int64(len(payload)), SHA256: digest(payload)}})
	acceptHTTPRelay(t, s, client, v, targetToken)
	invalid := waitRelayResult(t, relayDownloadAsync(s, client, v, targetToken, "/archive", map[string]string{"Range": "bytes=0-1"}))
	if invalid.Code != 416 {
		t.Fatal("archive falsely promised Range resume", invalid.Code)
	}
	cancel, sourceDone := startRelaySource(s, client, v, sourceToken, map[string][]byte{"file": payload})
	defer func() { cancel(); <-sourceDone }()
	result := waitRelayResult(t, relayDownloadAsync(s, client, v, targetToken, "/archive", nil))
	if result.Err != nil || result.Code != 200 {
		t.Fatal("archive relay", result.Code, result.Err, string(result.Body))
	}
	archive, err := zip.NewReader(bytes.NewReader(result.Body), int64(len(result.Body)))
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, entry := range archive.File {
		found[entry.Name] = true
		if entry.Name == "folder/nested/file.txt" {
			f, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(f)
			f.Close()
			if !bytes.Equal(b, payload) {
				t.Fatal("ZIP payload corrupted")
			}
		}
	}
	for _, name := range []string{"folder/", "folder/empty/", "folder/zero", "folder/nested/file.txt"} {
		if !found[name] {
			t.Fatal("lost directory/path", name)
		}
	}
	waitRelayState(t, s, v, "delivered")
}

func TestRelayCancellationWhileThrottledCannotReleasePayload(t *testing.T) {
	for _, action := range []string{"pause", "revoke", "receive-permission"} {
		t.Run(action, func(t *testing.T) {
			s := newSecurityServer(t)
			_, sourceToken := relayBrowser(t, s, "relay-source")
			target, targetToken := relayBrowser(t, s, "relay-target")
			client := pinnedClient(s.fingerprint)
			payload := bytes.Repeat([]byte("a"), 8192)
			v := createHTTPRelay(t, s, client, sourceToken, target, []model.File{{ID: "file", Path: "throttled.bin", Size: int64(len(payload)), SHA256: digest(payload)}})
			acceptHTTPRelay(t, s, client, v, targetToken)
			if err := s.traffic.SetLimits(8192, 0); err != nil {
				t.Fatal(err)
			}
			cancel, sourceDone := startRelaySource(s, client, v, sourceToken, map[string][]byte{"file": payload})
			defer func() { cancel(); <-sourceDone }()
			result := relayDownloadAsync(s, client, v, targetToken, "/files/file/download", nil)
			deadline := time.Now().Add(3 * time.Second)
			for s.traffic.Snapshot().ReceivedBytes < int64(len(payload)) {
				if time.Now().After(deadline) {
					t.Fatal("source did not supply throttled block")
				}
				time.Sleep(time.Millisecond)
			}
			time.Sleep(50 * time.Millisecond)
			var err error
			switch action {
			case "pause":
				err = requestJSON(context.Background(), client, "POST", securityURL(s)+"/api/v1/relays/"+v.ID+"/pause", targetToken, map[string]string{}, nil)
			case "revoke":
				params, _ := json.Marshal(map[string]string{"id": target.ID})
				_, err = s.Control("device.revoke", params)
			case "receive-permission":
				target.CanReceive = false
				params, _ := json.Marshal(target)
				_, err = s.Control("device.update", params)
			}
			if err != nil {
				t.Fatal(err)
			}
			got := waitRelayResult(t, result)
			if bytes.Contains(got.Body, payload[:128]) || s.traffic.Snapshot().SentBytes != 0 {
				t.Fatal("payload escaped after cancellation during limiter wait", len(got.Body), s.traffic.Snapshot())
			}
		})
	}
}

func TestRelayDeliveredRetryRoleRevocationInterruptsWithoutErasingHistory(t *testing.T) {
	s := newSecurityServer(t)
	_, sourceToken := relayBrowser(t, s, "relay-source")
	target, targetToken := relayBrowser(t, s, "relay-target")
	client := pinnedClient(s.fingerprint)
	payload := bytes.Repeat([]byte("b"), 8192)
	v := createHTTPRelay(t, s, client, sourceToken, target, []model.File{{ID: "file", Path: "retry.bin", Size: int64(len(payload)), SHA256: digest(payload)}})
	acceptHTTPRelay(t, s, client, v, targetToken)
	cancel, sourceDone := startRelaySource(s, client, v, sourceToken, map[string][]byte{"file": payload})
	defer func() { cancel(); <-sourceDone }()
	first := waitRelayResult(t, relayDownloadAsync(s, client, v, targetToken, "/files/file/download", nil))
	if first.Code != 200 || first.Err != nil {
		t.Fatal(first.Code, first.Err)
	}
	waitRelayState(t, s, v, "delivered")
	before := s.traffic.Snapshot()
	if err := s.traffic.SetLimits(8192, 0); err != nil {
		t.Fatal(err)
	}
	retry := relayDownloadAsync(s, client, v, targetToken, "/files/file/download", map[string]string{"Range": "bytes=0-8191"})
	deadline := time.Now().Add(3 * time.Second)
	for s.traffic.Snapshot().ReceivedBytes < before.ReceivedBytes+int64(len(payload)) {
		if time.Now().After(deadline) {
			t.Fatal("retry did not request original source")
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	target.CanReceive = false
	params, _ := json.Marshal(target)
	if _, err := s.Control("device.update", params); err != nil {
		t.Fatal(err)
	}
	result := waitRelayResult(t, retry)
	if bytes.Contains(result.Body, payload[:128]) || s.traffic.Snapshot().SentBytes != before.SentBytes {
		t.Fatal("terminal retry ignored permission revocation")
	}
	got, _ := s.relays.Get(v.ID, "")
	if got.State != "delivered" || got.DeliveredBytes != int64(len(payload)) {
		t.Fatal("revoking retry erased earlier transport history", got)
	}
}

func TestRelayFailedArchiveAbortsHTTPRatherThanEndingTruncatedZIPCleanly(t *testing.T) {
	s := newSecurityServer(t)
	_, sourceToken := relayBrowser(t, s, "relay-source")
	target, targetToken := relayBrowser(t, s, "relay-target")
	client := pinnedClient(s.fingerprint)
	original := append(bytes.Repeat([]byte("c"), int(model.ChunkSize)), []byte("original tail")...)
	changed := append([]byte{}, original...)
	changed[len(changed)-1] = 'z'
	v := createHTTPRelay(t, s, client, sourceToken, target, []model.File{{ID: "file", Path: "folder/source.bin", Size: int64(len(original)), SHA256: digest(original)}})
	acceptHTTPRelay(t, s, client, v, targetToken)
	cancel, sourceDone := startRelaySource(s, client, v, sourceToken, map[string][]byte{"file": changed})
	defer func() { cancel(); <-sourceDone }()
	result := waitRelayResult(t, relayDownloadAsync(s, client, v, targetToken, "/archive", nil))
	if result.Err == nil {
		t.Fatal("truncated archive ended as successful HTTP", result.Code, len(result.Body))
	}
	if _, err := zip.NewReader(bytes.NewReader(result.Body), int64(len(result.Body))); err == nil {
		t.Fatal("failed integrity archive appeared complete")
	}
	waitRelayState(t, s, v, "failed")
}

func TestRelayIfRangeDateRequiresExactValidatorMatch(t *testing.T) {
	modified := time.Unix(1700000000, 0).UTC()
	file := model.File{ID: "file", Path: "file", Size: 10, Modified: modified.UnixMilli(), SHA256: digest([]byte("0123456789"))}
	for _, input := range []struct {
		date time.Time
		want string
	}{{modified, "bytes=0-1"}, {modified.Add(time.Hour), ""}, {modified.Add(-time.Hour), ""}} {
		r, _ := http.NewRequest("GET", "https://localhost/file", nil)
		r.Header.Set("Range", "bytes=0-1")
		r.Header.Set("If-Range", input.date.Format(http.TimeFormat))
		w := &relayHeaderOnly{headers: http.Header{}}
		done, got := relayPreconditions(w, r, file)
		if done || got != input.want {
			t.Fatal("If-Range date did not match exact Last-Modified", input.date, got)
		}
	}
}

func TestRelayRangeAboveTwoGiBRequestsOnlyTheCorrectBlock(t *testing.T) {
	s := newSecurityServer(t)
	_, sourceToken := relayBrowser(t, s, "relay-source")
	target, targetToken := relayBrowser(t, s, "relay-target")
	client := pinnedClient(s.fingerprint)
	const offset int64 = 3 * 1024 * 1024 * 1024
	v := createHTTPRelay(t, s, client, sourceToken, target, []model.File{{ID: "huge", Path: "huge.bin", Size: offset + 1, SHA256: digest([]byte("manifest identity for large-source range fixture"))}})
	acceptHTTPRelay(t, s, client, v, targetToken)
	download := relayDownloadAsync(s, client, v, targetToken, "/files/huge/download", map[string]string{"Range": fmt.Sprintf("bytes=%d-%d", offset, offset)})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	request, err := nextHTTPRelayRequest(ctx, s, client, v, sourceToken)
	if err != nil {
		t.Fatal(err)
	}
	if request.Index != offset/model.ChunkSize {
		t.Fatal("64-bit range selected incorrect source block", request.Index)
	}
	if err = putHTTPRelayBlock(ctx, s, client, v, sourceToken, *request, []byte("x")); err != nil {
		t.Fatal(err)
	}
	result := waitRelayResult(t, download)
	if result.Err != nil || result.Code != 206 || string(result.Body) != "x" || result.Header.Get("Content-Range") != "bytes 3221225472-3221225472/3221225473" {
		t.Fatal("64-bit relay range failed", result.Code, result.Err, result.Header)
	}
	got, _ := s.relays.Get(v.ID, "")
	if got.DeliveredBytes != 1 || got.TotalBytes != offset+1 {
		t.Fatal("large range progress overflow", got)
	}
	// Only one byte was transported; this is not a complete 3 GiB transfer test.
}

type relayHeaderOnly struct{ headers http.Header }

func (w *relayHeaderOnly) Header() http.Header         { return w.headers }
func (w *relayHeaderOnly) WriteHeader(int)             {}
func (w *relayHeaderOnly) Write(b []byte) (int, error) { return len(b), nil }
