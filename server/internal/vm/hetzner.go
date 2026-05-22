package vm

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

const hetznerServerType = "cx22"

// hcloudServerOps is a narrow interface over hcloud.ServerClient for testing.
type hcloudServerOps interface {
	Create(ctx context.Context, opts hcloud.ServerCreateOpts) (hcloud.ServerCreateResult, *hcloud.Response, error)
	GetByID(ctx context.Context, id int64) (*hcloud.Server, *hcloud.Response, error)
	Delete(ctx context.Context, server *hcloud.Server) (*hcloud.Response, error)
	CreateImage(ctx context.Context, server *hcloud.Server, opts *hcloud.ServerCreateImageOpts) (hcloud.ServerCreateImageResult, *hcloud.Response, error)
}

// hcloudSSHKeyOps is a narrow interface over hcloud.SSHKeyClient for testing.
type hcloudSSHKeyOps interface {
	Create(ctx context.Context, opts hcloud.SSHKeyCreateOpts) (*hcloud.SSHKey, *hcloud.Response, error)
	Delete(ctx context.Context, sshKey *hcloud.SSHKey) (*hcloud.Response, error)
	GetByName(ctx context.Context, name string) (*hcloud.SSHKey, *hcloud.Response, error)
}

// hcloudActionOps is a narrow interface over hcloud.ActionClient for testing.
type hcloudActionOps interface {
	WaitFor(ctx context.Context, actions ...*hcloud.Action) error
}

// HetznerProvider implements VMProvider and ImageProvider using Hetzner Cloud.
type HetznerProvider struct {
	servers  hcloudServerOps
	sshKeys  hcloudSSHKeyOps
	actions  hcloudActionOps
	location string
}

// NewHetznerProvider creates a HetznerProvider from a hcloud.Client.
func NewHetznerProvider(client *hcloud.Client, location string) *HetznerProvider {
	if location == "" {
		location = "nbg1"
	}
	return &HetznerProvider{
		servers:  &client.Server,
		sshKeys:  &client.SSHKey,
		actions:  &client.Action,
		location: location,
	}
}

// NewHetznerProviderFromClients creates a HetznerProvider from narrow interfaces (for testing).
func NewHetznerProviderFromClients(servers hcloudServerOps, sshKeys hcloudSSHKeyOps, actions hcloudActionOps, location string) *HetznerProvider {
	return &HetznerProvider{servers: servers, sshKeys: sshKeys, actions: actions, location: location}
}

// CreateVM creates a session server from the snapshot identified by req.ImageID.
func (p *HetznerProvider) CreateVM(ctx context.Context, req CreateVMRequest) (*VM, error) {
	keyName := fmt.Sprintf("agentsdx-session-%d", time.Now().UnixMilli())
	sshKey, _, err := p.sshKeys.Create(ctx, hcloud.SSHKeyCreateOpts{
		Name:      keyName,
		PublicKey: req.AuthorizedKey,
	})
	if err != nil {
		return nil, fmt.Errorf("create ssh key: %w", err)
	}

	result, _, err := p.servers.Create(ctx, hcloud.ServerCreateOpts{
		Name:       fmt.Sprintf("agentsdx-session-%d", time.Now().UnixMilli()),
		ServerType: &hcloud.ServerType{Name: hetznerServerType},
		Image:      imageRef(req.ImageID),
		Location:   &hcloud.Location{Name: p.location},
		SSHKeys:    []*hcloud.SSHKey{sshKey},
		UserData:   req.UserData,
		Labels:     map[string]string{"agentsdx-type": "session", "agentsdx-sshkey": keyName},
	})
	if err != nil {
		_, _ = p.sshKeys.Delete(ctx, sshKey) //nolint:errcheck
		return nil, fmt.Errorf("create server: %w", err)
	}

	return &VM{
		ID:        strconv.FormatInt(result.Server.ID, 10),
		IPAddress: result.Server.PublicNet.IPv4.IP.String(),
		State:     VMStateStarting,
	}, nil
}

// GetVM fetches server status and maps it to VM state.
func (p *HetznerProvider) GetVM(ctx context.Context, vmID string) (*VM, error) {
	id, err := strconv.ParseInt(vmID, 10, 64)
	if err != nil {
		return &VM{ID: vmID, State: VMStateUnknown}, nil
	}
	server, _, err := p.servers.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get server: %w", err)
	}
	if server == nil {
		return &VM{ID: vmID, State: VMStateUnknown}, nil
	}

	state := VMStateStarting
	switch server.Status {
	case hcloud.ServerStatusRunning:
		state = VMStateRunning
	case hcloud.ServerStatusOff, hcloud.ServerStatusDeleting:
		state = VMStateStopped
	}

	ip := ""
	if server.PublicNet.IPv4.IP != nil {
		ip = server.PublicNet.IPv4.IP.String()
	}
	return &VM{ID: vmID, IPAddress: ip, State: state}, nil
}

