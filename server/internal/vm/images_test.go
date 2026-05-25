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
		"profile-a": {vm.ProviderHetzner: "snap-a"},
		"profile-b": {vm.ProviderHetzner: "snap-b", vm.ProviderLocal: "local-b"},
	})
	store := vm.NewImageStore(filepath.Join(dir, "images.json"))
	entries, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	type entry struct{ hetzner, local string }
	seen := make(map[string]entry)
	for _, e := range entries {
		seen[e.ProfileName] = entry{hetzner: e.Hetzner, local: e.Local}
	}
	if seen["profile-a"].hetzner != "snap-a" {
		t.Errorf("profile-a hetzner: got %q", seen["profile-a"].hetzner)
	}
	if seen["profile-a"].local != "" {
		t.Errorf("profile-a local: expected empty, got %q", seen["profile-a"].local)
	}
	if seen["profile-b"].hetzner != "snap-b" {
		t.Errorf("profile-b hetzner: got %q", seen["profile-b"].hetzner)
	}
	if seen["profile-b"].local != "local-b" {
		t.Errorf("profile-b local: got %q", seen["profile-b"].local)
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

func TestImageStore_SetAndGetImageID_Hetzner(t *testing.T) {
	dir := t.TempDir()
	store := vm.NewImageStore(filepath.Join(dir, "images.json"))

	if err := store.SetImageID(vm.ProviderHetzner, "my-profile", "hetz-123"); err != nil {
		t.Fatalf("SetImageID: %v", err)
	}

	got, err := store.GetImageID(vm.ProviderHetzner, "my-profile")
	if err != nil {
		t.Fatalf("GetImageID: %v", err)
	}
	if got != "hetz-123" {
		t.Errorf("got %q, want %q", got, "hetz-123")
	}
}

func TestImageStore_SetAndGetImageID_Local(t *testing.T) {
	dir := t.TempDir()
	store := vm.NewImageStore(filepath.Join(dir, "images.json"))

	if err := store.SetImageID(vm.ProviderLocal, "my-profile", "local-img-456"); err != nil {
		t.Fatalf("SetImageID: %v", err)
	}

	got, err := store.GetImageID(vm.ProviderLocal, "my-profile")
	if err != nil {
		t.Fatalf("GetImageID: %v", err)
	}
	if got != "local-img-456" {
		t.Errorf("got %q, want %q", got, "local-img-456")
	}
}

func TestImageStore_GetImageID_NotFound(t *testing.T) {
	dir := t.TempDir()
	store := vm.NewImageStore(filepath.Join(dir, "images.json"))

	_, err := store.GetImageID(vm.ProviderLocal, "missing")
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
}

func TestImageStore_GetImageID_Empty(t *testing.T) {
	dir := t.TempDir()
	writeImagesJSON(t, dir, map[string]vm.ImageRecord{
		"no-image": {vm.ProviderLocal: ""},
	})
	store := vm.NewImageStore(filepath.Join(dir, "images.json"))

	_, err := store.GetImageID(vm.ProviderLocal, "no-image")
	if err == nil {
		t.Fatal("expected error for empty image id")
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
