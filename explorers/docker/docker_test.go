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

package docker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/containerd/containerd/metadata"
	bolt "go.etcd.io/bbolt"
)

func TestNewExplorer(t *testing.T) {
	tmpDir := t.TempDir()

	// Case 1: Docker root does not exist
	_, err := NewExplorer("", "containerd_root", filepath.Join(tmpDir, "non_existent"))
	if err == nil {
		t.Errorf("NewExplorer expected error for non-existent docker root, got nil")
	}

	// Case 2: Containerd root is empty
	dockerRoot := filepath.Join(tmpDir, "docker_root")
	if err := os.Mkdir(dockerRoot, 0755); err != nil {
		t.Fatalf("failed to create docker root: %v", err)
	}
	_, err = NewExplorer("", "", dockerRoot)
	if err == nil {
		t.Errorf("NewExplorer expected error for empty containerd root, got nil")
	}

	// Case 3: Valid paths (manifest and snapshot files don't need to exist for NewExplorer to succeed)
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	if err := os.Mkdir(containerdRoot, 0755); err != nil {
		t.Fatalf("failed to create containerd root: %v", err)
	}

	exp, err := NewExplorer("", containerdRoot, dockerRoot)
	if err != nil {
		t.Fatalf("NewExplorer failed: %v", err)
	}

	if exp.Type() != "docker" {
		t.Errorf("expected type 'docker', got '%s'", exp.Type())
	}
}

func TestGetContainerIDs(t *testing.T) {
	tmpDir := t.TempDir()
	dockerRoot := filepath.Join(tmpDir, "docker_root")
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	if err := os.Mkdir(dockerRoot, 0755); err != nil {
		t.Fatalf("failed to create docker root: %v", err)
	}
	if err := os.Mkdir(containerdRoot, 0755); err != nil {
		t.Fatalf("failed to create containerd root: %v", err)
	}

	// Create dummy container directories
	containersDir := filepath.Join(dockerRoot, "containers")
	if err := os.Mkdir(containersDir, 0755); err != nil {
		t.Fatalf("failed to create containers dir: %v", err)
	}

	cID1 := "container1111111111111111111111111111111111111111111111111111111111"
	cID2 := "container2222222222222222222222222222222222222222222222222222222222"
	if err := os.Mkdir(filepath.Join(containersDir, cID1), 0755); err != nil {
		t.Fatalf("failed to create container dir 1: %v", err)
	}
	if err := os.Mkdir(filepath.Join(containersDir, cID2), 0755); err != nil {
		t.Fatalf("failed to create container dir 2: %v", err)
	}

	exp, err := NewExplorer("", containerdRoot, dockerRoot)
	if err != nil {
		t.Fatalf("failed to create explorer: %v", err)
	}

	ids, err := exp.(*explorer).GetContainerIDs(context.Background(), "")
	if err != nil {
		t.Fatalf("GetContainerIDs failed: %v", err)
	}

	if len(ids) != 2 {
		t.Errorf("expected 2 container IDs, got %d", len(ids))
	}

	found1, found2 := false, false
	for _, id := range ids {
		if id == cID1 {
			found1 = true
		}
		if id == cID2 {
			found2 = true
		}
	}
	if !found1 || !found2 {
		t.Errorf("GetContainerIDs did not return expected IDs. Got: %v", ids)
	}
}

