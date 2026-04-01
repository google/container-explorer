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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/google/container-explorer/explorers"
	"github.com/google/container-explorer/explorers/containerd"
	"github.com/google/container-explorer/explorers/docker"
	"github.com/google/container-explorer/explorers/podman"

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

		ctx, runtimeConfig, err := parseRuntimeConfig(clictx)
		if err != nil {
			log.Fatal(err)
		}

		imageRootDir := runtimeConfig["imageRootDir"].(string)
		containerdRootDir := runtimeConfig["containerdRootDir"].(string)
		dockerRootDir := runtimeConfig["dockerRootDir"].(string)
		layercache := runtimeConfig["layercache"].(string)
		sc := runtimeConfig["supportContainerData"].(*explorers.SupportContainer)

		ctrdxplr, nil := containerd.NewExplorer(imageRootDir, containerdRootDir, dockerRootDir, layercache, sc)
		nss, err := ctrdxplr.ListNamespaces(ctx)
		if err != nil {
			log.Fatal(err)
		}

		fmt.Println("NAMESPACE")
		for _, ns := range nss {
			fmt.Println(ns)
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
		output := clictx.GlobalString("output")
		outputfile := clictx.GlobalString("output-file")
		filters := clictx.String("filter")

		//ctx, runtimeConfig, err := parseRuntimeConfig(clictx)
		ctx, runtimeConfig, err := parseRuntimeConfig(clictx)
		if err != nil {
			log.WithField("message", err).Error("setting environment")
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

		containermap := make(map[string]explorers.Container)

		ctrdxplr, err := containerd.NewExplorer(imageRootDir, containerdRootDir, dockerRootDir, layercache, sc)
		if err != nil {
			log.Error("unable to get containerd explorer")
		} else {
			ctrdContainers, err := ctrdxplr.ListContainers(ctx)
			if err != nil {
				log.WithField("message", err).Error("listing containerd containers")
			} else {
				for _, container := range ctrdContainers {
					containermap[container.ID] = container
				}
			}
		}

		dkrxplr, err := docker.NewExplorer(imageRootDir, containerdRootDir, dockerRootDir)
		if err != nil {
			log.Error("unable to get docker explorer")
		} else {
			dockerContainers, err := dkrxplr.ListContainers(ctx)
			if err != nil {
				log.WithField("message", err).Error("listing docker containers")
			} else {
				// Docker container has additional information
				// enriching container information collected using containerd
				for _, dockerContainer := range dockerContainers {
					container, ok := containermap[dockerContainer.ID]
					if ok {
						container.Name = dockerContainer.Name
						container.ProcessID = dockerContainer.ProcessID
						container.Status = dockerContainer.Status
						containermap[container.ID] = container
					} else {
						containermap[dockerContainer.ID] = dockerContainer
					}
				}
			}
		}

		var containers []explorers.Container
		for _, container := range containermap {
			containers = append(containers, container)
		}

		// Podman containers
		pmxplr, err := podman.NewExplorer(imageRootDir)
		if err != nil {
			log.Error("unable to get podman explorer")
		} else {
			podmanContainers, err := pmxplr.ListContainers(ctx)
			if err != nil {
				log.Error("listing podman containers")
			} else {
				containers = append(containers, podmanContainers...)
			}
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
		output := clictx.GlobalString("output")
		outputfile := clictx.GlobalString("output-file")

		ctx, runtimeConfig, err := parseRuntimeConfig(clictx)
		if err != nil {
			log.WithField("message", err).Error("setting environment")
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

		var containerImages []explorers.Image

		// Collecting containerd images
		ctrdxplr, err := containerd.NewExplorer(imageRootDir, containerdRootDir, dockerRootDir, layercache, sc)
		if err != nil {
			log.Error("unable to get containerd explorer handle")
		} else {
			ctrdImages, err := ctrdxplr.ListImages(ctx)
			if err != nil {
				log.WithField("message", err).Error("listing images")
			} else {
				containerImages = append(containerImages, ctrdImages...)
			}
		}

		// Collecting Docker images
		dkrxplr, err := docker.NewExplorer(imageRootDir, containerdRootDir, dockerRootDir)
		if err != nil {
			log.Error("unable to get docker explorer")
		} else {
			dockerImages, err := dkrxplr.ListImages(ctx)
			if err != nil {
				log.WithField("message", err).Error("listing docker images")
			} else {
				containerImages = append(containerImages, dockerImages...)
			}
		}

		// Podman images
		pdmxplr, err := podman.NewExplorer(imageRootDir)
		if err != nil {
			log.Error("unable to get podman explorer")
		} else {
			podmanImages, err := pdmxplr.ListImages(ctx)
			if err != nil {
				log.WithField("message", err).Error("listing podman images")
			} else {
				containerImages = append(containerImages, podmanImages...)
			}
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
		output := clictx.GlobalString("output")
		outputfile := clictx.GlobalString("output-file")

		ctx, runtimeConfig, err := parseRuntimeConfig(clictx)
		if err != nil {
			log.WithField("message", err).Error("setting environment")
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

		var containerContents []explorers.Content

		// Containerd content
		ctrdxplr, err := containerd.NewExplorer(imageRootDir, containerdRootDir, dockerRootDir, layercache, sc)
		if err != nil {
			log.Error("unable to get containerd explorer")
		} else {
			ctrdContents, err := ctrdxplr.ListContent(ctx)
			if err != nil {
				log.WithField("message", err).Error("listing containerd content")
			} else {
				containerContents = append(containerContents, ctrdContents...)
			}
		}

		// Docker content
		dkrxplr, err := docker.NewExplorer(imageRootDir, containerdRootDir, dockerRootDir)
		if err != nil {
			log.Error("unable to get docker explorer")
		} else {
			dkrContents, err := dkrxplr.ListContent(ctx)
			if err != nil {
				log.WithField("message", err).Error("listing docker content")
			} else {
				containerContents = append(containerContents, dkrContents...)
			}
		}

		// Podman content
		pmxplr, err := podman.NewExplorer(imageRootDir)
		if err != nil {
			log.Error("unable to get podman explorer")
		} else {
			pmContents, err := pmxplr.ListContent(ctx)
			if err != nil {
				log.WithField("message", err).Error("listing podman content")
			} else {
				containerContents = append(containerContents, pmContents...)
			}
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
		output := clictx.GlobalString("output")
		outputfile := clictx.GlobalString("output-file")

		ctx, runtimeConfig, err := parseRuntimeConfig(clictx)
		if err != nil {
			log.WithField("message", err).Error("setting environment")
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

		var containerSnapshotKeyInfos []explorers.SnapshotKeyInfo

		// Containerd explorer
		ctrdxplr, err := containerd.NewExplorer(imageRootDir, containerdRootDir, dockerRootDir, layercache, sc)
		if err != nil {
			log.Error("unable to get containerd explorer")
		} else {
			ctrdSnapshotKeyInfos, err := ctrdxplr.ListSnapshots(ctx)
			if err != nil {
				log.WithField("message", err).Error("listing containerd snapshots")
			} else {
				containerSnapshotKeyInfos = append(containerSnapshotKeyInfos, ctrdSnapshotKeyInfos...)
			}
		}

		// Docker explorer
		dkrxplr, err := docker.NewExplorer(imageRootDir, containerdRootDir, dockerRootDir)
		if err != nil {
			log.Error("unable to get docker explorer")
		} else {
			dkrSnapshotKeyInfos, err := dkrxplr.ListSnapshots(ctx)
			if err != nil {
				log.WithField("message", err).Error("listing docker snapshots")
			} else {
				containerSnapshotKeyInfos = append(containerSnapshotKeyInfos, dkrSnapshotKeyInfos...)
			}
		}

		// Podman explorer
		pmxplr, err := podman.NewExplorer(imageRootDir)
		if err != nil {
			log.Error("unable to get podman explorer")
		} else {
			pmSnapshotKeyInfos, err := pmxplr.ListSnapshots(ctx)
			if err != nil {
				log.WithField("message", err).Error("listing podman snapshots")
			} else {
				containerSnapshotKeyInfos = append(containerSnapshotKeyInfos, pmSnapshotKeyInfos...)
			}
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
			var ssfilepath string

			switch s.ContainerType {
			case "containerd":
				ssfilepath = filepath.Join(ctrdxplr.SnapshotRoot(s.Snapshotter), s.OverlayPath)
			case "docker":
				ssfilepath = filepath.Join(dkrxplr.SnapshotRoot(s.Snapshotter), s.OverlayPath)
			case "podman":
				ssfilepath = filepath.Join(pmxplr.SnapshotRoot(s.Snapshotter), s.OverlayPath)
			}

			//ssfilepath := filepath.Join(exp.SnapshotRoot(s.Snapshotter), s.OverlayPath)

			switch strings.ToLower(output) {
			case "json_line":
				s.OverlayPath = ssfilepath
				printAsJSONLine(s)
			default:
				if clictx.Bool("full-overlay-path") {
					s.OverlayPath = ssfilepath
				}

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
		output := clictx.GlobalString("output")
		outputfile := clictx.GlobalString("output-file")

		ctx, runtimeConfig, err := parseRuntimeConfig(clictx)
		if err != nil {
			log.WithField("message", err).Error("setting environment")
			if outputfile != "" {
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

		//		var containerTasks []explorers.Task
		taskMap := make(map[string]explorers.Task)

		// Containerd tasks
		ctrdxplr, err := containerd.NewExplorer(imageRootDir, containerdRootDir, dockerRootDir, layercache, sc)
		if err != nil {
			log.Error("unable to get containerd explorer")
		} else {
			ctrdTasks, err := ctrdxplr.ListTasks(ctx)
			if err != nil {
				log.WithField("message", err).Error("listing containerd task")
			} else {
				for _, task := range ctrdTasks {
					taskMap[task.Name] = task
				}
			}
		}

		// Docker tasks
		dkrxplr, err := docker.NewExplorer(imageRootDir, containerdRootDir, dockerRootDir)
		if err != nil {
			log.Error("unable to get docker explorer")
		} else {
			dockerTasks, err := dkrxplr.ListTasks(ctx)
			if err != nil {
				log.WithField("message", err).Error("listing docker tasks")
			} else {
				// Docker task has additional information
				// enriching task information collected using containerd manifest.
				for _, t := range dockerTasks {
					task, ok := taskMap[t.Name]
					if ok {
						task.PID = t.PID
						task.Status = t.Status
						taskMap[t.Name] = task
					} else {
						taskMap[t.Name] = t
					}
				}
			}
		}

		// Podman tasks
		pmxplr, err := podman.NewExplorer(imageRootDir)
		if err != nil {
			log.Error("unable to get podman explorer")
		} else {
			podmanTasks, err := pmxplr.ListTasks(ctx)
			if err != nil {
				log.WithField("message", err).Error("listing podman tasks")
			} else {
				for _, task := range podmanTasks {
					taskMap[task.Name] = task
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

// labelString retruns a string of comma separated key-value pairs.
func labelString(labels map[string]string) string {
	var lablestrings []string

	for k, v := range labels {
		lablestrings = append(lablestrings, strings.Join([]string{k, v}, "="))
	}
	return strings.Join(lablestrings, ",")
}

// arrayToString returns a string of comma separated value of an array.
func arrayToString(array []string) string {
	var result string

	for i, val := range array {
		if i == 0 {
			result = val
			continue
		}
		result = fmt.Sprintf("%s,%s", result, val)
	}

	return result
}

// writeOutputFile writes JSON data to specified file.
func writeOutputFile(v any, outputfile string) {
	data, _ := json.Marshal(v)
	os.WriteFile(outputfile, data, 0644)
}
