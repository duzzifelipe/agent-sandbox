package vm

import (
	"encoding/json"
	"fmt"
	"os"
)

// Provider identifies a VM backend.
type Provider string

const (
	ProviderHetzner Provider = "hetzner"
	ProviderLocal   Provider = "local"
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

// GetImageID returns the image ID for the given provider and profileName.
func (s *ImageStore) GetImageID(provider Provider, profileName string) (string, error) {
	records, err := s.load()
	if os.IsNotExist(err) {
		return "", fmt.Errorf("no image built for profile %q: run 'images build' first", profileName)
	}
	if err != nil {
		return "", fmt.Errorf("load images: %w", err)
	}
	rec, ok := records[profileName]
	if !ok {
		return "", fmt.Errorf("no image record for profile %q", profileName)
	}
	id := rec[provider]
	if id == "" {
		return "", fmt.Errorf("no %s image built for profile %q", provider, profileName)
	}
	return id, nil
}

// SetImageID writes or updates the image ID for the given provider and profileName.
func (s *ImageStore) SetImageID(provider Provider, profileName, id string) error {
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
	rec[provider] = id
	records[profileName] = rec
	return s.save(records)
}

// GetHetznerSnapshotID returns the Hetzner snapshot ID for profileName.
func (s *ImageStore) GetHetznerSnapshotID(profileName string) (string, error) {
	return s.GetImageID(ProviderHetzner, profileName)
}

// SetHetznerSnapshotID writes or updates the Hetzner snapshot ID for profileName.
func (s *ImageStore) SetHetznerSnapshotID(profileName, snapshotID string) error {
	return s.SetImageID(ProviderHetzner, profileName, snapshotID)
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
