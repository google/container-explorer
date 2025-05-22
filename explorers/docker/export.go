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
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/containerd/containerd/namespaces"
	"github.com/google/container-explorer/explorers"
	"github.com/google/container-explorer/utils"
	log "github.com/sirupsen/logrus"
)

// ExportContainer exports a container either as a raw image or an archive.
func (e *explorer) ExportContainer(ctx context.Context, containerID string, outputDir string, exportOption map[string]bool) error {
	// Check if the specified containerID exists.
	containerExists := false

	containers, err := e.ListContainers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
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
		return fmt.Errorf("container %s does not exist", containerID)
	}

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
	log.Infof("Attempting to mount container %s to %s", targetContainer.ID, mountpoint)

	if err := e.MountContainer(ctx, targetContainer.ID, mountpoint); err != nil {
		// If mountpoint was created, attempt to clean it up.
		_ = os.Remove(mountpoint) // Best effort removal
		return fmt.Errorf("failed to mount container %s: %w", targetContainer.ID, err)
	}
	log.Infof("Successfully mounted container %s to %s", targetContainer.ID, mountpoint)

	// Defer unmount and cleanup of the mountpoint
	defer func() {
		log.Infof("Cleaning up mountpoint %s for container %s", mountpoint, targetContainer.ID)
		unmountCmd := exec.Command("umount", mountpoint)
		unmountCmdOutput, unmountErr := unmountCmd.CombinedOutput() // Run and get output/error
		if unmountErr != nil {
			log.Warnf("Failed to unmount %s: %v. Output: %s", mountpoint, unmountErr, string(unmountCmdOutput))
		} else {
			log.Infof("Successfully unmounted %s. Output: %s", mountpoint, string(unmountCmdOutput))
		}

		if rmErr := os.Remove(mountpoint); rmErr != nil {
			log.Warnf("Failed to remove temporary mountpoint directory %s: %v", mountpoint, rmErr)
		} else {
			log.Infof("Successfully removed mountpoint directory %s", mountpoint)
		}
	}()

	if exportOption["image"] {
		log.Infof("Exporting container %s as a raw image to %s", targetContainer.ID, outputDir)
		if err := exportContainerImage(ctx, targetContainer.ID, mountpoint, outputDir); err != nil {
			return fmt.Errorf("failed to export container %s as raw image: %w", targetContainer.ID, err)
		}
		log.Infof("Successfully exported container %s as a raw image.", targetContainer.ID)
	}

	if exportOption["archive"] {
		log.Infof("Exporting container %s as an archive to %s", targetContainer.ID, outputDir)
		if err := exportContainerArchive(ctx, targetContainer.ID, mountpoint, outputDir); err != nil {
			return fmt.Errorf("failed to export container %s as archive: %w", targetContainer.ID, err)
		}
		log.Infof("Successfully exported container %s as an archive.", targetContainer.ID)
	}

	return nil
}

// ExportAllContainers exports all Docker containers to specified output directory.
func (e *explorer) ExportAllContainers(ctx context.Context, outputDir string, exportOption map[string]bool, filter map[string]string, exportSupportContainers bool) error {
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
				"error": err,
			}).Warnf("error listing containers in namespace")
			continue
		}

		for _, container := range containers {
			log.WithFields(log.Fields{
				"containerID": container.ID,
				"name": container.Runtime.Name,
				"namespace": container.Namespace,
				"containerType": container.ContainerType,
			}).Debug("processing Docker container for export")

			if !exportSupportContainers && container.SupportContainer{
				log.WithFields(log.Fields{
					"containerID": container.ID,
					"name": container.Runtime.Name,
					"namespace": container.Namespace,
					"containerType": container.ContainerType,
				}).Debug("skipping Kubernetes support containers")
				continue
			}

			if utils.IgnoreContainer(container, filter) {
				log.WithFields(log.Fields{
					"containerID": container.ID,
					"name": container.Runtime.Name,
					"namespace": container.Namespace,
					"containerType": container.ContainerType,
				}).Debug("ignoring Docker container for export")
				continue
			}

			err := e.ExportContainer(ctx, container.ID, outputDir, exportOption)
			if err != nil {
				log.WithFields(log.Fields{
					"containerID": container.ID,
					"name": container.Runtime.Name,
					"namespace": container.Namespace,
					"containerType": container.ContainerType,
					"error": err,
				}).Error("error exporting Docker container")
			}
		}
	}

	// Default
	return nil
}

