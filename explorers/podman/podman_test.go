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

package podman

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/google/container-explorer/explorers"
	"github.com/google/container-explorer/utils"
	_ "github.com/mattn/go-sqlite3"
)

// helper to populate passwd file
func createMockPasswd(t *testing.T, imageroot string, lines []string) {
	passwdDir := filepath.Join(imageroot, "etc")
	if err := os.MkdirAll(passwdDir, 0755); err != nil {
		t.Fatalf("failed to create etc dir: %v", err)
	}
	data := []byte("")
	for _, line := range lines {
		data = append(data, []byte(line+"\n")...)
	}
	if err := os.WriteFile(filepath.Join(passwdDir, "passwd"), data, 0600); err != nil {
		t.Fatalf("failed to write passwd file: %v", err)
	}
}

// helper to create mock SQLite db for tasks
func createMockSQLiteDB(t *testing.T, dbfile string, states map[string]string) {
	if err := os.MkdirAll(filepath.Dir(dbfile), 0755); err != nil {
		t.Fatalf("failed to create db directory: %v", err)
	}

	db, err := sql.Open("sqlite3", dbfile)
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE ContainerState (ID TEXT PRIMARY KEY, JSON TEXT)")
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	for id, stateJSON := range states {
		_, err = db.Exec("INSERT INTO ContainerState (ID, JSON) VALUES (?, ?)", id, stateJSON)
		if err != nil {
			t.Fatalf("failed to insert state: %v", err)
		}
	}
}

func TestNewExplorer_NoRootDirs(t *testing.T) {
	tmpDir := t.TempDir()

	// NewExplorer should succeed even if passwd is missing, but with empty podmanRootDirs
	exp, err := NewExplorer(tmpDir)
	if err != nil {
		t.Fatalf("NewExplorer failed: %v", err)
	}

	pDirs := exp.(*explorer).podmanRootDirs
	if len(pDirs) != 0 {
		t.Errorf("expected 0 podman root directories, got %d: %v", len(pDirs), pDirs)
	}
}

func TestNewExplorer_SuccessRootless(t *testing.T) {
	tmpDir := t.TempDir()

	createMockPasswd(t, tmpDir, []string{
		"mockuser:x:1000:1000:Mock User:/home/mockuser:/bin/bash",
	})
	graphRoot := filepath.Join(tmpDir, "home", "mockuser", ".local", "share", "containers", "storage")
	if err := os.MkdirAll(graphRoot, 0755); err != nil {
		t.Fatalf("failed to create graphroot: %v", err)
	}

	exp, err := NewExplorer(tmpDir)
	if err != nil {
		t.Fatalf("NewExplorer failed: %v", err)
	}

	if exp.Type() != "podman" {
		t.Errorf("expected type 'podman', got '%s'", exp.Type())
	}

	pDirs := exp.(*explorer).podmanRootDirs
	if len(pDirs) != 1 {
		t.Fatalf("expected 1 podman root directory, got %d: %v", len(pDirs), pDirs)
	}

	expectedDir := filepath.Join(tmpDir, "home", "mockuser", ".local", "share", "containers")
	if pDirs[0] != expectedDir {
		t.Errorf("expected podman root dir '%s', got '%s'", expectedDir, pDirs[0])
	}
}

func TestListNamespacesAndSnapshots(t *testing.T) {
	// These are not implemented/supported in Podman, verify they return nil, nil
	tmpDir := t.TempDir()
	createMockPasswd(t, tmpDir, []string{"mockuser:x:1000:1000:Mock User:/home/mockuser:/bin/bash"})
	graphRoot := filepath.Join(tmpDir, "home", "mockuser", ".local", "share", "containers", "storage")
	_ = os.MkdirAll(graphRoot, 0755)

	exp, err := NewExplorer(tmpDir)
	if err != nil {
		t.Fatalf("NewExplorer failed: %v", err)
	}

	nss, err := exp.ListNamespaces(context.Background())
	if err != nil || nss != nil {
		t.Errorf("ListNamespaces expected nil, nil; got %v, %v", nss, err)
	}

	snaps, err := exp.ListSnapshots(context.Background())
	if err != nil || snaps != nil {
		t.Errorf("ListSnapshots expected nil, nil; got %v, %v", snaps, err)
	}

	if exp.SnapshotRoot("overlayfs") != "" {
		t.Errorf("SnapshotRoot expected empty string, got '%s'", exp.SnapshotRoot("overlayfs"))
	}
}

