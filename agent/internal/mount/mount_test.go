package mount_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/IvanBez42/Portcullio/agent/internal/luks"
	"github.com/IvanBez42/Portcullio/agent/internal/mount"
	"github.com/IvanBez42/Portcullio/agent/internal/shellout"
	"github.com/IvanBez42/Portcullio/agent/test/loopback"
)

const testPassphrase = "test-passphrase-only"

// Mounts a size-bounded tmpfs at path -- test-only, no production caller //
func mountTmpfs(path string, sizeMB int) error {
	opt := fmt.Sprintf("size=%dm", sizeMB)
	if _, err := shellout.Run(nil, "mount", "-t", "tmpfs", "-o", opt, "tmpfs", path); err != nil {
		return fmt.Errorf("mount_test: mount tmpfs stub at %s: %w", path, err)
	}
	return nil
}

// Skips the test if any binary isn't on PATH //
func requireBinaries(t *testing.T, bins ...string) {
	t.Helper()
	for _, bin := range bins {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("mount: required binary %q not found on PATH", bin)
		}
	}
}

// Checks MountReal/MountedSource/IsMounted on a real filesystem //
func TestMountReal(t *testing.T) {
	loopback.RequireRoot(t)
	loopback.RequireBinaries(t)
	requireBinaries(t, "mount", "umount", "mkfs.ext4")

	dir := t.TempDir()
	dev, err := loopback.Create(dir, 64)
	if err != nil {
		t.Fatalf("loopback.Create: %v", err)
	}
	t.Cleanup(func() {
		if err := dev.TeardownAll(); err != nil {
			t.Logf("teardown: %v", err)
		}
	})
	if err := dev.Attach(); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := dev.Format([]byte(testPassphrase)); err != nil {
		t.Fatalf("Format: %v", err)
	}

	const mapperName = "portcullio-mount-selftest"
	t.Cleanup(func() {
		if err := luks.Close(mapperName); err != nil {
			t.Logf("luks.Close cleanup: %v", err)
		}
	})
	if err := luks.Open(dev.LoopPath(), mapperName, []byte(testPassphrase)); err != nil {
		t.Fatalf("luks.Open: %v", err)
	}
	mapperPath := luks.MapperPath(mapperName)

	if err := loopback.Mkfs(mapperPath, "ext4"); err != nil {
		t.Fatalf("loopback.Mkfs: %v", err)
	}

	target := t.TempDir()
	t.Cleanup(func() {
		if err := mount.Unmount(target); err != nil {
			t.Logf("Unmount cleanup: %v", err)
		}
	})
	if err := mount.MountReal(mapperPath, "ext4", target); err != nil {
		t.Fatalf("MountReal: %v", err)
	}

	mounted, err := mount.IsMounted(target)
	if err != nil {
		t.Fatalf("IsMounted: %v", err)
	}
	if !mounted {
		t.Fatalf("IsMounted = false right after MountReal")
	}
	source, ok, err := mount.MountedSource(target)
	if err != nil {
		t.Fatalf("MountedSource: %v", err)
	}
	if !ok || source != mapperPath {
		t.Fatalf("MountedSource = (%q, %v), want (%q, true)", source, ok, mapperPath)
	}

	if err := mount.Unmount(target); err != nil {
		t.Fatalf("Unmount: %v", err)
	}
	mounted, err = mount.IsMounted(target)
	if err != nil {
		t.Fatalf("IsMounted after Unmount: %v", err)
	}
	if mounted {
		t.Fatalf("IsMounted = true after Unmount")
	}
}

// Checks MountTmpfs is genuinely ephemeral //
func TestTmpfsStubSwap(t *testing.T) {
	requireBinaries(t, "mount", "umount")
	loopback.RequireRoot(t)

	stubDir := t.TempDir()
	if err := mountTmpfs(stubDir, 16); err != nil {
		t.Fatalf("MountTmpfs: %v", err)
	}
	unmounted := false
	t.Cleanup(func() {
		if unmounted {
			return
		}
		if err := mount.Unmount(stubDir); err != nil {
			t.Logf("Unmount cleanup: %v", err)
		}
	})

	source, ok, err := mount.MountedSource(stubDir)
	if err != nil {
		t.Fatalf("MountedSource: %v", err)
	}
	if !ok || source != "tmpfs" {
		t.Fatalf("MountedSource = (%q, %v), want (\"tmpfs\", true)", source, ok)
	}

	canary := filepath.Join(stubDir, "canary.txt")
	if err := os.WriteFile(canary, []byte("plaintext that must never reach real disk"), 0o644); err != nil {
		t.Fatalf("write canary: %v", err)
	}
	if _, err := os.Stat(canary); err != nil {
		t.Fatalf("canary should exist while tmpfs is mounted: %v", err)
	}

	if err := mount.Unmount(stubDir); err != nil {
		t.Fatalf("Unmount: %v", err)
	}
	unmounted = true

	if _, err := os.Stat(canary); !os.IsNotExist(err) {
		t.Fatalf("canary should be gone after unmounting tmpfs, stat err = %v", err)
	}
}

