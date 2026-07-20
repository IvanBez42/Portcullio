package vault

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/IvanBez42/Portcullio/agent/internal/luks"
	"github.com/IvanBez42/Portcullio/agent/internal/mount"
)

// Static, per-vault configuration //
type Config struct {
	ImagePath  string
	MapperName string
	Fstype     string
	MountPath  string // fixed bind path; an immutable bare dir while sealed, the real fs while unsealed
}

// State is a vault's classification at a point in time.
type State int

const (
	Sealed State = iota
	Unsealing
	Unsealed
	Degraded
)

func (s State) String() string {
	switch s {
	case Sealed:
		return "sealed"
	case Unsealing:
		return "unsealing"
	case Unsealed:
		return "unsealed"
	case Degraded:
		return "degraded"
	default:
		return fmt.Sprintf("vault.State(%d)", int(s))
	}
}

// Result of a Status check //
type Info struct {
	State  State
	Detail string
}

// Sequences luks/mount into unseal/seal transitions //
type Vault struct {
	cfg  Config
	mu   sync.Mutex
	busy atomic.Bool
}

// Returns a Vault for cfg //
func New(cfg Config) *Vault {
	return &Vault{cfg: cfg}
}

// Reports the vault's current classification //
func (v *Vault) Status() (Info, error) {
	if v.busy.Load() {
		return Info{State: Unsealing}, nil
	}

	mapped, err := luks.IsMapped(v.cfg.MapperName)
	if err != nil {
		return Info{}, err
	}
	source, mountedOK, err := mount.MountedSource(v.cfg.MountPath)
	if err != nil {
		return Info{}, err
	}
	mapperPath := luks.MapperPath(v.cfg.MapperName)

	switch {
	case !mapped && !mountedOK:
		return Info{State: Sealed}, nil
	case mapped && mountedOK && source == mapperPath:
		return Info{State: Unsealed}, nil
	default:
		detail := fmt.Sprintf("mapped=%v mountedOK=%v source=%q (want mapper %q or nothing mounted)",
			mapped, mountedOK, source, mapperPath)
		return Info{State: Degraded, Detail: detail}, nil
	}
}

// Idempotent bootstrap: makes the bare stub immutable if nothing's mounted //
func (v *Vault) EnsureSealed() error {
	_, mountedOK, err := mount.MountedSource(v.cfg.MountPath)
	if err != nil {
		return err
	}
	if mountedOK {
		return nil
	}
	return mount.EnsureImmutable(v.cfg.MountPath)
}

// Startup convergence for a Degraded vault //
func (v *Vault) Reconcile(handleTimeout, pollInterval time.Duration) (healed bool, info Info, err error) {
	info, err = v.Status()
	if err != nil {
		return false, Info{}, err
	}
	if info.State != Degraded {
		return false, info, nil
	}

	sealErr := v.Seal(handleTimeout, pollInterval)

	info, err = v.Status()
	if err != nil {
		return false, Info{}, err
	}
	if sealErr != nil {
		return false, info, sealErr
	}
	return true, info, nil
}

// Attaches, opens, and mounts the real filesystem //
func (v *Vault) Unseal(passphrase []byte) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.busy.Store(true)
	defer v.busy.Store(false)
	defer zeroBytes(passphrase)

	cfg := v.cfg

	loopPath, err := luks.AttachLoop(cfg.ImagePath)
	if err != nil {
		return fmt.Errorf("vault: unseal %s: %w", cfg.ImagePath, err)
	}

	openErr := luks.Open(loopPath, cfg.MapperName, passphrase)
	zeroBytes(passphrase) // last real use of the passphrase was the Open call above
	if openErr != nil {
		_ = luks.DetachLoop(loopPath)
		return fmt.Errorf("vault: unseal %s: %w", cfg.ImagePath, openErr)
	}
	mapperPath := luks.MapperPath(cfg.MapperName)

	source, mountedOK, err := mount.MountedSource(cfg.MountPath)
	if err != nil {
		return v.rollbackToSealed(loopPath, fmt.Errorf("vault: unseal %s: %w", cfg.ImagePath, err))
	}

	if !(mountedOK && source == mapperPath) {
		if mountedOK {
			holders, err := mount.CheckHandles(cfg.MountPath)
			if err != nil {
				return v.rollbackToSealed(loopPath, fmt.Errorf("vault: unseal %s: %w", cfg.ImagePath, err))
			}
			if len(holders) > 0 {
				return v.rollbackToSealed(loopPath, fmt.Errorf(
					"vault: refusing to unseal %s: %s still held by %v", cfg.ImagePath, cfg.MountPath, holders))
			}
			if err := mount.Unmount(cfg.MountPath); err != nil {
				return v.rollbackToSealed(loopPath, fmt.Errorf("vault: unseal %s: %w", cfg.ImagePath, err))
			}
		}
		if err := mount.MountReal(mapperPath, cfg.Fstype, cfg.MountPath); err != nil {
			return v.rollbackToSealed(loopPath, fmt.Errorf("vault: unseal %s: %w", cfg.ImagePath, err))
		}
	}

	return nil
}

