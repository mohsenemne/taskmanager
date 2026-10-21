// Package config loads the service configuration from the environment and
// validates it before the process starts.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Supported deployment environments.
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

// Config holds every runtime setting of the service.
//
// Values are parsed and validated in Load, so the rest of the service can use
// them directly instead of re-checking raw strings.
type Config struct {
	// Env selects behaviour that differs between local development and production.
	Env string
	// HTTPAddr is the listen address of the HTTP server, in host:port form.
	HTTPAddr string
	// LogLevel is the minimum level the logger emits.
	LogLevel slog.Level
	// DatabaseURL is the PostgreSQL connection string.
	DatabaseURL string
	// ReadHeaderTimeout bounds how long a client may take to send request headers.
	ReadHeaderTimeout time.Duration
	// ReadTimeout bounds how long a client may take to send a full request.
	ReadTimeout time.Duration
	// WriteTimeout bounds how long a response may take to be written.
	WriteTimeout time.Duration
	// IdleTimeout bounds how long an idle keep-alive connection is kept open.
	IdleTimeout time.Duration
	// ShutdownTimeout is the time budget for in-flight requests on shutdown.
	ShutdownTimeout time.Duration
}

// Load reads the configuration from the process environment and validates it.
// Empty variables fall back to their default, except DATABASE_URL which has
// none. Invalid values are reported together in a single error so a bad
// deployment fails once, not one variable at a time.
func Load() (Config, error) {
	l := &loader{}
	cfg := Config{
		Env:               l.str("APP_ENV", EnvDevelopment),
		HTTPAddr:          l.str("HTTP_ADDR", ":8080"),
		LogLevel:          l.level("LOG_LEVEL", slog.LevelInfo),
		DatabaseURL:       l.str("DATABASE_URL", ""),
		ReadHeaderTimeout: l.duration("READ_HEADER_TIMEOUT", 5*time.Second),
		ReadTimeout:       l.duration("READ_TIMEOUT", 10*time.Second),
		WriteTimeout:      l.duration("WRITE_TIMEOUT", 15*time.Second),
		IdleTimeout:       l.duration("IDLE_TIMEOUT", 60*time.Second),
		ShutdownTimeout:   l.duration("SHUTDOWN_TIMEOUT", 10*time.Second),
	}

	errs := append(l.errs, cfg.validate()...)
	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	return cfg, nil
}

func (c Config) validate() []error {
	var errs []error

	if c.Env != EnvDevelopment && c.Env != EnvProduction {
		errs = append(errs, fmt.Errorf("APP_ENV: %q must be either %q or %q", c.Env, EnvDevelopment, EnvProduction))
	}
	if err := validateHTTPAddr(c.HTTPAddr); err != nil {
		errs = append(errs, err)
	}
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL: must be set, for example postgres://user:pass@localhost:5432/tasks?sslmode=disable"))
	}

	return errs
}

func validateHTTPAddr(addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("HTTP_ADDR: %q must be in host:port form such as :8080 or 127.0.0.1:8080: %w", addr, err)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("HTTP_ADDR: %q has an invalid port", addr)
	}
	return nil
}

func parseLogLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("%q must be one of debug, info, warn or error", raw)
	}
}

// loader reads environment variables and collects parse errors along the way.
type loader struct {
	errs []error
}

func (l *loader) str(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func (l *loader) level(key string, def slog.Level) slog.Level {
	raw := l.str(key, "")
	if raw == "" {
		return def
	}
	level, err := parseLogLevel(raw)
	if err != nil {
		l.errs = append(l.errs, fmt.Errorf("%s: %w", key, err))
		return def
	}
	return level
}

func (l *loader) duration(key string, def time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		l.errs = append(l.errs, fmt.Errorf("%s: %q is not a valid duration such as 5s or 1m: %w", key, raw, err))
		return def
	}
	if d <= 0 {
		l.errs = append(l.errs, fmt.Errorf("%s: %q must be positive", key, raw))
		return def
	}
	return d
}
