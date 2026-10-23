package store

import (
	"context"
	"errors"
	"sync"
)

var (
	// ErrNotFound states that the map does not have the url associated with the code provided
	ErrNotFound = errors.New("url not found")
	// ErrCodeExists states that the code have existed in the map
	ErrCodeExists = errors.New("code exists in the map")
)

// MemoryStore is an in memory data structure to store all code to links data
// MemoryStore have many operations that can modify the map of code to links
// The store is safe from concurrent use by multiple goroutines
type MemoryStore struct {
	mu         sync.RWMutex // protects codeToLink from concurrent access
	codeToLink map[string]string
}

// NewInMemoryStore is a function to generate a blank empty MemoryStore data structure
// NewInMemoryStore returns a pointer to the newly created store for multiple owners
func NewInMemoryStore() *MemoryStore {
	return &MemoryStore{
		codeToLink: make(map[string]string),
	}
}

// Save add a new code and url mapping into the MemoryStore's map.
// ErrCodeExists is returned when the code used to save is duplicated with an entry
func (s *MemoryStore) Save(_ context.Context, code, url string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.codeToLink[code]; ok {
		return ErrCodeExists
	}
	s.codeToLink[code] = url
	return nil
}

// Get returns the mapped url as the code provided.
// ErrNotFound is returned when the code is missing
func (s *MemoryStore) Get(_ context.Context, code string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	url, ok := s.codeToLink[code]
	if !ok {
		return "", ErrNotFound
	}
	return url, nil
}