func TestListContainers(t *testing.T) {
	tmpDir := t.TempDir()
	createMockPasswd(t, tmpDir, []string{"mockuser:x:1000:1000:Mock User:/home/mockuser:/bin/bash"})
	storageDir := filepath.Join(tmpDir, "home", "mockuser", ".local", "share", "containers", "storage")
	_ = os.MkdirAll(storageDir, 0755)

	exp, err := NewExplorer(tmpDir)
	if err != nil {
		t.Fatalf("NewExplorer failed: %v", err)
	}

	// Case 1: Empty containers list
	ctrs, err := exp.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers failed: %v", err)
	}
	if len(ctrs) != 0 {
		t.Errorf("expected 0 containers, got %d", len(ctrs))
	}

	// Case 2: Populate containers.json
	containerID := "c1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234"
	metadata := containerMetadata{
		ImageName: "docker.io/library/ubuntu:latest",
		ImageID:   "i1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234",
		Name:      "my-ubuntu-container",
		CreatedAt: 1718818818,
	}
	metadataBytes, _ := json.Marshal(metadata)

	now := time.Now().UTC().Truncate(time.Second)

	configs := []containerConfig{
		{
			ID:       containerID,
			Names:    []string{"my-ubuntu-container"},
			Image:    "i1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234",
			Layer:    "l1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234",
			Metadata: string(metadataBytes),
			Created:  now.Format(time.RFC3339Nano),
		},
	}
	configsBytes, _ := json.Marshal(configs)

	overlayContainersDir := filepath.Join(storageDir, "overlay-containers")
	_ = os.MkdirAll(overlayContainersDir, 0755)
	if err := os.WriteFile(filepath.Join(overlayContainersDir, "containers.json"), configsBytes, 0600); err != nil {
		t.Fatalf("failed to write containers.json: %v", err)
	}

	ctrs, err = exp.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers failed: %v", err)
	}

	if len(ctrs) != 1 {
		t.Fatalf("expected 1 container, got %d", len(ctrs))
	}

	ctr := ctrs[0]
	if ctr.ID != containerID {
		t.Errorf("expected ID %s, got %s", containerID, ctr.ID)
	}
	if ctr.Name != "my-ubuntu-container" {
		t.Errorf("expected Name 'my-ubuntu-container', got '%s'", ctr.Name)
	}
	if ctr.Image != "docker.io/library/ubuntu:latest" {
		t.Errorf("expected Image 'docker.io/library/ubuntu:latest', got '%s'", ctr.Image)
	}
	if !ctr.CreatedAt.Equal(now) {
		t.Errorf("expected CreatedAt %v, got %v", now, ctr.CreatedAt)
	}
}

func TestGetContainerByID(t *testing.T) {
	tmpDir := t.TempDir()
	createMockPasswd(t, tmpDir, []string{"mockuser:x:1000:1000:Mock User:/home/mockuser:/bin/bash"})
	storageDir := filepath.Join(tmpDir, "home", "mockuser", ".local", "share", "containers", "storage")
	_ = os.MkdirAll(storageDir, 0755)

	exp, err := NewExplorer(tmpDir)
	if err != nil {
		t.Fatalf("NewExplorer failed: %v", err)
	}

	containerID := "c1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234"
	metadata := containerMetadata{
		ImageName: "ubuntu:latest",
		Name:      "my-ubuntu-container",
	}
	metadataBytes, _ := json.Marshal(metadata)

	configs := []containerConfig{
		{
			ID:       containerID,
			Names:    []string{"my-ubuntu-container"},
			Metadata: string(metadataBytes),
		},
	}
	configsBytes, _ := json.Marshal(configs)
	overlayContainersDir := filepath.Join(storageDir, "overlay-containers")
	_ = os.MkdirAll(overlayContainersDir, 0755)
	_ = os.WriteFile(filepath.Join(overlayContainersDir, "containers.json"), configsBytes, 0600)

	// Case 1: Search by ID
	ctr, err := exp.GetContainerByID(context.Background(), containerID)
	if err != nil {
		t.Fatalf("GetContainerByID failed: %v", err)
	}
	if ctr.ID != containerID {
		t.Errorf("expected ID %s, got %s", containerID, ctr.ID)
	}

	// Case 2: Search by Name
	ctr, err = exp.GetContainerByID(context.Background(), "my-ubuntu-container")
	if err != nil {
		t.Fatalf("GetContainerByID failed: %v", err)
	}
	if ctr.ID != containerID {
		t.Errorf("expected ID %s, got %s", containerID, ctr.ID)
	}

	// Case 3: Not found
	ctr, err = exp.GetContainerByID(context.Background(), "non_existent")
	if err == nil {
		t.Errorf("GetContainerByID expected error for non-existent container, got nil")
	}
	if ctr != nil {
		t.Errorf("expected container to be nil on error, got %+v", ctr)
	}
}

