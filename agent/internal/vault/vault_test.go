package vault_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/IvanBez42/Portcullio/agent/internal/luks"
	"github.com/IvanBez42/Portcullio/agent/internal/mount"
	"github.com/IvanBez42/Portcullio/agent/internal/provision"
	"github.com/IvanBez42/Portcullio/agent/internal/vault"
	"github.com/IvanBez42/Portcullio/agent/test/loopback"
)

const testPassphrase = "test-passphrase-only"

func requireBinaries(t *testing.T, bins ...string) {
	t.Helper()
	for _, bin := range bins {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("vault: required binary %q not found on PATH", bin)
		}
	}
}

// Test-only: clears chattr +i so cleanup can remove the dir //
func clearImmutable(t *testing.T, path string) {
	t.Helper()
	if _, err := exec.Command("chattr", "-i", path).CombinedOutput(); err != nil {
		t.Logf("chattr -i cleanup on %s: %v", path, err)
	}
}

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

// Creates a real, provisioned vault + fresh mount path //
func provisionTestVault(t *testing.T, mapperName string) vault.Config {
	t.Helper()
	loopback.RequireRoot(t)
	loopback.RequireBinaries(t)
	requireBinaries(t, "mount", "umount", "mkfs.ext4", "chattr")

	dir := t.TempDir()
	imagePath := filepath.Join(dir, "vault.img")

	t.Cleanup(func() {
		_ = luks.Close(mapperName)
		if loopPath, ok, _ := luks.FindLoopDevice(imagePath); ok {
			_ = luks.DetachLoop(loopPath)
		}
		os.Remove(imagePath)
	})

	requireNoErr(t, provision.CreateVault(provision.CreateVaultParams{
		ImagePath:  imagePath,
		SizeMB:     64,
		Fstype:     "ext4",
		MapperName: mapperName,
		Passphrase: []byte(testPassphrase),
	}), "CreateVault")

	mountPath := t.TempDir()
	t.Cleanup(func() { clearImmutable(t, mountPath) })
	t.Cleanup(func() {
		if err := mount.Unmount(mountPath); err != nil {
			t.Logf("Unmount cleanup: %v", err)
		}
	})

	return vault.Config{
		ImagePath:  imagePath,
		MapperName: mapperName,
		Fstype:     "ext4",
		MountPath:  mountPath,
	}
}

// Checks Unseal produces an Unsealed vault //
func TestUnsealThenStatusUnsealed(t *testing.T) {
	cfg := provisionTestVault(t, "portcullio-vault-unseal-1")
	v := vault.New(cfg)

	requireNoErr(t, v.Unseal([]byte(testPassphrase)), "Unseal")
	t.Cleanup(func() {
		if err := v.Seal(2*time.Second, 100*time.Millisecond); err != nil {
			t.Logf("Seal cleanup: %v", err)
		}
	})

	info, err := v.Status()
	requireNoErr(t, err, "Status")
	if info.State != vault.Unsealed {
		t.Fatalf("Status = %v, want Unsealed", info.State)
	}
}

// Checks Seal produces a Sealed vault, nothing mounted //
func TestSealThenStatusSealed(t *testing.T) {
	requireBinaries(t, "lsattr")
	cfg := provisionTestVault(t, "portcullio-vault-seal-1")
	v := vault.New(cfg)

	requireNoErr(t, v.Unseal([]byte(testPassphrase)), "Unseal")
	requireNoErr(t, v.Seal(2*time.Second, 100*time.Millisecond), "Seal")

	info, err := v.Status()
	requireNoErr(t, err, "Status")
	if info.State != vault.Sealed {
		t.Fatalf("Status = %v, want Sealed", info.State)
	}

	_, ok, err := mount.MountedSource(cfg.MountPath)
	requireNoErr(t, err, "MountedSource")
	if ok {
		t.Fatalf("MountedSource reports something still mounted at %s after Seal, want nothing mounted", cfg.MountPath)
	}
	immutable, err := mount.IsImmutable(cfg.MountPath)
	requireNoErr(t, err, "IsImmutable")
	if !immutable {
		t.Fatalf("IsImmutable = false after Seal, want true")
	}
}

// Checks Seal aborts on a held handle //
func TestSealAbortsOnHeldHandle(t *testing.T) {
	requireBinaries(t, "fuser")
	cfg := provisionTestVault(t, "portcullio-vault-seal-held")
	v := vault.New(cfg)

	requireNoErr(t, v.Unseal([]byte(testPassphrase)), "Unseal")

	f, err := os.Create(filepath.Join(cfg.MountPath, "held.txt"))
	requireNoErr(t, err, "create held file")
	t.Cleanup(func() {
		_ = f.Close()
		if err := v.Seal(2*time.Second, 100*time.Millisecond); err != nil {
			t.Logf("Seal cleanup: %v", err)
		}
	})

	requireErr(t, v.Seal(300*time.Millisecond, 50*time.Millisecond), "Seal succeeded while a file was held open, want refusal")

	info, err := v.Status()
	requireNoErr(t, err, "Status")
	if info.State != vault.Unsealed {
		t.Fatalf("Status after aborted Seal = %v, want still Unsealed", info.State)
	}
}

