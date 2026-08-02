package runtimeconfig

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
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
