/*
Copyright 2021 Google LLC

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

// Package containerd implements the ContainerExplorer interface for exploring containerd managed containers.
package containerd

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/metadata"
	"github.com/containerd/containerd/namespaces"

	"github.com/google/container-explorer/explorers"
	"github.com/google/container-explorer/utils"

	spec "github.com/opencontainers/runtime-spec/specs-go"

	log "github.com/sirupsen/logrus"
	bolt "go.etcd.io/bbolt"
)

type explorer struct {
	imageRoot      string // mounted image path
	containerdRoot string
	dockerRoot     string
	manifestFile   string // path to manifest database file i.e. meta.db
	snapshotFile   string
	layercache     string                      // layer cache folder within snapshot root
	mdb            *bolt.DB                    // manifest database
	sc             *explorers.SupportContainer // support container structure object
}

// NewExplorer returns a ContainerExplorer interface to explore containerd.
func NewExplorer(imageRoot string, containerdRoot string, dockerRoot string, layercache string, sc *explorers.SupportContainer) (explorers.ContainerExplorer, error) {
	opt := &bolt.Options{
		ReadOnly: true,
	}

	exists, err := utils.PathExists(containerdRoot)
	if err != nil {
		return nil, fmt.Errorf("checking containerd root directory: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("contained root directory does not exist")
	}

	manifestFile := filepath.Join(containerdRoot, "io.containerd.metadata.v1.bolt", "meta.db")
	exists, err = utils.PathExists(manifestFile)
	if err != nil {
		return nil, fmt.Errorf("checking containerd manifest file: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("containerd manifest file meta.db does not exist")
	}

	db, err := bolt.Open(manifestFile, 0444, opt)
	if err != nil {
		return &explorer{}, err
	}

	return &explorer{
		imageRoot:      imageRoot,
		containerdRoot: containerdRoot,
		dockerRoot:     dockerRoot,
		manifestFile:   manifestFile,
		layercache:     layercache,
		mdb:            db,
		sc:             sc,
	}, nil
}

// SnapshotRoot returns the root directory containing snapshot information.
//
// Containerd requires snapshot database metadata.db which is stored within
// the snapshot root directory.
//
// The default snapshot root directory location for containerd is
// /var/lib/containerd/io.containerd.snapshotter.v1.overlayfs
func (e *explorer) SnapshotRoot(snapshotter string) string {
	snapshotRoot := "unknown"
	if snapshotter == "" {
		return snapshotRoot
	}

	dirs, _ := filepath.Glob(filepath.Join(e.containerdRoot, "*"))
	for _, dir := range dirs {
		if strings.Contains(strings.ToLower(dir), strings.ToLower(snapshotter)) {
			filepath.WalkDir(dir, func(path string, _ fs.DirEntry, _ error) error {
				if strings.Contains(path, "metadata.db") {
					snapshotRoot, _ = filepath.Split(path)
					log.WithFields(log.Fields{
						"path":         path,
						"snapshotRoot": snapshotRoot,
					}).Debug("snapshot root")
					return fs.SkipAll
				}
				return nil
			})
			return snapshotRoot
		}
	}
	return snapshotRoot
}

// ListNamespace returns namespaces.
//
// In containerd the namespace information is stored in metadata file meta.db.
func (e *explorer) ListNamespaces(ctx context.Context) ([]string, error) {
	var nss []string

	err := e.mdb.View(func(tx *bolt.Tx) error {
		store := metadata.NewNamespaceStore(tx)
		results, err := store.List(ctx)
		if err != nil {
			return err
		}
		nss = results
		return nil
	})
	if err != nil {
		return nil, err
	}

	return nss, nil
}

// ListContainers returns the information about containers.
//
// In containerd the container information is stored in metadata file meta.db.
func (e *explorer) ListContainers(ctx context.Context) ([]explorers.Container, error) {
	var ceContainers []explorers.Container

	nss, err := e.ListNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	store := metadata.NewContainerStore(metadata.NewDB(e.mdb, nil, nil))

	for _, ns := range nss {
		ctx = namespaces.WithNamespace(ctx, ns)

		results, err := store.List(ctx)
		if err != nil {
			return nil, err
		}

		for _, result := range results {
			ceCtr := convertToContainerExplorerContainer(ns, result)
			ceCtr.ImageBase = imageBasename(ceCtr.Image)
			ceCtr.SupportContainer = e.sc.IsSupportContainer(ceCtr)
			ceTask, err := e.GetContainerTask(ctx, ceCtr)
			if err != nil {
				log.WithField("containerID", ceCtr.ID).Error("failed getting container task")
			}
			ceCtr.ProcessID = ceTask.PID
			ceCtr.ContainerType = ceTask.ContainerType
			ceCtr.Status = ceTask.Status

			ceContainers = append(ceContainers, ceCtr)
		}
	}
	return ceContainers, nil
}

// ListImages returns the information about content.
//
// In containerd, the image information is stored in metadata file meta.db.
func (e *explorer) ListImages(ctx context.Context) ([]explorers.Image, error) {
	var ceImages []explorers.Image

	nss, err := e.ListNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	store := metadata.NewImageStore(metadata.NewDB(e.mdb, nil, nil))

	for _, ns := range nss {
		ctx = namespaces.WithNamespace(ctx, ns)

		results, err := store.List(ctx)
		if err != nil {
			return nil, err
		}

		for _, result := range results {
			ceImages = append(ceImages, explorers.Image{
				Namespace:             ns,
				ContainerType:         "containerd",
				SupportContainerImage: e.sc.SupportContainerImage(imageBasename(result.Name)),
				Image:                 result,
			})
		}
	}
	return ceImages, nil
}

// ListContent returns the information about content.
//
// In containerd, the content information is stored in metadata file meta.db.
func (e *explorer) ListContent(ctx context.Context) ([]explorers.Content, error) {
	var ceContent []explorers.Content

	nss, err := e.ListNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	store := newBlobStore(e.mdb)

	for _, ns := range nss {
		ctx = namespaces.WithNamespace(ctx, ns)

		results, err := store.List(ctx)
		if err != nil {
			return nil, err
		}

		for _, result := range results {
			ceContent = append(ceContent, explorers.Content{
				Namespace: ns,
				Info:      result,
			})
		}
	}

	return ceContent, nil
}

// ListSnapshots returns the snapshot information.
//
// In containerd, the snapshot information is stored in two different files:
// - metadata file (meta.db)
// - snapshot file (metadata.db)
//
// These files contain some overlapping fields.
//
// The metadata file meta.db contains snapshot information and container
// references the the snapshot information.
//
// The snapshot file metadata.db contains information about the snapshots only
// without reference to a container. This file also containers informations
// that are more relevant to manage snapshots.
//
// For Examples:
//   - Snapshot type i.e. active or committed
//   - Snapshot ID that refers to overlay path i.e /var/lib/containerd/io.containerd.snapshotter.v1.overlayfs/snapshots/<id>/fs
//
// Snapshot ID is required when mounting the container.
func (e *explorer) ListSnapshots(ctx context.Context) ([]explorers.SnapshotKeyInfo, error) {
	var ceSnapshots []explorers.SnapshotKeyInfo

	nss, err := e.ListNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	// snapshot database
	opts := bolt.Options{
		ReadOnly: true,
	}

	if e.snapshotFile == "" {
		snapshotterFolder := e.SnapshotRoot("overlayfs")
		if snapshotterFolder != "unknown" {
			e.snapshotFile = filepath.Join(snapshotterFolder, "metadata.db")
		}
	}

	var ssdb *bolt.DB
	if e.snapshotFile != "" {
		ssdb, err = bolt.Open(e.snapshotFile, 0444, &opts)
		if err != nil {
			log.WithFields(log.Fields{
				"snapshotFile": e.snapshotFile,
			}).Error(err)
		}
	}

	store := newSnapshotStore(e.containerdRoot, e.layercache, e.mdb, ssdb)

	for _, ns := range nss {
		ctx = namespaces.WithNamespace(ctx, ns)

		results, err := store.List(ctx)
		if err != nil {
			return nil, err
		}

		ceSnapshots = append(ceSnapshots, results...)
	}

	return ceSnapshots, nil
}

// ListTasks returns container tasks status
func (e *explorer) ListTasks(ctx context.Context) ([]explorers.Task, error) {
	if e.imageRoot == "" {
		log.Error("image-root is empty: unable to list tasks")
		return nil, nil
	}

	// Holds container task information.
	var ceTasks []explorers.Task

	ctrs, err := e.ListContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}
	for _, ctr := range ctrs {
		ceTask, err := e.GetContainerTask(ctx, ctr)
		if err != nil {
			return nil, fmt.Errorf("failed getting a container's task: %w", err)
		}

		ceTasks = append(ceTasks, ceTask)
	}

	return ceTasks, nil
}

// GetContainerTask returns container task
func (e *explorer) GetContainerTask(ctx context.Context, ctr explorers.Container) (explorers.Task, error) {
	ctx = namespaces.WithNamespace(ctx, ctr.Namespace)

	// Only return container spec
	v, err := e.InfoContainer(ctx, ctr.ID, true)
	if err != nil {
		return explorers.Task{}, fmt.Errorf("failed getting container spec for %s container: %w", ctr.ID, err)
	}
	ctrSpec := v.(spec.Spec)

	var cgroupsPath string
	var containerType string

	// Compute cgroup path for docker and containerd containers
	if strings.Contains(ctrSpec.Linux.CgroupsPath, "docker") {
		containerType = "docker"

		// compute for docker
		//
		// Spec file `config.json` contains key cgroupsPath as `system.slice:docker:<container_id>`.
		// The path maps on file system to `/sys/fs/cgroup/system.slice/docker-<container_id>.scope`.
		m := strings.Split(ctrSpec.Linux.CgroupsPath, ":")
		if len(m) != 3 {
			return explorers.Task{}, fmt.Errorf("expecting pattern system.slice:docker:<container_id> and got %d fields", len(m))
		}

		// docker cgroup directory i.e. system.slice
		cgroupNS := m[0]
		// container cgroup information
		cgroupCtrDir := fmt.Sprintf("%s-%s.scope", m[1], m[2])
		// abolute path to container cgroup directory
		cgroupsPath = filepath.Join(e.imageRoot, "sys", "fs", "cgroup", cgroupNS, cgroupCtrDir)
	} else {
		containerType = "containerd"

		// compute for containerd
		//
		// Spec file contains "cgroupsPath": "/default/<container_id>",
		cgroupsPath = filepath.Join(e.imageRoot, "sys", "fs", "cgroup", ctrSpec.Linux.CgroupsPath)
	}

	// Verify the path actually exist on the system.
	// If a container is deleted then cgroup may not exist for the container
	if !explorers.PathExists(cgroupsPath, false) {
		log.WithFields(log.Fields{
			"containerID": ctr.ID,
			"cgroupsPath": cgroupsPath,
		}).Debug("container cgroup path does not exit")

		return explorers.Task{
			Namespace:     ctr.Namespace,
			Name:          ctr.ID,
			ContainerType: containerType,
			Status:        "UNKNOWN",
		}, nil
	}

	status, err := explorers.GetTaskStatus(cgroupsPath)
	if err != nil {
		// Only print the error message.
		// The default return should contain status UNKNOWN
		log.WithField("containerID", ctr.ID).Errorf("failed getting container status for container: %v", err)
	}

	// Get container process ID
	ctrPID := explorers.GetTaskPID(cgroupsPath)
	if ctrPID == -1 && containerType == "containerd" {
		state, err := e.GetContainerState(ctx, ctr)
		if err != nil {
			log.WithField("containerID", ctr.ID).Error("failed getting container state")
		}
		if state.InitProcessPid != 0 {
			ctrPID = state.InitProcessPid
		}
	}

	return explorers.Task{
		Namespace:     ctr.Namespace,
		Name:          ctr.ID,
		PID:           ctrPID,
		ContainerType: containerType,
		Status:        status,
	}, nil
}

// GetContainerState returns container runtime state
func (e *explorer) GetContainerState(_ context.Context, ctr explorers.Container) (explorers.State, error) {
	stateDir := filepath.Join(e.imageRoot, "run", "containerd", "runc", ctr.Namespace, ctr.ID)
	if !explorers.PathExists(stateDir, false) {
		return explorers.State{}, fmt.Errorf("container state directory %s did not exist", stateDir)
	}

	stateFile := filepath.Join(stateDir, "state.json")
	if !explorers.PathExists(stateFile, true) {
		return explorers.State{}, fmt.Errorf("container state file %s did not exist", stateFile)
	}

	data, err := os.ReadFile(stateFile)
	if err != nil {
		return explorers.State{}, err
	}

	var state explorers.State
	if err := json.Unmarshal(data, &state); err != nil {
		return explorers.State{}, fmt.Errorf("unmarshalling state data: %w", err)
	}
	return state, nil
}

// getContainerStoreInfo finds the container across all namespaces and returns it and its namespace
func (e *explorer) getContainerStoreInfo(ctx context.Context, containerID string) (containers.Container, string, error) {
	nss, err := e.ListNamespaces(ctx)
	if err != nil {
		return containers.Container{}, "", err
	}

	store := metadata.NewContainerStore(metadata.NewDB(e.mdb, nil, nil))

	for _, ns := range nss {
		nsCtx := namespaces.WithNamespace(ctx, ns)
		container, err := store.Get(nsCtx, containerID)
		if err == nil {
			return container, ns, nil
		}
	}

	return containers.Container{}, "", fmt.Errorf("no matching container")
}

// InfoContainer returns container internal information.
func (e *explorer) InfoContainer(ctx context.Context, containerID string, spec bool) (any, error) {
	container, _, err := e.getContainerStoreInfo(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("getting container %s: %w", containerID, err)
	}

	if container.Spec != nil && container.Spec.GetValue() != nil {
		v, err := parseSpec(container.Spec.GetValue())
		if err != nil {
			return nil, err
		}

		// Only return spec
		if spec {
			return v, nil
		}

		// Return container and spec info
		return struct {
			containers.Container
			Spec any `json:"Spec,omitempty"`
		}{
			Container: container,
			Spec:      v,
		}, nil
	}

	// default return
	return nil, nil
}

// resolveSnapshotter checks the snapshotter in meta.db.
// If not found, falls back to "overlayfs".
func (e *explorer) resolveSnapshotter(ctx context.Context, container *containers.Container) error {
	if container.Snapshotter != "" && container.SnapshotKey != "" {
		return nil
	}

	namespace, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		if container.Snapshotter == "" {
			container.Snapshotter = "overlayfs" // default fallback
		}
		if container.SnapshotKey == "" {
			container.SnapshotKey = container.ID
		}
		return fmt.Errorf("failed to get namespace from context when resolving snapshotter: %w", err)
	}

	var foundSnapshotter string
	var foundSnapshotKey string

	dbErr := e.mdb.View(func(tx *bolt.Tx) error {
		bkt := getSnapshottersBucket(tx, namespace)
		if bkt == nil {
			return nil
		}

		searchSnapshotter := func(snapshotterName string) error {
			ssbkt := bkt.Bucket([]byte(snapshotterName))
			if ssbkt == nil {
				return nil
			}

			if container.SnapshotKey != "" {
				if skBkt := ssbkt.Bucket([]byte(container.SnapshotKey)); skBkt != nil {
					foundSnapshotter = snapshotterName
					foundSnapshotKey = container.SnapshotKey
					return fmt.Errorf("found")
				}
			} else {
				// Try container.ID as the snapshot key
				if skBkt := ssbkt.Bucket([]byte(container.ID)); skBkt != nil {
					foundSnapshotter = snapshotterName
					foundSnapshotKey = container.ID
					return fmt.Errorf("found")
				}

				// Search for a snapshot key containing container.ID
				var matchedKey string
				_ = ssbkt.ForEach(func(k, _ []byte) error {
					keyStr := string(k)
					if strings.Contains(keyStr, container.ID) {
						matchedKey = keyStr
						return fmt.Errorf("found_match")
					}
					return nil
				})
				if matchedKey != "" {
					foundSnapshotter = snapshotterName
					foundSnapshotKey = matchedKey
					return fmt.Errorf("found")
				}
			}
			return nil
		}

		if container.Snapshotter != "" {
			return searchSnapshotter(container.Snapshotter)
		}

		// If container.Snapshotter is empty, search all snapshotters
		_ = bkt.ForEach(func(k, _ []byte) error {
			if err := searchSnapshotter(string(k)); err != nil {
				return err
			}
			return nil
		})
		return nil
	})

	if dbErr != nil && dbErr.Error() != "found" {
		return fmt.Errorf("failed to view database: %w", dbErr)
	}

	if foundSnapshotter != "" {
		container.Snapshotter = foundSnapshotter
		container.SnapshotKey = foundSnapshotKey
		log.Infof("resolved empty snapshotter/key for container %s to snapshotter=%s, key=%s", container.ID, foundSnapshotter, foundSnapshotKey)
	} else {
		if container.Snapshotter == "" {
			container.Snapshotter = "overlayfs" // default fallback
		}
		if container.SnapshotKey == "" {
			container.SnapshotKey = container.ID // default fallback to container ID
		}
		log.Warnf("could not resolve snapshotter/key in meta.db for container %s, falling back to snapshotter=%s, key=%s", container.ID, container.Snapshotter, container.SnapshotKey)
	}
	return nil
}

// MountContainer mounts a container to the specified path
func (e *explorer) MountContainer(ctx context.Context, containerID string, mountpoint string) error {
	container, ns, err := e.getContainerStoreInfo(ctx, containerID)
	if err != nil {
		return fmt.Errorf("failed getting container information %v", err)
	}

	ctx = namespaces.WithNamespace(ctx, ns)

	if err = e.resolveSnapshotter(ctx, &container); err != nil {
		return fmt.Errorf("failed resolving snapshotter: %w", err)
	}

	// Snapshot database metadata.db access
	opts := bolt.Options{
		ReadOnly: true,
	}

	if e.snapshotFile == "" {
		snapshotterFolder := e.SnapshotRoot(container.Snapshotter)
		if snapshotterFolder != "unknown" {
			e.snapshotFile = filepath.Join(snapshotterFolder, "metadata.db")
		}
	}

	log.WithFields(log.Fields{
		"containerID":     containerID,
		"namespace":       ns,
		"snapshotter":     container.Snapshotter,
		"snapshotKey":     container.SnapshotKey,
		"image":           container.Image,
		"snapshotterFile": e.snapshotFile,
	}).Debug("containerd container snapshotter")

	ssDB, err := bolt.Open(e.snapshotFile, 0444, &opts)
	if err != nil {
		return fmt.Errorf("failed opening %s snapshot database %v", container.Snapshotter, err)
	}

	// snapshot store
	ssStore := newSnapshotStore(e.containerdRoot, e.layercache, e.mdb, ssDB)
	var mountArgs []string
	hasWorkDir := false
	snapshotRoot, _ := filepath.Split(e.snapshotFile)
	matches, _ := filepath.Glob(filepath.Join(snapshotRoot, "snapshots/*/work"))
	if len(matches) > 0 {
		hasWorkDir = true
	}
	if container.Snapshotter == "native" {
		upperdir, err := ssStore.NativePath(ctx, container)
		log.WithFields(log.Fields{
			"upperdir": upperdir,
		}).Debug("native directories")
		if err != nil {
			return fmt.Errorf("failed to get native path %v", err)
		}
		mountArgs = []string{"-t", "bind", upperdir, mountpoint, "-o", "rbind,ro"}
	} else if hasWorkDir {
		lowerdir, upperdir, workdir, err := ssStore.OverlayPath(ctx, container)
		log.WithFields(log.Fields{
			"lowerdir": lowerdir,
			"upperdir": upperdir,
			"workdir":  workdir,
		}).Debug("overlay directories")
		if err != nil {
			return fmt.Errorf("failed to get overlay path %v", err)
		}

		if lowerdir == "" {
			return fmt.Errorf("lowerdir is empty")
		}

		// TODO(rmaskey): Use github.com/containerd/containerd/mount.Mount to mount
		// a container
		mountopts := fmt.Sprintf("ro,lowerdir=%s:%s", upperdir, lowerdir)
		mountArgs = []string{"-t", "overlay", "overlay", "-o", mountopts, mountpoint}
	} else {
		log.Errorf("unsupported snapshotter: %s", container.Snapshotter)
	}

	log.Debug("container mount command ", mountArgs)

	cmd := exec.Command("mount", mountArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Errorf("running mount command: %v", err)

		log.Error("invalid overlayfs lowerdir path: use --debug to view lowerdir path")

		return err
	}

	if string(out) != "" {
		log.Infof("mount command output: %s", string(out))
	}

	// default
	return nil
}

