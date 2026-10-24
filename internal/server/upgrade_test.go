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
	if current.Version != "0.2.0" || current.ID != old.ID || current.Fingerprint != old.Fingerprint || current.Settings.ReceiveDirectory != received {
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
