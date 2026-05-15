package state

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type State struct {
	path string
}

func New(path string) *State {
	return &State{path: path}
}

func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agentsdx", "sessions.json")
}

func (s *State) Set(profile, sessionID string) error {
	m := s.load()
	m[profile] = sessionID
	return s.save(m)
}

func (s *State) Get(profile string) (string, bool) {
	m := s.load()
	id, ok := m[profile]
	return id, ok
}

func (s *State) Delete(profile string) {
	m := s.load()
	delete(m, profile)
	_ = s.save(m)
}

func (s *State) load() map[string]string {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return map[string]string{}
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]string{}
	}
	return m
}

func (s *State) save(m map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}
