package provision_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/IvanBez42/Portcullio/agent/internal/luks"
	"github.com/IvanBez42/Portcullio/agent/internal/mount"
	"github.com/IvanBez42/Portcullio/agent/internal/provision"
	"github.com/IvanBez42/Portcullio/agent/test/loopback"
)

const testPassphrase = "test-passphrase-only"

func requireBinaries(t *testing.T, bins ...string) {
	t.Helper()
	for _, bin := range bins {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("provision: required binary %q not found on PATH", bin)
		}
	}
}

// Checks a created vault survives a normal unseal cycle //
func TestCreateVaultThenNormalUnsealCycle(t *testing.T) {
	loopback.RequireRoot(t)
	loopback.RequireBinaries(t)
	requireBinaries(t, "mount", "umount", "mkfs.ext4")

	dir := t.TempDir()
	imagePath := filepath.Join(dir, "vault.img")
	const mapperName = "portcullio-provision-selftest-1"

	t.Cleanup(func() {
		_ = luks.Close(mapperName)
		if loopPath, ok, _ := luks.FindLoopDevice(imagePath); ok {
			_ = luks.DetachLoop(loopPath)
		}
		os.Remove(imagePath)
	})

	err := provision.CreateVault(provision.CreateVaultParams{
		ImagePath:  imagePath,
		SizeMB:     64,
		Fstype:     "ext4",
		MapperName: mapperName,
		Passphrase: []byte(testPassphrase),
	})
	if err != nil {
		t.Fatalf("CreateVault: %v", err)
	}

	if _, err := os.Stat(imagePath); err != nil {
		t.Fatalf("stat %s after CreateVault: %v", imagePath, err)
	}

	mapped, err := luks.IsMapped(mapperName)
	if err != nil {
		t.Fatalf("IsMapped after CreateVault: %v", err)
	}
	if mapped {
		t.Fatalf("IsMapped = true right after CreateVault, want fully torn down")
	}
	if _, ok, err := luks.FindLoopDevice(imagePath); err != nil {
		t.Fatalf("FindLoopDevice after CreateVault: %v", err)
	} else if ok {
		t.Fatalf("FindLoopDevice = attached right after CreateVault, want fully detached")
	}

	// Simulate a normal unseal cycle against the freshly created vault.
	loopPath, err := luks.AttachLoop(imagePath)
	if err != nil {
		t.Fatalf("AttachLoop: %v", err)
	}
	if err := luks.Open(loopPath, mapperName, []byte(testPassphrase)); err != nil {
		t.Fatalf("Open: %v", err)
	}
	mapperPath := luks.MapperPath(mapperName)

	target := t.TempDir()
	if err := mount.MountReal(mapperPath, "ext4", target); err != nil {
		t.Fatalf("MountReal: %v", err)
	}
	t.Cleanup(func() {
		if err := mount.Unmount(target); err != nil {
			t.Logf("Unmount cleanup: %v", err)
		}
	})
}

// Checks CreateVault refuses an existing image path //
func TestCreateVaultRefusesIfImageAlreadyExists(t *testing.T) {
	loopback.RequireRoot(t)
	loopback.RequireBinaries(t)

	dir := t.TempDir()
	imagePath := filepath.Join(dir, "already-exists.img")
	const original = "pretend existing vault data"
	if err := os.WriteFile(imagePath, []byte(original), 0o600); err != nil {
		t.Fatalf("seed existing file: %v", err)
	}

	err := provision.CreateVault(provision.CreateVaultParams{
		ImagePath:  imagePath,
		SizeMB:     64,
		Fstype:     "ext4",
		MapperName: "portcullio-provision-should-not-exist",
		Passphrase: []byte(testPassphrase),
	})
	if err == nil {
		t.Fatalf("CreateVault succeeded against an already-existing file, want refusal")
	}

	got, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatalf("read back existing file: %v", err)
	}
	if string(got) != original {
		t.Fatalf("existing file content changed: got %q, want %q", got, original)
	}
}

// Checks CreateVault refuses an oversized SizeMB //
func TestCreateVaultRefusesIfSizeExceedsAvailableSpace(t *testing.T) {
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "too-big.img")

	err := provision.CreateVault(provision.CreateVaultParams{
		ImagePath:  imagePath,
		SizeMB:     999999999, // ~954 PiB -- no real test machine has this much free
		Fstype:     "ext4",
		MapperName: "portcullio-provision-too-big",
		Passphrase: []byte(testPassphrase),
	})
	if err == nil {
		t.Fatalf("CreateVault succeeded with a SizeMB no real disk could satisfy, want refusal")
	}

	if _, statErr := os.Stat(imagePath); !os.IsNotExist(statErr) {
		t.Fatalf("backing file should not exist after a refused create, stat err = %v", statErr)
	}
}

