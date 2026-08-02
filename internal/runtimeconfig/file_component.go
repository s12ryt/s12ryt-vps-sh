package runtimeconfig

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/s12ryt/s12ryt-vps-sh/internal/deploy"
	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
)

type FileComponentOptions struct {
	Path     string
	Resolve  func(domain.Config) (Input, error)
	Validate func([]byte) error
}

type FileComponent struct {
	path     string
	resolve  func(domain.Config) (Input, error)
	validate func([]byte) error
}

type fileChange struct {
	path      string
	previous  []byte
	candidate []byte
}

var _ deploy.Component = (*FileComponent)(nil)
var _ deploy.PreparedChange = (*fileChange)(nil)

func NewFileComponent(options FileComponentOptions) (*FileComponent, error) {
	if !filepath.IsAbs(options.Path) {
		return nil, errors.New("sing-box configuration path must be absolute")
	}
	if options.Resolve == nil {
		return nil, errors.New("runtime configuration resolver is required")
	}
	if options.Validate == nil {
		return nil, errors.New("sing-box configuration validator is required")
	}
	return &FileComponent{
		path:     filepath.Clean(options.Path),
		resolve:  options.Resolve,
		validate: options.Validate,
	}, nil
}

func (component *FileComponent) Prepare(
	ctx context.Context,
	current domain.Config,
	candidate domain.Config,
) (deploy.PreparedChange, error) {
	if ctx == nil {
		return nil, errors.New("deployment context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("prepare sing-box configuration: %w", err)
	}

	previousPayload, err := component.compile(current)
	if err != nil {
		return nil, fmt.Errorf("compile current sing-box configuration: %w", err)
	}
	candidatePayload, err := component.compile(candidate)
	if err != nil {
		return nil, fmt.Errorf("compile candidate sing-box configuration: %w", err)
	}
	if err := component.validate(candidatePayload); err != nil {
		return nil, fmt.Errorf("validate candidate sing-box configuration: %w", err)
	}

	return &fileChange{
		path:      component.path,
		previous:  append([]byte(nil), previousPayload...),
		candidate: append([]byte(nil), candidatePayload...),
	}, nil
}

func (component *FileComponent) compile(config domain.Config) ([]byte, error) {
	input, err := component.resolve(config)
	if err != nil {
		return nil, fmt.Errorf("resolve runtime inputs: %w", err)
	}
	payload, err := CompileServerConfig(input)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func (change *fileChange) Apply(ctx context.Context) error {
	if err := atomicWriteRuntimeConfig(ctx, change.path, change.candidate); err != nil {
		return fmt.Errorf("apply sing-box configuration: %w", err)
	}
	return nil
}

func (change *fileChange) Rollback(ctx context.Context) error {
	if err := atomicWriteRuntimeConfig(ctx, change.path, change.previous); err != nil {
		return fmt.Errorf("restore sing-box configuration: %w", err)
	}
	return nil
}

func atomicWriteRuntimeConfig(ctx context.Context, path string, payload []byte) error {
	if ctx == nil {
		return errors.New("write context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".sing-box.*")
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("secure temporary configuration: %w", err)
	}
	if _, err := temporary.Write(payload); err != nil {
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
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace sing-box configuration: %w", err)
	}
	if err := syncRuntimeDirectory(directory); err != nil {
		return fmt.Errorf("sync configuration directory: %w", err)
	}
	return nil
}

func syncRuntimeDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
