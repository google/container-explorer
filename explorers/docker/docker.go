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

// Package docker implements the ContainerExplorer interface for exploring Docker managed containers.
package docker

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/metadata"

	"github.com/google/container-explorer/explorers"
	"github.com/google/container-explorer/utils"

	"github.com/containerd/containerd/v2/core/mount"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	log "github.com/sirupsen/logrus"
	bolt "go.etcd.io/bbolt"
)

const (
	configV2Filename     = "config.v2.json"
	containerDirName     = "containers"
	lowerdirName         = "lower"
	imageDirName         = "image"
	repositoriesFileName = "repositories.json"
	storageOverlay2      = "overlay2"
)

var imagerepo map[string]string

type ImageName map[string]string

type ImageRepository struct {
	Repositories map[string]ImageName
}

type explorer struct {
	imageRoot      string                      // Image root directory
	containerdRoot string                      // containerd root directory
	dockerRoot     string                      // Docker root directory
	manifestPath   string                      // Containerd manifest path. io.containerd.manifest.v1.bolt/meta.db
	manifestDB     *bolt.DB                    // Containerd manifest database handle
	snapshotPath   string                      // Docker29+ overlayfs snapshotter database (containerd's overlayfs snapshotter)
	snapshotDB     *bolt.DB                    // Containerd overlayfs snapshotter database handle
	sc             *explorers.SupportContainer // support container object
}

// NewExplorer returns a ContainerExplorer interface to explorer docker managed
// containers.
func NewExplorer(imageRoot string, containerdRoot string, dockerRoot string) (explorers.ContainerExplorer, error) {
	if _, err := utils.PathExists(dockerRoot); err != nil {
		return nil, fmt.Errorf("docker root directory does not exist")
	}

	// Checking if containerd directory exists
	var mdb *bolt.DB
	var sdb *bolt.DB
	var err error

	if containerdRoot == "" {
		return nil, fmt.Errorf("containerd root directory is empty")
	}

	manifestPath := filepath.Join(containerdRoot, "io.containerd.metadata.v1.bolt", "meta.db")

	if fileExists(manifestPath) {
		mdb, err = bolt.Open(manifestPath, 0444, &bolt.Options{ReadOnly: true})
		if err != nil {
			return &explorer{}, err
		}
	}

	// Starting with Docker version 29, Docker uses containerd's overlayfs snapshotter.
	// Specifying the use of containerd overlayfs snapshotter for current implementation.
	// This may change in the future.
	// TODO: Better way to identify the snapshotter filesystem in use.
	snapshotPath := filepath.Join(containerdRoot, "io.containerd.snapshotter.v1.overlayfs", "metadata.db")
	if fileExists(snapshotPath) {
		sdb, err = bolt.Open(snapshotPath, 0644, &bolt.Options{ReadOnly: true})
		if err != nil {
			if mdb != nil {
				mdb.Close()
			}
			return &explorer{}, err
		}
	}

	log.WithFields(log.Fields{
		"imageRootDir":      imageRoot,
		"containerdRootDir": containerdRoot,
		"dockerRootDir":     dockerRoot,
		"manifestPath":      manifestPath,
		"snapshotPath":      snapshotPath,
	}).Debug("new docker explorer")

	sc, _ := explorers.NewSupportContainer("")

	return &explorer{
		imageRoot:      imageRoot,
		containerdRoot: containerdRoot,
		dockerRoot:     dockerRoot,
		manifestPath:   manifestPath,
		manifestDB:     mdb,
		snapshotPath:   snapshotPath,
		snapshotDB:     sdb,
		sc:             sc,
	}, nil
}

// SnapshotRoot returns the snapshot root directory for docker managed
// containers.
//
//nolint:revive // function is not yet implemented
func (e *explorer) SnapshotRoot(snapshotter string) string {
	// TODO(rmaskey): implement the function
	return ""
}