// Checks CheckHandles detects and clears open handles //
func TestCheckHandles(t *testing.T) {
	requireBinaries(t, "mount", "umount", "fuser")
	loopback.RequireRoot(t)

	stubDir := t.TempDir()
	if err := mountTmpfs(stubDir, 16); err != nil {
		t.Fatalf("MountTmpfs: %v", err)
	}
	t.Cleanup(func() {
		if err := mount.Unmount(stubDir); err != nil {
			t.Logf("Unmount cleanup: %v", err)
		}
	})

	f, err := os.Create(filepath.Join(stubDir, "held.txt"))
	if err != nil {
		t.Fatalf("create held file: %v", err)
	}

	holders, err := mount.CheckHandles(stubDir)
	if err != nil {
		t.Fatalf("CheckHandles (file open): %v", err)
	}
	if len(holders) == 0 {
		t.Fatalf("CheckHandles found no holders while a file is open under %s", stubDir)
	}

	if err := f.Close(); err != nil {
		t.Fatalf("close held file: %v", err)
	}

	holders, err = mount.CheckHandles(stubDir)
	if err != nil {
		t.Fatalf("CheckHandles (file closed): %v", err)
	}
	if len(holders) != 0 {
		t.Fatalf("CheckHandles still reports holders after close: %v", holders)
	}
}

// Checks WaitForNoHandles returns once a handle is released //
func TestWaitForNoHandles(t *testing.T) {
	requireBinaries(t, "mount", "umount", "fuser")
	loopback.RequireRoot(t)

	stubDir := t.TempDir()
	if err := mountTmpfs(stubDir, 16); err != nil {
		t.Fatalf("MountTmpfs: %v", err)
	}
	t.Cleanup(func() {
		if err := mount.Unmount(stubDir); err != nil {
			t.Logf("Unmount cleanup: %v", err)
		}
	})

	f, err := os.Create(filepath.Join(stubDir, "held.txt"))
	if err != nil {
		t.Fatalf("create held file: %v", err)
	}
	go func() {
		time.Sleep(150 * time.Millisecond)
		f.Close()
	}()

	holders, err := mount.WaitForNoHandles(stubDir, 2*time.Second, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForNoHandles: %v", err)
	}
	if len(holders) != 0 {
		t.Fatalf("WaitForNoHandles returned holders after the handle was released: %v", holders)
	}
}

// Test-only: clears chattr +i so cleanup can remove the dir //
func clearImmutable(t *testing.T, path string) {
	t.Helper()
	if _, err := exec.Command("chattr", "-i", path).CombinedOutput(); err != nil {
		t.Logf("chattr -i cleanup on %s: %v", path, err)
	}
}

// Checks EnsureImmutable blocks writes //
func TestEnsureImmutableBlocksWrites(t *testing.T) {
	requireBinaries(t, "chattr")
	loopback.RequireRoot(t)

	dir := filepath.Join(t.TempDir(), "sealed-stub")
	if err := mount.EnsureImmutable(dir); err != nil {
		t.Fatalf("EnsureImmutable: %v", err)
	}
	t.Cleanup(func() { clearImmutable(t, dir) })

	if err := os.WriteFile(filepath.Join(dir, "canary.txt"), []byte("must never land here"), 0o644); err == nil {
		t.Fatalf("write succeeded inside an immutable directory, want a permission error")
	}
}

// Checks EnsureImmutable is idempotent //
func TestEnsureImmutableIdempotent(t *testing.T) {
	requireBinaries(t, "chattr")
	loopback.RequireRoot(t)

	dir := t.TempDir()
	if err := mount.EnsureImmutable(dir); err != nil {
		t.Fatalf("first EnsureImmutable: %v", err)
	}
	t.Cleanup(func() { clearImmutable(t, dir) })

	if err := mount.EnsureImmutable(dir); err != nil {
		t.Fatalf("second EnsureImmutable: %v", err)
	}
}

// Checks the immutable flag survives a mount/umount cycle //
func TestEnsureImmutableSurvivesMountCycle(t *testing.T) {
	requireBinaries(t, "chattr", "mount", "umount")
	loopback.RequireRoot(t)

	dir := t.TempDir()
	if err := mount.EnsureImmutable(dir); err != nil {
		t.Fatalf("EnsureImmutable: %v", err)
	}
	t.Cleanup(func() { clearImmutable(t, dir) })

	if err := os.WriteFile(filepath.Join(dir, "before.txt"), []byte("x"), 0o644); err == nil {
		t.Fatalf("write succeeded before mounting anything, want a permission error")
	}

	if err := mountTmpfs(dir, 16); err != nil {
		t.Fatalf("MountTmpfs: %v", err)
	}
	unmounted := false
	t.Cleanup(func() {
		if unmounted {
			return
		}
		if err := mount.Unmount(dir); err != nil {
			t.Logf("Unmount cleanup: %v", err)
		}
	})

	if err := os.WriteFile(filepath.Join(dir, "during.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write failed while a real filesystem shadows the immutable directory: %v", err)
	}

	if err := mount.Unmount(dir); err != nil {
		t.Fatalf("Unmount: %v", err)
	}
	unmounted = true

	if err := os.WriteFile(filepath.Join(dir, "after.txt"), []byte("x"), 0o644); err == nil {
		t.Fatalf("write succeeded after unmount, want the underlying directory to still be immutable")
	}
}

// Checks IsImmutable before and after EnsureImmutable //
func TestIsImmutable(t *testing.T) {
	requireBinaries(t, "chattr", "lsattr")
	loopback.RequireRoot(t)

	dir := t.TempDir()
	before, err := mount.IsImmutable(dir)
	if err != nil {
		t.Fatalf("IsImmutable (before): %v", err)
	}
	if before {
		t.Fatalf("IsImmutable = true before EnsureImmutable ever ran")
	}

	if err := mount.EnsureImmutable(dir); err != nil {
		t.Fatalf("EnsureImmutable: %v", err)
	}
	t.Cleanup(func() { clearImmutable(t, dir) })

	after, err := mount.IsImmutable(dir)
	if err != nil {
		t.Fatalf("IsImmutable (after): %v", err)
	}
	if !after {
		t.Fatalf("IsImmutable = false after EnsureImmutable")
	}
}
