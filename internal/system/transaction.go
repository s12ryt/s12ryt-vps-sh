package system

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var allowedCommands = map[string]struct{}{
	"firewall-cmd": {},
	"ip":           {},
	"nft":          {},
	"rc-service":   {},
	"rc-update":    {},
	"sing-box":     {},
	"systemctl":    {},
	"ufw":          {},
}

type Command struct {
	Name string
	Args []string
}

type Step struct {
	Apply    Command
	Rollback Command
}

type Runner interface {
	Run(context.Context, Command) error
}

type Transaction struct {
	runner Runner
}

func NewTransaction(runner Runner) (*Transaction, error) {
	if runner == nil {
		return nil, errors.New("transaction runner is required")
	}
	return &Transaction{runner: runner}, nil
}

func (transaction *Transaction) Execute(ctx context.Context, steps []Step) error {
	if ctx == nil {
		return errors.New("transaction context is required")
	}
	if err := validateSteps(steps); err != nil {
		return err
	}

	completed := make([]Step, 0, len(steps))
	for _, step := range steps {
		if err := transaction.runner.Run(ctx, step.Apply); err != nil {
			applyErr := fmt.Errorf("apply %s: %w", formatCommand(step.Apply), err)
			return errors.Join(applyErr, transaction.rollback(context.WithoutCancel(ctx), completed))
		}
		completed = append(completed, step)
	}
	return nil
}

func (transaction *Transaction) rollback(ctx context.Context, completed []Step) error {
	var rollbackErrors []error
	for index := len(completed) - 1; index >= 0; index-- {
		command := completed[index].Rollback
		if err := transaction.runner.Run(ctx, command); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback %s: %w", formatCommand(command), err))
		}
	}
	return errors.Join(rollbackErrors...)
}

func validateSteps(steps []Step) error {
	if len(steps) == 0 {
		return errors.New("transaction requires at least one step")
	}
	for index, step := range steps {
		if err := validateCommand(step.Apply); err != nil {
			return fmt.Errorf("step %d apply command: %w", index, err)
		}
		if err := validateCommand(step.Rollback); err != nil {
			return fmt.Errorf("step %d rollback command: %w", index, err)
		}
	}
	return nil
}

func validateCommand(command Command) error {
	if _, allowed := allowedCommands[command.Name]; !allowed {
		return fmt.Errorf("command %q is not allowed", command.Name)
	}
	for _, argument := range command.Args {
		if strings.ContainsRune(argument, '\x00') {
			return errors.New("command argument contains a null byte")
		}
	}
	return nil
}

func formatCommand(command Command) string {
	if len(command.Args) == 0 {
		return command.Name
	}
	return command.Name + " " + strings.Join(command.Args, " ")
}
