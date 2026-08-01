package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
)

func TestConfigStoreSavesRootOnlyStateAndLoadsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "config.json")
	configStore := NewConfigStore(path)
	config := domain.DefaultConfig()

	if err := configStore.Save(config); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := configStore.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Panel.Port != config.Panel.Port || loaded.SchemaVersion != domain.SchemaVersion {
		t.Fatalf("Load() = %#v, want saved config", loaded)
	}

	directoryInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat(directory) error = %v", err)
	}
	if got := directoryInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("directory mode = %o, want 700", got)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(config) error = %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("config mode = %o, want 600", got)
	}
}

func TestConfigStoreKeepsPreviousVersionAsBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	configStore := NewConfigStore(path)
	original := domain.DefaultConfig()
	if err := configStore.Save(original); err != nil {
		t.Fatalf("first Save() error = %v", err)
	}

	updated := original
	updated.Panel.Port = 35555
	if err := configStore.Save(updated); err != nil {
		t.Fatalf("second Save() error = %v", err)
	}

	backupData, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("ReadFile(backup) error = %v", err)
	}
	var backup domain.Config
	if err := json.Unmarshal(backupData, &backup); err != nil {
		t.Fatalf("Unmarshal(backup) error = %v", err)
	}
	if backup.Panel.Port != original.Panel.Port {
		t.Fatalf("backup port = %d, want %d", backup.Panel.Port, original.Panel.Port)
	}
	loaded, err := configStore.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Panel.Port != updated.Panel.Port {
		t.Fatalf("current port = %d, want %d", loaded.Panel.Port, updated.Panel.Port)
	}
}

func TestConfigStoreRejectsInvalidStateWithoutReplacingCurrentFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	configStore := NewConfigStore(path)
	original := domain.DefaultConfig()
	if err := configStore.Save(original); err != nil {
		t.Fatalf("Save(original) error = %v", err)
	}

	invalid := original
	invalid.Panel.Port = 0
	if err := configStore.Save(invalid); err == nil {
		t.Fatal("Save(invalid) error = nil, want rejection")
	}
	loaded, err := configStore.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Panel.Port != original.Panel.Port {
		t.Fatalf("current port = %d after rejected save, want %d", loaded.Panel.Port, original.Panel.Port)
	}
}

func TestConfigStoreRejectsMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := NewConfigStore(path).Load(); err == nil {
		t.Fatal("Load() error = nil, want malformed JSON rejection")
	}
}
