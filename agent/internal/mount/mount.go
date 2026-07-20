package mount

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/IvanBez42/Portcullio/agent/internal/shellout"
)

// Makes path an immutable directory (chattr +i), creating it first //
func EnsureImmutable(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("mount: create %s: %w", path, err)
	}
	if _, err := shellout.Run(nil, "chattr", "+i", path); err != nil {
		return fmt.Errorf("mount: chattr +i %s: %w", path, err)
	}
	return nil
}

// Checks if path has the immutable attribute set //
func IsImmutable(path string) (bool, error) {
	out, err := shellout.Run(nil, "lsattr", "-d", path)
	if err != nil {
		return false, fmt.Errorf("mount: lsattr -d %s: %w", path, err)
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return false, fmt.Errorf("mount: lsattr -d %s: unexpected empty output %q", path, out)
	}
	return strings.Contains(fields[0], "i"), nil
}

// Clears the immutable attribute and removes path entirely //
func RemoveImmutable(path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("mount: stat %s: %w", path, err)
	}
	if _, err := shellout.Run(nil, "chattr", "-i", path); err != nil {
		return fmt.Errorf("mount: chattr -i %s: %w", path, err)
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("mount: remove %s: %w", path, err)
	}
	return nil
}

// Mounts source at target with fstype //
func MountReal(source, fstype, target string) error {
	if _, err := shellout.Run(nil, "mount", "-t", fstype, source, target); err != nil {
		return fmt.Errorf("mount: mount %s (%s) at %s: %w", source, fstype, target, err)
	}
	return nil
}

// Unmounts path //
func Unmount(path string) error {
	if _, err := shellout.Run(nil, "umount", path); err != nil {
		return fmt.Errorf("mount: umount %s: %w", path, err)
	}
	return nil
}

// Checks what's mounted at path, if anything //
func MountedSource(path string) (source string, ok bool, err error) {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return "", false, fmt.Errorf("mount: read /proc/mounts: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if unescapeMountField(fields[1]) == path {
			return unescapeMountField(fields[0]), true, nil
		}
	}
	return "", false, nil
}

// Checks if anything is mounted at path //
func IsMounted(path string) (bool, error) {
	_, ok, err := MountedSource(path)
	return ok, err
}

// Reverses /proc/mounts' octal escaping //
func unescapeMountField(s string) string {
	replacer := strings.NewReplacer(
		`\040`, " ",
		`\011`, "\t",
		`\012`, "\n",
		`\134`, `\`,
	)
	return replacer.Replace(s)
}

// Reports bytes used on the filesystem mounted at path //
func DiskUsage(path string) (usedBytes int64, err error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("mount: statfs %s: %w", path, err)
	}
	return (int64(stat.Blocks) - int64(stat.Bfree)) * int64(stat.Bsize), nil
}

// Checks which processes hold path open //
func CheckHandles(path string) ([]string, error) {
	out, err := shellout.Run(nil, "fuser", "-m", path)
	if err == nil {
		return parseFuserHolders(out), nil
	}
	// fuser: nonzero means either nothing found or a real error //
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}
	return nil, fmt.Errorf("mount: check handles on %s: %w", path, err)
}

// Polls CheckHandles until clear or timeout //
func WaitForNoHandles(path string, timeout, pollInterval time.Duration) ([]string, error) {
	deadline := time.Now().Add(timeout)
	for {
		holders, err := CheckHandles(path)
		if err != nil {
			return nil, err
		}
		if len(holders) == 0 || time.Now().After(deadline) {
			return holders, nil
		}
		time.Sleep(pollInterval)
	}
}

// Extracts holder tokens from fuser -m output //
func parseFuserHolders(out string) []string {
	_, rest, found := strings.Cut(out, ":")
	if !found {
		return strings.Fields(out)
	}
	return strings.Fields(rest)
}
