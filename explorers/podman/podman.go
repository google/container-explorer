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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/container-explorer/explorers"
	"github.com/google/container-explorer/utils"

	"github.com/containerd/containerd/images"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/containerd/containerd/containers"
	"github.com/containers/podman/v6/libpod"
	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
)

const (
	defaultUserPodmanDir = ".local/share/containers"
)

type explorer struct {
	imageroot string
}

// NewExplorer returns ContainerExplorer interface to explore podman containers.
func NewExplorer(imageroot string) (explorers.ContainerExplorer, error) {
	return &explorer{
		imageroot: imageroot,
	}, nil
}

// GetContainerByID returns Container for a given container ID or container name.
func (e *explorer) GetContainerByID(ctx context.Context, containerID string) (*explorers.Container, error) {
	containers, err := e.ListContainers(ctx)
	if err != nil {
		return nil, err
	}

	for _, container := range containers {
		if container.ID == containerID || container.Name == containerID {
			return &container, nil
		}
	}

	return nil, fmt.Errorf("no matching container")
}

// ListNamespaces returns podman namespaces if exist.
func (e *explorer) ListNamespaces(ctx context.Context) ([]string, error) {
	// No namespaces in podman returning nil, nil
	log.Info("listing namespaces is not supported in podman")

	return nil, nil
}

// ListSnapshots returns podman containers snapshots.
func (e *explorer) ListSnapshots(ctx context.Context) ([]explorers.SnapshotKeyInfo, error) {
	// No snapshots for podman
	log.Info("listing snapshots is not implemented in podman")

	return nil, nil
}

// SnapshotRoot returns snapshot root directory.
func (e *explorer) SnapshotRoot(snapshotter string) string {
	// No snapshot root for podman
	log.Info("snapshot root concept is not applicable in podman")

	return ""
}

// ListContainers returns all podman containers.
func (e *explorer) ListContainers(ctx context.Context) ([]explorers.Container, error) {
	var podmanContainers []explorers.Container

	podmanRootDirs, err := e.getPodmanRootDirs()
	if err != nil {
		return nil, fmt.Errorf("getting podman root directories: %w", err)
	}

	for _, podmanRootDir := range podmanRootDirs {
		configs, err := e.readContainerConfig(podmanRootDir)
		if err != nil {
			log.WithFields(log.Fields{"podmanRootDir": podmanRootDir, "error": err}).Debug("reading container config")
			continue
		}

		var metadata containerMetadata

		for _, config := range configs {
			if err := json.Unmarshal([]byte(config.Metadata), &metadata); err != nil {
				log.WithFields(log.Fields{"containerID": config.ID, "error": err}).Debug("unmarshalling container metadata")
				continue
			}

			parsedTime, err := time.Parse(time.RFC3339Nano, config.Created)
			if err != nil {
				log.WithFields(log.Fields{"containerID": config.ID, "error": err}).Debug("parsing container creation time")
			}

			podmanContainer := explorers.Container{
				Name:             metadata.Name,
				Hostname:         metadata.Name,
				ImageBase:        metadata.ImageName,
				SupportContainer: false,
				ContainerType:    "podman",
				Container: containers.Container{
					ID:        config.ID,
					Image:     metadata.ImageName,
					CreatedAt: parsedTime,
				},
			}
			podmanContainers = append(podmanContainers, podmanContainer)
		}
	}

	return podmanContainers, nil
}

