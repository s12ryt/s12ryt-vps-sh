package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/s12ryt/s12ryt-vps-sh/internal/resourceprofile"
	"github.com/s12ryt/s12ryt-vps-sh/internal/runtimeconfig"
	"github.com/s12ryt/s12ryt-vps-sh/internal/store"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: resource-fixture OUTPUT_DIRECTORY")
		os.Exit(2)
	}
	if err := writeFixture(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "resource fixture:", err)
		os.Exit(1)
	}
}

func writeFixture(output string) error {
	absoluteOutput, err := filepath.Abs(output)
	if err != nil {
		return fmt.Errorf("resolve output directory: %w", err)
	}
	if _, err := os.Lstat(absoluteOutput); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return fmt.Errorf("inspect output directory: %w", err)
		}
		return errors.New("output directory already exists")
	}
	if err := os.MkdirAll(absoluteOutput, 0o700); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.Chmod(absoluteOutput, 0o700); err != nil {
		return fmt.Errorf("secure output directory: %w", err)
	}
	completed := false
	defer func() {
		if !completed {
			_ = os.RemoveAll(absoluteOutput)
		}
	}()

	entropy := make([]byte, resourceprofile.ResourceNodeCount*16)
	for nodeIndex := 0; nodeIndex < resourceprofile.ResourceNodeCount; nodeIndex++ {
		for byteIndex := 0; byteIndex < 16; byteIndex++ {
			entropy[nodeIndex*16+byteIndex] = byte(nodeIndex + byteIndex)
		}
	}
	profile, err := resourceprofile.Build(bytes.NewReader(entropy))
	if err != nil {
		return fmt.Errorf("build workload: %w", err)
	}

	configPath := filepath.Join(absoluteOutput, "config.json")
	if err := store.NewConfigStore(configPath).Save(profile.Config); err != nil {
		return fmt.Errorf("save configuration: %w", err)
	}
	statePath := filepath.Join(absoluteOutput, "runtime.json")
	stateStore, err := runtimeconfig.NewDeploymentStateStore(statePath)
	if err != nil {
		return fmt.Errorf("create deployment state store: %w", err)
	}
	if err := stateStore.Save(profile.State); err != nil {
		return fmt.Errorf("save deployment state: %w", err)
	}
	input, err := profile.State.Resolve(profile.Config)
	if err != nil {
		return fmt.Errorf("resolve workload: %w", err)
	}
	payload, err := runtimeconfig.CompileServerConfig(input)
	if err != nil {
		return fmt.Errorf("compile sing-box configuration: %w", err)
	}
	if err := writeProtectedFile(filepath.Join(absoluteOutput, "sing-box.json"), payload); err != nil {
		return err
	}

	var addresses strings.Builder
	for _, address := range profile.Addresses {
		addresses.WriteString(address.String())
		addresses.WriteByte('\n')
	}
	if err := writeProtectedFile(filepath.Join(absoluteOutput, "addresses.txt"), []byte(addresses.String())); err != nil {
		return err
	}
	completed = true
	return nil
}

func writeProtectedFile(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", filepath.Base(path), err)
	}
	completed := false
	defer func() {
		if !completed {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(content); err != nil {
		file.Close()
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync %s: %w", filepath.Base(path), err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", filepath.Base(path), err)
	}
	completed = true
	return nil
}
