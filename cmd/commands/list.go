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
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/google/container-explorer/explorers"

	log "github.com/sirupsen/logrus"

	"github.com/urfave/cli"
)

const tsLayout = "2006-01-02T15:04:05Z"

var ListCommand = cli.Command{
	Name:    "list",
	Aliases: []string{"ls"},
	Usage:   "lists container related information",
	Subcommands: cli.Commands{
		listNamespaces,
		listContainers,
		listContents,
		listImages,
		listSnapshots,
		listTasks,
	},
}

var listNamespaces = cli.Command{
	Name:        "namespaces",
	Aliases:     []string{"namespace", "ns"},
	Usage:       "list all namespaces",
	Description: "list all namespaces",
	Action: func(clictx *cli.Context) error {
		exps := GetExplorers()
		fmt.Println("NAMESPACE")
		for _, xplr := range exps {
			// Currently namespaces are only relevant for containerd.
			if xplr.Type() != "containerd" {
				continue
			}

			nss, err := xplr.ListNamespaces(GlobalConfig.Context)
			if err != nil {
				log.Fatal(err)
			}

			for _, ns := range nss {
				fmt.Println(ns)
			}
		}

		return nil
	},
}

var listContainers = cli.Command{
	Name:        "containers",
	Aliases:     []string{"container"},
	Usage:       "list containers for all namespaces",
	Description: "list containers for all namespaces",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "filter, f",
			Usage: "comma separated label filter using key=value pair",
		},
		cli.BoolFlag{
			Name:  "show-support-containers, s",
			Usage: "show supporting containers created by Kubernetes",
		},
		cli.BoolFlag{
			Name:  "no-labels, L",
			Usage: "hide container labels",
		},
		cli.BoolFlag{
			Name:  "updated",
			Usage: "show updated timestamp",
		},
		cli.BoolFlag{
			Name:  "ports, p",
			Usage: "show exposed ports",
		},
		cli.BoolFlag{
			Name:  "running, r",
			Usage: "show running docker managed containers",
		},
	},
	Action: func(clictx *cli.Context) error {
		output := GlobalConfig.Output
		outputfile := GlobalConfig.OutputFile
		filters := clictx.String("filter")

		containermap := make(map[string]explorers.Container)
		exps := GetExplorers()

		// First pass: Collect all containers
		for _, xplr := range exps {
			engineContainers, err := xplr.ListContainers(GlobalConfig.Context)
			if err != nil {
				engineName := xplr.Type()
				log.WithField("message", err).Errorf("listing %s containers", engineName)
				continue
			}

			for _, c := range engineContainers {
				// Merging logic: Docker might enrich containerd containers
				if existing, ok := containermap[c.ID]; ok {
					// Enriching with name, pid, status if they are more complete
					if existing.Name == "" {
						existing.Name = c.Name
					}
					if existing.ProcessID == 0 {
						existing.ProcessID = c.ProcessID
					}
					if existing.Status == "" {
						existing.Status = c.Status
					}
					containermap[c.ID] = existing
				} else {
					containermap[c.ID] = c
				}
			}
		}

		var containers []explorers.Container
		for _, container := range containermap {
			containers = append(containers, container)
		}

		// Filter containers
		filteredContainers := containers[:0]
		if filters != "" {
			labelFilters := strings.Split(filters, ",")

			for _, container := range containers {
				include := true

				for _, f := range labelFilters {
					if !strings.Contains(f, "=") {
						continue
					}
					key := strings.Split(f, "=")[0]
					value := strings.Split(f, "=")[1]
					labelValue, ok := container.Labels[key]
					if !ok {
						include = false
						break
					}

					if labelValue != value {
						include = false
						break
					}
				}
				if include {
					filteredContainers = append(filteredContainers, container)
				}
			}
			containers = filteredContainers
		}

		// Handling JSON output
		if strings.ToLower(output) == "json" {
			if outputfile != "" {
				writeOutputFile(containers, outputfile)
			} else {
				printAsJSON(containers)
			}
			return nil
		}

		// Handling table output
		tw := tabwriter.NewWriter(os.Stdout, 1, 8, 1, '\t', 0)
		defer tw.Flush()

		if output == "table" {
			displayFields := "CONTAINER TYPE\tNAMESPACE\tCONTAINER ID\tCONTAINER NAME\tIMAGE\tCREATED AT\tPID\tSTATUS"
			// show updated timestamp
			if clictx.Bool("updated") {
				displayFields = fmt.Sprintf("%v\tUPDATED AT", displayFields)
			}
			// show exposed ports
			if clictx.Bool("ports") {
				displayFields = fmt.Sprintf("%v\tEXPOSED PORTS", displayFields)
			}
			// show labels
			if !clictx.Bool("no-labels") {
				displayFields = fmt.Sprintf("%v\tLABELS", displayFields)
			}
			fmt.Fprintf(tw, "%v\n", displayFields)
		}

		for _, container := range containers {
			// Show Kubernetes support containers created
			if !clictx.Bool("show-support-containers") && container.SupportContainer {
				log.WithFields(log.Fields{
					"namespace":        container.Namespace,
					"containerID":      container.ID,
					"supportcontainer": container.SupportContainer,
				}).Info("skipping support container")

				continue
			}

			switch strings.ToLower(output) {
			case "json_line":
				printAsJSONLine(container)
			default:
				displayValues := fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s",
					container.ContainerType,
					container.Namespace,
					container.ID,
					container.Name,
					container.Image,
					container.CreatedAt.Format(tsLayout),
					container.ProcessID,
					container.Status,
				)
				// show updated timestamp value
				if clictx.Bool("updated") {
					displayValues = fmt.Sprintf("%v\t%s", displayValues, container.UpdatedAt.Format(tsLayout))
				}
				// show exposed ports value
				if clictx.Bool("ports") {
					displayValues = fmt.Sprintf("%v\t%s", displayValues, arrayToString(container.ExposedPorts))
				}
				// show labels values
				if !clictx.Bool("no-labels") {
					displayValues = fmt.Sprintf("%v\t%v", displayValues, labelString(container.Labels))
				}
				fmt.Fprintf(tw, "%v\n", displayValues)
			}
		}

		return nil
	},
}

