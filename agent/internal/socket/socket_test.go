package socket_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IvanBez42/Portcullio/agent/internal/luks"
	"github.com/IvanBez42/Portcullio/agent/internal/mount"
	"github.com/IvanBez42/Portcullio/agent/internal/provision"
	"github.com/IvanBez42/Portcullio/agent/internal/socket"
	"github.com/IvanBez42/Portcullio/agent/test/loopback"
)

const testPassphrase = "test-passphrase-only"

func requireBinaries(t *testing.T, bins ...string) {
	t.Helper()
	for _, bin := range bins {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("socket: required binary %q not found on PATH", bin)
		}
	}
}

// Skips the test if docker isn't usable //
func requireDocker(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("socket: docker not found on PATH")
	}
}

func randomContainerName(t *testing.T, prefix string) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("generate random container name: %v", err)
	}
	return prefix + "-" + hex.EncodeToString(b)
}

// Creates (but does not start) a disposable busybox container //
func createLinkableContainer(t *testing.T, name string) {
	t.Helper()
	cmd := exec.Command("docker", "create", "--name", name,
		"busybox:latest", "sh", "-c", "sleep 3600")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("docker create %s: %v: %s", name, err, out)
	}
	t.Cleanup(func() {
		cmd := exec.Command("docker", "rm", "-f", name)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("docker rm -f %s cleanup: %v: %s", name, err, out)
		}
	})
}

// Checks if a container is running //
func containerRunning(t *testing.T, name string) bool {
	t.Helper()
	out, err := exec.Command("docker", "inspect", "--format", "{{.State.Running}}", name).Output()
	if err != nil {
		t.Fatalf("docker inspect %s: %v", name, err)
	}
	return strings.TrimSpace(string(out)) == "true"
}

// Builds a HandlerConfig against fresh temp dirs //
func newHandlerConfig(t *testing.T) socket.HandlerConfig {
	t.Helper()
	dir := t.TempDir()
	inputDir := filepath.Join(dir, "input")
	mountArea := filepath.Join(dir, "mounts")
	if err := os.MkdirAll(inputDir, 0o700); err != nil {
		t.Fatalf("mkdir input dir: %v", err)
	}
	if err := os.MkdirAll(mountArea, 0o700); err != nil {
		t.Fatalf("mkdir mount area: %v", err)
	}
	return socket.HandlerConfig{
		InputDir:          inputDir,
		MountAreaDir:      mountArea,
		Fstype:            "ext4",
		SealHandleTimeout: 2 * time.Second,
		SealPollInterval:  100 * time.Millisecond,
	}
}

// Tears down real system state left behind by a test vault //
func vaultCleanup(t *testing.T, cfg socket.HandlerConfig, vaultID string) {
	t.Helper()
	mapperName := "portcullio-" + vaultID
	imagePath := filepath.Join(cfg.InputDir, vaultID+".img")
	mountPath := filepath.Join(cfg.MountAreaDir, vaultID)
	t.Cleanup(func() {
		if err := mount.Unmount(mountPath); err != nil {
			t.Logf("Unmount cleanup: %v", err)
		}
		if out, err := exec.Command("chattr", "-i", mountPath).CombinedOutput(); err != nil {
			t.Logf("chattr -i cleanup: %v: %s", err, out)
		}
		_ = luks.Close(mapperName)
		if loopPath, ok, _ := luks.FindLoopDevice(imagePath); ok {
			_ = luks.DetachLoop(loopPath)
		}
		os.Remove(imagePath)
	})
}

// Starts a real socket.Server on a fresh Unix socket //
func startServer(t *testing.T, handler *socket.AgentHandler) string {
	t.Helper()
	sockPath := filepath.Join(t.TempDir(), "agent.sock")
	srv, err := socket.NewServer(sockPath, handler)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go func() {
		_ = srv.Serve()
	}()
	t.Cleanup(func() {
		_ = srv.Close()
	})
	return sockPath
}

