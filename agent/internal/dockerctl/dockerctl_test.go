package dockerctl_test

import (
	"crypto/rand"
	"encoding/hex"
	"os/exec"
	"strings"
	"testing"

	"github.com/IvanBez42/Portcullio/agent/internal/dockerctl"
)

// Testing Docker.sock //
func requireDocker(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("dockerctl: docker not found on PATH")
	}
}

func randomName(t *testing.T, prefix string) string {
	t.Helper()
	b := make([]byte, 6)
	_, err := rand.Read(b)
	requireNoErr(t, err, "generate random container name suffix")
	return prefix + "-" + hex.EncodeToString(b)
}

// Creates a fake container //
func createContainer(t *testing.T, name string) {
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

// Container running check //
func containerRunning(t *testing.T, name string) bool {
	t.Helper()
	out, err := exec.Command("docker", "inspect", "--format", "{{.State.Running}}", name).Output()
	if err != nil {
		t.Fatalf("docker inspect %s: %v", name, err)
	}
	return strings.TrimSpace(string(out)) == "true"
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

// Checks ordinary containers are linkable without any label //
func TestLinkableContainersIncludesOrdinaryContainers(t *testing.T) {
	requireDocker(t)
	name := randomName(t, "portcullio-test-ordinary")
	createContainer(t, name)

	names, err := dockerctl.LinkableContainers()
	requireNoErr(t, err, "LinkableContainers")
	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
	}
	if !got[name] {
		t.Fatalf("LinkableContainers = %v, want it to include %q", names, name)
	}
}

// Checks agent/ui named containers are excluded //
func TestLinkableContainersExcludesAgentAndUI(t *testing.T) {
	requireDocker(t)
	agentLike := randomName(t, "portcullio-agent")
	uiLike := randomName(t, "portcullio-ui")
	ordinary := randomName(t, "portcullio-test-ordinary")
	createContainer(t, agentLike)
	createContainer(t, uiLike)
	createContainer(t, ordinary)

	names, err := dockerctl.LinkableContainers()
	requireNoErr(t, err, "LinkableContainers")
	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
	}
	if got[agentLike] {
		t.Fatalf("LinkableContainers = %v, want it to exclude agent-like name %q", names, agentLike)
	}
	if got[uiLike] {
		t.Fatalf("LinkableContainers = %v, want it to exclude ui-like name %q", names, uiLike)
	}
	if !got[ordinary] {
		t.Fatalf("LinkableContainers = %v, want it to still include ordinary name %q", names, ordinary)
	}
}

// Checks Start refuses an unlinkable container name //
func TestStartRefusesUnknownContainerName(t *testing.T) {
	requireDocker(t)

	requireErr(t, dockerctl.Start([]string{"not-a-real-container"}), "Start succeeded with a nonexistent container name, want refusal")

	agentLike := randomName(t, "portcullio-agent")
	createContainer(t, agentLike)
	requireErr(t, dockerctl.Start([]string{agentLike}), "Start succeeded targeting an agent-like container name, want refusal")
}

// Checks Stop refuses an unlinkable container name //
func TestStopRefusesUnknownContainerName(t *testing.T) {
	requireDocker(t)

	requireErr(t, dockerctl.Stop([]string{"not-a-real-container"}), "Stop succeeded with a nonexistent container name, want refusal")
}

func TestStartActuallyStartsContainer(t *testing.T) {
	requireDocker(t)
	name := randomName(t, "portcullio-test-start")
	createContainer(t, name)

	requireNoErr(t, dockerctl.Start([]string{name}), "Start")
	if !containerRunning(t, name) {
		t.Fatalf("%s not running after Start", name)
	}
}

func TestStopActuallyStopsContainer(t *testing.T) {
	requireDocker(t)
	name := randomName(t, "portcullio-test-stop")
	createContainer(t, name)

	requireNoErr(t, dockerctl.Start([]string{name}), "Start")
	if !containerRunning(t, name) {
		t.Fatalf("%s not running after Start", name)
	}

	requireNoErr(t, dockerctl.Stop([]string{name}), "Stop")
	if containerRunning(t, name) {
		t.Fatalf("%s still running after Stop", name)
	}
}