var listImages = cli.Command{
	Name:        "images",
	Aliases:     []string{"image"},
	Usage:       "list images for all namespaces",
	Description: "list images for all namespaces",
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:  "show-support-containers, s",
			Usage: "show Kubernetes support container images",
		},
		cli.BoolFlag{
			Name:  "updated",
			Usage: "show updated timestamp",
		},
		cli.BoolFlag{
			Name:  "no-labels, L",
			Usage: "hide image labels",
		},
	},
	Action: func(clictx *cli.Context) error {
		output := GlobalConfig.Output
		outputfile := GlobalConfig.OutputFile

		var containerImages []explorers.Image
		exps := GetExplorers()

		for _, xplr := range exps {
			engineImages, err := xplr.ListImages(GlobalConfig.Context)
			if err != nil {
				engineName := xplr.Type()
				log.WithField("message", err).Errorf("listing %s images", engineName)
				continue
			}
			containerImages = append(containerImages, engineImages...)
		}

		// Handle JSON output
		if strings.ToLower(output) == "json" {
			if outputfile != "" {
				writeOutputFile(containerImages, outputfile)
			} else {
				printAsJSON(containerImages)
			}
			return nil
		}

		// Handle table output
		tw := tabwriter.NewWriter(os.Stdout, 1, 8, 1, '\t', 0)
		defer tw.Flush()

		// Setting table output
		if strings.ToLower(output) == "table" {
			displayFields := "CONTAINER TYPE\tNAMESPACE\tNAME\tCREATED AT\tDIGEST\tTYPE"
			if clictx.Bool("updated") {
				displayFields = fmt.Sprintf("%v\tUPDATED AT", displayFields)
			}
			if !clictx.Bool("no-labels") {
				displayFields = fmt.Sprintf("%v\tLABELS", displayFields)
			}

			fmt.Fprintf(tw, "%v\n", displayFields)
		}

		for _, image := range containerImages {
			if !clictx.Bool("show-support-containers") && image.SupportContainerImage {
				log.WithFields(log.Fields{
					"namespace": image.Namespace,
					"image":     image.Name,
				}).Debug("skipping Kubernetes support container image")
				continue
			}

			switch strings.ToLower(output) {
			case "json_line":
				printAsJSONLine(image)
			default:
				displayValues := fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s",
					image.ContainerType,
					image.Namespace,
					image.Name,
					image.CreatedAt.Format(tsLayout),
					string(image.Target.Digest),
					image.Target.MediaType,
				)
				if clictx.Bool("updated") {
					displayValues = fmt.Sprintf("%v\t%s", displayValues, image.UpdatedAt.Format(tsLayout))
				}
				if !clictx.Bool("no-labels") {
					displayValues = fmt.Sprintf("%v\t%s", displayValues, labelString(image.Labels))
				}
				fmt.Fprintf(tw, "%v\n", displayValues)
			}
		}
		return nil
	},
}

