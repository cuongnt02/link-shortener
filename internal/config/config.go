package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Config defines environment properties of the application
type Config struct {
	Host            string
	Port            string
	DBPath          string
	BaseURL         string
	ShutdownTimeout time.Duration
	LogLevel        slog.Level
}

// Load reads the configuration through getenv, applies defaults, and validates every value
func Load(getenv func(string) string) (Config, error) {
	host := readConfig(getenv, "HOST", "")
	port := readConfig(getenv, "PORT", "8080")
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		return Config{}, fmt.Errorf("invalid PORT provided: %w", err)
	}
	if portNumber < 1 || portNumber > 65535 {
		return Config{}, fmt.Errorf("PORT %q must be between 1 and 65535", port)
	}
	port = strconv.Itoa(portNumber)

	dbPath := readConfig(getenv, "DB_PATH", "links.db")

	baseURL := readConfig(getenv, "BASE_URL", "http://localhost:"+port)
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return Config{}, fmt.Errorf("BASE_URL %q: is not a valid url: %w", baseURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return Config{}, fmt.Errorf("BASE_URL %q: scheme must be http or https", baseURL)
	}
	if parsed.Host == "" {
		return Config{}, fmt.Errorf("BASE_URL %q: provided host should not be empty", baseURL)
	}

	timeout := readConfig(getenv, "SHUTDOWN_TIMEOUT", "10s")
	timeoutDuration, err := time.ParseDuration(timeout)
	if err != nil {
		return Config{}, fmt.Errorf("invalid SHUTDOWN_TIMEOUT provided: %w", err)
	}

	if timeoutDuration <= 0 {
		return Config{}, fmt.Errorf("SHUTDOWN_TIMEOUT provided must be greater than zero: %v", timeoutDuration)
	}

	var logLevel slog.Level
	logLevelEnv := readConfig(getenv, "LOG_LEVEL", "info")
	err = logLevel.UnmarshalText([]byte(logLevelEnv))
	if err != nil {
		return Config{},
			fmt.Errorf("invalid LOG_LEVEL provided: %w", err)
	}
	cfg := Config{
		Host:            host,
		Port:            port,
		DBPath:          dbPath,
		BaseURL:         strings.TrimRight(baseURL, "/"),
		ShutdownTimeout: timeoutDuration,
		LogLevel:        logLevel,
	}

	return cfg, nil
}

func readConfig(getenv func(string) string, env string, defaultValue string) string {
	attr := getenv(env)
	if attr == "" {
		attr = defaultValue
	}
	return attr
}