func call(t *testing.T, sockPath string, req socket.Request) socket.Response {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	resp, err := socket.Call(ctx, sockPath, req)
	if err != nil {
		t.Fatalf("Call(%s): %v", req.Verb, err)
	}
	return resp
}

// Checks the full verb set end-to-end over a real socket //
func TestFullLifecycleOverSocket(t *testing.T) {
	loopback.RequireRoot(t)
	loopback.RequireBinaries(t)
	requireBinaries(t, "mount", "umount", "mkfs.ext4", "chattr")

	cfg := newHandlerConfig(t)
	const vaultID = "lifecycle-vault"
	vaultCleanup(t, cfg, vaultID)

	handler := socket.NewAgentHandler(cfg)
	sockPath := startServer(t, handler)

	createResp := call(t, sockPath, socket.Request{
		Verb: socket.VerbCreate, VaultID: vaultID,
		Passphrase: []byte(testPassphrase), SizeMB: 64,
	})
	if !createResp.OK {
		t.Fatalf("create: %s", createResp.Error)
	}
	if createResp.State != "sealed" {
		t.Fatalf("create State = %q, want sealed", createResp.State)
	}

	statusResp := call(t, sockPath, socket.Request{Verb: socket.VerbStatus})
	if !statusResp.OK {
		t.Fatalf("status (all): %s", statusResp.Error)
	}
	if len(statusResp.Vaults) != 1 || statusResp.Vaults[0].VaultID != vaultID || statusResp.Vaults[0].State != "sealed" {
		t.Fatalf("status (all) = %+v, want exactly one sealed %q", statusResp.Vaults, vaultID)
	}

	unsealResp := call(t, sockPath, socket.Request{
		Verb: socket.VerbUnseal, VaultID: vaultID, Passphrase: []byte(testPassphrase),
	})
	if !unsealResp.OK {
		t.Fatalf("unseal: %s", unsealResp.Error)
	}
	if unsealResp.State != "unsealed" {
		t.Fatalf("unseal State = %q, want unsealed", unsealResp.State)
	}

	statusOneResp := call(t, sockPath, socket.Request{Verb: socket.VerbStatus, VaultID: vaultID})
	if !statusOneResp.OK || len(statusOneResp.Vaults) != 1 || statusOneResp.Vaults[0].State != "unsealed" {
		t.Fatalf("status (one) after unseal = %+v (err %q), want unsealed", statusOneResp.Vaults, statusOneResp.Error)
	}

	sealResp := call(t, sockPath, socket.Request{Verb: socket.VerbSeal, VaultID: vaultID})
	if !sealResp.OK {
		t.Fatalf("seal: %s", sealResp.Error)
	}
	if sealResp.State != "sealed" {
		t.Fatalf("seal State = %q, want sealed", sealResp.State)
	}

	destroyResp := call(t, sockPath, socket.Request{Verb: socket.VerbDestroy, VaultID: vaultID})
	if !destroyResp.OK {
		t.Fatalf("destroy: %s", destroyResp.Error)
	}

	afterDestroy := call(t, sockPath, socket.Request{Verb: socket.VerbStatus, VaultID: vaultID})
	if afterDestroy.OK {
		t.Fatalf("status after destroy succeeded, want refusal (vault should no longer exist)")
	}

	// Checks the mount stub is gone too, not just the image //
	mountPath := filepath.Join(cfg.MountAreaDir, vaultID)
	if _, err := os.Stat(mountPath); !os.IsNotExist(err) {
		t.Fatalf("mount stub %s should be gone after destroy, stat err = %v", mountPath, err)
	}
}