// ListNamespaces returns namespaces for docker managed containers.
func (e *explorer) ListNamespaces(ctx context.Context) ([]string, error) {
	var nss []string

	// Namespaces in metadata file i.e. meta.db
	// in /var/lib/containerd/io.containerd.metadata.v1.bolt/meta.db
	if e.manifestDB != nil {
		err := e.manifestDB.View(func(tx *bolt.Tx) error {
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
	}

	return nss, nil
}

// GetContainerByID returns a Container for a given container ID.
func (e *explorer) GetContainerByID(ctx context.Context, containerID string) (*explorers.Container, error) {
	containers, err := e.ListContainers(ctx)
	if err != nil {
		return nil, err
	}

	for _, container := range containers {
		if container.ID == containerID {
			return &container, nil
		}
	}

	return nil, fmt.Errorf("no matching container")
}

// Type returns the container runtime type, which is "docker".
func (e *explorer) Type() string {
	return "docker"
}

// GetContainerIDs returns container ID
func (e *explorer) GetContainerIDs(_ context.Context, _ string) ([]string, error) {
	containerPaths, err := filepath.Glob(filepath.Join(e.dockerRoot, containerDirName, "*"))
	if err != nil {
		return nil, err
	}

	var containerIDs []string
	for _, containerPath := range containerPaths {
		_, containerID := filepath.Split(containerPath)
		containerIDs = append(containerIDs, containerID)
	}
	return containerIDs, nil
}

// ListContainers returns container information.
func (e *explorer) ListContainers(ctx context.Context) ([]explorers.Container, error) {
	containerDir := filepath.Join(e.dockerRoot, containerDirName)
	log.WithFields(log.Fields{
		"dockerRoot":   e.dockerRoot,
		"containerDir": containerDir,
	}).Debug("docker containers directory")

	containerIDs, err := e.GetContainerIDs(ctx, containerDir)
	if err != nil {
		return nil, err
	}

	var cecontainers []explorers.Container

	for _, containerID := range containerIDs {
		cectr, err := e.GetCEContainer(ctx, containerID)
		if err != nil {
			return nil, err
		}
		cecontainers = append(cecontainers, cectr)
	}

	return cecontainers, nil
}

// structure to hold limited docker image information
//
// The structure hold information from the file
// /var/lib/docker/image/overlay2/imagedb/content/sha256/<imageid>
//
// Reference to docker source code https://github.com/moby/moby/image/image.go

type rootfs struct {
	Rfstype string   `json:"type"`
	DiffIDs []string `json:"diff_ids"`
}

// Refer to struct History
type historyItem struct {
	Created    time.Time `json:"created"`
	Author     string    `json:"author,omitempty"`
	CreatedBy  string    `json:"created_by,omitempty"`
	Comment    string    `json:"comment,omitempty"`
	EmptyLayer bool      `json:"empty_layer,omitempty"`
}

// Refer to structs Image and V1Image
type imageContentSummary struct {
	ID              string        `json:"id,omitempty"`
	Architecture    string        `json:"architecture"`
	Comment         string        `json:"comment,omitempty"`
	Config          Config        `json:"config"`
	Container       string        `json:"container"`
	ContainerConfig Config        `json:"container_config"`
	Created         time.Time     `json:"created"`
	DockerVersion   string        `json:"docker_version"`
	History         []historyItem `json:"history"`
	Os              string        `json:"os"`
	Parent          string        `json:"parent,omitempty"`
	Rootfs          rootfs        `json:"rootfs"`
}

// ListImages returns information about docker images.
func (e *explorer) ListImages(_ context.Context) ([]explorers.Image, error) {
	// TODO (rmaskey): Handle docker version 1 images

	// Docker version 2
	//
	// Check for valid image repositories directory
	repositoriesDir := filepath.Join(e.dockerRoot, imageDirName)
	if !fileExists(repositoriesDir) {
		return nil, fmt.Errorf("valid image repositories directory %s not found", repositoriesDir)
	}

	storageDirs, err := filepath.Glob(filepath.Join(repositoriesDir, "*"))
	if err != nil {
		return nil, fmt.Errorf("listing storage directories %v", err)
	}

	var ceimages []explorers.Image

	for _, storageDir := range storageDirs {
		_, storageName := filepath.Split(storageDir)
		repositoriesFile := filepath.Join(storageDir, repositoriesFileName)

		log.WithFields(log.Fields{
			"storageName":      storageName,
			"storageDir":       storageDir,
			"repositoriesFile": repositoriesFile,
		}).Debug("image repository file")

		data, err := os.ReadFile(repositoriesFile)
		if err != nil {
			log.WithFields(log.Fields{
				"storageName":      storageName,
				"repositoriesFile": repositoriesFile,
				"error":            err,
			}).Debug("repositories.json does not exist")
			continue
		}

		var r ImageRepository
		if err := json.Unmarshal(data, &r); err != nil {
			log.WithFields(log.Fields{
				"repositoriesFile": repositoriesFile,
				"message":          err,
			}).Debug("unmarshalling repositories.json")
			continue
		}

		for _, distvalue := range r.Repositories {
			for k, v := range distvalue {
				image := images.Image{
					Name: k,
					Target: ocispec.Descriptor{
						Digest: digest.Digest(v),
					},
				}

				if storageName == storageOverlay2 {
					imageContent, err := readImageContent(storageName, storageDir, image.Target.Digest)
					if err != nil {
						log.Errorf("reading image content file: %v", err)
					} else {
						image.CreatedAt = imageContent.Created
					}
				}

				ceimages = append(ceimages, explorers.Image{
					ContainerType:         "docker",
					Image:                 image,
					SupportContainerImage: e.sc.SupportContainerImage(imageBasename(image.Name)),
				})
			}
		}
	}

	return ceimages, nil
}

// ListContent returns content information.
func (e *explorer) ListContent(_ context.Context) ([]explorers.Content, error) {
	// TODO(rmaskey): implement the function
	log.Info("listing docker content not implemented")

	return nil, nil
}

// ListSnapshots returns snapshot information.
func (e *explorer) ListSnapshots(_ context.Context) ([]explorers.SnapshotKeyInfo, error) {
	// TODO(rmaskey): implement the function
	log.Info("listing docker snapshots is not implemented")

	return nil, nil
}

// ListTasks returns container task status
func (e *explorer) ListTasks(_ context.Context) ([]explorers.Task, error) {
	var tasks []explorers.Task

	containerPaths, err := filepath.Glob(filepath.Join(e.dockerRoot, "containers", "*"))
	if err != nil {
		return nil, fmt.Errorf("listing docker container directories: %w", err)
	}

	for _, containerPath := range containerPaths {
		configFile := filepath.Join(containerPath, "config.v2.json")

		configData, err := os.ReadFile(configFile)
		if err != nil {
			return nil, fmt.Errorf("reading container config: %w", err)
		}

		var config ConfigFile
		if err := json.Unmarshal(configData, &config); err != nil {
			return nil, fmt.Errorf("unmarshalling config.v2.json: %w", err)
		}

		var status string
		if config.State.Paused {
			status = "paused"
		} else if config.State.Running {
			status = "running"
		}

		task := explorers.Task{
			ContainerType: "docker",
			Name:          config.ID,
			PID:           int(config.State.Pid),
			Status:        status,
		}

		tasks = append(tasks, task)
	}

	return tasks, nil
}

// InfoContainer returns container internal information.
func (e *explorer) InfoContainer(ctx context.Context, containerID string, _ bool) (any, error) {
	_, err := e.GetContainerByID(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("getting container %s: %w", containerID, err)
	}

	container, err := e.ReadContainerConfig(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("reading container config: %w", err)
	}

	return container, nil
}

// MountContainer mounts a container's filesystem layers to the specified mountpoint path.
func (e *explorer) MountContainer(ctx context.Context, containerID string, mountpoint string) error {
	container, err := e.ReadContainerConfig(ctx, containerID)
	if err != nil {
		return fmt.Errorf("reading container config: %w", err)
	}

	switch container.Driver {
	case "overlay2":
		return e.mountDockerV2Container(ctx, container, containerID, mountpoint)
	case "overlayfs":
		return e.mountDockerV29Container(ctx, container, containerID, mountpoint)
	default:
		return fmt.Errorf("unsupported storage driver: %s", container.Driver)
	}
}

// mountDockerV2Container mounts a container to the specified path
func (e *explorer) mountDockerV2Container(_ context.Context, container ConfigFile, containerID string, mountpoint string) error {
	containerMountIDPath := filepath.Join(e.dockerRoot, imageDirName, container.Driver, "layerdb", "mounts", containerID, "mount-id")
	log.WithField("containerMountIDPath", containerMountIDPath).Debug("container mount-id path")

	mountIDByte, err := os.ReadFile(containerMountIDPath)
	if err != nil {
		return fmt.Errorf("reading container mount-id")
	}
	mountID := strings.TrimSpace(string(mountIDByte))
	log.WithField("mount-id", mountID).Debug("container mount-id")

	// build container lower directory
	lowerdirpath := filepath.Join(e.dockerRoot, container.Driver, mountID, lowerdirName)
	log.WithField("lowerdirpath", lowerdirpath).Debug("container lowerdir path")
	//nolint:gosec // G703: Path is constructed from trusted docker root and config
	data, err := os.ReadFile(lowerdirpath)
	if err != nil {
		return fmt.Errorf("reading lower file %v", err)
	}

	// Computing lowerdir for mounting
	var lowerDirs []string
	for _, ldir := range strings.Split(strings.TrimSpace(string(data)), ":") {
		lowerDirs = append(lowerDirs, filepath.Join(e.dockerRoot, container.Driver, ldir))
	}
	lowerDir := strings.Join(lowerDirs, ":")

	// Getting upperdir for mounting
	//nolint:gosec // G703: Path is constructed from trusted docker root and config
	upperData, err := os.ReadFile(filepath.Join(e.dockerRoot, container.Driver, mountID, "link"))
	if err != nil {
		return fmt.Errorf("reading link file %v", err)
	}
	upperDir := filepath.Join(e.dockerRoot, container.Driver, "l", strings.TrimSpace(string(upperData)))

	log.WithFields(log.Fields{
		"lowerdir": lowerDir,
		"upperdir": upperDir,
	}).Debug("container overlay directories")

	// mounting container
	mountopts := fmt.Sprintf("ro,lowerdir=%s:%s", upperDir, lowerDir)
	mountargs := []string{"-t", "overlay", "overlay", "-o", mountopts, mountpoint}

	cmd := exec.Command("mount", mountargs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Errorf("running mount command: %v", mountargs)

		if strings.Contains(err.Error(), " 32") {
			if string(out) != "" {
				return fmt.Errorf("invalid lowerdir path %v; output: %s", err, strings.TrimSpace(string(out)))
			}
			return fmt.Errorf("invalid lowerdir path %v: use --debug to view lowerdir path", err)
		}
		if string(out) != "" {
			return fmt.Errorf("executing mount command %v; output: %s", err, strings.TrimSpace(string(out)))
		}
		return fmt.Errorf("executing mount command %v", err)
	}

	if string(out) != "" {
		log.WithField("mount command", string(out)).Debug("container mount command")
	}

	return nil
}

func (e *explorer) GetOverlayfsLayers(namespace string, containerID string) (string, []string, error) {
	var overlayPath string
	var activeBucketName string
	var activeID uint64
	var upperPath string
	var lowerPaths []string

	if e.snapshotDB == nil {
		return "", nil, fmt.Errorf("no access to snapshot database %s", e.snapshotPath)
	}

	overlayPath = filepath.Dir(e.snapshotPath)

	err := e.snapshotDB.View(func(tx *bolt.Tx) error {
		// Open the top-level "v1" Bucket
		v1Bucket := tx.Bucket([]byte("v1"))
		if v1Bucket == nil {
			return fmt.Errorf("top-level bucket v1 not found in database")
		}

		// Open the "snapshots" sub-bucket
		snapshotsBucket := v1Bucket.Bucket([]byte("snapshots"))
		if snapshotsBucket == nil {
			return fmt.Errorf("level-two bucket snapshots not found in database")
		}

		// 1. Local snapshot bucket matching the container ID.
		// Match namespace if provided otherwise just match container ID.
		// The key/bucket name format in bbolt is typically "<namespace>/<db-sequence-id><snapshot-key>"
		var matches []string
		err := snapshotsBucket.ForEach(func(k, v []byte) error {
			if v == nil { // Sub-bucket
				sName := string(k)
				parts := strings.Split(sName, "/")
				if len(parts) == 3 && parts[0] == namespace {
					if parts[2] == containerID {
						activeBucketName = sName
						return nil
					}
					// Allow matching short container ID (prefix match)
					if strings.HasPrefix(parts[2], containerID) {
						matches = append(matches, sName)
					}
				}
			}

			return nil
		})
		if err != nil {
			return fmt.Errorf("checking snapshot bucket: %w", err)
		}

		if activeBucketName == "" {
			if len(matches) == 0 {
				return fmt.Errorf("snapshot matching %q not found in namespace %q", containerID, namespace)
			} else if len(matches) > 1 {
				return fmt.Errorf("multiple snapshots match prefix %q in namespace %q: %v", containerID, namespace, matches)
			}
			activeBucketName = matches[0]
		}

		// 2. Trace the parent chain to reconstruct the overlay layers
		currentBucketName := activeBucketName
		visited := make(map[string]bool)
		for currentBucketName != "" {
			if visited[currentBucketName] {
				return fmt.Errorf("cycle detected in snapshot parent chain at %q", currentBucketName)
			}
			visited[currentBucketName] = true

			b := snapshotsBucket.Bucket([]byte(currentBucketName))
			if b == nil {
				return fmt.Errorf("failed to open snapshot bucket: %s", currentBucketName)
			}

			idBytes := b.Get([]byte("id"))
			if len(idBytes) == 0 {
				return fmt.Errorf("key id not found in for snapshot %q", currentBucketName)
			}
			dirID, _ := binary.Uvarint(idBytes)

			parentBytes := b.Get([]byte("parent"))
			parentName := string(parentBytes)

			if currentBucketName == activeBucketName {
				activeID = dirID
			} else {
				fsPath := filepath.Join(overlayPath, "snapshots", fmt.Sprintf("%d", dirID), "fs")
				lowerPaths = append(lowerPaths, fsPath)
			}

			currentBucketName = parentName
		}

		return nil
	})

	if err != nil {
		return "", nil, fmt.Errorf("getting snapshot: %w", err)
	}

	activeSnapDir := filepath.Join(overlayPath, "snapshots", fmt.Sprintf("%d", activeID))
	upperPath = filepath.Join(activeSnapDir, "fs")

	return upperPath, lowerPaths, nil
}

// mountDockerV29Container mounts container layers for Docker version 29+ using containerd's overlayfs snapshotter metadata.
func (e *explorer) mountDockerV29Container(_ context.Context, _ ConfigFile, containerID string, mountpoint string) error {
	upperdir, lowerPaths, err := e.GetOverlayfsLayers("moby", containerID)
	if err != nil {
		return fmt.Errorf("getting overlay layers: %w", err)
	}

	// Checking mount point
	absMountPoint, err := filepath.Abs(mountpoint)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute mount path: %w", err)
	}

	// Create the mountpoint if it does not exist
	if _, err := os.Stat(absMountPoint); os.IsNotExist(err) {
		if err := os.MkdirAll(absMountPoint, 0o755); err != nil {
			return fmt.Errorf("failed to create mount point directory: %w", err)
		}
	}

	// Prepare standard readonly overlayfs mount options
	options := []string{
		"ro",
		fmt.Sprintf("lowerdir=%s:%s", upperdir, strings.Join(lowerPaths, ":")),
	}
	mounts := []mount.Mount{
		{
			Type:    "overlay",
			Source:  "overlay",
			Options: options,
		},
	}

	if err := mount.All(mounts, absMountPoint); err != nil {
		return fmt.Errorf("read-only overlay mount failed: %w", err)
	}

	return nil
}

// MountAllContainers mounts all the containers
func (e *explorer) MountAllContainers(ctx context.Context, mountpoint string, filter string, skipsupportcontainers bool) error {
	containerDir := filepath.Join(e.dockerRoot, containerDirName)
	log.WithField("containerDir", containerDir).Debug("docker containers directory")

	containerIDs, err := e.GetContainerIDs(ctx, containerDir)
	if err != nil {
		return fmt.Errorf("failed listing containers ID %v", err)
	}
	if containerIDs == nil {
		return fmt.Errorf("no container ID returned")
	}

	filters := strings.Split(filter, ",")

	for _, containerID := range containerIDs {
		cecontainer, err := e.GetCEContainer(ctx, containerID)
		if err != nil {
			log.WithField("containerID", containerID).Error("getting container details")
			log.WithField("containerID", containerID).Warn("skipping container mount")
			continue
		}

		if skipsupportcontainers && cecontainer.SupportContainer {
			log.WithFields(log.Fields{
				"namespace":   cecontainer.Namespace,
				"containerID": cecontainer.ID,
			}).Info("skipping Kubernetes support container")
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

			labelValue, ok := cecontainer.Labels[key]
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

		// Create mountpoint for each container
		ctrmountpoint := filepath.Join(mountpoint, cecontainer.ID)
		if err := os.MkdirAll(ctrmountpoint, 0755); err != nil {
			log.WithFields(log.Fields{
				"namespace":   cecontainer.Namespace,
				"containerID": cecontainer.ID,
				"mountpoint":  ctrmountpoint,
			}).Error("creating mountpoint for container")
			log.WithField("containerID", containerID).Warn("skippoing container mount")
			continue
		}

		if err := e.MountContainer(ctx, containerID, ctrmountpoint); err != nil {
			log.WithFields(log.Fields{
				"containerID": containerID,
				"message":     err.Error(),
			}).Error("mounting container")
		}
	}

	// default
	return nil
}

// ContainerDrift finds drifted files from all the containers
func (e *explorer) ContainerDrift(ctx context.Context, filter string, skipsupportcontainers bool, containerID string) ([]explorers.Drift, error) {
	var drifts []explorers.Drift
	containerDir := filepath.Join(e.dockerRoot, containerDirName)
	log.WithField("containerDir", containerDir).Debug("docker containers directory")

	containerIDs, err := e.GetContainerIDs(ctx, containerDir)
	if err != nil {
		return nil, fmt.Errorf("failed listing container IDs %v", err)
	}
	if containerIDs == nil {
		return nil, fmt.Errorf("no container IDs returned")
	}

	filters := strings.Split(filter, ",")

	for _, id := range containerIDs {
		cecontainer, err := e.GetCEContainer(ctx, id)
		if err != nil {
			log.WithFields(log.Fields{
				"containerID": id,
				"message":     err.Error(),
			}).Warn("unable to get container details. Skipping container mount")
			continue
		}

		// If containerID is supplied & doesn't match skip
		if containerID != "" && cecontainer.ID != containerID {
			continue
		}

		if skipsupportcontainers && cecontainer.SupportContainer {
			log.WithFields(log.Fields{
				"namespace":   cecontainer.Namespace,
				"containerID": cecontainer.ID,
			}).Info("skipping Kubernetes support container")
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

			labelValue, ok := cecontainer.Labels[key]
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

		container, err := e.ReadContainerConfig(ctx, cecontainer.ID)
		if err != nil {
			log.WithFields(log.Fields{"containerID": cecontainer.ID, "error": err}).Error("getting container")
			continue
		}

		// Container upper directory for drift scanning
		var upperDir string

		switch container.Driver {
		case "overlay2":
			containerMountIDPath := filepath.Join(e.dockerRoot, imageDirName, container.Driver, "layerdb", "mounts", container.ID, "mount-id")

			mountIDByte, err := os.ReadFile(containerMountIDPath)
			if err != nil {
				log.WithFields(log.Fields{
					"containerID": container.ID,
					"message":     err,
				}).Info("reading container mount-id")
				continue
			}
			mountID := strings.TrimSpace(string(mountIDByte))

			upperDirLinkFile := filepath.Join(e.dockerRoot, container.Driver, mountID, "link")

			//nolint:gosec // G703: Path is constructed from trusted docker root and config
			linkData, err := os.ReadFile(upperDirLinkFile)
			if err != nil {
				log.WithFields(log.Fields{
					"containerID": container.ID,
					"message":     err,
				}).Info("reading upperdir link file")
				continue
			}
			upperDir = filepath.Join(e.dockerRoot, container.Driver, "l", strings.TrimSpace(string(linkData)))

		case "overlayfs":
			upperDir, _, err = e.GetOverlayfsLayers("moby", container.ID)
			if err != nil {
				log.WithFields(log.Fields{
					"containerID": container.ID,
					"error":       err,
				}).Info("getting upperdir snapshot")
				continue
			}

		default:
			log.WithField("containerID", container.ID).Warn("unable to find upperdir")
			log.WithFields(log.Fields{
				"containerType": e.Type(),
				"containerID":   container.ID,
				"driver":        container.Driver,
			}).Info("unsupported driver")
			upperDir = ""
			continue
		}

		// ScanDiff
		addedOrModified, inaccessibleFiles, err := explorers.ScanDiffDirectory(upperDir)
		if err != nil {
			log.WithFields(log.Fields{"containerID": container.ID, "error": err}).Error("failed to scan diff directory")
			continue
		}
		drift := explorers.Drift{
			ContainerID:       cecontainer.ID,
			ContainerType:     cecontainer.ContainerType,
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
	}
	// default
	return drifts, nil
}

// Close releases internal resources.
func (e *explorer) Close() error {
	var errs []string
	// Close handle to io.containerd.manifest.v1.bolt.v1/meta.db
	if e.manifestDB != nil {
		if err := e.manifestDB.Close(); err != nil {
			errs = append(errs, fmt.Sprintf("closing manifestDB: %v", err))
		}
	}

	// Close handle to io.containerd.snapshotter.overlayfs.v1/metadata.db
	if e.snapshotDB != nil {
		if err := e.snapshotDB.Close(); err != nil {
			errs = append(errs, fmt.Sprintf("closing snapshotDB: %v", err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("close errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// ReadContainerConfig returns container configuration
func (e *explorer) ReadContainerConfig(_ context.Context, containerID string) (ConfigFile, error) {
	containerDir := filepath.Join(e.dockerRoot, containerDirName, containerID)
	log.WithField("containerDir", containerDir).Debug("container directory")
	if !fileExists(containerDir) {
		return ConfigFile{}, fmt.Errorf("container does not exist")
	}

	containerConfigFile := filepath.Join(containerDir, configV2Filename)
	log.WithField("containerConfigFile", containerConfigFile).Debug("container configuration file")
	if !fileExists(containerConfigFile) {
		return ConfigFile{}, fmt.Errorf("container config file %s does not exist", configV2Filename)
	}

	data, err := os.ReadFile(containerConfigFile)
	if err != nil {
		return ConfigFile{}, fmt.Errorf("reading container config file %s %v", configV2Filename, err)
	}

	var container ConfigFile
	if err := json.Unmarshal(data, &container); err != nil {
		return ConfigFile{}, fmt.Errorf("unmarshalling container config %v", err)
	}

	return container, nil
}

// GetCEContainer returns ContainerExplorer container
func (e *explorer) GetCEContainer(ctx context.Context, containerID string) (explorers.Container, error) {
	if imagerepo == nil {
		imagerepo, _ = e.GetRepositories(ctx)
	}

	// Get docker container configuration based on container ID
	config, err := e.ReadContainerConfig(ctx, containerID)
	if err != nil {
		return explorers.Container{}, err
	}

	cectr := convertToContainerExplorerContainer(config)

	// Use image friendly name if exits
	if imagerepo != nil {
		if val, found := imagerepo[cectr.Image]; found {
			cectr.Image = val
		}
	}

	// Extract imagebase name from image name
	if strings.HasPrefix(config.Name, "/") {
		cectr.Name = strings.ReplaceAll(config.Name, "/", "")
	} else {
		cectr.Name = config.Name
	}

	cectr.ImageBase = imageBasename(cectr.Image)

	// Support container is only relevant for GKE running containerd.
	cectr.SupportContainer = false

	return cectr, nil
}

// fileExists checks if a file or directory exists at the given path.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// GetRepositories returns mapping of image ID to name
func (e *explorer) GetRepositories(_ context.Context) (map[string]string, error) {
	repositoriesDir := filepath.Join(e.dockerRoot, imageDirName)
	if !fileExists(repositoriesDir) {
		return nil, fmt.Errorf("image repository directory %s does not exist", repositoriesDir)
	}

	storageDirs, err := filepath.Glob(filepath.Join(repositoriesDir, "*"))
	if err != nil {
		return nil, fmt.Errorf("listing storage directories: %v", err)
	}

	for _, storageDir := range storageDirs {
		_, storageName := filepath.Split(storageDir)

		if storageName != "overlay2" {
			// TODO(rmaskey): handle other storage
			log.WithField("storageName", storageName).Info("storage not supported")
			continue
		}

		// Handle overlay2 storage
		repositoriesFile := filepath.Join(storageDir, repositoriesFileName)
		data, err := os.ReadFile(repositoriesFile)
		if err != nil {
			return nil, fmt.Errorf("failed reading repositories file %s: %v", repositoriesFile, err)
		}

		var r ImageRepository
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("unmarshalling repositories file")
		}

		repositories := make(map[string]string)
		for _, osdist := range r.Repositories {
			for k, v := range osdist {
				// repositories.json may contain multiple entries with same digest.
				// Using the record that contains the friendly name rather <distro>@<digest> pattern
				//
				// Example: Two labels have the same hash
				// "nginx": {
				//   "nginx:latest": "sha256:605c77e624ddb75e6110f997c58876baa13f8754486b461117934b24a9dc3a85",
				//   "nginx@sha256:0d17b565c37bcbd895e9d92315a05c1c3c9a29f762b011a10c54a66cd53c9b31": "sha256:605c77e624ddb75e6110f997c58876baa13f8754486b461117934b24a9dc3a85"
				// }
				if !strings.Contains(k, "@") {
					repositories[v] = k
				}
			}
		}
		return repositories, nil
	}

	return nil, nil
}

// convertToContainerExplorerContainer maps docker config data to container
// explorer container structure
func convertToContainerExplorerContainer(config ConfigFile) explorers.Container {
	var exposedPorts []string

	if config.Config.ExposedPorts != nil {
		for k := range config.Config.ExposedPorts {
			exposedPorts = append(exposedPorts, k)
		}
	}

	var status string
	const notStarted = "0001-01-01T00:00:00Z"

	if config.State.StartedAt.Format("2006-01-02T15:04:05Z") == notStarted {
		status = "CREATED"
	} else if config.State.Running && config.State.Paused {
		status = "PAUSED"
	} else if config.State.Running && !config.State.Paused {
		status = "RUNNING"
	} else if !config.State.Running && config.State.Paused {
		status = "UNKNOWN"
	} else if !config.State.Running && !config.State.Paused {
		status = "STOPPED"
	}

	var containerName string
	if strings.HasPrefix(config.Name, "/") {
		containerName = strings.Replace(config.Name, "/", "", 1)
	}

	return explorers.Container{
		Name:          containerName,
		Hostname:      containerName,
		ProcessID:     int(config.State.Pid),
		ContainerType: "docker",
		Container: containers.Container{
			ID:          config.ID,
			CreatedAt:   config.Created,
			Image:       config.Image,
			Snapshotter: config.Driver,
			Runtime: containers.RuntimeInfo{
				Name: config.Name,
			},
		},
		Running:      config.State.Running,
		ExposedPorts: exposedPorts,
		Status:       status,
	}
}

// readImageContent reads the content of overlay2 image content
func readImageContent(storageName string, storagePath string, digest digest.Digest) (imageContentSummary, error) {
	m := strings.Split(string(digest), ":")
	if len(m) != 2 {
		return imageContentSummary{}, fmt.Errorf("expecting two colon separated values")
	}
	algo := m[0]
	filename := m[1]

	imageContentFile := filepath.Join(storagePath, "imagedb", "content", algo, filename)
	log.WithFields(log.Fields{
		"imageContentFile": imageContentFile,
	}).Debug("reading docker image content file")

	data, err := os.ReadFile(imageContentFile)
	if err != nil {
		log.WithFields(log.Fields{
			"storageName": storageName,
			"algo":        algo,
			"filename":    filename,
		}).Debug("reading docker image content file")

		return imageContentSummary{}, err
	}

	var imageContent imageContentSummary
	if err := json.Unmarshal(data, &imageContent); err != nil {
		return imageContentSummary{}, err
	}

	return imageContent, nil
}

// imageBasename returns the base name of an image
func imageBasename(name string) string {
	imageBase := strings.ReplaceAll(name, "\"", "")

	if strings.Contains(imageBase, "@") {
		imageBase = strings.Split(imageBase, "@")[0]
	}

	log.WithFields(log.Fields{
		"imageName": name,
		"imageBase": imageBase,
	}).Debug("extracting image base from image")

	return imageBase
}
