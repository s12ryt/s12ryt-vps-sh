package runtimeprocess

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestExecStarterRunsProcessWithoutShellAndCapturesOutput(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	t.Setenv("S12RYT_EXEC_STARTER_HELPER", "1")
	var output bytes.Buffer
	starter, err := NewExecStarter(&output)
	if err != nil {
		t.Fatalf("NewExecStarter() error = %v", err)
	}

	process, err := starter.Start(executable, "-test.run=TestExecStarterHelperProcess", "--", "literal;$HOME")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := process.Wait(); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if got := output.String(); !strings.Contains(got, "stdout:literal;$HOME") || !strings.Contains(got, "stderr:literal;$HOME") {
		t.Fatalf("captured output = %q", got)
	}
}

func TestNewExecStarterRejectsUnsafeOptions(t *testing.T) {
	if _, err := NewExecStarter(nil); err == nil {
		t.Fatal("NewExecStarter(nil) accepted missing output")
	}
	starter, err := NewExecStarter(&bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := starter.Start(""); err == nil {
		t.Fatal("Start() accepted an empty executable name")
	}
}

func TestExecStarterHelperProcess(t *testing.T) {
	if os.Getenv("S12RYT_EXEC_STARTER_HELPER") != "1" {
		return
	}
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		os.Exit(2)
	}
	argument := os.Args[separator+1]
	_, _ = os.Stdout.WriteString("stdout:" + argument + "\n")
	_, _ = os.Stderr.WriteString("stderr:" + argument + "\n")
	os.Exit(0)
}