// ListImages returns podman images.
func (e *explorer) ListImages(ctx context.Context) ([]explorers.Image, error) {
	podmanRootDirs, err := e.getPodmanRootDirs()
	if err != nil {
		return nil, fmt.Errorf("getting podman root directories: %w", err)
	}

	var ceImages []explorers.Image

	for _, podmanRootDir := range podmanRootDirs {
		imageConfigFile := filepath.Join(podmanRootDir, "storage", "overlay-images", "images.json")
		if ok := utils.PathExistsV2(imageConfigFile); !ok {
			log.WithField("imageConfigPath", imageConfigFile).Info("podman image config file not found")
			continue
		}

		data, err := os.ReadFile(imageConfigFile)
		if err != nil {
			log.WithError(err).Error("reading image config file")
			continue
		}

		var pmImages []containerImage
		if err := json.Unmarshal(data, &pmImages); err != nil {
			log.WithError(err).Error("unmarshalling image config file")
			continue
		}

		for _, pmImage := range pmImages {
			createdAt, err := time.Parse(time.RFC3339Nano, pmImage.Created)
			if err != nil {
				log.WithFields(log.Fields{"imageID": pmImage.ID, "error": err}).Debug("parsing image creation time")
			}

			var imageManifest ocispec.Manifest

			imageManifestFile := filepath.Join(podmanRootDir, "storage", "overlay-images", pmImage.ID, "manifest")
			imageManifestData, err := os.ReadFile(imageManifestFile)
			if err != nil {
				log.WithFields(log.Fields{
					"imageManifestFile": imageManifestFile,
					"error":             err,
				}).Error("reading podman image manifest file")
			} else {
				if err := json.Unmarshal(imageManifestData, &imageManifest); err != nil {
					log.WithFields(log.Fields{"imageID": pmImage.ID, "error": err}).Debug("unmarshalling image manifest")
				}
			}

			imageName := ""
			if len(pmImage.Names) > 0 {
				imageName = pmImage.Names[0]
			}

			ceImage := explorers.Image{
				ContainerType: "podman",
				Image: images.Image{
					Name: imageName,
					Target: ocispec.Descriptor{
						Digest:    digest.Digest(pmImage.Digest),
						MediaType: imageManifest.MediaType,
					},
					CreatedAt: createdAt,
				},
				SupportContainerImage: false,
			}

			ceImages = append(ceImages, ceImage)
		}
	}

	return ceImages, nil
}

// ListContent returns container contents.
func (e *explorer) ListContent(ctx context.Context) ([]explorers.Content, error) {
	log.Info("listing content is not implemented for podman")

	return nil, nil
}

// ListTasks returns running tasks.
func (e *explorer) ListTasks(ctx context.Context) ([]explorers.Task, error) {
	podmanRootDirs, err := e.getPodmanRootDirs()
	if err != nil {
		return nil, fmt.Errorf("getting podman root dirs: %w", err)
	}

	var containerTasks []explorers.Task

	for _, podmanroot := range podmanRootDirs {
		dbfile := filepath.Join(podmanroot, "storage", "db.sql")

		conn, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", dbfile))
		if err != nil {
			return nil, fmt.Errorf("opening sqlite database: %w", err)
		}

		err = func() error {
			defer conn.Close()

			rows, err := conn.Query("SELECT ID, JSON FROM ContainerState;")
			if err != nil {
				return fmt.Errorf("query podman container state: %w", err)
			}
			defer rows.Close()

			for rows.Next() {
				var id, stateJSON string
				if err := rows.Scan(&id, &stateJSON); err != nil {
					return fmt.Errorf("reading container state row: %w", err)
				}

				var containerstate libpod.ContainerState
				if err := json.Unmarshal([]byte(stateJSON), &containerstate); err != nil {
					return fmt.Errorf("unmarshalling podman container state json: %w", err)
				}

				containerTask := explorers.Task{
					ContainerType: "podman",
					Name:          id,
					PID:           containerstate.PID,
					Status:        containerstate.State.String(),
				}

				containerTasks = append(containerTasks, containerTask)
			}
			return nil
		}()
		if err != nil {
			return nil, err
		}
	}

	return containerTasks, nil
}

// InfoContainer returns container information similar to `docker inspect` output.
func (e *explorer) InfoContainer(ctx context.Context, containerID string, spec bool) (any, error) {
	log.Info("currently not implemented")

	return nil, nil
}

// MountContainer mounts podman container for a given ID or name.
func (e *explorer) MountContainer(ctx context.Context, containerID string, mountpoint string) error {
	podmanRootDirs, err := e.getPodmanRootDirs()
	if err != nil {
		return fmt.Errorf("getting podman directories: %w", err)
	}

	for _, podmanRootDir := range podmanRootDirs {
		configs, err := e.readContainerConfig(podmanRootDir)
		if err != nil {
			log.WithFields(log.Fields{"podmanRootDir": podmanRootDir, "error": err}).Debug("reading containers.json")
			continue
		}

		for _, config := range configs {
			if config.ID == containerID || (len(config.Names) > 0 && config.Names[0] == containerID) {
				return e.mountContainer(ctx, podmanRootDir, containerID, config.Layer, mountpoint)
			}
		}
	}

	return fmt.Errorf("no matching container")
}

