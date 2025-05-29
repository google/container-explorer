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
	"fmt"
	"runtime"

	"github.com/google/container-explorer/explorers"
	"github.com/google/container-explorer/explorers/containerd"
	"github.com/google/container-explorer/explorers/docker"

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
			Name: "image",
			Usage: "output container as raw image",
		},
		cli.BoolFlag{
			Name: "archive",
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
			return err
		}

		namespace := runtimeConfig["namespace"].(string)
		imageRootDir := runtimeConfig["imageRootDir"].(string)
		containerdRootDir := runtimeConfig["containerdRootDir"].(string)
		dockerRootDir := runtimeConfig["dockerRootDir"].(string)
		metadataFile := runtimeConfig["metadataFile"].(string)
		snapshotFile := runtimeConfig["snapshotFile"].(string)
		layercache := runtimeConfig["layerCache"].(string)
		sc := runtimeConfig["supportContainer"].(*explorers.SupportContainer)

		log.WithFields(log.Fields{
			"namespace":   namespace,
			"containerID": containerID,
			"outputDir":  outputDir,
			"exportAsImage": exportAsImage,
			"exportAsArchive": exportAsArchive,
		}).Debug("Processing export request")

		cXplr, err := containerd.NewExplorer(imageRootDir, containerdRootDir, metadataFile, snapshotFile, layercache, sc)
		if err == nil {
			if err := cXplr.ExportContainer(ctx, containerID, outputDir, exportOptions); err != nil {
				log.Errorf("exporting %s as containerd container: %v", containerID, err)
			}
		} else {
			log.Errorf("getting containerd explorer: %v", err)
		}

		dXplr, err := docker.NewExplorer(dockerRootDir, containerdRootDir, metadataFile, snapshotFile, sc)
		if err == nil {
			if err := dXplr.ExportContainer(ctx, containerID, outputDir, exportOptions); err != nil {
				log.Errorf("exporting %s as Docker container: %v", containerID, err)
			}
		} else {
			log.Errorf("getting Docker explorer: %v", err)
		}

		// default return
		return nil
	},
}

var ExportAllCommand = cli.Command{
	Name: "export-all",
	Aliases: []string{"export_all"},
	Usage: "export all containers as image or archive",
	Description: "export all containers as image or archive",
	ArgsUsage: "OUTPUTDIR",
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name: "image",
			Usage: "output container as raw image",
		},
		cli.BoolFlag{
			Name: "archive",
			Usage: "output container as archive",
		},
		cli.StringFlag{
			Name: "filter",
			Usage: "comma separated label filter using key=value",
		},
		cli.BoolFlag{
			Name: "export-support-containers",
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
			return err
		}

		namespace := runtimeConfig["namespace"].(string)
		imageRootDir := runtimeConfig["imageRootDir"].(string)
		containerdRootDir := runtimeConfig["containerdRootDir"].(string)
		dockerRootDir := runtimeConfig["dockerRootDir"].(string)
		metadataFile := runtimeConfig["metadataFile"].(string)
		snapshotFile := runtimeConfig["snapshotFile"].(string)
		layercache := runtimeConfig["layerCache"].(string)
		sc := runtimeConfig["supportContainer"].(*explorers.SupportContainer)

		log.WithFields(log.Fields{
			"namespace":   namespace,
			"outputDir":  outputDir,
			"exportAsImage": exportAsImage,
			"exportAsArchive": exportAsArchive,
		}).Debug("Processing export-all request")

		// Exporting all containerd containers
		cXplr, err := containerd.NewExplorer(imageRootDir, containerdRootDir, metadataFile, snapshotFile, layercache, sc)
		if err == nil {
			if err := cXplr.ExportAllContainers(ctx, outputDir, exportOptions, filterMap, exportSupportContainers); err != nil {
				log.Errorf("exporting all containerd containers as image or archive: %v", err)
			}
		} else {
			log.Errorf("getting containerd explorer: %v", err)
		}

		// Exporting all Docker containers
		dXplr, err := docker.NewExplorer(dockerRootDir, containerdRootDir, metadataFile, snapshotFile, sc)
		if err == nil {
			if err := dXplr.ExportAllContainers(ctx, outputDir, exportOptions, filterMap, exportSupportContainers); err != nil {
				log.Errorf("exporting all Docker containers as image or archive: %v", err)
			}
		} else {
			log.Errorf("getting Docker explorer: %v", err)
		}

		return nil
	},
}