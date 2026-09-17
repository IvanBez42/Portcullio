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

// Checks MountReal/MountedSource/IsMounted on a real filesystem //
func TestMountReal(t *testing.T) {
	loopback.RequireRoot(t)
	loopback.RequireBinaries(t)
	requireBinaries(t, "mount", "umount", "mkfs.ext4")

	dir := t.TempDir()
	dev, err := loopback.Create(dir, 64)
	requireNoErr(t, err, "loopback.Create")
	t.Cleanup(func() {
		if err := dev.TeardownAll(); err != nil {
			t.Logf("teardown: %v", err)
		}
	})
	requireNoErr(t, dev.Attach(), "Attach")
	requireNoErr(t, dev.Format([]byte(testPassphrase)), "Format")

	const mapperName = "portcullio-mount-selftest"
	t.Cleanup(func() {
		if err := luks.Close(mapperName); err != nil {
			t.Logf("luks.Close cleanup: %v", err)
		}
	})
	requireNoErr(t, luks.Open(dev.LoopPath(), mapperName, []byte(testPassphrase)), "luks.Open")
	mapperPath := luks.MapperPath(mapperName)

	requireNoErr(t, loopback.Mkfs(mapperPath, "ext4"), "loopback.Mkfs")

	target := t.TempDir()
	t.Cleanup(func() {
		if err := mount.Unmount(target); err != nil {
			t.Logf("Unmount cleanup: %v", err)
		}
	})
	requireNoErr(t, mount.MountReal(mapperPath, "ext4", target), "MountReal")

	mounted, err := mount.IsMounted(target)
	requireNoErr(t, err, "IsMounted")
	if !mounted {
		t.Fatalf("IsMounted = false right after MountReal")
	}
	source, ok, err := mount.MountedSource(target)
	requireNoErr(t, err, "MountedSource")
	if !ok || source != mapperPath {
		t.Fatalf("MountedSource = (%q, %v), want (%q, true)", source, ok, mapperPath)
	}

	requireNoErr(t, mount.Unmount(target), "Unmount")
	mounted, err = mount.IsMounted(target)
	requireNoErr(t, err, "IsMounted after Unmount")
	if mounted {
		t.Fatalf("IsMounted = true after Unmount")
	}
}

// Checks MountTmpfs is genuinely ephemeral //
func TestTmpfsStubSwap(t *testing.T) {
	requireBinaries(t, "mount", "umount")
	loopback.RequireRoot(t)

	stubDir := t.TempDir()
	requireNoErr(t, mountTmpfs(stubDir, 16), "MountTmpfs")
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
	requireNoErr(t, err, "MountedSource")
	if !ok || source != "tmpfs" {
		t.Fatalf("MountedSource = (%q, %v), want (\"tmpfs\", true)", source, ok)
	}

	canary := filepath.Join(stubDir, "canary.txt")
	requireNoErr(t, os.WriteFile(canary, []byte("plaintext that must never reach real disk"), 0o644), "write canary")
	_, err = os.Stat(canary)
	requireNoErr(t, err, "canary should exist while tmpfs is mounted")

	requireNoErr(t, mount.Unmount(stubDir), "Unmount")
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
	requireNoErr(t, mountTmpfs(stubDir, 16), "MountTmpfs")
	t.Cleanup(func() {
		if err := mount.Unmount(stubDir); err != nil {
			t.Logf("Unmount cleanup: %v", err)
		}
	})

	f, err := os.Create(filepath.Join(stubDir, "held.txt"))
	requireNoErr(t, err, "create held file")

	holders, err := mount.CheckHandles(stubDir)
	requireNoErr(t, err, "CheckHandles (file open)")
	if len(holders) == 0 {
		t.Fatalf("CheckHandles found no holders while a file is open under %s", stubDir)
	}

	requireNoErr(t, f.Close(), "close held file")

	holders, err = mount.CheckHandles(stubDir)
	requireNoErr(t, err, "CheckHandles (file closed)")
	if len(holders) != 0 {
		t.Fatalf("CheckHandles still reports holders after close: %v", holders)
	}
}

