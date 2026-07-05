// Copyright 2024-2026 The Backup_remote_files Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package config

import (
	"errors"
	"flag"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/knadh/koanf/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"backup_remote_files/testutil"
)

func TestVersion(t *testing.T) {
	wantedVersion := "1.2.3"
	output := versionInfo(wantedVersion)
	s := strings.Split(output, "\n")
	assert.Equal(t, "Version        : "+wantedVersion, s[0], "Printing version")
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

	cfg, err := New("test_env_config.yaml", defaultPort)
	assert.NoError(t, err)

	assert.Equal(t, "2h0m0s", cfg.Interval.String())
	assert.Equal(t, "30m0s", cfg.RetryInterval.String())
	assert.Equal(t, "custom_prefix", cfg.MetricsPrefix)
}

func TestLoggerConfigDefaults(t *testing.T) {
	k := koanf.New(".")
	opts := loggerConfig(k)

	assert.Equal(t, "INFO", opts.Level)
	assert.Equal(t, "", opts.Filename)
	assert.Equal(t, 5, opts.MaxSize)
	assert.Equal(t, 10, opts.MaxBackups)
	assert.Equal(t, 14, opts.MaxAge)
	assert.True(t, opts.Compress)
	assert.False(t, opts.JSON)
}

func TestLoggerConfigOverrides(t *testing.T) {
	k := koanf.New(".")
	assert.NoError(t, k.Set("logging.level", "DEBUG"))
	assert.NoError(t, k.Set("logging.filename", "/var/log/test.log"))
	assert.NoError(t, k.Set("logging.maxSize", 50))
	assert.NoError(t, k.Set("logging.maxBackups", 20))
	assert.NoError(t, k.Set("logging.maxAge", 30))
	assert.NoError(t, k.Set("logging.compress", false))
	assert.NoError(t, k.Set("logging.json", true))
	opts := loggerConfig(k)

	assert.Equal(t, "DEBUG", opts.Level)
	assert.Equal(t, "/var/log/test.log", opts.Filename)
	assert.Equal(t, 50, opts.MaxSize)
	assert.Equal(t, 20, opts.MaxBackups)
	assert.Equal(t, 30, opts.MaxAge)
	assert.False(t, opts.Compress)
	assert.True(t, opts.JSON)
}

func TestParseFlags_Success(t *testing.T) {
	flags, err := ParseFlags("1.0", []string{"-c", "cfg.yaml", "-p", "8080"})
	require.NoError(t, err)
	assert.Equal(t, "cfg.yaml", flags.ConfigFile)
	assert.Equal(t, 8080, flags.Port)
}

func TestParseFlags_DefaultPort(t *testing.T) {
	flags, err := ParseFlags("1.0", []string{"-c", "cfg.yaml"})
	require.NoError(t, err)
	assert.Equal(t, defaultPort, flags.Port)
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
	_, err := New("nonexistent.yaml", 8080)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read configuration file")
}

func TestLookupConfigKey_CamelCase(t *testing.T) {
	k := koanf.New(".")
	require.NoError(t, k.Set("camelCase", "value"))
	val, ok := lookupConfigKey(k, "camelCase")
	assert.True(t, ok)
	assert.Equal(t, "value", val)
}

func TestLookupConfigKey_Underscore(t *testing.T) {
	k := koanf.New(".")
	require.NoError(t, k.Set("underscore_key", "value"))
	val, ok := lookupConfigKey(k, "underscore.key")
	assert.True(t, ok)
	assert.Equal(t, "value", val)
}

func TestLookupConfigKey_NotFound(t *testing.T) {
	k := koanf.New(".")
	_, ok := lookupConfigKey(k, "nonexistent")
	assert.False(t, ok)
}

func TestGetConfigString_Default(t *testing.T) {
	k := koanf.New(".")
	val := getConfigString(k, "missing", "default")
	assert.Equal(t, "default", val)
}

func TestGetConfigDuration_ParseError(t *testing.T) {
	k := koanf.New(".")
	require.NoError(t, k.Set("badDuration", "not-a-duration"))
	_, err := getConfigDuration(k, "badDuration", "24h")
	assert.Error(t, err)
}

func TestGetConfigDuration_DefaultValue(t *testing.T) {
	k := koanf.New(".")
	d, err := getConfigDuration(k, "missing", "1h")
	require.NoError(t, err)
	assert.Equal(t, 1*time.Hour, d)
}

func TestNew_LoggingSection(t *testing.T) {
	testutil.UseTempDir(t)
	configContent := "logging:\n  level: DEBUG\ninterval: 1h\nretryInterval: 1h\n"
	err := os.WriteFile("logging_config.yaml", []byte(configContent), 0600)
	require.NoError(t, err)

	cfg, err := New("logging_config.yaml", 8080)
	require.NoError(t, err)
	assert.Equal(t, 8080, cfg.Port)
}

func TestNew_InvalidRetryInterval(t *testing.T) {
	testutil.UseTempDir(t)
	configContent := "interval: 1h\nretryInterval: 'bad-duration'\n"
	err := os.WriteFile("bad_retry.yaml", []byte(configContent), 0600)
	require.NoError(t, err)

	_, err = New("bad_retry.yaml", 8080)
	assert.Error(t, err)
}

func TestNew_InvalidInterval(t *testing.T) {
	testutil.UseTempDir(t)
	configContent := "interval: 'not-a-duration'\nretryInterval: 1h\n"
	err := os.WriteFile("bad_interval.yaml", []byte(configContent), 0600)
	require.NoError(t, err)

	_, err = New("bad_interval.yaml", 8080)
	assert.Error(t, err)
}

func TestLoggerConfigPartialOverrides(t *testing.T) {
	k := koanf.New(".")
	assert.NoError(t, k.Set("logging.level", "ERROR"))
	assert.NoError(t, k.Set("logging.filename", "stdout"))
	opts := loggerConfig(k)

	assert.Equal(t, "ERROR", opts.Level)
	assert.Equal(t, "stdout", opts.Filename)
	assert.Equal(t, 5, opts.MaxSize)
	assert.Equal(t, 10, opts.MaxBackups)
	assert.Equal(t, 14, opts.MaxAge)
	assert.True(t, opts.Compress)
	assert.False(t, opts.JSON)
}