// Checks unseal starts and seal stops linked services //
func TestUnsealStartsAndSealStopsLinkedServices(t *testing.T) {
	loopback.RequireRoot(t)
	loopback.RequireBinaries(t)
	requireBinaries(t, "mount", "umount", "mkfs.ext4", "chattr")
	requireDocker(t)

	containerName := randomContainerName(t, "portcullio-test-svc")
	createLinkableContainer(t, containerName)

	cfg := newHandlerConfig(t)
	const vaultID = "service-linked-vault"
	vaultCleanup(t, cfg, vaultID)

	handler := socket.NewAgentHandler(cfg)
	sockPath := startServer(t, handler)

	createResp := call(t, sockPath, socket.Request{
		Verb: socket.VerbCreate, VaultID: vaultID,
		Passphrase: []byte(testPassphrase), SizeMB: 64,
	})
	if !createResp.OK {
		t.Fatalf("create: %s", createResp.Error)
	}

	unsealResp := call(t, sockPath, socket.Request{
		Verb: socket.VerbUnseal, VaultID: vaultID,
		Passphrase: []byte(testPassphrase), Services: []string{containerName},
	})
	if !unsealResp.OK {
		t.Fatalf("unseal: %s", unsealResp.Error)
	}
	if !containerRunning(t, containerName) {
		t.Fatalf("%s not running after unseal with linked services", containerName)
	}

	sealResp := call(t, sockPath, socket.Request{
		Verb: socket.VerbSeal, VaultID: vaultID, Services: []string{containerName},
	})
	if !sealResp.OK {
		t.Fatalf("seal: %s", sealResp.Error)
	}
	if containerRunning(t, containerName) {
		t.Fatalf("%s still running after seal with linked services", containerName)
	}
}

// Checks when a container was last started //
func containerStartedAt(t *testing.T, name string) string {
	t.Helper()
	out, err := exec.Command("docker", "inspect", "--format", "{{.State.StartedAt}}", name).Output()
	if err != nil {
		t.Fatalf("docker inspect %s: %v", name, err)
	}
	return strings.TrimSpace(string(out))
}

// Checks unseal restarts an already-running linked service //
func TestUnsealRestartsAlreadyRunningLinkedService(t *testing.T) {
	loopback.RequireRoot(t)
	loopback.RequireBinaries(t)
	requireBinaries(t, "mount", "umount", "mkfs.ext4", "chattr")
	requireDocker(t)

	containerName := randomContainerName(t, "portcullio-test-svc")
	createLinkableContainer(t, containerName)
	if out, err := exec.Command("docker", "start", containerName).CombinedOutput(); err != nil {
		t.Fatalf("docker start %s (pre-unseal): %v: %s", containerName, err, out)
	}
	startedBefore := containerStartedAt(t, containerName)

	cfg := newHandlerConfig(t)
	const vaultID = "service-already-running-vault"
	vaultCleanup(t, cfg, vaultID)

	handler := socket.NewAgentHandler(cfg)
	sockPath := startServer(t, handler)

	createResp := call(t, sockPath, socket.Request{
		Verb: socket.VerbCreate, VaultID: vaultID,
		Passphrase: []byte(testPassphrase), SizeMB: 64,
	})
	if !createResp.OK {
		t.Fatalf("create: %s", createResp.Error)
	}

	unsealResp := call(t, sockPath, socket.Request{
		Verb: socket.VerbUnseal, VaultID: vaultID,
		Passphrase: []byte(testPassphrase), Services: []string{containerName},
	})
	if !unsealResp.OK {
		t.Fatalf("unseal: %s", unsealResp.Error)
	}
	if !containerRunning(t, containerName) {
		t.Fatalf("%s not running after unseal", containerName)
	}
	startedAfter := containerStartedAt(t, containerName)
	if startedAfter == startedBefore {
		t.Fatalf("%s StartedAt unchanged by unseal (%s) -- container was never actually restarted, so its mount namespace still shows whatever was mounted before Unseal ran", containerName, startedAfter)
	}
}

