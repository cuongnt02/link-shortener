package handler_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/cuongnt02/link-shortener/internal/handler"
	"github.com/cuongnt02/link-shortener/internal/link"
	"github.com/cuongnt02/link-shortener/internal/shortener"
	"github.com/cuongnt02/link-shortener/internal/store"
)

// Integration tests: a real HTTP server (httptest.NewServer) runs the full
// router from h.Routes(), and a real HTTP client sends requests over the
// network. Nothing is faked: the real handler, service, code generator
// and memory store all work together, wired the same way main.go does it.

// codePattern is what every generated code must look like:
// exactly 6 characters from the base62 alphabet.
var codePattern = regexp.MustCompile(`^[A-Za-z0-9]{6}$`)

// newTestServer starts a server running the app's routes. The handler's
// base URL must be the server's own URL so that short_url values can be
// followed, but that URL only exists after the server starts. So the server
// starts with an empty mux, and the app's routes are added to it afterwards.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	generate := func() (string, error) { return shortener.GenCode(6) }
	svc := link.NewService(store.NewInMemoryStore(), generate)
	h := handler.New(svc, srv.URL)
	mux.Handle("/", h.Routes())
	return srv
}

// noRedirectClient returns a client that does not follow redirects, so a
// test can inspect the 302 response itself.
func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// shortenOverHTTP sends POST /shorten for targetURL and returns the decoded response.
func shortenOverHTTP(t *testing.T, srv *httptest.Server, targetURL string) shortenResponse {
	t.Helper()
	body, err := json.Marshal(map[string]string{"url": targetURL})
	if err != nil {
		t.Fatalf("setup: marshal request: %v", err)
	}
	res, err := http.Post(srv.URL+"/shorten", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST /shorten: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("POST /shorten status = %d, want %d; body: %q", res.StatusCode, http.StatusCreated, b)
	}
	var resp shortenResponse
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		t.Fatalf("POST /shorten body is not valid JSON: %v", err)
	}
	return resp
}

func TestRouteHealth(t *testing.T) {
	srv := newTestServer(t)

	res, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}

	if res.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

// TestShortenThenRedirect checks that the short_url returned by
// POST /shorten responds with a 302 pointing at the original URL.
func TestShortenThenRedirect(t *testing.T) {
	srv := newTestServer(t)
	target := "https://go.dev/doc"

	resp := shortenOverHTTP(t, srv, target)

	if !codePattern.MatchString(resp.Code) {
		t.Errorf("code = %q, want 6 base62 characters", resp.Code)
	}
	if want := srv.URL + "/" + resp.Code; resp.ShortURL != want {
		t.Errorf("short_url = %q, want %q", resp.ShortURL, want)
	}

	res, err := noRedirectClient().Get(resp.ShortURL)
	if err != nil {
		t.Fatalf("GET %s: %v", resp.ShortURL, err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want %d", res.StatusCode, http.StatusFound)
	}
	if got := res.Header.Get("Location"); got != target {
		t.Errorf("Location = %q, want %q", got, target)
	}
}

// TestFollowShortLinkEndToEnd checks the whole user journey: a second test
// server plays the destination website, and a normal client (one that
// follows redirects) opens the short link and lands on that site.
func TestFollowShortLinkEndToEnd(t *testing.T) {
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "welcome to the destination")
	}))
	t.Cleanup(destination.Close)
	srv := newTestServer(t)

	resp := shortenOverHTTP(t, srv, destination.URL+"/landing")

	res, err := http.Get(resp.ShortURL)
	if err != nil {
		t.Fatalf("GET %s: %v", resp.ShortURL, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}

	if res.StatusCode != http.StatusOK {
		t.Errorf("final status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	if got, want := res.Request.URL.String(), destination.URL+"/landing"; got != want {
		t.Errorf("ended up at %q, want %q", got, want)
	}
	if string(body) != "welcome to the destination" {
		t.Errorf("final body = %q, want the destination page", body)
	}
}

// TestShortenRejectsBadInputEndToEnd checks that bad input becomes a 400
// through the real chain: the handler rejects bad JSON itself, and the
// real service rejects bad URLs. The unit tests check each part alone;
// this checks they are connected correctly.
func TestShortenRejectsBadInputEndToEnd(t *testing.T) {
	srv := newTestServer(t)
	bodies := []string{
		`not json`,
		`{}`,
		`{"url":"go.dev"}`,
		`{"url":"ftp://go.dev/file"}`,
		`{"url":"https://"}`,
	}
	for _, body := range bodies {
		t.Run(body, func(t *testing.T) {
			res, err := http.Post(srv.URL+"/shorten", "application/json", strings.NewReader(body))
			if err != nil {
				t.Fatalf("POST /shorten: %v", err)
			}
			res.Body.Close()

			if res.StatusCode != http.StatusBadRequest {
				t.Errorf("body %q: status = %d, want %d", body, res.StatusCode, http.StatusBadRequest)
			}
		})
	}
}

// TestRouting checks how the router answers requests that don't match a
// route exactly.
func TestRouting(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"wrong method on /shorten", http.MethodPut, "/shorten", http.StatusMethodNotAllowed},
		{"wrong method on /health", http.MethodPost, "/health", http.StatusMethodNotAllowed},
		{"wrong method on a code", http.MethodDelete, "/abc123", http.StatusMethodNotAllowed},
		// GET /shorten is not a 405: it matches "GET /{code}" with
		// code "shorten", which doesn't exist, so the answer is 404.
		{"GET /shorten is treated as a code", http.MethodGet, "/shorten", http.StatusNotFound},
		{"root path", http.MethodGet, "/", http.StatusNotFound},
		{"nested path", http.MethodGet, "/abc/def", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t)
			req, err := http.NewRequest(tt.method, srv.URL+tt.path, nil)
			if err != nil {
				t.Fatalf("setup: build request: %v", err)
			}

			res, err := noRedirectClient().Do(req)
			if err != nil {
				t.Fatalf("%s %s: %v", tt.method, tt.path, err)
			}
			res.Body.Close()

			if res.StatusCode != tt.wantStatus {
				t.Errorf("%s %s: status = %d, want %d", tt.method, tt.path, res.StatusCode, tt.wantStatus)
			}
		})
	}
}

// TestManyLinks checks that 100 links all get different codes and each one
// still points at its own URL.
func TestManyLinks(t *testing.T) {
	srv := newTestServer(t)
	client := noRedirectClient()
	codeToURL := make(map[string]string)

	for i := range 100 {
		target := fmt.Sprintf("https://example.com/page/%d", i)
		resp := shortenOverHTTP(t, srv, target)
		if prev, dup := codeToURL[resp.Code]; dup {
			t.Fatalf("code %q was returned for both %q and %q", resp.Code, prev, target)
		}
		codeToURL[resp.Code] = target
	}

	for code, want := range codeToURL {
		res, err := client.Get(srv.URL + "/" + code)
		if err != nil {
			t.Fatalf("GET /%s: %v", code, err)
		}
		res.Body.Close()
		if got := res.Header.Get("Location"); got != want {
			t.Errorf("GET /%s: Location = %q, want %q", code, got, want)
		}
	}
}
