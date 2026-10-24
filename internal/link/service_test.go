package link_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/cuongnt02/link-shortener/internal/link"
	"github.com/cuongnt02/link-shortener/internal/store"
)

// Unit tests for the link service. The service only talks to its
// dependencies through a Store interface and a generator function, so
// these tests replace both with fakes they fully control. No real store,
// no randomness, no HTTP.

// Both real stores must satisfy link.Store. These lines don't run
// anything: if a store's methods stop matching the interface, the test
// package fails to compile.
var (
	_ link.Store = (*store.MemoryStore)(nil)
	_ link.Store = (*store.SQLiteStore)(nil)
)

// errBoom stands in for an unexpected failure, like a database going down.
var errBoom = errors.New("boom: database unreachable")

// fakeStore is an in-memory link.Store that tests can inspect and make fail.
type fakeStore struct {
	mu        sync.Mutex
	links     map[string]string
	saveErr   error // if set, every Save returns it
	getErr    error // if set, every Get returns it
	saveCalls int
	lastCtx   context.Context
}

func newFakeStore() *fakeStore {
	return &fakeStore{links: make(map[string]string)}
}

func (f *fakeStore) Save(ctx context.Context, code, url string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saveCalls++
	f.lastCtx = ctx
	if f.saveErr != nil {
		return f.saveErr
	}
	if _, taken := f.links[code]; taken {
		return store.ErrCodeExists
	}
	f.links[code] = url
	return nil
}

func (f *fakeStore) Get(ctx context.Context, code string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastCtx = ctx
	if f.getErr != nil {
		return "", f.getErr
	}
	url, ok := f.links[code]
	if !ok {
		return "", store.ErrNotFound
	}
	return url, nil
}

// fakeGenerator returns the given codes in order, then errors. It counts
// how many times it was called.
type fakeGenerator struct {
	codes []string
	calls int
}

func (g *fakeGenerator) next() (string, error) {
	if g.calls >= len(g.codes) {
		g.calls++
		return "", errors.New("fakeGenerator: ran out of codes")
	}
	code := g.codes[g.calls]
	g.calls++
	return code, nil
}

// repeatCode returns a generator that always returns the same code.
func repeatCode(code string, n int) *fakeGenerator {
	codes := make([]string, n)
	for i := range codes {
		codes[i] = code
	}
	return &fakeGenerator{codes: codes}
}

func TestShortenSavesAndReturnsCode(t *testing.T) {
	st := newFakeStore()
	gen := &fakeGenerator{codes: []string{"abc123"}}
	svc := link.NewService(st, gen.next)

	code, err := svc.Shorten(t.Context(), "https://go.dev")

	if err != nil {
		t.Fatalf("Shorten returned error: %v", err)
	}
	if code != "abc123" {
		t.Errorf("Shorten returned code %q, want %q", code, "abc123")
	}
	if got := st.links["abc123"]; got != "https://go.dev" {
		t.Errorf("store has %q for code %q, want %q", got, "abc123", "https://go.dev")
	}
}

func TestShortenAcceptsURLVariants(t *testing.T) {
	urls := []string{
		"http://example.com",
		"https://example.com/a/long/path",
		"https://example.com:8443/path",
		"https://example.com/search?q=golang&page=2",
		"https://example.com/page#section",
	}
	for _, u := range urls {
		t.Run(u, func(t *testing.T) {
			st := newFakeStore()
			svc := link.NewService(st, (&fakeGenerator{codes: []string{"abc123"}}).next)

			if _, err := svc.Shorten(t.Context(), u); err != nil {
				t.Fatalf("Shorten(%q) returned error: %v", u, err)
			}
			if got := st.links["abc123"]; got != u {
				t.Errorf("stored url = %q, want %q (unchanged)", got, u)
			}
		})
	}
}

// TestShortenRejectsInvalidURL checks that bad URLs return ErrInvalidURL
// and that the service stops before generating a code or touching the store.
func TestShortenRejectsInvalidURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"empty", ""},
		{"relative path", "/just/a/path"},
		{"no scheme", "go.dev"},
		{"ftp scheme", "ftp://go.dev/file"},
		{"javascript scheme", "javascript:alert(1)"},
		{"scheme without host", "https://"},
		{"not parseable", "https://exa mple.com/%zz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newFakeStore()
			gen := &fakeGenerator{codes: []string{"abc123"}}
			svc := link.NewService(st, gen.next)

			code, err := svc.Shorten(t.Context(), tt.url)

			if !errors.Is(err, link.ErrInvalidURL) {
				t.Errorf("Shorten(%q) error = %v, want %v", tt.url, err, link.ErrInvalidURL)
			}
			if code != "" {
				t.Errorf("Shorten(%q) code = %q, want empty", tt.url, code)
			}
			if gen.calls != 0 {
				t.Errorf("generator called %d times, want 0 for an invalid url", gen.calls)
			}
			if st.saveCalls != 0 {
				t.Errorf("store.Save called %d times, want 0 for an invalid url", st.saveCalls)
			}
		})
	}
}

