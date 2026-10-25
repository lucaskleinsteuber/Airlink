package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/lucasfd3/Airlink/internal/model"
)

func TestNewSetupDefaultsToDownloadsAndPreservesLegacyCustomSettings(t *testing.T) {
	store, err := openStore(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if store.snapshot().Settings.ReceiveDirectory != defaultReceiveDirectory() || store.snapshot().Settings.SetupCompleted {
		t.Fatal("new setup silently completed or nested its receive folder", store.snapshot().Settings)
	}
	legacyDir := t.TempDir()
	custom := filepath.Join(t.TempDir(), "Existing custom destination")
	fixture := persisted{ID: "old-identity", Settings: model.Settings{Name: "Old saved name", ReceiveDirectory: custom, Appearance: "dark", Language: "en", Notifications: true, Organization: model.Organization{Rules: []model.SortRule{}}}, Devices: map[string]deviceRecord{"trusted": {Device: model.Device{ID: "trusted", Paired: true, AutoAccept: true}}}}
	data, _ := json.Marshal(fixture)
	if err = os.WriteFile(filepath.Join(legacyDir, "settings.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := openStore(legacyDir, "")
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.snapshot()
	if got.ID != fixture.ID || got.Settings.Name != fixture.Settings.Name || got.Settings.ReceiveDirectory != custom || got.Settings.SetupCompleted || !got.Devices["trusted"].Device.AutoAccept {
		t.Fatal("opening setup rewrote legacy data", got.Settings)
	}
}

func TestSetupCompletionFailureDoesNotChangeSavedOrIncomingConfiguration(t *testing.T) {
	s := integrationHost(t)
	before := s.store.snapshot().Settings
	settings := before
	settings.SetupCompleted = true
	settings.Name = "Laptop Windows"
	settings.ReceiveDirectory = filepath.Join(t.TempDir(), "Chosen Downloads")
	settings.Organization = model.Organization{DetectDuplicates: true, Rules: []model.SortRule{}}
	payload, _ := json.Marshal(settings)
	originalPath := s.store.path
	blocked := filepath.Join(t.TempDir(), "blocked-parent")
	if err := os.WriteFile(blocked, []byte("file, not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	s.store.path = filepath.Join(blocked, "settings.json")
	if _, err := s.Control("settings.update", payload); err == nil {
		t.Fatal("failed setup write claimed success")
	}
	s.store.path = originalPath
	if after := s.store.snapshot().Settings; !reflect.DeepEqual(before, after) {
		t.Fatal("failed setup changed live settings", after)
	}
	data, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved persisted
	if err = json.Unmarshal(data, &saved); err != nil || saved.Settings.SetupCompleted || saved.Settings.Name != before.Name || saved.Settings.ReceiveDirectory != before.ReceiveDirectory {
		t.Fatal("failed setup changed persisted settings", saved.Settings, err)
	}
	device, _ := integrationDevice(t, s, true)
	incoming, err := s.engine.CreateIncoming(device, []model.File{{ID: "new", Path: "still-current.txt", Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := filepath.EvalSymlinks(before.ReceiveDirectory)
	if incoming.Destination != expected {
		t.Fatal("failed setup changed incoming destination", incoming.Destination)
	}
	if _, err = s.Control("settings.update", payload); err != nil {
		t.Fatal("setup retry failed", err)
	}
	if after := s.store.snapshot().Settings; !after.SetupCompleted || after.Name != "Laptop Windows" || after.ReceiveDirectory != settings.ReceiveDirectory || !after.Organization.DetectDuplicates {
		t.Fatal("confirmed setup was not saved", after)
	}
}

func TestSetupValidationFailureCannotMarkSetupComplete(t *testing.T) {
	s := integrationHost(t)
	settings := s.store.snapshot().Settings
	settings.SetupCompleted = true
	settings.Name = " "
	payload, _ := json.Marshal(settings)
	if _, err := s.Control("settings.update", payload); err == nil {
		t.Fatal("invalid setup accepted")
	}
	if s.store.snapshot().Settings.SetupCompleted {
		t.Fatal("invalid setup was marked complete")
	}
}
