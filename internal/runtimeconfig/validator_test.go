package runtimeconfig

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSingBoxValidatorChecksProtectedTemporaryConfiguration(t *testing.T) {
	temporaryDirectory := t.TempDir()
	payload := []byte("{\"inbounds\":[],\"outbounds\":[]}\n")
	runner := &recordingValidatorRunner{t: t, wantPayload: payload}
	validator, err := NewSingBoxValidator(SingBoxValidatorOptions{
		BinaryPath:         "/opt/s12ryt-ipv6/bin/sing-box",
		TemporaryDirectory: temporaryDirectory,
		Runner:             runner,
	})
	if err != nil {
		t.Fatalf("NewSingBoxValidator() error = %v", err)
	}

	if err := validator.Validate(payload); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if runner.name != "/opt/s12ryt-ipv6/bin/sing-box" {
		t.Fatalf("runner name = %q", runner.name)
	}
	if len(runner.args) != 3 || runner.args[0] != "check" || runner.args[1] != "-c" {
		t.Fatalf("runner args = %#v", runner.args)
	}
	if _, err := os.Stat(runner.args[2]); !os.IsNotExist(err) {
		t.Fatalf("temporary configuration remains after validation: %v", err)
	}
}

func TestSingBoxValidatorPreservesCheckFailureAndCleansTemporaryFile(t *testing.T) {
	wantErr := errors.New("sing-box rejected configuration")
	runner := &recordingValidatorRunner{t: t, wantErr: wantErr}
	validator, err := NewSingBoxValidator(SingBoxValidatorOptions{
		BinaryPath:         "/opt/s12ryt-ipv6/bin/sing-box",
		TemporaryDirectory: t.TempDir(),
		Runner:             runner,
	})
	if err != nil {
		t.Fatalf("NewSingBoxValidator() error = %v", err)
	}

	if err := validator.Validate([]byte("{}\n")); !errors.Is(err, wantErr) {
		t.Fatalf("Validate() error = %v, want errors.Is(%v)", err, wantErr)
	}
	if _, err := os.Stat(runner.args[2]); !os.IsNotExist(err) {
		t.Fatalf("temporary configuration remains after failure: %v", err)
	}
}

func TestNewSingBoxValidatorRejectsUnsafeOptions(t *testing.T) {
	valid := SingBoxValidatorOptions{
		BinaryPath:         "/opt/s12ryt-ipv6/bin/sing-box",
		TemporaryDirectory: "/opt/s12ryt-ipv6/config",
		Runner:             &recordingValidatorRunner{t: t},
	}
	for name, mutate := range map[string]func(*SingBoxValidatorOptions){
		"relative binary":    func(options *SingBoxValidatorOptions) { options.BinaryPath = "sing-box" },
		"relative directory": func(options *SingBoxValidatorOptions) { options.TemporaryDirectory = "config" },
		"missing runner":     func(options *SingBoxValidatorOptions) { options.Runner = nil },
	} {
		t.Run(name, func(t *testing.T) {
			options := valid
			mutate(&options)
			if _, err := NewSingBoxValidator(options); err == nil {
				t.Fatal("NewSingBoxValidator() error = nil, want rejection")
			}
		})
	}
}

func TestSingBoxValidatorRejectsEmptyPayloadBeforeRunningCommand(t *testing.T) {
	runner := &recordingValidatorRunner{t: t}
	validator, err := NewSingBoxValidator(SingBoxValidatorOptions{
		BinaryPath:         "/opt/s12ryt-ipv6/bin/sing-box",
		TemporaryDirectory: t.TempDir(),
		Runner:             runner,
	})
	if err != nil {
		t.Fatalf("NewSingBoxValidator() error = %v", err)
	}
	if err := validator.Validate(nil); err == nil {
		t.Fatal("Validate(nil) error = nil, want rejection")
	}
	if runner.called {
		t.Fatal("runner called for an empty payload")
	}
}

