// Copyright 2023 The Backup_remote_files Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package logger

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetOtherTxtLogFile(t *testing.T) {
	filename := "txt_test.log"

	// Remove the logs file before the test
	if _, err := os.Stat(filename); !errors.Is(err, os.ErrNotExist) {
		os.Remove(filename)
	}

	l := newLogger(&LogOptions{Filename: filename})
	l.Warn("Message")
	if !assert.FileExists(t, filename) {
		return
	}

	defer os.Remove(filename)

	txtFile, err := os.Open(filename)
	if !assert.NoError(t, err) {
		return
	}

	defer txtFile.Close()
	byteValue, _ := io.ReadAll(txtFile)

	assert.Contains(t, string(byteValue), "level=WARN")
	assert.Contains(t, string(byteValue), "msg=Message")
}

func TestLogLevel(t *testing.T) {
	os.Setenv("LOG_LEVEL", "DEBUG")
	defer os.Unsetenv("LOG_LEVEL")

	l := newLogger(nil)
	assert.True(t, l.Enabled(nil, -4)) // slog.LevelDebug is -4
}
