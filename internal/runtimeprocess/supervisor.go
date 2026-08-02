package runtimeprocess

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

type Process interface {
	Signal(os.Signal) error
	Wait() error
	Kill() error
}

type Starter interface {
	Start(name string, args ...string) (Process, error)
}

type SupervisorOptions struct {
	BinaryPath string
	ConfigPath string
	Starter    Starter
}

type Supervisor struct {
	mu         sync.RWMutex
	binaryPath string
	configPath string
	starter    Starter
	process    Process
	done       chan struct{}
	exitErr    error
	started    bool
	exited     bool
}

func NewSupervisor(options SupervisorOptions) (*Supervisor, error) {
	if !filepath.IsAbs(options.BinaryPath) {
		return nil, errors.New("sing-box binary path must be absolute")
	}
	if !filepath.IsAbs(options.ConfigPath) {
		return nil, errors.New("sing-box configuration path must be absolute")
	}
	if options.Starter == nil {
		return nil, errors.New("sing-box process starter is required")
	}
	return &Supervisor{
		binaryPath: filepath.Clean(options.BinaryPath),
		configPath: filepath.Clean(options.ConfigPath),
		starter:    options.Starter,
	}, nil
}

func (supervisor *Supervisor) Start() error {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	if supervisor.started && !supervisor.exited {
		return errors.New("sing-box process is already running")
	}
	process, err := supervisor.starter.Start(
		supervisor.binaryPath,
		"run", "-c", supervisor.configPath,
	)
	if err != nil {
		return fmt.Errorf("start sing-box process: %w", err)
	}
	supervisor.process = process
	supervisor.done = make(chan struct{})
	supervisor.exitErr = nil
	supervisor.started = true
	supervisor.exited = false
	done := supervisor.done
	go supervisor.wait(process, done)
	return nil
}

func (supervisor *Supervisor) wait(process Process, done chan struct{}) {
	err := process.Wait()
	supervisor.mu.Lock()
	if supervisor.process == process && supervisor.done == done {
		supervisor.exitErr = err
		supervisor.exited = true
		close(done)
	}
	supervisor.mu.Unlock()
}

func (supervisor *Supervisor) Reload(ctx context.Context) error {
	if err := supervisor.checkContext(ctx); err != nil {
		return err
	}
	process, err := supervisor.runningProcess()
	if err != nil {
		return err
	}
	if err := process.Signal(syscall.SIGHUP); err != nil {
		return fmt.Errorf("reload sing-box process: %w", err)
	}
	return nil
}

func (supervisor *Supervisor) Healthy(ctx context.Context) error {
	if err := supervisor.checkContext(ctx); err != nil {
		return err
	}
	_, err := supervisor.runningProcess()
	return err
}

func (supervisor *Supervisor) Stop(ctx context.Context) error {
	if err := supervisor.checkContext(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	supervisor.mu.RLock()
	if !supervisor.started {
		supervisor.mu.RUnlock()
		return errors.New("sing-box process has not been started")
	}
	process := supervisor.process
	done := supervisor.done
	alreadyExited := supervisor.exited
	supervisor.mu.RUnlock()
	if alreadyExited {
		return supervisor.processExitError()
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("stop sing-box process: %w", err)
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		killErr := process.Kill()
		if killErr != nil {
			killErr = fmt.Errorf("kill sing-box process: %w", killErr)
		}
		return errors.Join(ctx.Err(), killErr)
	}
}

func (supervisor *Supervisor) runningProcess() (Process, error) {
	supervisor.mu.RLock()
	defer supervisor.mu.RUnlock()
	if !supervisor.started {
		return nil, errors.New("sing-box process has not been started")
	}
	if supervisor.exited {
		if supervisor.exitErr != nil {
			return nil, fmt.Errorf("sing-box process exited: %w", supervisor.exitErr)
		}
		return nil, errors.New("sing-box process exited unexpectedly")
	}
	return supervisor.process, nil
}

func (supervisor *Supervisor) processExitError() error {
	supervisor.mu.RLock()
	defer supervisor.mu.RUnlock()
	if supervisor.exitErr != nil {
		return fmt.Errorf("sing-box process exited: %w", supervisor.exitErr)
	}
	return nil
}

func (supervisor *Supervisor) checkContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("sing-box process context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}
