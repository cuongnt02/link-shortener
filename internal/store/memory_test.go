package store_test

import (
	"testing"

	"github.com/cuongnt02/link-shortener/internal/store"
)

// TestMemoryStore runs the full store contract against the in-memory store.
func TestMemoryStore(t *testing.T) {
	runStoreContract(t, func(t *testing.T) linkStore {
		return store.NewInMemoryStore()
	})
}