func TestListImages(t *testing.T) {
	tmpDir := t.TempDir()
	createMockPasswd(t, tmpDir, []string{"mockuser:x:1000:1000:Mock User:/home/mockuser:/bin/bash"})
	storageDir := filepath.Join(tmpDir, "home", "mockuser", ".local", "share", "containers", "storage")
	_ = os.MkdirAll(storageDir, 0755)

	exp, err := NewExplorer(tmpDir)
	if err != nil {
		t.Fatalf("NewExplorer failed: %v", err)
	}

	// Populate images.json
	imagesDir := filepath.Join(storageDir, "overlay-images")
	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		t.Fatalf("failed to create images dir: %v", err)
	}

	imageID := "i1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234"
	now := time.Now().UTC().Truncate(time.Second)

	imagesData := []containerImage{
		{
			ID:      imageID,
			Digest:  "sha256:abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234",
			Names:   []string{"docker.io/library/ubuntu:latest"},
			Created: now.Format(time.RFC3339Nano),
		},
	}
	imagesBytes, _ := json.Marshal(imagesData)
	if err := os.WriteFile(filepath.Join(imagesDir, "images.json"), imagesBytes, 0600); err != nil {
		t.Fatalf("failed to write images.json: %v", err)
	}

	// Populate mock manifest
	imageManifestDir := filepath.Join(imagesDir, imageID)
	if err := os.MkdirAll(imageManifestDir, 0755); err != nil {
		t.Fatalf("failed to create image manifest dir: %v", err)
	}
	mockManifest := struct {
		MediaType string `json:"mediaType"`
	}{
		MediaType: "application/vnd.oci.image.manifest.v1+json",
	}
	manifestBytes, _ := json.Marshal(mockManifest)
	if err := os.WriteFile(filepath.Join(imageManifestDir, "manifest"), manifestBytes, 0600); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	imgs, err := exp.ListImages(context.Background())
	if err != nil {
		t.Fatalf("ListImages failed: %v", err)
	}

	if len(imgs) != 1 {
		t.Fatalf("expected 1 image, got %d", len(imgs))
	}

	img := imgs[0]
	if img.Name != "docker.io/library/ubuntu:latest" {
		t.Errorf("expected Name 'docker.io/library/ubuntu:latest', got '%s'", img.Name)
	}
	if string(img.Target.Digest) != "sha256:abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234" {
		t.Errorf("expected Digest 'sha256:abcd...', got '%s'", string(img.Target.Digest))
	}
	if img.Target.MediaType != "application/vnd.oci.image.manifest.v1+json" {
		t.Errorf("expected MediaType 'application/vnd.oci...', got '%s'", img.Target.MediaType)
	}
	if !img.CreatedAt.Equal(now) {
		t.Errorf("expected CreatedAt %v, got %v", now, img.CreatedAt)
	}
}

func TestListTasks(t *testing.T) {
	tmpDir := t.TempDir()
	createMockPasswd(t, tmpDir, []string{"mockuser:x:1000:1000:Mock User:/home/mockuser:/bin/bash"})
	storageDir := filepath.Join(tmpDir, "home", "mockuser", ".local", "share", "containers", "storage")
	_ = os.MkdirAll(storageDir, 0755)

	exp, err := NewExplorer(tmpDir)
	if err != nil {
		t.Fatalf("NewExplorer failed: %v", err)
	}

	// 1. Verify ListTasks returns empty when db.sql is missing
	tasks, err := exp.ListTasks(context.Background())
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}

	// 2. Create mock SQLite database
	dbFile := filepath.Join(storageDir, "db.sql")

	// State 3 corresponds to running, state 5 to paused
	// {"state": 3, "pid": 1234}
	// {"state": 5, "pid": 5678}
	mockStates := map[string]string{
		"container_1": `{"state": 3, "pid": 1234}`,
		"container_2": `{"state": 5, "pid": 5678}`,
	}
	createMockSQLiteDB(t, dbFile, mockStates)

	tasks, err = exp.ListTasks(context.Background())
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}

	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}

	var t1, t2 explorers.Task
	for _, task := range tasks {
		if task.Name == "container_1" {
			t1 = task
		}
		if task.Name == "container_2" {
			t2 = task
		}
	}

	if t1.PID != 1234 || t1.Status != "running" {
		t.Errorf("expected container_1 PID 1234 and Status 'running', got PID %d Status '%s'", t1.PID, t1.Status)
	}
	if t2.PID != 5678 || t2.Status != "paused" {
		t.Errorf("expected container_2 PID 5678 and Status 'paused', got PID %d Status '%s'", t2.PID, t2.Status)
	}
}

