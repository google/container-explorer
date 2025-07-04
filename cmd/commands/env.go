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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/containerd/containerd/namespaces"
	"github.com/google/container-explorer/explorers"
	"github.com/google/container-explorer/explorers/containerd"
	"github.com/google/container-explorer/explorers/docker"
	"github.com/urfave/cli"

	log "github.com/sirupsen/logrus"
)

const (
	containerdRootDir = "/var/lib/containerd"
	dockerRootDir     = "/var/lib/docker"
)

// explorerEnvironment returns a ContainerExplorer interface.
// Containers managed using containerd and docker implement ContainerExplorer
// interface.
func explorerEnvironment(clictx *cli.Context) (context.Context, explorers.ContainerExplorer, func(), error) {
	ctx, cancel := context.WithCancel(context.Background())

	imageroot := clictx.GlobalString("image-root")
	containerdroot := clictx.GlobalString("containerd-root")
	dockerroot := clictx.GlobalString("docker-root")
	metadatafile := clictx.GlobalString("metadata-file")
	snapshotfile := clictx.GlobalString("snapshot-metadata-file")
	layercache := clictx.GlobalString("layer-cache")

	// Read support container data if provided using global switch.
	var sc *explorers.SupportContainer
	if clictx.GlobalString("support-container-data") != "" {
		var err error
		sc, err = explorers.NewSupportContainer(clictx.GlobalString("support-container-data"))
		if err != nil {
			log.Errorf("getting new support container: %v", err)
		}
	}

	// Handle docker managed containers.
	//
	// Use the global flag --docker-managed to specify container
	// managed using docker. This includes Kubernetes containers
	// managed using docker.
	if clictx.GlobalBool("docker-managed") {
		if dockerroot == "" && imageroot == "" {
			fmt.Printf("Missing required argument. Use --image-root or --docker-root\n")
			os.Exit(1)
		}

		if imageroot != "" && dockerroot == "" {
			dockerroot = filepath.Join(
				imageroot,
				strings.Replace(dockerRootDir, "/", "", 1),
			)
		}

		log.WithFields(log.Fields{
			"imageroot":      imageroot,
			"containerdroot": containerdroot,
			"dockerroot":     dockerroot,
			"manifestfile":   metadatafile,
			"snapshotfile":   snapshotfile,
			"sc":             &sc,
		}).Debug("docker container environment")

		de, _ := docker.NewExplorer(dockerroot, containerdroot, metadatafile, snapshotfile, sc)
		return ctx, de, func() {
			cancel()
		}, nil
	}

	// Handle containerd managed containers.
	//
	// The default is containerd managed containers. This includes
	// Kubernetes managed containers.
	if containerdroot == "" && imageroot == "" {
		fmt.Printf("Missing required arguments. Use --image-root or --containerd-root\n")
		os.Exit(1)
	}

	if imageroot != "" && containerdroot == "" {
		containerdroot = filepath.Join(
			imageroot,
			strings.Replace(containerdRootDir, "/", "", 1),
		)
	}

	if metadatafile == "" {
		metadatafile = filepath.Join(containerdroot, "io.containerd.metadata.v1.bolt", "meta.db")
	}

	log.WithFields(log.Fields{
		"imageroot":      imageroot,
		"containerdroot": containerdroot,
		"dockerroot":     dockerroot,
		"manifestfile":   metadatafile,
		"snapshotfile":   snapshotfile,
	}).Debug("containerd container environment")

	if !clictx.GlobalBool("use-layer-cache") {
		layercache = ""
	}
	cde, err := containerd.NewExplorer(imageroot, containerdroot, metadatafile, snapshotfile, layercache, sc)
	if err != nil {
		return ctx, nil, func() { cancel() }, err
	}
	return ctx, cde, func() {
		cancel()
	}, nil
}

func parseRuntimeConfig(clictx *cli.Context) (context.Context, map[string]interface{}, error) {
	// Global options
	namespace := clictx.GlobalString("namespace")
	imageRootDir := clictx.GlobalString("image-root")
	containerdRootDir := clictx.GlobalString("containerd-root")
	dockerRootDir := clictx.GlobalString("docker-root")
	metadataFile := clictx.GlobalString("metadata-file")
	snapshotFile := clictx.GlobalString("snapshot-metadata-file")
	layerCache := clictx.GlobalString("layer-cache")
	useLayerCache := clictx.GlobalBool("use-layer-cache")
	supportDataFile := clictx.GlobalString("support-container-data")

	ctx := context.Background()
	ctx = namespaces.WithNamespace(ctx, namespace)

	if imageRootDir == "" && containerdRootDir == "" && dockerRootDir == "" {
		return ctx, nil, fmt.Errorf("Missing required arguments. Use --image-root, --containerd-root or --docker-root")
	}

	if containerdRootDir == "" && imageRootDir != "" {
		 containerdRootDir = filepath.Join(imageRootDir, "var", "lib", "containerd")
	}

	if dockerRootDir == "" && imageRootDir != "" {
		dockerRootDir = filepath.Join(imageRootDir, "var", "lib", "docker")
	}

	if metadataFile == "" {
		metadataFile = filepath.Join(containerdRootDir, "io.containerd.metadata.v1.bolt", "meta.db")
	}

	if !useLayerCache {
		layerCache = ""
	}

	log.WithFields(log.Fields{
		"imageRootDir":      imageRootDir,
		"containerdRootDir": containerdRootDir,
		"dockerRootDir":     dockerRootDir,
		"metadataFile":   metadataFile,
		"snapshotFile":   snapshotFile,
		"layerCache":     layerCache,
		"useLayerCache":  useLayerCache,
		"supportDataFile": supportDataFile,
	}).Debug("container-explorer runtime configuration settings")

	runtimeConfig := make(map[string]interface{})
	runtimeConfig["namespace"] = namespace
	runtimeConfig["imageRootDir"] = imageRootDir
	runtimeConfig["containerdRootDir"] = containerdRootDir
	runtimeConfig["dockerRootDir"] = dockerRootDir
	runtimeConfig["metadataFile"] = metadataFile
	runtimeConfig["snapshotFile"] = snapshotFile
	runtimeConfig["layerCache"] = layerCache

	var err error
	var sc *explorers.SupportContainer
	if supportDataFile != "" {
		sc, err = explorers.NewSupportContainer(clictx.GlobalString("support-container-data"))
		if err != nil {
			log.Errorf("getting new support container: %v", err)
		}
	}
	runtimeConfig["supportContainer"] = sc

	return ctx, runtimeConfig, nil
}