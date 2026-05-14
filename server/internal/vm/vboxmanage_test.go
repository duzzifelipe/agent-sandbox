package vm_test

import (
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/vm"
)

func TestParseVMInfo_Running(t *testing.T) {
	output := `VMState="running"
VMStateChangeTime="2026-05-14T10:00:00.000000000"
name="agentsdx-abc123"
`
	info := vm.ParseVMInfo(output)
	if info["VMState"] != "running" {
		t.Errorf("VMState: got %q, want %q", info["VMState"], "running")
	}
	if info["name"] != "agentsdx-abc123" {
		t.Errorf("name: got %q, want %q", info["name"], "agentsdx-abc123")
	}
}

func TestParseVMInfo_PoweredOff(t *testing.T) {
	output := `VMState="poweroff"
name="agentsdx-xyz"
`
	info := vm.ParseVMInfo(output)
	if info["VMState"] != "poweroff" {
		t.Errorf("VMState: got %q, want %q", info["VMState"], "poweroff")
	}
}

func TestParseGuestProperty_Found(t *testing.T) {
	output := "Value: 192.168.56.101\n"
	val, ok := vm.ParseGuestProperty(output)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if val != "192.168.56.101" {
		t.Errorf("got %q, want %q", val, "192.168.56.101")
	}
}

func TestParseGuestProperty_NoValue(t *testing.T) {
	output := "No value set!\n"
	_, ok := vm.ParseGuestProperty(output)
	if ok {
		t.Fatal("expected ok=false for no value")
	}
}
