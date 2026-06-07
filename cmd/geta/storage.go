package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"sync"

	zeta "github.com/gematik/zeta-client-go"
)

// jsonFileStorage is the CLI's persistent state backend. On-disk format is
// the flat `{"key": "value"}` JSON that zeta-cli uses — the same profile
// file is interoperable between the two tools.
//
// Atomic writes (temp file → fsync → os.Rename), 0600 perms, in-process
// sync.Mutex for concurrent calls. Plaintext (no encryption); a separate
// secret-aware wrapper layer would be needed for at-rest encryption.
//
// In-memory map is the source of truth after the first load; cross-process
// writes during the lifetime of one instance are not observed.
type jsonFileStorage struct {
	mu   sync.Mutex
	path string
	data map[string]string
}

func openStorage(path string) (*jsonFileStorage, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	s := &jsonFileStorage{path: path, data: map[string]string{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) || len(b) == 0 {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, fmt.Errorf("storage: parse %s: %w", path, err)
	}
	return s, nil
}

func (s *jsonFileStorage) Put(key, value string) error {
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

func (s *jsonFileStorage) Get(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	if !ok {
		return "", zeta.ErrNotFound
	}
	return v, nil
}

func (s *jsonFileStorage) Remove(key string) error {
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

func (s *jsonFileStorage) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Clear always writes an empty file even when in-memory map is already
	// empty — the file might exist with stale content this instance never
	// loaded. Matches zeta-cli's clear semantics.
	next := map[string]string{}
	if err := s.writeAtomic(next); err != nil {
		return err
	}
	s.data = next
	return nil
}

// writeAtomic serializes data with the same indent zeta-cli uses (2 spaces,
// pretty-printed), writes via temp file + fsync + os.Rename. The 0600 perms
// come from os.CreateTemp's default (matches zeta-cli's POSIX mode).
func (s *jsonFileStorage) writeAtomic(data map[string]string) error {
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
