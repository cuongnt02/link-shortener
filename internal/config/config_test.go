package config_test

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cuongnt02/link-shortener/internal/config"
)

// Unit tests for configuration. Load reads settings through a getenv
// function instead of os.Getenv directly, so each test passes its own
// fake environment: no real environment variables are touched, and tests
// can't affect each other.

// envFrom returns a getenv function backed by a map. Missing keys return
// "", exactly like os.Getenv does for unset variables.
func envFrom(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

func mustLoad(t *testing.T, vars map[string]string) config.Config {
	t.Helper()
	cfg, err := config.Load(envFrom(vars))
	if err != nil {
		t.Fatalf("Load(%v) returned error: %v", vars, err)
	}
	return cfg
}

// TestDefaults checks that an empty environment gives a working local setup.
// An empty Host means "listen on all network interfaces".
func TestDefaults(t *testing.T) {
	cfg := mustLoad(t, nil)

	want := config.Config{
		Host:            "",
		Port:            "8080",
		DBPath:          "links.db",
		BaseURL:         "http://localhost:8080",
		ShutdownTimeout: 10 * time.Second,
		LogLevel:        slog.LevelInfo,
	}
	if cfg != want {
		t.Errorf("Load(empty env) =\n  %+v\nwant\n  %+v", cfg, want)
	}
}

// TestAllValuesSet checks that every variable is read into its field.
func TestAllValuesSet(t *testing.T) {
	cfg := mustLoad(t, map[string]string{
		"HOST":             "127.0.0.1",
		"PORT":             "9090",
		"DB_PATH":          "/data/prod.db",
		"BASE_URL":         "https://sho.rt",
		"SHUTDOWN_TIMEOUT": "30s",
		"LOG_LEVEL":        "debug",
	})

	want := config.Config{
		Host:            "127.0.0.1",
		Port:            "9090",
		DBPath:          "/data/prod.db",
		BaseURL:         "https://sho.rt",
		ShutdownTimeout: 30 * time.Second,
		LogLevel:        slog.LevelDebug,
	}
	if cfg != want {
		t.Errorf("Load =\n  %+v\nwant\n  %+v", cfg, want)
	}
}

// TestBaseURLDefaultFollowsPort: if only PORT is set, the default base URL
// must use that port, or every short_url would point at the wrong port.
func TestBaseURLDefaultFollowsPort(t *testing.T) {
	cfg := mustLoad(t, map[string]string{"PORT": "9000"})

	if want := "http://localhost:9000"; cfg.BaseURL != want {
		t.Errorf("BaseURL = %q, want %q", cfg.BaseURL, want)
	}
}

// TestBaseURLTrailingSlashRemoved: the handler builds short URLs as
// BaseURL + "/" + code, so a trailing slash would produce "//".
func TestBaseURLTrailingSlashRemoved(t *testing.T) {
	cfg := mustLoad(t, map[string]string{"BASE_URL": "https://sho.rt/"})

	if want := "https://sho.rt"; cfg.BaseURL != want {
		t.Errorf("BaseURL = %q, want %q", cfg.BaseURL, want)
	}
}

func TestLogLevels(t *testing.T) {
	tests := []struct {
		value string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			cfg := mustLoad(t, map[string]string{"LOG_LEVEL": tt.value})
			if cfg.LogLevel != tt.want {
				t.Errorf("LOG_LEVEL=%q: LogLevel = %v, want %v", tt.value, cfg.LogLevel, tt.want)
			}
		})
	}
}

// TestInvalidValues checks that bad settings are refused at startup, and
// that the error names the variable, so whoever deploys the app knows
// exactly what to fix.
func TestInvalidValues(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{"PORT", "abc"},
		{"PORT", "0"},
		{"PORT", "-1"},
		{"PORT", "70000"},
		{"BASE_URL", "not a url"},
		{"BASE_URL", "ftp://sho.rt"},
		{"BASE_URL", "https://"},
		{"SHUTDOWN_TIMEOUT", "soon"},
		{"SHUTDOWN_TIMEOUT", "10"}, // a number without a unit is ambiguous
		{"SHUTDOWN_TIMEOUT", "0s"},
		{"SHUTDOWN_TIMEOUT", "-5s"},
		{"LOG_LEVEL", "loud"},
	}
	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			_, err := config.Load(envFrom(map[string]string{tt.key: tt.value}))

			if err == nil {
				t.Fatalf("Load with %s=%q returned no error", tt.key, tt.value)
			}
			if !strings.Contains(err.Error(), tt.key) {
				t.Errorf("error %q does not mention %s", err, tt.key)
			}
		})
	}
}

// TestLoadUsesOnlyGetenv checks that Load never reads the real
// environment behind the caller's back: a real PORT variable is set, but
// the getenv passed in says nothing, so the default must win.
func TestLoadUsesOnlyGetenv(t *testing.T) {
	t.Setenv("PORT", "1234")

	cfg := mustLoad(t, nil)

	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want %q: Load read the real environment instead of getenv", cfg.Port, "8080")
	}
}
