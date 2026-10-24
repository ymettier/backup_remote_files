// Copyright 2024-2026 The Backup_remote_files Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package logger

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"backup_remote_files/testutil"
)

func resetGlobal() {
	mu.Lock()
	defer mu.Unlock()
	logger = nil
}

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
	tests := []struct {
		name      string
		opts      *LogOptions
		env       string
		wantDebug bool
		wantInfo  bool
	}{
		{
			name:      "debug enabled via env",
			env:       "DEBUG",
			wantDebug: true,
			wantInfo:  true,
		},
		{
			name:      "debug enabled via opts",
			opts:      &LogOptions{Level: "DEBUG"},
			wantDebug: true,
			wantInfo:  true,
		},
		{
			name:      "invalid level defaults to info",
			env:       "BOGUS",
			wantDebug: false,
			wantInfo:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				os.Setenv("BRF_LOG_LEVEL", tt.env)
				defer os.Unsetenv("BRF_LOG_LEVEL")
			}

			l := newLogger(tt.opts)
			assert.Equal(t, tt.wantDebug, l.Enabled(t.Context(), slog.LevelDebug))
			assert.Equal(t, tt.wantInfo, l.Enabled(t.Context(), slog.LevelInfo))
		})
	}
}

func TestGetWriter(t *testing.T) {
	tests := []struct {
		name     string
		opts     *LogOptions
		wantNil  bool
		wantFile bool
		useTmp   bool
	}{
		{
			name:     "file with defaults",
			opts:     &LogOptions{Filename: "lumberjack_test.log"},
			wantNil:  false,
			wantFile: true,
			useTmp:   true,
		},
		{
			name:     "file with zero rotation values",
			opts:     &LogOptions{Filename: "lumberjack_defaults.log", MaxSize: 0, MaxBackups: 0, MaxAge: 0, Compress: false},
			wantNil:  false,
			wantFile: true,
			useTmp:   true,
		},
		{
			name:     "file with custom rotation values",
			opts:     &LogOptions{Filename: "lumberjack_custom.log", MaxSize: 50, MaxBackups: 5, MaxAge: 7, Compress: true},
			wantNil:  false,
			wantFile: true,
			useTmp:   true,
		},
		{
			name:     "stdout", //nolint:goconst
			opts:     &LogOptions{Filename: "stdout"},
			wantNil:  false,
			wantFile: false,
			useTmp:   false,
		},
		{
			name:     "stderr", //nolint:goconst
			opts:     &LogOptions{Filename: "stderr"},
			wantNil:  false,
			wantFile: false,
			useTmp:   false,
		},
		{
			name:     "nil opts",
			opts:     nil,
			wantNil:  false,
			wantFile: false,
			useTmp:   false,
		},
		{
			name:     "empty filename",
			opts:     &LogOptions{Filename: ""},
			wantNil:  false,
			wantFile: false,
			useTmp:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.useTmp {
				testutil.UseTempDir(t)
			}
			w, ok := getWriter(tt.opts)
			if tt.wantNil {
				assert.Nil(t, w)
			} else {
				assert.NotNil(t, w)
			}
			assert.Equal(t, tt.wantFile, ok)
			if tt.opts != nil && tt.opts.Filename != "" && tt.opts.Filename != "stdout" && tt.opts.Filename != "stderr" {
				_, err := w.Write([]byte("test"))
				assert.NoError(t, err)
			}
		})
	}
}

func TestGetAndReset(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "singleton returns same instance",
			run: func(t *testing.T) {
				resetGlobal()
				l1 := Get()
				require.NotNil(t, l1)
				l2 := Get()
				assert.Same(t, l1, l2)
			},
		},
		{
			name: "reset replaces instance",
			run: func(t *testing.T) {
				resetGlobal()
				l1 := Get()
				require.NotNil(t, l1)
				Reset(&LogOptions{Filename: "stderr", JSON: true})
				l2 := Get()
				assert.NotSame(t, l1, l2)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t)
		})
	}
}

func TestContextLogger(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "with context and from context roundtrip",
			run: func(t *testing.T) {
				l := newLogger(nil)
				ctx := WithCtx(t.Context(), l)
				extracted := FromCtx(ctx)
				assert.Same(t, l, extracted)
			},
		},
		{
			name: "from context defaults to global logger",
			run: func(t *testing.T) {
				resetGlobal()
				extracted := FromCtx(t.Context())
				assert.NotNil(t, extracted)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t)
		})
	}
}

func TestGet_RaceCondition(t *testing.T) {
	resetGlobal()

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l := Get()
			assert.NotNil(t, l)
		}()
	}
	wg.Wait()
}

func TestRedactURL(t *testing.T) {
	tests := []struct {
		name   string
		rawURL string
		want   string
	}{
		{ //nolint:gosec
			name:   "strips credentials",
			rawURL: "https://user:secret@example.com/path",
			want:   "https://example.com/path", //nolint:goconst
		},
		{
			name:   "no credentials unchanged",
			rawURL: "https://example.com/path",
			want:   "https://example.com/path",
		},
		{
			name:   "unparseable returned as-is",
			rawURL: "://not a url",
			want:   "://not a url",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, RedactURL(tt.rawURL))
		})
	}
}