func TestInfoContainer(t *testing.T) {
	tmpDir := t.TempDir()
	createMockPasswd(t, tmpDir, []string{"mockuser:x:1000:1000:Mock User:/home/mockuser:/bin/bash"})
	storageDir := filepath.Join(tmpDir, "home", "mockuser", ".local", "share", "containers", "storage")
	_ = os.MkdirAll(storageDir, 0755)

	exp, err := NewExplorer(tmpDir)
	if err != nil {
		t.Fatalf("NewExplorer failed: %v", err)
	}

	containerID := "c1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234"
	metadata := containerMetadata{
		ImageName: "ubuntu:latest",
		Name:      "my-ubuntu-container",
	}
	metadataBytes, _ := json.Marshal(metadata)

	configs := []containerConfig{
		{
			ID:       containerID,
			Names:    []string{"my-ubuntu-container"},
			Metadata: string(metadataBytes),
			Created:  "2026-06-19T04:00:00Z",
		},
	}
	configsBytes, _ := json.Marshal(configs)
	overlayContainersDir := filepath.Join(storageDir, "overlay-containers")
	_ = os.MkdirAll(overlayContainersDir, 0755)
	_ = os.WriteFile(filepath.Join(overlayContainersDir, "containers.json"), configsBytes, 0600)

	// Create OCI Spec config.json
	cDir := filepath.Join(overlayContainersDir, containerID, "userdata")
	if err := os.MkdirAll(cDir, 0755); err != nil {
		t.Fatalf("failed to create userdata dir: %v", err)
	}

	mockSpec := struct {
		Annotations map[string]string `json:"annotations"`
	}{
		Annotations: map[string]string{
			"annotation-key": "annotation-value",
		},
	}
	specBytes, _ := json.Marshal(mockSpec)
	if err := os.WriteFile(filepath.Join(cDir, "config.json"), specBytes, 0600); err != nil {
		t.Fatalf("failed to write OCI config.json: %v", err)
	}

	// Case 1: showSpec = false
	info, err := exp.InfoContainer(context.Background(), containerID, false)
	if err != nil {
		t.Fatalf("InfoContainer failed: %v", err)
	}

	// InfoContainer returns:
	// struct {
	//     containers.Container
	//     Spec any
	// }
	v := reflect.ValueOf(info)
	if v.Kind() != reflect.Struct {
		t.Fatalf("expected struct, got %s", v.Kind())
	}

	ctrVal := v.FieldByName("Container")
	if !ctrVal.IsValid() {
		t.Fatalf("Container field not found")
	}

	idVal := ctrVal.FieldByName("ID")
	if idVal.String() != containerID {
		t.Errorf("expected ID %s, got %s", containerID, idVal.String())
	}

	// Case 2: showSpec = true
	infoSpec, err := exp.InfoContainer(context.Background(), containerID, true)
	if err != nil {
		t.Fatalf("InfoContainer failed: %v", err)
	}
	// When showSpec=true, it returns ociSpec directly
	vSpec := reflect.ValueOf(infoSpec)
	annotationsVal := vSpec.FieldByName("Annotations")
	if !annotationsVal.IsValid() {
		t.Fatalf("Annotations field not found in returned Spec")
	}
}

