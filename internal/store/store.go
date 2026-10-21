package store

import (
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
// The store is safe from concurrent use
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

// Save add a new code and url mapping into the MemoryStore's map
func (s *MemoryStore) Save(code, url string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if _, ok := s.codeToLink[code]; ok {
		return ErrCodeExists
	}
	s.codeToLink[code] = url
	return nil
}

// Get returns the mapped url as the code provided. Returns a blank string and an error when the code provided not exists in the map
func (s *MemoryStore) Get(code string) (string, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	url, ok := s.codeToLink[code]
	if !ok {
		return "", ErrNotFound
	}
	return url, nil
}
