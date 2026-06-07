// Copyright 2023 The Backup_remote_files Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package logger

import (
	"context"
	"encoding/json"
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

func TestGetOtherJsonLogFile(t *testing.T) {
	filename := "json_test.log"

	// Remove the logs file before the test
	if _, err := os.Stat(filename); !errors.Is(err, os.ErrNotExist) {
		os.Remove(filename)
	}

	l := newLogger(&LogOptions{Filename: filename, JSON: true})
	l.Warn("Message")
	if !assert.FileExists(t, filename) {
		return
	}

	defer os.Remove(filename)

	jsonFile, err := os.Open(filename)
	if !assert.NoError(t, err) {
		return
	}

	defer jsonFile.Close()
	byteValue, _ := io.ReadAll(jsonFile)

	// Ensure that the content of the file is in json format
	assert.True(t, json.Valid(byteValue))

	var result map[string]any
	err = json.Unmarshal(byteValue, &result)
	assert.NoError(t, err)
	assert.Equal(t, "WARN", result["level"])
	assert.Equal(t, "Message", result["msg"])
}

func TestLogLevel(t *testing.T) {
	os.Setenv("LOG_LEVEL", "DEBUG")
	defer os.Unsetenv("LOG_LEVEL")

	l := newLogger(nil)
	assert.True(t, l.Enabled(context.Background(), -4)) // slog.LevelDebug is -4
}