// exportContainerImage creates a raw disk image file of a calculated size based on
// the content of the mountpoint, formats it to ext4, and saves it to outputDir.
func exportContainerImage(ctx context.Context, containerID string, mountpoint string, outputDir string) error {
	// 1. Calculate the required size for the image.
	contentSize, err := utils.CalculateDirectorySize(mountpoint)
	if err != nil {
		return fmt.Errorf("failed to calculate content size for %s: %w", mountpoint, err)
	}
	log.Infof("Calculated content size for %s: %d bytes", mountpoint, contentSize)

	// Add overhead for filesystem structures (e.g., 20MB base + 5% of content size for inodes, metadata)
	overhead := int64(20*1024*1024) + (contentSize / 20)
	imageSize := contentSize + overhead
	log.Infof("Target image size for %s: %d bytes (content: %d, overhead: %d)", containerID, imageSize, contentSize, overhead)

	imageFileName := fmt.Sprintf("%s.img", containerID)
	imageFilePath := filepath.Join(outputDir, imageFileName)

	log.WithFields(log.Fields{
		"containerID":   containerID,
		"imageFilePath": imageFilePath,
		"imageSize":     imageSize,
	}).Info("Preparing to create and format disk image")

	// 2. Create the image file
	imgFile, err := os.Create(imageFilePath)
	if err != nil {
		return fmt.Errorf("failed to create image file %s: %w", imageFilePath, err)
	}

	// 3. Set the image file size
	if err := imgFile.Truncate(imageSize); err != nil {
		imgFile.Close() // Attempt to close before returning
		return fmt.Errorf("failed to truncate image file %s to size %d: %w", imageFilePath, imageSize, err)
	}

	// 4. Sync and Close the file before formatting
	if err := imgFile.Sync(); err != nil {
		imgFile.Close() // Attempt to close before returning
		log.Warnf("failed to sync image file %s after truncation: %v", imageFilePath, err)
	}
	if err := imgFile.Close(); err != nil {
		return fmt.Errorf("failed to close image file %s before formatting: %w", imageFilePath, err)
	}
	log.Infof("Successfully created and sized image file: %s", imageFilePath)

	// 5. Format the image file as ext4
	log.WithFields(log.Fields{
		"imageFilePath": imageFilePath,
	}).Info("Formatting image as ext4...")

	mkfsCmd := exec.CommandContext(ctx, "mkfs.ext4", "-F", "-q", imageFilePath)
	mkfsOutput, err := mkfsCmd.CombinedOutput()
	if err != nil {
		log.WithFields(log.Fields{
			"command": mkfsCmd.String(),
			"output":  string(mkfsOutput),
			"error":   err,
		}).Error("mkfs.ext4 command failed")
		return fmt.Errorf("mkfs.ext4 failed for %s: %w. Output: %s", imageFilePath, err, string(mkfsOutput))
	}

	log.WithFields(log.Fields{
		"imageFilePath": imageFilePath,
		"output":        string(mkfsOutput),
	}).Info("Successfully formatted image as ext4")

	// 6. Mount the formatted image, copy data, then unmount.
	log.Infof("Preparing to copy data from %s to image %s", mountpoint, imageFilePath)

	imageMountDir, err := os.MkdirTemp(outputDir, fmt.Sprintf("%s-img-mount-*.d", containerID))
	if err != nil {
		return fmt.Errorf("failed to create temporary mount directory for image %s: %w", imageFilePath, err)
	}
	log.Infof("Created temporary image mount directory: %s", imageMountDir)

	var loopDevice string
	var imageSuccessfullyMounted bool = false

	// Defer cleanup actions in LIFO order (unmount image, detach loop, remove temp dir)
	defer func() {
		if imageSuccessfullyMounted {
			log.Infof("Unmounting image from %s", imageMountDir)
			umountCmd := exec.Command("umount", imageMountDir) // Use non-contextual command for cleanup
			// Best effort unmount
			if umountErr := umountCmd.Run(); umountErr != nil {
				umountOutput, _ := umountCmd.CombinedOutput() // Get output for logging
				log.Warnf("Failed to unmount image filesystem from %s: %v. Output: %s", imageMountDir, umountErr, string(umountOutput))
			} else {
				log.Infof("Successfully unmounted image filesystem from %s", imageMountDir)
			}
		}

		if loopDevice != "" {
			log.Infof("Detaching loop device %s for image %s", loopDevice, imageFilePath)
			losetupDetachCmd := exec.Command("losetup", "-d", loopDevice) // Use non-contextual command for cleanup
			// Best effort detach
			if detachErr := losetupDetachCmd.Run(); detachErr != nil {
				detachOutput, _ := losetupDetachCmd.CombinedOutput() // Get output for logging
				log.Warnf("Failed to detach loop device %s: %v. Output: %s", loopDevice, detachErr, string(detachOutput))
			} else {
				log.Infof("Successfully detached loop device %s", loopDevice)
			}
		}

		log.Infof("Removing temporary image mount directory %s", imageMountDir)
		if err := os.RemoveAll(imageMountDir); err != nil {
			log.Warnf("Failed to remove temporary image mount directory %s: %v", imageMountDir, err)
		}
	}()

	// 6.1. Setup loop device
	log.Infof("Setting up loop device for %s", imageFilePath)
	losetupCmd := exec.CommandContext(ctx, "losetup", "-f", "--show", imageFilePath)
	loopDeviceBytes, err := losetupCmd.Output() // Use Output to capture stdout, which is the loop device path
	if err != nil {
		// If Output() fails, CombinedOutput() can give more info if stderr was involved
		losetupCombinedOutput, _ := exec.CommandContext(ctx, "losetup", "-f", "--show", imageFilePath).CombinedOutput()
		log.Errorf("losetup -f --show %s failed: %v. Output: %s", imageFilePath, err, string(losetupCombinedOutput))
		return fmt.Errorf("losetup -f --show %s failed: %w. Output: %s", imageFilePath, err, string(losetupCombinedOutput))
	}
	loopDevice = strings.TrimSpace(string(loopDeviceBytes))
	if loopDevice == "" {
		log.Errorf("losetup -f --show %s returned an empty loop device path.", imageFilePath)
		return fmt.Errorf("losetup -f --show %s returned an empty loop device path", imageFilePath)
	}
	log.Infof("Image %s associated with loop device %s", imageFilePath, loopDevice)

	// 6.2. Mount the loop device
	log.Infof("Mounting loop device %s to %s", loopDevice, imageMountDir)
	mountImageCmd := exec.CommandContext(ctx, "mount", loopDevice, imageMountDir)
	mountImageOutput, err := mountImageCmd.CombinedOutput()
	if err != nil {
		log.Errorf("Failed to mount %s to %s: %v. Output: %s", loopDevice, imageMountDir, err, string(mountImageOutput))
		return fmt.Errorf("failed to mount loop device %s to %s: %w. Output: %s", loopDevice, imageMountDir, err, string(mountImageOutput))
	}
	imageSuccessfullyMounted = true // Set flag for deferred cleanup
	log.Infof("Successfully mounted %s to %s. Output: %s", loopDevice, imageMountDir, string(mountImageOutput))

	// 6.3. Copy content from container's mountpoint to the image's mountpoint
	// Source path: mountpoint + "/." to copy contents of the directory, not the directory itself.
	sourcePathFiles, _ := filepath.Glob(filepath.Join(mountpoint, "*"))

	for _, sourcePathForCopy := range sourcePathFiles {
		log.Infof("Copying contents from %s to %s using 'cp -a'", sourcePathForCopy, imageMountDir)

		copyCmd := exec.Command("cp", "-a", sourcePathForCopy, imageMountDir)
		copyOutput, err := copyCmd.CombinedOutput()
		if err != nil {
			log.Errorf("Failed to copy data from %s to %s: %v. Output: %s", sourcePathForCopy, imageMountDir, err, string(copyOutput))
			return fmt.Errorf("failed to copy data from %s to %s: %w. Output: %s", sourcePathForCopy, imageMountDir, err, string(copyOutput))
		}
		log.Infof("Successfully copied data from %s to %s. Output: %s", sourcePathForCopy, imageMountDir, string(copyOutput))
	}

	// 6.4. Sync filesystem buffers to ensure all data is written to the image
	log.Info("Syncing filesystem buffers for the image.")
	syncCmd := exec.CommandContext(ctx, "sync")
	if syncErr := syncCmd.Run(); syncErr != nil {
		// This is usually not fatal but good to log.
		syncOutput, _ := syncCmd.CombinedOutput() // Get output for logging
		log.Warnf("sync command failed after copying to image: %v. Output: %s", syncErr, string(syncOutput))
	} else {
		log.Info("Filesystem buffers synced.")
	}

	log.Infof("Image %s successfully created, formatted, and populated.", imageFilePath)

	return nil
}

