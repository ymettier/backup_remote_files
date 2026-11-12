// Copyright 2024-2026 The Backup_remote_files Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package config

import (
	"errors"
	"flag"
	"os"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"backup_remote_files/internal/logger"
	"backup_remote_files/internal/testutil"
)

func defaultKoanf(t *testing.T) *koanf.Koanf {
	t.Helper()
	k := koanf.New(".")
	require.NoError(t, k.Load(rawbytes.Provider(defaultConfig), yaml.Parser()))
	return k
}

func TestVersion(t *testing.T) {
	wantedVersion := "1.2.3"
	output := versionInfo(wantedVersion)
	s := strings.Split(output, "\n")
	assert.Equal(t, "Version        : "+wantedVersion, s[0], "Printing version")
}

func TestFormatVersionInfo(t *testing.T) {
	commitTime := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)

	tests := []struct {
		name     string
		info     *debug.BuildInfo
		contains []string
		absent   []string
	}{
		{
			name:     "no build info",
			info:     nil,
			contains: []string{"Version        : 1.2.3\n"},
			absent:   []string{"Revision", "Dirty Build", "Last Commit", "Go Version"},
		},
		{
			name: "full build info",
			info: &debug.BuildInfo{
				GoVersion: "go1.26.0",
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "abc123"},
					{Key: "vcs.time", Value: commitTime.Format(time.RFC3339)},
					{Key: "vcs.modified", Value: "true"},
				},
			},
			contains: []string{
				"Revision       : abc123",
				"Dirty Build    : true",
				"Last Commit    : " + commitTime.String(),
				"Go Version     : go1.26.0",
			},
		},
		{
			name: "defaults when settings are empty",
			info: &debug.BuildInfo{
				GoVersion: "go1.26.0",
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: ""},
					{Key: "vcs.modified", Value: "false"},
				},
			},
			contains: []string{
				"Revision       : unknown",
				"Dirty Build    : false",
				"Last Commit    : 0001-01-01 00:00:00 +0000 UTC",
			},
		},
		{
			name: "invalid commit time is ignored",
			info: &debug.BuildInfo{
				Settings: []debug.BuildSetting{
					{Key: "vcs.time", Value: "not-a-time"},
				},
			},
			contains: []string{"Last Commit    : 0001-01-01 00:00:00 +0000 UTC"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := formatVersionInfo("1.2.3", tt.info)
			for _, want := range tt.contains {
				assert.Contains(t, output, want)
			}
			for _, unwanted := range tt.absent {
				assert.NotContains(t, output, unwanted)
			}
		})
	}
}

func TestEnvVariableOverrides(t *testing.T) {
	testutil.UseTempDir(t)
	configContent := `
backups:
  test:
    url: "http://example.com"
    username: "user"
    password: "pass"
    outputFile: "/tmp/test.out"
interval: "24h"
retryInterval: "1h"
exporter:
  metricsPrefix: "default_prefix"
`
	err := os.WriteFile("test_env_config.yaml", []byte(configContent), 0600)
	assert.NoError(t, err)

	os.Setenv("BRF_INTERVAL", "2h")
	os.Setenv("BRF_RETRYINTERVAL", "30m")
	os.Setenv("BRF_METRICSPREFIX", "custom_prefix")
	defer func() {
		os.Unsetenv("BRF_INTERVAL")
		os.Unsetenv("BRF_RETRYINTERVAL")
		os.Unsetenv("BRF_METRICSPREFIX")
	}()

	cfg, err := New("test_env_config.yaml", 0, false)
	assert.NoError(t, err)

	assert.Equal(t, "2h0m0s", cfg.Interval.String())
	assert.Equal(t, "30m0s", cfg.RetryInterval.String())
	assert.Equal(t, "custom_prefix", cfg.MetricsPrefix)
}

