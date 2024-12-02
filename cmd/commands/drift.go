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
	"fmt"
	"runtime"
	"os"
	"strings"
	"text/tabwriter"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

var DriftCommand = cli.Command{
	Name:        "drift",
	Aliases:     []string{"diff"},
	Usage:       "identifies container filesystem changes",
	Description: "identifies container filesystem changes for all containers",
	ArgsUsage: "",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "filter",
			Usage: "comma separated label filter using key=value pair",
		},
		cli.BoolFlag{
			Name:  "mount-support-containers",
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

		ctx, exp, cancel, err := explorerEnvironment(clictx)
		if err != nil {
			return err
		}
		defer cancel()

		drifts, err := exp.ContainerDrift(ctx, filter, !clictx.Bool("mount-support-containers"))
		if err != nil {
			log.WithField("message", err).Error("retrieving container drift")
            if output == "json" && outputfile != "" {
                data := []string{}
                writeOutputFile(data, outputfile)
            }
            return nil
		}
        // Handle output formats
        if strings.ToLower(output) == "json" {
            if outputfile != "" {
                writeOutputFile(drifts, outputfile)
            } else {
                printAsJSON(drifts)
            }
            return nil
        }

        // Default to table output
        tw := tabwriter.NewWriter(os.Stdout, 1, 8, 1, '\t', 0)
        defer tw.Flush()

        if output == "table" {
            // Define the header
            fmt.Fprintf(tw, "CONTAINER ID\tADDED/MODIFIED\tDELETED\n")
        }

        for _, drift := range drifts {
            switch strings.ToLower(output) {
            case "json_line":
                printAsJSONLine(drift)
            default:
                // Prepare the data for display
                addedOrModified := strings.Join(drift.AddedOrModified, ", ")
                inaccessibleFiles := strings.Join(drift.InaccessibleFiles, ", ")

                displayValues := fmt.Sprintf("%s\t%s\t%s",
                    drift.ContainerID,
                    addedOrModified,
                    inaccessibleFiles,
                )

                fmt.Fprintf(tw, "%v\n", displayValues)
            }
        }
		// default
		return nil
	},
}