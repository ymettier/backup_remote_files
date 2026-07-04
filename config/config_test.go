// Copyright 2023 The Backup_remote_files Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package config

import (
	"backup_remote_files/testutil"
	"os"
	"strings"
	"testing"

	"github.com/knadh/koanf/v2"
	"github.com/stretchr/testify/assert"
)

func TestVersion(t *testing.T) {
	wantedVersion := "1.2.3"
	output := printVersion(wantedVersion)
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
	assert.Nil(t, err)

	os.Setenv("BRF_INTERVAL", "2h")
	os.Setenv("BRF_RETRYINTERVAL", "30m")
	os.Setenv("BRF_METRICSPREFIX", "custom_prefix")
	defer func() {
		os.Unsetenv("BRF_INTERVAL")
		os.Unsetenv("BRF_RETRYINTERVAL")
		os.Unsetenv("BRF_METRICSPREFIX")
	}()

	oldArgs := os.Args
	os.Args = []string{"test", "-c", "test_env_config.yaml"}
	defer func() { os.Args = oldArgs }()

	cfg, err := New("test")
	assert.Nil(t, err)

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