func TestLoggerConfig(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*koanf.Koanf)
		want     logger.LogOptions
		wantJSON bool
	}{
		{
			name:  "defaults",
			setup: func(*koanf.Koanf) {},
			want: logger.LogOptions{
				Level:      "INFO",
				Filename:   "",
				MaxSize:    5,
				MaxBackups: 10,
				MaxAge:     14,
				Compress:   true,
				JSON:       false,
			},
		},
		{
			name: "overrides",
			setup: func(k *koanf.Koanf) {
				require.NoError(t, k.Set("logging.level", "DEBUG"))
				require.NoError(t, k.Set("logging.filename", "/var/log/test.log"))
				require.NoError(t, k.Set("logging.maxSize", 50))
				require.NoError(t, k.Set("logging.maxBackups", 20))
				require.NoError(t, k.Set("logging.maxAge", 30))
				require.NoError(t, k.Set("logging.compress", false))
				require.NoError(t, k.Set("logging.json", true))
			},
			want: logger.LogOptions{
				Level:      "DEBUG",
				Filename:   "/var/log/test.log",
				MaxSize:    50,
				MaxBackups: 20,
				MaxAge:     30,
				Compress:   false,
				JSON:       true,
			},
		},
		{
			name: "partial overrides",
			setup: func(k *koanf.Koanf) {
				require.NoError(t, k.Set("logging.level", "ERROR"))
				require.NoError(t, k.Set("logging.filename", "stdout"))
			},
			want: logger.LogOptions{
				Level:      "ERROR",
				Filename:   "stdout",
				MaxSize:    5,
				MaxBackups: 10,
				MaxAge:     14,
				Compress:   true,
				JSON:       false,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := defaultKoanf(t)
			tt.setup(k)
			opts := loggerConfig(k)
			assert.Equal(t, tt.want.Level, opts.Level)
			assert.Equal(t, tt.want.Filename, opts.Filename)
			assert.Equal(t, tt.want.MaxSize, opts.MaxSize)
			assert.Equal(t, tt.want.MaxBackups, opts.MaxBackups)
			assert.Equal(t, tt.want.MaxAge, opts.MaxAge)
			assert.Equal(t, tt.want.Compress, opts.Compress)
			assert.Equal(t, tt.want.JSON, opts.JSON)
		})
	}
}

func TestParseFlags_Success(t *testing.T) {
	flags, err := ParseFlags("1.0", []string{"-c", "cfg.yaml", "-p", "8080"})
	require.NoError(t, err)
	assert.Equal(t, "cfg.yaml", flags.ConfigFile)
	assert.Equal(t, 8080, flags.Port)
	assert.True(t, flags.PortSet)
}

func TestParseFlags_DefaultPort(t *testing.T) {
	flags, err := ParseFlags("1.0", []string{"-c", "cfg.yaml"})
	require.NoError(t, err)
	assert.Equal(t, defaultPort, flags.Port)
	assert.False(t, flags.PortSet)
}

func TestParseFlags_Help(t *testing.T) {
	_, err := ParseFlags("1.0", []string{"-h"})
	assert.True(t, errors.Is(err, flag.ErrHelp))
}

func TestParseFlags_Version(t *testing.T) {
	_, err := ParseFlags("1.0", []string{"-V"})
	assert.True(t, errors.Is(err, flag.ErrHelp))
}

func TestParseFlags_ConfigRequired(t *testing.T) {
	_, err := ParseFlags("1.0", []string{"-p", "8080"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "config is required")
}

func TestParseFlags_InvalidFlag(t *testing.T) {
	_, err := ParseFlags("1.0", []string{"--bogus"})
	assert.Error(t, err)
}

func TestNew_FileNotFound(t *testing.T) {
	_, err := New("nonexistent.yaml", 0, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read configuration file")
}

func TestEnvKey(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want string
	}{
		{name: "flat key", env: "BRF_INTERVAL", want: "interval"},
		{name: "camelCase flat key", env: "BRF_RETRYINTERVAL", want: "retryInterval"},
		{name: "metrics prefix", env: "BRF_METRICSPREFIX", want: "exporter.metricsPrefix"},
		{name: "exporter port", env: "BRF_EXPORTER_PORT", want: "exporter.port"},
		{name: "exporter metrics prefix", env: "BRF_EXPORTER_METRICSPREFIX", want: "exporter.metricsPrefix"},
		{name: "backup timeout", env: "BRF_BACKUPTIMEOUT", want: "backupTimeout"},
		{name: "nested logging level", env: "BRF_LOGGING_LEVEL", want: "logging.level"},
		{name: "logger level alias", env: "BRF_LOG_LEVEL", want: "logging.level"},
		{name: "nested camelCase key", env: "BRF_LOGGING_MAXSIZE", want: "logging.maxSize"},
		{name: "nested backup key", env: "BRF_BACKUPS_ID_URL", want: "backups.id.url"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, envKey(tt.env))
		})
	}
}

func TestGetConfigDuration_ParseError(t *testing.T) {
	k := koanf.New(".")
	require.NoError(t, k.Set("badDuration", "not-a-duration"))
	_, err := getConfigDuration(k, "badDuration")
	assert.Error(t, err)
}

