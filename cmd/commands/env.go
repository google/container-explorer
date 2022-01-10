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

package commands

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/google/container-explorer/explorers"
	"github.com/google/container-explorer/explorers/containerd"
	"github.com/google/container-explorer/explorers/docker"
	"github.com/urfave/cli"

	log "github.com/sirupsen/logrus"
)

const (
	containerdMetadataFilename = "meta.db"
	containerdMetadataDir      = "io.containerd.metadata"
)

// explorerEnvironment returns a ContainerExplorer interface.
// Containers managed using containerd and docker implement ContainerExplorer
// interface.
func explorerEnvironment(clictx *cli.Context) (context.Context, explorers.ContainerExplorer, func(), error) {
	ctx, cancel := context.WithCancel(context.Background())

	imageroot := clictx.GlobalString("image-root")
	metadatafile := clictx.GlobalString("metadata-file")
	snapshotfile := clictx.GlobalString("snapshot-metadata-file")

	// Computes containerdroot based on the global flags --image-root
	// and --containerd-root
	containerdroot := clictx.GlobalString("containerd-root")
	if imageroot != "" {
		containerdroot = filepath.Join(
			imageroot,
			strings.Replace(containerdroot, "/", "", 1),
		)
	}

	// Computes metadata file i.e. meta.db path based on the global flags
	// --image-root, --containerd-root, and --manifest-file.
	//
	// The containerd implementation requires the meta.db file.
	// The docker implementation may use the meta.db if exists.
	if metadatafile == "" {
		dirs, err := filepath.Glob(filepath.Join(containerdroot, "*"))
		if err != nil {
			return ctx, nil, func() { cancel() }, err
		}
		for _, d := range dirs {
			if strings.Contains(d, containerdMetadataDir) {
				metadatafile = filepath.Join(d, containerdMetadataFilename)
				break
			}
		}
	}

	// Computes the snapshot medata file i.e. metadata.db that contains
	// crucial information about the overlay file system layers.
	//
	// The containerd implementation requires the metadata.db file to compute
	// the overlay file system layers. The default location of the metadata.db
	// for containerd is /var/lib/containerd/io.containerd.snapshotter.v1.overlay/metadata.db
	if snapshotfile == "" {
		snapshotfile = filepath.Join(containerdroot, "io.containerd.snapshotter.v1.overlayfs", "metadata.db")
	}

	// Handle docker managed containers.
	//
	// Use the global flag --docker-managed to specify container
	// managed using docker. This includes Kubernetes containers
	// managed using docker.
	if clictx.GlobalBool("docker-managed") {
		dockerroot := clictx.GlobalString("docker-root")

		if imageroot != "" {
			dockerroot = filepath.Join(
				imageroot,
				strings.Replace(dockerroot, "/", "", 1),
			)
		}

		log.WithFields(log.Fields{
			"imageroot":      imageroot,
			"containerdroot": containerdroot,
			"dockerroot":     dockerroot,
			"manifestfile":   metadatafile,
			"snapshotfile":   snapshotfile,
		}).Debug("docker container environment")

		de, _ := docker.NewExplorer(dockerroot, containerdroot, metadatafile, snapshotfile)
		return ctx, de, func() {
			cancel()
		}, nil
	}

	// Handle containerd managed containers.
	//
	// The default is containerd managed containers. This includes
	// Kubernetes managed containers.
	log.WithFields(log.Fields{
		"imageroot":      imageroot,
		"containerdroot": containerdroot,
		"manifestfile":   metadatafile,
		"snapshotfile":   snapshotfile,
	}).Debug("containerd container environment")

	cde, err := containerd.NewExplorer(containerdroot, metadatafile, snapshotfile)
	if err != nil {
		return ctx, nil, func() { cancel() }, err
	}
	return ctx, cde, func() {
		cancel()
	}, nil
}
