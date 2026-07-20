package shellout

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Returned when a command fails due to insufficient privilege //
var ErrNeedsPrivilege = errors.New("shellout: operation requires root or CAP_SYS_ADMIN")

// Bounds every command run through Run //
const DefaultTimeout = 15 * time.Second

// Runs name with args, feeding stdin, bounded by DefaultTimeout //
func Run(stdin []byte, name string, args ...string) (string, error) {
	return RunTimeout(DefaultTimeout, stdin, name, args...)
}

// Same as Run, with an explicit timeout //
func RunTimeout(timeout time.Duration, stdin []byte, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return out.String(), fmt.Errorf("shellout: %s %v timed out after %s (possible interactive prompt / missing --batch-mode)", name, args, timeout)
	}
	if err != nil {
		return out.String(), classifyError(name, args, out.String(), err)
	}
	return out.String(), nil
}

func classifyError(name string, args []string, output string, err error) error {
	lower := strings.ToLower(output)
	if strings.Contains(lower, "permission denied") || strings.Contains(lower, "operation not permitted") {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), ErrNeedsPrivilege, strings.TrimSpace(output))
	}
	return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(output))
}