// MountAllContainers mounts all the containers
func (e *explorer) MountAllContainers(ctx context.Context, mountpoint string, filter string, skipsupportcontainers bool) error {
	ctrs, err := e.ListContainers(ctx)
	if err != nil {
		return err
	}

	filters := strings.Split(filter, ",")

	for _, ctr := range ctrs {
		// Skip Docker-managed containers to avoid double mounting (as they will be mounted by the Docker explorer)
		if ctr.Namespace == "moby" {
			continue
		}

		// Skip Kubernetes suppot containers
		if skipsupportcontainers && ctr.SupportContainer {
			log.WithFields(log.Fields{
				"namespace":   ctr.Namespace,
				"containerID": ctr.ID,
			}).Info("skipping Kubernetes containers")

			continue
		}

		// Only mount containers matching the filter.
		mount := true
		for _, f := range filters {
			if !strings.Contains(f, "=") {
				continue
			}

			key := strings.Split(f, "=")[0]
			value := strings.Split(f, "=")[1]

			labelValue, ok := ctr.Labels[key]
			if !ok {
				mount = false
				break
			}

			if labelValue != value {
				mount = false
				break
			}
		}

		if !mount {
			continue
		}

		// Create a subdirectory within the specified mountpoint
		ctrmountpoint := filepath.Join(mountpoint, ctr.ID)
		if err := os.MkdirAll(ctrmountpoint, 0755); err != nil {
			log.WithFields(log.Fields{
				"namespace":   ctr.Namespace,
				"containerID": ctr.ID,
				"mountpoint":  mountpoint,
			}).Error("creating mount point for a container")

			log.WithField("containerID", ctr.ID).Warn("skipping container mount")
			continue
		}

		// Clear snapshot database for each container
		e.snapshotFile = ""
		ctx = namespaces.WithNamespace(ctx, ctr.Namespace)
		if err := e.MountContainer(ctx, ctr.ID, ctrmountpoint); err != nil {
			return err
		}
	}

	// default
	return nil
}

