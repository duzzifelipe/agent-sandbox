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
	ProviderHetzner Provider = "hetzner"
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

// GetHetznerSnapshotID returns the Hetzner snapshot ID for profileName.
func (s *ImageStore) GetHetznerSnapshotID(profileName string) (string, error) {
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
	id := rec[ProviderHetzner]
	if id == "" {
		return "", fmt.Errorf("no hetzner snapshot built for profile %q", profileName)
	}
	return id, nil
}

// SetHetznerSnapshotID writes or updates the Hetzner snapshot ID for profileName.
func (s *ImageStore) SetHetznerSnapshotID(profileName, snapshotID string) error {
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
	rec[ProviderHetzner] = snapshotID
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