// MountAllContainers mounts all podman containers.
func (e *explorer) MountAllContainers(ctx context.Context, mountpoint string, filter string, skipsupportcontainers bool) error {
	containers, err := e.ListContainers(ctx)
	if err != nil {
		return fmt.Errorf("listing container: %w", err)
	}

	for _, container := range containers {
		containerMountPoint := filepath.Join(mountpoint, container.ID)
		if err := os.MkdirAll(containerMountPoint, 0755); err != nil {
			return fmt.Errorf("creating a container mountpoint: %w", err)
		}

		containerName := container.Name
		if err := e.MountContainer(ctx, containerName, containerMountPoint); err != nil {
			return err
		}
	}

	return nil
}

// ContainerDrift finds the drifted files from containers.
//   - skipsupportcontainers are only applicable for containerd containers used in GKE. It is not used in Docker and podman.
//   - filter uses labels to filter the containers. `filter` is not used in podman containers.
func (e *explorer) ContainerDrift(ctx context.Context, filter string, skipsupportcontainers bool, containerID string) ([]explorers.Drift, error) {
	var drifts []explorers.Drift

	podmanRootDirs, err := e.getPodmanRootDirs()
	if err != nil {
		return nil, fmt.Errorf("getting podman root directories: %w", err)
	}

	for _, podmanRootDir := range podmanRootDirs {
		configs, err := e.readContainerConfig(podmanRootDir)
		if err != nil {
			log.WithFields(log.Fields{"podmanRootDir": podmanRootDir, "error": err}).Error("reading container config")
			continue
		}

		for _, config := range configs {
			// If containerID is supplied & doesn't match skip
			if containerID != "" && config.ID != containerID && (len(config.Names) == 0 || config.Names[0] != containerID) {
				continue
			}

			log.WithFields(log.Fields{"containerType": "podman", "containerID": config.ID}).Debug("checking container drift")

			// Get upperdir for Podman container
			overlayDir := filepath.Join(podmanRootDir, "storage", "overlay")
			layerDir := filepath.Join(overlayDir, config.Layer)

			linkFile := filepath.Join(layerDir, "link")
			linkData, err := os.ReadFile(linkFile)
			if err != nil {
				log.WithFields(log.Fields{"container": config.ID, "error": err}).Error("reading link file")
				continue
			}
			upperDir := filepath.Join(overlayDir, "l", string(linkData))
			log.WithFields(log.Fields{"containerType": "podman", "containerID": config.ID, "upperdir": upperDir}).Debug("checking upper layer for drift")

			// Scan upperdir
			addedOrModified, inaccessibleFiles, err := explorers.ScanDiffDirectory(upperDir)
			if err != nil {
				log.WithFields(log.Fields{"container": config.ID, "error": err}).Error("failed to scan diff directory")
				continue
			}

			drift := explorers.Drift{
				ContainerID:       config.ID,
				ContainerType:     "podman",
				AddedOrModified:   addedOrModified,
				InaccessibleFiles: inaccessibleFiles,
			}

			log.WithFields(log.Fields{
				"containerType":        drift.ContainerType,
				"containerID":          drift.ContainerID,
				"numAddedOrModified":   len(drift.AddedOrModified),
				"numInaccessibleFiles": len(drift.InaccessibleFiles),
			}).Debug("container drift detail")

			drifts = append(drifts, drift)
		}
	}

	return drifts, nil
}

func (e *explorer) Close() error {
	return nil
}