// ContainerDrift finds drifted files from all the containers
func (e *explorer) ContainerDrift(ctx context.Context, filter string, skipsupportcontainers bool, containerID string) ([]explorers.Drift, error) {
	var drifts []explorers.Drift
	ctrs, err := e.ListContainers(ctx)
	if err != nil {
		return nil, err
	}

	filters := strings.Split(filter, ",")

	for _, ctr := range ctrs {
		// Temporary fix to remove duplicate errors for Docker engine 29 containers
		// TODO: Build support for Docker engine 29.
		if ctr.Namespace == "moby" {
			continue
		}

		// If containerID is supplied & doesn't match skip
		if containerID != "" && ctr.ID != containerID {
			continue
		}

		// Skip Kubernetes suppot containers
		if skipsupportcontainers && ctr.SupportContainer {
			log.WithFields(log.Fields{
				"namespace":   ctr.Namespace,
				"containerID": ctr.ID,
			}).Info("skipping Kubernetes containers")

			continue
		}

		// Only analyze containers matching the filter.
		analyze := true
		for _, f := range filters {
			if !strings.Contains(f, "=") {
				continue
			}

			key := strings.Split(f, "=")[0]
			value := strings.Split(f, "=")[1]

			labelValue, ok := ctr.Labels[key]
			if !ok {
				analyze = false
				break
			}

			if labelValue != value {
				analyze = false
				break
			}
		}

		if !analyze {
			continue
		}

		e.snapshotFile = ""
		ctx = namespaces.WithNamespace(ctx, ctr.Namespace)
		store := metadata.NewContainerStore(metadata.NewDB(e.mdb, nil, nil))

		container, err := store.Get(ctx, ctr.ID)
		if err != nil {
			log.WithFields(log.Fields{"containerID": ctr.ID, "error": err}).Error("getting container information")
			continue
		}
		if err := e.resolveSnapshotter(ctx, &container); err != nil {
			return nil, fmt.Errorf("failed to resolve snapshotter: %w", err)
		}
		// Snapshot database metadata.db access
		opts := bolt.Options{
			ReadOnly: true,
		}
		if e.snapshotFile == "" {
			snapshotterFolder := e.SnapshotRoot(container.Snapshotter)
			if snapshotterFolder != "unknown" {
				e.snapshotFile = filepath.Join(snapshotterFolder, "metadata.db")
			}
		}
		log.WithFields(log.Fields{
			"snapshotter":       container.Snapshotter,
			"snapshotKey":       container.SnapshotKey,
			"image":             container.Image,
			"snapshotterFolder": e.snapshotFile,
		}).Debug("container snapshotter")
		ssdb, err := bolt.Open(e.snapshotFile, 0444, &opts)
		if err != nil {
			log.WithFields(log.Fields{"containerID": ctr.ID, "error": err}).Error("failed to open snapshot database")
			continue
		}
		// snapshot store
		ssstore := newSnapshotStore(e.containerdRoot, e.layercache, e.mdb, ssdb)
		hasWorkDir := false
		snapshotRoot, _ := filepath.Split(e.snapshotFile)
		matches, _ := filepath.Glob(filepath.Join(snapshotRoot, "snapshots/*/work"))
		if len(matches) > 0 {
			hasWorkDir = true
		}
		if container.Snapshotter == "native" {
			upperdir, err := ssstore.NativePath(ctx, container)
			log.WithFields(log.Fields{
				"upperdir": upperdir,
			}).Debug("native directories")
			if err != nil {
				log.WithFields(log.Fields{"containerID": ctr.ID, "error": err}).Error("failed to get native path")
				continue
			}
		} else if hasWorkDir {
			lowerdir, upperdir, workdir, err := ssstore.OverlayPath(ctx, container)
			log.WithFields(log.Fields{
				"lowerdir": lowerdir,
				"upperdir": upperdir,
				"workdir":  workdir,
			}).Debug("overlay directories")

			log.WithFields(log.Fields{
				"containerID": ctr.ID,
			}).Debug("checking container drift")
			if err != nil {
				log.WithFields(log.Fields{"containerID": ctr.ID, "error": err}).Error("failed to get overlay path")
				continue
			}
			if lowerdir == "" {
				log.WithFields(log.Fields{"containerID": ctr.ID}).Error("lowerdir is empty")
				continue
			}

			// Scan upperdir
			addedOrModified, inaccessibleFiles, err := explorers.ScanDiffDirectory(upperdir)
			if err != nil {
				log.WithFields(log.Fields{"containerID": ctr.ID, "error": err}).Error("failed to scan diff directory")
				continue
			}

			drift := explorers.Drift{
				ContainerID:       ctr.ID,
				ContainerType:     ctr.ContainerType,
				AddedOrModified:   addedOrModified,
				InaccessibleFiles: inaccessibleFiles,
			}

			drifts = append(drifts, drift)

			for _, path := range addedOrModified {
				log.WithFields(log.Fields{
					"A ": path}).Debug("added or modified files")
			}
			if len(inaccessibleFiles) > 0 {
				for _, path := range inaccessibleFiles {
					log.WithFields(log.Fields{
						"D ": path}).Debug("deleted files")
				}
			}
		} else {
			log.Error("unsupported snapshotter ", container.Snapshotter)
		}
	}

	// default
	return drifts, nil
}

