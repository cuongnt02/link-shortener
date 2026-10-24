package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cuongnt02/link-shortener/internal/store"
)

// This file is the store CONTRACT: the behavior every store must have,
// whatever it uses underneath. memory_test.go and sqlite_test.go each run
// the whole contract against their own store. If both pass, the service
// can use either one without noticing a difference.

// linkStore is the behavior under test. It matches link.Store; it is
// redeclared here so the store tests don't depend on the link package.
type linkStore interface {
	Save(ctx context.Context, code, url string) error
	Get(ctx context.Context, code string) (string, error)
}

// storeFactory returns a new, empty store for one test. It also registers
// any cleanup the store needs (like closing a database) with t.Cleanup.
type storeFactory func(t *testing.T) linkStore

// runStoreContract runs every contract test as a subtest.
func runStoreContract(t *testing.T, newStore storeFactory) {
	t.Run("GetOnEmptyStore", func(t *testing.T) { testGetOnEmptyStore(t, newStore(t)) })
	t.Run("SaveThenGet", func(t *testing.T) { testSaveThenGet(t, newStore(t)) })
	t.Run("MultipleEntries", func(t *testing.T) { testMultipleEntries(t, newStore(t)) })
	t.Run("GetUnknownCodeAfterSave", func(t *testing.T) { testGetUnknownCodeAfterSave(t, newStore(t)) })
	t.Run("CaseSensitivity", func(t *testing.T) { testCaseSensitivity(t, newStore(t)) })
	t.Run("SaveDuplicateCode", func(t *testing.T) { testSaveDuplicateCode(t, newStore(t)) })
	t.Run("ValuesAreStoredExactly", func(t *testing.T) { testValuesAreStoredExactly(t, newStore(t)) })
	t.Run("SQLCharactersAreJustData", func(t *testing.T) { testSQLCharactersAreJustData(t, newStore(t)) })
	t.Run("Concurrency", func(t *testing.T) { runConcurrencyContract(t, newStore) })
}

// mustSave saves a code/url pair and stops the test if it fails.
// Use it for setup steps that the test is not about.
func mustSave(t *testing.T, s linkStore, code, url string) {
	t.Helper()
	if err := s.Save(t.Context(), code, url); err != nil {
		t.Fatalf("setup: Save(%q, %q) returned error: %v", code, url, err)
	}
}

// assertGet checks that code maps to wantURL with no error.
func assertGet(t *testing.T, s linkStore, code, wantURL string) {
	t.Helper()
	got, err := s.Get(t.Context(), code)
	if err != nil {
		t.Errorf("Get(%q) returned error: %v, want nil", code, err)
		return
	}
	if got != wantURL {
		t.Errorf("Get(%q) = %q, want %q", code, got, wantURL)
	}
}

// assertNotFound checks that code is missing: empty url and ErrNotFound.
func assertNotFound(t *testing.T, s linkStore, code string) {
	t.Helper()
	got, err := s.Get(t.Context(), code)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Get(%q) error = %v, want %v", code, err, store.ErrNotFound)
	}
	if got != "" {
		t.Errorf("Get(%q) = %q, want empty string", code, got)
	}
}

// testGetOnEmptyStore checks that a new store contains nothing.
func testGetOnEmptyStore(t *testing.T, s linkStore) {
	assertNotFound(t, s, "hello")
}

// testSaveThenGet checks that a saved code returns exactly the saved url.
func testSaveThenGet(t *testing.T, s linkStore) {
	if err := s.Save(t.Context(), "hello", "https://example.com"); err != nil {
		t.Fatalf("Save(%q, %q) returned error: %v, want nil", "hello", "https://example.com", err)
	}

	assertGet(t, s, "hello", "https://example.com")
}

// testMultipleEntries checks that entries stay separate: all codes are
// saved first, then read back, so a store that only remembers the last
// save would fail.
func testMultipleEntries(t *testing.T, s linkStore) {
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

	for _, e := range entries {
		mustSave(t, s, e.code, e.url)
	}

	for _, e := range entries {
		t.Run(e.code, func(t *testing.T) {
			assertGet(t, s, e.code, e.url)
		})
	}
}

// testGetUnknownCodeAfterSave checks that a non-empty store still reports
// codes it has never seen as not found.
func testGetUnknownCodeAfterSave(t *testing.T, s linkStore) {
	mustSave(t, s, "hello", "https://example.com")

	assertNotFound(t, s, "world")
}

// testCaseSensitivity checks that codes differing only in case are
// different entries. Watch out: some databases compare text without
// caring about case unless told otherwise.
func testCaseSensitivity(t *testing.T, s linkStore) {
	mustSave(t, s, "hello", "https://example.com")
	mustSave(t, s, "HELLO", "https://google.com")

	assertGet(t, s, "hello", "https://example.com")
	assertGet(t, s, "HELLO", "https://google.com")
	assertNotFound(t, s, "Hello")
}

// testSaveDuplicateCode checks that saving an existing code is rejected
// with ErrCodeExists and does not overwrite the original url.
func testSaveDuplicateCode(t *testing.T, s linkStore) {
	mustSave(t, s, "hello", "https://example.com")

	err := s.Save(t.Context(), "hello", "https://google.com")
	if !errors.Is(err, store.ErrCodeExists) {
		t.Errorf("Save(%q, %q) on existing code: error = %v, want %v", "hello", "https://google.com", err, store.ErrCodeExists)
	}

	assertGet(t, s, "hello", "https://example.com")
}

// testValuesAreStoredExactly checks that unusual but valid urls come back
// byte for byte: very long, non-ASCII, and full of punctuation.
func testValuesAreStoredExactly(t *testing.T, s linkStore) {
	urls := map[string]string{
		"long":    "https://example.com/" + strings.Repeat("a", 4000),
		"unicode": "https://example.com/xin-chào/日本語?q=héllo",
		"symbols": "https://example.com/p?a=1&b=%20x&c=[]{}#frag",
	}
	for code, url := range urls {
		mustSave(t, s, code, url)
	}
	for code, url := range urls {
		t.Run(code, func(t *testing.T) {
			assertGet(t, s, code, url)
		})
	}
}

// testSQLCharactersAreJustData checks that quotes and SQL keywords in a
// code or url are stored as plain text, never run as SQL. A store that
// builds queries by gluing strings together fails this test, or worse.
func testSQLCharactersAreJustData(t *testing.T, s linkStore) {
	mustSave(t, s, "normal", "https://example.com")
	evilCode := "x'; DROP TABLE links; --"
	evilURL := `https://example.com/?q=' OR '1'='1" --`

	mustSave(t, s, evilCode, evilURL)

	assertGet(t, s, evilCode, evilURL)
	assertGet(t, s, "normal", "https://example.com") // the table must still be there
	assertNotFound(t, s, "' OR '1'='1")
}
