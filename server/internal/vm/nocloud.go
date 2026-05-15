package vm

import (
	"encoding/base64"
	"fmt"
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

// BuildUserData returns cloud-init user-data that:
//   - registers the VM access authorized key
//   - writes /root/.ssh/id_rsa (git private key, base64-encoded to avoid YAML issues)
//   - writes /etc/agentsdx.env with session context for entrypoint.sh
func BuildUserData(authorizedKey, gitPrivateKey, sessionID, serverURL, profileName string) string {
	encodedKey := base64.StdEncoding.EncodeToString([]byte(gitPrivateKey))
	return fmt.Sprintf(`#cloud-config
ssh_authorized_keys:
  - %s
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
