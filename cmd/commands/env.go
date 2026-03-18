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
func parseRuntimeConfig(clictx *cli.Context) (context.Context, map[string]interface{}, error) {
	ctx := context.Background()

	imageroot := clictx.GlobalString("image-root")
	containerdroot := clictx.GlobalString("containerd-root")
	dockerroot := clictx.GlobalString("docker-root")
	layercache := clictx.GlobalString("layer-cache")

	// Exit if image and container root directories are empty
	if imageroot == "" && containerdroot == "" && dockerroot == "" {
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
	if dockerroot == "" {
		if imageroot != "" {
			dockerroot = filepath.Join(imageroot, strings.Replace(defaultDockerRootDir, "/", "", 1))
		} else if containerdroot != "" {
			parentDir := filepath.Dir(strings.TrimSuffix(containerdroot, "/"))
			dockerroot = filepath.Join(parentDir, "docker")
		}
	}

	log.WithFields(log.Fields{
		"imageroot":      imageroot,
		"containerdroot": containerdroot,
		"dockerroot":     dockerroot,
		"sc":             &sc,
	}).Debug("docker container environment")

	// Handle containerd managed containers.
	//
	// The default is containerd managed containers. This includes
	// Kubernetes managed containers.
	if containerdroot == "" {
		if imageroot != "" {
			containerdroot = filepath.Join(imageroot, strings.Replace(defaultContainerdRootDir, "/", "", 1))
		} else if dockerroot != "" {
			parentDir := filepath.Dir(strings.TrimSuffix(dockerroot, "/"))
			containerdroot = filepath.Join(parentDir, "containerd")
		}
	}

	log.WithFields(log.Fields{
		"imageroot":      imageroot,
		"containerdroot": containerdroot,
		"dockerroot":     dockerroot,
	}).Debug("containerd container environment")

	if !clictx.GlobalBool("use-layer-cache") {
		layercache = ""
	}

	runtimeConfig := make(map[string]interface{})
	runtimeConfig["imageRootDir"] = imageroot
	runtimeConfig["containerdRootDir"] = containerdroot
	runtimeConfig["dockerRootDir"] = dockerroot
	runtimeConfig["podmanRootDir"] = ""
	runtimeConfig["layercache"] = layercache
	runtimeConfig["supportContainerData"] = sc

	return ctx, runtimeConfig, nil
}

