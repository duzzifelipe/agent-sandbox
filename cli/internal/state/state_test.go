package state_test

import (
	"path/filepath"
	"testing"

	"github.com/duck-labs/agentsdx-cli/internal/state"
)

func TestSetAndGet(t *testing.T) {
	dir := t.TempDir()
	s := state.New(filepath.Join(dir, "sessions.json"))

	if err := s.Set("myprofile", "session-abc"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	id, ok := s.Get("myprofile")
	if !ok || id != "session-abc" {
		t.Fatalf("Get: got %q, %v; want session-abc, true", id, ok)
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	s := state.New(filepath.Join(dir, "sessions.json"))

	_ = s.Set("myprofile", "session-abc")
	s.Delete("myprofile")

	_, ok := s.Get("myprofile")
	if ok {
		t.Fatal("expected profile to be deleted")
	}
}

func TestGetMissing(t *testing.T) {
	dir := t.TempDir()
	s := state.New(filepath.Join(dir, "sessions.json"))

	_, ok := s.Get("nonexistent")
	if ok {
		t.Fatal("expected false for missing profile")
	}
}

func TestPersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")

	s1 := state.New(path)
	_ = s1.Set("myprofile", "session-xyz")

	s2 := state.New(path)
	id, ok := s2.Get("myprofile")
	if !ok || id != "session-xyz" {
		t.Fatalf("got %q, %v; want session-xyz, true", id, ok)
	}
}