// Checks CreateVault refuses a too-short passphrase //
func TestCreateVaultRefusesIfPassphraseTooShort(t *testing.T) {
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "short-passphrase.img")

	err := provision.CreateVault(provision.CreateVaultParams{
		ImagePath:  imagePath,
		SizeMB:     64,
		Fstype:     "ext4",
		MapperName: "portcullio-provision-short-passphrase",
		Passphrase: []byte("short"),
	})
	if err == nil {
		t.Fatalf("CreateVault succeeded with a too-short passphrase, want refusal")
	}

	if _, statErr := os.Stat(imagePath); !os.IsNotExist(statErr) {
		t.Fatalf("backing file should not exist after a refused create, stat err = %v", statErr)
	}
}

// Checks DestroyVault refuses a still-open mapper //
func TestDestroyVaultRefusesIfStillMapped(t *testing.T) {
	loopback.RequireRoot(t)
	loopback.RequireBinaries(t)

	dir := t.TempDir()
	dev, err := loopback.Create(dir, 64)
	if err != nil {
		t.Fatalf("loopback.Create: %v", err)
	}
	if err := dev.Attach(); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := dev.Format([]byte(testPassphrase)); err != nil {
		t.Fatalf("Format: %v", err)
	}
	imagePath := dev.BackingPath()
	const mapperName = "portcullio-provision-still-mapped"

	t.Cleanup(func() {
		_ = luks.Close(mapperName)
		if loopPath, ok, _ := luks.FindLoopDevice(imagePath); ok {
			_ = luks.DetachLoop(loopPath)
		}
		os.Remove(imagePath)
	})

	if err := luks.Open(dev.LoopPath(), mapperName, []byte(testPassphrase)); err != nil {
		t.Fatalf("luks.Open: %v", err)
	}

	mountPath := filepath.Join(t.TempDir(), "mount-stub")
	if err := mount.EnsureImmutable(mountPath); err != nil {
		t.Fatalf("EnsureImmutable: %v", err)
	}
	t.Cleanup(func() {
		if err := mount.RemoveImmutable(mountPath); err != nil {
			t.Logf("RemoveImmutable cleanup: %v", err)
		}
	})

	if err := provision.DestroyVault(imagePath, mapperName, mountPath); err == nil {
		t.Fatalf("DestroyVault succeeded while mapper was still open, want refusal")
	}

	if _, err := os.Stat(imagePath); err != nil {
		t.Fatalf("backing file should still exist after refused destroy: %v", err)
	}
	mapped, err := luks.IsMapped(mapperName)
	if err != nil {
		t.Fatalf("IsMapped: %v", err)
	}
	if !mapped {
		t.Fatalf("mapper should still be open after a refused destroy")
	}
	if _, err := os.Stat(mountPath); err != nil {
		t.Fatalf("mount stub should still exist after refused destroy: %v", err)
	}
}

// Checks DestroyVault deletes a sealed vault //
func TestDestroyVaultDeletesWhenSealed(t *testing.T) {
	loopback.RequireRoot(t)
	loopback.RequireBinaries(t)
	requireBinaries(t, "mount", "umount", "mkfs.ext4")

	dir := t.TempDir()
	imagePath := filepath.Join(dir, "to-destroy.img")
	const mapperName = "portcullio-provision-selftest-2"

	if err := provision.CreateVault(provision.CreateVaultParams{
		ImagePath:  imagePath,
		SizeMB:     64,
		Fstype:     "ext4",
		MapperName: mapperName,
		Passphrase: []byte(testPassphrase),
	}); err != nil {
		t.Fatalf("CreateVault: %v", err)
	}

	mountPath := filepath.Join(t.TempDir(), "mount-stub")
	if err := mount.EnsureImmutable(mountPath); err != nil {
		t.Fatalf("EnsureImmutable: %v", err)
	}

	if err := provision.DestroyVault(imagePath, mapperName, mountPath); err != nil {
		t.Fatalf("DestroyVault: %v", err)
	}

	if _, err := os.Stat(imagePath); !os.IsNotExist(err) {
		t.Fatalf("backing file should be gone after DestroyVault, stat err = %v", err)
	}
	if _, err := os.Stat(mountPath); !os.IsNotExist(err) {
		t.Fatalf("mount stub should be gone after DestroyVault (immutable dir must be cleared, not just left behind), stat err = %v", err)
	}
}

// Checks AvailableSpace reports a positive value //
func TestAvailableSpaceReportsPositiveValue(t *testing.T) {
	avail, err := provision.AvailableSpace(t.TempDir())
	if err != nil {
		t.Fatalf("AvailableSpace: %v", err)
	}
	if avail <= 0 {
		t.Fatalf("AvailableSpace = %d, want > 0", avail)
	}
}
