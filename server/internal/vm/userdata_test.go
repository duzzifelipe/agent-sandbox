package vm_test

import (
	"strings"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/vm"
)

func TestBuildUserData_ContainsSSHKey(t *testing.T) {
	ud := vm.BuildUserData("ssh-rsa AAAA...", "-----BEGIN OPENSSH PRIVATE KEY-----\nABC\n-----END OPENSSH PRIVATE KEY-----", "sess-1", "http://server:8080", "myprofile")
	if !strings.Contains(ud, "ssh-rsa AAAA...") {
		t.Errorf("user-data missing authorized key")
	}
	if !strings.Contains(ud, "/root/.ssh/id_rsa") {
		t.Errorf("user-data missing id_rsa write_files entry")
	}
}

func TestBuildUserData_ContainsEnvFile(t *testing.T) {
	ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-42", "http://server:8080", "work-backend")
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