func TestContainerDrift(t *testing.T) {
	tmpDir := t.TempDir()
	createMockPasswd(t, tmpDir, []string{"mockuser:x:1000:1000:Mock User:/home/mockuser:/bin/bash"})
	storageDir := filepath.Join(tmpDir, "home", "mockuser", ".local", "share", "containers", "storage")
	_ = os.MkdirAll(storageDir, 0755)

	exp, err := NewExplorer(tmpDir)
	if err != nil {
		t.Fatalf("NewExplorer failed: %v", err)
	}

	containerID := "c1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234"
	layerID := "layer-123"
	configs := []containerConfig{
		{
			ID:    containerID,
			Layer: layerID,
		},
	}
	configsBytes, _ := json.Marshal(configs)
	overlayContainersDir := filepath.Join(storageDir, "overlay-containers")
	_ = os.MkdirAll(overlayContainersDir, 0755)
	_ = os.WriteFile(filepath.Join(overlayContainersDir, "containers.json"), configsBytes, 0600)

	// Setup overlay layer link
	overlayDir := filepath.Join(storageDir, "overlay")
	layerDir := filepath.Join(overlayDir, layerID)
	if err := os.MkdirAll(layerDir, 0755); err != nil {
		t.Fatalf("failed to create layer dir: %v", err)
	}

	// Write link file -> points to "link-name"
	if err := os.WriteFile(filepath.Join(layerDir, "link"), []byte("link-name"), 0600); err != nil {
		t.Fatalf("failed to write link file: %v", err)
	}

	// Create upperdir: <overlayDir>/l/link-name
	upperDir := filepath.Join(overlayDir, "l", "link-name")
	if err := os.MkdirAll(upperDir, 0755); err != nil {
		t.Fatalf("failed to create upperDir: %v", err)
	}

	// Write some drift files in upperdir
	modifiedFile := filepath.Join(upperDir, "etc-config.conf")
	if err := os.WriteFile(modifiedFile, []byte("new settings"), 0600); err != nil {
		t.Fatalf("failed to write drift file: %v", err)
	}

	drifts, err := exp.ContainerDrift(context.Background(), "", false, containerID)
	if err != nil {
		t.Fatalf("ContainerDrift failed: %v", err)
	}

	if len(drifts) != 1 {
		t.Fatalf("expected 1 drift, got %d", len(drifts))
	}

	drift := drifts[0]
	if drift.ContainerID != containerID {
		t.Errorf("expected ContainerID %s, got %s", containerID, drift.ContainerID)
	}

	if len(drift.AddedOrModified) != 1 {
		t.Fatalf("expected 1 added/modified file, got %d", len(drift.AddedOrModified))
	}

	expectedPath := "/etc-config.conf"
	if drift.AddedOrModified[0].FullPath != expectedPath {
		t.Errorf("expected drift path '%s', got '%s'", expectedPath, drift.AddedOrModified[0].FullPath)
	}
}

type mockCommandCall struct {
	Name string
	Args []string
}

type mockCommandResponse struct {
	Output []byte
	Stdout string
	Stderr string
	Err    error
}

type mockCommandRunner struct {
	Calls     []mockCommandCall
	Responses map[string]mockCommandResponse
}

func (m *mockCommandRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	m.Calls = append(m.Calls, mockCommandCall{Name: name, Args: args})
	if r, ok := m.Responses[name]; ok {
		return r.Output, r.Err
	}
	return nil, nil
}

func (m *mockCommandRunner) RunSeparate(_ context.Context, name string, args []string, stdout, stderr io.Writer) error {
	m.Calls = append(m.Calls, mockCommandCall{Name: name, Args: args})
	if r, ok := m.Responses[name]; ok {
		if r.Stdout != "" {
			_, _ = stdout.Write([]byte(r.Stdout))
		}
		if r.Stderr != "" {
			_, _ = stderr.Write([]byte(r.Stderr))
		}
		return r.Err
	}
	return nil
}

func (m *mockCommandRunner) RunWithoutContext(name string, args ...string) ([]byte, error) {
	m.Calls = append(m.Calls, mockCommandCall{Name: name, Args: args})
	if r, ok := m.Responses[name]; ok {
		return r.Output, r.Err
	}
	return nil, nil
}

func TestExportContainer(t *testing.T) {
	tmpDir := t.TempDir()
	createMockPasswd(t, tmpDir, []string{"mockuser:x:1000:1000:Mock User:/home/mockuser:/bin/bash"})
	storageDir := filepath.Join(tmpDir, "home", "mockuser", ".local", "share", "containers", "storage")
	_ = os.MkdirAll(storageDir, 0755)

	exp, err := NewExplorer(tmpDir)
	if err != nil {
		t.Fatalf("NewExplorer failed: %v", err)
	}

	containerID := "c1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234"
	metadata := containerMetadata{
		ImageName: "ubuntu:latest",
		Name:      "my-ubuntu-container",
	}
	metadataBytes, _ := json.Marshal(metadata)

	configs := []containerConfig{
		{
			ID:       containerID,
			Names:    []string{"my-ubuntu-container"},
			Metadata: string(metadataBytes),
			Layer:    "layer-123",
		},
	}
	configsBytes, _ := json.Marshal(configs)
	overlayContainersDir := filepath.Join(storageDir, "overlay-containers")
	_ = os.MkdirAll(overlayContainersDir, 0755)
	_ = os.WriteFile(filepath.Join(overlayContainersDir, "containers.json"), configsBytes, 0600)

	// Setup overlay layer link and lower
	overlayDir := filepath.Join(storageDir, "overlay")
	layerDir := filepath.Join(overlayDir, "layer-123")
	_ = os.MkdirAll(layerDir, 0755)
	_ = os.WriteFile(filepath.Join(layerDir, "link"), []byte("link-name"), 0600)
	_ = os.WriteFile(filepath.Join(layerDir, "lower"), []byte("lower-name"), 0600)

	// Override runner
	origRunner := utils.Runner
	mockRunner := &mockCommandRunner{
		Responses: map[string]mockCommandResponse{
			"losetup": {Stdout: "/dev/loop123\n", Err: nil},
		},
	}
	utils.Runner = mockRunner
	defer func() { utils.Runner = origRunner }()

	outputDir := filepath.Join(tmpDir, "output")
	exportOptions := map[string]bool{
		"archive": true,
		"image":   true,
	}

	err = exp.ExportContainer(context.Background(), containerID, outputDir, exportOptions)
	if err != nil {
		t.Fatalf("ExportContainer failed: %v", err)
	}

	// Verify command calls
	callNames := make([]string, len(mockRunner.Calls))
	for i, c := range mockRunner.Calls {
		callNames[i] = c.Name
	}
	t.Logf("mockRunner calls: %v", callNames)

	hasMount := false
	hasUmount := false
	hasTar := false
	for _, name := range callNames {
		if name == "mount" {
			hasMount = true
		}
		if name == "umount" {
			hasUmount = true
		}
		if name == "tar" {
			hasTar = true
		}
	}

	if !hasMount {
		t.Errorf("expected 'mount' to be executed")
	}
	if !hasUmount {
		t.Errorf("expected 'umount' to be executed")
	}
	if !hasTar {
		t.Errorf("expected 'tar' to be executed")
	}
}

