/*
Copyright 2026 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
you may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package commands

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/metadata"
	"github.com/containerd/containerd/namespaces"
	"github.com/gogo/protobuf/types"
	"github.com/google/container-explorer/utils"
	oci "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/urfave/cli"
	bolt "go.etcd.io/bbolt"
)

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

func setupMockContainerd(t *testing.T, containerdRoot string, ns string, ctrID string) {
	metaDir := filepath.Join(containerdRoot, "io.containerd.metadata.v1.bolt")
	_ = os.MkdirAll(metaDir, 0755)
	dbPath := filepath.Join(metaDir, "meta.db")
	db, err := bolt.Open(dbPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to open bolt db: %v", err)
	}
	defer db.Close()

	err = db.Update(func(tx *bolt.Tx) error {
		nsStore := metadata.NewNamespaceStore(tx)
		return nsStore.Create(context.Background(), ns, nil)
	})
	if err != nil {
		t.Fatalf("failed to populate namespace: %v", err)
	}

	if ctrID != "" {
		dbStore := metadata.NewDB(db, nil, nil)
		cStore := metadata.NewContainerStore(dbStore)

		specObj := oci.Spec{
			Linux: &oci.Linux{
				CgroupsPath: "/default/" + ctrID,
			},
			Process: &oci.Process{
				Args: []string{"sleep", "10"},
			},
		}
		specJSON, _ := json.Marshal(specObj)
		anySpec := &types.Any{
			TypeUrl: "types.containerd.io/opencontainers/runtime-spec/1/Spec",
			Value:   specJSON,
		}

		c := containers.Container{
			ID:          ctrID,
			Image:       "ubuntu:latest",
			Snapshotter: "overlayfs",
			SnapshotKey: "snap-" + ctrID,
			Runtime: containers.RuntimeInfo{
				Name: "io.containerd.runc.v2",
			},
			Spec: anySpec,
		}
		ctx := namespaces.WithNamespace(context.Background(), ns)
		_, err = cStore.Create(ctx, c)
		if err != nil {
			t.Fatalf("failed to create container: %v", err)
		}
	}
}

func setupMockDocker(t *testing.T, dockerRoot string, containerID string) {
	containerDir := filepath.Join(dockerRoot, "containers", containerID)
	if err := os.MkdirAll(containerDir, 0755); err != nil {
		t.Fatalf("failed to create docker container dir: %v", err)
	}

	config := fmt.Sprintf(`{
		"ID": "%s",
		"Created": "2026-06-12T00:30:43Z",
		"Path": "sleep",
		"Args": ["10"],
		"State": {
			"Running": true,
			"Pid": 1234
		},
		"Config": {
			"Image": "ubuntu:latest",
			"Labels": {
				"app": "test-docker"
			}
		}
	}`, containerID)
	if err := os.WriteFile(filepath.Join(containerDir, "config.v2.json"), []byte(config), 0600); err != nil {
		t.Fatalf("failed to write config.v2.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(containerDir, "hostconfig.json"), []byte(`{}`), 0600); err != nil {
		t.Fatalf("failed to write hostconfig.json: %v", err)
	}
}

func createMetaSnapshot(tx *bolt.Tx, ns, snapshotter, key, name, parent string, created time.Time) error {
	v1Bkt, err := tx.CreateBucketIfNotExists([]byte("v1"))
	if err != nil {
		return err
	}
	nsBkt, err := v1Bkt.CreateBucketIfNotExists([]byte(ns))
	if err != nil {
		return err
	}
	snapshotsBkt, err := nsBkt.CreateBucketIfNotExists([]byte("snapshots"))
	if err != nil {
		return err
	}
	sterBkt, err := snapshotsBkt.CreateBucketIfNotExists([]byte(snapshotter))
	if err != nil {
		return err
	}
	keyBkt, err := sterBkt.CreateBucketIfNotExists([]byte(key))
	if err != nil {
		return err
	}

	_ = keyBkt.Put([]byte("name"), []byte(name))
	_ = keyBkt.Put([]byte("parent"), []byte(parent))
	tBytes, _ := created.MarshalBinary()
	_ = keyBkt.Put([]byte("createdat"), tBytes)
	return nil
}

func createOverlaySnapshot(tx *bolt.Tx, key string, id uint64, kind byte, parent string, size uint64, created time.Time) error {
	v1Bkt, err := tx.CreateBucketIfNotExists([]byte("v1"))
	if err != nil {
		return err
	}
	snapsBucket, err := v1Bkt.CreateBucketIfNotExists([]byte("snapshots"))
	if err != nil {
		return err
	}
	keyBucket, err := snapsBucket.CreateBucketIfNotExists([]byte(key))
	if err != nil {
		return err
	}

	idBuf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(idBuf, id)
	_ = keyBucket.Put([]byte("id"), idBuf[:n])

	kindBuf := make([]byte, binary.MaxVarintLen64)
	n = binary.PutUvarint(kindBuf, uint64(kind))
	_ = keyBucket.Put([]byte("kind"), kindBuf[:n])

	_ = keyBucket.Put([]byte("parent"), []byte(parent))

	sizeBuf := make([]byte, binary.MaxVarintLen64)
	n = binary.PutUvarint(sizeBuf, size)
	_ = keyBucket.Put([]byte("size"), sizeBuf[:n])

	tBytes, _ := created.MarshalBinary()
	_ = keyBucket.Put([]byte("createdat"), tBytes)
	return nil
}

func runApp(args []string) (string, error) {
	app := cli.NewApp()
	app.Flags = []cli.Flag{
		cli.StringFlag{Name: "containerd-root, c"},
		cli.StringFlag{Name: "image-root, i"},
		cli.StringFlag{Name: "docker-root, D"},
		cli.StringFlag{Name: "output"},
	}
	app.Commands = []cli.Command{
		ListCommand,
		InfoCommand,
		InspectCommand,
		MountCommand,
		DriftCommand,
		ExportCommand,
	}
	app.Before = func(clictx *cli.Context) error {
		return InitializeRuntime(clictx)
	}

	// Capture output
	var stdoutBuf bytes.Buffer
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := app.Run(args)

	w.Close()
	os.Stdout = oldStdout
	_, _ = io.Copy(&stdoutBuf, r)

	return stdoutBuf.String(), err
}

func TestCLI_ListNamespaces(t *testing.T) {
	tmpDir := t.TempDir()
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	setupMockContainerd(t, containerdRoot, "ns-test-1", "")

	args := []string{"container-explorer", "--containerd-root", containerdRoot, "list", "namespaces"}
	output, err := runApp(args)
	if err != nil {
		t.Fatalf("runApp failed: %v", err)
	}

	if !strings.Contains(output, "ns-test-1") {
		t.Errorf("expected output to contain 'ns-test-1', got:\n%s", output)
	}
}

func TestCLI_ListContainers(t *testing.T) {
	tmpDir := t.TempDir()
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	setupMockContainerd(t, containerdRoot, "ns-test-2", "container-cli-1")

	dockerRoot := filepath.Join(tmpDir, "docker_root")
	setupMockDocker(t, dockerRoot, "container-docker-1")

	args := []string{"container-explorer", "--containerd-root", containerdRoot, "--docker-root", dockerRoot, "list", "containers"}
	output, err := runApp(args)
	if err != nil {
		t.Fatalf("runApp failed: %v", err)
	}

	if !strings.Contains(output, "container-cli-1") {
		t.Errorf("expected output to contain 'container-cli-1', got:\n%s", output)
	}
	if !strings.Contains(output, "container-docker-1") {
		t.Errorf("expected output to contain 'container-docker-1', got:\n%s", output)
	}
}

func TestCLI_InfoContainer(t *testing.T) {
	tmpDir := t.TempDir()
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	setupMockContainerd(t, containerdRoot, "ns-test-3", "container-cli-2")

	args := []string{"container-explorer", "--containerd-root", containerdRoot, "info", "container", "container-cli-2"}
	output, err := runApp(args)
	if err != nil {
		t.Fatalf("runApp failed: %v", err)
	}

	if !strings.Contains(output, "container-cli-2") {
		t.Errorf("expected output to contain 'container-cli-2', got:\n%s", output)
	}
}

func TestCLI_InspectContainer(t *testing.T) {
	tmpDir := t.TempDir()
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	setupMockContainerd(t, containerdRoot, "ns-test-3", "container-cli-2")

	args := []string{"container-explorer", "--containerd-root", containerdRoot, "inspect", "container-cli-2"}
	output, err := runApp(args)
	if err != nil {
		t.Fatalf("runApp failed: %v", err)
	}

	if !strings.Contains(output, "container-cli-2") {
		t.Errorf("expected output to contain 'container-cli-2', got:\n%s", output)
	}
}

func TestCLI_Drift(t *testing.T) {
	tmpDir := t.TempDir()
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	setupMockContainerd(t, containerdRoot, "ns-test-4", "container-cli-3")

	// Setup databases and mock directory structure on disk for containerd drift
	metaDir := filepath.Join(containerdRoot, "io.containerd.metadata.v1.bolt")
	dbPath := filepath.Join(metaDir, "meta.db")
	db, err := bolt.Open(dbPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to open meta.db: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	_ = db.Update(func(tx *bolt.Tx) error {
		_ = createMetaSnapshot(tx, "ns-test-4", "overlayfs", "snap-container-cli-3", "snapshot-name-1", "snapshot-name-parent", now)
		return createMetaSnapshot(tx, "ns-test-4", "overlayfs", "snapshot-name-parent", "snapshot-name-parent", "", now)
	})
	db.Close()

	snapshotterDir := filepath.Join(containerdRoot, "io.containerd.snapshotter.v1.overlayfs")
	_ = os.MkdirAll(snapshotterDir, 0755)
	ssDBPath := filepath.Join(snapshotterDir, "metadata.db")
	ssDB, err := bolt.Open(ssDBPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to open snapshotter metadata.db: %v", err)
	}
	_ = ssDB.Update(func(tx *bolt.Tx) error {
		_ = createOverlaySnapshot(tx, "snapshot-name-1", 42, 2, "snapshot-name-parent", 10240, now)
		return createOverlaySnapshot(tx, "snapshot-name-parent", 41, 2, "", 10240, now)
	})
	ssDB.Close()

	upperDir := filepath.Join(snapshotterDir, "snapshots", "42", "fs")
	_ = os.MkdirAll(upperDir, 0755)
	_ = os.MkdirAll(filepath.Join(snapshotterDir, "snapshots", "42", "work"), 0755)
	driftFile := filepath.Join(upperDir, "etc", "test-cli.conf")
	_ = os.MkdirAll(filepath.Dir(driftFile), 0755)
	_ = os.WriteFile(driftFile, []byte("some config change"), 0600)

	args := []string{"container-explorer", "--containerd-root", containerdRoot, "drift", "container-cli-3"}
	output, err := runApp(args)
	if err != nil {
		t.Fatalf("runApp failed: %v", err)
	}

	if !strings.Contains(output, "test-cli.conf") {
		t.Errorf("expected output to contain 'test-cli.conf', got:\n%s", output)
	}
}

func TestCLI_Export(t *testing.T) {
	tmpDir := t.TempDir()
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	setupMockContainerd(t, containerdRoot, "ns-test-5", "container-cli-4")

	metaDir := filepath.Join(containerdRoot, "io.containerd.metadata.v1.bolt")
	dbPath := filepath.Join(metaDir, "meta.db")
	db, err := bolt.Open(dbPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to open meta.db: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	_ = db.Update(func(tx *bolt.Tx) error {
		_ = createMetaSnapshot(tx, "ns-test-5", "overlayfs", "snap-container-cli-4", "snapshot-name-1", "snapshot-name-parent", now)
		return createMetaSnapshot(tx, "ns-test-5", "overlayfs", "snapshot-name-parent", "snapshot-name-parent", "", now)
	})
	db.Close()

	snapshotterDir := filepath.Join(containerdRoot, "io.containerd.snapshotter.v1.overlayfs")
	_ = os.MkdirAll(snapshotterDir, 0755)
	ssDBPath := filepath.Join(snapshotterDir, "metadata.db")
	ssDB, err := bolt.Open(ssDBPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to open snapshotter metadata.db: %v", err)
	}
	_ = ssDB.Update(func(tx *bolt.Tx) error {
		_ = createOverlaySnapshot(tx, "snapshot-name-1", 42, 2, "snapshot-name-parent", 10240, now)
		return createOverlaySnapshot(tx, "snapshot-name-parent", 41, 2, "", 10240, now)
	})
	ssDB.Close()

	_ = os.MkdirAll(filepath.Join(snapshotterDir, "snapshots", "42", "work"), 0755)
	_ = os.MkdirAll(filepath.Join(snapshotterDir, "snapshots", "42", "fs"), 0755)
	_ = os.MkdirAll(filepath.Join(snapshotterDir, "snapshots", "41", "fs"), 0755)

	// Set up mock runner
	origRunner := utils.Runner
	mockRunner := &mockCommandRunner{
		Responses: map[string]mockCommandResponse{
			"losetup": {Stdout: "/dev/loop123\n", Err: nil},
		},
	}
	utils.Runner = mockRunner
	defer func() { utils.Runner = origRunner }()

	outputDir := filepath.Join(tmpDir, "output")
	args := []string{"container-explorer", "--containerd-root", containerdRoot, "export", "--archive", "container-cli-4", outputDir}
	_, err = runApp(args)
	if err != nil {
		t.Fatalf("runApp failed: %v", err)
	}

	hasMount := false
	hasUmount := false
	hasTar := false
	for _, c := range mockRunner.Calls {
		if c.Name == "mount" {
			hasMount = true
		}
		if c.Name == "umount" {
			hasUmount = true
		}
		if c.Name == "tar" {
			hasTar = true
		}
	}

	if !hasMount {
		t.Errorf("expected 'mount' command to be executed")
	}
	if !hasUmount {
		t.Errorf("expected 'umount' command to be executed")
	}
	if !hasTar {
		t.Errorf("expected 'tar' command to be executed")
	}
}

func TestGetDockerDataRoot(t *testing.T) {
	// Case 1: Config does not exist -> default
	tmpDir := t.TempDir()
	path := getDockerDataRoot(tmpDir)
	if path != defaultDockerRootDir {
		t.Errorf("expected default docker data root %q, got %q", defaultDockerRootDir, path)
	}

	// Case 2: Config exists but is invalid JSON
	dockerConfigDir := filepath.Join(tmpDir, "etc", "docker")
	_ = os.MkdirAll(dockerConfigDir, 0755)
	_ = os.WriteFile(filepath.Join(dockerConfigDir, "daemon.json"), []byte("{invalid-json}"), 0600)
	path = getDockerDataRoot(tmpDir)
	if path != defaultDockerRootDir {
		t.Errorf("expected default docker data root on invalid JSON, got %q", path)
	}

	// Case 3: Config exists, valid JSON, but missing data-root
	_ = os.WriteFile(filepath.Join(dockerConfigDir, "daemon.json"), []byte(`{"debug": true}`), 0600)
	path = getDockerDataRoot(tmpDir)
	if path != defaultDockerRootDir {
		t.Errorf("expected default docker data root on missing data-root, got %q", path)
	}

	// Case 4: Config exists, valid JSON, custom data-root
	_ = os.WriteFile(filepath.Join(dockerConfigDir, "daemon.json"), []byte(`{"data-root": "/custom/docker/root"}`), 0600)
	path = getDockerDataRoot(tmpDir)
	if path != "/custom/docker/root" {
		t.Errorf("expected custom docker data root '/custom/docker/root', got %q", path)
	}
}

func TestGetContainerdDataDir(t *testing.T) {
	// Case 1: Config does not exist -> default
	tmpDir := t.TempDir()
	path := getContainerdDataDir(tmpDir)
	if path != defaultContainerdRootDir {
		t.Errorf("expected default containerd root %q, got %q", defaultContainerdRootDir, path)
	}

	// Case 2: Config exists but parsing fails (invalid TOML)
	containerdConfigDir := filepath.Join(tmpDir, "etc", "containerd")
	_ = os.MkdirAll(containerdConfigDir, 0755)
	_ = os.WriteFile(filepath.Join(containerdConfigDir, "config.toml"), []byte("invalid-toml"), 0600)
	path = getContainerdDataDir(tmpDir)
	if path != defaultContainerdRootDir {
		t.Errorf("expected default containerd root on invalid TOML, got %q", path)
	}

	// Case 3: Config exists, valid TOML, but missing root
	_ = os.WriteFile(filepath.Join(containerdConfigDir, "config.toml"), []byte(`version = 2`), 0600)
	path = getContainerdDataDir(tmpDir)
	if path != defaultContainerdRootDir {
		t.Errorf("expected default containerd root on missing root key, got %q", path)
	}

	// Case 4: Config exists, valid TOML, custom root
	_ = os.WriteFile(filepath.Join(containerdConfigDir, "config.toml"), []byte(`root = "/custom/containerd/root"`), 0600)
	path = getContainerdDataDir(tmpDir)
	if path != "/custom/containerd/root" {
		t.Errorf("expected custom containerd root '/custom/containerd/root', got %q", path)
	}
}

func TestGetFilterMap(t *testing.T) {
	// Case 1: Empty filter
	m := getFilterMap("")
	if m != nil {
		t.Errorf("expected nil filter map for empty string, got %v", m)
	}

	// Case 2: Valid single pair
	m = getFilterMap("key=val")
	if len(m) != 1 || m["key"] != "val" {
		t.Errorf("expected {'key': 'val'}, got %v", m)
	}

	// Case 3: Valid multiple pairs with spaces
	m = getFilterMap(" key1 = val1 , key2=val2 ")
	if len(m) != 2 || m["key1"] != "val1" || m["key2"] != "val2" {
		t.Errorf("expected {'key1': 'val1', 'key2': 'val2'}, got %v", m)
	}

	// Case 4: Malformed filters (ignored)
	m = getFilterMap("key1,key2=val2,key3=")
	if len(m) != 2 || m["key2"] != "val2" || m["key3"] != "" {
		t.Errorf("expected {'key2': 'val2', 'key3': ''}, got %v", m)
	}
}