func TestReadContainerConfig(t *testing.T) {
	tmpDir := t.TempDir()
	dockerRoot := filepath.Join(tmpDir, "docker_root")
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	if err := os.Mkdir(dockerRoot, 0755); err != nil {
		t.Fatalf("failed to create docker root: %v", err)
	}
	if err := os.Mkdir(containerdRoot, 0755); err != nil {
		t.Fatalf("failed to create containerd root: %v", err)
	}

	exp, err := NewExplorer("", containerdRoot, dockerRoot)
	if err != nil {
		t.Fatalf("failed to create explorer: %v", err)
	}

	// Case 1: Container does not exist
	_, err = exp.(*explorer).ReadContainerConfig(context.Background(), "non_existent")
	if err == nil {
		t.Errorf("ReadContainerConfig expected error for non-existent container, got nil")
	}

	// Case 2: Container directory exists, but config.v2.json is missing
	cID := "test_container"
	containersDir := filepath.Join(dockerRoot, "containers")
	cDir := filepath.Join(containersDir, cID)
	if err := os.MkdirAll(cDir, 0755); err != nil {
		t.Fatalf("failed to create container dir: %v", err)
	}
	_, err = exp.(*explorer).ReadContainerConfig(context.Background(), cID)
	if err == nil {
		t.Errorf("ReadContainerConfig expected error for missing config.v2.json, got nil")
	}

	// Case 3: Valid config.v2.json
	now := time.Now().UTC().Truncate(time.Second) // Truncate to avoid millisecond/nanosecond comparison issues after JSON serialization
	expectedConfig := ConfigFile{
		ID:      cID,
		Name:    "/my-test-container",
		Created: now,
		Driver:  "overlay2",
		State: State{
			Running:   true,
			Pid:       1234,
			StartedAt: now,
		},
		Config: Config{
			Image: "ubuntu:latest",
			Labels: map[string]string{
				"app": "test",
			},
		},
	}

	data, err := json.Marshal(expectedConfig)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	if err := os.WriteFile(filepath.Join(cDir, "config.v2.json"), data, 0600); err != nil {
		t.Fatalf("failed to write config.v2.json: %v", err)
	}

	config, err := exp.(*explorer).ReadContainerConfig(context.Background(), cID)
	if err != nil {
		t.Fatalf("ReadContainerConfig failed: %v", err)
	}

	if config.ID != expectedConfig.ID {
		t.Errorf("expected ID %s, got %s", expectedConfig.ID, config.ID)
	}
	if config.Name != expectedConfig.Name {
		t.Errorf("expected Name %s, got %s", expectedConfig.Name, config.Name)
	}
	if !config.Created.Equal(expectedConfig.Created) {
		t.Errorf("expected Created %v, got %v", expectedConfig.Created, config.Created)
	}
	if config.Driver != expectedConfig.Driver {
		t.Errorf("expected Driver %s, got %s", expectedConfig.Driver, config.Driver)
	}
	if config.State.Running != expectedConfig.State.Running {
		t.Errorf("expected State.Running %t, got %t", expectedConfig.State.Running, config.State.Running)
	}
	if config.State.Pid != expectedConfig.State.Pid {
		t.Errorf("expected State.Pid %d, got %d", expectedConfig.State.Pid, config.State.Pid)
	}
	if !config.State.StartedAt.Equal(expectedConfig.State.StartedAt) {
		t.Errorf("expected State.StartedAt %v, got %v", expectedConfig.State.StartedAt, config.State.StartedAt)
	}
	if config.Config.Image != expectedConfig.Config.Image {
		t.Errorf("expected Config.Image %s, got %s", expectedConfig.Config.Image, config.Config.Image)
	}

	// Case 4: Invalid JSON in config.v2.json
	invalidData := []byte(`{"invalid": "json",`)
	if err := os.WriteFile(filepath.Join(cDir, "config.v2.json"), invalidData, 0600); err != nil {
		t.Fatalf("failed to write invalid config.v2.json: %v", err)
	}
	_, err = exp.(*explorer).ReadContainerConfig(context.Background(), cID)
	if err == nil {
		t.Errorf("ReadContainerConfig expected error for invalid JSON, got nil")
	}
}

