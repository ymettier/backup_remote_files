// Copyright 2026 The Backup_remote_files Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package testutil

import (
	"os"
	"testing"
)

var (
	getwd = os.Getwd
	chdir = os.Chdir
)

// UseTempDir changes the working directory to a fresh temporary directory for
// the duration of the test, restoring the original directory on cleanup.
func UseTempDir(t *testing.T) {
	t.Helper()
	if err := useTempDir(t); err != nil {
		t.Fatal(err)
	}
}

// useTempDir performs the directory switch and returns an error instead of
// aborting, so its failure paths can be tested without failing the test.
func useTempDir(t testing.TB) error {
	dir := t.TempDir()
	oldDir, err := getwd()
	if err != nil {
		return err
	}
	if err := chdir(dir); err != nil {
		return err
	}
	t.Cleanup(func() {
		if err := chdir(oldDir); err != nil {
			t.Logf("failed to restore working directory: %v", err)
		}
	})
	return nil
}
