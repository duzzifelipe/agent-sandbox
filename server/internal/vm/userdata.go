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
bootcmd:
  - mkdir -p /root/.ssh
  - chmod 700 /root/.ssh
ssh_authorized_keys:
  - "%s"
write_files:
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
`, authorizedKey, encodedKey, serverURL, sessionID, profileName)
}
