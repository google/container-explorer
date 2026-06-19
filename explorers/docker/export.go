/*
Copyright 2025 Google LLC

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
	"fmt"
	"os"

	"github.com/containerd/containerd/namespaces"
	"github.com/google/container-explorer/explorers"
	"github.com/google/container-explorer/utils"
	log "github.com/sirupsen/logrus"
)

// ExportContainer exports a container either as a raw image or an archive.
func (e *explorer) ExportContainer(ctx context.Context, containerID string, outputDir string, exportOptions map[string]bool) error {
	// Check if the specified containerID exists.
	containerExists := false

	containerNamespaces, err := e.ListNamespaces(ctx)
	if err != nil {
		return fmt.Errorf("listing namespaces: %w", err)
	}

	for _, containerNamespace := range containerNamespaces {
		ctx = namespaces.WithNamespace(ctx, containerNamespace)

		containers, err := e.ListContainers(ctx)
		if err != nil {
			log.WithFields(log.Fields{
				"namespace": containerNamespace,
				"error":     err,
			}).Warnf("error listing containers in namespace")

			continue
		}

		var targetContainer explorers.Container

		for _, container := range containers {
			if container.ID == containerID {
				targetContainer = container // Found the container
				containerExists = true
				break
			}
		}

		if !containerExists {
			log.WithFields(log.Fields{
				"containerID": containerID,
				"namespace":   containerNamespace,
			}).Debug("no container in namespace")

			continue
		}

		// Continue the following if a matching containerID is found.
		log.WithFields(log.Fields{
			"containerID":   targetContainer.ID,
			"name":          targetContainer.Runtime.Name,
			"namespace":     targetContainer.Namespace,
			"containerType": targetContainer.ContainerType,
		}).Info("container found")

		// Ensure outputDir exists
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return fmt.Errorf("failed to create output directory %s: %w", outputDir, err)
		}

		// Mount the container
		var mountpoint string
		for {
			mountpoint = utils.GetMountPoint()
			exists, _ := utils.PathExists(mountpoint)
			if !exists {
				// Create the mountpoint directory
				if err := os.MkdirAll(mountpoint, 0755); err != nil {
					return fmt.Errorf("failed to create mountpoint directory %s: %w", mountpoint, err)
				}
				break
			}
		}
		log.Infof("attempting to mount container %s to %s", targetContainer.ID, mountpoint)

		if err := e.MountContainer(ctx, targetContainer.ID, mountpoint); err != nil {
			// If mountpoint was created, attempt to clean it up.
			_ = os.Remove(mountpoint) // Best effort removal
			return fmt.Errorf("failed to mount container %s: %w", targetContainer.ID, err)
		}
		log.Infof("successfully mounted container %s to %s", targetContainer.ID, mountpoint)

		// Defer unmount and cleanup of the mountpoint
		defer func() {
			log.Infof("cleaning up mountpoint %s for container %s", mountpoint, targetContainer.ID)
			unmountCmdOutput, unmountErr := utils.Runner.RunWithoutContext("umount", mountpoint)
			if unmountErr != nil {
				log.Warnf("failed to unmount %s: %v; output: %s", mountpoint, unmountErr, string(unmountCmdOutput))
			} else {
				log.Infof("successfully unmounted %s; output: %s", mountpoint, string(unmountCmdOutput))
			}

			if rmErr := os.Remove(mountpoint); rmErr != nil {
				log.Warnf("failed to remove temporary mountpoint directory %s: %v", mountpoint, rmErr)
			} else {
				log.Infof("successfully removed mountpoint directory %s", mountpoint)
			}
		}()

		if exportOptions["image"] {
			log.Infof("exporting container %s as a raw image to %s", targetContainer.ID, outputDir)
			if err := utils.ExportContainerImage(ctx, targetContainer.ID, mountpoint, outputDir); err != nil {
				return fmt.Errorf("failed to export container %s as raw image: %w", targetContainer.ID, err)
			}
			log.Infof("successfully exported container %s as a raw image", targetContainer.ID)
		}

		if exportOptions["archive"] {
			log.Infof("exporting container %s as an archive to %s", targetContainer.ID, outputDir)
			if err := utils.ExportContainerArchive(ctx, targetContainer.ID, mountpoint, outputDir); err != nil {
				return fmt.Errorf("failed to export container %s as archive: %w", targetContainer.ID, err)
			}
			log.Infof("successfully exported container %s as an archive", targetContainer.ID)
		}
	}

	if !containerExists {
		log.Infof("container %s not found in docker containers", containerID)
	}

	return nil
}

// ExportAllContainers exports all Docker containers to specified output directory.
func (e *explorer) ExportAllContainers(ctx context.Context, outputDir string, exportOptions map[string]bool, filter map[string]string, exportSupportContainers bool) error {
	containerNamespaces, err := e.ListNamespaces(ctx)
	if err != nil {
		return fmt.Errorf("listing namespaces: %w", err)
	}

	for _, containerNamespace := range containerNamespaces {
		ctx = namespaces.WithNamespace(ctx, containerNamespace)

		containers, err := e.ListContainers(ctx)
		if err != nil {
			log.WithFields(log.Fields{
				"namespace": containerNamespace,
				"error":     err,
			}).Warnf("error listing containers in namespace")

			continue
		}
		log.WithFields(log.Fields{
			"namespace":      containerNamespace,
			"containerCount": len(containers),
		}).Debug("Docker containers in namespace")

		for _, container := range containers {
			log.WithFields(log.Fields{
				"containerID":   container.ID,
				"name":          container.Runtime.Name,
				"namespace":     container.Namespace,
				"containerType": container.ContainerType,
			}).Debug("processing Docker container for export")

			if !exportSupportContainers && container.SupportContainer {
				log.WithFields(log.Fields{
					"containerID":   container.ID,
					"name":          container.Runtime.Name,
					"namespace":     container.Namespace,
					"containerType": container.ContainerType,
				}).Debug("skipping Kubernetes support containers")
				continue
			}

			if utils.IncludeContainer(container, filter) {
				log.WithFields(log.Fields{
					"containerID":   container.ID,
					"name":          container.Runtime.Name,
					"namespace":     container.Namespace,
					"containerType": container.ContainerType,
				}).Debug("ignoring Docker container for export")

				err := e.ExportContainer(ctx, container.ID, outputDir, exportOptions)
				if err != nil {
					log.WithFields(log.Fields{
						"containerID":   container.ID,
						"name":          container.Runtime.Name,
						"namespace":     container.Namespace,
						"containerType": container.ContainerType,
						"error":         err,
					}).Error("error exporting Docker container")
				}
			}
		}
	}

	// Default
	return nil
}
