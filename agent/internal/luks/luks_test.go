package luks_test

import (
	"os"
	"testing"

	"github.com/IvanBez42/Portcullio/agent/internal/luks"
	"github.com/IvanBez42/Portcullio/agent/test/loopback"
)

const testPassphrase = "test-passphrase-only"

// Checks Open/Close work and are idempotent //
func TestOpenCloseAndIdempotency(t *testing.T) {
	loopback.RequireRoot(t)
	loopback.RequireBinaries(t)

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
	loopPath := dev.LoopPath()
	const mapperName = "portcullio-luks-selftest"

	// Cleanup: close before detach //
	t.Cleanup(func() {
		if err := luks.Close(mapperName); err != nil {
			t.Logf("luks.Close cleanup: %v", err)
		}
	})

	if err := luks.Open(loopPath, mapperName, []byte(testPassphrase)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	mapped, err := luks.IsMapped(mapperName)
	if err != nil {
		t.Fatalf("IsMapped: %v", err)
	}
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
	if err := luks.Open(loopPath, mapperName, []byte(testPassphrase)); err != nil {
		t.Fatalf("second Open (should be idempotent no-op): %v", err)
	}

	if err := luks.Close(mapperName); err != nil {
		t.Fatalf("Close: %v", err)
	}
	mapped, err = luks.IsMapped(mapperName)
	if err != nil {
		t.Fatalf("IsMapped after Close: %v", err)
	}
	if mapped {
		t.Fatalf("IsMapped = true after Close")
	}

	// Checks re-closing a closed mapper is a no-op //
	if err := luks.Close(mapperName); err != nil {
		t.Fatalf("second Close (should be idempotent no-op): %v", err)
	}
}

// Checks AttachLoop is idempotent //
func TestAttachLoopIdempotent(t *testing.T) {
	loopback.RequireRoot(t)
	loopback.RequireBinaries(t)

	dir := t.TempDir()
	dev, err := loopback.Create(dir, 64)
	if err != nil {
		t.Fatalf("loopback.Create: %v", err)
	}
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
	if err != nil {
		t.Fatalf("AttachLoop: %v", err)
	}
	if loopPath == "" {
		t.Fatalf("AttachLoop returned empty loop path")
	}

	again, err := luks.AttachLoop(imagePath)
	if err != nil {
		t.Fatalf("second AttachLoop (should be idempotent): %v", err)
	}
	if again != loopPath {
		t.Fatalf("second AttachLoop returned %q, want existing %q", again, loopPath)
	}

	found, ok, err := luks.FindLoopDevice(imagePath)
	if err != nil {
		t.Fatalf("FindLoopDevice: %v", err)
	}
	if !ok {
		t.Fatalf("FindLoopDevice: ok = false, want true")
	}
	if found != loopPath {
		t.Fatalf("FindLoopDevice = %q, want %q", found, loopPath)
	}
}
