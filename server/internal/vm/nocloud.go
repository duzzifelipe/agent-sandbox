package vm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kdomanski/iso9660"
)

// WriteNoCloudISO creates a NoCloud data source ISO at dir/nocloud.iso.
// The ISO has volume label "cidata" and contains meta-data and user-data files.
// Returns the absolute path to the generated ISO.
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

// NoCloudUserData returns cloud-init user-data that installs an SSH authorized key.
func NoCloudUserData(authorizedKey string) string {
	return fmt.Sprintf("#cloud-config\nssh_authorized_keys:\n  - %s\n", authorizedKey)
}
