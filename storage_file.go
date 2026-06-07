package zeta

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"sync"
)

// FileStorage is a Storage implementation backed by a flat JSON file.
// On-disk format is a JSON object of string-to-string pairs, pretty-printed
// with 2-space indent. Atomic writes (temp file + fsync + os.Rename),
// 0600 perms.
//
// Plaintext at rest. The SDK passes plaintext state (access/refresh tokens,
// registration data) to any Storage implementation; wrap with your own
// encryption layer for production deployments.
//
// In-memory map is the source of truth after first load; cross-process
// writes during the lifetime of one instance are not observed.
type FileStorage struct {
	mu   sync.Mutex
	path string
	data map[string]string
}

// OpenFileStorage opens or creates the JSON storage file at path. Missing
// parent directories are created with 0700 perms. A missing or empty file
// is treated as an empty store; a malformed file returns a parse error.
func OpenFileStorage(path string) (*FileStorage, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	s := &FileStorage{path: path, data: map[string]string{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) || len(b) == 0 {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, fmt.Errorf("zeta: parse %s: %w", path, err)
	}
	return s, nil
}

// Path returns the on-disk path the FileStorage was opened against.
func (s *FileStorage) Path() string { return s.path }

func (s *FileStorage) Put(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]string, len(s.data)+1)
	maps.Copy(next, s.data)
	next[key] = value
	if err := s.writeAtomic(next); err != nil {
		return err
	}
	s.data = next
	return nil
}

func (s *FileStorage) Get(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (s *FileStorage) Remove(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[key]; !ok {
		return nil
	}
	next := make(map[string]string, len(s.data))
	for k, v := range s.data {
		if k != key {
			next[k] = v
		}
	}
	if err := s.writeAtomic(next); err != nil {
		return err
	}
	s.data = next
	return nil
}

// Clear always writes an empty file even when the in-memory map is already
// empty — the file may exist on disk with stale content this instance never
// loaded. Matches the broader Storage.Clear contract: "after Clear, the
// underlying store is empty."
func (s *FileStorage) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := map[string]string{}
	if err := s.writeAtomic(next); err != nil {
		return err
	}
	s.data = next
	return nil
}

func (s *FileStorage) writeAtomic(data map[string]string) error {
	buf, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".tmp.*")
	if err != nil {
		return err
	}
	cleanup := func() { _ = os.Remove(tmp.Name()) }
	if _, err := tmp.Write(buf); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		cleanup()
		return err
	}
	return nil
}