var listContents = cli.Command{
	Name:        "contents",
	Aliases:     []string{"content"},
	Usage:       "list content for all namespaces",
	Description: "list content for all namespaces",
	Action: func(clictx *cli.Context) error {
		output := GlobalConfig.Output
		outputfile := GlobalConfig.OutputFile

		var containerContents []explorers.Content
		exps := GetExplorers()

		for _, xplr := range exps {
			engineContents, err := xplr.ListContent(GlobalConfig.Context)
			if err != nil {
				engineName := xplr.Type()
				log.WithField("message", err).Errorf("listing %s content", engineName)
				continue
			}
			containerContents = append(containerContents, engineContents...)
		}

		// Handling JSON output
		if strings.ToLower(output) == "json" {
			if outputfile != "" {
				writeOutputFile(containerContents, outputfile)
			} else {
				printAsJSON(containerContents)
			}
			return nil
		}

		// Handling table output
		tw := tabwriter.NewWriter(os.Stdout, 1, 8, 1, '\t', 0)
		defer tw.Flush()

		if strings.ToLower(output) == "table" {
			fmt.Fprintf(tw, "CONTAINER TYPE\tNAMESPACE\tDIGEST\tSIZE\tCREATED AT\tUPDATED AT\tLABELS\n")
		}

		for _, c := range containerContents {
			switch strings.ToLower(output) {
			case "json_line":
				printAsJSONLine(c)
			default:
				fmt.Fprintf(tw, "%s\t%s\t%s\t%v\t%v\t%v\t%s\n",
					c.ContainerType,
					c.Namespace,
					c.Digest,
					c.Size,
					c.CreatedAt.Format(tsLayout),
					c.UpdatedAt.Format(tsLayout),
					labelString(c.Labels),
				)
			}
		}

		return nil
	},
}