// TestShortenRetriesOnCollision: the first two generated codes are already
// taken, so the service must keep trying and succeed on the third.
func TestShortenRetriesOnCollision(t *testing.T) {
	st := newFakeStore()
	st.links["taken1"] = "https://first.example"
	st.links["taken2"] = "https://second.example"
	gen := &fakeGenerator{codes: []string{"taken1", "taken2", "free99"}}
	svc := link.NewService(st, gen.next)

	code, err := svc.Shorten(t.Context(), "https://go.dev")

	if err != nil {
		t.Fatalf("Shorten returned error: %v", err)
	}
	if code != "free99" {
		t.Errorf("Shorten returned code %q, want %q", code, "free99")
	}
	if st.saveCalls != 3 {
		t.Errorf("store.Save called %d times, want 3", st.saveCalls)
	}
	if got := st.links["taken1"]; got != "https://first.example" {
		t.Errorf("existing link %q was changed to %q", "taken1", got)
	}
}

// TestShortenGivesUpAfterMaxAttempts: every generated code is taken, so the
// service must stop after exactly MaxAttempts tries and return an error.
func TestShortenGivesUpAfterMaxAttempts(t *testing.T) {
	st := newFakeStore()
	st.links["dup"] = "https://existing.example"
	gen := repeatCode("dup", link.MaxAttempts+5)
	svc := link.NewService(st, gen.next)

	code, err := svc.Shorten(t.Context(), "https://go.dev")

	if err == nil {
		t.Fatalf("Shorten returned code %q and no error, want an error", code)
	}
	if errors.Is(err, link.ErrInvalidURL) {
		t.Errorf("error = %v; must not be ErrInvalidURL, the url was valid", err)
	}
	if st.saveCalls != link.MaxAttempts {
		t.Errorf("store.Save called %d times, want exactly %d", st.saveCalls, link.MaxAttempts)
	}
}

// TestShortenGeneratorError: if the generator fails, the error comes back
// wrapped (errors.Is still finds it) and nothing is saved.
func TestShortenGeneratorError(t *testing.T) {
	st := newFakeStore()
	failing := func() (string, error) { return "", errBoom }
	svc := link.NewService(st, failing)

	_, err := svc.Shorten(t.Context(), "https://go.dev")

	if !errors.Is(err, errBoom) {
		t.Errorf("error = %v, want it to wrap %v", err, errBoom)
	}
	if st.saveCalls != 0 {
		t.Errorf("store.Save called %d times, want 0", st.saveCalls)
	}
}

// TestShortenStoreError: an unexpected store error is not a collision, so
// the service must not retry. It returns the error wrapped.
func TestShortenStoreError(t *testing.T) {
	st := newFakeStore()
	st.saveErr = errBoom
	gen := &fakeGenerator{codes: []string{"abc123", "def456", "ghi789"}}
	svc := link.NewService(st, gen.next)

	_, err := svc.Shorten(t.Context(), "https://go.dev")

	if !errors.Is(err, errBoom) {
		t.Errorf("error = %v, want it to wrap %v", err, errBoom)
	}
	if st.saveCalls != 1 {
		t.Errorf("store.Save called %d times, want 1 (no retry on unexpected errors)", st.saveCalls)
	}
}

func TestResolveKnownCode(t *testing.T) {
	st := newFakeStore()
	st.links["abc123"] = "https://go.dev/doc"
	svc := link.NewService(st, (&fakeGenerator{}).next)

	got, err := svc.Resolve(t.Context(), "abc123")

	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got != "https://go.dev/doc" {
		t.Errorf("Resolve = %q, want %q", got, "https://go.dev/doc")
	}
}

// TestResolveUnknownCode: the store's not-found error is translated into
// the service's own link.ErrNotFound, so callers never need to know which
// store is behind the service.
func TestResolveUnknownCode(t *testing.T) {
	svc := link.NewService(newFakeStore(), (&fakeGenerator{}).next)

	got, err := svc.Resolve(t.Context(), "nope42")

	if !errors.Is(err, link.ErrNotFound) {
		t.Errorf("error = %v, want %v", err, link.ErrNotFound)
	}
	if got != "" {
		t.Errorf("Resolve = %q, want empty", got)
	}
}

// TestResolveStoreError: an unexpected store error must come back wrapped,
// and must NOT look like "not found". Otherwise the handler would answer
// 404 while the database is down, instead of 500.
func TestResolveStoreError(t *testing.T) {
	st := newFakeStore()
	st.getErr = errBoom
	svc := link.NewService(st, (&fakeGenerator{}).next)

	_, err := svc.Resolve(t.Context(), "abc123")

	if !errors.Is(err, errBoom) {
		t.Errorf("error = %v, want it to wrap %v", err, errBoom)
	}
	if errors.Is(err, link.ErrNotFound) {
		t.Errorf("error = %v; a store failure must not be reported as ErrNotFound", err)
	}
}

// TestContextReachesStore checks that the service passes the caller's
// context down to the store unchanged, instead of making its own.
// Stage 6's database store needs it to cancel queries.
func TestContextReachesStore(t *testing.T) {
	type ctxKey struct{}
	ctx := context.WithValue(t.Context(), ctxKey{}, "request-42")
	st := newFakeStore()
	svc := link.NewService(st, (&fakeGenerator{codes: []string{"abc123"}}).next)

	if _, err := svc.Shorten(ctx, "https://go.dev"); err != nil {
		t.Fatalf("Shorten returned error: %v", err)
	}
	if got := st.lastCtx.Value(ctxKey{}); got != "request-42" {
		t.Errorf("Save got a different context (value %v), want the caller's context", got)
	}

	st.lastCtx = nil
	if _, err := svc.Resolve(ctx, "abc123"); err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if st.lastCtx == nil || st.lastCtx.Value(ctxKey{}) != "request-42" {
		t.Errorf("Get did not receive the caller's context")
	}
}
