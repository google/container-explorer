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
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type MockCommandCall struct {
	Name string
	Args []string
}

type MockResponse struct {
	Output []byte
	Err    error
	Stdout string
	Stderr string
}

type MockCommandRunner struct {
	Calls     []MockCommandCall
	Responses map[string]MockResponse
}

func (m *MockCommandRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	m.Calls = append(m.Calls, MockCommandCall{Name: name, Args: args})
	if resp, ok := m.Responses[name]; ok {
		return resp.Output, resp.Err
	}
	return nil, nil
}

func (m *MockCommandRunner) RunSeparate(_ context.Context, name string, args []string, stdout, stderr io.Writer) error {
	m.Calls = append(m.Calls, MockCommandCall{Name: name, Args: args})
	if resp, ok := m.Responses[name]; ok {
		if resp.Stdout != "" {
			_, _ = stdout.Write([]byte(resp.Stdout))
		}
		if resp.Stderr != "" {
			_, _ = stderr.Write([]byte(resp.Stderr))
		}
		return resp.Err
	}
	return nil
}

func (m *MockCommandRunner) RunWithoutContext(name string, args ...string) ([]byte, error) {
	m.Calls = append(m.Calls, MockCommandCall{Name: name, Args: args})
	if resp, ok := m.Responses[name]; ok {
		return resp.Output, resp.Err
	}
	return nil, nil
}

func TestExportContainerImage_Success(t *testing.T) {
	tmpDir := t.TempDir()
	mountpoint := filepath.Join(tmpDir, "container_mount")
	if err := os.Mkdir(mountpoint, 0755); err != nil {
		t.Fatalf("failed to create mock mountpoint: %v", err)
	}

	dummyFile := filepath.Join(mountpoint, "file1.txt")
	dummyData := []byte("hello world") // 11 bytes
	if err := os.WriteFile(dummyFile, dummyData, 0600); err != nil {
		t.Fatalf("failed to write dummy file: %v", err)
	}

	outputDir := filepath.Join(tmpDir, "output")
	containerID := "ctr1"

	mockRunner := &MockCommandRunner{
		Responses: map[string]MockResponse{
			"mkfs.ext4": {Output: []byte("formatted"), Err: nil},
			"losetup":   {Stdout: "/dev/loop123\n", Err: nil}, // for "losetup -f --show"
			"mount":     {Output: []byte("mounted"), Err: nil},
			"cp":        {Output: []byte("copied"), Err: nil},
			"sync":      {Output: []byte("synced"), Err: nil},
			"umount":    {Output: []byte("unmounted"), Err: nil},
		},
	}
	oldRunner := Runner
	Runner = mockRunner
	defer func() { Runner = oldRunner }()

	err := ExportContainerImage(context.Background(), containerID, mountpoint, outputDir)
	if err != nil {
		t.Fatalf("ExportContainerImage failed: %v", err)
	}

	expectedRawFile := filepath.Join(outputDir, "ctr1.raw")
	if _, err := os.Stat(expectedRawFile); os.IsNotExist(err) {
		t.Errorf("expected raw disk file %s was not created", expectedRawFile)
	}

	expectedCalls := []MockCommandCall{
		{Name: "mkfs.ext4", Args: []string{"-F", "-q", expectedRawFile}},
		{Name: "losetup", Args: []string{"-f", "--show", expectedRawFile}},
		{Name: "mount", Args: []string{"/dev/loop123", ""}}, // temp mount dir path is dynamic, verify separately or check prefix
		{Name: "cp", Args: []string{"-a", filepath.Join(mountpoint, "."), ""}},
		{Name: "sync", Args: []string{"-f", ""}},
		{Name: "umount", Args: []string{""}},
		{Name: "losetup", Args: []string{"-d", "/dev/loop123"}},
	}

	if len(mockRunner.Calls) != len(expectedCalls) {
		t.Fatalf("expected %d command calls, got %d. Calls: %+v", len(expectedCalls), len(mockRunner.Calls), mockRunner.Calls)
	}

	for i, call := range mockRunner.Calls {
		expected := expectedCalls[i]
		if call.Name != expected.Name {
			t.Errorf("call %d: expected name '%s', got '%s'", i, expected.Name, call.Name)
		}
		switch expected.Name {
		case "mount":
			if call.Args[0] != "/dev/loop123" {
				t.Errorf("call %d: expected arg[0] '/dev/loop123', got '%s'", i, call.Args[0])
			}
			if !strings.Contains(call.Args[1], "ctr1-img-mount-") {
				t.Errorf("call %d: expected arg[1] to contain 'ctr1-img-mount-', got '%s'", i, call.Args[1])
			}
		case "cp":
			if call.Args[0] != "-a" {
				t.Errorf("call %d: expected arg[0] '-a', got '%s'", i, call.Args[0])
			}
			if call.Args[1] != filepath.Join(mountpoint, ".") {
				t.Errorf("call %d: expected arg[1] '%s', got '%s'", i, filepath.Join(mountpoint, "."), call.Args[1])
			}
			if !strings.Contains(call.Args[2], "ctr1-img-mount-") {
				t.Errorf("call %d: expected arg[2] to contain 'ctr1-img-mount-', got '%s'", i, call.Args[2])
			}
		case "sync":
			if call.Args[0] != "-f" {
				t.Errorf("call %d: expected arg[0] '-f', got '%s'", i, call.Args[0])
			}
			if !strings.Contains(call.Args[1], "ctr1-img-mount-") {
				t.Errorf("call %d: expected arg[1] to contain 'ctr1-img-mount-', got '%s'", i, call.Args[1])
			}
		case "umount":
			// Could be "umount" or "umount -l"
			if !strings.Contains(call.Args[len(call.Args)-1], "ctr1-img-mount-") {
				t.Errorf("call %d: expected last arg to contain 'ctr1-img-mount-', got '%s'", i, call.Args[len(call.Args)-1])
			}
		default:
			if !reflect.DeepEqual(call.Args, expected.Args) {
				t.Errorf("call %d: expected args %v, got %v", i, expected.Args, call.Args)
			}
		}
	}
}

