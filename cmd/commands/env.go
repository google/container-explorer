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
	"github.com/urfave/cli"

	log "github.com/sirupsen/logrus"
)

const (
	defaultContainerdRootDir = "/var/lib/containerd"
	defaultDockerRootDir     = "/var/lib/docker"
)

// RuntimeConfig holds the global configuration for container-explorer.
type RuntimeConfig struct {
	Context               context.Context
	ImageRootDir          string
	ContainerdRootDir     string
	DockerRootDir         string
	PodmanRootDir         string
	LayerCache            string
	SupportContainerData  *explorers.SupportContainer
	Output                string
	OutputFile            string
	Debug                 bool
}

// GlobalConfig is the package-level configuration object.
var GlobalConfig RuntimeConfig

// InitializeRuntime sets up the global configuration from the CLI context.
func InitializeRuntime(clictx *cli.Context) error {
	GlobalConfig.Context = context.Background()
	GlobalConfig.Debug = clictx.GlobalBool("debug")
	GlobalConfig.ImageRootDir = clictx.GlobalString("image-root")
	GlobalConfig.ContainerdRootDir = clictx.GlobalString("containerd-root")
	GlobalConfig.DockerRootDir = clictx.GlobalString("docker-root")
	GlobalConfig.LayerCache = clictx.GlobalString("layer-cache")
	GlobalConfig.Output = clictx.GlobalString("output")
	GlobalConfig.OutputFile = clictx.GlobalString("output-file")

	// Read support container data if provided.
	supportContainerFile := clictx.GlobalString("support-container-data")
	sc, err := explorers.NewSupportContainer(supportContainerFile)
	if err != nil {
		log.Errorf("getting new support container: %v", err)
	}
	GlobalConfig.SupportContainerData = sc

	// Handle docker managed containers root.
	if GlobalConfig.DockerRootDir == "" {
		if GlobalConfig.ImageRootDir != "" {
			GlobalConfig.DockerRootDir = filepath.Join(GlobalConfig.ImageRootDir, strings.Replace(defaultDockerRootDir, "/", "", 1))
		} else if GlobalConfig.ContainerdRootDir != "" {
			parentDir := filepath.Dir(strings.TrimSuffix(GlobalConfig.ContainerdRootDir, "/"))
			GlobalConfig.DockerRootDir = filepath.Join(parentDir, "docker")
		}
	}

	// Handle containerd managed containers root.
	if GlobalConfig.ContainerdRootDir == "" {
		if GlobalConfig.ImageRootDir != "" {
			GlobalConfig.ContainerdRootDir = filepath.Join(GlobalConfig.ImageRootDir, strings.Replace(defaultContainerdRootDir, "/", "", 1))
		} else if GlobalConfig.DockerRootDir != "" {
			parentDir := filepath.Dir(strings.TrimSuffix(GlobalConfig.DockerRootDir, "/"))
			GlobalConfig.ContainerdRootDir = filepath.Join(parentDir, "containerd")
		}
	}

	if !clictx.GlobalBool("use-layer-cache") {
		GlobalConfig.LayerCache = ""
	}

	log.WithFields(log.Fields{
		"imageRoot":            GlobalConfig.ImageRootDir,
		"containerdRoot":       GlobalConfig.ContainerdRootDir,
		"dockerRoot":           GlobalConfig.DockerRootDir,
		"layercache":           GlobalConfig.LayerCache,
		"supportContainerData": GlobalConfig.SupportContainerData,
		"debug":                GlobalConfig.Debug,
	}).Debug("runtime configuration initialized")

	return nil
}

// parseRuntimeConfig is kept for backward compatibility during migration.
// It now returns the GlobalConfig values.
func parseRuntimeConfig(clictx *cli.Context) (context.Context, map[string]any, error) {
	// If GlobalConfig hasn't been initialized (e.g. if Before hook wasn't called), initialize it now.
	// This ensures existing calls still work reliably.
	if GlobalConfig.Context == nil {
		if err := InitializeRuntime(clictx); err != nil {
			return nil, nil, err
		}
	}

	runtimeConfig := make(map[string]any)
	runtimeConfig["imageRootDir"] = GlobalConfig.ImageRootDir
	runtimeConfig["containerdRootDir"] = GlobalConfig.ContainerdRootDir
	runtimeConfig["dockerRootDir"] = GlobalConfig.DockerRootDir
	runtimeConfig["podmanRootDir"] = ""
	runtimeConfig["layercache"] = GlobalConfig.LayerCache
	runtimeConfig["supportContainerData"] = GlobalConfig.SupportContainerData

	return GlobalConfig.Context, runtimeConfig, nil
}
