package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

// System tests: build the real binary with `go build`, start it as a
// separate process configured only through environment variables, and
// talk to it over HTTP, exactly like a deployment would. These are the
// slowest tests, so `go test -short` skips them.
//
// The process is stopped with Kill (a hard stop, like a crash or a power
// cut), because Windows can't send Ctrl+C to another process from Go.
// Graceful shutdown is tested in-process in main_test.go instead.

var (
	buildOnce sync.Once
	binPath   string
	buildErr  error
)

// TestMain runs all tests in this package, then removes the built binary.
func TestMain(m *testing.M) {
	code := m.Run()
	if binPath != "" {
		os.RemoveAll(filepath.Dir(binPath))
	}
	os.Exit(code)
}

// buildBinary compiles this package once per test run and returns the
// path to the executable.
func buildBinary(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping system test in -short mode")
	}
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "link-shortener-bin")
		if err != nil {
			buildErr = err
			return
		}
		name := "server"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		binPath = filepath.Join(dir, name)
		out, err := exec.Command("go", "build", "-o", binPath, ".").CombinedOutput()
		if err != nil {
			buildErr = &buildError{err: err, output: string(out)}
		}
	})
	if buildErr != nil {
		t.Fatalf("building the server: %v", buildErr)
	}
	return binPath
}

type buildError struct {
	err    error
	output string
}

func (e *buildError) Error() string { return e.err.Error() + "\n" + e.output }

// syncBuffer is a bytes.Buffer that is safe to write from the goroutine
// exec uses to copy the process's output, while the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.buf.Bytes())
}

// startBinary starts the server process with env (added on top of the
// real environment) and waits until it is healthy. The returned stop
// function kills the process and waits for it to exit.
func startBinary(t *testing.T, env map[string]string) (stdout *syncBuffer, stop func()) {
	t.Helper()
	cmd := exec.Command(buildBinary(t))
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	stdout = &syncBuffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stdout
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting server process: %v", err)
	}

	// exited is closed when the process ends, so any number of readers
	// (waitHealthy and stop) can wait on it.
	exited := make(chan error)
	go func() {
		cmd.Wait()
		close(exited)
	}()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cmd.Process.Kill()
			<-exited
		})
	}
	t.Cleanup(stop)

	waitHealthy(t, env["BASE_URL"], exited)
	return stdout, stop
}

// TestSystemShortenAndRedirect checks the built program end to end, with
// every setting coming from environment variables.
func TestSystemShortenAndRedirect(t *testing.T) {
	env := testEnv(t)
	startBinary(t, env)

	code, shortURL := shorten(t, env["BASE_URL"], "https://go.dev")

	if want := env["BASE_URL"] + "/" + code; shortURL != want {
		t.Errorf("short_url = %q, want %q", shortURL, want)
	}
	assertRedirects(t, env["BASE_URL"], code, "https://go.dev")
}

// TestSystemLinksSurviveCrash hard-kills the process, with no chance to
// clean up, then starts a new one on the same database. Links must still
// be there: committed data must survive a crash.
func TestSystemLinksSurviveCrash(t *testing.T) {
	env := testEnv(t)
	_, stop := startBinary(t, env)
	code, _ := shorten(t, env["BASE_URL"], "https://go.dev/doc")
	stop()

	port := freePort(t)
	env["PORT"], env["BASE_URL"] = port, "http://127.0.0.1:"+port
	startBinary(t, env)

	assertRedirects(t, env["BASE_URL"], code, "https://go.dev/doc")
}

// TestSystemLogsAreJSON checks that everything the program prints is one
// JSON object per line with at least "time", "level" and "msg", so log
// tools can parse it.
func TestSystemLogsAreJSON(t *testing.T) {
	env := testEnv(t)
	stdout, stop := startBinary(t, env)
	shorten(t, env["BASE_URL"], "https://go.dev")
	stop()

	out := stdout.Bytes()
	lines := bytes.Split(bytes.TrimSpace(out), []byte("\n"))
	if len(lines) == 0 || len(lines[0]) == 0 {
		t.Fatalf("server printed nothing, want at least a startup log line")
	}
	for i, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal(bytes.TrimSpace(line), &entry); err != nil {
			t.Errorf("line %d is not JSON: %q", i+1, line)
			continue
		}
		for _, key := range []string{"time", "level", "msg"} {
			if _, ok := entry[key]; !ok {
				t.Errorf("line %d has no %q field: %q", i+1, key, line)
			}
		}
	}
}

// TestSystemBadConfigExitsWithError checks that the program refuses to
// start with an invalid setting, exits with a non-zero code, and says why.
func TestSystemBadConfigExitsWithError(t *testing.T) {
	env := testEnv(t)
	env["PORT"] = "not-a-port"
	cmd := exec.Command(buildBinary(t))
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	out, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("program exited with %v, want a non-zero exit code; output: %q", err, out)
	}
	if !bytes.Contains(out, []byte("PORT")) {
		t.Errorf("output %q does not mention PORT", out)
	}
}
