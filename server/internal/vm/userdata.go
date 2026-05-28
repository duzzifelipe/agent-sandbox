package vm

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// BuildUserData returns cloud-init user-data that injects SSH keys, agent env vars,
// and any per-profile secrets as additional env vars in /etc/agentsdx.env.
func BuildUserData(authorizedKey, gitPrivateKey, sessionID, serverURL, profileName string, secrets map[string]string) string {
	encodedKey := base64.StdEncoding.EncodeToString([]byte(gitPrivateKey))

	var extraEnv strings.Builder
	for k, v := range secrets {
		fmt.Fprintf(&extraEnv, "      %s=%s\n", k, v)
	}

	return fmt.Sprintf(`#cloud-config
ssh_authorized_keys:
  - %s
write_files:
  - path: /home/ubuntu/.ssh/id_rsa
    permissions: '0600'
    encoding: b64
    content: %s
  - path: /etc/agentsdx.env
    permissions: '0644'
    content: |
      AGENTSDX_SERVER_URL=%s
      AGENTSDX_SESSION_ID=%s
      AGENTSDX_PROFILE=%s
%sruncmd:
  - mkdir -p /home/ubuntu/.ssh && chmod 700 /home/ubuntu/.ssh && chown -R ubuntu:ubuntu /home/ubuntu/.ssh
`, authorizedKey, encodedKey, serverURL, sessionID, profileName, extraEnv.String())
}