// Checks WaitForNoHandles returns once a handle is released //
func TestWaitForNoHandles(t *testing.T) {
	requireBinaries(t, "mount", "umount", "fuser")
	loopback.RequireRoot(t)

	stubDir := t.TempDir()
	requireNoErr(t, mountTmpfs(stubDir, 16), "MountTmpfs")
	t.Cleanup(func() {
		if err := mount.Unmount(stubDir); err != nil {
			t.Logf("Unmount cleanup: %v", err)
		}
	})

	f, err := os.Create(filepath.Join(stubDir, "held.txt"))
	requireNoErr(t, err, "create held file")
	go func() {
		time.Sleep(150 * time.Millisecond)
		f.Close()
	}()

	holders, err := mount.WaitForNoHandles(stubDir, 2*time.Second, 50*time.Millisecond)
	requireNoErr(t, err, "WaitForNoHandles")
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
	requireNoErr(t, mount.EnsureImmutable(dir), "EnsureImmutable")
	t.Cleanup(func() { clearImmutable(t, dir) })

	requireErr(t, os.WriteFile(filepath.Join(dir, "canary.txt"), []byte("must never land here"), 0o644), "write succeeded inside an immutable directory, want a permission error")
}

// Checks EnsureImmutable is idempotent //
func TestEnsureImmutableIdempotent(t *testing.T) {
	requireBinaries(t, "chattr")
	loopback.RequireRoot(t)

	dir := t.TempDir()
	requireNoErr(t, mount.EnsureImmutable(dir), "first EnsureImmutable")
	t.Cleanup(func() { clearImmutable(t, dir) })

	requireNoErr(t, mount.EnsureImmutable(dir), "second EnsureImmutable")
}

// Checks the immutable flag survives a mount/umount cycle //
func TestEnsureImmutableSurvivesMountCycle(t *testing.T) {
	requireBinaries(t, "chattr", "mount", "umount")
	loopback.RequireRoot(t)

	dir := t.TempDir()
	requireNoErr(t, mount.EnsureImmutable(dir), "EnsureImmutable")
	t.Cleanup(func() { clearImmutable(t, dir) })

	requireErr(t, os.WriteFile(filepath.Join(dir, "before.txt"), []byte("x"), 0o644), "write succeeded before mounting anything, want a permission error")

	requireNoErr(t, mountTmpfs(dir, 16), "MountTmpfs")
	unmounted := false
	t.Cleanup(func() {
		if unmounted {
			return
		}
		if err := mount.Unmount(dir); err != nil {
			t.Logf("Unmount cleanup: %v", err)
		}
	})

	requireNoErr(t, os.WriteFile(filepath.Join(dir, "during.txt"), []byte("x"), 0o644), "write failed while a real filesystem shadows the immutable directory")

	requireNoErr(t, mount.Unmount(dir), "Unmount")
	unmounted = true

	requireErr(t, os.WriteFile(filepath.Join(dir, "after.txt"), []byte("x"), 0o644), "write succeeded after unmount, want the underlying directory to still be immutable")
}

// Checks IsImmutable before and after EnsureImmutable //
func TestIsImmutable(t *testing.T) {
	requireBinaries(t, "chattr", "lsattr")
	loopback.RequireRoot(t)

	dir := t.TempDir()
	before, err := mount.IsImmutable(dir)
	requireNoErr(t, err, "IsImmutable (before)")
	if before {
		t.Fatalf("IsImmutable = true before EnsureImmutable ever ran")
	}

	requireNoErr(t, mount.EnsureImmutable(dir), "EnsureImmutable")
	t.Cleanup(func() { clearImmutable(t, dir) })

	after, err := mount.IsImmutable(dir)
	requireNoErr(t, err, "IsImmutable (after)")
	if !after {
		t.Fatalf("IsImmutable = false after EnsureImmutable")
	}
}
