package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucasfd3/Airlink/internal/model"
)

// This optional compatibility run executes only a previous headless helper in
// temporary directories. It never launches or installs an application bundle.
func TestUpgradeFromReleased010PreservesData(t *testing.T) {
	previous := os.Getenv("AIRLINK_PREVIOUS_HELPER")
	if previous == "" {
		t.Skip("set AIRLINK_PREVIOUS_HELPER to a built 0.1.0 service")
	}
	version, err := exec.Command(previous, "--version").Output()
	if err != nil || strings.TrimSpace(string(version)) != "0.1.0" {
		t.Fatal("previous helper must report 0.1.0", err)
	}
	base := t.TempDir()
	state, received := filepath.Join(base, "state"), filepath.Join(base, "received")
	command := exec.Command(previous, "--stdio", "--no-discovery", "--data-dir", state, "--receive-dir", received)
	stdin, _ := command.StdinPipe()
	stdout, _ := command.StdoutPipe()
	command.Stderr = io.Discard
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	finished := false
	defer func() {
		if !finished {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	decoder, encoder := json.NewDecoder(stdout), json.NewEncoder(stdin)
	sequence := 0
	rpc := func(method string, params any, result any) {
		t.Helper()
		sequence++
		if err := encoder.Encode(map[string]any{"id": sequence, "method": method, "params": params}); err != nil {
			t.Fatal(err)
		}
		var response struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  string          `json:"error"`
		}
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		if response.ID != sequence || response.Error != "" {
			t.Fatal("old helper RPC failed", response.Error)
		}
		if result != nil {
			if err := json.Unmarshal(response.Result, result); err != nil {
				t.Fatal(err)
			}
		}
	}
	var old model.Status
	rpc("status", nil, &old)
	if old.Version != "0.1.0" {
		t.Fatal("unexpected old helper")
	}
	client := pinnedClient(old.Fingerprint)
	url := old.URLs[len(old.URLs)-1]
	var pending struct {
		ID    string `json:"requestId"`
		Token string `json:"pollToken"`
	}
	if err = requestJSON(context.Background(), client, http.MethodPost, url+"/api/v1/pair/request", "", map[string]string{"name": "Preserved browser", "kind": "browser"}, &pending); err != nil {
		t.Fatal(err)
	}
	rpc("pair.approve", map[string]string{"id": pending.ID}, nil)
	body, _ := json.Marshal(map[string]string{"requestId": pending.ID, "pollToken": pending.Token})
	response, err := client.Post(url+"/api/v1/pair/poll", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	cookies := response.Cookies()
	_ = response.Body.Close()
	if len(cookies) == 0 {
		t.Fatal("old helper did not issue a pairing credential")
	}
	token := cookies[0].Value
	data := []byte("Preserve this received file during the 0.2 upgrade.\n")
	var transfer model.Transfer
	file := model.File{ID: "existing-file", Path: "preserved.txt", Size: int64(len(data))}
	if err = requestJSON(context.Background(), client, http.MethodPost, url+"/api/v1/transfers", token, model.Offer{Files: []model.File{file}}, &transfer); err != nil {
		t.Fatal(err)
	}
	rpc("transfer.accept", map[string]string{"id": transfer.ID}, nil)
	req, _ := http.NewRequest(http.MethodPut, url+"/api/v1/transfers/"+transfer.ID+"/files/"+file.ID+"/chunks/0", bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Chunk-SHA256", digest(data))
	response, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.Status)
	}
	if err = requestJSON(context.Background(), client, http.MethodPost, url+"/api/v1/transfers/"+transfer.ID+"/files/"+file.ID+"/complete", token, map[string]string{"sha256": digest(data)}, nil); err != nil {
		t.Fatal(err)
	}
	rpc("shutdown", nil, nil)
	_ = stdin.Close()
	if err = command.Wait(); err != nil {
		t.Fatal(err)
	}
	finished = true
	upgraded, err := New(Options{DataDir: state, NoDiscovery: true})
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	current := upgraded.Status()
	if current.Version != model.Version || current.ID != old.ID || current.Fingerprint != old.Fingerprint || current.Settings.ReceiveDirectory != received {
		t.Fatal("upgrade replaced identity/settings")
	}
	device, ok := upgraded.store.authenticate(token)
	if !ok || device.CanClipboard {
		t.Fatal("old trust lost or new permission enabled without consent")
	}
	stored, err := upgraded.engine.Get(transfer.ID)
	if err != nil || stored.State != "completed" {
		t.Fatal("old transfer history lost", err)
	}
	got, err := os.ReadFile(filepath.Join(stored.Destination, file.Path))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("old received data changed", err)
	}
	share, err := upgraded.createShare("local", model.ShareCreate{TransferID: transfer.ID, MaxDownloads: 1})
	if err != nil || !strings.Contains(share.URL, "#") {
		t.Fatal("cannot share old received content", err)
	}
}

