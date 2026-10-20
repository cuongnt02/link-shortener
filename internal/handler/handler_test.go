package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/cuongnt02/link-shortener/internal/handler"
	"github.com/cuongnt02/link-shortener/internal/store"
)

// Unit tests: each test calls one handler method directly with a fake
// request (httptest.NewRequest) and records the response
// (httptest.NewRecorder). No router and no network are involved.

const testBaseURL = "http://short.test"

// codePattern is what every generated code must look like:
// exactly 6 characters from the base62 alphabet.
var codePattern = regexp.MustCompile(`^[A-Za-z0-9]{6}$`)

// shortenResponse mirrors the JSON body that POST /shorten returns.
type shortenResponse struct {
	Code     string `json:"code"`
	ShortURL string `json:"short_url"`
}

// newTestHandler returns a handler backed by a fresh, empty store.
// The store is returned too, so tests can check what was saved.
func newTestHandler(t *testing.T) (*handler.Handler, *store.MemoryStore) {
	t.Helper()
	s := store.NewInMemoryStore()
	return handler.New(s, testBaseURL), s
}

// callShorten sends body to the Shorten handler and returns the recorded response.
func callShorten(t *testing.T, h *handler.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/shorten", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Shorten(rec, req)
	return rec
}

// callRedirect sends GET /{code} to the Redirect handler. Without a router,
// nothing fills in the {code} path value, so the test sets it by hand.
func callRedirect(t *testing.T, h *handler.Handler, code string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/"+code, nil)
	req.SetPathValue("code", code)
	rec := httptest.NewRecorder()
	h.Redirect(rec, req)
	return rec
}

// decodeShorten checks for a 201 JSON response and decodes its body.
func decodeShorten(t *testing.T, rec *httptest.ResponseRecorder) shortenResponse {
	t.Helper()
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want prefix %q", ct, "application/json")
	}
	var resp shortenResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	return resp
}

func TestHealth(t *testing.T) {
	h, _ := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	h.Health(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "ok" {
		t.Errorf("body = %q, want %q", got, "ok")
	}
}

// TestShortenValidURL checks the full success response, and that the link
// was really saved in the store.
func TestShortenValidURL(t *testing.T) {
	h, s := newTestHandler(t)

	resp := decodeShorten(t, callShorten(t, h, `{"url":"https://go.dev"}`))

	if !codePattern.MatchString(resp.Code) {
		t.Errorf("code = %q, want 6 base62 characters", resp.Code)
	}
	if want := testBaseURL + "/" + resp.Code; resp.ShortURL != want {
		t.Errorf("short_url = %q, want %q", resp.ShortURL, want)
	}
	got, err := s.Get(resp.Code)
	if err != nil {
		t.Fatalf("store.Get(%q) after shorten returned error: %v", resp.Code, err)
	}
	if want := "https://go.dev"; got != want {
		t.Errorf("store.Get(%q) = %q, want %q", resp.Code, got, want)
	}
}

// TestShortenAcceptsURLVariants checks that real-world URLs with ports,
// paths, query strings and fragments are accepted unchanged.
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
			h, s := newTestHandler(t)
			body, err := json.Marshal(map[string]string{"url": u})
			if err != nil {
				t.Fatalf("setup: marshal request: %v", err)
			}

			resp := decodeShorten(t, callShorten(t, h, string(body)))

			got, err := s.Get(resp.Code)
			if err != nil {
				t.Fatalf("store.Get(%q) returned error: %v", resp.Code, err)
			}
			if got != u {
				t.Errorf("stored url = %q, want %q", got, u)
			}
		})
	}
}

// TestShortenSameURLTwice checks that every request creates a new link,
// even for a URL that was shortened before.
func TestShortenSameURLTwice(t *testing.T) {
	h, _ := newTestHandler(t)
	body := `{"url":"https://go.dev"}`

	first := decodeShorten(t, callShorten(t, h, body))
	second := decodeShorten(t, callShorten(t, h, body))

	if first.Code == second.Code {
		t.Errorf("both requests returned code %q, want two different codes", first.Code)
	}
}

// TestShortenRejectsBadInput checks that every invalid request gets
// 400 Bad Request.
func TestShortenRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"empty body", ``},
		{"not json", `hello`},
		{"malformed json", `{"url":`},
		{"url field missing", `{}`},
		{"url is empty", `{"url":""}`},
		{"url is not a string", `{"url":123}`},
		{"relative path", `{"url":"/just/a/path"}`},
		{"no scheme", `{"url":"go.dev"}`},
		{"ftp scheme", `{"url":"ftp://go.dev/file"}`},
		{"javascript scheme", `{"url":"javascript:alert(1)"}`},
		{"scheme without host", `{"url":"https://"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _ := newTestHandler(t)

			rec := callShorten(t, h, tt.body)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("body %q: status = %d, want %d", tt.body, rec.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestRedirectKnownCode(t *testing.T) {
	h, s := newTestHandler(t)
	if err := s.Save("abc123", "https://go.dev/doc"); err != nil {
		t.Fatalf("setup: Save returned error: %v", err)
	}

	rec := callRedirect(t, h, "abc123")

	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if got, want := rec.Header().Get("Location"), "https://go.dev/doc"; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
}

func TestRedirectUnknownCode(t *testing.T) {
	h, _ := newTestHandler(t)

	rec := callRedirect(t, h, "nope42")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("Location = %q, want no Location header", loc)
	}
}
