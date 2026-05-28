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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/container-explorer/explorers"
	"github.com/urfave/cli"

	containerdConfig "github.com/containerd/containerd/services/server/config"
	dockerConfig "github.com/docker/docker/daemon/config"
	log "github.com/sirupsen/logrus"
)

const (
	defaultContainerdRootDir = "/var/lib/containerd"
	defaultDockerRootDir     = "/var/lib/docker"
)

// RuntimeConfig holds the global configuration for container-explorer.
type RuntimeConfig struct {
	Context              context.Context
	ImageRootDir         string
	ContainerdRootDir    string
	DockerRootDir        string
	PodmanRootDir        string
	LayerCache           string
	SupportContainerData *explorers.SupportContainer
	Output               string
	OutputFile           string
	Debug                bool
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
			dockerDataDir := getDockerDataRoot(GlobalConfig.ImageRootDir)
			GlobalConfig.DockerRootDir = filepath.Join(GlobalConfig.ImageRootDir, strings.Replace(dockerDataDir, "/", "", 1))
		} else if GlobalConfig.ContainerdRootDir != "" {
			parentDir := filepath.Dir(strings.TrimSuffix(GlobalConfig.ContainerdRootDir, "/"))
			GlobalConfig.DockerRootDir = filepath.Join(parentDir, "docker")
		}
	}

	// Handle containerd managed containers root.
	if GlobalConfig.ContainerdRootDir == "" {
		if GlobalConfig.ImageRootDir != "" {
			containerdDataDir := getContainerdDataDir(GlobalConfig.ImageRootDir)
			GlobalConfig.ContainerdRootDir = filepath.Join(GlobalConfig.ImageRootDir, strings.Replace(containerdDataDir, "/", "", 1))
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

// getDockerDataRoot returns Docker data-root directory.
// Returns custom path if configured, otherwise returns the default path.
func getDockerDataRoot(imageRootDir string) string {
	configPath := filepath.Join(imageRootDir, "etc", "docker", "daemon.json")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.WithFields(log.Fields{"configPath": configPath, "error": err}).Debug("reading docker config")
		return defaultDockerRootDir
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		log.WithFields(log.Fields{"configPath": configPath, "error": err}).Debug("reading docker config file")
		return defaultDockerRootDir
	}

	var cfg dockerConfig.Config

	err = json.Unmarshal(data, &cfg)
	if err != nil {
		log.WithFields(log.Fields{"configPath": configPath, "error": err}).Debug("unmarshalling docker config")
		return defaultDockerRootDir
	}

	if cfg.Root == "" {
		return defaultDockerRootDir
	}

	return cfg.Root
}

// getContainerDataDir returns containerd root directory.
// Returns custom path if configured, otherwise returns the default path.
func getContainerdDataDir(imageRootDir string) string {
	configPath := filepath.Join(imageRootDir, "etc", "containerd", "config.toml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.WithFields(log.Fields{"configPath": configPath, "error": err}).Debug("reading containerd config")
		return defaultContainerdRootDir
	}

	var cfg containerdConfig.Config

	if err := containerdConfig.LoadConfig(configPath, &cfg); err != nil {
		log.WithFields(log.Fields{"configPath": configPath, "error": err}).Debug("parsing containerd config")
		return defaultContainerdRootDir
	}

	if cfg.Root == "" {
		return defaultContainerdRootDir
	}

	return cfg.Root
}
