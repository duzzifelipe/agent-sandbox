package vm_test

import (
	"context"
	"net"
	"strconv"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/vm"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// --- fake hcloud clients ---

type fakeServerClient struct {
	createResult hcloud.ServerCreateResult
	createErr    error
	getServer    *hcloud.Server
	getErr       error
	deleted      []*hcloud.Server
	imageResult  hcloud.ServerCreateImageResult
	imageErr     error
}

func (f *fakeServerClient) Create(_ context.Context, _ hcloud.ServerCreateOpts) (hcloud.ServerCreateResult, *hcloud.Response, error) {
	return f.createResult, nil, f.createErr
}

func (f *fakeServerClient) GetByID(_ context.Context, _ int64) (*hcloud.Server, *hcloud.Response, error) {
	return f.getServer, nil, f.getErr
}

func (f *fakeServerClient) Delete(_ context.Context, s *hcloud.Server) (*hcloud.Response, error) {
	f.deleted = append(f.deleted, s)
	return nil, nil
}

func (f *fakeServerClient) CreateImage(_ context.Context, _ *hcloud.Server, _ *hcloud.ServerCreateImageOpts) (hcloud.ServerCreateImageResult, *hcloud.Response, error) {
	return f.imageResult, nil, f.imageErr
}

type fakeSSHKeyClient struct {
	created   *hcloud.SSHKey
	createErr error
	byName    *hcloud.SSHKey
	deleted   []*hcloud.SSHKey
}

func (f *fakeSSHKeyClient) Create(_ context.Context, opts hcloud.SSHKeyCreateOpts) (*hcloud.SSHKey, *hcloud.Response, error) {
	if f.createErr != nil {
		return nil, nil, f.createErr
	}
	f.created = &hcloud.SSHKey{ID: 1, Name: opts.Name}
	return f.created, nil, nil
}

func (f *fakeSSHKeyClient) Delete(_ context.Context, k *hcloud.SSHKey) (*hcloud.Response, error) {
	f.deleted = append(f.deleted, k)
	return nil, nil
}

func (f *fakeSSHKeyClient) GetByName(_ context.Context, name string) (*hcloud.SSHKey, *hcloud.Response, error) {
	if f.byName != nil && f.byName.Name == name {
		return f.byName, nil, nil
	}
	return nil, nil, nil
}

type fakeActionClient struct{ err error }

func (f *fakeActionClient) WaitFor(_ context.Context, _ ...*hcloud.Action) error {
	return f.err
}

func newTestProvider(servers *fakeServerClient, keys *fakeSSHKeyClient, actions *fakeActionClient) *vm.HetznerProvider {
	return vm.NewHetznerProviderFromClients(servers, keys, actions, "nbg1")
}

// --- tests ---

func TestHetznerProvider_CreateVM_ReturnsStartingWithIP(t *testing.T) {
	ip := net.ParseIP("1.2.3.4")
	servers := &fakeServerClient{
		createResult: hcloud.ServerCreateResult{
			Server: &hcloud.Server{
				ID:     42,
				Status: hcloud.ServerStatusStarting,
				PublicNet: hcloud.ServerPublicNet{
					IPv4: hcloud.ServerPublicNetIPv4{IP: ip},
				},
				Labels: map[string]string{},
			},
			Action: &hcloud.Action{},
		},
	}
	p := newTestProvider(servers, &fakeSSHKeyClient{}, &fakeActionClient{})

	v, err := p.CreateVM(context.Background(), vm.CreateVMRequest{
		ProfileName:   "dev",
		ImageID:       "99",
		AuthorizedKey: "ssh-ed25519 AAAA",
		UserData:      "#cloud-config\n",
	})

	if err != nil {
		t.Fatalf("CreateVM: %v", err)
	}
	if v.ID != "42" {
		t.Errorf("ID: got %q, want %q", v.ID, "42")
	}
	if v.IPAddress != "1.2.3.4" {
		t.Errorf("IPAddress: got %q, want %q", v.IPAddress, "1.2.3.4")
	}
	if v.State != vm.VMStateStarting {
		t.Errorf("State: got %q, want %q", v.State, vm.VMStateStarting)
	}
}

func TestHetznerProvider_GetVM_MapsStatus(t *testing.T) {
	ip := net.ParseIP("5.6.7.8")
	servers := &fakeServerClient{
		getServer: &hcloud.Server{
			ID:     10,
			Status: hcloud.ServerStatusRunning,
			PublicNet: hcloud.ServerPublicNet{
				IPv4: hcloud.ServerPublicNetIPv4{IP: ip},
			},
		},
	}
	p := newTestProvider(servers, &fakeSSHKeyClient{}, &fakeActionClient{})

	v, err := p.GetVM(context.Background(), "10")
	if err != nil {
		t.Fatalf("GetVM: %v", err)
	}
	if v.State != vm.VMStateRunning {
		t.Errorf("State: got %q, want %q", v.State, vm.VMStateRunning)
	}
	if v.IPAddress != "5.6.7.8" {
		t.Errorf("IPAddress: got %q, want %q", v.IPAddress, "5.6.7.8")
	}
}

func TestHetznerProvider_GetVM_UnknownID_ReturnsUnknown(t *testing.T) {
	p := newTestProvider(&fakeServerClient{}, &fakeSSHKeyClient{}, &fakeActionClient{})

	v, err := p.GetVM(context.Background(), "not-a-number")
	if err != nil {
		t.Fatalf("GetVM: %v", err)
	}
	if v.State != vm.VMStateUnknown {
		t.Errorf("State: got %q, want %q", v.State, vm.VMStateUnknown)
	}
}

func TestHetznerProvider_DestroyVM_DeletesServerAndKey(t *testing.T) {
	servers := &fakeServerClient{
		getServer: &hcloud.Server{
			ID:     7,
			Labels: map[string]string{"agentsdx-sshkey": "agentsdx-session-7"},
		},
	}
	keys := &fakeSSHKeyClient{
		byName: &hcloud.SSHKey{ID: 3, Name: "agentsdx-session-7"},
	}
	p := newTestProvider(servers, keys, &fakeActionClient{})

	if err := p.DestroyVM(context.Background(), "7"); err != nil {
		t.Fatalf("DestroyVM: %v", err)
	}
	if len(servers.deleted) != 1 {
		t.Errorf("expected 1 server deleted, got %d", len(servers.deleted))
	}
	if len(keys.deleted) != 1 {
		t.Errorf("expected 1 ssh key deleted, got %d", len(keys.deleted))
	}
}

func TestHetznerProvider_CreateBuildVM_ReturnsRunningVM(t *testing.T) {
	ip := net.ParseIP("9.9.9.9")
	servers := &fakeServerClient{
		createResult: hcloud.ServerCreateResult{
			Server: &hcloud.Server{
				ID:     55,
				Status: hcloud.ServerStatusRunning,
				PublicNet: hcloud.ServerPublicNet{
					IPv4: hcloud.ServerPublicNetIPv4{IP: ip},
				},
				Labels: map[string]string{},
			},
			Action: &hcloud.Action{},
		},
	}
	p := newTestProvider(servers, &fakeSSHKeyClient{}, &fakeActionClient{})

	v, err := p.CreateBuildVM(context.Background(), "ubuntu-24.04", "ssh-ed25519 AAAA")
	if err != nil {
		t.Fatalf("CreateBuildVM: %v", err)
	}
	if v.IPAddress != "9.9.9.9" {
		t.Errorf("IPAddress: got %q, want %q", v.IPAddress, "9.9.9.9")
	}
	if v.State != vm.VMStateRunning {
		t.Errorf("State: got %q, want %q", v.State, vm.VMStateRunning)
	}
}

func TestHetznerProvider_SnapshotVM_ReturnsImageID(t *testing.T) {
	imageID := int64(777)
	servers := &fakeServerClient{
		imageResult: hcloud.ServerCreateImageResult{
			Image:  &hcloud.Image{ID: imageID},
			Action: &hcloud.Action{},
		},
	}
	p := newTestProvider(servers, &fakeSSHKeyClient{}, &fakeActionClient{})

	got, err := p.SnapshotVM(context.Background(), "55", "my-profile")
	if err != nil {
		t.Fatalf("SnapshotVM: %v", err)
	}
	if got != strconv.FormatInt(imageID, 10) {
		t.Errorf("snapshotID: got %q, want %q", got, strconv.FormatInt(imageID, 10))
	}
}