func TestGetCEContainer(t *testing.T) {
	tmpDir := t.TempDir()
	dockerRoot := filepath.Join(tmpDir, "docker_root")
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	if err := os.Mkdir(dockerRoot, 0755); err != nil {
		t.Fatalf("failed to create docker root: %v", err)
	}
	if err := os.Mkdir(containerdRoot, 0755); err != nil {
		t.Fatalf("failed to create containerd root: %v", err)
	}

	exp, err := NewExplorer("", containerdRoot, dockerRoot)
	if err != nil {
		t.Fatalf("failed to create explorer: %v", err)
	}

	cID := "test_container"
	containersDir := filepath.Join(dockerRoot, "containers")
	cDir := filepath.Join(containersDir, cID)
	if err := os.MkdirAll(cDir, 0755); err != nil {
		t.Fatalf("failed to create container dir: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	dockerConfig := ConfigFile{
		ID:      cID,
		Name:    "/my-test-container",
		Created: now,
		Driver:  "overlay2",
		Image:   "sha256:imagehash12345",
		State: State{
			Running:   true,
			Pid:       1234,
			StartedAt: now,
		},
		Config: Config{
			Labels: map[string]string{
				"app": "test",
			},
		},
	}

	data, err := json.Marshal(dockerConfig)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cDir, "config.v2.json"), data, 0600); err != nil {
		t.Fatalf("failed to write config.v2.json: %v", err)
	}

	ceCtr, err := exp.(*explorer).GetCEContainer(context.Background(), cID)
	if err != nil {
		t.Fatalf("GetCEContainer failed: %v", err)
	}

	if ceCtr.ID != cID {
		t.Errorf("expected ID %s, got %s", cID, ceCtr.ID)
	}
	// Name should have "/" trimmed
	if ceCtr.Name != "my-test-container" {
		t.Errorf("expected Name 'my-test-container', got '%s'", ceCtr.Name)
	}
	if ceCtr.ProcessID != 1234 {
		t.Errorf("expected ProcessID 1234, got %d", ceCtr.ProcessID)
	}
	if ceCtr.ContainerType != "docker" {
		t.Errorf("expected ContainerType 'docker', got '%s'", ceCtr.ContainerType)
	}
	if ceCtr.Status != "RUNNING" {
		t.Errorf("expected Status 'RUNNING', got '%s'", ceCtr.Status)
	}
	if ceCtr.Image != "sha256:imagehash12345" {
		t.Errorf("expected Image 'sha256:imagehash12345', got '%s'", ceCtr.Image)
	}
}

func TestListContainers(t *testing.T) {
	tmpDir := t.TempDir()
	dockerRoot := filepath.Join(tmpDir, "docker_root")
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	if err := os.Mkdir(dockerRoot, 0755); err != nil {
		t.Fatalf("failed to create docker root: %v", err)
	}
	if err := os.Mkdir(containerdRoot, 0755); err != nil {
		t.Fatalf("failed to create containerd root: %v", err)
	}

	exp, err := NewExplorer("", containerdRoot, dockerRoot)
	if err != nil {
		t.Fatalf("failed to create explorer: %v", err)
	}

	// Create 2 containers
	containersDir := filepath.Join(dockerRoot, "containers")
	for _, cID := range []string{"c1", "c2"} {
		cDir := filepath.Join(containersDir, cID)
		if err := os.MkdirAll(cDir, 0755); err != nil {
			t.Fatalf("failed to create container dir: %v", err)
		}
		dockerConfig := ConfigFile{
			ID:     cID,
			Name:   "/" + cID + "-name",
			Driver: "overlay2",
		}
		data, _ := json.Marshal(dockerConfig)
		if err := os.WriteFile(filepath.Join(cDir, "config.v2.json"), data, 0600); err != nil {
			t.Fatalf("failed to write config.v2.json: %v", err)
		}
	}

	ctrs, err := exp.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers failed: %v", err)
	}

	if len(ctrs) != 2 {
		t.Errorf("expected 2 containers, got %d", len(ctrs))
	}
}

