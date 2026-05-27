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
	"strings"

	"github.com/google/container-explorer/explorers"

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
			Name:  "image, i",
			Usage: "output container as raw image",
		},
		cli.BoolFlag{
			Name:  "archive, a",
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

		log.WithFields(log.Fields{
			"containerID":     containerID,
			"outputDir":       outputDir,
			"exportAsImage":   exportAsImage,
			"exportAsArchive": exportAsArchive,
		}).Debug("processing export request")

		matched, err := ForMatchingContainer(GlobalConfig.Context, containerID, func(xplr explorers.ContainerExplorer) error {
			return xplr.ExportContainer(GlobalConfig.Context, containerID, outputDir, exportOptions)
		})

		if !matched {
			return fmt.Errorf("no matching container")
		}
		return err
	},
}

var ExportAllCommand = cli.Command{
	Name:        "export-all",
	Aliases:     []string{"export_all"},
	Usage:       "export all containers as image or archive",
	Description: "export all containers as image or archive",
	ArgsUsage:   "OUTPUTDIR",
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:  "image, i",
			Usage: "output container as raw image",
		},
		cli.BoolFlag{
			Name:  "archive, a",
			Usage: "output container as archive",
		},
		cli.StringFlag{
			Name:  "container-engine, e",
			Usage: "supported container engine containerd, docker, and podman",
			Value: "all",
		},
		cli.StringFlag{
			Name:  "filter, f",
			Usage: "comma separated label filter using key=value",
		},
		cli.BoolFlag{
			Name:  "export-support-containers, s",
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

		exps := GetExplorers()
		for _, xplr := range exps {
			engineName := xplr.Type()
			if containerEngine == "all" || strings.ToLower(containerEngine) == engineName {
				if err := xplr.ExportAllContainers(GlobalConfig.Context, outputDir, exportOptions, filterMap, exportSupportContainers); err != nil {
					log.Errorf("exporting all %s containers as image or archive: %v", engineName, err)
				}
			}
		}

		return nil
	},
}

