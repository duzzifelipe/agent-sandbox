package vm

import (
	"encoding/base64"
	"fmt"
)

// BuildUserData returns cloud-init user-data that injects SSH keys and agent env.
// Session state is tracked by polling the Hetzner API, not by a VM callback.
func BuildUserData(authorizedKey, gitPrivateKey, sessionID, serverURL, profileName string) string {
	encodedKey := base64.StdEncoding.EncodeToString([]byte(gitPrivateKey))
	return fmt.Sprintf(`#cloud-config
write_files:
  - path: /root/.ssh/authorized_keys
    permissions: '0600'
    content: |
      %s
  - path: /root/.ssh/id_rsa
    permissions: '0600'
    encoding: b64
    content: %s
  - path: /etc/agentsdx.env
    permissions: '0600'
    content: |
      AGENTSDX_SERVER_URL=%s
      AGENTSDX_SESSION_ID=%s
      AGENTSDX_PROFILE=%s
runcmd:
  - mkdir -p /root/.ssh && chmod 700 /root/.ssh
  - sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
  - systemctl reload ssh
`, authorizedKey, encodedKey, serverURL, sessionID, profileName)
}
