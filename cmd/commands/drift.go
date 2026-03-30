/*
Copyright 2024 Google LLC

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
	"runtime"
	"strings"
	"text/tabwriter"

	"github.com/google/container-explorer/explorers"
	"github.com/google/container-explorer/explorers/containerd"
	"github.com/google/container-explorer/explorers/docker"
	"github.com/google/container-explorer/explorers/podman"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

var DriftCommand = cli.Command{
	Name:        "drift",
	Aliases:     []string{"diff"},
	Usage:       "identifies container filesystem changes",
	Description: "identifies container filesystem changes for all containers",
	ArgsUsage:   "[containerID]",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "filter, f",
			Usage: "comma separated label filter using key=value pair",
		},
		cli.BoolFlag{
			Name:  "mount-support-containers, s",
			Usage: "mount Kubernetes supporting containers",
		},
	},
	Action: func(clictx *cli.Context) error {
		// Mounting a container is only supported on a Linux operating system.
		if runtime.GOOS != "linux" {
			return fmt.Errorf("feature is only supported on Linux")
		}
		output := clictx.GlobalString("output")
		outputfile := clictx.GlobalString("output-file")
		filter := clictx.String("filter")

		// Getting container ID positional arg
		var containerID string
		if clictx.Args().Present() {
			containerID = clictx.Args().First()
		}

		ctx, runtimeConfig, err := parseRuntimeConfig(clictx)
		if err != nil {
			log.WithField("message", err).Error("setting container explorer environment")
			if output == "json" && outputfile != "" {
				data := []string{}
				writeOutputFile(data, outputfile)
			}
			return nil
		}

		imageRootDir := runtimeConfig["imageRootDir"].(string)
		containerdRootDir := runtimeConfig["containerdRootDir"].(string)
		dockerRootDir := runtimeConfig["dockerRootDir"].(string)
		layercache := runtimeConfig["layercache"].(string)
		sc := runtimeConfig["supportContainerData"].(*explorers.SupportContainer)

		var allDrifts []explorers.Drift

		// Docker
		dkrxplr, err := docker.NewExplorer(imageRootDir, containerdRootDir, dockerRootDir)
		if err != nil {
			log.Error("unable to get docker explorer")
		} else {
			drifts, err := checkContainerDrift(ctx, dkrxplr, filter, clictx, containerID)
			if err != nil {
				log.WithField("message", err).Error("retrieving docker container drift")
			} else if drifts != nil {
				allDrifts = append(allDrifts, drifts...)
			}
		}

		// Podman
		pmxplr, err := podman.NewExplorer(imageRootDir)
		if err != nil {
			log.Error("unable to get podman explorer")
		} else {
			drifts, err := checkContainerDrift(ctx, pmxplr, filter, clictx, containerID)
			if err != nil {
				log.WithField("message", err).Error("retrieving podman container drift")
			} else if drifts != nil {
				allDrifts = append(allDrifts, drifts...)
			}
		}

		// Containerd
		ctrxplr, err := containerd.NewExplorer(imageRootDir, containerdRootDir, dockerRootDir, layercache, sc)
		if err != nil {
			log.Error("unable to get containerd explorer")
		} else {
			drifts, err := checkContainerDrift(ctx, ctrxplr, filter, clictx, containerID)
			if err != nil {
				log.WithField("message", err).Error("retrieving containerd container drift")
			} else if drifts != nil {
				allDrifts = append(allDrifts, drifts...)
			}
		}

		// Handle output formats
		if strings.ToLower(output) == "json" {
			if outputfile != "" {
				writeOutputFile(allDrifts, outputfile)
			} else {
				printAsJSON(allDrifts)
			}
			return nil
		}

		// Default to table output
		tw := tabwriter.NewWriter(os.Stdout, 1, 8, 1, '\t', 0)
		defer tw.Flush()

		if output == "table" {
			// Define the header
			fmt.Fprintf(tw, "CONTAINER TYPE\tCONTAINER ID\tADDED/MODIFIED\tDELETED\n")
		}

		for _, drift := range allDrifts {
			switch strings.ToLower(output) {
			case "json_line":
				printAsJSONLine(drift)
			default:
				// Prepare the data for display
				var addedOrModifiedFiles []string
				var inaccessibleFiles []string

				for _, fileinfo := range drift.AddedOrModified {
					if fileinfo.FileType == "executable" {
						addedOrModifiedFiles = append(addedOrModifiedFiles, fileinfo.FullPath+" (executable)")
					} else {
						addedOrModifiedFiles = append(addedOrModifiedFiles, fileinfo.FullPath)
					}
				}

				for _, fileinfo := range drift.InaccessibleFiles {
					inaccessibleFiles = append(inaccessibleFiles, fileinfo.FullPath)
				}

				displayAddedOrModifiedFiles := strings.Join(addedOrModifiedFiles, ", ")
				displayInaccessibleFiles := strings.Join(inaccessibleFiles, ", ")

				displayValues := fmt.Sprintf("%s\t%s\t%s\t%s",
					drift.ContainerType,
					drift.ContainerID,
					displayAddedOrModifiedFiles,
					displayInaccessibleFiles,
				)

				fmt.Fprintf(tw, "%v\n", displayValues)
			}
		}

		// default
		return nil
	},
}

func checkContainerDrift(ctx context.Context, xplr explorers.ContainerExplorer, filter string, clictx *cli.Context, containerID string) ([]explorers.Drift, error) {
	return xplr.ContainerDrift(ctx, filter, !clictx.Bool("mount-support-containers"), containerID)
}
