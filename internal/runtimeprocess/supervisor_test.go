package runtimeprocess

import (
	"context"
	"errors"
	"os"
	"reflect"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestSupervisorStartsReloadsAndStopsSingBoxProcess(t *testing.T) {
	process := newFakeProcess()
	starter := &recordingStarter{process: process}
	supervisor, err := NewSupervisor(SupervisorOptions{
		BinaryPath: "/opt/s12ryt-ipv6/bin/sing-box",
		ConfigPath: "/opt/s12ryt-ipv6/config/sing-box.json",
		Starter:    starter,
	})
	if err != nil {
		t.Fatalf("NewSupervisor() error = %v", err)
	}

	if err := supervisor.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if starter.name != "/opt/s12ryt-ipv6/bin/sing-box" || !reflect.DeepEqual(starter.args, []string{"run", "-c", "/opt/s12ryt-ipv6/config/sing-box.json"}) {
		t.Fatalf("starter call = %s %#v", starter.name, starter.args)
	}
	if err := supervisor.Healthy(context.Background()); err != nil {
		t.Fatalf("Healthy() error = %v", err)
	}
	if err := supervisor.Reload(context.Background()); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if !reflect.DeepEqual(process.signals(), []os.Signal{syscall.SIGHUP}) {
		t.Fatalf("reload signals = %#v", process.signals())
	}
	if err := supervisor.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if !reflect.DeepEqual(process.signals(), []os.Signal{syscall.SIGHUP, syscall.SIGTERM}) {
		t.Fatalf("stop signals = %#v", process.signals())
	}
}

func TestSupervisorReportsUnexpectedProcessExit(t *testing.T) {
	wantErr := errors.New("sing-box crashed")
	process := newFakeProcess()
	supervisor, err := NewSupervisor(SupervisorOptions{
		BinaryPath: "/opt/s12ryt-ipv6/bin/sing-box",
		ConfigPath: "/opt/s12ryt-ipv6/config/sing-box.json",
		Starter:    &recordingStarter{process: process},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Start(); err != nil {
		t.Fatal(err)
	}
	process.exit(wantErr)

	deadline := time.Now().Add(time.Second)
	for {
		err := supervisor.Healthy(context.Background())
		if errors.Is(err, wantErr) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Healthy() error = %v, want process error", err)
		}
		time.Sleep(time.Millisecond)
	}
	if err := supervisor.Reload(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("Reload() error = %v, want process error", err)
	}
}

func TestSupervisorStopHonorsCancelledContext(t *testing.T) {
	process := newFakeProcess()
	process.blockTermination = true
	supervisor, err := NewSupervisor(SupervisorOptions{
		BinaryPath: "/opt/s12ryt-ipv6/bin/sing-box",
		ConfigPath: "/opt/s12ryt-ipv6/config/sing-box.json",
		Starter:    &recordingStarter{process: process},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Start(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := supervisor.Stop(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop() error = %v, want context cancellation", err)
	}
	if !process.killed {
		t.Fatal("cancelled Stop() did not force-kill the child process")
	}
}

func TestNewSupervisorRejectsUnsafeOptions(t *testing.T) {
	valid := SupervisorOptions{
		BinaryPath: "/opt/s12ryt-ipv6/bin/sing-box",
		ConfigPath: "/opt/s12ryt-ipv6/config/sing-box.json",
		Starter:    &recordingStarter{process: newFakeProcess()},
	}
	mutations := []func(*SupervisorOptions){
		func(options *SupervisorOptions) { options.BinaryPath = "sing-box" },
		func(options *SupervisorOptions) { options.ConfigPath = "sing-box.json" },
		func(options *SupervisorOptions) { options.Starter = nil },
	}
	for _, mutate := range mutations {
		options := valid
		mutate(&options)
		if _, err := NewSupervisor(options); err == nil {
			t.Fatalf("NewSupervisor(%#v) accepted unsafe options", options)
		}
	}
}

type recordingStarter struct {
	name    string
	args    []string
	process Process
}

func (starter *recordingStarter) Start(name string, args ...string) (Process, error) {
	starter.name = name
	starter.args = append([]string(nil), args...)
	return starter.process, nil
}

type fakeProcess struct {
	mu               sync.Mutex
	wait             chan error
	recordedSignals  []os.Signal
	killed           bool
	blockTermination bool
	exited           bool
}

func newFakeProcess() *fakeProcess {
	return &fakeProcess{wait: make(chan error, 1)}
}

func (process *fakeProcess) Signal(signal os.Signal) error {
	process.mu.Lock()
	defer process.mu.Unlock()
	process.recordedSignals = append(process.recordedSignals, signal)
	if signal == syscall.SIGTERM && !process.blockTermination && !process.exited {
		process.exited = true
		process.wait <- nil
	}
	return nil
}

func (process *fakeProcess) Wait() error {
	return <-process.wait
}

func (process *fakeProcess) Kill() error {
	process.mu.Lock()
	defer process.mu.Unlock()
	process.killed = true
	if !process.exited {
		process.exited = true
		process.wait <- nil
	}
	return nil
}

func (process *fakeProcess) exit(err error) {
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.exited {
		return
	}
	process.exited = true
	process.wait <- err
}

func (process *fakeProcess) signals() []os.Signal {
	process.mu.Lock()
	defer process.mu.Unlock()
	return append([]os.Signal(nil), process.recordedSignals...)
}
