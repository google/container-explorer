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
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

// PathExists returns true if the specified file or directory exists.
// This function follows symbolic links.
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

// PathExistsV2 returns true if the path exists.
// It follows symbolic links and returns false on any error (including permission denied).
func PathExistsV2(path string) bool {
	exists, _ := PathExists(path)
	return exists
}

const (
	charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

// GenerateRandomString creates a random string of a fixed length.
// It is thread-safe and uses crypto/rand for high-quality randomness.
func GenerateRandomString(stringLength int) string {
	b := make([]byte, stringLength)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return ""
	}
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b)
}

// GetMountPoint returns a random temporary directory path to be used as a mount point.
func GetMountPoint() string {
	mountSuffix := GenerateRandomString(6)
	mountPoint := filepath.Join(os.TempDir(), "mnt", mountSuffix)
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
			// If it's a broken symlink, skip it.
			if os.IsNotExist(statErr) && (d.Type()&fs.ModeSymlink != 0) {
				log.Debugf("skipping broken symlink %s", path)
				return nil // Continue walking
			}
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