func TestListTasks(t *testing.T) {
	tmpDir := t.TempDir()
	dockerRoot := filepath.Join(tmpDir, "docker_root")
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	if err := os.Mkdir(dockerRoot, 0755); err != nil {
		t.Fatalf("failed to create docker root: %v", err)
	}
	if err := os.Mkdir(containerdRoot, 0755); err != nil {
		t.Fatalf("failed to create containerd root: %v", err)
	}

	exp, err := NewExplorer("", containerdRoot, dockerRoot)
	if err != nil {
		t.Fatalf("failed to create explorer: %v", err)
	}

	// Create 2 containers, one running, one paused
	containersDir := filepath.Join(dockerRoot, "containers")

	// Running container
	cDir1 := filepath.Join(containersDir, "c1")
	os.MkdirAll(cDir1, 0755)
	config1 := ConfigFile{
		ID: "c1",
		State: State{
			Running: true,
			Pid:     111,
		},
	}
	data1, _ := json.Marshal(config1)
	os.WriteFile(filepath.Join(cDir1, "config.v2.json"), data1, 0600)

	// Paused container
	cDir2 := filepath.Join(containersDir, "c2")
	os.MkdirAll(cDir2, 0755)
	config2 := ConfigFile{
		ID: "c2",
		State: State{
			Running: true,
			Paused:  true,
			Pid:     222,
		},
	}
	data2, _ := json.Marshal(config2)
	os.WriteFile(filepath.Join(cDir2, "config.v2.json"), data2, 0600)

	tasks, err := exp.ListTasks(context.Background())
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}

	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}

	found1, found2 := false, false
	for _, task := range tasks {
		if task.Name == "c1" {
			found1 = true
			if task.PID != 111 {
				t.Errorf("expected PID 111, got %d", task.PID)
			}
			if task.Status != "running" {
				t.Errorf("expected status 'running', got '%s'", task.Status)
			}
		}
		if task.Name == "c2" {
			found2 = true
			if task.PID != 222 {
				t.Errorf("expected PID 222, got %d", task.PID)
			}
			if task.Status != "paused" {
				t.Errorf("expected status 'paused', got '%s'", task.Status)
			}
		}
	}
	if !found1 || !found2 {
		t.Errorf("ListTasks did not return expected tasks. Got: %v", tasks)
	}
}

func TestInfoContainer(t *testing.T) {
	tmpDir := t.TempDir()
	dockerRoot := filepath.Join(tmpDir, "docker_root")
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	if err := os.Mkdir(dockerRoot, 0755); err != nil {
		t.Fatalf("failed to create docker root: %v", err)
	}
	if err := os.Mkdir(containerdRoot, 0755); err != nil {
		t.Fatalf("failed to create containerd root: %v", err)
	}

	exp, err := NewExplorer("", containerdRoot, dockerRoot)
	if err != nil {
		t.Fatalf("failed to create explorer: %v", err)
	}

	cID := "test_container"
	containersDir := filepath.Join(dockerRoot, "containers")
	cDir := filepath.Join(containersDir, cID)
	if err := os.MkdirAll(cDir, 0755); err != nil {
		t.Fatalf("failed to create container dir: %v", err)
	}

	dockerConfig := ConfigFile{
		ID:     cID,
		Name:   "/test-container",
		Driver: "overlay2",
	}
	data, _ := json.Marshal(dockerConfig)
	if err := os.WriteFile(filepath.Join(cDir, "config.v2.json"), data, 0600); err != nil {
		t.Fatalf("failed to write config.v2.json: %v", err)
	}

	info, err := exp.InfoContainer(context.Background(), cID, false)
	if err != nil {
		t.Fatalf("InfoContainer failed: %v", err)
	}

	configFile, ok := info.(ConfigFile)
	if !ok {
		t.Fatalf("InfoContainer expected to return ConfigFile struct, got %T", info)
	}

	if configFile.ID != cID {
		t.Errorf("expected ID %s, got %s", cID, configFile.ID)
	}
}