// Close releases the internal resources
func (e *explorer) Close() error {
	return e.mdb.Close()
}

func (e *explorer) GetContainerByID(ctx context.Context, containerID string) (*explorers.Container, error) {
	container, ns, err := e.getContainerStoreInfo(ctx, containerID)
	if err != nil {
		return nil, err
	}

	cectr := convertToContainerExplorerContainer(ns, container)
	cectr.ImageBase = imageBasename(cectr.Image)
	cectr.SupportContainer = e.sc.IsSupportContainer(cectr)
	task, err := e.GetContainerTask(ctx, cectr)
	if err != nil {
		log.WithField("containerID", cectr.ID).Error("failed getting container task")
	}
	cectr.ProcessID = task.PID
	cectr.ContainerType = task.ContainerType
	cectr.Status = task.Status

	return &cectr, nil
}

func (e *explorer) Type() string {
	return "containerd"
}

// convertToContainerExplorerContainer returns a Container object which is
// superset of containers.Container object.
func convertToContainerExplorerContainer(ns string, ctr containers.Container) explorers.Container {
	var hostname string

	// Try using io.kubernetes.pod.name as the hostname.
	//
	// TODO(rmaskey): Research if EKS and AKS has similar labels used
	// for storing hostname.
	if value, match := ctr.Labels["io.kubernetes.pod.name"]; match {
		hostname = value
	}

	// Get hostname from runtime fields
	if hostname == "" && ctr.Spec != nil && ctr.Spec.GetValue() != nil {
		var v spec.Spec
		json.Unmarshal(ctr.Spec.GetValue(), &v)

		if v.Hostname != "" {
			hostname = v.Hostname
		} else {
			// Using HOSTNAME from environment as last resort.
			// HOSTNAME contains node's hostname.
			for _, kv := range v.Process.Env {
				if strings.HasPrefix(kv, "HOSTNAME=") {
					hostname = strings.TrimSpace(strings.Split(kv, "=")[1])
					break
				}
			}
		}
	}

	return explorers.Container{
		Namespace: ns,
		Name:      ctr.ID,
		Hostname:  hostname,
		Container: ctr,
	}
}

// parseSpec parses containerd spec and returns the information as JSON.
func parseSpec(data []byte) (any, error) {
	var v spec.Spec
	json.Unmarshal(data, &v)
	return v, nil
}

// imageBasename returns the image base name without version information to
// match with supportcontainer.yaml configuration.
func imageBasename(image string) string {
	imagebase := image

	if strings.Contains(imagebase, "@") {
		imagebase = strings.Split(imagebase, "@")[0]
	}

	if strings.Contains(imagebase, ":") {
		imagebase = strings.Split(imagebase, ":")[0]
	}
	return imagebase
}
