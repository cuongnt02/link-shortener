package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// In-process tests for run: the whole app starts inside the test process,
// with its own fake environment, a free port and a temporary database.
// Stopping it is just canceling the context, the same thing Ctrl+C does
// in the real program through signal.NotifyContext.

// freePort asks the OS for a port nobody is using, then releases it so the
// app can take it. There's a tiny chance another program grabs it first;
// that's acceptable for tests.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	defer l.Close()
	return fmt.Sprint(l.Addr().(*net.TCPAddr).Port)
}

// testEnv returns a valid environment: a free port and a database in a
// temporary folder. Tests change single entries to break things on purpose.
func testEnv(t *testing.T) map[string]string {
	t.Helper()
	port := freePort(t)
	return map[string]string{
		// Listen on loopback only: it is all the tests need, and Windows
		// Firewall does not prompt for loopback-only servers.
		"HOST":             "127.0.0.1",
		"PORT":             port,
		"DB_PATH":          filepath.Join(t.TempDir(), "links.db"),
		"BASE_URL":         "http://127.0.0.1:" + port,
		"SHUTDOWN_TIMEOUT": "15s",
		"LOG_LEVEL":        "debug",
	}
}

// startRun runs the app in a goroutine. It returns a cancel function that
// stops the app, and a channel that receives run's return value.
func startRun(t *testing.T, env map[string]string) (stop context.CancelFunc, done <-chan error) {
	t.Helper()
	return startRunWithOutput(t, env, io.Discard)
}

// startRunWithOutput is startRun, but run writes its logs to stdout.
func startRunWithOutput(t *testing.T, env map[string]string, stdout io.Writer) (stop context.CancelFunc, done <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	errc := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		errc <- run(ctx, func(key string) string { return env[key] }, stdout)
		close(finished)
	}()
	// At the end of the test, stop the app and wait until run has really
	// returned. Otherwise the temporary folder would be deleted while the
	// database file is still open, which fails on Windows.
	t.Cleanup(func() {
		cancel()
		<-finished
	})
	// Before stopping, close the test's own idle keep-alive connections.
	// Shutdown waits up to 5 seconds for a connection that was opened but
	// never sent a request, and Go's HTTP client sometimes opens a spare
	// one. Without this, a slow machine could hit the shutdown timeout.
	stop = func() {
		http.DefaultClient.CloseIdleConnections()
		cancel()
	}
	return stop, errc
}

// waitHealthy polls GET /health until it answers 200, or fails the test.
// The server starts in the background, so the first requests may be
// refused. If run exits early instead, its error is reported.
func waitHealthy(t *testing.T, baseURL string, done <-chan error) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("run exited before the server became healthy: %v", err)
		default:
		}
		res, err := http.Get(baseURL + "/health")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("server at %s did not become healthy within 10s", baseURL)
}

// waitDone waits for run to return and gives back its error.
func waitDone(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return within 10s")
		return nil
	}
}

// shorten sends POST /shorten and returns the code and short URL.
func shorten(t *testing.T, baseURL, target string) (code, shortURL string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"url": target})
	res, err := http.Post(baseURL+"/shorten", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST /shorten: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("POST /shorten status = %d, want %d", res.StatusCode, http.StatusCreated)
	}
	var resp struct {
		Code     string `json:"code"`
		ShortURL string `json:"short_url"`
	}
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding POST /shorten body: %v", err)
	}
	return resp.Code, resp.ShortURL
}

// TestRunServesAndShutsDownCleanly starts the app, uses it, stops it, and
// checks that run returns nil and that the database was closed properly.
func TestRunServesAndShutsDownCleanly(t *testing.T) {
	env := testEnv(t)
	stop, done := startRun(t, env)
	waitHealthy(t, env["BASE_URL"], done)

	code, shortURL := shorten(t, env["BASE_URL"], "https://go.dev")
	if want := env["BASE_URL"] + "/" + code; shortURL != want {
		t.Errorf("short_url = %q, want %q (BASE_URL from the environment)", shortURL, want)
	}

	stop()

	if err := waitDone(t, done); err != nil {
		t.Fatalf("run returned %v after shutdown, want nil", err)
	}
	// The database must be the file named by DB_PATH, nowhere else.
	if _, err := os.Stat(env["DB_PATH"]); err != nil {
		t.Errorf("database file %s not found (%v): DB_PATH was not used", env["DB_PATH"], err)
	}
	// SQLite deletes the write-ahead log file when the last connection
	// closes cleanly. If it's still there, the store was never closed.
	if _, err := os.Stat(env["DB_PATH"] + "-wal"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s-wal still exists after shutdown (stat error: %v): the database was not closed", env["DB_PATH"], err)
	}
	// After shutdown, the port must be released: nobody is listening anymore.
	if res, err := http.Get(env["BASE_URL"] + "/health"); err == nil {
		res.Body.Close()
		t.Errorf("server still answering after shutdown")
	}
}

