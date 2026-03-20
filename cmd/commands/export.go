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

package commands

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"github.com/google/container-explorer/explorers"
	"github.com/google/container-explorer/explorers/containerd"
	"github.com/google/container-explorer/explorers/docker"
	"github.com/google/container-explorer/explorers/podman"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

var ExportCommand = cli.Command{
	Name:        "export",
	Usage:       "export a container as image or archive",
	Description: "export a container as image or archive",
	ArgsUsage:   "ID OUTPUTDIR",
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:  "image",
			Usage: "output container as raw image",
		},
		cli.BoolFlag{
			Name:  "archive",
			Usage: "output container as archive",
		},
	},
	Action: func(clictx *cli.Context) error {

		// Export a container is only supported on a Linux operating system.
		if runtime.GOOS != "linux" {
			return fmt.Errorf("exporting a container is only supported on Linux")
		}

		if clictx.NArg() < 2 {
			return fmt.Errorf("container ID and output directory are required")
		}

		containerID := clictx.Args().First()
		outputDir := clictx.Args().Get(1)

		exportAsImage := clictx.Bool("image")
		exportAsArchive := clictx.Bool("archive")

		// At least one options is required. If not provided by user
		// export as image file.
		if !exportAsArchive && !exportAsImage {
			exportAsImage = true
		}

		exportOptions := make(map[string]bool)
		exportOptions["image"] = exportAsImage
		exportOptions["archive"] = exportAsArchive

		// Process container-explorer runtime arguments
		ctx, runtimeConfig, err := parseRuntimeConfig(clictx)
		if err != nil {
			return fmt.Errorf("parsing runtime configuration: %w", err)
		}

		imageRootDir := runtimeConfig["imageRootDir"].(string)
		containerdRootDir := runtimeConfig["containerdRootDir"].(string)
		dockerRootDir := runtimeConfig["dockerRootDir"].(string)
		layercache := runtimeConfig["layercache"].(string)
		sc := runtimeConfig["supportContainerData"].(*explorers.SupportContainer)

		log.WithFields(log.Fields{
			"containerID":     containerID,
			"outputDir":       outputDir,
			"exportAsImage":   exportAsImage,
			"exportAsArchive": exportAsArchive,
		}).Debug("processing export request")

		ctrxplr, err := containerd.NewExplorer(imageRootDir, containerdRootDir, dockerRootDir, layercache, sc)
		if err != nil {
			log.Errorf("getting containerd explorer: %v", err)
		} else {
			matched, err := exportContainer(ctx, ctrxplr, containerID, outputDir, exportOptions)
			if matched {
				return err
			}
		}

		dkrxplr, err := docker.NewExplorer(imageRootDir, containerdRootDir, dockerRootDir)
		if err != nil {
			log.Errorf("getting docker explorer: %v", err)
		} else {
			matched, err := exportContainer(ctx, dkrxplr, containerID, outputDir, exportOptions)
			if matched {
				return err
			}
		}

		pmxplr, err := podman.NewExplorer(imageRootDir)
		if err != nil {
			log.Errorf("getting podman explorer: %v", err)
		} else {
			matched, err := exportContainer(ctx, pmxplr, containerID, outputDir, exportOptions)
			if matched {
				return err
			}
		}

		// default return
		return fmt.Errorf("no matching container")
	},
}

func exportContainer(ctx context.Context, xplr explorers.ContainerExplorer, containerID string, outputDir string, exportOptions map[string]bool) (bool, error) {
	container, err := xplr.GetContainerByID(ctx, containerID)
	if err != nil {
		return false, err
	}

	if container == nil {
		return false, fmt.Errorf("container is nil")
	}

	if err := xplr.ExportContainer(ctx, containerID, outputDir, exportOptions); err != nil {
		return true, fmt.Errorf("exporting container %s: %w", containerID, err)
	}

	return true, nil
}

