package runtimeconfig

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type ValidationRunner interface {
	Run(name string, args ...string) error
}

type SingBoxValidatorOptions struct {
	BinaryPath         string
	TemporaryDirectory string
	Runner             ValidationRunner
}

type SingBoxValidator struct {
	binaryPath         string
	temporaryDirectory string
	runner             ValidationRunner
}

type ExecValidationRunner struct {
	timeout time.Duration
	output  io.Writer
}

func NewExecValidationRunner(timeout time.Duration, output io.Writer) (*ExecValidationRunner, error) {
	if timeout <= 0 {
		return nil, errors.New("sing-box command timeout must be positive")
	}
	if output == nil {
		return nil, errors.New("sing-box command output is required")
	}
	return &ExecValidationRunner{timeout: timeout, output: output}, nil
}

func (runner *ExecValidationRunner) Run(name string, args ...string) error {
	if name == "" {
		return errors.New("sing-box command name is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), runner.timeout)
	defer cancel()

	command := exec.CommandContext(ctx, name, args...)
	command.Stdout = runner.output
	command.Stderr = runner.output
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("sing-box command timed out: %w", ctx.Err())
		}
		return fmt.Errorf("sing-box command failed: %w", err)
	}
	return nil
}

func NewSingBoxValidator(options SingBoxValidatorOptions) (*SingBoxValidator, error) {
	if !filepath.IsAbs(options.BinaryPath) {
		return nil, errors.New("sing-box binary path must be absolute")
	}
	if !filepath.IsAbs(options.TemporaryDirectory) {
		return nil, errors.New("sing-box temporary directory must be absolute")
	}
	if options.Runner == nil {
		return nil, errors.New("sing-box validation runner is required")
	}
	return &SingBoxValidator{
		binaryPath:         filepath.Clean(options.BinaryPath),
		temporaryDirectory: filepath.Clean(options.TemporaryDirectory),
		runner:             options.Runner,
	}, nil
}

func (validator *SingBoxValidator) Validate(payload []byte) error {
	if len(payload) == 0 {
		return errors.New("sing-box configuration payload is empty")
	}
	if err := os.MkdirAll(validator.temporaryDirectory, 0o700); err != nil {
		return fmt.Errorf("create sing-box temporary directory: %w", err)
	}
	temporary, err := os.CreateTemp(validator.temporaryDirectory, ".sing-box-check.*")
	if err != nil {
		return fmt.Errorf("create temporary sing-box configuration: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("secure temporary sing-box configuration: %w", err)
	}
	if _, err := temporary.Write(payload); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary sing-box configuration: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary sing-box configuration: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary sing-box configuration: %w", err)
	}

	if err := validator.runner.Run(validator.binaryPath, "check", "-c", temporaryPath); err != nil {
		return fmt.Errorf("sing-box configuration check failed: %w", err)
	}
	return nil
}
