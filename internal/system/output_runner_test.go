package system

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExecOutputRunnerReturnsStdoutAndSeparatesDiagnostics(t *testing.T) {
	installSystemHelperCommand(t, "ip")
	var diagnostics bytes.Buffer
	runner, err := NewExecOutputRunner(ExecOutputRunnerOptions{
		Timeout:     time.Second,
		ErrorOutput: &diagnostics,
	})
	if err != nil {
		t.Fatalf("NewExecOutputRunner() error = %v", err)
	}

	payload, err := runner.Output(
		context.Background(),
		"ip",
		"-test.run=TestSystemExecHelperProcess",
		"--",
		"success",
		`literal;$HOME`,
	)
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}
	if string(payload) != "stdout:literal;$HOME\n" {
		t.Fatalf("Output() = %q", payload)
	}
	if diagnostics.String() != "stderr:literal;$HOME\n" {
		t.Fatalf("diagnostics = %q", diagnostics.String())
	}
	if home := os.Getenv("HOME"); home != "" && strings.Contains(string(payload), home) {
		t.Fatalf("shell expanded literal argument: %q", payload)
	}
}

func TestExecOutputRunnerReportsExitFailureAndTimeout(t *testing.T) {
	installSystemHelperCommand(t, "ip")
	for name, testCase := range map[string]struct {
		timeout time.Duration
		mode    string
		target  error
	}{
		"exit failure": {timeout: time.Second, mode: "failure"},
		"timeout":      {timeout: 20 * time.Millisecond, mode: "wait", target: context.DeadlineExceeded},
	} {
		t.Run(name, func(t *testing.T) {
			runner, err := NewExecOutputRunner(ExecOutputRunnerOptions{
				Timeout:     testCase.timeout,
				ErrorOutput: io.Discard,
			})
			if err != nil {
				t.Fatalf("NewExecOutputRunner() error = %v", err)
			}
			_, err = runner.Output(
				context.Background(),
				"ip",
				"-test.run=TestSystemExecHelperProcess",
				"--",
				testCase.mode,
				"value",
			)
			if err == nil {
				t.Fatal("Output() error = nil, want failure")
			}
			if testCase.target != nil && !errors.Is(err, testCase.target) {
				t.Fatalf("Output() error = %v, want %v", err, testCase.target)
			}
		})
	}
}

func TestExecOutputRunnerRejectsUnsafeConfigurationAndCommand(t *testing.T) {
	for name, options := range map[string]ExecOutputRunnerOptions{
		"zero timeout": {ErrorOutput: io.Discard},
		"nil output":   {Timeout: time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewExecOutputRunner(options); err == nil {
				t.Fatal("NewExecOutputRunner() error = nil, want rejection")
			}
		})
	}

	runner, err := NewExecOutputRunner(ExecOutputRunnerOptions{Timeout: time.Second, ErrorOutput: io.Discard})
	if err != nil {
		t.Fatalf("NewExecOutputRunner() error = %v", err)
	}
	for name, call := range map[string]struct {
		ctx  context.Context
		name string
		args []string
	}{
		"shell":       {ctx: context.Background(), name: "sh", args: []string{"-c", "id"}},
		"empty":       {ctx: context.Background()},
		"null byte":   {ctx: context.Background(), name: "ip", args: []string{"bad\x00argument"}},
		"missing ctx": {name: "ip"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := runner.Output(call.ctx, call.name, call.args...); err == nil {
				t.Fatal("Output() error = nil, want rejection")
			}
		})
	}
}
