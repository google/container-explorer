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

	"github.com/google/container-explorer/explorers"
	"github.com/urfave/cli"

	log "github.com/sirupsen/logrus"
)

const (
	defaultContainerdRootDir = "/var/lib/containerd"
	defaultDockerRootDir     = "/var/lib/docker"
)

// parseRuntimeConfig parses container explorer runtime configuration and returns as a map.
func parseRuntimeConfig(clictx *cli.Context) (context.Context, map[string]any, error) {
	ctx := context.Background()

	imageRoot := clictx.GlobalString("image-root")
	containerdRoot := clictx.GlobalString("containerd-root")
	dockerRoot := clictx.GlobalString("docker-root")
	layercache := clictx.GlobalString("layer-cache")

	// Exit if image and container root directories are empty
	if imageRoot == "" && containerdRoot == "" && dockerRoot == "" {
		fmt.Printf("Missing required argument. Use --image-root or --containerd-root or --docker-root\n")
		os.Exit(1)
	}

	// Read support container data if provided using global switch.
	supportContainerFile := clictx.GlobalString("support-container-data")
	sc, err := explorers.NewSupportContainer(supportContainerFile)
	if err != nil {
		log.Errorf("getting new support container: %v", err)
	}

	// Handle docker managed containers.
	//
	// This includes Kubernetes containers managed using docker.
	if dockerRoot == "" {
		if imageRoot != "" {
			dockerRoot = filepath.Join(imageRoot, strings.Replace(defaultDockerRootDir, "/", "", 1))
		} else if containerdRoot != "" {
			parentDir := filepath.Dir(strings.TrimSuffix(containerdRoot, "/"))
			dockerRoot = filepath.Join(parentDir, "docker")
		}
	}

	// Handle containerd managed containers.
	//
	// The default is containerd managed containers. This includes
	// Kubernetes managed containers.
	if containerdRoot == "" {
		if imageRoot != "" {
			containerdRoot = filepath.Join(imageRoot, strings.Replace(defaultContainerdRootDir, "/", "", 1))
		} else if dockerRoot != "" {
			parentDir := filepath.Dir(strings.TrimSuffix(dockerRoot, "/"))
			containerdRoot = filepath.Join(parentDir, "containerd")
		}
	}

	if !clictx.GlobalBool("use-layer-cache") {
		layercache = ""
	}

	log.WithFields(log.Fields{
		"imageRoot":            imageRoot,
		"containerdRoot":       containerdRoot,
		"dockerRoot":           dockerRoot,
		"layercache":           layercache,
		"supportContainerData": sc,
	}).Debug("runtime configuration")

	runtimeConfig := make(map[string]any)
	runtimeConfig["imageRootDir"] = imageRoot
	runtimeConfig["containerdRootDir"] = containerdRoot
	runtimeConfig["dockerRootDir"] = dockerRoot
	runtimeConfig["podmanRootDir"] = ""
	runtimeConfig["layercache"] = layercache
	runtimeConfig["supportContainerData"] = sc

	return ctx, runtimeConfig, nil
}
