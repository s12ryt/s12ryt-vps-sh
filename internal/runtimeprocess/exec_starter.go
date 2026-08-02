package runtimeprocess

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

type ExecStarter struct {
	output io.Writer
}

type execProcess struct {
	command *exec.Cmd
}

func NewExecStarter(output io.Writer) (*ExecStarter, error) {
	if output == nil {
		return nil, errors.New("process output is required")
	}
	return &ExecStarter{output: output}, nil
}

func (starter *ExecStarter) Start(name string, args ...string) (Process, error) {
	if name == "" {
		return nil, errors.New("process executable name is required")
	}
	command := exec.Command(name, args...)
	command.Stdout = starter.output
	command.Stderr = starter.output
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start process: %w", err)
	}
	return &execProcess{command: command}, nil
}

func (process *execProcess) Signal(signal os.Signal) error {
	if err := process.command.Process.Signal(signal); err != nil {
		return fmt.Errorf("signal process: %w", err)
	}
	return nil
}

func (process *execProcess) Wait() error {
	return process.command.Wait()
}

func (process *execProcess) Kill() error {
	if err := process.command.Process.Kill(); err != nil {
		return fmt.Errorf("kill process: %w", err)
	}
	return nil
}
