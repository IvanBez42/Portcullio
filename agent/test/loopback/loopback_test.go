package loopback

import (
	"os"
	"testing"
)

// Fails the test immediately if err is non-nil //
func requireNoErr(t *testing.T, err error, label string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
}

// Checks the full Device lifecycle: Create -> ... -> Destroy //
func TestLoopbackDeviceLifecycle(t *testing.T) {
	RequireRoot(t)
	RequireBinaries(t)

	dir := t.TempDir()
	dev, err := Create(dir, 64)
	requireNoErr(t, err, "Create")
	t.Cleanup(func() {
		if err := dev.TeardownAll(); err != nil {
			t.Logf("teardown: %v", err)
		}
	})

	requireNoErr(t, dev.Attach(), "Attach")
	requireNoErr(t, dev.Format([]byte("test-passphrase-only")), "Format")
	requireNoErr(t, dev.Open([]byte("test-passphrase-only")), "Open")

	mapperPath := dev.MapperPath()
	fi, err := os.Stat(mapperPath)
	if err != nil {
		t.Fatalf("stat %s: %v", mapperPath, err)
	}
	// Checks it's a block device, not a char device //
	if fi.Mode()&os.ModeDevice == 0 || fi.Mode()&os.ModeCharDevice != 0 {
		t.Fatalf("%s exists but is not a block device (mode=%v)", mapperPath, fi.Mode())
	}

	requireNoErr(t, dev.Close(), "Close")
	requireNoErr(t, dev.Detach(), "Detach")
	requireNoErr(t, dev.Destroy(), "Destroy")
	if _, err := os.Stat(dev.backingPath); !os.IsNotExist(err) {
		t.Fatalf("expected backing file removed, stat err = %v", err)
	}
}
