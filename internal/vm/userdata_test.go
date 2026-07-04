package vm_test

import (
	"strings"
	"testing"

	"github.com/duck-labs/agentsdx/internal/types"
	"github.com/duck-labs/agentsdx/internal/vm"
)

func TestBuildUserData_basic(t *testing.T) {
	ud := vm.BuildUserData("ssh-ed25519 AAAA", "git-private-key", "myprofile", nil, nil, nil)
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
	if strings.Contains(ud, "packages:") {
		t.Errorf("should not contain packages section, got:\n%s", ud)
	}
}

func TestBuildUserData_secrets(t *testing.T) {
	secrets := map[string]string{"MY_TOKEN": "abc123"}
	ud := vm.BuildUserData("key", "priv", "p", secrets, nil, nil)
	if !strings.Contains(ud, "MY_TOKEN=abc123") {
		t.Errorf("expected MY_TOKEN in userdata, got:\n%s", ud)
	}
}

func TestBuildUserData_project(t *testing.T) {
	projects := []types.ProjectConfig{{Repo: "https://github.com/foo/bar", Path: "~/bar"}}
	ud := vm.BuildUserData("key", "priv", "p", nil, projects, nil)
	if !strings.Contains(ud, "git clone https://github.com/foo/bar ~/bar") {
		t.Errorf("expected git clone in userdata, got:\n%s", ud)
	}
}

func TestBuildUserData_portForward(t *testing.T) {
	ud := vm.BuildUserData("key", "priv", "p", nil, nil, []string{"8080:80", "5432:5432"})
	if !strings.Contains(ud, "      - 8080:80") {
		t.Errorf("expected 8080:80 in userdata, got:\n%s", ud)
	}
	if !strings.Contains(ud, "      - 5432:5432") {
		t.Errorf("expected 5432:5432 in userdata, got:\n%s", ud)
	}
	if !strings.Contains(ud, "packages:") {
		t.Errorf("expected packages section in userdata, got:\n%s", ud)
	}
	if !strings.Contains(ud, "qemu-guest-agent") {
		t.Errorf("expected qemu-guest-agent package, got:\n%s", ud)
	}
}