func TestListImages(t *testing.T) {
	tmpDir := t.TempDir()
	dockerRoot := filepath.Join(tmpDir, "docker_root")
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	if err := os.Mkdir(dockerRoot, 0755); err != nil {
		t.Fatalf("failed to create docker root: %v", err)
	}
	if err := os.Mkdir(containerdRoot, 0755); err != nil {
		t.Fatalf("failed to create containerd root: %v", err)
	}

	exp, err := NewExplorer("", containerdRoot, dockerRoot)
	if err != nil {
		t.Fatalf("failed to create explorer: %v", err)
	}

	// Create repositories.json in docker_root/image/overlay2/
	overlay2Dir := filepath.Join(dockerRoot, "image", "overlay2")
	if err := os.MkdirAll(overlay2Dir, 0755); err != nil {
		t.Fatalf("failed to create overlay2 dir: %v", err)
	}

	repoData := ImageRepository{
		Repositories: map[string]ImageName{
			"ubuntu": {
				"ubuntu:latest": "sha256:abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234",
			},
		},
	}
	repoJSON, err := json.Marshal(repoData)
	if err != nil {
		t.Fatalf("failed to marshal repo data: %v", err)
	}
	if err := os.WriteFile(filepath.Join(overlay2Dir, "repositories.json"), repoJSON, 0600); err != nil {
		t.Fatalf("failed to write repositories.json: %v", err)
	}

	// Create image content file under imagedb/content/sha256/abcd1234...
	imageContentDir := filepath.Join(overlay2Dir, "imagedb", "content", "sha256")
	if err := os.MkdirAll(imageContentDir, 0755); err != nil {
		t.Fatalf("failed to create image content dir: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	contentSummary := imageContentSummary{
		Created: now,
		Os:      "linux",
	}
	contentJSON, err := json.Marshal(contentSummary)
	if err != nil {
		t.Fatalf("failed to marshal image content: %v", err)
	}

	imageIDFilename := "abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234"
	if err := os.WriteFile(filepath.Join(imageContentDir, imageIDFilename), contentJSON, 0600); err != nil {
		t.Fatalf("failed to write image content file: %v", err)
	}

	images, err := exp.ListImages(context.Background())
	if err != nil {
		t.Fatalf("ListImages failed: %v", err)
	}

	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}

	img := images[0]
	if img.Name != "ubuntu:latest" {
		t.Errorf("expected image name 'ubuntu:latest', got '%s'", img.Name)
	}
	if string(img.Target.Digest) != "sha256:abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234" {
		t.Errorf("expected digest 'sha256:abcd...', got '%s'", string(img.Target.Digest))
	}
	if !img.CreatedAt.Equal(now) {
		t.Errorf("expected CreatedAt %v, got %v", now, img.CreatedAt)
	}
}

func TestListNamespaces(t *testing.T) {
	tmpDir := t.TempDir()
	dockerRoot := filepath.Join(tmpDir, "docker_root")
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	if err := os.Mkdir(dockerRoot, 0755); err != nil {
		t.Fatalf("failed to create docker root: %v", err)
	}
	if err := os.Mkdir(containerdRoot, 0755); err != nil {
		t.Fatalf("failed to create containerd root: %v", err)
	}

	// Create meta.db in containerdRoot/io.containerd.metadata.v1.bolt/meta.db
	metaDir := filepath.Join(containerdRoot, "io.containerd.metadata.v1.bolt")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatalf("failed to create meta dir: %v", err)
	}

	dbPath := filepath.Join(metaDir, "meta.db")
	db, err := bolt.Open(dbPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to open bolt db: %v", err)
	}

	// Populate database with namespaces using containerd's metadata store
	err = db.Update(func(tx *bolt.Tx) error {
		store := metadata.NewNamespaceStore(tx)
		if err := store.Create(context.Background(), "ns1", nil); err != nil {
			return err
		}
		if err := store.Create(context.Background(), "ns2", nil); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		db.Close()
		t.Fatalf("failed to populate db: %v", err)
	}
	db.Close()

	exp, err := NewExplorer("", containerdRoot, dockerRoot)
	if err != nil {
		t.Fatalf("failed to create explorer: %v", err)
	}
	defer exp.Close()

	nss, err := exp.ListNamespaces(context.Background())
	if err != nil {
		t.Fatalf("ListNamespaces failed: %v", err)
	}

	if len(nss) != 2 {
		t.Errorf("expected 2 namespaces, got %d", len(nss))
	}

	found1, found2 := false, false
	for _, ns := range nss {
		if ns == "ns1" {
			found1 = true
		}
		if ns == "ns2" {
			found2 = true
		}
	}
	if !found1 || !found2 {
		t.Errorf("expected namespaces 'ns1' and 'ns2', got %v", nss)
	}
}