// DestroyVM deletes the server and its associated SSH key.
func (p *HetznerProvider) DestroyVM(ctx context.Context, vmID string) error {
	return p.deleteServerAndKey(ctx, vmID)
}

// CreateBuildVM creates a temporary server for image provisioning.
// Blocks until hcloud reports the server as started.
func (p *HetznerProvider) CreateBuildVM(ctx context.Context, baseImage, authorizedKey string) (*VM, error) {
	keyName := fmt.Sprintf("agentsdx-build-%d", time.Now().UnixMilli())
	sshKey, _, err := p.sshKeys.Create(ctx, hcloud.SSHKeyCreateOpts{
		Name:      keyName,
		PublicKey: authorizedKey,
	})
	if err != nil {
		return nil, fmt.Errorf("create ssh key: %w", err)
	}

	result, _, err := p.servers.Create(ctx, hcloud.ServerCreateOpts{
		Name:       fmt.Sprintf("agentsdx-build-%d", time.Now().UnixMilli()),
		ServerType: &hcloud.ServerType{Name: hetznerServerType},
		Image:      imageRef(baseImage),
		Location:   &hcloud.Location{Name: p.location},
		SSHKeys:    []*hcloud.SSHKey{sshKey},
		Labels:     map[string]string{"agentsdx-type": "build", "agentsdx-sshkey": keyName},
	})
	if err != nil {
		_, _ = p.sshKeys.Delete(ctx, sshKey)
		return nil, fmt.Errorf("create build server: %w", err)
	}

	allActions := []*hcloud.Action{result.Action}
	allActions = append(allActions, result.NextActions...)
	if err := p.actions.WaitFor(ctx, allActions...); err != nil {
		_, _ = p.servers.Delete(ctx, result.Server)
		_, _ = p.sshKeys.Delete(ctx, sshKey)
		return nil, fmt.Errorf("wait for build server: %w", err)
	}

	return &VM{
		ID:        strconv.FormatInt(result.Server.ID, 10),
		IPAddress: result.Server.PublicNet.IPv4.IP.String(),
		State:     VMStateRunning,
	}, nil
}

// SnapshotVM takes a snapshot of the server and returns the snapshot image ID.
func (p *HetznerProvider) SnapshotVM(ctx context.Context, vmID, snapshotName string) (string, error) {
	id, err := strconv.ParseInt(vmID, 10, 64)
	if err != nil {
		return "", fmt.Errorf("parse vm id: %w", err)
	}
	desc := snapshotName
	result, _, err := p.servers.CreateImage(ctx, &hcloud.Server{ID: id}, &hcloud.ServerCreateImageOpts{
		Type:        hcloud.ImageTypeSnapshot,
		Description: &desc,
		Labels:      map[string]string{"agentsdx-profile": snapshotName},
	})
	if err != nil {
		return "", fmt.Errorf("create snapshot: %w", err)
	}
	if err := p.actions.WaitFor(ctx, result.Action); err != nil {
		return "", fmt.Errorf("wait for snapshot: %w", err)
	}
	return strconv.FormatInt(result.Image.ID, 10), nil
}

// DestroyBuildVM deletes a build server and its SSH key.
func (p *HetznerProvider) DestroyBuildVM(ctx context.Context, vmID string) error {
	return p.deleteServerAndKey(ctx, vmID)
}

func (p *HetznerProvider) deleteServerAndKey(ctx context.Context, vmID string) error {
	id, err := strconv.ParseInt(vmID, 10, 64)
	if err != nil {
		return nil
	}
	server, _, err := p.servers.GetByID(ctx, id)
	if err != nil || server == nil {
		return nil
	}
	sshKeyName := server.Labels["agentsdx-sshkey"]
	_, _ = p.servers.Delete(ctx, server)
	if sshKeyName != "" {
		if key, _, _ := p.sshKeys.GetByName(ctx, sshKeyName); key != nil {
			_, _ = p.sshKeys.Delete(ctx, key)
		}
	}
	return nil
}

// imageRef builds an hcloud.Image reference from an ID string (numeric = snapshot)
// or name string (e.g. "ubuntu-24.04" = public image).
func imageRef(imageID string) *hcloud.Image {
	if id, err := strconv.ParseInt(imageID, 10, 64); err == nil {
		return &hcloud.Image{ID: id}
	}
	return &hcloud.Image{Name: imageID}
}
