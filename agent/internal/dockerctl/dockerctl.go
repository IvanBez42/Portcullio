package dockerctl

import (
	"fmt"
	"strings"
	"time"

	"github.com/IvanBez42/Portcullio/agent/internal/shellout"
)

// Required for the agent to avoid trying to link to itself or the UI //
var selfContainerPrefixes = []string{"portcullio-agent-", "portcullio-ui-"}

func isSelf(name string) bool {
	for _, prefix := range selfContainerPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// Provides all containers except the agent and UI //
func LinkableContainers() ([]string, error) {
	out, err := shellout.Run(nil, "docker", "ps", "-a", "--format", "{{.Names}}")
	if err != nil {
		return nil, fmt.Errorf("dockerctl: list containers: %w", err)
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return nil, nil
	}
	all := strings.Split(trimmed, "\n")
	names := make([]string, 0, len(all))
	for _, name := range all {
		if !isSelf(name) {
			names = append(names, name)
		}
	}
	return names, nil
}

// Ensure container exists //
func validate(names []string) error {
	known, err := LinkableContainers()
	if err != nil {
		return err
	}
	knownSet := make(map[string]bool, len(known))
	for _, s := range known {
		knownSet[s] = true
	}
	for _, s := range names {
		if !knownSet[s] {
			return fmt.Errorf("dockerctl: %q is not a linkable container", s)
		}
	}
	return nil
}

const startTimeout = 5 * time.Minute

// Docker Start //
func Start(names []string) error {
	if len(names) == 0 {
		return nil
	}
	if err := validate(names); err != nil {
		return err
	}
	args := append([]string{"start"}, names...)
	if _, err := shellout.RunTimeout(startTimeout, nil, "docker", args...); err != nil {
		return fmt.Errorf("dockerctl: start %v: %w", names, err)
	}
	return nil
}

// Docker Stop //
func Stop(names []string) error {
	if len(names) == 0 {
		return nil
	}
	if err := validate(names); err != nil {
		return err
	}
	args := append([]string{"stop"}, names...)
	if _, err := shellout.Run(nil, "docker", args...); err != nil {
		return fmt.Errorf("dockerctl: stop %v: %w", names, err)
	}
	return nil
}
