package provision

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/IvanBez42/Portcullio/agent/internal/luks"
	"github.com/IvanBez42/Portcullio/agent/internal/mount"
	"github.com/IvanBez42/Portcullio/agent/internal/shellout"
)

// Inputs to CreateVault //
type CreateVaultParams struct {
	ImagePath  string // backing file to create; must not already exist
	SizeMB     int    // >= 32, per internal/luks's LUKS2 header headroom rule
	Fstype     string // e.g. "ext4"
	MapperName string
	Passphrase []byte // zeroed by CreateVault before it returns
}

// Provisions a brand-new sealed vault //
func CreateVault(p CreateVaultParams) error {
	defer zeroBytes(p.Passphrase)

	if p.SizeMB < 32 {
		return fmt.Errorf("provision: SizeMB=%d too small, need >= 32 for a LUKS2 header", p.SizeMB)
	}

	if len(p.Passphrase) < 8 {
		return fmt.Errorf("provision: passphrase too short, need >= 8 characters")
	}

	avail, err := AvailableSpace(filepath.Dir(p.ImagePath))
	if err != nil {
		return err
	}
	if int64(p.SizeMB)*1024*1024 > avail {
		return fmt.Errorf("provision: SizeMB=%d exceeds available space (%d MB free)", p.SizeMB, avail/(1024*1024))
	}

	if err := createSparseFile(p.ImagePath, p.SizeMB); err != nil {
		return err
	}

	loopPath, err := luks.AttachLoop(p.ImagePath)
	if err != nil {
		os.Remove(p.ImagePath)
		return err
	}

	if err := format(loopPath, p.Passphrase); err != nil {
		_ = luks.DetachLoop(loopPath)
		os.Remove(p.ImagePath)
		return err
	}

	openErr := luks.Open(loopPath, p.MapperName, p.Passphrase)
	zeroBytes(p.Passphrase) // Zeros the passphrase immediately after use //
	if openErr != nil {
		_ = luks.DetachLoop(loopPath)
		os.Remove(p.ImagePath)
		return openErr
	}
	mapperPath := luks.MapperPath(p.MapperName)

	if err := mkfs(mapperPath, p.Fstype); err != nil {
		_ = luks.Close(p.MapperName)
		_ = luks.DetachLoop(loopPath)
		os.Remove(p.ImagePath)
		return err
	}

	if err := luks.Close(p.MapperName); err != nil {
		_ = luks.DetachLoop(loopPath)
		os.Remove(p.ImagePath)
		return err
	}

	if err := luks.DetachLoop(loopPath); err != nil {
		os.Remove(p.ImagePath)
		return err
	}

	return nil
}

// Permanently deletes a vault's backing file and mount stub //
func DestroyVault(imagePath, mapperName, mountPath string) error {
	mapped, err := luks.IsMapped(mapperName)
	if err != nil {
		return err
	}
	if mapped {
		return fmt.Errorf("provision: refusing to destroy %s: mapper %s is still open; seal it first", imagePath, mapperName)
	}
	if mounted, err := mount.IsMounted(mountPath); err != nil {
		return err
	} else if mounted {
		return fmt.Errorf("provision: refusing to destroy %s: %s is still mounted; seal it first", imagePath, mountPath)
	}

	loopPath, attached, err := luks.FindLoopDevice(imagePath)
	if err != nil {
		return err
	}
	if attached {
		if err := luks.DetachLoop(loopPath); err != nil {
			return err
		}
	}

	if err := os.Remove(imagePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("provision: remove %s: %w", imagePath, err)
	}

	if err := mount.RemoveImmutable(mountPath); err != nil {
		return fmt.Errorf("provision: remove mount stub %s: %w", mountPath, err)
	}
	return nil
}

// Reports free bytes on the filesystem backing dir //
func AvailableSpace(dir string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return 0, fmt.Errorf("provision: statfs %s: %w", dir, err)
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

// Allocates a new backing file //
func createSparseFile(path string, sizeMB int) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("provision: refusing to create vault: %s already exists", path)
		}
		return fmt.Errorf("provision: create backing file: %w", err)
	}
	defer f.Close()
	if err := f.Truncate(int64(sizeMB) * 1024 * 1024); err != nil {
		return fmt.Errorf("provision: truncate backing file: %w", err)
	}
	return nil
}

// Caps RAM Usage for LUKS create //
const argon2idMemoryKiB = 262144 // 256 MiB

// Runs cryptsetup luksFormat //
func format(loopPath string, passphrase []byte) error {
	if _, err := shellout.Run(passphrase,
		"cryptsetup", "--batch-mode", "--type", "luks2",
		"--pbkdf", "argon2id",
		"--pbkdf-memory", fmt.Sprint(argon2idMemoryKiB),
		"--key-file", "-", "luksFormat", loopPath); err != nil {
		return fmt.Errorf("provision: luksFormat %s: %w", loopPath, err)
	}
	return nil
}

// Formats mapperPath with fstype //
func mkfs(mapperPath, fstype string) error {
	if _, err := shellout.Run(nil, "mkfs."+fstype, "-q", "-F", mapperPath); err != nil {
		return fmt.Errorf("provision: mkfs.%s %s: %w", fstype, mapperPath, err)
	}
	return nil
}

// Overwrites b in place //
func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
