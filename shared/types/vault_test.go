package types_test

import (
	"encoding/json"
	"testing"

	"github.com/duck-labs/agentsdx-shared/types"
)

func TestVaultData_JSONRoundtrip(t *testing.T) {
	original := types.VaultData{
		GitPrivateKey:      "git-priv-key",
		GitPublicKey:       "git-pub-key",
		VMAccessPrivateKey: "vm-priv-key",
		VMAccessPublicKey:  "vm-pub-key",
		AgentStatePaths:    []string{".claude/", ".claude.json"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got types.VaultData
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.GitPrivateKey != original.GitPrivateKey {
		t.Errorf("GitPrivateKey: got %q, want %q", got.GitPrivateKey, original.GitPrivateKey)
	}
	if got.VMAccessPublicKey != original.VMAccessPublicKey {
		t.Errorf("VMAccessPublicKey: got %q, want %q", got.VMAccessPublicKey, original.VMAccessPublicKey)
	}
	if len(got.AgentStatePaths) != 2 {
		t.Errorf("AgentStatePaths: got %d items, want 2", len(got.AgentStatePaths))
	}
	if got.AgentStatePaths[0] != ".claude/" || got.AgentStatePaths[1] != ".claude.json" {
		t.Errorf("AgentStatePaths: got %v", got.AgentStatePaths)
	}
}

func TestVaultData_DefaultAgentStatePaths(t *testing.T) {
	v := types.DefaultVaultData()
	if len(v.AgentStatePaths) != 2 {
		t.Fatalf("expected 2 default paths, got %d", len(v.AgentStatePaths))
	}
	if v.AgentStatePaths[0] != ".claude/" {
		t.Errorf("first path: got %q, want %q", v.AgentStatePaths[0], ".claude/")
	}
	if v.AgentStatePaths[1] != ".claude.json" {
		t.Errorf("second path: got %q, want %q", v.AgentStatePaths[1], ".claude.json")
	}
}

func TestVaultData_SecretsField(t *testing.T) {
	vd := types.VaultData{
		Secrets: map[string]string{"GITHUB_PAT": "ghp_abc", "OPENAI_API_KEY": "sk-xyz"},
	}
	data, err := json.Marshal(vd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got types.VaultData
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Secrets["GITHUB_PAT"] != "ghp_abc" {
		t.Errorf("GITHUB_PAT: got %q, want %q", got.Secrets["GITHUB_PAT"], "ghp_abc")
	}
	if got.Secrets["OPENAI_API_KEY"] != "sk-xyz" {
		t.Errorf("OPENAI_API_KEY: got %q, want %q", got.Secrets["OPENAI_API_KEY"], "sk-xyz")
	}
}
