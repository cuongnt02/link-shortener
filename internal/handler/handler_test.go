package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cuongnt02/link-shortener/internal/handler"
	"github.com/cuongnt02/link-shortener/internal/link"
)

// Unit tests: each test calls one handler method directly with a fake
// request (httptest.NewRequest) and records the response
// (httptest.NewRecorder). The handler's link service is replaced by a
// fake, so these tests only check the handler's own job: turning HTTP
// into service calls, and service results into HTTP responses.

// The real service must satisfy the handler's interface. If the method
// signatures drift apart, this package fails to compile.
var _ handler.LinkService = (*link.Service)(nil)

const testBaseURL = "http://short.test"

// shortenResponse mirrors the JSON body that POST /shorten returns.
type shortenResponse struct {
	Code     string `json:"code"`
	ShortURL string `json:"short_url"`
}

// errInternal stands in for an unexpected failure. Its message contains
// details that must never be shown to clients.
var errInternal = errors.New("db password=hunter2 connection refused")

// fakeService is a handler.LinkService that returns preset results and
// records what the handler passed in.
type fakeService struct {
	shortenCode string
	shortenErr  error
	resolveURL  string
	resolveErr  error

	shortenCalls int
	gotURL       string
	resolveCalls int
	gotCode      string
}

func (f *fakeService) Shorten(_ context.Context, rawURL string) (string, error) {
	f.shortenCalls++
	f.gotURL = rawURL
	return f.shortenCode, f.shortenErr
}

func (f *fakeService) Resolve(_ context.Context, code string) (string, error) {
	f.resolveCalls++
	f.gotCode = code
	return f.resolveURL, f.resolveErr
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

// assertNoLeak checks that an error response doesn't expose internal details.
func assertNoLeak(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if body := rec.Body.String(); strings.Contains(body, "hunter2") {
		t.Errorf("response body leaks internal error details: %q", body)
	}
}

func TestHealth(t *testing.T) {
	h := handler.New(&fakeService{}, testBaseURL)
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

// TestShortenSuccess checks the handler passes the url to the service and
// builds the 201 JSON response from the code the service returns.
func TestShortenSuccess(t *testing.T) {
	svc := &fakeService{shortenCode: "abc123"}
	h := handler.New(svc, testBaseURL)

	rec := callShorten(t, h, `{"url":"https://go.dev"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want prefix %q", ct, "application/json")
	}
	if svc.gotURL != "https://go.dev" {
		t.Errorf("service received url %q, want %q", svc.gotURL, "https://go.dev")
	}
	var resp shortenResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if resp.Code != "abc123" {
		t.Errorf("code = %q, want %q", resp.Code, "abc123")
	}
	if want := testBaseURL + "/abc123"; resp.ShortURL != want {
		t.Errorf("short_url = %q, want %q", resp.ShortURL, want)
	}
}

// TestShortenBadJSON checks that a body the handler can't decode is
// rejected with 400 before the service is ever called.
func TestShortenBadJSON(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"empty body", ``},
		{"not json", `hello`},
		{"malformed json", `{"url":`},
		{"url is not a string", `{"url":123}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{shortenCode: "abc123"}
			h := handler.New(svc, testBaseURL)

			rec := callShorten(t, h, tt.body)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("body %q: status = %d, want %d", tt.body, rec.Code, http.StatusBadRequest)
			}
			if svc.shortenCalls != 0 {
				t.Errorf("service called %d times, want 0 for undecodable JSON", svc.shortenCalls)
			}
		})
	}
}

// TestShortenServiceErrors checks how each kind of service error maps to
// an HTTP status. Errors are wrapped the way a real service would wrap
// them, so the handler must use errors.Is, not ==.
func TestShortenServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"invalid url", link.ErrInvalidURL, http.StatusBadRequest},
		{"wrapped invalid url", fmt.Errorf("%w: scheme %q not allowed", link.ErrInvalidURL, "ftp"), http.StatusBadRequest},
		{"unexpected error", errInternal, http.StatusInternalServerError},
		{"wrapped unexpected error", fmt.Errorf("saving link: %w", errInternal), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := handler.New(&fakeService{shortenErr: tt.err}, testBaseURL)

			rec := callShorten(t, h, `{"url":"https://go.dev"}`)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			assertNoLeak(t, rec)
		})
	}
}

func TestRedirectKnownCode(t *testing.T) {
	svc := &fakeService{resolveURL: "https://go.dev/doc"}
	h := handler.New(svc, testBaseURL)

	rec := callRedirect(t, h, "abc123")

	if svc.gotCode != "abc123" {
		t.Errorf("service received code %q, want %q", svc.gotCode, "abc123")
	}
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if got, want := rec.Header().Get("Location"), "https://go.dev/doc"; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
}

// TestRedirectServiceErrors checks the status for each kind of service
// error, and that no redirect is sent when resolving failed.
func TestRedirectServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"not found", link.ErrNotFound, http.StatusNotFound},
		{"wrapped not found", fmt.Errorf("resolve %q: %w", "nope42", link.ErrNotFound), http.StatusNotFound},
		{"unexpected error", errInternal, http.StatusInternalServerError},
		{"wrapped unexpected error", fmt.Errorf("loading link: %w", errInternal), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := handler.New(&fakeService{resolveErr: tt.err}, testBaseURL)

			rec := callRedirect(t, h, "nope42")

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if loc := rec.Header().Get("Location"); loc != "" {
				t.Errorf("Location = %q, want no Location header", loc)
			}
			assertNoLeak(t, rec)
		})
	}
}
