package vm_test

import (
	"os"
	"strings"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/vm"
	"github.com/kdomanski/iso9660"
)

func TestWriteNoCloudISO_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	metaData := "instance-id: test\nlocal-hostname: test-vm\n"
	userData := "#cloud-config\nssh_authorized_keys:\n  - ssh-rsa AAAA...\n"

	isoPath, err := vm.WriteNoCloudISO(dir, metaData, userData)
	if err != nil {
		t.Fatalf("WriteNoCloudISO: %v", err)
	}
	if _, err := os.Stat(isoPath); err != nil {
		t.Fatalf("ISO file not found: %v", err)
	}
}

func TestWriteNoCloudISO_ContainsFiles(t *testing.T) {
	dir := t.TempDir()
	isoPath, err := vm.WriteNoCloudISO(dir, "instance-id: abc\n", "#cloud-config\n")
	if err != nil {
		t.Fatalf("WriteNoCloudISO: %v", err)
	}
	f, err := os.Open(isoPath)
	if err != nil {
		t.Fatalf("open ISO: %v", err)
	}
	defer f.Close()
	img, err := iso9660.OpenImage(f)
	if err != nil {
		t.Fatalf("open iso9660 image: %v", err)
	}
	root, err := img.RootDir()
	if err != nil {
		t.Fatalf("root dir: %v", err)
	}
	children, err := root.GetChildren()
	if err != nil {
		t.Fatalf("get children: %v", err)
	}
	names := make(map[string]bool)
	for _, c := range children {
		names[c.Name()] = true
	}
	for _, want := range []string{"meta-data", "user-data"} {
		if !names[want] {
			t.Errorf("ISO missing file %q; got names: %v", want, names)
		}
	}
}

func TestBuildUserData_ContainsSSHKey(t *testing.T) {
	ud := vm.BuildUserData("ssh-rsa AAAA...", "-----BEGIN OPENSSH PRIVATE KEY-----\nABC\n-----END OPENSSH PRIVATE KEY-----", "sess-1", "http://server:8080", "myprofile", "")
	if !strings.Contains(ud, "ssh-rsa AAAA...") {
		t.Errorf("user-data missing authorized key")
	}
	if !strings.Contains(ud, "/root/.ssh/id_rsa") {
		t.Errorf("user-data missing id_rsa write_files entry")
	}
}

func TestBuildUserData_ContainsEnvFile(t *testing.T) {
	ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-42", "http://server:8080", "work-backend", "")
	if !strings.Contains(ud, "/etc/agentsdx.env") {
		t.Errorf("user-data missing agentsdx.env write_files entry")
	}
	if !strings.Contains(ud, "AGENTSDX_SERVER_URL=http://server:8080") {
		t.Errorf("user-data missing AGENTSDX_SERVER_URL")
	}
	if !strings.Contains(ud, "AGENTSDX_SESSION_ID=sess-42") {
		t.Errorf("user-data missing AGENTSDX_SESSION_ID")
	}
	if !strings.Contains(ud, "AGENTSDX_PROFILE=work-backend") {
		t.Errorf("user-data missing AGENTSDX_PROFILE")
	}
}

func TestBuildUserData_ContainsCallbackRuncmd(t *testing.T) {
	ud := vm.BuildUserData(
		"ssh-rsa AAAA...", "git-key", "sess-1",
		"http://server:8080", "myprofile",
		"http://server:8080/sessions/sess-1/ip",
	)
	if !strings.Contains(ud, "runcmd") {
		t.Errorf("user-data missing runcmd section")
	}
	if !strings.Contains(ud, "curl -f --retry") {
		t.Errorf("runcmd missing POST curl command")
	}
	// Callback uses the VM's default gateway so it works behind NAT (e.g. Apple VZ).
	if !strings.Contains(ud, "ip route show default") {
		t.Errorf("runcmd should discover the default gateway")
	}
	if !strings.Contains(ud, ":8080/sessions/sess-1/ip") {
		t.Errorf("runcmd should include port and path from callback URL")
	}
	if !strings.Contains(ud, `\"ip\":\"$IP\"`) {
		t.Errorf("runcmd missing ip payload")
	}
}

func TestBuildUserData_NoCallbackWhenURLEmpty(t *testing.T) {
	ud := vm.BuildUserData(
		"ssh-rsa AAAA...", "git-key", "sess-1",
		"http://server:8080", "myprofile",
		"",
	)
	if strings.Contains(ud, "runcmd") {
		t.Errorf("user-data should not contain runcmd when vmCallbackURL is empty")
	}
}