// TestRunLinksPersistAcrossRestarts runs the app twice on the same database.
func TestRunLinksPersistAcrossRestarts(t *testing.T) {
	env := testEnv(t)

	stop, done := startRun(t, env)
	waitHealthy(t, env["BASE_URL"], done)
	code, _ := shorten(t, env["BASE_URL"], "https://go.dev/doc")
	stop()
	if err := waitDone(t, done); err != nil {
		t.Fatalf("first run returned %v, want nil", err)
	}

	// Same database file, but a new port for the second run.
	port := freePort(t)
	env["PORT"], env["BASE_URL"] = port, "http://127.0.0.1:"+port
	_, done = startRun(t, env)
	waitHealthy(t, env["BASE_URL"], done)

	assertRedirects(t, env["BASE_URL"], code, "https://go.dev/doc")
}

// TestRunFailsFast checks that startup problems make run return an error
// quickly, instead of hanging or starting a broken server.
func TestRunFailsFast(t *testing.T) {
	tests := []struct {
		name     string
		breakEnv func(t *testing.T, env map[string]string)
	}{
		{"invalid config", func(t *testing.T, env map[string]string) {
			env["PORT"] = "not-a-port"
		}},
		{"database folder missing", func(t *testing.T, env map[string]string) {
			env["DB_PATH"] = filepath.Join(t.TempDir(), "missing", "links.db")
		}},
		{"port already in use", func(t *testing.T, env map[string]string) {
			l, err := net.Listen("tcp", "127.0.0.1:"+env["PORT"])
			if err != nil {
				t.Fatalf("setup: occupying port: %v", err)
			}
			t.Cleanup(func() { l.Close() })
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testEnv(t)
			tt.breakEnv(t, env)

			_, done := startRun(t, env)

			if err := waitDone(t, done); err == nil {
				t.Errorf("run returned nil, want an error")
			}
		})
	}
}

// assertRedirects checks that GET /{code} answers 302 to wantURL.
func assertRedirects(t *testing.T, baseURL, code, wantURL string) {
	t.Helper()
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	res, err := client.Get(baseURL + "/" + code)
	if err != nil {
		t.Fatalf("GET /%s: %v", code, err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusFound {
		t.Errorf("GET /%s: status = %d, want %d", code, res.StatusCode, http.StatusFound)
	}
	if got := res.Header.Get("Location"); got != wantURL {
		t.Errorf("GET /%s: Location = %q, want %q", code, got, wantURL)
	}
}

// TestRunRespectsLogLevel starts the app with LOG_LEVEL=error, so the
// usual INFO lines (like "listening" and "stopped") must not be printed.
// With LOG_LEVEL=debug, they must be.
func TestRunRespectsLogLevel(t *testing.T) {
	tests := []struct {
		level    string
		wantInfo bool
	}{
		{"error", false},
		{"debug", true},
	}
	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			env := testEnv(t)
			env["LOG_LEVEL"] = tt.level
			out := &syncBuffer{}
			stop, done := startRunWithOutput(t, env, out)
			waitHealthy(t, env["BASE_URL"], done)
			stop()
			if err := waitDone(t, done); err != nil {
				t.Fatalf("run returned %v, want nil", err)
			}

			gotInfo := bytes.Contains(out.Bytes(), []byte(`"level":"INFO"`))
			if gotInfo != tt.wantInfo {
				t.Errorf("LOG_LEVEL=%s: INFO lines printed = %v, want %v; output:\n%s", tt.level, gotInfo, tt.wantInfo, out.Bytes())
			}
		})
	}
}