func TestExportAllContainers(t *testing.T) {
	tmpDir := t.TempDir()
	createMockPasswd(t, tmpDir, []string{"mockuser:x:1000:1000:Mock User:/home/mockuser:/bin/bash"})
	storageDir := filepath.Join(tmpDir, "home", "mockuser", ".local", "share", "containers", "storage")
	_ = os.MkdirAll(storageDir, 0755)

	exp, err := NewExplorer(tmpDir)
	if err != nil {
		t.Fatalf("NewExplorer failed: %v", err)
	}

	containerID := "c1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234"
	metadata := containerMetadata{
		ImageName: "ubuntu:latest",
		Name:      "my-ubuntu-container",
	}
	metadataBytes, _ := json.Marshal(metadata)

	configs := []containerConfig{
		{
			ID:       containerID,
			Names:    []string{"my-ubuntu-container"},
			Metadata: string(metadataBytes),
			Layer:    "layer-123",
		},
	}
	configsBytes, _ := json.Marshal(configs)
	overlayContainersDir := filepath.Join(storageDir, "overlay-containers")
	_ = os.MkdirAll(overlayContainersDir, 0755)
	_ = os.WriteFile(filepath.Join(overlayContainersDir, "containers.json"), configsBytes, 0600)

	// Setup overlay layer link and lower
	overlayDir := filepath.Join(storageDir, "overlay")
	layerDir := filepath.Join(overlayDir, "layer-123")
	_ = os.MkdirAll(layerDir, 0755)
	_ = os.WriteFile(filepath.Join(layerDir, "link"), []byte("link-name"), 0600)
	_ = os.WriteFile(filepath.Join(layerDir, "lower"), []byte("lower-name"), 0600)

	// Override runner
	origRunner := utils.Runner
	mockRunner := &mockCommandRunner{
		Responses: map[string]mockCommandResponse{
			"losetup": {Stdout: "/dev/loop123\n", Err: nil},
		},
	}
	utils.Runner = mockRunner
	defer func() { utils.Runner = origRunner }()

	outputDir := filepath.Join(tmpDir, "output")
	exportOptions := map[string]bool{
		"archive": true,
	}

	err = exp.ExportAllContainers(context.Background(), outputDir, exportOptions, nil, false)
	if err != nil {
		t.Fatalf("ExportAllContainers failed: %v", err)
	}

	// Verify we attempted to export the container
	exportedArchive := filepath.Join(outputDir, containerID+".tar.gz")
	hasTar := false
	for _, c := range mockRunner.Calls {
		if c.Name == "tar" {
			hasTar = true
			foundOut := false
			for _, arg := range c.Args {
				if arg == exportedArchive {
					foundOut = true
				}
			}
			if !foundOut {
				t.Errorf("expected tar command to export to '%s', args were: %v", exportedArchive, c.Args)
			}
		}
	}
	if !hasTar {
		t.Errorf("expected 'tar' to be executed")
	}
}

