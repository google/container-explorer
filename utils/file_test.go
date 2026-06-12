/*
Copyright 2026 Google LLC

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
	"os"
	"path/filepath"
	"testing"
)

func TestPathExists(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Test case 1: Existing directory
	exists, err := PathExists(tmpDir)
	if err != nil {
		t.Errorf("PathExists(%q) returned unexpected error: %v", tmpDir, err)
	}
	if !exists {
		t.Errorf("PathExists(%q) returned false, expected true", tmpDir)
	}

	// Test case 2: Existing file
	tmpFile := filepath.Join(tmpDir, "testfile")
	if err := os.WriteFile(tmpFile, []byte("content"), 0600); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	exists, err = PathExists(tmpFile)
	if err != nil {
		t.Errorf("PathExists(%q) returned unexpected error: %v", tmpFile, err)
	}
	if !exists {
		t.Errorf("PathExists(%q) returned false, expected true", tmpFile)
	}

	// Test case 3: Non-existing path
	nonExistingPath := filepath.Join(tmpDir, "doesnotexist")
	exists, err = PathExists(nonExistingPath)
	if err != nil {
		t.Errorf("PathExists(%q) returned unexpected error: %v", nonExistingPath, err)
	}
	if exists {
		t.Errorf("PathExists(%q) returned true, expected false", nonExistingPath)
	}

	// Test case 4: Symlink to existing file
	symlinkPath := filepath.Join(tmpDir, "symlink")
	if err := os.Symlink(tmpFile, symlinkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}
	exists, err = PathExists(symlinkPath)
	if err != nil {
		t.Errorf("PathExists(%q) returned unexpected error: %v", symlinkPath, err)
	}
	if !exists {
		t.Errorf("PathExists(%q) returned false, expected true", symlinkPath)
	}

	// Test case 5: Broken symlink (should return false, nil)
	brokenSymlinkPath := filepath.Join(tmpDir, "brokensymlink")
	if err := os.Symlink(nonExistingPath, brokenSymlinkPath); err != nil {
		t.Fatalf("failed to create broken symlink: %v", err)
	}
	exists, err = PathExists(brokenSymlinkPath)
	if err != nil {
		t.Errorf("PathExists(%q) returned unexpected error: %v", brokenSymlinkPath, err)
	}
	if exists {
		t.Errorf("PathExists(%q) returned true, expected false", brokenSymlinkPath)
	}
}

func TestPathExistsV2(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Test case 1: Existing directory
	if !PathExistsV2(tmpDir) {
		t.Errorf("PathExistsV2(%q) returned false, expected true", tmpDir)
	}

	// Test case 2: Existing file
	tmpFile := filepath.Join(tmpDir, "testfile")
	if err := os.WriteFile(tmpFile, []byte("content"), 0600); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if !PathExistsV2(tmpFile) {
		t.Errorf("PathExistsV2(%q) returned false, expected true", tmpFile)
	}

	// Test case 3: Non-existing path
	nonExistingPath := filepath.Join(tmpDir, "doesnotexist")
	if PathExistsV2(nonExistingPath) {
		t.Errorf("PathExistsV2(%q) returned true, expected false", nonExistingPath)
	}

	// Test case 4: Broken symlink
	brokenSymlinkPath := filepath.Join(tmpDir, "brokensymlink")
	if err := os.Symlink(nonExistingPath, brokenSymlinkPath); err != nil {
		t.Fatalf("failed to create broken symlink: %v", err)
	}
	if PathExistsV2(brokenSymlinkPath) {
		t.Errorf("PathExistsV2(%q) returned true, expected false", brokenSymlinkPath)
	}
}

func TestGenerateRandomString(t *testing.T) {
	t.Parallel()

	lengths := []int{0, 1, 5, 10, 100}
	for _, length := range lengths {
		str := GenerateRandomString(length)
		if len(str) != length {
			t.Errorf("GenerateRandomString(%d) returned string of length %d, expected %d", length, len(str), length)
		}

		// Verify characters are from the charset
		for _, char := range str {
			if !isCharInCharset(char, charset) {
				t.Errorf("GenerateRandomString(%d) returned string containing invalid character: %c", length, char)
			}
		}
	}

	// Verify randomness (weak check)
	str1 := GenerateRandomString(10)
	str2 := GenerateRandomString(10)
	if str1 == str2 && len(str1) > 0 {
		t.Errorf("GenerateRandomString(10) returned identical strings: %q and %q", str1, str2)
	}
}

func isCharInCharset(char rune, charset string) bool {
	for _, c := range charset {
		if char == c {
			return true
		}
	}
	return false
}

func TestGetMountPoint(t *testing.T) {
	t.Parallel()

	mp := GetMountPoint()
	if mp == "" {
		t.Error("GetMountPoint() returned empty string")
	}

	// Verify it is under TempDir
	tempDir := os.TempDir()
	rel, err := filepath.Rel(tempDir, mp)
	if err != nil {
		t.Errorf("failed to get relative path: %v", err)
	}
	if filepath.IsAbs(rel) || rel == ".." || len(rel) > 0 && rel[0] == '.' {
		t.Errorf("GetMountPoint() returned %q, which is not under %q", mp, tempDir)
	}

	// Verify structure: <tempdir>/mnt/<random>
	dir, file := filepath.Split(mp)
	if filepath.Base(dir) != "mnt" {
		t.Errorf("GetMountPoint() returned %q, expected parent directory to be 'mnt'", mp)
	}
	if len(file) != 6 {
		t.Errorf("GetMountPoint() returned %q, expected random suffix of length 6, got %d", mp, len(file))
	}
}

func TestCalculateDirectorySize(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Test case 1: Empty directory
	size, err := CalculateDirectorySize(tmpDir)
	if err != nil {
		t.Errorf("CalculateDirectorySize(%q) returned unexpected error: %v", tmpDir, err)
	}
	if size != 0 {
		t.Errorf("CalculateDirectorySize(%q) returned size %d, expected 0", tmpDir, size)
	}

	// Test case 2: Directory with files
	file1 := filepath.Join(tmpDir, "file1")
	file2 := filepath.Join(tmpDir, "file2")
	content1 := []byte("hello")  // 5 bytes
	content2 := []byte("world!") // 6 bytes
	if err := os.WriteFile(file1, content1, 0600); err != nil {
		t.Fatalf("failed to create file1: %v", err)
	}
	if err := os.WriteFile(file2, content2, 0600); err != nil {
		t.Fatalf("failed to create file2: %v", err)
	}

	expectedSize := int64(len(content1) + len(content2))
	size, err = CalculateDirectorySize(tmpDir)
	if err != nil {
		t.Errorf("CalculateDirectorySize(%q) returned unexpected error: %v", tmpDir, err)
	}
	if size != expectedSize {
		t.Errorf("CalculateDirectorySize(%q) returned size %d, expected %d", tmpDir, size, expectedSize)
	}

	// Test case 3: Nested directories
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0750); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	file3 := filepath.Join(subDir, "file3")
	content3 := []byte("nested") // 6 bytes
	if err := os.WriteFile(file3, content3, 0600); err != nil {
		t.Fatalf("failed to create file3: %v", err)
	}

	expectedSize += int64(len(content3))
	size, err = CalculateDirectorySize(tmpDir)
	if err != nil {
		t.Errorf("CalculateDirectorySize(%q) returned unexpected error: %v", tmpDir, err)
	}
	if size != expectedSize {
		t.Errorf("CalculateDirectorySize(%q) returned size %d, expected %d", tmpDir, size, expectedSize)
	}

	// Test case 4: Directory with symlink to file
	symlinkToFile := filepath.Join(tmpDir, "symlink_to_file")
	if err := os.Symlink(file1, symlinkToFile); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}
	// The target file1 is already in the directory, so WalkDir will encounter it twice:
	// once as file1, and once as symlink_to_file.
	// os.Stat follows symlinks, so it will stat file1 for both.
	// The size of file1 should be counted twice.
	expectedSize += int64(len(content1))
	size, err = CalculateDirectorySize(tmpDir)
	if err != nil {
		t.Errorf("CalculateDirectorySize(%q) returned unexpected error: %v", tmpDir, err)
	}
	if size != expectedSize {
		t.Errorf("CalculateDirectorySize(%q) returned size %d, expected %d", tmpDir, size, expectedSize)
	}

	// Test case 5: Directory with broken symlink (should be skipped, no error)
	brokenSymlink := filepath.Join(tmpDir, "broken_symlink")
	if err := os.Symlink(filepath.Join(tmpDir, "non_existent"), brokenSymlink); err != nil {
		t.Fatalf("failed to create broken symlink: %v", err)
	}
	// Size should not change, and no error should be returned
	size, err = CalculateDirectorySize(tmpDir)
	if err != nil {
		t.Errorf("CalculateDirectorySize(%q) returned unexpected error: %v", tmpDir, err)
	}
	if size != expectedSize {
		t.Errorf("CalculateDirectorySize(%q) returned size %d, expected %d", tmpDir, size, expectedSize)
	}

	// Test case 6: Input is a file, not a directory (should error)
	_, err = CalculateDirectorySize(file1)
	if err == nil {
		t.Errorf("CalculateDirectorySize(%q) did not return error, expected one", file1)
	}

	// Test case 7: Input directory does not exist (should error)
	_, err = CalculateDirectorySize(filepath.Join(tmpDir, "non_existent_dir"))
	if err == nil {
		t.Error("CalculateDirectorySize on non-existent dir did not return error, expected one")
	}
}