func TestExecValidationRunnerExecutesCommandAndCapturesOutput(t *testing.T) {
	t.Setenv("S12RYT_VALIDATOR_HELPER_MODE", "success")
	var output bytes.Buffer
	runner, err := NewExecValidationRunner(time.Second, &output)
	if err != nil {
		t.Fatalf("NewExecValidationRunner() error = %v", err)
	}
	if err := runner.Run(
		os.Args[0],
		"-test.run=TestValidationRunnerHelperProcess",
		"--",
		"check",
		"-c",
		"/tmp/sing-box.json",
	); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if output.String() != "validated\n" {
		t.Fatalf("runner output = %q", output.String())
	}
}

func TestExecValidationRunnerPreservesFailureAndTimeout(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		mode    string
		timeout time.Duration
		wantErr error
	}{
		{name: "non-zero exit", mode: "failure", timeout: time.Second},
		{name: "timeout", mode: "sleep", timeout: 20 * time.Millisecond, wantErr: context.DeadlineExceeded},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("S12RYT_VALIDATOR_HELPER_MODE", testCase.mode)
			var output bytes.Buffer
			runner, err := NewExecValidationRunner(testCase.timeout, &output)
			if err != nil {
				t.Fatalf("NewExecValidationRunner() error = %v", err)
			}
			err = runner.Run(os.Args[0], "-test.run=TestValidationRunnerHelperProcess")
			if err == nil {
				t.Fatal("Run() error = nil, want failure")
			}
			if testCase.wantErr != nil && !errors.Is(err, testCase.wantErr) {
				t.Fatalf("Run() error = %v, want errors.Is(%v)", err, testCase.wantErr)
			}
			if output.Len() == 0 {
				t.Fatal("runner did not preserve command output")
			}
		})
	}
}

func TestNewExecValidationRunnerRejectsUnsafeOptions(t *testing.T) {
	if _, err := NewExecValidationRunner(0, &bytes.Buffer{}); err == nil {
		t.Fatal("zero timeout was accepted")
	}
	if _, err := NewExecValidationRunner(time.Second, nil); err == nil {
		t.Fatal("nil output was accepted")
	}
}

func TestValidationRunnerHelperProcess(t *testing.T) {
	switch os.Getenv("S12RYT_VALIDATOR_HELPER_MODE") {
	case "success":
		args := os.Args
		separator := -1
		for index, argument := range args {
			if argument == "--" {
				separator = index
				break
			}
		}
		commandArgs := args[separator+1:]
		if separator < 0 || len(commandArgs) != 3 || commandArgs[0] != "check" || commandArgs[1] != "-c" || commandArgs[2] != "/tmp/sing-box.json" {
			os.Stderr.WriteString("unexpected arguments\n")
			os.Exit(2)
		}
		os.Stdout.WriteString("validated\n")
		os.Exit(0)
	case "failure":
		os.Stderr.WriteString("configuration rejected\n")
		os.Exit(9)
	case "sleep":
		os.Stdout.WriteString("waiting\n")
		time.Sleep(time.Second)
		os.Exit(0)
	}
}

type recordingValidatorRunner struct {
	t           *testing.T
	wantPayload []byte
	wantErr     error
	called      bool
	name        string
	args        []string
}

func (runner *recordingValidatorRunner) Run(name string, args ...string) error {
	runner.called = true
	runner.name = name
	runner.args = append([]string(nil), args...)
	if len(args) != 3 {
		runner.t.Fatalf("Run() args = %#v", args)
	}
	path := args[2]
	if filepath.Dir(path) == "." {
		runner.t.Fatalf("temporary path is not rooted: %q", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		runner.t.Fatalf("stat temporary configuration: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		runner.t.Fatalf("temporary config mode = %04o, want 0600", info.Mode().Perm())
	}
	if runner.wantPayload != nil {
		contents, err := os.ReadFile(path)
		if err != nil {
			runner.t.Fatalf("read temporary configuration: %v", err)
		}
		if string(contents) != string(runner.wantPayload) {
			runner.t.Fatalf("temporary payload = %q, want %q", contents, runner.wantPayload)
		}
	}
	return runner.wantErr
}