// exportContainerArchive creates a .tar.gz archive of the content of the mountpoint.
func exportContainerArchive(ctx context.Context, containerID string, mountpoint string, outputDir string) error {
	archiveFileName := fmt.Sprintf("%s.tar.gz", containerID)
	archiveFilePath := filepath.Join(outputDir, archiveFileName)

	log.WithFields(log.Fields{
		"containerID":     containerID,
		"mountpoint":      mountpoint,
		"archiveFilePath": archiveFilePath,
	}).Info("Preparing to create container archive")

	// Command: tar -czf <archiveFilePath> -C <mountpoint> .
	// -c: create
	// -z: gzip
	// -f: file
	// -C <dir>: change to directory <dir> before processing files
	// .: process all files in the current directory (which is <mountpoint> due to -C)
	tarCmd := exec.CommandContext(ctx, "tar", "-czf", archiveFilePath, "-C", mountpoint, ".")

	tarOutput, err := tarCmd.CombinedOutput()
	if err != nil {
		log.WithFields(log.Fields{
			"command": tarCmd.String(),
			"output":  string(tarOutput),
			"error":   err,
		}).Error("tar command failed")
		return fmt.Errorf("failed to create archive %s: %w. Output: %s", archiveFilePath, err, string(tarOutput))
	}

	log.WithFields(log.Fields{
		"archiveFilePath": archiveFilePath,
		"output":          string(tarOutput),
	}).Info("Successfully created container archive")

	return nil
}
