package types_test

import (
	"encoding/json"
	"testing"

	"github.com/duck-labs/agentsdx-shared/types"
)

func TestCreateSessionRequest_JSON(t *testing.T) {
	req := types.CreateSessionRequest{ProfileName: "work-backend"}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got types.CreateSessionRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ProfileName != "work-backend" {
		t.Errorf("ProfileName: got %q, want %q", got.ProfileName, "work-backend")
	}
}

func TestSessionResponse_JSON(t *testing.T) {
	resp := types.SessionResponse{
		ID:        "abc123",
		Profile:   "work-backend",
		State:     types.SessionStateRunning,
		IPAddress: "192.168.56.10",
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got types.SessionResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != "abc123" {
		t.Errorf("ID: got %q, want %q", got.ID, "abc123")
	}
	if got.State != types.SessionStateRunning {
		t.Errorf("State: got %q, want %q", got.State, types.SessionStateRunning)
	}
	if got.IPAddress != "192.168.56.10" {
		t.Errorf("IPAddress: got %q, want %q", got.IPAddress, "192.168.56.10")
	}
}

func TestSessionStates_Values(t *testing.T) {
	states := []string{
		types.SessionStatePending,
		types.SessionStateStarting,
		types.SessionStateRunning,
		types.SessionStateStopping,
		types.SessionStateDestroyed,
	}
	for _, s := range states {
		if s == "" {
			t.Errorf("session state constant is empty string")
		}
	}
}

func TestBuildImageRequest_JSON(t *testing.T) {
	req := types.BuildImageRequest{ProfileName: "work-backend"}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got types.BuildImageRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ProfileName != "work-backend" {
		t.Errorf("ProfileName: got %q, want %q", got.ProfileName, "work-backend")
	}
}

func TestImageEntry_JSON(t *testing.T) {
	entry := types.ImageEntry{
		ProfileName: "work-backend",
		VirtualBox:  "/data/images/work-backend.ova",
		Hetzner:     "",
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got types.ImageEntry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.VirtualBox != "/data/images/work-backend.ova" {
		t.Errorf("VirtualBox: got %q, want %q", got.VirtualBox, "/data/images/work-backend.ova")
	}
}
