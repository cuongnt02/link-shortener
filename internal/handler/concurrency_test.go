package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// Integration test for concurrency: many clients shorten links at the same
// time against one real test server, the way real traffic arrives.
// Run with -race to see unsafe access reported:
//
//	go test -race -count=1 ./internal/handler/

// shortenConcurrently is like shortenOverHTTP, but returns an error instead
// of calling t.Fatalf: Fatalf may only be called from the test's own
// goroutine, not from goroutines the test starts.
func shortenConcurrently(srvURL, targetURL string) (shortenResponse, error) {
	body, err := json.Marshal(map[string]string{"url": targetURL})
	if err != nil {
		return shortenResponse{}, err
	}
	res, err := http.Post(srvURL+"/shorten", "application/json", strings.NewReader(string(body)))
	if err != nil {
		return shortenResponse{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		return shortenResponse{}, fmt.Errorf("status = %d, want %d", res.StatusCode, http.StatusCreated)
	}
	var resp shortenResponse
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return shortenResponse{}, fmt.Errorf("decoding body: %w", err)
	}
	return resp, nil
}

// TestConcurrentShortenAndRedirect sends 50 POST /shorten requests at once,
// then checks every code is unique and redirects to its own URL.
func TestConcurrentShortenAndRedirect(t *testing.T) {
	const clients = 50
	srv := newTestServer(t)
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex // protects codeToURL, which every goroutine writes to
	codeToURL := make(map[string]string)

	for i := range clients {
		wg.Go(func() {
			<-start
			target := fmt.Sprintf("https://example.com/client/%d", i)
			resp, err := shortenConcurrently(srv.URL, target)
			if err != nil {
				t.Errorf("client %d: POST /shorten: %v", i, err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if prev, dup := codeToURL[resp.Code]; dup {
				t.Errorf("code %q returned for both %q and %q", resp.Code, prev, target)
			}
			codeToURL[resp.Code] = target
		})
	}
	close(start)
	wg.Wait()

	if len(codeToURL) != clients {
		t.Fatalf("got %d unique codes, want %d", len(codeToURL), clients)
	}

	// Read them all back concurrently too: redirects run in parallel with
	// each other in production as well.
	client := noRedirectClient()
	for code, want := range codeToURL {
		wg.Go(func() {
			res, err := client.Get(srv.URL + "/" + code)
			if err != nil {
				t.Errorf("GET /%s: %v", code, err)
				return
			}
			res.Body.Close()
			if got := res.Header.Get("Location"); got != want {
				t.Errorf("GET /%s: Location = %q, want %q", code, got, want)
			}
		})
	}
	wg.Wait()
}
