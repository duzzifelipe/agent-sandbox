package vm_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/vm"
)

func writeImagesJSON(t *testing.T, dir string, data map[string]vm.ImageRecord) string {
	t.Helper()
	path := filepath.Join(dir, "images.json")
	b, _ := json.Marshal(data)
	_ = os.WriteFile(path, b, 0644)
	return path
}

func TestImageStore_GetQEMUPath_Found(t *testing.T) {
	dir := t.TempDir()
	writeImagesJSON(t, dir, map[string]vm.ImageRecord{
		"my-profile": {vm.ProviderQEMU: "/data/images/my-profile.qcow2"},
	})

	store := vm.NewImageStore(filepath.Join(dir, "images.json"))
	path, err := store.GetQEMUPath("my-profile")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/data/images/my-profile.qcow2" {
		t.Errorf("got %q, want %q", path, "/data/images/my-profile.qcow2")
	}
}

func TestImageStore_GetQEMUPath_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeImagesJSON(t, dir, map[string]vm.ImageRecord{})

	store := vm.NewImageStore(filepath.Join(dir, "images.json"))
	_, err := store.GetQEMUPath("missing")
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
}

func TestImageStore_GetQEMUPath_EmptyPath(t *testing.T) {
	dir := t.TempDir()
	writeImagesJSON(t, dir, map[string]vm.ImageRecord{
		"no-image": {vm.ProviderQEMU: ""},
	})

	store := vm.NewImageStore(filepath.Join(dir, "images.json"))
	_, err := store.GetQEMUPath("no-image")
	if err == nil {
		t.Fatal("expected error for empty qemu path")
	}
}

func TestImageStore_SetQEMUPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "images.json")
	store := vm.NewImageStore(path)

	if err := store.SetQEMUPath("my-profile", "/data/images/my-profile.qcow2"); err != nil {
		t.Fatalf("SetQEMUPath: %v", err)
	}

	got, err := store.GetQEMUPath("my-profile")
	if err != nil {
		t.Fatalf("GetQEMUPath after set: %v", err)
	}
	if got != "/data/images/my-profile.qcow2" {
		t.Errorf("got %q, want %q", got, "/data/images/my-profile.qcow2")
	}
}

func TestImageStore_List_Empty(t *testing.T) {
	dir := t.TempDir()
	store := vm.NewImageStore(filepath.Join(dir, "images.json"))
	entries, err := store.List()
	if err != nil {
		t.Fatalf("List on missing file: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestImageStore_List_ReturnsAll(t *testing.T) {
	dir := t.TempDir()
	writeImagesJSON(t, dir, map[string]vm.ImageRecord{
		"profile-a": {vm.ProviderQEMU: "/data/images/a.qcow2"},
		"profile-b": {vm.ProviderQEMU: "/data/images/b.qcow2"},
	})
	store := vm.NewImageStore(filepath.Join(dir, "images.json"))
	entries, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	seen := make(map[string]string)
	for _, e := range entries {
		seen[e.ProfileName] = e.QEMU
	}
	if seen["profile-a"] != "/data/images/a.qcow2" {
		t.Errorf("profile-a: got %q", seen["profile-a"])
	}
	if seen["profile-b"] != "/data/images/b.qcow2" {
		t.Errorf("profile-b: got %q", seen["profile-b"])
	}
}

func TestImageStore_SetAndGetHetznerSnapshotID(t *testing.T) {
	dir := t.TempDir()
	store := vm.NewImageStore(filepath.Join(dir, "images.json"))

	if err := store.SetHetznerSnapshotID("my-profile", "98765"); err != nil {
		t.Fatalf("SetHetznerSnapshotID: %v", err)
	}

	got, err := store.GetHetznerSnapshotID("my-profile")
	if err != nil {
		t.Fatalf("GetHetznerSnapshotID: %v", err)
	}
	if got != "98765" {
		t.Errorf("got %q, want %q", got, "98765")
	}
}

func TestImageStore_GetHetznerSnapshotID_NotFound(t *testing.T) {
	dir := t.TempDir()
	store := vm.NewImageStore(filepath.Join(dir, "images.json"))

	_, err := store.GetHetznerSnapshotID("missing")
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
}

func TestImageStore_GetHetznerSnapshotID_Empty(t *testing.T) {
	dir := t.TempDir()
	writeImagesJSON(t, dir, map[string]vm.ImageRecord{
		"no-snapshot": {vm.ProviderHetzner: ""},
	})
	store := vm.NewImageStore(filepath.Join(dir, "images.json"))

	_, err := store.GetHetznerSnapshotID("no-snapshot")
	if err == nil {
		t.Fatal("expected error for empty snapshot id")
	}
}
