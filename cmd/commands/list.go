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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

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
		listContent,
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

		ctx, exp, cancel, err := explorerEnvironment(clictx)
		if err != nil {
			log.Fatal(err)
		}
		defer cancel()

		nss, err := exp.ListNamespaces(ctx)
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
			Name:  "filter",
			Usage: "comma separated label filter using key=value pair",
		},
		cli.BoolFlag{
			Name:  "show-support-containers",
			Usage: "show supporting containers created by Kubernetes",
		},
		cli.BoolFlag{
			Name:  "no-labels",
			Usage: "hide container labels",
		},
		cli.BoolFlag{
			Name:  "updated",
			Usage: "show updated timestamp",
		},
		cli.BoolFlag{
			Name:  "ports",
			Usage: "show exposed ports",
		},
		cli.BoolFlag{
			Name:  "running",
			Usage: "show running docker managed containers",
		},
	},
	Action: func(clictx *cli.Context) error {
		output := clictx.GlobalString("output")
		outputfile := clictx.GlobalString("output-file")
		filters := clictx.String("filter")

		ctx, exp, cancel, err := explorerEnvironment(clictx)
		if err != nil {
			log.WithField("message", err).Error("setting environment")
			if output == "json" && outputfile != "" {
				data := []string{}
				writeOutputFile(data, outputfile)
			}
			return nil
		}
		defer cancel()

		containers, err := exp.ListContainers(ctx)
		if err != nil {
			log.WithField("message", err).Error("listing containers")
			if output == "json" && outputfile != "" {
				data := []string{}
				writeOutputFile(data, outputfile)
			}
			return nil
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

		if strings.ToLower(output) == "json" {
			if outputfile != "" {
				writeOutputFile(containers, outputfile)
			} else {
				printAsJSON(containers)
			}
			return nil
		}

		tw := tabwriter.NewWriter(os.Stdout, 1, 8, 1, '\t', 0)
		defer tw.Flush()

		if output == "table" {
			displayFields := "NAMESPACE\tTYPE\tCONTAINER ID\tCONTAINER HOSTNAME\tIMAGE\tCREATED AT\tPID\tSTATUS"
			// show updated timestamp
			if clictx.Bool("updated") {
				displayFields = fmt.Sprintf("%v\tUPDATED AT", displayFields)
			}
			// show exposed ports
			if clictx.Bool("ports") {
				displayFields = fmt.Sprintf("%v\tEXPOSED PORTS", displayFields)
			}
			// display docker container name
			if clictx.GlobalBool("docker-managed") {
				displayFields = fmt.Sprintf("%v\tNAME", displayFields)
			}
			// show labels
			if !clictx.Bool("no-labels") {
				displayFields = fmt.Sprintf("%v\tLABELS", displayFields)
			}
			fmt.Fprintf(tw, "%v\n", displayFields)
		}

		for _, container := range containers {
			// Show Kubernetes support containers created
			// by GKE, EKS, and AKS
			if !clictx.Bool("show-support-containers") && container.SupportContainer {
				log.WithFields(log.Fields{
					"namespace":        container.Namespace,
					"containerid":      container.ID,
					"supportcontainer": container.SupportContainer,
				}).Info("skip support container")

				continue
			}

			// Show only running containers.
			//
			// This is currently supported only on a docker managed containers.
			if clictx.GlobalBool("docker-managed") && clictx.Bool("running") {
				if !container.Running {
					log.WithFields(log.Fields{
						"containerid": container.ID,
						"image":       container.Image,
					}).Info("skip container that was not running")

					continue
				}
			}

			switch strings.ToLower(output) {
			case "json_line":
				printAsJSONLine(container)
			default:
				displayValues := fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s",
					container.Namespace,
					container.ContainerType,
					container.ID,
					container.Hostname,
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
				// show docker container name
				if clictx.GlobalBool("docker-managed") {
					displayValues = fmt.Sprintf("%v\t%s", displayValues, strings.Replace(container.Runtime.Name, "/", "", 1))
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
			Name:  "show-support-containers",
			Usage: "show Kubernetes support container images",
		},
		cli.BoolFlag{
			Name:  "updated",
			Usage: "show updated timestamp",
		},
		cli.BoolFlag{
			Name:  "no-labels",
			Usage: "hide image labels",
		},
	},
	Action: func(clictx *cli.Context) error {
		output := clictx.GlobalString("output")
		outputfile := clictx.GlobalString("output-file")

		ctx, exp, cancel, err := explorerEnvironment(clictx)
		if err != nil {
			log.WithField("message", err).Error("setting environment")
			if output == "json" && outputfile != "" {
				data := []string{}
				writeOutputFile(data, outputfile)
			}
			return nil
		}
		defer cancel()

		images, err := exp.ListImages(ctx)
		if err != nil {
			log.WithField("message", err).Error("listing images")
			if output == "json" && outputfile != "" {
				data := []string{}
				writeOutputFile(data, outputfile)
			}
			return nil
		}

		if strings.ToLower(output) == "json" {
			if outputfile != "" {
				writeOutputFile(images, outputfile)
			} else {
				printAsJSON(images)
			}
			return nil
		}

		tw := tabwriter.NewWriter(os.Stdout, 1, 8, 1, '\t', 0)
		defer tw.Flush()

		// Setting table output
		if strings.ToLower(output) == "table" {
			displayFields := "NAMESPACE\tNAME\tCREATED AT\tDIGEST\tTYPE"
			if clictx.Bool("updated") {
				displayFields = fmt.Sprintf("%v\tUPDATED AT", displayFields)
			}
			if !clictx.Bool("no-labels") {
				displayFields = fmt.Sprintf("%v\tLABELS", displayFields)
			}

			fmt.Fprintf(tw, "%v\n", displayFields)
		}

		for _, image := range images {
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
				displayValues := fmt.Sprintf("%s\t%s\t%s\t%s\t%s",
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

var listContent = cli.Command{
	Name:        "content",
	Aliases:     []string{"content"},
	Usage:       "list content for all namespaces",
	Description: "list content for all namespaces",
	Action: func(clictx *cli.Context) error {
		output := clictx.GlobalString("output")
		outputfile := clictx.GlobalString("outputfile")

		ctx, exp, cancel, err := explorerEnvironment(clictx)
		if err != nil {
			log.WithField("message", err).Error("setting environment")
			if output == "json" && outputfile != "" {
				data := []string{}
				writeOutputFile(data, outputfile)
			}
			return nil
		}
		defer cancel()

		content, err := exp.ListContent(ctx)
		if err != nil {
			log.WithField("message", err).Error("listing content")
			if output == "json" && outputfile != "" {
				data := []string{}
				writeOutputFile(data, outputfile)
			}
			return nil
		}

		if strings.ToLower(output) == "json" {
			if outputfile != "" {
				writeOutputFile(content, outputfile)
			} else {
				printAsJSON(content)
			}
			return nil
		}

		tw := tabwriter.NewWriter(os.Stdout, 1, 8, 1, '\t', 0)
		defer tw.Flush()

		if strings.ToLower(output) == "table" {
			fmt.Fprintf(tw, "NAMESPACE\tDIGEST\tSIZE\tCREATED AT\tUPDATED AT\tLABELS\n")
		}

		for _, c := range content {
			switch strings.ToLower(output) {
			case "json_line":
				printAsJSONLine(c)
			default:
				fmt.Fprintf(tw, "%s\t%s\t%v\t%v\t%v\t%s\n",
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
			Name:  "no-labels",
			Usage: "hide snapshot labels",
		},
		cli.BoolFlag{
			Name:  "full-overlay-path",
			Usage: "show overlay full path",
		},
	},
	Action: func(clictx *cli.Context) error {
		output := clictx.GlobalString("output")
		outputfile := clictx.GlobalString("outputfile")

		ctx, exp, cancel, err := explorerEnvironment(clictx)
		if err != nil {
			//log.Fatal(err)
			log.WithField("message", err).Error("setting environment")
			if output == "json" && outputfile != "" {
				data := []string{}
				writeOutputFile(data, outputfile)
			}
			return nil
		}
		defer cancel()

		ss, err := exp.ListSnapshots(ctx)
		if err != nil {
			//log.Fatal(err)
			log.WithField("message", err).Error("listing snapshot")
			if output == "json" && outputfile != "" {
				data := []string{}
				writeOutputFile(data, outputfile)
			}
			return nil
		}

		if strings.ToLower(output) == "json" {
			if outputfile != "" {
				writeOutputFile(ss, outputfile)
			} else {
				printAsJSON(ss)
			}
			return nil
		}

		tw := tabwriter.NewWriter(os.Stdout, 1, 8, 1, '\t', 0)
		defer tw.Flush()

		// Setting table output header
		if strings.ToLower(output) == "table" {
			displayFields := "NAMESPACE\tSNAPSHOTTER\tCREATED AT\tUPDATED AT\tKIND\tNAME\tPARENT\tLAYER PATH"
			if !clictx.Bool("no-labels") {
				displayFields = fmt.Sprintf("%s\tLABELS", displayFields)
			}
			fmt.Fprintf(tw, "%v\n", displayFields)
		}

		for _, s := range ss {
			ssfilepath := filepath.Join(exp.SnapshotRoot(s.Snapshotter), s.OverlayPath)

			switch strings.ToLower(output) {
			case "json_line":
				s.OverlayPath = ssfilepath
				printAsJSONLine(s)
			default:
				if clictx.Bool("full-overlay-path") {
					s.OverlayPath = ssfilepath
				}

				displayValue := fmt.Sprintf("%v\t%v\t%v\t%v\t%v\t%v\t%v\t%v",
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
		outputfile := clictx.GlobalString("outputfile")

		ctx, exp, cancel, err := explorerEnvironment(clictx)
		if err != nil {
			log.WithField("message", err).Error("setting environment")
			if outputfile != "" {
				data := []string{}
				writeOutputFile(data, outputfile)
			}
			return nil
		}
		defer cancel()

		tasks, err := exp.ListTasks(ctx)
		if err != nil {
			log.WithField("message", err).Error("listing task")
			if outputfile != "" {
				data := []string{}
				writeOutputFile(data, outputfile)
			}
			return nil
		}

		if strings.ToLower(output) == "json" {
			printAsJSON(tasks)
			return nil
		}

		tw := tabwriter.NewWriter(os.Stdout, 1, 8, 1, '\t', 0)
		defer tw.Flush()

		displayFields := "NAMESPACE\tCONTAINER ID\tCONTAINER TYPE\tPID\tSTATUS"
		fmt.Fprintf(tw, "%v\n", displayFields)

		for _, t := range tasks {
			switch strings.ToLower(output) {
			case "json_line":
				printAsJSONLine(t)
			default:
				displayValues := fmt.Sprintf("%v\t%v\t%v\t%v\t%v",
					t.Namespace,
					t.Name,
					t.ContainerType,
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
func writeOutputFile(v interface{}, outputfile string) {
	data, _ := json.Marshal(v)
	ioutil.WriteFile(outputfile, data, 0644)
}
