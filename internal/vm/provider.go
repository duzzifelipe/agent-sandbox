package vm

import "context"

type VMProvider interface {
	CreateVM(ctx context.Context, req CreateVMRequest) (*VM, error)
	DestroyVM(ctx context.Context, vmID string) error
	GetVM(ctx context.Context, vmID string) (*VM, error)
}

type ImageProvider interface {
	CreateBuildVM(ctx context.Context, baseImage, authorizedKey string) (*VM, error)
	SnapshotVM(ctx context.Context, vmID, snapshotName string) (string, error)
	DestroyBuildVM(ctx context.Context, vmID string) error
}

type CreateVMRequest struct {
	ProfileName   string
	ImageID       string
	AuthorizedKey string
	UserData      string
}

type VM struct {
	ID        string
	IPAddress string
	SSHPort   int
	State     string
}

const (
	VMStateStarting = "starting"
	VMStateRunning  = "running"
	VMStateStopped  = "stopped"
	VMStateUnknown  = "unknown"
)
