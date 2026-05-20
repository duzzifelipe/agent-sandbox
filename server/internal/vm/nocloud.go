package vm

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/kdomanski/iso9660"
)

// WriteNoCloudISO creates a NoCloud data source ISO at dir/nocloud.iso.
func WriteNoCloudISO(dir, metaData, userData string) (string, error) {
	writer, err := iso9660.NewWriter()
	if err != nil {
		return "", fmt.Errorf("new iso writer: %w", err)
	}
	defer writer.Cleanup()

	if err := writer.AddFile(strings.NewReader(metaData), "meta-data"); err != nil {
		return "", fmt.Errorf("add meta-data: %w", err)
	}
	if err := writer.AddFile(strings.NewReader(userData), "user-data"); err != nil {
		return "", fmt.Errorf("add user-data: %w", err)
	}

	isoPath := filepath.Join(dir, "nocloud.iso")
	f, err := os.OpenFile(isoPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("create iso file: %w", err)
	}
	defer f.Close()

	if err := writer.WriteTo(f, "cidata"); err != nil {
		return "", fmt.Errorf("write iso: %w", err)
	}
	return isoPath, nil
}

// NoCloudMetaData returns minimal cloud-init meta-data for the given instance ID.
func NoCloudMetaData(instanceID string) string {
	return fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n", instanceID, instanceID)
}

// BuildUserData returns cloud-init user-data. When vmCallbackURL is non-empty,
// a runcmd block is added that POSTs the VM's IP to that URL after boot.
func BuildUserData(authorizedKey, gitPrivateKey, sessionID, serverURL, profileName, vmCallbackURL string) string {
	encodedKey := base64.StdEncoding.EncodeToString([]byte(gitPrivateKey))
	ud := fmt.Sprintf(`#cloud-config
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

	if vmCallbackURL != "" {
		// Build the callback URL using the VM's default gateway as the host.
		// This handles NAT networking (e.g. Apple VZ) where the host is reachable
		// via the gateway rather than via the server URL's literal hostname.
		u, _ := url.Parse(vmCallbackURL)
		portSuffix := ""
		if p := u.Port(); p != "" {
			portSuffix = ":" + p
		}
		callbackPath := u.Path
		ud += fmt.Sprintf(`runcmd:
  - |
    for i in $(seq 1 36); do
      GW=$(ip route show default | awk '/^default/ {print $3; exit}')
      IP=$(ip -4 addr show | awk '/inet / && !/127\./ {split($2,a,"/"); print a[1]; exit}')
      [ -n "$GW" ] && [ -n "$IP" ] && break
      sleep 5
    done
    [ -z "$GW" ] || [ -z "$IP" ] && exit 1
    curl -f --retry 5 --retry-delay 5 --retry-all-errors \
      -X POST "http://${GW}%s%s" \
      -H 'Content-Type: application/json' \
      -d "{\"ip\":\"$IP\"}"
`, portSuffix, callbackPath)
	}
	return ud
}
