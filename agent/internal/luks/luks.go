package luks

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/IvanBez42/Portcullio/agent/internal/shellout"
)

// Checks if imagePath is attached to a loop device //
func FindLoopDevice(imagePath string) (loopPath string, ok bool, err error) {
	out, err := shellout.Run(nil, "losetup", "-j", imagePath, "-O", "NAME", "--noheadings")
	if err != nil {
		return "", false, fmt.Errorf("luks: find loop device for %s: %w", imagePath, err)
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return "", false, nil
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) > 1 {
		return "", false, fmt.Errorf("luks: %s is attached to multiple loop devices (%v); refusing to guess which one", imagePath, lines)
	}
	return strings.TrimSpace(lines[0]), true, nil
}

// Attaches imagePath to a loop device, reusing one if already attached //
func AttachLoop(imagePath string) (string, error) {
	if loopPath, ok, err := FindLoopDevice(imagePath); err != nil {
		return "", err
	} else if ok {
		return loopPath, nil
	}
	out, err := shellout.Run(nil, "losetup", "--find", "--show", imagePath)
	if err != nil {
		return "", fmt.Errorf("luks: attach loop for %s: %w", imagePath, err)
	}
	return strings.TrimSpace(out), nil
}

// Detaches loopPath from its loop device //
func DetachLoop(loopPath string) error {
	if _, err := shellout.Run(nil, "losetup", "--detach", loopPath); err != nil {
		return fmt.Errorf("luks: detach %s: %w", loopPath, err)
	}
	return nil
}

// Runs cryptsetup status and classifies the result //
func queryStatus(mapperName string) (out string, active bool, err error) {
	out, err = shellout.Run(nil, "cryptsetup", "status", mapperName)
	if err == nil {
		return out, true, nil
	}
	if strings.Contains(out, "is inactive") {
		return out, false, nil
	}
	return out, false, fmt.Errorf("luks: check status of %s: %w", mapperName, err)
}

// Checks if mapperName is an active dm-crypt mapping //
func IsMapped(mapperName string) (bool, error) {
	_, active, err := queryStatus(mapperName)
	return active, err
}

// Opens loopPath as mapperName via cryptsetup luksOpen //
func Open(loopPath, mapperName string, passphrase []byte) error {
	out, active, err := queryStatus(mapperName)
	if err != nil {
		return err
	}
	if active {
		mappedDevice, found := parseStatusDevice(out)
		if !found {
			return fmt.Errorf("luks: %s is mapped but its backing device could not be parsed from cryptsetup status output", mapperName)
		}
		if mappedDevice != loopPath {
			return fmt.Errorf("luks: mapper %s is already open against %s, not the requested %s", mapperName, mappedDevice, loopPath)
		}
		return nil
	}

	if _, err := shellout.Run(passphrase, "cryptsetup", "--key-file", "-", "luksOpen", loopPath, mapperName); err != nil {
		return fmt.Errorf("luks: open %s as %s: %w", loopPath, mapperName, err)
	}
	return nil
}

// Closes mapperName via cryptsetup luksClose //
func Close(mapperName string) error {
	mapped, err := IsMapped(mapperName)
	if err != nil {
		return err
	}
	if !mapped {
		return nil
	}
	if _, err := shellout.Run(nil, "cryptsetup", "luksClose", mapperName); err != nil {
		return fmt.Errorf("luks: close %s: %w", mapperName, err)
	}
	return nil
}

// Returns the /dev/mapper path for mapperName //
func MapperPath(mapperName string) string {
	return filepath.Join("/dev/mapper", mapperName)
}

// Extracts the device path from cryptsetup status output //
func parseStatusDevice(statusOutput string) (string, bool) {
	for _, line := range strings.Split(statusOutput, "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "device:"); ok {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}
