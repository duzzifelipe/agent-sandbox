package vm_test

import (
	"strings"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/vm"
)

func TestBuildUserData_ContainsSSHKey(t *testing.T) {
	ud := vm.BuildUserData("ssh-rsa AAAA...", "-----BEGIN OPENSSH PRIVATE KEY-----\nABC\n-----END OPENSSH PRIVATE KEY-----", "sess-1", "http://server:8080", "myprofile", nil)
	if !strings.Contains(ud, "ssh-rsa AAAA...") {
		t.Errorf("user-data missing authorized key")
	}
	if !strings.Contains(ud, "/root/.ssh/id_rsa") {
		t.Errorf("user-data missing id_rsa write_files entry")
	}
}

func TestBuildUserData_ContainsEnvFile(t *testing.T) {
	ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-42", "http://server:8080", "work-backend", nil)
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

func TestBuildUserData_InjectsSecrets(t *testing.T) {
	secrets := map[string]string{
		"GITHUB_PAT":     "ghp_abc",
		"OPENAI_API_KEY": "sk-xyz",
	}
	ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-1", "http://server:8080", "myprofile", secrets)
	if !strings.Contains(ud, "GITHUB_PAT=ghp_abc") {
		t.Errorf("user-data missing GITHUB_PAT=ghp_abc")
	}
	if !strings.Contains(ud, "OPENAI_API_KEY=sk-xyz") {
		t.Errorf("user-data missing OPENAI_API_KEY=sk-xyz")
	}
}

func TestBuildUserData_NoSecretsOmitsExtraLines(t *testing.T) {
	ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-1", "http://server:8080", "myprofile", nil)
	if !strings.Contains(ud, "AGENTSDX_PROFILE=myprofile") {
		t.Errorf("user-data missing AGENTSDX_PROFILE")
	}
}
