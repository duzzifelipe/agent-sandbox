// Package vm defines the VMProvider and ImageProvider interfaces and their supporting types.
package vm

import "context"

// VMProvider manages session VM lifecycle.
type VMProvider interface {
	CreateVM(ctx context.Context, req CreateVMRequest) (*VM, error)
	DestroyVM(ctx context.Context, vmID string) error
	GetVM(ctx context.Context, vmID string) (*VM, error)
}

// ImageProvider manages image build VM lifecycle.
// CreateBuildVM blocks until the server is running and ready for SSH.
type ImageProvider interface {
	CreateBuildVM(ctx context.Context, baseImage, authorizedKey string) (*VM, error)
	SnapshotVM(ctx context.Context, vmID, snapshotName string) (string, error)
	DestroyBuildVM(ctx context.Context, vmID string) error
}

// CreateVMRequest is passed to VMProvider.CreateVM.
type CreateVMRequest struct {
	ProfileName   string
	ImageID       string // snapshot ID or base image name
	AuthorizedKey string // public key placed in authorized_keys
	UserData      string // cloud-init user-data
}

// VM is returned by VMProvider methods.
type VM struct {
	ID        string
	IPAddress string
	SSHPort   int // SSHPort is the forwarded SSH port; 0 means use port 22 (direct access).
	State     string
}

const (
	VMStateStarting = "starting"
	VMStateRunning  = "running"
	VMStateStopped  = "stopped"
	VMStateUnknown  = "unknown"
)
