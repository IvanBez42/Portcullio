package luks_test

import (
	"os"
	"testing"

	"github.com/IvanBez42/Portcullio/agent/internal/luks"
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

// Checks Open/Close work and are idempotent //
func TestOpenCloseAndIdempotency(t *testing.T) {
	loopback.RequireRoot(t)
	loopback.RequireBinaries(t)

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
	loopPath := dev.LoopPath()
	const mapperName = "portcullio-luks-selftest"

	// Cleanup: close before detach //
	t.Cleanup(func() {
		if err := luks.Close(mapperName); err != nil {
			t.Logf("luks.Close cleanup: %v", err)
		}
	})

	requireNoErr(t, luks.Open(loopPath, mapperName, []byte(testPassphrase)), "Open")

	mapped, err := luks.IsMapped(mapperName)
	requireNoErr(t, err, "IsMapped")
	if !mapped {
		t.Fatalf("IsMapped = false right after Open")
	}

	mapperPath := luks.MapperPath(mapperName)
	fi, err := os.Stat(mapperPath)
	if err != nil {
		t.Fatalf("stat %s: %v", mapperPath, err)
	}
	if fi.Mode()&os.ModeDevice == 0 || fi.Mode()&os.ModeCharDevice != 0 {
		t.Fatalf("%s exists but is not a block device (mode=%v)", mapperPath, fi.Mode())
	}

	// Checks re-opening an open mapper is a no-op //
	requireNoErr(t, luks.Open(loopPath, mapperName, []byte(testPassphrase)), "second Open (should be idempotent no-op)")

	requireNoErr(t, luks.Close(mapperName), "Close")
	mapped, err = luks.IsMapped(mapperName)
	requireNoErr(t, err, "IsMapped after Close")
	if mapped {
		t.Fatalf("IsMapped = true after Close")
	}

	// Checks re-closing a closed mapper is a no-op //
	requireNoErr(t, luks.Close(mapperName), "second Close (should be idempotent no-op)")
}

// Checks AttachLoop is idempotent //
func TestAttachLoopIdempotent(t *testing.T) {
	loopback.RequireRoot(t)
	loopback.RequireBinaries(t)

	dir := t.TempDir()
	dev, err := loopback.Create(dir, 64)
	requireNoErr(t, err, "loopback.Create")
	imagePath := dev.BackingPath()

	var loopPath string
	t.Cleanup(func() {
		if loopPath != "" {
			if err := luks.DetachLoop(loopPath); err != nil {
				t.Logf("DetachLoop cleanup: %v", err)
			}
		}
		if err := dev.Destroy(); err != nil {
			t.Logf("Destroy cleanup: %v", err)
		}
	})

	loopPath, err = luks.AttachLoop(imagePath)
	requireNoErr(t, err, "AttachLoop")
	if loopPath == "" {
		t.Fatalf("AttachLoop returned empty loop path")
	}

	again, err := luks.AttachLoop(imagePath)
	requireNoErr(t, err, "second AttachLoop (should be idempotent)")
	if again != loopPath {
		t.Fatalf("second AttachLoop returned %q, want existing %q", again, loopPath)
	}

	found, ok, err := luks.FindLoopDevice(imagePath)
	requireNoErr(t, err, "FindLoopDevice")
	if !ok {
		t.Fatalf("FindLoopDevice: ok = false, want true")
	}
	if found != loopPath {
		t.Fatalf("FindLoopDevice = %q, want %q", found, loopPath)
	}
}
