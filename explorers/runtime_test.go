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
	"os"
	"path/filepath"
	"testing"
)

func TestReadCgroupEvents(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	tests := []struct {
		name          string
		fileContent   string
		createFile    bool
		wantPopulated int
		wantFrozen    int
		wantErr       bool
	}{
		{
			name:          "Populated and not frozen",
			fileContent:   "populated 1\nfrozen 0\n",
			createFile:    true,
			wantPopulated: 1,
			wantFrozen:    0,
			wantErr:       false,
		},
		{
			name:          "Populated and frozen",
			fileContent:   "populated 1\nfrozen 1\n",
			createFile:    true,
			wantPopulated: 1,
			wantFrozen:    1,
			wantErr:       false,
		},
		{
			name:          "Not populated and not frozen",
			fileContent:   "populated 0\nfrozen 0\n",
			createFile:    true,
			wantPopulated: 0,
			wantFrozen:    0,
			wantErr:       false,
		},
		{
			name:          "Missing file",
			createFile:    false,
			wantPopulated: -1,
			wantFrozen:    -1,
			wantErr:       true,
		},
		{
			name:          "Malformed file (missing values)",
			fileContent:   "populated\nfrozen\n",
			createFile:    true,
			wantPopulated: -1,
			wantFrozen:    -1,
			wantErr:       false,
		},
		{
			name:          "Malformed file (bad values)",
			fileContent:   "populated abc\nfrozen xyz\n",
			createFile:    true,
			wantPopulated: -1,
			wantFrozen:    -1,
			wantErr:       false,
		},
		{
			name:          "Extra spaces",
			fileContent:   "populated   1  \nfrozen   0  \n",
			createFile:    true,
			wantPopulated: 1,
			wantFrozen:    0,
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(tmpDir, tt.name)
			if err := os.Mkdir(dir, 0750); err != nil {
				t.Fatalf("failed to create dir: %v", err)
			}

			if tt.createFile {
				err := os.WriteFile(filepath.Join(dir, "cgroup.events"), []byte(tt.fileContent), 0600)
				if err != nil {
					t.Fatalf("failed to write file: %v", err)
				}
			}

			populated, frozen, err := ReadCgroupEvents(dir)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadCgroupEvents() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if populated != tt.wantPopulated {
				t.Errorf("ReadCgroupEvents() populated = %v, want %v", populated, tt.wantPopulated)
			}
			if frozen != tt.wantFrozen {
				t.Errorf("ReadCgroupEvents() frozen = %v, want %v", frozen, tt.wantFrozen)
			}
		})
	}
}

func TestGetTaskStatus(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	tests := []struct {
		name        string
		fileContent string
		createFile  bool
		wantStatus  string
		wantErr     bool
	}{
		{
			name:        "Running",
			fileContent: "populated 1\nfrozen 0\n",
			createFile:  true,
			wantStatus:  "RUNNING",
			wantErr:     false,
		},
		{
			name:        "Paused",
			fileContent: "populated 1\nfrozen 1\n",
			createFile:  true,
			wantStatus:  "PAUSED",
			wantErr:     false,
		},
		{
			name:        "Stopped",
			fileContent: "populated 0\nfrozen 0\n",
			createFile:  true,
			wantStatus:  "STOPPED",
			wantErr:     false,
		},
		{
			name:        "Unknown (populated 0, frozen 1)",
			fileContent: "populated 0\nfrozen 1\n",
			createFile:  true,
			wantStatus:  "UNKNOWN",
			wantErr:     true,
		},
		{
			name:       "Missing file (error)",
			createFile: false,
			wantStatus: "UNKNOWN",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(tmpDir, tt.name)
			if err := os.Mkdir(dir, 0750); err != nil {
				t.Fatalf("failed to create dir: %v", err)
			}

			if tt.createFile {
				err := os.WriteFile(filepath.Join(dir, "cgroup.events"), []byte(tt.fileContent), 0600)
				if err != nil {
					t.Fatalf("failed to write file: %v", err)
				}
			}

			status, err := GetTaskStatus(dir)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetTaskStatus() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if status != tt.wantStatus {
				t.Errorf("GetTaskStatus() status = %v, want %v", status, tt.wantStatus)
			}
		})
	}
}

func TestGetTaskPID(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	tests := []struct {
		name        string
		fileContent string
		createFile  bool
		wantPID     int
	}{
		{
			name:        "Valid PID",
			fileContent: "1234\n",
			createFile:  true,
			wantPID:     1234,
		},
		{
			name:        "Multiple PIDs (returns first)",
			fileContent: "5678\n9012\n",
			createFile:  true,
			wantPID:     5678,
		},
		{
			name:        "Empty file",
			fileContent: "",
			createFile:  true,
			wantPID:     -1,
		},
		{
			name:        "Invalid PID (not a number)",
			fileContent: "abc\n",
			createFile:  true,
			wantPID:     -1,
		},
		{
			name:       "Missing file",
			createFile: false,
			wantPID:    -1,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(tmpDir, tt.name)
			if err := os.Mkdir(dir, 0750); err != nil {
				t.Fatalf("failed to create dir: %v", err)
			}

			if tt.createFile {
				err := os.WriteFile(filepath.Join(dir, "cgroup.procs"), []byte(tt.fileContent), 0600)
				if err != nil {
					t.Fatalf("failed to write file: %v", err)
				}
			}

			pid := GetTaskPID(dir)
			if pid != tt.wantPID {
				t.Errorf("GetTaskPID() pid = %v, want %v", pid, tt.wantPID)
			}
		})
	}
}

func TestPathExistsInternal(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Test case 1: Existing directory, looking for directory
	if !PathExists(tmpDir, false) {
		t.Errorf("PathExists(%q, false) returned false, expected true", tmpDir)
	}

	// Test case 2: Existing directory, looking for file
	if PathExists(tmpDir, true) {
		t.Errorf("PathExists(%q, true) returned true, expected false", tmpDir)
	}

	// Test case 3: Existing file, looking for file
	tmpFile := filepath.Join(tmpDir, "testfile")
	if err := os.WriteFile(tmpFile, []byte("content"), 0600); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if !PathExists(tmpFile, true) {
		t.Errorf("PathExists(%q, true) returned false, expected true", tmpFile)
	}

	// Test case 4: Existing file, looking for directory
	if PathExists(tmpFile, false) {
		t.Errorf("PathExists(%q, false) returned true, expected false", tmpFile)
	}

	// Test case 5: Non-existing path
	nonExistingPath := filepath.Join(tmpDir, "doesnotexist")
	if PathExists(nonExistingPath, true) {
		t.Errorf("PathExists(%q, true) returned true, expected false", nonExistingPath)
	}
	if PathExists(nonExistingPath, false) {
		t.Errorf("PathExists(%q, false) returned true, expected false", nonExistingPath)
	}
}
