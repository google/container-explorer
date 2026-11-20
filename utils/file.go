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
	"strings"

	log "github.com/sirupsen/logrus"
)

func hasControlChar(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// SafePathSegment validates that s is safe to use as a single path segment
// (e.g. a container ID or link ID used as a directory or filename). It returns
// an error if s is empty, is "." or "..", contains a path separator, or
// contains a control character. This protects callers that build host paths
// from untrusted on-disk metadata against directory traversal.
func SafePathSegment(s string) error {
	if s == "" {
		return errors.New("empty path segment")
	}
	if s == "." || s == ".." {
		return fmt.Errorf("path segment %q is a relative directory reference", s)
	}
	if strings.ContainsRune(s, '/') || strings.ContainsRune(s, '\\') || strings.ContainsRune(s, os.PathSeparator) {
		return fmt.Errorf("path segment %q contains a path separator", s)
	}
	if hasControlChar(s) {
		return fmt.Errorf("path segment %q contains a control character", s)
	}
	return nil
}

// SafeOverlayOptionPath verifies that p is safe to embed as a path value
// inside an overlayfs mount-option string (e.g. "lowerdir=%s"). The kernel and
// mount(8) tokenise the -o string on ',' and overlayfs splits lowerdir on ':',
// so a path containing either character would let image-authored content inject
// additional mount options (rw, upperdir=, workdir=, ...). Legitimate overlay2
// link/lower entries are short alphanumeric IDs and never contain these
// characters.
func SafeOverlayOptionPath(p string) error {
	if p == "" {
		return errors.New("empty overlay path")
	}
	if strings.ContainsAny(p, `,:\"`) {
		return fmt.Errorf("overlay path %q contains a mount-option metacharacter", p)
	}
	if hasControlChar(p) {
		return fmt.Errorf("overlay path %q contains a control character", p)
	}
	return nil
}

// SafeJoin joins base and parts using filepath.Join and verifies the resulting
// cleaned path is still contained within base. It returns an error if the
// result escapes base via ".." segments or an absolute path in parts. Callers
// should use this when any element of parts is derived from untrusted on-disk
// metadata (e.g. overlay link/lower files authored inside a container image).
func SafeJoin(base string, parts ...string) (string, error) {
	joined := filepath.Join(append([]string{base}, parts...)...)
	rel, err := filepath.Rel(base, joined)
	if err != nil {
		return "", fmt.Errorf("computing relative path from %q to %q: %w", base, joined, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q escapes base directory %q", joined, base)
	}
	return joined, nil
}

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
