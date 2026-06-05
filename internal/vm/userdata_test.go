package vm_test

import (
	"strings"
	"testing"

	"github.com/duck-labs/agentsdx/internal/types"
	"github.com/duck-labs/agentsdx/internal/vm"
)

func TestBuildUserData_basic(t *testing.T) {
	ud := vm.BuildUserData("ssh-ed25519 AAAA", "git-private-key", "myprofile", nil, nil)
	if !strings.Contains(ud, "#cloud-config") {
		t.Error("missing cloud-config header")
	}
	if !strings.Contains(ud, "AGENTSDX_PROFILE=myprofile") {
		t.Error("missing AGENTSDX_PROFILE")
	}
	if strings.Contains(ud, "AGENTSDX_SERVER_URL") {
		t.Error("should not contain AGENTSDX_SERVER_URL")
	}
	if strings.Contains(ud, "AGENTSDX_SESSION_ID") {
		t.Error("should not contain AGENTSDX_SESSION_ID")
	}
}

func TestBuildUserData_secrets(t *testing.T) {
	secrets := map[string]string{"MY_TOKEN": "abc123"}
	ud := vm.BuildUserData("key", "priv", "p", secrets, nil)
	if !strings.Contains(ud, "MY_TOKEN=abc123") {
		t.Errorf("expected MY_TOKEN in userdata, got:\n%s", ud)
	}
}

func TestBuildUserData_project(t *testing.T) {
	projects := []types.ProjectConfig{{Repo: "https://github.com/foo/bar", Path: "~/bar"}}
	ud := vm.BuildUserData("key", "priv", "p", nil, projects)
	if !strings.Contains(ud, "git clone https://github.com/foo/bar ~/bar") {
		t.Errorf("expected git clone in userdata, got:\n%s", ud)
	}
}
