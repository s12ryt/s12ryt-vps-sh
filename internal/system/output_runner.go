package system

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
)

type ExecOutputRunnerOptions struct {
	Timeout     time.Duration
	ErrorOutput io.Writer
}

type ExecOutputRunner struct {
	timeout     time.Duration
	errorOutput io.Writer
}

func NewExecOutputRunner(options ExecOutputRunnerOptions) (*ExecOutputRunner, error) {
	if options.Timeout <= 0 {
		return nil, errors.New("system output command timeout must be positive")
	}
	if options.ErrorOutput == nil {
		return nil, errors.New("system output command error writer is required")
	}
	return &ExecOutputRunner{timeout: options.Timeout, errorOutput: options.ErrorOutput}, nil
}

func (runner *ExecOutputRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("system output command context is required")
	}
	command := Command{Name: name, Args: append([]string(nil), args...)}
	if err := validateCommand(command); err != nil {
		return nil, err
	}
	commandContext, cancel := context.WithTimeout(ctx, runner.timeout)
	defer cancel()

	var stdout bytes.Buffer
	process := exec.CommandContext(commandContext, command.Name, command.Args...)
	process.Stdout = &stdout
	process.Stderr = runner.errorOutput
	if err := process.Run(); err != nil {
		if contextErr := commandContext.Err(); contextErr != nil {
			return nil, fmt.Errorf("run %s: %w", formatCommand(command), contextErr)
		}
		return nil, fmt.Errorf("run %s: %w", formatCommand(command), err)
	}
	if err := commandContext.Err(); err != nil {
		return nil, fmt.Errorf("run %s: %w", formatCommand(command), err)
	}
	return append([]byte(nil), stdout.Bytes()...), nil
}