// Checks seal succeeds even if stopping services fails //
func TestSealSucceedsDespiteFailedStop(t *testing.T) {
	loopback.RequireRoot(t)
	loopback.RequireBinaries(t)
	requireBinaries(t, "mount", "umount", "mkfs.ext4", "chattr")
	requireDocker(t)

	cfg := newHandlerConfig(t)
	const vaultID = "stop-fails-vault"
	vaultCleanup(t, cfg, vaultID)

	handler := socket.NewAgentHandler(cfg)
	sockPath := startServer(t, handler)

	createResp := call(t, sockPath, socket.Request{
		Verb: socket.VerbCreate, VaultID: vaultID,
		Passphrase: []byte(testPassphrase), SizeMB: 64,
	})
	if !createResp.OK {
		t.Fatalf("create: %s", createResp.Error)
	}
	unsealResp := call(t, sockPath, socket.Request{
		Verb: socket.VerbUnseal, VaultID: vaultID, Passphrase: []byte(testPassphrase),
	})
	if !unsealResp.OK {
		t.Fatalf("unseal: %s", unsealResp.Error)
	}

	sealResp := call(t, sockPath, socket.Request{
		Verb: socket.VerbSeal, VaultID: vaultID, Services: []string{"not-a-real-container"},
	})
	if !sealResp.OK {
		t.Fatalf("seal with a failing stop = refused (%s), want success with a reported warning", sealResp.Error)
	}
	if sealResp.State != "sealed" {
		t.Fatalf("seal State = %q, want sealed", sealResp.State)
	}
	if sealResp.Error == "" {
		t.Fatalf("seal succeeded but didn't report the stop failure")
	}
}

// Checks the services verb lists linkable containers //
func TestServicesListsLinkableContainers(t *testing.T) {
	requireDocker(t)

	containerName := randomContainerName(t, "portcullio-test-menu")
	createLinkableContainer(t, containerName)

	cfg := newHandlerConfig(t)
	handler := socket.NewAgentHandler(cfg)
	sockPath := startServer(t, handler)

	resp := call(t, sockPath, socket.Request{Verb: socket.VerbServices})
	if !resp.OK {
		t.Fatalf("services: %s", resp.Error)
	}
	found := false
	for _, s := range resp.Services {
		if s == containerName {
			found = true
		}
	}
	if !found {
		t.Fatalf("services = %+v, want it to include %q", resp.Services, containerName)
	}
}

// Checks the space verb reports available MB //
func TestSpaceReportsAvailableMB(t *testing.T) {
	cfg := newHandlerConfig(t)
	handler := socket.NewAgentHandler(cfg)
	sockPath := startServer(t, handler)

	resp := call(t, sockPath, socket.Request{Verb: socket.VerbSpace})
	if !resp.OK {
		t.Fatalf("space: %s", resp.Error)
	}
	if resp.AvailableMB <= 0 {
		t.Fatalf("AvailableMB = %d, want > 0", resp.AvailableMB)
	}
}

// Checks status refuses an invalid vault_id //
func TestStatusRefusesInvalidVaultID(t *testing.T) {
	cfg := newHandlerConfig(t)
	handler := socket.NewAgentHandler(cfg)
	sockPath := startServer(t, handler)

	resp := call(t, sockPath, socket.Request{Verb: socket.VerbStatus, VaultID: "../etc/passwd"})
	if resp.OK {
		t.Fatalf("status with a path-traversal vault_id succeeded, want refusal")
	}
}

// Checks unseal refuses an unknown vault_id //
func TestUnsealRefusesUnknownVault(t *testing.T) {
	cfg := newHandlerConfig(t)
	handler := socket.NewAgentHandler(cfg)
	sockPath := startServer(t, handler)

	resp := call(t, sockPath, socket.Request{
		Verb: socket.VerbUnseal, VaultID: "never-created", Passphrase: []byte(testPassphrase),
	})
	if resp.OK {
		t.Fatalf("unseal of a never-created vault_id succeeded, want refusal")
	}
}