func TestGetConfigDuration_DefaultValue(t *testing.T) {
	k := defaultKoanf(t)
	d, err := getConfigDuration(k, "interval")
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, d)
}

func TestNew_Defaults(t *testing.T) {
	testutil.UseTempDir(t)
	require.NoError(t, os.WriteFile("empty.yaml", []byte("{}\n"), 0600))

	cfg, err := New("empty.yaml", 0, false)
	require.NoError(t, err)

	assert.Equal(t, defaultPort, cfg.Port)
	assert.Equal(t, 24*time.Hour, cfg.Interval)
	assert.Equal(t, 24*time.Hour, cfg.RetryInterval)
	assert.Equal(t, "backupremotefiles", cfg.MetricsPrefix)
	assert.Empty(t, cfg.Backups)
}

func TestNew_ExporterPort(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		port    int
		portSet bool
		want    int
	}{
		{
			name:   "config port",
			config: "exporter:\n  port: 9999\n",
			want:   9999,
		},
		{
			name:    "CLI overrides config port",
			config:  "exporter:\n  port: 9999\n",
			port:    1234,
			portSet: true,
			want:    1234,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutil.UseTempDir(t)
			require.NoError(t, os.WriteFile("port.yaml", []byte(tt.config), 0600))

			cfg, err := New("port.yaml", tt.port, tt.portSet)
			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.Port)
		})
	}
}

func TestNew_BackupDefaultTimeout(t *testing.T) {
	testutil.UseTempDir(t)
	configContent := "backups:\n  test:\n    url: http://example.com\n    outputFile: out.txt\n"
	require.NoError(t, os.WriteFile("no_timeout.yaml", []byte(configContent), 0600))

	cfg, err := New("no_timeout.yaml", 0, false)
	require.NoError(t, err)

	require.Len(t, cfg.Backups, 1)
	assert.Equal(t, time.Minute, cfg.Backups[0].Timeout)
}

func TestNew_InvalidBackupTimeout(t *testing.T) {
	testutil.UseTempDir(t)
	configContent := "backups:\n  test:\n    url: http://example.com\n    outputFile: out.txt\n    timeout: 'bad-duration'\n"
	require.NoError(t, os.WriteFile("bad_timeout.yaml", []byte(configContent), 0600))

	_, err := New("bad_timeout.yaml", 0, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid duration")
}

func TestNew_LoggingSection(t *testing.T) {
	testutil.UseTempDir(t)
	configContent := "logging:\n  level: DEBUG\ninterval: 1h\nretryInterval: 1h\n"
	err := os.WriteFile("logging_config.yaml", []byte(configContent), 0600)
	require.NoError(t, err)

	cfg, err := New("logging_config.yaml", 8080, true)
	require.NoError(t, err)
	assert.Equal(t, 8080, cfg.Port)
}

func TestNew_InvalidRetryInterval(t *testing.T) {
	testutil.UseTempDir(t)
	configContent := "interval: 1h\nretryInterval: 'bad-duration'\n"
	err := os.WriteFile("bad_retry.yaml", []byte(configContent), 0600)
	require.NoError(t, err)

	_, err = New("bad_retry.yaml", 0, false)
	assert.Error(t, err)
}

func TestNew_InvalidInterval(t *testing.T) {
	testutil.UseTempDir(t)
	configContent := "interval: 'not-a-duration'\nretryInterval: 1h\n"
	err := os.WriteFile("bad_interval.yaml", []byte(configContent), 0600)
	require.NoError(t, err)

	_, err = New("bad_interval.yaml", 0, false)
	assert.Error(t, err)
}

func TestNew_MissingURL(t *testing.T) {
	testutil.UseTempDir(t)
	configContent := "backups:\n  test:\n    outputFile: out.txt\ninterval: 1h\nretryInterval: 1h\n"
	err := os.WriteFile("missing_url.yaml", []byte(configContent), 0600)
	require.NoError(t, err)

	_, err = New("missing_url.yaml", 0, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "url is required")
}

func TestNew_MissingOutputFile(t *testing.T) {
	testutil.UseTempDir(t)
	configContent := "backups:\n  test:\n    url: http://example.com\ninterval: 1h\nretryInterval: 1h\n"
	err := os.WriteFile("missing_output.yaml", []byte(configContent), 0600)
	require.NoError(t, err)

	_, err = New("missing_output.yaml", 0, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "outputFile is required")
}
