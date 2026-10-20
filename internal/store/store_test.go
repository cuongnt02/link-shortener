package store_test

import (
	"errors"
	"testing"

	"github.com/cuongnt02/link-shortener/internal/store"
)

// mustSave saves a code/url pair and stops the test if it fails.
// Use it for setup steps that the test is not about.
func mustSave(t *testing.T, s *store.MemoryStore, code, url string) {
	t.Helper()
	if err := s.Save(code, url); err != nil {
		t.Fatalf("setup: Save(%q, %q) returned error: %v", code, url, err)
	}
}

// assertGet checks that code maps to wantURL with no error.
func assertGet(t *testing.T, s *store.MemoryStore, code, wantURL string) {
	t.Helper()
	got, err := s.Get(code)
	if err != nil {
		t.Errorf("Get(%q) returned error: %v, want nil", code, err)
		return
	}
	if got != wantURL {
		t.Errorf("Get(%q) = %q, want %q", code, got, wantURL)
	}
}

// assertNotFound checks that code is missing: empty url and ErrNotFound.
func assertNotFound(t *testing.T, s *store.MemoryStore, code string) {
	t.Helper()
	got, err := s.Get(code)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Get(%q) error = %v, want %v", code, err, store.ErrNotFound)
	}
	if got != "" {
		t.Errorf("Get(%q) = %q, want empty string", code, got)
	}
}

// TestGetOnEmptyStore checks that a new store contains nothing.
func TestGetOnEmptyStore(t *testing.T) {
	s := store.NewInMemoryStore()

	assertNotFound(t, s, "hello")
}

// TestSaveThenGet checks that a saved code returns exactly the saved url.
func TestSaveThenGet(t *testing.T) {
	s := store.NewInMemoryStore()

	if err := s.Save("hello", "https://example.com"); err != nil {
		t.Fatalf("Save(%q, %q) returned error: %v, want nil", "hello", "https://example.com", err)
	}

	assertGet(t, s, "hello", "https://example.com")
}

// TestMultipleEntries checks that entries stay separate: all codes are
// saved first, then read back, so a store that only remembers the last
// save would fail.
func TestMultipleEntries(t *testing.T) {
	entries := []struct {
		code string
		url  string
	}{
		{"hello", "https://example.com"},
		{"google", "https://google.com"},
		{"mslearn", "https://learn.microsoft.com"},
		{"portfolio", "https://cuongnt02.io.vn"},
		{"abc", "https://abc.com.vn"},
	}

	s := store.NewInMemoryStore()
	for _, e := range entries {
		mustSave(t, s, e.code, e.url)
	}

	for _, e := range entries {
		t.Run(e.code, func(t *testing.T) {
			assertGet(t, s, e.code, e.url)
		})
	}
}

// TestGetUnknownCodeAfterSave checks that a non-empty store still reports
// codes it has never seen as not found.
func TestGetUnknownCodeAfterSave(t *testing.T) {
	s := store.NewInMemoryStore()
	mustSave(t, s, "hello", "https://example.com")

	assertNotFound(t, s, "world")
}

// TestCaseSensitivity checks that codes differing only in case are
// different entries.
func TestCaseSensitivity(t *testing.T) {
	s := store.NewInMemoryStore()
	mustSave(t, s, "hello", "https://example.com")
	mustSave(t, s, "HELLO", "https://google.com")

	assertGet(t, s, "hello", "https://example.com")
	assertGet(t, s, "HELLO", "https://google.com")
	assertNotFound(t, s, "Hello")
}

// TestSaveDuplicateCode checks that saving an existing code is rejected
// and does not overwrite the original url.
func TestSaveDuplicateCode(t *testing.T) {
	s := store.NewInMemoryStore()
	mustSave(t, s, "hello", "https://example.com")

	err := s.Save("hello", "https://google.com")
	if !errors.Is(err, store.ErrCodeExists) {
		t.Errorf("Save(%q, %q) on existing code: error = %v, want %v", "hello", "https://google.com", err, store.ErrCodeExists)
	}

	assertGet(t, s, "hello", "https://example.com")
}