var ExportAllCommand = cli.Command{
	Name:        "export-all",
	Aliases:     []string{"export_all"},
	Usage:       "export all containers as image or archive",
	Description: "export all containers as image or archive",
	ArgsUsage:   "OUTPUTDIR",
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:  "image",
			Usage: "output container as raw image",
		},
		cli.BoolFlag{
			Name:  "archive",
			Usage: "output container as archive",
		},
		cli.StringFlag{
			Name:  "container-engine",
			Usage: "supported container engine containerd, docker, and podman",
			Value: "all",
		},
		cli.StringFlag{
			Name:  "filter",
			Usage: "comma separated label filter using key=value",
		},
		cli.BoolFlag{
			Name:  "export-support-containers",
			Usage: "export Kubernetes supporting containers",
		},
	},
	Action: func(clictx *cli.Context) error {
		// Exporting containers only supported on a Linux operating system.
		if runtime.GOOS != "linux" {
			return fmt.Errorf("exporting containers is only supported on Linux")
		}

		if clictx.NArg() < 1 {
			return fmt.Errorf("output directory is required")
		}
		outputDir := clictx.Args().First()

		exportAsImage := clictx.Bool("image")
		exportAsArchive := clictx.Bool("archive")
		containerEngine := clictx.String("container-engine")

		// At least one options is required. If not provided by user
		// export as image file.
		if !exportAsArchive && !exportAsImage {
			exportAsImage = true
		}

		exportOptions := make(map[string]bool)
		exportOptions["image"] = exportAsImage
		exportOptions["archive"] = exportAsArchive

		filterString := clictx.String("filter")
		filterMap := getFilterMap(filterString)

		exportSupportContainers := clictx.Bool("export-support-containers")

		// Process container-explorer runtime arguments
		ctx, runtimeConfig, err := parseRuntimeConfig(clictx)
		if err != nil {
			return fmt.Errorf("parsing runtime configuration: %w", err)
		}

		imageRootDir := runtimeConfig["imageRootDir"].(string)
		containerdRootDir := runtimeConfig["containerdRootDir"].(string)
		dockerRootDir := runtimeConfig["dockerRootDir"].(string)
		layercache := runtimeConfig["layercache"].(string)
		sc := runtimeConfig["supportContainerData"].(*explorers.SupportContainer)

		// Exporting all containerd containers
		ctrxplr, err := containerd.NewExplorer(imageRootDir, containerdRootDir, dockerRootDir, layercache, sc)
		if err != nil {
			log.Errorf("getting containerd explorer: %v", err)
		} else {
			if containerEngine == "all" || strings.ToLower(containerEngine) == "containerd" {
				if err := ctrxplr.ExportAllContainers(ctx, outputDir, exportOptions, filterMap, exportSupportContainers); err != nil {
					log.Errorf("exporting all containerd containers as image or archive: %v", err)
				}
			}
		}

		// Exporting all Docker containers
		dkrxplr, err := docker.NewExplorer(imageRootDir, containerdRootDir, dockerRootDir)
		if err != nil {
			log.Errorf("getting docker explorer: %v", err)
		} else {
			if containerEngine == "all" || strings.ToLower(containerEngine) == "docker" {
				if err := dkrxplr.ExportAllContainers(ctx, outputDir, exportOptions, filterMap, exportSupportContainers); err != nil {
					log.Errorf("exporting all docker containers as image or archive: %v", err)
				}
			}
		}

		// Exporting podman container
		pmxplr, err := podman.NewExplorer(imageRootDir)
		if err != nil {
			log.Errorf("getting podman container: %v", err)
		} else {
			if containerEngine == "all" || strings.ToLower(containerEngine) == "podman" {
				if err := pmxplr.ExportAllContainers(ctx, outputDir, exportOptions, filterMap, exportSupportContainers); err != nil {
					log.Errorf("exporting all podman containers as image or archive: %v", err)
				}
			}
		}

		return nil
	},
}
