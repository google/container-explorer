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

package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/metadata"
	"github.com/google/container-explorer/explorers"
	log "github.com/sirupsen/logrus"
	bolt "go.etcd.io/bbolt"
)

const (
	configV1Filename  = "config.json"
	configV2Filename  = "config.v2.json"
	containersDirName = "containers"
)

type explorer struct {
	root          string // docker root directory
	contaierdroot string
	manifest      string
	snapshot      string
	mdb           *bolt.DB // manifest database file
}

// NewExplorer returns a ContainerExplorer interface to explorer docker managed
// containers.
func NewExplorer(root string, containerdroot string, manifest string, snapshot string) (explorers.ContainerExplorer, error) {
	opt := &bolt.Options{
		ReadOnly: true,
	}
	db, err := bolt.Open(manifest, 0444, opt)
	if err != nil {
		return &explorer{}, err
	}

	return &explorer{
		root:          root,
		contaierdroot: containerdroot,
		manifest:      manifest,
		snapshot:      snapshot,
		mdb:           db,
	}, nil
}

// SnapshotRoot returns the snapshot root director for docker managed
// containers.
func (e *explorer) SnapshotRoot(snapshotter string) string {
	// TODO(rmaskey): implement the function
	return ""
}

// ListNamespaces returns namespaces for docker managed containers.
func (e *explorer) ListNamespaces(ctx context.Context) ([]string, error) {
	var nss []string

	// Namespaces in metadata file i.e. meta.db
	// in /var/lib/containerd/io.containerd.metadata.v1.bolt/meta.db
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

	// TODO(rmaskey): implement the function

	return nss, nil
}

// GetContainerIDs returns container ID
func (e *explorer) GetContainerIDs(ctx context.Context, containerdir string) ([]string, error) {
	containerpaths, err := filepath.Glob(filepath.Join(containerdir, "*"))
	if err != nil {
		return nil, err
	}

	var containerids []string
	for _, containerpath := range containerpaths {
		_, containerid := filepath.Split(containerpath)
		containerids = append(containerids, containerid)
	}
	return containerids, nil
}

// ListContainers returns container information.
func (e *explorer) ListContainers(ctx context.Context) ([]explorers.Container, error) {
	containersdir := filepath.Join(e.root, containersDirName)
	containerids, err := e.GetContainerIDs(ctx, containersdir)
	if err != nil {
		return nil, err
	}

	var cecontainers []explorers.Container

	for _, containerid := range containerids {
		containerpath := filepath.Join(containersdir, containerid)

		// Read docker config version 2
		configpath := filepath.Join(containerpath, configV2Filename)
		if fileExists(configpath) {
			data, err := ioutil.ReadFile(configpath)
			if err != nil {
				return nil, fmt.Errorf("reading docker config file %s %v", configV2Filename, err)
			}

			var config ConfigFile
			if err := json.Unmarshal(data, &config); err != nil {
				return nil, fmt.Errorf("unmarshalling config file data %v", err)
			}
			cecontainer := convertToContainerExplorerContainer(config)
			cecontainers = append(cecontainers, cecontainer)

			continue
		}

		// Read docker config version 1
		configpath = filepath.Join(containerpath, configV1Filename)
		if fileExists(configpath) {
			// TODO (rmaskey): parse v1 confg
			continue
		}

		log.WithFields(log.Fields{
			"containerid": containerid,
		}).Error("configuration file not found")
	}

	return cecontainers, nil
}

// ListImages returns container images.
func (e *explorer) ListImages(ctx context.Context) ([]explorers.Image, error) {
	// TODO(rmaskey): implement the function
	return nil, nil
}

// ListContent returns content information.
func (e *explorer) ListContent(ctx context.Context) ([]explorers.Content, error) {
	// TODO(rmaskey): implement the function
	return nil, nil
}

// ListSnapshots returns snapshot information.
func (e *explorer) ListSnapshots(ctx context.Context) ([]explorers.SnapshotKeyInfo, error) {
	// TODO(rmaskey): implement the function
	return nil, nil
}

// InfoContainer returns container internal information.
func (e *explorer) InfoContainer(ctx context.Context, containerid string, spec bool) (interface{}, error) {
	// default return
	return nil, nil
}

// MountContainer mounts a container to the specified path
func (e *explorer) MountContainer(ctx context.Context, containerid string, mountpoint string) error {
	return nil
}

// MountAllContainers mounts all the containers
func (e *explorer) MountAllContainers(ctx context.Context, mountpoint string, skipsupportcontainers bool) error {
	// default
	return nil
}

// Close releases internal resources.
func (e *explorer) Close() error {
	return e.mdb.Close()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// convertToContainerExplorerContainer maps docker config data to container
// explorer container structure
func convertToContainerExplorerContainer(config ConfigFile) explorers.Container {
	var exposedports []string

	if config.Config.ExposedPorts != nil {
		for k := range config.Config.ExposedPorts {
			exposedports = append(exposedports, k)
		}
	}

	return explorers.Container{
		Hostname: config.Config.Hostname,
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
		ExposedPorts: exposedports,
	}
}
