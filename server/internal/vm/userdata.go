package vm

import (
	"encoding/base64"
	"fmt"
)

// BuildUserData returns cloud-init user-data that injects SSH keys, agent env,
// and a callback to report the VM IP to the server on first boot.
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
runcmd:
  - IP=$(ip route get 1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src") print $(i+1)}' | head -1)
  - curl -sf -X POST "%s/sessions/%s/ready" -H "Content-Type: application/json" -d "{\"ip_address\":\"$IP\"}" || true
`, authorizedKey, encodedKey, serverURL, sessionID, profileName, serverURL, sessionID)
}
