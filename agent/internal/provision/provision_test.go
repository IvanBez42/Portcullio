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

// Fails the test immediately if err is non-nil //
func requireNoErr(t *testing.T, err error, label string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
}

// Fails the test immediately if err is nil //
func requireErr(t *testing.T, err error, msg string) {
	t.Helper()
	if err == nil {
		t.Fatal(msg)
	}
}

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
	requireNoErr(t, err, "CreateVault")

	if _, err := os.Stat(imagePath); err != nil {
		t.Fatalf("stat %s after CreateVault: %v", imagePath, err)
	}

	mapped, err := luks.IsMapped(mapperName)
	requireNoErr(t, err, "IsMapped after CreateVault")
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
	requireNoErr(t, err, "AttachLoop")
	requireNoErr(t, luks.Open(loopPath, mapperName, []byte(testPassphrase)), "Open")
	mapperPath := luks.MapperPath(mapperName)

	target := t.TempDir()
	requireNoErr(t, mount.MountReal(mapperPath, "ext4", target), "MountReal")
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
	requireNoErr(t, os.WriteFile(imagePath, []byte(original), 0o600), "seed existing file")

	err := provision.CreateVault(provision.CreateVaultParams{
		ImagePath:  imagePath,
		SizeMB:     64,
		Fstype:     "ext4",
		MapperName: "portcullio-provision-should-not-exist",
		Passphrase: []byte(testPassphrase),
	})
	requireErr(t, err, "CreateVault succeeded against an already-existing file, want refusal")

	got, err := os.ReadFile(imagePath)
	requireNoErr(t, err, "read back existing file")
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
	requireErr(t, err, "CreateVault succeeded with a SizeMB no real disk could satisfy, want refusal")

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
	requireErr(t, err, "CreateVault succeeded with a too-short passphrase, want refusal")

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
	requireNoErr(t, err, "loopback.Create")
	requireNoErr(t, dev.Attach(), "Attach")
	requireNoErr(t, dev.Format([]byte(testPassphrase)), "Format")
	imagePath := dev.BackingPath()
	const mapperName = "portcullio-provision-still-mapped"

	t.Cleanup(func() {
		_ = luks.Close(mapperName)
		if loopPath, ok, _ := luks.FindLoopDevice(imagePath); ok {
			_ = luks.DetachLoop(loopPath)
		}
		os.Remove(imagePath)
	})

	requireNoErr(t, luks.Open(dev.LoopPath(), mapperName, []byte(testPassphrase)), "luks.Open")

	mountPath := filepath.Join(t.TempDir(), "mount-stub")
	requireNoErr(t, mount.EnsureImmutable(mountPath), "EnsureImmutable")
	t.Cleanup(func() {
		if err := mount.RemoveImmutable(mountPath); err != nil {
			t.Logf("RemoveImmutable cleanup: %v", err)
		}
	})

	requireErr(t, provision.DestroyVault(imagePath, mapperName, mountPath), "DestroyVault succeeded while mapper was still open, want refusal")

	_, err = os.Stat(imagePath)
	requireNoErr(t, err, "backing file should still exist after refused destroy")
	mapped, err := luks.IsMapped(mapperName)
	requireNoErr(t, err, "IsMapped")
	if !mapped {
		t.Fatalf("mapper should still be open after a refused destroy")
	}
	_, err = os.Stat(mountPath)
	requireNoErr(t, err, "mount stub should still exist after refused destroy")
}

// Checks DestroyVault deletes a sealed vault //
func TestDestroyVaultDeletesWhenSealed(t *testing.T) {
	loopback.RequireRoot(t)
	loopback.RequireBinaries(t)
	requireBinaries(t, "mount", "umount", "mkfs.ext4")

	dir := t.TempDir()
	imagePath := filepath.Join(dir, "to-destroy.img")
	const mapperName = "portcullio-provision-selftest-2"

	requireNoErr(t, provision.CreateVault(provision.CreateVaultParams{
		ImagePath:  imagePath,
		SizeMB:     64,
		Fstype:     "ext4",
		MapperName: mapperName,
		Passphrase: []byte(testPassphrase),
	}), "CreateVault")

	mountPath := filepath.Join(t.TempDir(), "mount-stub")
	requireNoErr(t, mount.EnsureImmutable(mountPath), "EnsureImmutable")

	requireNoErr(t, provision.DestroyVault(imagePath, mapperName, mountPath), "DestroyVault")

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
	requireNoErr(t, err, "AvailableSpace")
	if avail <= 0 {
		t.Fatalf("AvailableSpace = %d, want > 0", avail)
	}
}