func TestExportContainerImage_MkfsFailure(t *testing.T) {
	tmpDir := t.TempDir()
	mountpoint := filepath.Join(tmpDir, "container_mount")
	_ = os.Mkdir(mountpoint, 0755)

	outputDir := filepath.Join(tmpDir, "output")
	containerID := "ctr1"

	// Mock mkfs.ext4 to fail
	mockRunner := &MockCommandRunner{
		Responses: map[string]MockResponse{
			"mkfs.ext4": {Err: fmt.Errorf("mkfs error"), Output: []byte("failed to format")},
		},
	}
	oldRunner := Runner
	Runner = mockRunner
	defer func() { Runner = oldRunner }()

	err := ExportContainerImage(context.Background(), containerID, mountpoint, outputDir)
	if err == nil {
		t.Fatalf("ExportContainerImage expected failure, got nil")
	}

	// Verify the raw disk file was cleaned up (removed)
	expectedRawFile := filepath.Join(outputDir, "ctr1.raw")
	if _, err := os.Stat(expectedRawFile); !os.IsNotExist(err) {
		t.Errorf("expected raw disk file %s to be cleaned up, but it exists", expectedRawFile)
	}
}

func TestExportContainerImage_LosetupFailure(t *testing.T) {
	tmpDir := t.TempDir()
	mountpoint := filepath.Join(tmpDir, "container_mount")
	_ = os.Mkdir(mountpoint, 0755)

	outputDir := filepath.Join(tmpDir, "output")
	containerID := "ctr1"

	// Mock mkfs.ext4 success, but losetup fails
	mockRunner := &MockCommandRunner{
		Responses: map[string]MockResponse{
			"mkfs.ext4": {Err: nil},
			"losetup":   {Err: fmt.Errorf("losetup error"), Stderr: "loop device busy"},
		},
	}
	oldRunner := Runner
	Runner = mockRunner
	defer func() { Runner = oldRunner }()

	err := ExportContainerImage(context.Background(), containerID, mountpoint, outputDir)
	if err == nil {
		t.Fatalf("ExportContainerImage expected failure, got nil")
	}

	// Verify raw disk file cleaned up
	expectedRawFile := filepath.Join(outputDir, "ctr1.raw")
	if _, err := os.Stat(expectedRawFile); !os.IsNotExist(err) {
		t.Errorf("expected raw disk file %s to be cleaned up, but it exists", expectedRawFile)
	}
}

func TestExportContainerArchive_Success(t *testing.T) {
	tmpDir := t.TempDir()
	mountpoint := filepath.Join(tmpDir, "container_mount")
	_ = os.Mkdir(mountpoint, 0755)

	outputDir := filepath.Join(tmpDir, "output")
	containerID := "ctr1"

	mockRunner := &MockCommandRunner{
		Responses: map[string]MockResponse{
			"tar": {Err: nil},
		},
	}
	oldRunner := Runner
	Runner = mockRunner
	defer func() { Runner = oldRunner }()

	err := ExportContainerArchive(context.Background(), containerID, mountpoint, outputDir)
	if err != nil {
		t.Fatalf("ExportContainerArchive failed: %v", err)
	}

	expectedArchiveFile := filepath.Join(outputDir, "ctr1.tar.gz")
	// Verify tar was called with correct arguments
	expectedCall := MockCommandCall{
		Name: "tar",
		Args: []string{"-czf", expectedArchiveFile, "-C", mountpoint, "."},
	}

	if len(mockRunner.Calls) != 1 {
		t.Fatalf("expected 1 command call, got %d", len(mockRunner.Calls))
	}
	if !reflect.DeepEqual(mockRunner.Calls[0], expectedCall) {
		t.Errorf("expected call %v, got %v", expectedCall, mockRunner.Calls[0])
	}
}

func TestExportContainerArchive_Failure(t *testing.T) {
	tmpDir := t.TempDir()
	mountpoint := filepath.Join(tmpDir, "container_mount")
	_ = os.Mkdir(mountpoint, 0755)

	outputDir := filepath.Join(tmpDir, "output")
	containerID := "ctr1"

	// Mock tar to fail
	mockRunner := &MockCommandRunner{
		Responses: map[string]MockResponse{
			"tar": {Err: fmt.Errorf("tar error"), Output: []byte("tar: write error")},
		},
	}
	oldRunner := Runner
	Runner = mockRunner
	defer func() { Runner = oldRunner }()

	err := ExportContainerArchive(context.Background(), containerID, mountpoint, outputDir)
	if err == nil {
		t.Fatalf("ExportContainerArchive expected failure, got nil")
	}

	// Verify the archive file was cleaned up (removed)
	expectedArchiveFile := filepath.Join(outputDir, "ctr1.tar.gz")
	if _, err := os.Stat(expectedArchiveFile); !os.IsNotExist(err) {
		t.Errorf("expected archive file %s to be cleaned up, but it exists", expectedArchiveFile)
	}
}
