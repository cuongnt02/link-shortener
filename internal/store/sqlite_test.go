package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/cuongnt02/link-shortener/internal/store"
)

// SQLite tests: the full store contract, plus behavior only a real database
// has. Each test gets its own database file inside t.TempDir(), a folder Go
// deletes when the test ends, so tests never share data or leave files behind.

// openSQLite opens a store on path and closes it when the test ends.
func openSQLite(t *testing.T, path string) *store.SQLiteStore {
	t.Helper()
	s, err := store.NewSQLiteStore(t.Context(), path)
	if err != nil {
		t.Fatalf("NewSQLiteStore(%q) returned error: %v", path, err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close returned error: %v", err)
		}
	})
	return s
}

// newTempSQLite opens a store on a fresh database file.
func newTempSQLite(t *testing.T) *store.SQLiteStore {
	t.Helper()
	return openSQLite(t, filepath.Join(t.TempDir(), "links.db"))
}

// TestSQLiteStore runs the full store contract against SQLite.
func TestSQLiteStore(t *testing.T) {
	runStoreContract(t, func(t *testing.T) linkStore {
		return newTempSQLite(t)
	})
}

// TestSQLiteLinksSurviveRestart is the reason a database exists: close the
// store, open the same file again, and the links are still there.
func TestSQLiteLinksSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.db")

	first, err := store.NewSQLiteStore(t.Context(), path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	mustSave(t, first, "hello", "https://example.com")
	if err := first.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	second := openSQLite(t, path)

	assertGet(t, second, "hello", "https://example.com")
	// The duplicate rule must also survive the restart.
	if err := second.Save(t.Context(), "hello", "https://other.example"); !errors.Is(err, store.ErrCodeExists) {
		t.Errorf("Save of existing code after restart: error = %v, want %v", err, store.ErrCodeExists)
	}
}

// TestSQLiteOpenTwiceIsSafe checks that setting up the schema on a file
// that already has it does not fail. The app does this on every start.
func TestSQLiteOpenTwiceIsSafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.db")
	first := openSQLite(t, path)
	mustSave(t, first, "hello", "https://example.com")

	second := openSQLite(t, path)

	assertGet(t, second, "hello", "https://example.com")
}

// TestSQLiteOpenFailsForBadPath checks that a database that can't be
// opened is reported by the constructor, not by the first request
// minutes later. Here the parent folder doesn't exist.
func TestSQLiteOpenFailsForBadPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-folder", "links.db")

	s, err := store.NewSQLiteStore(t.Context(), path)

	if err == nil {
		s.Close()
		t.Fatalf("NewSQLiteStore(%q) returned no error, want an error", path)
	}
}

// TestSQLiteCanceledContext checks that the store respects its context: a
// canceled request must fail with an error wrapping context.Canceled, and
// must not be mistaken for "not found" or "code exists".
func TestSQLiteCanceledContext(t *testing.T) {
	s := newTempSQLite(t)
	mustSave(t, s, "hello", "https://example.com")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := s.Get(ctx, "hello")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Get with canceled context: error = %v, want it to wrap %v", err, context.Canceled)
	}
	if errors.Is(err, store.ErrNotFound) {
		t.Errorf("Get with canceled context reported ErrNotFound; a canceled request is not a missing code")
	}

	err = s.Save(ctx, "new", "https://example.com/new")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Save with canceled context: error = %v, want it to wrap %v", err, context.Canceled)
	}
	if errors.Is(err, store.ErrCodeExists) {
		t.Errorf("Save with canceled context reported ErrCodeExists")
	}
}

// TestSQLiteUseAfterClose checks that a closed store returns real errors,
// not ErrNotFound: "the database is gone" must become a 500, not a 404.
func TestSQLiteUseAfterClose(t *testing.T) {
	s, err := store.NewSQLiteStore(t.Context(), filepath.Join(t.TempDir(), "links.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore returned error: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	_, err = s.Get(t.Context(), "hello")

	if err == nil {
		t.Fatalf("Get after Close returned no error")
	}
	if errors.Is(err, store.ErrNotFound) {
		t.Errorf("Get after Close returned ErrNotFound (%v); want a real error", err)
	}
}