// Unwinds a failed Unseal back to Sealed //
func (v *Vault) rollbackToSealed(loopPath string, cause error) error {
	cfg := v.cfg
	var rollbackErrs []error

	if _, mountedOK, err := mount.MountedSource(cfg.MountPath); err != nil {
		rollbackErrs = append(rollbackErrs, err)
	} else if mountedOK {
		if err := mount.Unmount(cfg.MountPath); err != nil {
			rollbackErrs = append(rollbackErrs, err)
		}
	}
	if _, stillMounted, err := mount.MountedSource(cfg.MountPath); err != nil {
		rollbackErrs = append(rollbackErrs, err)
	} else if !stillMounted {
		if err := mount.EnsureImmutable(cfg.MountPath); err != nil {
			rollbackErrs = append(rollbackErrs, err)
		}
	}
	if err := luks.Close(cfg.MapperName); err != nil {
		rollbackErrs = append(rollbackErrs, err)
	}
	if err := luks.DetachLoop(loopPath); err != nil {
		rollbackErrs = append(rollbackErrs, err)
	}

	if len(rollbackErrs) == 0 {
		return cause
	}
	return fmt.Errorf("%w (additionally, rollback hit errors: %v)", cause, rollbackErrs)
}

// Unmounts, closes the mapper, and detaches the loop device //
func (v *Vault) Seal(timeout, pollInterval time.Duration) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.busy.Store(true)
	defer v.busy.Store(false)

	cfg := v.cfg
	mapperPath := luks.MapperPath(cfg.MapperName)

	source, mountedOK, err := mount.MountedSource(cfg.MountPath)
	if err != nil {
		return fmt.Errorf("vault: seal %s: %w", cfg.ImagePath, err)
	}

	if mountedOK {
		if source != mapperPath {
			return fmt.Errorf("vault: seal %s: refusing to seal, unexpected mount source %q at %s", cfg.ImagePath, source, cfg.MountPath)
		}
		holders, err := mount.WaitForNoHandles(cfg.MountPath, timeout, pollInterval)
		if err != nil {
			return fmt.Errorf("vault: seal %s: %w", cfg.ImagePath, err)
		}
		if len(holders) > 0 {
			return fmt.Errorf("vault: refusing to seal %s: %s still held by %v", cfg.ImagePath, cfg.MountPath, holders)
		}
		if err := mount.Unmount(cfg.MountPath); err != nil {
			return fmt.Errorf("vault: seal %s: %w", cfg.ImagePath, err)
		}
	}

	// Defensive convergence check, not expected to do real work //
	if err := mount.EnsureImmutable(cfg.MountPath); err != nil {
		return fmt.Errorf("vault: seal %s: %w", cfg.ImagePath, err)
	}

	if err := luks.Close(cfg.MapperName); err != nil {
		return fmt.Errorf("vault: seal %s: %w", cfg.ImagePath, err)
	}

	if loopPath, ok, err := luks.FindLoopDevice(cfg.ImagePath); err != nil {
		return fmt.Errorf("vault: seal %s: %w", cfg.ImagePath, err)
	} else if ok {
		if err := luks.DetachLoop(loopPath); err != nil {
			return fmt.Errorf("vault: seal %s: %w", cfg.ImagePath, err)
		}
	}

	return nil
}

// Overwrites b in place //
func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
