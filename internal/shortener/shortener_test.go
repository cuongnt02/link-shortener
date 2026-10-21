package shortener_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/cuongnt02/link-shortener/internal/shortener"
)

// The tests keep their own copy of the alphabet on purpose: if someone
// changes the alphabet in shortener.go by accident, these tests notice.
const base62 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

func mustGenCode(t *testing.T, length int) string {
	t.Helper()
	code, err := shortener.GenCode(length)
	if err != nil {
		t.Fatalf("GenCode(%d) returned error: %v", length, err)
	}
	return code
}

func TestGenCodeLength(t *testing.T) {
	for _, length := range []int{1, 6, 32} {
		t.Run(fmt.Sprintf("length=%d", length), func(t *testing.T) {
			if got := len(mustGenCode(t, length)); got != length {
				t.Errorf("len(GenCode(%d)) = %d, want %d", length, got, length)
			}
		})
	}
}

func TestGenCodeOnlyUsesAlphabet(t *testing.T) {
	code := mustGenCode(t, 1000)
	for i, c := range code {
		if !strings.ContainsRune(base62, c) {
			t.Fatalf("GenCode returned character %q at index %d, not in the base62 alphabet", c, i)
		}
	}
}

// TestGenCodeUsesWholeAlphabet generates 10,000 characters and checks that
// every one of the 62 characters shows up at least once. This catches
// off-by-one bugs that can never pick the first or last character.
// With a fair generator, the chance of a character missing is far below
// one in a trillion, so this test is not flaky.
func TestGenCodeUsesWholeAlphabet(t *testing.T) {
	seen := make(map[rune]bool)
	for _, c := range mustGenCode(t, 10_000) {
		seen[c] = true
	}
	for _, c := range base62 {
		if !seen[c] {
			t.Errorf("character %q never appeared in 10,000 generated characters", c)
		}
	}
}

// TestGenCodeIsRandom generates 1,000 codes of length 6. With 62^6 (about
// 57 billion) possible codes, even one duplicate is very unlikely (about
// 1 in 100,000), so allowing one keeps the test from failing by bad luck,
// while a broken generator (e.g. one that repeats) still fails.
func TestGenCodeIsRandom(t *testing.T) {
	const n = 1000
	seen := make(map[string]bool, n)
	for range n {
		seen[mustGenCode(t, 6)] = true
	}
	if len(seen) < n-1 {
		t.Errorf("got %d unique codes out of %d, want at least %d", len(seen), n, n-1)
	}
}

func TestGenCodeRejectsInvalidLength(t *testing.T) {
	for _, length := range []int{0, -1, -100} {
		code, err := shortener.GenCode(length)
		if !errors.Is(err, shortener.ErrInvalidCodeLength) {
			t.Errorf("GenCode(%d) error = %v, want %v", length, err, shortener.ErrInvalidCodeLength)
		}
		if code != "" {
			t.Errorf("GenCode(%d) = %q, want empty string", length, code)
		}
	}
}

// TestGenCodeConcurrent calls GenCode from many goroutines at once.
// Run with -race: GenCode must be safe to call from concurrent requests.
func TestGenCodeConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			if _, err := shortener.GenCode(6); err != nil {
				t.Errorf("GenCode(6) returned error: %v", err)
			}
		})
	}
	wg.Wait()
}