func TestListContainers_MalformedMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	createMockPasswd(t, tmpDir, []string{"mockuser:x:1000:1000:Mock User:/home/mockuser:/bin/bash"})
	storageDir := filepath.Join(tmpDir, "home", "mockuser", ".local", "share", "containers", "storage")
	_ = os.MkdirAll(storageDir, 0755)

	exp, err := NewExplorer(tmpDir)
	if err != nil {
		t.Fatalf("NewExplorer failed: %v", err)
	}

	configs := []containerConfig{
		{
			ID:       "container-1",
			Metadata: "{malformed json",
		},
		{
			ID:       "container-2",
			Metadata: `{"image-name":"ubuntu","name":"valid-container"}`,
		},
	}
	configsBytes, _ := json.Marshal(configs)
	overlayContainersDir := filepath.Join(storageDir, "overlay-containers")
	_ = os.MkdirAll(overlayContainersDir, 0755)
	_ = os.WriteFile(filepath.Join(overlayContainersDir, "containers.json"), configsBytes, 0600)

	ctrs, err := exp.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers failed: %v", err)
	}

	// Should skip container-1 and only return container-2
	if len(ctrs) != 1 {
		t.Errorf("expected 1 container, got %d", len(ctrs))
	} else if ctrs[0].ID != "container-2" {
		t.Errorf("expected container 'container-2', got '%s'", ctrs[0].ID)
	}
}

func TestListImages_MalformedImagesJSON(t *testing.T) {
	tmpDir := t.TempDir()
	createMockPasswd(t, tmpDir, []string{"mockuser:x:1000:1000:Mock User:/home/mockuser:/bin/bash"})
	storageDir := filepath.Join(tmpDir, "home", "mockuser", ".local", "share", "containers", "storage")
	_ = os.MkdirAll(storageDir, 0755)

	exp, err := NewExplorer(tmpDir)
	if err != nil {
		t.Fatalf("NewExplorer failed: %v", err)
	}

	imagesDir := filepath.Join(storageDir, "overlay-images")
	_ = os.MkdirAll(imagesDir, 0755)
	_ = os.WriteFile(filepath.Join(imagesDir, "images.json"), []byte("{malformed json"), 0600)

	imgs, err := exp.ListImages(context.Background())
	if err != nil {
		t.Fatalf("ListImages failed: %v", err)
	}
	// Should log error and skip, returning empty list
	if len(imgs) != 0 {
		t.Errorf("expected 0 images, got %d", len(imgs))
	}
}

func TestListImages_MissingOrMalformedManifest(t *testing.T) {
	tmpDir := t.TempDir()
	createMockPasswd(t, tmpDir, []string{"mockuser:x:1000:1000:Mock User:/home/mockuser:/bin/bash"})
	storageDir := filepath.Join(tmpDir, "home", "mockuser", ".local", "share", "containers", "storage")
	_ = os.MkdirAll(storageDir, 0755)

	exp, err := NewExplorer(tmpDir)
	if err != nil {
		t.Fatalf("NewExplorer failed: %v", err)
	}

	imagesDir := filepath.Join(storageDir, "overlay-images")
	_ = os.MkdirAll(imagesDir, 0755)

	imagesData := []containerImage{
		{
			ID:     "image-1",
			Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			Names:  []string{"ubuntu:latest"},
		},
		{
			ID:     "image-2",
			Digest: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
			Names:  []string{"alpine:latest"},
		},
	}
	imagesBytes, _ := json.Marshal(imagesData)
	_ = os.WriteFile(filepath.Join(imagesDir, "images.json"), imagesBytes, 0600)

	// image-1: missing manifest
	// image-2: malformed manifest
	image2ManifestDir := filepath.Join(imagesDir, "image-2")
	_ = os.MkdirAll(image2ManifestDir, 0755)
	_ = os.WriteFile(filepath.Join(image2ManifestDir, "manifest"), []byte("{malformed json"), 0600)

	imgs, err := exp.ListImages(context.Background())
	if err != nil {
		t.Fatalf("ListImages failed: %v", err)
	}

	// Should still return both images, but their media types will be empty
	if len(imgs) != 2 {
		t.Fatalf("expected 2 images, got %d", len(imgs))
	}
	for _, img := range imgs {
		if img.Target.MediaType != "" {
			t.Errorf("expected empty media type, got '%s'", img.Target.MediaType)
		}
	}
}

