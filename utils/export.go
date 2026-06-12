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

// Package utils implements common utility functions used by Container Explorer
package utils

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
)

// ExportContainerImage creates a raw disk image file of a container.
func ExportContainerImage(ctx context.Context, containerID string, mountpoint string, outputDir string) error {
	var success bool
	imageFileName := fmt.Sprintf("%s.raw", containerID)
	imageFilePath := filepath.Join(outputDir, imageFileName)

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory %s: %w", outputDir, err)
	}

	defer func() {
		if !success {
			log.Infof("cleaning up incomplete image file: %s", imageFilePath)
			os.Remove(imageFilePath)
		}
	}()

	// 1. Calculate the required size for the image.
	contentSize, err := CalculateDirectorySize(mountpoint)
	if err != nil {
		return fmt.Errorf("failed to calculate content size for %s: %w", mountpoint, err)
	}
	log.Infof("calculated the content size of %s as %d bytes", mountpoint, contentSize)

	// Add overhead for filesystem structures (e.g., 20MB base + 5% of content size for inodes, metadata)
	overhead := int64(20*1024*1024) + (contentSize / 20)
	imageSize := contentSize + overhead

	log.Infof("preparing to create target disk %s of size %d bytes", imageFilePath, imageSize)

	// 2. Create the image file
	imgFile, err := os.Create(imageFilePath)
	if err != nil {
		return fmt.Errorf("failed to create image file %s: %w", imageFilePath, err)
	}

	// 3. Set the image file size
	if err := imgFile.Truncate(imageSize); err != nil {
		imgFile.Close()
		return fmt.Errorf("failed to truncate image file %s to size %d: %w", imageFilePath, imageSize, err)
	}

	// 4. Sync and Close the file before formatting
	if err := imgFile.Sync(); err != nil {
		log.Warnf("failed to sync image file %s after truncation: %v", imageFilePath, err)
	}
	if err := imgFile.Close(); err != nil {
		return fmt.Errorf("failed to close image file %s before formatting: %w", imageFilePath, err)
	}
	log.Infof("successfully created container %s target image file %s", containerID, imageFilePath)

	// 5. Format the image file as ext4
	mkfsCmd := exec.CommandContext(ctx, "mkfs.ext4", "-F", "-q", imageFilePath)
	mkfsOutput, err := mkfsCmd.CombinedOutput()
	if err != nil {
		log.WithFields(log.Fields{
			"command": mkfsCmd.String(),
			"output":  string(mkfsOutput),
			"error":   err,
		}).Error("formatting target image")
		return fmt.Errorf("mkfs.ext4 failed for %s: %w. Output: %s", imageFilePath, err, string(mkfsOutput))
	}
	log.Infof("successfully formatted image %s as ext4", imageFilePath)

	// 6. Mount the formatted image, copy data, then unmount.
	log.Infof("preparing to copy data from %s to image %s", mountpoint, imageFilePath)

	imageMountDir, err := os.MkdirTemp("", fmt.Sprintf("%s-img-mount-*.d", containerID))
	if err != nil {
		return fmt.Errorf("failed to create temporary mount directory for image %s: %w", imageFilePath, err)
	}
	log.Infof("created temporary image mount directory: %s", imageMountDir)

	var loopDevice string
	imageSuccessfullyMounted := false

	// Defer cleanup actions in LIFO order (unmount image, detach loop, remove temp dir)
	var unmounted bool
	defer func() {
		if imageSuccessfullyMounted {
			log.Infof("unmounting image from %s", imageMountDir)
			umountCmd := exec.Command("umount", imageMountDir) // Use non-contextual command for cleanup
			// Best effort unmount
			if umountErr := umountCmd.Run(); umountErr == nil {
				unmounted = true
				log.Infof("successfully unmounted image filesystem from %s", imageMountDir)
			} else {
				umountOutput, _ := umountCmd.CombinedOutput() // Get output for logging
				log.Warnf("failed to unmount image filesystem from %s: %v; output: %s", imageMountDir, umountErr, string(umountOutput))
				// Try lazy unmount
				log.Infof("attempting lazy unmount from %s", imageMountDir)
				lazyUmountCmd := exec.Command("umount", "-l", imageMountDir)
				if lazyErr := lazyUmountCmd.Run(); lazyErr == nil {
					unmounted = true
				} else {
					lazyOutput, _ := lazyUmountCmd.CombinedOutput()
					log.Warnf("lazy unmount also failed: %v; output: %s", lazyErr, string(lazyOutput))
				}
			}
		}

		if loopDevice != "" {
			log.Infof("detaching loop device %s for image %s", loopDevice, imageFilePath)
			losetupDetachCmd := exec.Command("losetup", "-d", loopDevice) // Use non-contextual command for cleanup
			// Best effort detach
			if detachErr := losetupDetachCmd.Run(); detachErr != nil {
				detachOutput, _ := losetupDetachCmd.CombinedOutput() // Get output for logging
				log.Warnf("failed to detach loop device %s: %v; output: %s", loopDevice, detachErr, string(detachOutput))
			} else {
				log.Infof("successfully detached loop device %s", loopDevice)
			}
		}

		if !imageSuccessfullyMounted || unmounted {
			log.Infof("removing temporary image mount directory %s", imageMountDir)
			if err := os.RemoveAll(imageMountDir); err != nil {
				log.Warnf("failed to remove temporary image mount directory %s: %v", imageMountDir, err)
			}
		} else {
			log.Warnf("skipping removal of temporary image mount directory %s because unmount failed", imageMountDir)
		}
	}()

	// 6.1. Setup loop device
	log.Infof("setting up loop device for %s", imageFilePath)
	var stdoutBuf, stderrBuf bytes.Buffer
	losetupCmd := exec.CommandContext(ctx, "losetup", "-f", "--show", imageFilePath)
	losetupCmd.Stdout = &stdoutBuf
	losetupCmd.Stderr = &stderrBuf
	err = losetupCmd.Run()
	if err != nil {
		log.Errorf("losetup -f --show %s failed: %v; stderr: %s", imageFilePath, err, stderrBuf.String())
		return fmt.Errorf("losetup -f --show %s failed: %w. Output: %s", imageFilePath, err, stderrBuf.String())
	}
	loopDevice = strings.TrimSpace(stdoutBuf.String())
	if loopDevice == "" {
		log.Errorf("losetup -f --show %s returned an empty loop device path", imageFilePath)
		return fmt.Errorf("losetup -f --show %s returned an empty loop device path", imageFilePath)
	}
	log.Infof("image %s associated with loop device %s", imageFilePath, loopDevice)

	// 6.2. Mount the loop device
	log.Infof("mounting loop device %s to %s", loopDevice, imageMountDir)
	mountImageCmd := exec.CommandContext(ctx, "mount", loopDevice, imageMountDir)
	mountImageOutput, err := mountImageCmd.CombinedOutput()
	if err != nil {
		log.Errorf("failed to mount %s to %s: %v; output: %s", loopDevice, imageMountDir, err, string(mountImageOutput))
		return fmt.Errorf("failed to mount loop device %s to %s: %w. Output: %s", loopDevice, imageMountDir, err, string(mountImageOutput))
	}
	imageSuccessfullyMounted = true // Set flag for deferred cleanup
	log.Infof("successfully mounted %s to %s; output: %s", loopDevice, imageMountDir, string(mountImageOutput))

	// 6.3. Copy content from container's mountpoint to the image's mountpoint
	// Source path: mountpoint + "/." to copy contents of the directory, including hidden files.
	log.Infof("copying contents from %s to %s using 'cp -a'", mountpoint, imageMountDir)

	//nolint:gosec // G204: Command arguments are constructed from verified mountpoints
	copyCmd := exec.CommandContext(ctx, "cp", "-a", filepath.Join(mountpoint, "."), imageMountDir)
	copyOutput, err := copyCmd.CombinedOutput()
	if err != nil {
		log.Errorf("failed to copy data from %s to %s: %v; output: %s", mountpoint, imageMountDir, err, string(copyOutput))
		return fmt.Errorf("failed to copy data from %s to %s: %w. Output: %s", mountpoint, imageMountDir, err, string(copyOutput))
	}
	log.Infof("successfully copied data from %s to %s; output: %s", mountpoint, imageMountDir, string(copyOutput))

	// 6.4. Sync filesystem buffers to ensure all data is written to the image
	log.Info("syncing filesystem buffers for the image")
	syncCmd := exec.CommandContext(ctx, "sync", "-f", imageMountDir)
	if syncErr := syncCmd.Run(); syncErr != nil {
		syncOutput, _ := syncCmd.CombinedOutput() // Get output for logging
		log.Warnf("sync command failed after copying to image: %v. Output: %s", syncErr, string(syncOutput))
	} else {
		log.Info("filesystem buffers synced")
	}

	log.Infof("image %s successfully created, formatted, and populated", imageFilePath)

	success = true
	return nil
}

// ExportContainerArchive creates a .tar.gz archive of the content of the mountpoint.
func ExportContainerArchive(ctx context.Context, containerID string, mountpoint string, outputDir string) error {
	var success bool
	archiveFileName := fmt.Sprintf("%s.tar.gz", containerID)
	archiveFilePath := filepath.Join(outputDir, archiveFileName)

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory %s: %w", outputDir, err)
	}

	defer func() {
		if !success {
			log.Infof("cleaning up incomplete archive file: %s", archiveFilePath)
			os.Remove(archiveFilePath)
		}
	}()

	log.WithFields(log.Fields{
		"containerID":     containerID,
		"mountpoint":      mountpoint,
		"archiveFilePath": archiveFilePath,
	}).Debug("preparing to create container archive")

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
	}).Debug("successfully created container archive")

	success = true
	return nil
}
