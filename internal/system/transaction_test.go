package system

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type runnerResult struct {
	err error
}

type recordingRunner struct {
	calls   []Command
	results map[string][]runnerResult
}

func (runner *recordingRunner) Run(_ context.Context, command Command) error {
	runner.calls = append(runner.calls, command)
	key := command.Name + " " + strings.Join(command.Args, " ")
	results := runner.results[key]
	if len(results) == 0 {
		return nil
	}
	runner.results[key] = results[1:]
	return results[0].err
}

func TestTransactionExecutesAllStepsWithoutRollbackOnSuccess(t *testing.T) {
	runner := &recordingRunner{}
	transaction, err := NewTransaction(runner)
	if err != nil {
		t.Fatalf("NewTransaction: %v", err)
	}
	steps := []Step{
		{
			Apply:    Command{Name: "ip", Args: []string{"-6", "addr", "add", "2001:db8::10/64", "dev", "eth0"}},
			Rollback: Command{Name: "ip", Args: []string{"-6", "addr", "del", "2001:db8::10/64", "dev", "eth0"}},
		},
		{
			Apply:    Command{Name: "nft", Args: []string{"add", "table", "inet", "s12ryt-ipv6"}},
			Rollback: Command{Name: "nft", Args: []string{"delete", "table", "inet", "s12ryt-ipv6"}},
		},
	}

	if err := transaction.Execute(context.Background(), steps); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := []Command{steps[0].Apply, steps[1].Apply}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
}

func TestTransactionRollsBackCompletedStepsInReverseOrder(t *testing.T) {
	applyFailure := errors.New("firewall apply failed")
	runner := &recordingRunner{results: map[string][]runnerResult{
		"ufw allow 34456/tcp comment s12ryt-ipv6": {{err: applyFailure}},
	}}
	transaction, err := NewTransaction(runner)
	if err != nil {
		t.Fatalf("NewTransaction: %v", err)
	}
	steps := []Step{
		{
			Apply:    Command{Name: "ip", Args: []string{"-6", "addr", "add", "2001:db8::10/64", "dev", "eth0"}},
			Rollback: Command{Name: "ip", Args: []string{"-6", "addr", "del", "2001:db8::10/64", "dev", "eth0"}},
		},
		{
			Apply:    Command{Name: "ip", Args: []string{"-6", "rule", "add", "from", "2001:db8::10/128", "lookup", "42000"}},
			Rollback: Command{Name: "ip", Args: []string{"-6", "rule", "del", "from", "2001:db8::10/128", "lookup", "42000"}},
		},
		{
			Apply:    Command{Name: "ufw", Args: []string{"allow", "34456/tcp", "comment", "s12ryt-ipv6"}},
			Rollback: Command{Name: "ufw", Args: []string{"delete", "allow", "34456/tcp", "comment", "s12ryt-ipv6"}},
		},
	}

	err = transaction.Execute(context.Background(), steps)
	if !errors.Is(err, applyFailure) {
		t.Fatalf("Execute error = %v, want apply failure", err)
	}
	want := []Command{
		steps[0].Apply,
		steps[1].Apply,
		steps[2].Apply,
		steps[1].Rollback,
		steps[0].Rollback,
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
}

func TestTransactionReportsRollbackFailures(t *testing.T) {
	applyFailure := errors.New("service start failed")
	rollbackFailure := errors.New("address removal failed")
	runner := &recordingRunner{results: map[string][]runnerResult{
		"systemctl enable --now s12ryt-ipv6.service": {{err: applyFailure}},
		"ip -6 addr del 2001:db8::10/64 dev eth0":    {{err: rollbackFailure}},
	}}
	transaction, err := NewTransaction(runner)
	if err != nil {
		t.Fatalf("NewTransaction: %v", err)
	}
	steps := []Step{
		{
			Apply:    Command{Name: "ip", Args: []string{"-6", "addr", "add", "2001:db8::10/64", "dev", "eth0"}},
			Rollback: Command{Name: "ip", Args: []string{"-6", "addr", "del", "2001:db8::10/64", "dev", "eth0"}},
		},
		{
			Apply:    Command{Name: "systemctl", Args: []string{"enable", "--now", "s12ryt-ipv6.service"}},
			Rollback: Command{Name: "systemctl", Args: []string{"disable", "--now", "s12ryt-ipv6.service"}},
		},
	}

	err = transaction.Execute(context.Background(), steps)
	if !errors.Is(err, applyFailure) || !errors.Is(err, rollbackFailure) {
		t.Fatalf("Execute error = %v, want apply and rollback failures", err)
	}
}

func TestTransactionRejectsUnsafePlanBeforeRunningAnything(t *testing.T) {
	tests := map[string][]Step{
		"missing runner":   nil,
		"empty command":    {{Apply: Command{}, Rollback: Command{Name: "ip", Args: []string{"-6"}}}},
		"shell command":    {{Apply: Command{Name: "sh", Args: []string{"-c", "rm -rf /"}}, Rollback: Command{Name: "ip", Args: []string{"-6"}}}},
		"missing rollback": {{Apply: Command{Name: "ip", Args: []string{"-6", "addr", "add"}}}},
	}

	for name, steps := range tests {
		t.Run(name, func(t *testing.T) {
			runner := &recordingRunner{}
			if name == "missing runner" {
				if _, err := NewTransaction(nil); err == nil {
					t.Fatal("NewTransaction(nil) succeeded")
				}
				return
			}
			transaction, err := NewTransaction(runner)
			if err != nil {
				t.Fatalf("NewTransaction: %v", err)
			}
			if err := transaction.Execute(context.Background(), steps); err == nil {
				t.Fatal("Execute succeeded for unsafe plan")
			}
			if len(runner.calls) != 0 {
				t.Fatalf("unsafe plan executed commands: %#v", runner.calls)
			}
		})
	}
}
