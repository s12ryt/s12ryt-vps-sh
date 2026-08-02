package system

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
)

type ExecRunnerOptions struct {
	Timeout time.Duration
	Output  io.Writer
}

type ExecRunner struct {
	timeout time.Duration
	output  io.Writer
}

func NewExecRunner(options ExecRunnerOptions) (*ExecRunner, error) {
	if options.Timeout <= 0 {
		return nil, errors.New("system command timeout must be positive")
	}
	if options.Output == nil {
		return nil, errors.New("system command output writer is required")
	}
	return &ExecRunner{timeout: options.Timeout, output: options.Output}, nil
}

func (runner *ExecRunner) Run(ctx context.Context, command Command) error {
	if ctx == nil {
		return errors.New("system command context is required")
	}
	if err := validateCommand(command); err != nil {
		return err
	}
	commandContext, cancel := context.WithTimeout(ctx, runner.timeout)
	defer cancel()

	process := exec.CommandContext(commandContext, command.Name, command.Args...)
	process.Stdout = runner.output
	process.Stderr = runner.output
	if err := process.Run(); err != nil {
		if contextErr := commandContext.Err(); contextErr != nil {
			return fmt.Errorf("run %s: %w", formatCommand(command), contextErr)
		}
		return fmt.Errorf("run %s: %w", formatCommand(command), err)
	}
	if err := commandContext.Err(); err != nil {
		return fmt.Errorf("run %s: %w", formatCommand(command), err)
	}
	return nil
}