func TestUpgradeFromReleased020PreservesSharesAndDefaults(t *testing.T) {
	previous := os.Getenv("AIRLINK_PREVIOUS_HELPER_020")
	if previous == "" {
		t.Skip("set AIRLINK_PREVIOUS_HELPER_020 to a built 0.2.0 service")
	}
	version, err := exec.Command(previous, "--version").Output()
	if err != nil || strings.TrimSpace(string(version)) != "0.2.0" {
		t.Fatal("previous helper must report0.2.0", err)
	}
	base := t.TempDir()
	state, receive := filepath.Join(base, "state"), filepath.Join(base, "receive")
	original := filepath.Join(base, "original.txt")
	payload := []byte("An original shared in0.2 remains accessible after upgrading.\n")
	_ = os.WriteFile(original, payload, 0600)
	cmd := exec.Command(previous, "--stdio", "--no-discovery", "--data-dir", state, "--receive-dir", receive)
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := false
	defer func() {
		if !done {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	enc, dec := json.NewEncoder(in), json.NewDecoder(out)
	id := 0
	rpc := func(method string, params any, result any) {
		t.Helper()
		id++
		if e := enc.Encode(map[string]any{"id": id, "method": method, "params": params}); e != nil {
			t.Fatal(e)
		}
		var response struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  string          `json:"error"`
		}
		if e := dec.Decode(&response); e != nil {
			t.Fatal(e)
		}
		if response.ID != id || response.Error != "" {
			t.Fatal(response.Error)
		}
		if result != nil {
			if e := json.Unmarshal(response.Result, result); e != nil {
				t.Fatal(e)
			}
		}
	}
	var old model.Status
	rpc("status", nil, &old)
	var share model.Share
	rpc("share.create", model.ShareCreate{Paths: []string{original}, Password: "upgrade-only-password", MaxDownloads: 1}, &share)
	client := pinnedClient(old.Fingerprint)
	baseURL := old.URLs[len(old.URLs)-1]
	public := baseURL + "/api/v1/public/shares/" + share.ID
	secret := share.URL[strings.LastIndex(share.URL, "#")+1:]
	b, _ := json.Marshal(map[string]string{"secret": secret, "password": "upgrade-only-password"})
	res, err := client.Post(public+"/unlock", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	cookies := res.Cookies()
	res.Body.Close()
	if len(cookies) == 0 {
		t.Fatal("missing share session")
	}
	cookie := cookies[0]
	req, _ := http.NewRequest(http.MethodGet, public+"/files/"+share.Files[0].ID, nil)
	req.AddCookie(cookie)
	req.Header.Set("Range", "bytes=0-7")
	res, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if res.StatusCode != 206 {
		t.Fatal(res.Status)
	}
	rpc("shutdown", nil, nil)
	_ = in.Close()
	if err = cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	done = true
	next, err := New(Options{DataDir: state, NoDiscovery: true})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	status := next.Status()
	if status.Version != model.Version || status.ID != old.ID || status.Fingerprint != old.Fingerprint || status.Settings.ReceiveDirectory != receive || status.Settings.Shortcut != (model.Shortcut{Enabled: true, KeyCode: 37, Modifiers: 768}) || status.Settings.SendLimitBytesPerSecond != 0 {
		t.Fatal("upgrade changed identity or defaults")
	}
	summary, err := next.shares.Session(share.ID, cookie.Value)
	if err != nil || summary.Downloads != 1 || !summary.PasswordProtected {
		t.Fatal("persistent share session/quota lost", summary, err)
	}
	if err = next.shares.BeginDownload(share.ID, cookie.Value); err != nil {
		t.Fatal("resume counted another allowance", err)
	}
	got, err := os.ReadFile(original)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatal("original changed")
	}
}
