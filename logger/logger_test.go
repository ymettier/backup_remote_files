// Copyright 2023 The Backup_remote_files Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package logger

import (
	"backup_remote_files/testutil"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetOtherTxtLogFile(t *testing.T) {
	testutil.UseTempDir(t)
	filename := "txt_test.log"

	l := newLogger(&LogOptions{Filename: filename})
	l.Warn("Message")
	if !assert.FileExists(t, filename) {
		return
	}

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
	testutil.UseTempDir(t)
	filename := "json_test.log"

	l := newLogger(&LogOptions{Filename: filename, JSON: true})
	l.Warn("Message")
	if !assert.FileExists(t, filename) {
		return
	}

	jsonFile, err := os.Open(filename)
	if !assert.NoError(t, err) {
		return
	}

	defer jsonFile.Close()
	byteValue, _ := io.ReadAll(jsonFile)

	// Find the test message line (second line, after the config log)
	lines := strings.Split(string(byteValue), "\n")
	var messageLine string
	for _, line := range lines {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry["msg"] == "Message" {
			messageLine = line
			break
		}
	}
	assert.NotEmpty(t, messageLine, "should find the test message line")

	var result map[string]any
	err = json.Unmarshal([]byte(messageLine), &result)
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
