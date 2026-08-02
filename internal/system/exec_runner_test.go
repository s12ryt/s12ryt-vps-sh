package system

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecRunnerExecutesAllowedCommandWithoutShellExpansion(t *testing.T) {
	installSystemHelperCommand(t, "ip")
	var output bytes.Buffer
	runner, err := NewExecRunner(ExecRunnerOptions{Timeout: time.Second, Output: &output})
	if err != nil {
		t.Fatalf("NewExecRunner() error = %v", err)
	}
	command := Command{
		Name: "ip",
		Args: []string{"-test.run=TestSystemExecHelperProcess", "--", "success", `literal;$HOME`},
	}
	if err := runner.Run(context.Background(), command); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	for _, required := range []string{"stdout:literal;$HOME", "stderr:literal;$HOME"} {
		if !strings.Contains(output.String(), required) {
			t.Fatalf("output missing %q: %q", required, output.String())
		}
	}
	if home := os.Getenv("HOME"); home != "" && strings.Contains(output.String(), home) {
		t.Fatalf("shell expanded literal argument: %q", output.String())
	}
}

func TestExecRunnerReportsExitFailureAndTimeout(t *testing.T) {
	installSystemHelperCommand(t, "nft")
	for name, timeout, mode, target := range map[string]struct {
		timeout time.Duration
		mode    string
		target  error
	}{
		"exit failure": {timeout: time.Second, mode: "failure"},
		"timeout":      {timeout: 20 * time.Millisecond, mode: "wait", target: context.DeadlineExceeded},
	} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			runner, err := NewExecRunner(ExecRunnerOptions{Timeout: timeout, Output: &output})
			if err != nil {
				t.Fatalf("NewExecRunner() error = %v", err)
			}
			err = runner.Run(context.Background(), Command{
				Name: "nft",
				Args: []string{"-test.run=TestSystemExecHelperProcess", "--", mode, "value"},
			})
			if err == nil {
				t.Fatal("Run() error = nil, want failure")
			}
			if target != nil && !errors.Is(err, target) {
				t.Fatalf("Run() error = %v, want %v", err, target)
			}
		})
	}
}

func TestExecRunnerRejectsUnsafeConfigurationAndCommand(t *testing.T) {
	for name, options := range map[string]ExecRunnerOptions{
		"zero timeout": {Output: io.Discard},
		"nil output":   {Timeout: time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewExecRunner(options); err == nil {
				t.Fatal("NewExecRunner() error = nil, want rejection")
			}
		})
	}

	runner, err := NewExecRunner(ExecRunnerOptions{Timeout: time.Second, Output: io.Discard})
	if err != nil {
		t.Fatalf("NewExecRunner() error = %v", err)
	}
	for name, command := range map[string]Command{
		"shell":       {Name: "sh", Args: []string{"-c", "id"}},
		"empty":       {},
		"null byte":   {Name: "ip", Args: []string{"bad\x00argument"}},
		"missing ctx": {Name: "ip"},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			if name == "missing ctx" {
				ctx = nil
			}
			if err := runner.Run(ctx, command); err == nil {
				t.Fatal("Run() error = nil, want rejection")
			}
		})
	}
}

func TestSystemExecHelperProcess(t *testing.T) {
	if os.Getenv("S12RYT_SYSTEM_HELPER") != "1" {
		return
	}
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || len(os.Args) <= separator+2 {
		os.Exit(93)
	}
	mode := os.Args[separator+1]
	value := os.Args[separator+2]
	switch mode {
	case "success":
		fmt.Fprintf(os.Stdout, "stdout:%s\n", value)
		fmt.Fprintf(os.Stderr, "stderr:%s\n", value)
		os.Exit(0)
	case "failure":
		fmt.Fprintln(os.Stderr, "expected failure")
		os.Exit(17)
	case "wait":
		time.Sleep(5 * time.Second)
		os.Exit(0)
	default:
		os.Exit(94)
	}
}

func installSystemHelperCommand(t *testing.T, name string) {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, name)
	if err := os.Symlink(os.Args[0], path); err != nil {
		t.Fatalf("Symlink(helper) error = %v", err)
	}
	t.Setenv("PATH", directory)
	t.Setenv("S12RYT_SYSTEM_HELPER", "1")
}
