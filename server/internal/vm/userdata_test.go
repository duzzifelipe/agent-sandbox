package vm_test

import (
	"strings"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/vm"
	"github.com/duck-labs/agentsdx-shared/types"
)

func TestBuildUserData_ContainsSSHKey(t *testing.T) {
	ud := vm.BuildUserData("ssh-rsa AAAA...", "-----BEGIN OPENSSH PRIVATE KEY-----\nABC\n-----END OPENSSH PRIVATE KEY-----", "sess-1", "http://server:8080", "myprofile", nil, nil)
	if !strings.Contains(ud, "ssh-rsa AAAA...") {
		t.Errorf("user-data missing authorized key")
	}
	if !strings.Contains(ud, "/home/ubuntu/.ssh/id_rsa") {
		t.Errorf("user-data missing id_rsa write_files entry")
	}
}

func TestBuildUserData_ContainsEnvFile(t *testing.T) {
	ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-42", "http://server:8080", "work-backend", nil, nil)
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
	ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-1", "http://server:8080", "myprofile", secrets, nil)
	if !strings.Contains(ud, "GITHUB_PAT=ghp_abc") {
		t.Errorf("user-data missing GITHUB_PAT=ghp_abc")
	}
	if !strings.Contains(ud, "OPENAI_API_KEY=sk-xyz") {
		t.Errorf("user-data missing OPENAI_API_KEY=sk-xyz")
	}
}

func TestBuildUserData_NoSecretsOmitsExtraLines(t *testing.T) {
	ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-1", "http://server:8080", "myprofile", nil, nil)
	if !strings.Contains(ud, "AGENTSDX_PROFILE=myprofile") {
		t.Errorf("user-data missing AGENTSDX_PROFILE")
	}
}

func TestBuildUserData_ClonesProjects(t *testing.T) {
	projects := []types.ProjectConfig{
		{Repo: "https://github.com/org/api.git", Path: "~/api"},
	}
	ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-1", "http://server:8080", "myprofile", nil, projects)
	if !strings.Contains(ud, `git clone https://github.com/org/api.git ~/api`) {
		t.Errorf("user-data missing git clone command, got:\n%s", ud)
	}
}

func TestBuildUserData_InjectsTokenInCloneURL(t *testing.T) {
	projects := []types.ProjectConfig{
		{Repo: "https://github.com/org/private.git", Path: "~/private", AuthTokenEnv: "GITHUB_TOKEN"},
	}
	secrets := map[string]string{"GITHUB_TOKEN": "ghp_abc123"}
	ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-1", "http://server:8080", "myprofile", secrets, projects)
	if !strings.Contains(ud, "git clone https://ghp_abc123@github.com/org/private.git ~/private") {
		t.Errorf("user-data missing authenticated clone, got:\n%s", ud)
	}
}

func TestBuildUserData_MissingTokenSecretClonesWithoutAuth(t *testing.T) {
	projects := []types.ProjectConfig{
		{Repo: "https://github.com/org/private.git", Path: "~/private", AuthTokenEnv: "GITHUB_TOKEN"},
	}
	ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-1", "http://server:8080", "myprofile", nil, projects)
	if !strings.Contains(ud, "git clone https://github.com/org/private.git ~/private") {
		t.Errorf("expected unauthenticated clone, got:\n%s", ud)
	}
	if strings.Contains(ud, "@github.com") {
		t.Errorf("should not have token in URL when secret is missing")
	}
}

func TestBuildUserData_NoProjectsNoCloneCommands(t *testing.T) {
	ud := vm.BuildUserData("ssh-rsa AAAA...", "git-key", "sess-1", "http://server:8080", "myprofile", nil, nil)
	if strings.Contains(ud, "git clone") {
		t.Errorf("unexpected git clone in user-data with no projects")
	}
}
