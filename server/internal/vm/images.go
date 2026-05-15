package vm

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/duck-labs/agentsdx-shared/types"
)

// Provider identifies a VM backend.
type Provider string

const (
	ProviderVirtualBox Provider = "virtualbox"
	ProviderHetzner    Provider = "hetzner"
)

// ImageRecord maps a Provider to its built image path for a single profile.
type ImageRecord map[Provider]string

// ImageStore reads and writes images.json.
type ImageStore struct {
	path string
}

// NewImageStore creates an ImageStore backed by the given file path.
func NewImageStore(path string) *ImageStore {
	return &ImageStore{path: path}
}

// GetVirtualBoxPath returns the OVA path for profileName or an error if absent.
func (s *ImageStore) GetVirtualBoxPath(profileName string) (string, error) {
	records, err := s.load()
	if err != nil {
		return "", fmt.Errorf("load images: %w", err)
	}
	rec, ok := records[profileName]
	if !ok {
		return "", fmt.Errorf("no image record for profile %q", profileName)
	}
	p := rec[ProviderVirtualBox]
	if p == "" {
		return "", fmt.Errorf("no virtualbox image built for profile %q", profileName)
	}
	return p, nil
}

// SetVirtualBoxPath writes or updates the VirtualBox OVA path for profileName.
func (s *ImageStore) SetVirtualBoxPath(profileName, ovaPath string) error {
	records, err := s.load()
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("load images: %w", err)
	}
	if records == nil {
		records = make(map[string]ImageRecord)
	}
	rec := records[profileName]
	if rec == nil {
		rec = make(ImageRecord)
	}
	rec[ProviderVirtualBox] = ovaPath
	records[profileName] = rec
	return s.save(records)
}

// List returns all image entries from images.json.
// Returns an empty slice if the file does not exist yet.
func (s *ImageStore) List() ([]types.ImageEntry, error) {
	records, err := s.load()
	if os.IsNotExist(err) {
		return []types.ImageEntry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load images: %w", err)
	}
	entries := make([]types.ImageEntry, 0, len(records))
	for profileName, rec := range records {
		entries = append(entries, types.ImageEntry{
			ProfileName: profileName,
			VirtualBox:  rec[ProviderVirtualBox],
			Hetzner:     rec[ProviderHetzner],
		})
	}
	return entries, nil
}

func (s *ImageStore) load() (map[string]ImageRecord, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	var records map[string]ImageRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("parse images.json: %w", err)
	}
	return records, nil
}

func (s *ImageStore) save(records map[string]ImageRecord) error {
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal images: %w", err)
	}
	return os.WriteFile(s.path, data, 0644)
}