var listSnapshots = cli.Command{
	Name:        "snapshots",
	Aliases:     []string{"snapshot"},
	Usage:       "list snapshots for all namespaces",
	Description: "list snapshots for all namespaces",
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:  "no-labels, L",
			Usage: "hide snapshot labels",
		},
		cli.BoolFlag{
			Name:  "full-overlay-path, P",
			Usage: "show overlay full path",
		},
	},
	Action: func(clictx *cli.Context) error {
		output := GlobalConfig.Output
		outputfile := GlobalConfig.OutputFile

		var containerSnapshotKeyInfos []explorers.SnapshotKeyInfo
		exps := GetExplorers()

		for _, xplr := range exps {
			engineSnapshots, err := xplr.ListSnapshots(GlobalConfig.Context)
			if err != nil {
				engineName := xplr.Type()
				log.WithField("message", err).Errorf("listing %s snapshots", engineName)
				continue
			}

			// Add full overlay path if requested
			if clictx.Bool("full-overlay-path") {
				for i := range engineSnapshots {
					engineSnapshots[i].OverlayPath = filepath.Join(xplr.SnapshotRoot(engineSnapshots[i].Snapshotter), engineSnapshots[i].OverlayPath)
				}
			}

			containerSnapshotKeyInfos = append(containerSnapshotKeyInfos, engineSnapshots...)
		}

		// Handling JSON output
		if strings.ToLower(output) == "json" {
			if outputfile != "" {
				writeOutputFile(containerSnapshotKeyInfos, outputfile)
			} else {
				printAsJSON(containerSnapshotKeyInfos)
			}
			return nil
		}

		// Handling Table output
		tw := tabwriter.NewWriter(os.Stdout, 1, 8, 1, '\t', 0)
		defer tw.Flush()

		// Setting table output header
		if strings.ToLower(output) == "table" {
			displayFields := "CONTAINER TYPE\tNAMESPACE\tSNAPSHOTTER\tCREATED AT\tUPDATED AT\tKIND\tNAME\tPARENT\tLAYER PATH"
			if !clictx.Bool("no-labels") {
				displayFields = fmt.Sprintf("%s\tLABELS", displayFields)
			}
			fmt.Fprintf(tw, "%v\n", displayFields)
		}

		for _, s := range containerSnapshotKeyInfos {
			switch strings.ToLower(output) {
			case "json_line":
				printAsJSONLine(s)
			default:
				displayValue := fmt.Sprintf("%s\t%v\t%v\t%v\t%v\t%v\t%v\t%v\t%v",
					s.ContainerType,
					s.Namespace,
					s.Snapshotter,
					s.CreatedAt.Format(tsLayout),
					s.UpdatedAt.Format(tsLayout),
					s.Kind,
					s.Key,
					s.Parent,
					s.OverlayPath,
				)

				if !clictx.Bool("no-labels") {
					displayValue = fmt.Sprintf("%v\t%v", displayValue, labelString(s.Labels))
				}
				fmt.Fprintf(tw, "%v\n", displayValue)
			}
		}

		return nil
	},
}

var listTasks = cli.Command{
	Name:        "tasks",
	Aliases:     []string{"task"},
	Usage:       "list tasks",
	Description: "list container tasks",
	Action: func(clictx *cli.Context) error {
		output := GlobalConfig.Output
		outputfile := GlobalConfig.OutputFile

		taskMap := make(map[string]explorers.Task)
		exps := GetExplorers()

		for _, xplr := range exps {
			engineTasks, err := xplr.ListTasks(GlobalConfig.Context)
			if err != nil {
				engineName := xplr.Type()
				log.WithField("message", err).Errorf("listing %s tasks", engineName)
				continue
			}

			for _, t := range engineTasks {
				if existing, ok := taskMap[t.Name]; ok {
					if existing.PID == 0 {
						existing.PID = t.PID
					}
					if existing.Status == "" {
						existing.Status = t.Status
					}
					taskMap[t.Name] = existing
				} else {
					taskMap[t.Name] = t
				}
			}
		}

		var containerTasks []explorers.Task
		for _, task := range taskMap {
			containerTasks = append(containerTasks, task)
		}

		// Handling JSON output
		if strings.ToLower(output) == "json" {
			if outputfile != "" {
				writeOutputFile(containerTasks, outputfile)
			} else {
				printAsJSON(containerTasks)
			}
			return nil
		}

		// Handling table output
		tw := tabwriter.NewWriter(os.Stdout, 1, 8, 1, '\t', 0)
		defer tw.Flush()

		displayFields := "CONTAINER TYPE\tNAMESPACE\tCONTAINER ID\tPID\tSTATUS"
		fmt.Fprintf(tw, "%v\n", displayFields)

		for _, t := range containerTasks {
			switch strings.ToLower(output) {
			case "json_line":
				printAsJSONLine(t)
			default:
				displayValues := fmt.Sprintf("%v\t%v\t%v\t%v\t%v",
					t.ContainerType,
					t.Namespace,
					t.Name,
					t.PID,
					t.Status,
				)
				fmt.Fprintf(tw, "%v\n", displayValues)
			}
		}
		return nil
	},
}