// Checks EnsureSealed bootstraps from nothing //
func TestEnsureSealedBootstrapsFromNothing(t *testing.T) {
	loopback.RequireRoot(t)
	requireBinaries(t, "umount", "chattr", "lsattr")

	mountPath := t.TempDir()
	t.Cleanup(func() { clearImmutable(t, mountPath) })
	t.Cleanup(func() {
		if err := mount.Unmount(mountPath); err != nil {
			t.Logf("Unmount cleanup: %v", err)
		}
	})

	cfg := vault.Config{
		ImagePath:    "unused",
		MapperName:   "unused-mapper",
		Fstype:       "ext4",
		MountPath:    mountPath,
	}
	v := vault.New(cfg)

	requireNoErr(t, v.EnsureSealed(), "EnsureSealed")
	_, ok, err := mount.MountedSource(mountPath)
	requireNoErr(t, err, "MountedSource")
	if ok {
		t.Fatalf("MountedSource reports something mounted at %s after EnsureSealed, want nothing mounted", mountPath)
	}
	immutable, err := mount.IsImmutable(mountPath)
	requireNoErr(t, err, "IsImmutable")
	if !immutable {
		t.Fatalf("IsImmutable = false after EnsureSealed, want true")
	}

	requireNoErr(t, v.EnsureSealed(), "second EnsureSealed call")
}

// Checks Status reports Unsealing during a transition //
func TestStatusReportsUnsealingDuringTransition(t *testing.T) {
	cfg := provisionTestVault(t, "portcullio-vault-unsealing-status")
	v := vault.New(cfg)

	done := make(chan struct{})
	sawUnsealing := make(chan bool, 1)
	go func() {
		seen := false
		for {
			select {
			case <-done:
				sawUnsealing <- seen
				return
			default:
				if info, err := v.Status(); err == nil && info.State == vault.Unsealing {
					seen = true
				}
			}
		}
	}()

	unsealErr := v.Unseal([]byte(testPassphrase))
	close(done)
	requireNoErr(t, unsealErr, "Unseal")
	t.Cleanup(func() {
		if err := v.Seal(2*time.Second, 100*time.Millisecond); err != nil {
			t.Logf("Seal cleanup: %v", err)
		}
	})

	if !<-sawUnsealing {
		t.Fatalf("Status never reported Unsealing while Unseal was running")
	}
}

// Checks Reconcile is a no-op when already Sealed //
func TestReconcileNoOpWhenSealed(t *testing.T) {
	loopback.RequireRoot(t)
	requireBinaries(t, "umount", "chattr")

	mountPath := t.TempDir()
	t.Cleanup(func() { clearImmutable(t, mountPath) })
	t.Cleanup(func() {
		if err := mount.Unmount(mountPath); err != nil {
			t.Logf("Unmount cleanup: %v", err)
		}
	})

	cfg := vault.Config{
		ImagePath:    "unused",
		MapperName:   "unused-mapper-reconcile-noop",
		Fstype:       "ext4",
		MountPath:    mountPath,
	}
	v := vault.New(cfg)
	requireNoErr(t, v.EnsureSealed(), "EnsureSealed")

	healed, info, err := v.Reconcile(2*time.Second, 100*time.Millisecond)
	requireNoErr(t, err, "Reconcile")
	if healed {
		t.Fatalf("Reconcile healed = true against an already-Sealed vault, want no-op")
	}
	if info.State != vault.Sealed {
		t.Fatalf("Reconcile info.State = %v, want Sealed", info.State)
	}
}

