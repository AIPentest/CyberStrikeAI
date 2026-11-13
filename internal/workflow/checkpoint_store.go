package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// fileCheckPointStore persists Agent workflow checkpoints on disk (per run id).
type fileCheckPointStore struct {
	dir string
	mu  sync.RWMutex
}

func newFileCheckPointStore(dir string) (*fileCheckPointStore, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = filepath.Join("data", "workflow-checkpoints")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create workflow checkpoint dir: %w", err)
	}
	return &fileCheckPointStore{dir: dir}, nil
}

func (s *fileCheckPointStore) path(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("checkpoint id is empty")
	}
	if strings.Contains(id, "..") || strings.ContainsAny(id, `/\`) {
		return "", fmt.Errorf("invalid checkpoint id")
	}
	return filepath.Join(s.dir, id+".ckpt"), nil
}

func (s *fileCheckPointStore) Get(_ context.Context, checkPointID string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, err := s.path(checkPointID)
	if err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return data, true, nil
}

func (s *fileCheckPointStore) Set(_ context.Context, checkPointID string, checkPoint []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.path(checkPointID)
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, checkPoint, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
