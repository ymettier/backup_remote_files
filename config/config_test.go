// Copyright 2023 The Backup_remote_files Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package config

import (
	"backup_remote_files/testutil"
	"os"
	"strings"
	"testing"

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
