package loopback

import (
	"os"
	"testing"
)

// Checks the full Device lifecycle: Create -> ... -> Destroy //
func TestLoopbackDeviceLifecycle(t *testing.T) {
	RequireRoot(t)
	RequireBinaries(t)

	dir := t.TempDir()
	dev, err := Create(dir, 64)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		if err := dev.TeardownAll(); err != nil {
			t.Logf("teardown: %v", err)
		}
	})

	if err := dev.Attach(); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := dev.Format([]byte("test-passphrase-only")); err != nil {
		t.Fatalf("Format: %v", err)
	}
	if err := dev.Open([]byte("test-passphrase-only")); err != nil {
		t.Fatalf("Open: %v", err)
	}

	mapperPath := dev.MapperPath()
	fi, err := os.Stat(mapperPath)
	if err != nil {
		t.Fatalf("stat %s: %v", mapperPath, err)
	}
	// Checks it's a block device, not a char device //
	if fi.Mode()&os.ModeDevice == 0 || fi.Mode()&os.ModeCharDevice != 0 {
		t.Fatalf("%s exists but is not a block device (mode=%v)", mapperPath, fi.Mode())
	}

	if err := dev.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := dev.Detach(); err != nil {
		t.Fatalf("Detach: %v", err)
	}
	if err := dev.Destroy(); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if _, err := os.Stat(dev.backingPath); !os.IsNotExist(err) {
		t.Fatalf("expected backing file removed, stat err = %v", err)
	}
}
