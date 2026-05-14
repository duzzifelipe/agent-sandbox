package types

type VaultData struct {
	GitPrivateKey      string   `json:"git_private_key"`
	GitPublicKey       string   `json:"git_public_key"`
	VMAccessPrivateKey string   `json:"vm_access_private_key"`
	VMAccessPublicKey  string   `json:"vm_access_public_key"`
	AgentStatePaths    []string `json:"agent_state_paths"`
}

func DefaultVaultData() VaultData {
	return VaultData{
		AgentStatePaths: []string{".claude/", ".claude.json"},
	}
}
