/*
Copyright 2025 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"errors"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"time"
)

// PathExists returns true of specified file or directory exists.
// If symlink is provided, it returns error.
func PathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}

	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}

	return false, err
}

const (
	charset       = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

var seededRand *rand.Rand = rand.New(
	rand.NewSource(time.Now().UnixNano()))

// GenerateRandomString creates a random string of a fixed length (6 characters).
func GenerateRandomString(stringLength int) string {
	b := make([]byte, stringLength)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

func GetMountPoint() string {
	mountSuffix := GenerateRandomString(6)
	mountPoint := filepath.Join("/", "mnt", mountSuffix)
	return mountPoint
}

// CalculateDirectorySize calculates the total size in bytes of all regular files
// within the given rootPath. It follows symbolic links for files to count the
// target's size. It does not follow symbolic links for directories during traversal.
// Broken symbolic links are skipped.
func CalculateDirectorySize(rootPath string) (int64, error) {
	var totalSize int64

	// Ensure the rootPath is a directory and exists.
	// os.Stat follows symlinks, so if rootPath is a symlink to a directory,
	// rootInfo will be for the target directory.
	rootInfo, err := os.Stat(rootPath)
	if err != nil {
		return 0, fmt.Errorf("failed to stat root path %s: %w", rootPath, err)
	}
	if !rootInfo.IsDir() {
		return 0, fmt.Errorf("%s is not a directory", rootPath)
	}

	// filepath.WalkDir does not follow symbolic links to directories when recursing.
	// If rootPath itself is a symlink to a directory, WalkDir will follow it once.
	walkErr := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// This error is from WalkDir itself (e.g., permission denied reading a directory).
			// We'll return it to stop the walk.
			// You could choose to log this error and return nil to try to continue,
			// or return filepath.SkipDir if d is a directory.
			return fmt.Errorf("error accessing path %s: %w", path, err)
		}

		// We only care about files for sizing. Directories themselves don't add to the size.
		if d.IsDir() {
			return nil // Continue walking
		}

		// For non-directory entries (files, symlinks to files, etc.):
		// We use os.Stat(path) because it follows symlinks.
		// d.Info() would give info about the symlink itself, not its target.
		fileInfo, statErr := os.Stat(path)
		if statErr != nil {
			// If it's a broken symlink (os.IsNotExist error and entry is a symlink), skip it.
			if os.IsNotExist(statErr) && (d.Type()&fs.ModeSymlink != 0) {
				fmt.Fprintf(os.Stderr, "Warning: skipping broken symlink %s\n", path)
				return nil // Continue walking
			}
			// For other stat errors, stop the walk.
			return fmt.Errorf("failed to stat %s: %w", path, statErr)
		}

		// Add size if it's a regular file (or a symlink pointing to a regular file).
		if fileInfo.Mode().IsRegular() {
			totalSize += fileInfo.Size()
		}

		return nil // Continue walking
	})

	if walkErr != nil {
		return 0, walkErr // Return the error that stopped the walk.
	}

	return totalSize, nil
}