func (e *explorer) getUserHomeDirs() ([]string, error) {
	passwdFile := filepath.Join(e.imageroot, "etc", "passwd")
	data, err := os.ReadFile(passwdFile)
	if err != nil {
		return nil, fmt.Errorf("error reading passwd file: %w", err)
	}

	var homeDirs []string

	passwdLines := strings.Split(string(data), "\n")
	for _, passwdLine := range passwdLines {
		if passwdLine == "" {
			continue
		}
		// ubuntu:x:1000:1000:Ubuntu:/home/ubuntu:/bin/bash
		parts := strings.Split(passwdLine, ":")
		if len(parts) < 7 {
			log.WithField("line", passwdLine).Debug("skipping malformed passwd entry")
			continue
		}

		shell := parts[6]
		if !strings.HasSuffix(shell, "/bash") && !strings.HasSuffix(shell, "/sh") && !strings.HasSuffix(shell, "/zsh") && !strings.HasSuffix(shell, "/fish") {
			continue
		}

		if parts[5] == "" {
			log.WithField("line", passwdLine).Debug("passwd entry missing home directory")
			continue
		}

		homeDirs = append(homeDirs, parts[5])
	}

	return homeDirs, nil
}

func (e *explorer) getPodmanRootDirs() ([]string, error) {
	var podmanRootDirs []string

	// Podman containers in user directories
	usernames, err := e.getUserHomeDirs()
	if err != nil {
		log.WithError(err).Info("failed to list user home directories")
	} else {
		for _, username := range usernames {
			podmanroot := filepath.Join(e.imageroot, strings.Replace(username, "/", "", 1), defaultUserPodmanDir)
			ok := utils.PathExistsV2(podmanroot)
			if !ok {
				continue
			}
			podmanRootDirs = append(podmanRootDirs, podmanroot)
		}
	}

	// Podman containers in system directory
	systemPodmanRootDir := filepath.Join(e.imageroot, "var", "lib", "containers")
	if ok := utils.PathExistsV2(systemPodmanRootDir); ok {
		podmanRootDirs = append(podmanRootDirs, systemPodmanRootDir)
	}

	return podmanRootDirs, nil
}

func (e *explorer) readContainerConfig(podmanRootDir string) ([]containerConfig, error) {
	var configs []containerConfig

	containerDir := filepath.Join(podmanRootDir, "storage", "overlay-containers")
	configFile := filepath.Join(containerDir, "containers.json")

	configData, err := os.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("reading containers.json: %w", err)
	}

	if err := json.Unmarshal(configData, &configs); err != nil {
		return nil, fmt.Errorf("unmarshalling containers.json: %w", err)
	}

	return configs, nil
}

func (e *explorer) mountContainer(ctx context.Context, podmanRootDir string, containerID string, layer string, mountpoint string) error {
	overlayDir := filepath.Join(podmanRootDir, "storage", "overlay")
	layerDir := filepath.Join(overlayDir, layer)

	// Upperdir as link
	linkFile := filepath.Join(layerDir, "link")
	linkData, err := os.ReadFile(linkFile)
	if err != nil {
		return fmt.Errorf("reading link file: %w", err)
	}
	upperDir := filepath.Join(overlayDir, "l", string(linkData))

	// Lowerdir
	lowerFile := filepath.Join(layerDir, "lower")
	lowerData, err := os.ReadFile(lowerFile)
	if err != nil {
		return fmt.Errorf("reading lower file: %w", err)
	}

	log.WithFields(log.Fields{
		"containerID":   containerID,
		"podmanRootDir": podmanRootDir,
		"overlayDir":    overlayDir,
		"lowerdir":      string(lowerData),
		"upperdir":      string(linkData),
	}).Infof("container layers")

	var lowerDirs []string
	for _, lowerDir := range strings.Split(string(lowerData), ":") {
		lowerDirs = append(lowerDirs, filepath.Join(overlayDir, lowerDir))
	}
	lowerDir := strings.Join(lowerDirs, ":")

	// Linux mount options
	mountOpt := fmt.Sprintf("ro,lowerdir=%s:%s", upperDir, lowerDir)
	mountArgs := []string{"-t", "overlay", "overlay", "-o", mountOpt, mountpoint}

	cmd := exec.Command("mount", mountArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Infof("mount command: mount %s", strings.Join(mountArgs, " "))
		if string(out) != "" {
			return fmt.Errorf("running mount command: %w, output: %s", err, strings.TrimSpace(string(out)))
		}
		return fmt.Errorf("running mount command: %w", err)
	}
	if string(out) != "" {
		log.Infof("mount command output: %s", string(out))
	}

	return nil
}