func TestGetContainerByID(t *testing.T) {
	tmpDir := t.TempDir()
	dockerRoot := filepath.Join(tmpDir, "docker_root")
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	if err := os.Mkdir(dockerRoot, 0755); err != nil {
		t.Fatalf("failed to create docker root: %v", err)
	}
	if err := os.Mkdir(containerdRoot, 0755); err != nil {
		t.Fatalf("failed to create containerd root: %v", err)
	}

	exp, err := NewExplorer("", containerdRoot, dockerRoot)
	if err != nil {
		t.Fatalf("failed to create explorer: %v", err)
	}

	cID := "test_container"
	containersDir := filepath.Join(dockerRoot, "containers")
	cDir := filepath.Join(containersDir, cID)
	if err := os.MkdirAll(cDir, 0755); err != nil {
		t.Fatalf("failed to create container dir: %v", err)
	}

	dockerConfig := ConfigFile{
		ID:     cID,
		Name:   "/test-container",
		Driver: "overlay2",
	}
	data, _ := json.Marshal(dockerConfig)
	if err := os.WriteFile(filepath.Join(cDir, "config.v2.json"), data, 0600); err != nil {
		t.Fatalf("failed to write config.v2.json: %v", err)
	}

	// Case 1: Success path
	ctr, err := exp.GetContainerByID(context.Background(), cID)
	if err != nil {
		t.Fatalf("GetContainerByID failed: %v", err)
	}
	if ctr.ID != cID {
		t.Errorf("expected container ID %s, got %s", cID, ctr.ID)
	}

	// Case 2: Container not found
	_, err = exp.GetContainerByID(context.Background(), "non_existent")
	if err == nil {
		t.Errorf("GetContainerByID expected error for non-existent container, got nil")
	}
}

func TestListImages_MissingRepositoriesDir(t *testing.T) {
	tmpDir := t.TempDir()
	dockerRoot := filepath.Join(tmpDir, "docker_root")
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	if err := os.Mkdir(dockerRoot, 0755); err != nil {
		t.Fatalf("failed to create docker root: %v", err)
	}
	if err := os.Mkdir(containerdRoot, 0755); err != nil {
		t.Fatalf("failed to create containerd root: %v", err)
	}

	exp, err := NewExplorer("", containerdRoot, dockerRoot)
	if err != nil {
		t.Fatalf("failed to create explorer: %v", err)
	}

	_, err = exp.ListImages(context.Background())
	if err == nil {
		t.Errorf("ListImages expected error for missing repositories directory, got nil")
	}
}

func TestListTasks_ErrorCases(t *testing.T) {
	tmpDir := t.TempDir()
	dockerRoot := filepath.Join(tmpDir, "docker_root")
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	if err := os.Mkdir(dockerRoot, 0755); err != nil {
		t.Fatalf("failed to create docker root: %v", err)
	}
	if err := os.Mkdir(containerdRoot, 0755); err != nil {
		t.Fatalf("failed to create containerd root: %v", err)
	}

	exp, err := NewExplorer("", containerdRoot, dockerRoot)
	if err != nil {
		t.Fatalf("failed to create explorer: %v", err)
	}

	containersDir := filepath.Join(dockerRoot, "containers")
	cDir := filepath.Join(containersDir, "c1")
	if err := os.MkdirAll(cDir, 0755); err != nil {
		t.Fatalf("failed to create container dir: %v", err)
	}

	// Case 1: missing config.v2.json
	_, err = exp.ListTasks(context.Background())
	if err == nil {
		t.Errorf("ListTasks expected error when config.v2.json is missing, got nil")
	}

	// Case 2: invalid JSON config.v2.json
	invalidJSON := []byte(`{invalid: json`)
	if err := os.WriteFile(filepath.Join(cDir, "config.v2.json"), invalidJSON, 0600); err != nil {
		t.Fatalf("failed to write invalid config: %v", err)
	}

	_, err = exp.ListTasks(context.Background())
	if err == nil {
		t.Errorf("ListTasks expected error when config.v2.json has invalid JSON, got nil")
	}
}
