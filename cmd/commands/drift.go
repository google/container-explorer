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
	"fmt"
	"os"
	"runtime"
	"strings"
	"text/tabwriter"

	"github.com/google/container-explorer/explorers"

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
		output := GlobalConfig.Output
		outputfile := GlobalConfig.OutputFile
		filter := clictx.String("filter")

		// Getting container ID positional arg
		var containerID string
		if clictx.Args().Present() {
			containerID = clictx.Args().First()
		}

		var allDrifts []explorers.Drift

		exps := GetExplorers()
		for _, xplr := range exps {
			drifts, err := xplr.ContainerDrift(GlobalConfig.Context, filter, !clictx.Bool("mount-support-containers"), containerID)
			if err != nil {
				engineName := xplr.Type()
				log.WithField("message", err).Errorf("retrieving %s container drift", engineName)
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

