package loopback

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IvanBez42/Portcullio/agent/internal/shellout"
)

type deviceState int

const (
	stateCreated deviceState = iota
	stateAttached
	stateFormatted
	stateOpened
	stateClosed
	stateDetached
	stateDestroyed
)

// Disposable, file-backed LUKS2 loop device for tests //
type Device struct {
	backingPath string
	sizeMB      int
	loopPath    string // e.g. "/dev/loop7", set by Attach
	mapperName  string // e.g. "portcullio-test-3fa1c9", set by Open
	state       deviceState
}

// Allocates a sparse backing file of sizeMB //
func Create(dir string, sizeMB int) (*Device, error) {
	if sizeMB < 32 {
		return nil, fmt.Errorf("loopback: sizeMB=%d too small, need >= 32 for a LUKS2 header", sizeMB)
	}
	name, err := randomSuffix("backing")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name+".img")

	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("loopback: create backing file: %w", err)
	}
	defer f.Close()
	// Sparse file: sets apparent size without writing zeros //
	if err := f.Truncate(int64(sizeMB) * 1024 * 1024); err != nil {
		return nil, fmt.Errorf("loopback: truncate backing file: %w", err)
	}

	return &Device{backingPath: path, sizeMB: sizeMB, state: stateCreated}, nil
}

// Returns the backing file's path //
func (d *Device) BackingPath() string {
	return d.backingPath
}

// Returns the /dev/loopN path assigned by Attach //
func (d *Device) LoopPath() string {
	return d.loopPath
}

// Attaches the backing file to a free loop device //
func (d *Device) Attach() error {
	if d.state != stateCreated {
		return fmt.Errorf("loopback: Attach called in state %d, want stateCreated", d.state)
	}
	out, err := shellout.Run(nil, "losetup", "--find", "--show", d.backingPath)
	if err != nil {
		return err
	}
	d.loopPath = strings.TrimSpace(out)
	d.state = stateAttached
	return nil
}

// Runs cryptsetup luksFormat, with a cheap KDF for test speed //
func (d *Device) Format(passphrase []byte) error {
	if d.state != stateAttached {
		return fmt.Errorf("loopback: Format called in state %d, want stateAttached", d.state)
	}
	_, err := shellout.Run(passphrase,
		"cryptsetup", "--batch-mode", "--type", "luks2",
		"--pbkdf", "pbkdf2", "--pbkdf-force-iterations", "1000",
		"--key-file", "-", "luksFormat", d.loopPath)
	if err != nil {
		return err
	}
	d.state = stateFormatted
	return nil
}

// Runs cryptsetup luksOpen with a fresh mapper name //
func (d *Device) Open(passphrase []byte) error {
	if d.state != stateFormatted {
		return fmt.Errorf("loopback: Open called in state %d, want stateFormatted", d.state)
	}
	name, err := randomSuffix("portcullio-test")
	if err != nil {
		return err
	}
	if _, err := shellout.Run(passphrase, "cryptsetup", "--key-file", "-", "luksOpen", d.loopPath, name); err != nil {
		return err
	}
	d.mapperName = name
	d.state = stateOpened
	return nil
}

// Returns /dev/mapper/<name> once Open has run //
func (d *Device) MapperPath() string {
	if d.mapperName == "" {
		return ""
	}
	return filepath.Join("/dev/mapper", d.mapperName)
}

// Runs cryptsetup luksClose //
func (d *Device) Close() error {
	if d.state != stateOpened {
		return fmt.Errorf("loopback: Close called in state %d, want stateOpened", d.state)
	}
	if _, err := shellout.Run(nil, "cryptsetup", "luksClose", d.mapperName); err != nil {
		return err
	}
	d.state = stateClosed
	return nil
}

// Runs losetup --detach //
func (d *Device) Detach() error {
	if d.state != stateClosed && d.state != stateAttached && d.state != stateFormatted {
		return fmt.Errorf("loopback: Detach called in state %d, want stateClosed, stateAttached, or stateFormatted", d.state)
	}
	if _, err := shellout.Run(nil, "losetup", "--detach", d.loopPath); err != nil {
		return err
	}
	d.state = stateDetached
	return nil
}

// Removes the backing file //
func (d *Device) Destroy() error {
	if d.state != stateDetached && d.state != stateCreated {
		return fmt.Errorf("loopback: Destroy called in state %d, want stateDetached or stateCreated", d.state)
	}
	if err := os.Remove(d.backingPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("loopback: remove backing file: %w", err)
	}
	d.state = stateDestroyed
	return nil
}

// Best-effort Close+Detach+Destroy, in order //
func (d *Device) TeardownAll() error {
	var errs []error
	if d.state == stateOpened {
		if err := d.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if d.state == stateAttached || d.state == stateFormatted || d.state == stateClosed {
		if err := d.Detach(); err != nil {
			errs = append(errs, err)
		}
	}
	if d.state == stateDetached || d.state == stateCreated {
		if err := d.Destroy(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Skips the test if not running as root //
func RequireRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("loopback: requires root (CAP_SYS_ADMIN) to drive losetup/cryptsetup; re-run as root or in a privileged container")
	}
}

// Skips the test if cryptsetup/losetup isn't on PATH //
func RequireBinaries(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"cryptsetup", "losetup"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("loopback: required binary %q not found on PATH", bin)
		}
	}
}

// Formats mapperPath with fstype //
func Mkfs(mapperPath, fstype string) error {
	if _, err := shellout.Run(nil, "mkfs."+fstype, "-q", "-F", mapperPath); err != nil {
		return fmt.Errorf("loopback: mkfs.%s %s: %w", fstype, mapperPath, err)
	}
	return nil
}

func randomSuffix(prefix string) (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("loopback: generate random suffix: %w", err)
	}
	return prefix + "-" + hex.EncodeToString(b), nil
}