// Checks Reconcile heals a degraded vault //
func TestReconcileHealsDegradedVault(t *testing.T) {
	loopback.RequireRoot(t)
	loopback.RequireBinaries(t)
	requireBinaries(t, "umount", "chattr")

	dir := t.TempDir()
	dev, err := loopback.Create(dir, 64)
	requireNoErr(t, err, "loopback.Create")
	requireNoErr(t, dev.Attach(), "Attach")
	requireNoErr(t, dev.Format([]byte(testPassphrase)), "Format")
	imagePath := dev.BackingPath()
	const mapperName = "portcullio-vault-reconcile-heal"

	t.Cleanup(func() {
		_ = luks.Close(mapperName)
		if loopPath, ok, _ := luks.FindLoopDevice(imagePath); ok {
			_ = luks.DetachLoop(loopPath)
		}
	})

	requireNoErr(t, luks.Open(dev.LoopPath(), mapperName, []byte(testPassphrase)), "luks.Open")

	mountPath := t.TempDir()
	t.Cleanup(func() { clearImmutable(t, mountPath) })
	t.Cleanup(func() {
		if err := mount.Unmount(mountPath); err != nil {
			t.Logf("Unmount cleanup: %v", err)
		}
	})
	// Deliberately nothing mounted, simulating a crash //

	cfg := vault.Config{
		ImagePath:    imagePath,
		MapperName:   mapperName,
		Fstype:       "ext4",
		MountPath:    mountPath,
	}
	v := vault.New(cfg)

	before, err := v.Status()
	requireNoErr(t, err, "Status (before)")
	if before.State != vault.Degraded {
		t.Fatalf("Status (before) = %v, want Degraded (mapper open, nothing mounted at mount path)", before.State)
	}

	healed, info, err := v.Reconcile(2*time.Second, 100*time.Millisecond)
	requireNoErr(t, err, "Reconcile")
	if !healed {
		t.Fatalf("Reconcile healed = false, want true")
	}
	if info.State != vault.Sealed {
		t.Fatalf("Reconcile info.State = %v, want Sealed", info.State)
	}

	mapped, err := luks.IsMapped(mapperName)
	requireNoErr(t, err, "IsMapped")
	if mapped {
		t.Fatalf("IsMapped = true after Reconcile, want mapper closed")
	}
}

// Checks Reconcile reports failure on an unrecognized mount //
func TestReconcileReportsFailureOnUnrecognizedMount(t *testing.T) {
	loopback.RequireRoot(t)
	loopback.RequireBinaries(t)
	requireBinaries(t, "mount", "umount", "mkfs.ext4")

	// dev1: the vault under test //
	dir1 := t.TempDir()
	dev1, err := loopback.Create(dir1, 64)
	requireNoErr(t, err, "loopback.Create (dev1)")
	requireNoErr(t, dev1.Attach(), "Attach (dev1)")
	requireNoErr(t, dev1.Format([]byte(testPassphrase)), "Format (dev1)")
	imagePath := dev1.BackingPath()
	const mapperName = "portcullio-vault-reconcile-unrecognized"
	t.Cleanup(func() {
		_ = luks.Close(mapperName)
		if loopPath, ok, _ := luks.FindLoopDevice(imagePath); ok {
			_ = luks.DetachLoop(loopPath)
		}
	})
	requireNoErr(t, luks.Open(dev1.LoopPath(), mapperName, []byte(testPassphrase)), "luks.Open (dev1)")

	// dev2: an unrelated device standing in for something unrecognized //
	dir2 := t.TempDir()
	dev2, err := loopback.Create(dir2, 64)
	requireNoErr(t, err, "loopback.Create (dev2)")
	t.Cleanup(func() {
		if err := dev2.TeardownAll(); err != nil {
			t.Logf("dev2 teardown: %v", err)
		}
	})
	requireNoErr(t, dev2.Attach(), "Attach (dev2)")
	requireNoErr(t, dev2.Format([]byte(testPassphrase)), "Format (dev2)")
	requireNoErr(t, dev2.Open([]byte(testPassphrase)), "Open (dev2)")
	requireNoErr(t, loopback.Mkfs(dev2.MapperPath(), "ext4"), "Mkfs (dev2)")

	mountPath := t.TempDir()
	t.Cleanup(func() {
		if err := mount.Unmount(mountPath); err != nil {
			t.Logf("Unmount cleanup: %v", err)
		}
	})
	requireNoErr(t, mount.MountReal(dev2.MapperPath(), "ext4", mountPath), "MountReal (dev2 onto shared mountPath)")

	cfg := vault.Config{
		ImagePath:    imagePath,
		MapperName:   mapperName,
		Fstype:       "ext4",
		MountPath:    mountPath,
	}
	v := vault.New(cfg)

	before, err := v.Status()
	requireNoErr(t, err, "Status (before)")
	if before.State != vault.Degraded {
		t.Fatalf("Status (before) = %v, want Degraded (dev1 mapped, dev2's fs mounted at its path)", before.State)
	}

	healed, info, err := v.Reconcile(300*time.Millisecond, 50*time.Millisecond)
	requireErr(t, err, "Reconcile succeeded against an unrecognized mount, want refusal")
	if healed {
		t.Fatalf("Reconcile healed = true, want false")
	}
	if info.State != vault.Degraded {
		t.Fatalf("Reconcile info.State = %v, want still Degraded", info.State)
	}
}
