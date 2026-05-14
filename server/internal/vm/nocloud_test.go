package vm_test

import (
	"os"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/vm"
	"github.com/kdomanski/iso9660"
)

func TestWriteNoCloudISO_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	metaData := "instance-id: test\nlocal-hostname: test-vm\n"
	userData := "#cloud-config\nssh_authorized_keys:\n  - ssh-rsa AAAA...\n"

	isoPath, err := vm.WriteNoCloudISO(dir, metaData, userData)
	if err != nil {
		t.Fatalf("WriteNoCloudISO: %v", err)
	}

	if _, err := os.Stat(isoPath); err != nil {
		t.Fatalf("ISO file not found: %v", err)
	}
}

func TestWriteNoCloudISO_ContainsFiles(t *testing.T) {
	dir := t.TempDir()
	metaData := "instance-id: abc\nlocal-hostname: my-vm\n"
	userData := "#cloud-config\npackages:\n  - git\n"

	isoPath, err := vm.WriteNoCloudISO(dir, metaData, userData)
	if err != nil {
		t.Fatalf("WriteNoCloudISO: %v", err)
	}

	f, err := os.Open(isoPath)
	if err != nil {
		t.Fatalf("open ISO: %v", err)
	}
	defer f.Close()

	img, err := iso9660.OpenImage(f)
	if err != nil {
		t.Fatalf("open iso9660 image: %v", err)
	}

	root, err := img.RootDir()
	if err != nil {
		t.Fatalf("root dir: %v", err)
	}
	children, err := root.GetChildren()
	if err != nil {
		t.Fatalf("get children: %v", err)
	}

	names := make(map[string]bool)
	for _, c := range children {
		names[c.Name()] = true
	}
	for _, want := range []string{"meta-data", "user-data"} {
		if !names[want] {
			t.Errorf("ISO missing file %q; got names: %v", want, names)
		}
	}
}
