package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
)

type ConfigStore struct {
	path string
}

func NewConfigStore(path string) *ConfigStore {
	return &ConfigStore{path: path}
}

func (store *ConfigStore) Load() (domain.Config, error) {
	data, err := os.ReadFile(store.path)
	if err != nil {
		return domain.Config{}, fmt.Errorf("read configuration: %w", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var config domain.Config
	if err := decoder.Decode(&config); err != nil {
		return domain.Config{}, fmt.Errorf("decode configuration: %w", err)
	}
	if err := config.Validate(); err != nil {
		return domain.Config{}, fmt.Errorf("validate configuration: %w", err)
	}
	return config, nil
}

func (store *ConfigStore) Save(config domain.Config) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("validate configuration: %w", err)
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode configuration: %w", err)
	}
	data = append(data, '\n')

	directory := filepath.Dir(store.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("secure configuration directory: %w", err)
	}

	temporary, err := os.CreateTemp(directory, ".config.*")
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("secure temporary configuration: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary configuration: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary configuration: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary configuration: %w", err)
	}

	if err := store.backupCurrent(directory); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, store.path); err != nil {
		return fmt.Errorf("replace configuration: %w", err)
	}
	if err := syncDirectory(directory); err != nil {
		return fmt.Errorf("sync configuration directory: %w", err)
	}
	return nil
}

func (store *ConfigStore) backupCurrent(directory string) error {
	source, err := os.Open(store.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open current configuration: %w", err)
	}
	defer source.Close()

	backup, err := os.CreateTemp(directory, ".backup.*")
	if err != nil {
		return fmt.Errorf("create configuration backup: %w", err)
	}
	backupPath := backup.Name()
	defer os.Remove(backupPath)

	if err := backup.Chmod(0o600); err != nil {
		backup.Close()
		return fmt.Errorf("secure configuration backup: %w", err)
	}
	if _, err := io.Copy(backup, source); err != nil {
		backup.Close()
		return fmt.Errorf("write configuration backup: %w", err)
	}
	if err := backup.Sync(); err != nil {
		backup.Close()
		return fmt.Errorf("sync configuration backup: %w", err)
	}
	if err := backup.Close(); err != nil {
		return fmt.Errorf("close configuration backup: %w", err)
	}
	if err := os.Rename(backupPath, store.path+".bak"); err != nil {
		return fmt.Errorf("replace configuration backup: %w", err)
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
