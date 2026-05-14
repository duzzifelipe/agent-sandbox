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

func TestImageStore_GetVirtualBoxPath_Found(t *testing.T) {
	dir := t.TempDir()
	writeImagesJSON(t, dir, map[string]vm.ImageRecord{
		"my-profile": {"virtualbox": "/data/images/my-profile.ova"},
	})

	store := vm.NewImageStore(filepath.Join(dir, "images.json"))
	path, err := store.GetVirtualBoxPath("my-profile")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/data/images/my-profile.ova" {
		t.Errorf("got %q, want %q", path, "/data/images/my-profile.ova")
	}
}

func TestImageStore_GetVirtualBoxPath_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeImagesJSON(t, dir, map[string]vm.ImageRecord{})

	store := vm.NewImageStore(filepath.Join(dir, "images.json"))
	_, err := store.GetVirtualBoxPath("missing")
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
}

func TestImageStore_GetVirtualBoxPath_EmptyPath(t *testing.T) {
	dir := t.TempDir()
	writeImagesJSON(t, dir, map[string]vm.ImageRecord{
		"no-image": {"virtualbox": ""},
	})

	store := vm.NewImageStore(filepath.Join(dir, "images.json"))
	_, err := store.GetVirtualBoxPath("no-image")
	if err == nil {
		t.Fatal("expected error for empty virtualbox path")
	}
}

func TestImageStore_SetVirtualBoxPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "images.json")
	store := vm.NewImageStore(path)

	if err := store.SetVirtualBoxPath("my-profile", "/data/images/my-profile.ova"); err != nil {
		t.Fatalf("SetVirtualBoxPath: %v", err)
	}

	got, err := store.GetVirtualBoxPath("my-profile")
	if err != nil {
		t.Fatalf("GetVirtualBoxPath after set: %v", err)
	}
	if got != "/data/images/my-profile.ova" {
		t.Errorf("got %q, want %q", got, "/data/images/my-profile.ova")
	}
}
