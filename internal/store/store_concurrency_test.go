package store_test

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cuongnt02/link-shortener/internal/store"
)

// Concurrency tests: many goroutines use one store at the same time, the
// way concurrent HTTP requests do in the real server.
//
// Run them with the race detector:
//
//	go test -race -count=1 ./internal/store/
//
// Without -race, an unsafe map usually crashes the whole test run with
// "fatal error: concurrent map writes". With -race, the detector reports
// exactly which lines touched the map at the same time.

const goroutines = 100

// TestConcurrentSaves checks that saves from many goroutines at once are
// all kept.
func TestConcurrentSaves(t *testing.T) {
	s := store.NewInMemoryStore()
	start := make(chan struct{})
	var wg sync.WaitGroup

	for i := range goroutines {
		wg.Go(func() {
			<-start // wait, so all goroutines begin at the same moment
			code := fmt.Sprintf("code%d", i)
			if err := s.Save(code, "https://example.com/"+code); err != nil {
				t.Errorf("Save(%q) returned error: %v", code, err)
			}
		})
	}
	close(start)
	wg.Wait()

	for i := range goroutines {
		code := fmt.Sprintf("code%d", i)
		assertGet(t, s, code, "https://example.com/"+code)
	}
}

// TestConcurrentSaveAndGet checks that readers and writers can use the
// store at the same time.
func TestConcurrentSaveAndGet(t *testing.T) {
	s := store.NewInMemoryStore()
	mustSave(t, s, "existing", "https://example.com/existing")
	start := make(chan struct{})
	var wg sync.WaitGroup

	for i := range goroutines {
		// writer: adds a new code
		wg.Go(func() {
			<-start
			code := fmt.Sprintf("new%d", i)
			if err := s.Save(code, "https://example.com/"+code); err != nil {
				t.Errorf("Save(%q) returned error: %v", code, err)
			}
		})
		// reader: reads a code that exists the whole time
		wg.Go(func() {
			<-start
			got, err := s.Get("existing")
			if err != nil || got != "https://example.com/existing" {
				t.Errorf("Get(%q) = %q, %v; want %q, nil", "existing", got, err, "https://example.com/existing")
			}
		})
	}
	close(start)
	wg.Wait()
}

// TestConcurrentDuplicateSave has many goroutines race to save the same
// code. Exactly one may win; every other one must get ErrCodeExists.
//
// This catches a subtle bug: if "is the code taken?" and "save it" are
// two separate locked steps, two goroutines can both see "not taken" and
// both save. Being race-detector clean is not enough to pass this test.
func TestConcurrentDuplicateSave(t *testing.T) {
	s := store.NewInMemoryStore()
	start := make(chan struct{})
	var wg sync.WaitGroup
	var wins atomic.Int64
	var winnerURL atomic.Value

	for i := range goroutines {
		wg.Go(func() {
			<-start
			url := fmt.Sprintf("https://example.com/%d", i)
			err := s.Save("same", url)
			switch {
			case err == nil:
				wins.Add(1)
				winnerURL.Store(url)
			case errors.Is(err, store.ErrCodeExists):
				// expected for every goroutine but one
			default:
				t.Errorf("Save returned unexpected error: %v", err)
			}
		})
	}
	close(start)
	wg.Wait()

	if got := wins.Load(); got != 1 {
		t.Fatalf("%d goroutines saved code %q, want exactly 1", got, "same")
	}
	// The stored url must be the winner's, not one written by a "loser".
	assertGet(t, s, "same", winnerURL.Load().(string))
}
