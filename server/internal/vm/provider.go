// Package vm defines the VMProvider interface and supporting types.
package vm

import "context"

type VMProvider interface {
	CreateVM(ctx context.Context, req CreateVMRequest) (*VM, error)
	DestroyVM(ctx context.Context, vmID string) error
	GetVM(ctx context.Context, vmID string) (*VM, error)
	RegisterIP(ctx context.Context, vmID, ip string) error
}

type CreateVMRequest struct {
	ProfileName   string
	AuthorizedKey string // VM access public key placed in authorized_keys
	UserData      string // cloud-init user-data content
}

type VM struct {
	ID        string
	IPAddress string
	State     string // "starting" | "running" | "stopped" | "unknown"
}

const (
	VMStateStarting = "starting"
	VMStateRunning  = "running"
	VMStateStopped  = "stopped"
	VMStateUnknown  = "unknown"
)