func TestListTasks_ErrorPaths(t *testing.T) {
	tmpDir := t.TempDir()
	createMockPasswd(t, tmpDir, []string{"mockuser:x:1000:1000:Mock User:/home/mockuser:/bin/bash"})
	storageDir := filepath.Join(tmpDir, "home", "mockuser", ".local", "share", "containers", "storage")
	_ = os.MkdirAll(storageDir, 0755)

	exp, err := NewExplorer(tmpDir)
	if err != nil {
		t.Fatalf("NewExplorer failed: %v", err)
	}

	dbFile := filepath.Join(storageDir, "db.sql")

	// Case 1: Query failure (missing ContainerState table)
	db, err := sql.Open("sqlite3", dbFile)
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	// Create some other table
	_, _ = db.Exec("CREATE TABLE Dummy (ID TEXT)")
	db.Close()

	_, err = exp.ListTasks(context.Background())
	if err == nil {
		t.Errorf("ListTasks expected query error, got nil")
	}

	// Clean up db file
	_ = os.Remove(dbFile)

	// Case 2: Malformed JSON in ContainerState table
	mockStates := map[string]string{
		"container_1": `{malformed json`,
	}
	createMockSQLiteDB(t, dbFile, mockStates)

	_, err = exp.ListTasks(context.Background())
	if err == nil {
		t.Errorf("ListTasks expected json parsing error, got nil")
	}
}

func TestInfoContainer_ErrorPaths(t *testing.T) {
	tmpDir := t.TempDir()
	createMockPasswd(t, tmpDir, []string{"mockuser:x:1000:1000:Mock User:/home/mockuser:/bin/bash"})
	storageDir := filepath.Join(tmpDir, "home", "mockuser", ".local", "share", "containers", "storage")
	_ = os.MkdirAll(storageDir, 0755)

	exp, err := NewExplorer(tmpDir)
	if err != nil {
		t.Fatalf("NewExplorer failed: %v", err)
	}

	containerID := "c1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234"
	metadata := containerMetadata{
		ImageName: "ubuntu:latest",
		Name:      "my-ubuntu-container",
	}
	metadataBytes, _ := json.Marshal(metadata)

	configs := []containerConfig{
		{
			ID:       containerID,
			Names:    []string{"my-ubuntu-container"},
			Metadata: string(metadataBytes),
		},
	}
	configsBytes, _ := json.Marshal(configs)
	overlayContainersDir := filepath.Join(storageDir, "overlay-containers")
	_ = os.MkdirAll(overlayContainersDir, 0755)
	_ = os.WriteFile(filepath.Join(overlayContainersDir, "containers.json"), configsBytes, 0600)

	// Case 1: Missing spec file
	_, err = exp.InfoContainer(context.Background(), containerID, true)
	if err == nil {
		t.Errorf("InfoContainer expected error for missing config.json, got nil")
	}

	// Case 2: Malformed spec JSON
	cDir := filepath.Join(overlayContainersDir, containerID, "userdata")
	_ = os.MkdirAll(cDir, 0755)
	_ = os.WriteFile(filepath.Join(cDir, "config.json"), []byte("{malformed json"), 0600)

	_, err = exp.InfoContainer(context.Background(), containerID, true)
	if err == nil {
		t.Errorf("InfoContainer expected error for malformed config.json, got nil")
	}
}

func TestContainerDrift_MissingLink(t *testing.T) {
	tmpDir := t.TempDir()
	createMockPasswd(t, tmpDir, []string{"mockuser:x:1000:1000:Mock User:/home/mockuser:/bin/bash"})
	storageDir := filepath.Join(tmpDir, "home", "mockuser", ".local", "share", "containers", "storage")
	_ = os.MkdirAll(storageDir, 0755)

	exp, err := NewExplorer(tmpDir)
	if err != nil {
		t.Fatalf("NewExplorer failed: %v", err)
	}

	containerID := "c1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234"
	layerID := "layer-123"
	configs := []containerConfig{
		{
			ID:    containerID,
			Layer: layerID,
		},
	}
	configsBytes, _ := json.Marshal(configs)
	overlayContainersDir := filepath.Join(storageDir, "overlay-containers")
	_ = os.MkdirAll(overlayContainersDir, 0755)
	_ = os.WriteFile(filepath.Join(overlayContainersDir, "containers.json"), configsBytes, 0600)

	// Setup overlay layer directory without the link file
	overlayDir := filepath.Join(storageDir, "overlay")
	layerDir := filepath.Join(overlayDir, layerID)
	_ = os.MkdirAll(layerDir, 0755)

	drifts, err := exp.ContainerDrift(context.Background(), "", false, containerID)
	if err != nil {
		t.Fatalf("ContainerDrift failed: %v", err)
	}

	// ContainerDrift should log warning and continue, returning empty list of drifts
	if len(drifts) != 0 {
		t.Errorf("expected 0 drifts, got %d", len(drifts))
	}
}