// Creates a real, sealed vault at the path AgentHandler would derive //
func provisionVaultForHandler(t *testing.T, cfg socket.HandlerConfig, vaultID string) {
	t.Helper()
	loopback.RequireRoot(t)
	loopback.RequireBinaries(t)
	requireBinaries(t, "mount", "umount", "mkfs.ext4", "chattr")

	vaultCleanup(t, cfg, vaultID)
	imagePath := filepath.Join(cfg.InputDir, vaultID+".img")
	mapperName := "portcullio-" + vaultID
	if err := provision.CreateVault(provision.CreateVaultParams{
		ImagePath:  imagePath,
		SizeMB:     64,
		Fstype:     cfg.Fstype,
		MapperName: mapperName,
		Passphrase: []byte(testPassphrase),
	}); err != nil {
		t.Fatalf("CreateVault(%s): %v", vaultID, err)
	}
}

// Manufactures a degraded vault: mapper open, nothing mounted //
func makeDegradedMapperOpenNothingMounted(t *testing.T, cfg socket.HandlerConfig, vaultID string) {
	t.Helper()
	imagePath := filepath.Join(cfg.InputDir, vaultID+".img")
	mapperName := "portcullio-" + vaultID
	mountPath := filepath.Join(cfg.MountAreaDir, vaultID)

	loopPath, err := luks.AttachLoop(imagePath)
	if err != nil {
		t.Fatalf("AttachLoop(%s): %v", vaultID, err)
	}
	if err := luks.Open(loopPath, mapperName, []byte(testPassphrase)); err != nil {
		t.Fatalf("luks.Open(%s): %v", vaultID, err)
	}
	if err := os.MkdirAll(mountPath, 0o700); err != nil {
		t.Fatalf("mkdir mount path for %s: %v", vaultID, err)
	}
}

// Manufactures a degraded vault: unrecognized mount at its path //
func makeDegradedUnrecognizedMount(t *testing.T, cfg socket.HandlerConfig, vaultID string) *loopback.Device {
	t.Helper()
	imagePath := filepath.Join(cfg.InputDir, vaultID+".img")
	mapperName := "portcullio-" + vaultID
	mountPath := filepath.Join(cfg.MountAreaDir, vaultID)

	loopPath, err := luks.AttachLoop(imagePath)
	if err != nil {
		t.Fatalf("AttachLoop(%s): %v", vaultID, err)
	}
	if err := luks.Open(loopPath, mapperName, []byte(testPassphrase)); err != nil {
		t.Fatalf("luks.Open(%s): %v", vaultID, err)
	}

	dir := t.TempDir()
	dev, err := loopback.Create(dir, 64)
	if err != nil {
		t.Fatalf("loopback.Create (unrelated device): %v", err)
	}
	if err := dev.Attach(); err != nil {
		t.Fatalf("Attach (unrelated device): %v", err)
	}
	if err := dev.Format([]byte(testPassphrase)); err != nil {
		t.Fatalf("Format (unrelated device): %v", err)
	}
	if err := dev.Open([]byte(testPassphrase)); err != nil {
		t.Fatalf("Open (unrelated device): %v", err)
	}
	if err := loopback.Mkfs(dev.MapperPath(), cfg.Fstype); err != nil {
		t.Fatalf("Mkfs (unrelated device): %v", err)
	}

	if err := os.MkdirAll(mountPath, 0o700); err != nil {
		t.Fatalf("mkdir mount path for %s: %v", vaultID, err)
	}
	if err := mount.MountReal(dev.MapperPath(), cfg.Fstype, mountPath); err != nil {
		t.Fatalf("MountReal (unrelated device onto %s): %v", vaultID, err)
	}
	return dev
}

