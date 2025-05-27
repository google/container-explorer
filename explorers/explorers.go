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

package explorers

import (
	"context"
)

// ContainerExplorer defines the methods required to explore a container.
type ContainerExplorer interface {
	// Close releases the internal resources
	Close() error

	// ContainerDrift identifies container filesystem changes
	ContainerDrift(ctx context.Context, filter string, skipsupportcontainers bool, containerID string) ([]Drift, error)

	// ExportAllContainers exports all Docker and containerd containers.
	ExportAllContainers(ctx context.Context, outputDir string, exportOption map[string]bool, filter map[string]string, exportSupportContainers bool) error

	// ExportContainer exports a container as an image or archive.
	ExportContainer(ctx context.Context, containerID string, outputDir string, exportOption map[string]bool) error

	// InfoContainer returns container internal information
	InfoContainer(ctx context.Context, containerid string, spec bool) (interface{}, error)

	// ListContainers returns all the containers in all the namespaces.
	//
	// ListContainers returns the ContainerExplorer's Containers structure
	// that holds additional information about the containers.
	ListContainers(ctx context.Context) ([]Container, error)

	// ListContent returns information about content
	ListContent(ctx context.Context) ([]Content, error)

	// ListImages returns content information
	ListImages(ctx context.Context) ([]Image, error)

	// ListNamespaces returns all the namespaces in the metadata file i.e.
	// meta.db
	ListNamespaces(ctx context.Context) ([]string, error)

	// ListSnapshots returns the snapshot information
	ListSnapshots(ctx context.Context) ([]SnapshotKeyInfo, error)

	// ListTasks returns the container task status
	ListTasks(ctx context.Context) ([]Task, error)

	// MountAllContainer mounts all containers to the specfied path
	MountAllContainers(ctx context.Context, mountpoint string, filter string, skipsupportcontainers bool) error

	// MountContainer mounts a container to the specified path
	MountContainer(ctx context.Context, containerid string, mountpoint string) error

	// SnapshotRoot returns the directory containing snapshots and snapshot
	// database i.e. metadata.db
	//
	// SnapshotRoot is required for the containers managed using containerd.
	SnapshotRoot(snapshotter string) string
}
