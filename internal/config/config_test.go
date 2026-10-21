package config

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

// envKeys lists every variable Load reads, so each test starts from a known state.
var envKeys = []string{
	"APP_ENV",
	"HTTP_ADDR",
	"LOG_LEVEL",
	"READ_HEADER_TIMEOUT",
	"SHUTDOWN_TIMEOUT",
}

// ConfigSuite groups the configuration tests. SetupTest clears the environment
// before every test so that host settings cannot leak into the results.
type ConfigSuite struct {
	suite.Suite
}

func TestConfigSuite(t *testing.T) {
	suite.Run(t, new(ConfigSuite))
}

func (s *ConfigSuite) SetupTest() {
	for _, key := range envKeys {
		s.T().Setenv(key, "")
	}
}

func (s *ConfigSuite) TestLoadDefaults() {
	cfg, err := Load()
	s.Require().NoError(err)

	s.Equal(EnvDevelopment, cfg.Env)
	s.Equal(":8080", cfg.HTTPAddr)
	s.Equal(slog.LevelInfo, cfg.LogLevel)
	s.Equal(5*time.Second, cfg.ReadHeaderTimeout)
	s.Equal(10*time.Second, cfg.ShutdownTimeout)
}

func (s *ConfigSuite) TestLoadOverrides() {
	s.T().Setenv("APP_ENV", EnvProduction)
	s.T().Setenv("HTTP_ADDR", "127.0.0.1:9000")
	s.T().Setenv("LOG_LEVEL", "debug")
	s.T().Setenv("READ_HEADER_TIMEOUT", "2s")
	s.T().Setenv("SHUTDOWN_TIMEOUT", "90s")

	cfg, err := Load()
	s.Require().NoError(err)

	s.Equal(EnvProduction, cfg.Env)
	s.Equal("127.0.0.1:9000", cfg.HTTPAddr)
	s.Equal(slog.LevelDebug, cfg.LogLevel)
	s.Equal(2*time.Second, cfg.ReadHeaderTimeout)
	s.Equal(90*time.Second, cfg.ShutdownTimeout)
}

func (s *ConfigSuite) TestLogLevel() {
	tests := []struct {
		name  string
		value string
		want  slog.Level
	}{
		{name: "empty falls back to info", value: "", want: slog.LevelInfo},
		{name: "info", value: "info", want: slog.LevelInfo},
		{name: "debug", value: "debug", want: slog.LevelDebug},
		{name: "warn", value: "warn", want: slog.LevelWarn},
		{name: "warning alias is accepted", value: "WARNING", want: slog.LevelWarn},
		{name: "error", value: "error", want: slog.LevelError},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.T().Setenv("LOG_LEVEL", tt.value)

			cfg, err := Load()
			s.Require().NoError(err)
			s.Equal(tt.want, cfg.LogLevel)
		})
	}
}

func (s *ConfigSuite) TestLoadRejectsInvalidValues() {
	tests := []struct {
		name       string
		env        map[string]string
		wantErrors []string
	}{
		{
			name:       "unknown environment",
			env:        map[string]string{"APP_ENV": "staging"},
			wantErrors: []string{"APP_ENV"},
		},
		{
			name:       "address without port",
			env:        map[string]string{"HTTP_ADDR": "8080"},
			wantErrors: []string{"HTTP_ADDR"},
		},
		{
			name:       "port out of range",
			env:        map[string]string{"HTTP_ADDR": ":70000"},
			wantErrors: []string{"HTTP_ADDR"},
		},
		{
			name:       "unknown log level",
			env:        map[string]string{"LOG_LEVEL": "verbose"},
			wantErrors: []string{"LOG_LEVEL"},
		},
		{
			name:       "malformed duration",
			env:        map[string]string{"READ_HEADER_TIMEOUT": "fast"},
			wantErrors: []string{"READ_HEADER_TIMEOUT"},
		},
		{
			name:       "non-positive duration",
			env:        map[string]string{"SHUTDOWN_TIMEOUT": "-1s"},
			wantErrors: []string{"SHUTDOWN_TIMEOUT"},
		},
		{
			name: "several problems are reported together",
			env: map[string]string{
				"APP_ENV":   "staging",
				"HTTP_ADDR": "not-an-address",
				"LOG_LEVEL": "verbose",
			},
			wantErrors: []string{"APP_ENV", "HTTP_ADDR", "LOG_LEVEL"},
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			for key, value := range tt.env {
				s.T().Setenv(key, value)
			}

			_, err := Load()
			s.Require().Error(err)
			for _, want := range tt.wantErrors {
				s.ErrorContains(err, want)
			}
		})
	}
}
