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

package explorers

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestFileSHA256Sum(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "testfile")
	content := []byte("hello world")

	if err := os.WriteFile(filePath, content, 0600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Calculate expected hash
	hasher := sha256.New()
	hasher.Write(content)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))

	gotHash, err := FileSHA256Sum(filePath)
	if err != nil {
		t.Fatalf("FileSHA256Sum failed: %v", err)
	}

	if gotHash != expectedHash {
		t.Errorf("FileSHA256Sum() = %q, want %q", gotHash, expectedHash)
	}

	// Test non-existent file
	_, err = FileSHA256Sum(filepath.Join(tmpDir, "non_existent"))
	if err == nil {
		t.Error("FileSHA256Sum on non-existent file did not return error")
	}
}

func TestFileInfoAsJSON(t *testing.T) {
	t.Parallel()

	fi := &FileInfo{
		FileName:   "test.txt",
		FullPath:   "/path/to/test.txt",
		FileSize:   123,
		FileSHA256: "fake-sha256",
	}

	jsonBytes, err := fi.AsJSON()
	if err != nil {
		t.Fatalf("AsJSON failed: %v", err)
	}

	expectedJSON := `{"file_name":"test.txt","full_path":"/path/to/test.txt","file_size":123,"file_modified":"0001-01-01T00:00:00Z","file_accessed":"0001-01-01T00:00:00Z","file_changed":"0001-01-01T00:00:00Z","file_birth":"0001-01-01T00:00:00Z","file_sha256":"fake-sha256"}`
	if string(jsonBytes) != expectedJSON {
		t.Errorf("AsJSON() = %s, want %s", string(jsonBytes), expectedJSON)
	}
}

func TestScanDiffDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Setup mock directory structure:
	// tmpDir/
	//   dir1/
	//     file1.txt (regular file)
	//     file_exec.sh (executable file)
	//   dir2/
	//     file2.txt (regular file)
	//     link_to_file1 -> ../dir1/file1.txt (valid symlink)
	//     broken_link -> ../non_existent (broken symlink)
	//   dir_loop/
	//     link_to_parent -> .. (symlink loop)

	dir1 := filepath.Join(tmpDir, "dir1")
	dir2 := filepath.Join(tmpDir, "dir2")
	dirLoop := filepath.Join(tmpDir, "dir_loop")

	if err := os.MkdirAll(dir1, 0750); err != nil {
		t.Fatalf("failed to create dir1: %v", err)
	}
	if err := os.MkdirAll(dir2, 0750); err != nil {
		t.Fatalf("failed to create dir2: %v", err)
	}
	if err := os.MkdirAll(dirLoop, 0750); err != nil {
		t.Fatalf("failed to create dirLoop: %v", err)
	}

	file1 := filepath.Join(dir1, "file1.txt")
	content1 := []byte("content1")
	if err := os.WriteFile(file1, content1, 0600); err != nil {
		t.Fatalf("failed to write file1: %v", err)
	}

	fileExec := filepath.Join(dir1, "file_exec.sh")
	if err := os.WriteFile(fileExec, []byte("#!/bin/sh"), 0600); err != nil {
		t.Fatalf("failed to write fileExec: %v", err)
	}
	//nolint:gosec // G302: Explicitly need executable permissions for testing
	if err := os.Chmod(fileExec, 0700); err != nil {
		t.Fatalf("failed to chmod fileExec: %v", err)
	}

	file2 := filepath.Join(dir2, "file2.txt")
	if err := os.WriteFile(file2, []byte("content2"), 0600); err != nil {
		t.Fatalf("failed to write file2: %v", err)
	}

	linkToFile1 := filepath.Join(dir2, "link_to_file1")
	if err := os.Symlink(file1, linkToFile1); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	brokenLink := filepath.Join(dir2, "broken_link")
	if err := os.Symlink(filepath.Join(tmpDir, "non_existent"), brokenLink); err != nil {
		t.Fatalf("failed to create broken symlink: %v", err)
	}

	linkToParent := filepath.Join(dirLoop, "link_to_parent")
	if err := os.Symlink(tmpDir, linkToParent); err != nil {
		t.Fatalf("failed to create symlink loop: %v", err)
	}

	// Run ScanDiffDirectory
	addedOrModified, inaccessibleFiles, err := ScanDiffDirectory(tmpDir)
	if err != nil {
		t.Fatalf("ScanDiffDirectory failed: %v", err)
	}

	// Verify addedOrModified
	expectedAdded := map[string]struct {
		fileType string
		size     int64
	}{
		"/dir1/file1.txt":     {fileType: "", size: int64(len(content1))},
		"/dir1/file_exec.sh":  {fileType: "executable", size: 9},
		"/dir2/file2.txt":     {fileType: "", size: 8},
		"/dir2/link_to_file1": {fileType: "", size: int64(len(content1))},
	}

	if len(addedOrModified) != len(expectedAdded) {
		t.Errorf("expected %d added/modified files, got %d", len(expectedAdded), len(addedOrModified))
		for _, f := range addedOrModified {
			t.Logf("Found added: %q (type: %q)", f.FullPath, f.FileType)
		}
	}

	for _, f := range addedOrModified {
		exp, ok := expectedAdded[f.FullPath]
		if !ok {
			t.Errorf("unexpected added/modified file: %q", f.FullPath)
			continue
		}
		if f.FileType != exp.fileType {
			t.Errorf("file %q: expected Type %q, got %q", f.FullPath, exp.fileType, f.FileType)
		}
		if f.FileSize != exp.size {
			t.Errorf("file %q: expected Size %d, got %d", f.FullPath, exp.size, f.FileSize)
		}
		if f.FileSHA256 == "" && f.FileType != "directory" {
			t.Errorf("file %q: expected SHA256 to be populated", f.FullPath)
		}
	}

	// Verify inaccessibleFiles
	expectedInaccessible := map[string]bool{
		"/dir2/broken_link": true,
	}

	if len(inaccessibleFiles) != len(expectedInaccessible) {
		t.Errorf("expected %d inaccessible files, got %d", len(expectedInaccessible), len(inaccessibleFiles))
		for _, f := range inaccessibleFiles {
			t.Logf("Found inaccessible: %q", f.FullPath)
		}
	}

	for _, f := range inaccessibleFiles {
		if !expectedInaccessible[f.FullPath] {
			t.Errorf("unexpected inaccessible file: %q", f.FullPath)
		}
	}
}
