// Copyright 2026 The Backup_remote_files Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package testutil

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUseTempDir(t *testing.T) {
	originalDir, err := os.Getwd()
	require.NoError(t, err)

	UseTempDir(t)

	newDir, err := os.Getwd()
	require.NoError(t, err)

	assert.NotEqual(t, newDir, originalDir)
}

func TestUseTempDir_RestoresOriginal(t *testing.T) {
	originalDir, err := os.Getwd()
	require.NoError(t, err)

	t.Run("sub", func(t *testing.T) {
		UseTempDir(t)
	})

	restoredDir, err := os.Getwd()
	require.NoError(t, err)

	assert.Equal(t, originalDir, restoredDir)
}

func TestUseTempDir_CleanupLog(t *testing.T) {
	originalDir, err := os.Getwd()
	require.NoError(t, err)

	t.Run("sub", func(t *testing.T) {
		UseTempDir(t)
		dir, _ := os.Getwd()
		os.RemoveAll(dir)
	})

	restoredDir, err := os.Getwd()
	require.NoError(t, err)

	assert.Equal(t, originalDir, restoredDir)
}

func TestUseTempDir_ErrorGetwd(t *testing.T) {
	origGetwd := getwd
	getwd = func() (string, error) { return "", errors.New("getwd failed") }
	t.Cleanup(func() { getwd = origGetwd })

	if err := useTempDir(t); err == nil {
		assert.Fail(t, "expected error when getwd fails")
	}
}

func TestUseTempDir_ErrorChdir(t *testing.T) {
	origChdir := chdir
	chdir = func(string) error { return errors.New("chdir failed") }
	t.Cleanup(func() { chdir = origChdir })

	if err := useTempDir(t); err == nil {
		assert.Fail(t, "expected error when chdir fails")
	}
}

func TestUseTempDir_ErrorRestore(t *testing.T) {
	origChdir := chdir
	calls := 0
	chdir = func(path string) error {
		calls++
		err := origChdir(path)
		if calls > 1 {
			return errors.New("restore failed")
		}
		return err
	}
	t.Cleanup(func() { chdir = origChdir })

	UseTempDir(t)
}

// fakeTB is a testing.TB whose Fatal records the call instead of aborting,
// so the failure path of UseTempDir can be exercised without failing a test.
type fakeTB struct {
	testing.TB
	fatalCalled bool
}

func (f *fakeTB) Fatal(_ ...any) {
	f.fatalCalled = true
}

func TestUseTempDir_Fatal(t *testing.T) {
	origGetwd := getwd
	getwd = func() (string, error) { return "", errors.New("getwd failed") }
	t.Cleanup(func() { getwd = origGetwd })

	ft := &fakeTB{TB: t}
	UseTempDir(ft)
	if !ft.fatalCalled {
		assert.Fail(t, "expected UseTempDir to call Fatal when useTempDir returns an error")
	}
}