// Checks ReconcileAll heals a degraded vault, leaves a healthy one alone //
func TestReconcileAllHealsDegradedVaults(t *testing.T) {
	cfg := newHandlerConfig(t)
	const healthyID = "reconcile-all-healthy"
	const degradedID = "reconcile-all-degraded"

	provisionVaultForHandler(t, cfg, healthyID)
	provisionVaultForHandler(t, cfg, degradedID)
	makeDegradedMapperOpenNothingMounted(t, cfg, degradedID)

	handler := socket.NewAgentHandler(cfg)

	var logs []string
	if err := handler.ReconcileAll(2*time.Second, 100*time.Millisecond, func(msg string) {
		logs = append(logs, msg)
	}); err != nil {
		t.Fatalf("ReconcileAll: %v", err)
	}

	if len(logs) != 1 {
		t.Fatalf("ReconcileAll logs = %v, want exactly one line", logs)
	}
	if !strings.Contains(logs[0], degradedID) || !strings.Contains(logs[0], "auto-healed") {
		t.Fatalf("ReconcileAll log line = %q, want it to report %q auto-healed", logs[0], degradedID)
	}

	statusResp := handler.Handle(socket.Request{Verb: socket.VerbStatus})
	if !statusResp.OK {
		t.Fatalf("status after ReconcileAll: %s", statusResp.Error)
	}
	for _, v := range statusResp.Vaults {
		if v.State != "sealed" {
			t.Fatalf("vault %q state = %q after ReconcileAll, want sealed", v.VaultID, v.State)
		}
	}
}

// Checks ReconcileAll continues past an unhealable vault //
func TestReconcileAllContinuesPastAnUnhealableVault(t *testing.T) {
	cfg := newHandlerConfig(t)
	const healthyID = "reconcile-all-healthy-2"
	const stuckID = "reconcile-all-stuck"

	provisionVaultForHandler(t, cfg, healthyID)
	provisionVaultForHandler(t, cfg, stuckID)
	dev := makeDegradedUnrecognizedMount(t, cfg, stuckID)
	t.Cleanup(func() {
		if err := dev.TeardownAll(); err != nil {
			t.Logf("unrelated device teardown: %v", err)
		}
	})

	handler := socket.NewAgentHandler(cfg)

	var logs []string
	if err := handler.ReconcileAll(300*time.Millisecond, 50*time.Millisecond, func(msg string) {
		logs = append(logs, msg)
	}); err != nil {
		t.Fatalf("ReconcileAll: %v", err)
	}

	if len(logs) != 1 {
		t.Fatalf("ReconcileAll logs = %v, want exactly one line", logs)
	}
	if !strings.Contains(logs[0], stuckID) || !strings.Contains(logs[0], "needs attention") {
		t.Fatalf("ReconcileAll log line = %q, want it to report %q needing attention", logs[0], stuckID)
	}

	statusResp := handler.Handle(socket.Request{Verb: socket.VerbStatus, VaultID: healthyID})
	if !statusResp.OK || len(statusResp.Vaults) != 1 || statusResp.Vaults[0].State != "sealed" {
		t.Fatalf("status(%s) after ReconcileAll = %+v (err %q), want sealed", healthyID, statusResp.Vaults, statusResp.Error)
	}

	stuckResp := handler.Handle(socket.Request{Verb: socket.VerbStatus, VaultID: stuckID})
	if !stuckResp.OK || len(stuckResp.Vaults) != 1 || stuckResp.Vaults[0].State != "degraded" {
		t.Fatalf("status(%s) after ReconcileAll = %+v (err %q), want still degraded", stuckID, stuckResp.Vaults, stuckResp.Error)
	}
}

func TestStatusAllOnEmptyInputDirReturnsNoVaults(t *testing.T) {
	cfg := newHandlerConfig(t)
	handler := socket.NewAgentHandler(cfg)
	sockPath := startServer(t, handler)

	resp := call(t, sockPath, socket.Request{Verb: socket.VerbStatus})
	if !resp.OK {
		t.Fatalf("status (all) on empty input dir: %s", resp.Error)
	}
	if len(resp.Vaults) != 0 {
		t.Fatalf("status (all) on empty input dir = %+v, want none", resp.Vaults)
	}
}
