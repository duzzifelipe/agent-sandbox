package vm

import (
	"encoding/json"
	"fmt"
	"os"
)

type Provider string

const (
	ProviderLocal Provider = "local"
)

type ImageRecord map[Provider]string

type ImageStore struct {
	path string
}

func NewImageStore(path string) *ImageStore {
	return &ImageStore{path: path}
}

func (s *ImageStore) GetImageID(provider Provider, profileName string) (string, error) {
	records, err := s.load()
	if os.IsNotExist(err) {
		return "", fmt.Errorf("no image built for profile %q: run 'profiles build' first", profileName)
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

func (s *ImageStore) DeleteImage(profileName string) error {
	records, err := s.load()
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load images: %w", err)
	}
	rec, ok := records[profileName]
	if !ok {
		return nil
	}
	for _, snapshotPath := range rec {
		os.Remove(snapshotPath)
	}
	delete(records, profileName)
	return s.save(records)
}

func (s *ImageStore) save(records map[string]ImageRecord) error {
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal images: %w", err)
	}
	return os.WriteFile(s.path, data, 0644)
}
