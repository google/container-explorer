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

	log "github.com/sirupsen/logrus"

	"github.com/urfave/cli"
)

const tsLayout = "2006-01-02T15:04:05Z"

var ListCommand = cli.Command{
	Name:    "list",
	Aliases: []string{"ls"},
	Usage:   "Lists container related information",
	Subcommands: cli.Commands{
		listNamespaces,
		listContainers,
		listContent,
		listImages,
		listSnapshots,
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
		cli.BoolFlag{
			Name:  "skip-support-containers",
			Usage: "skip listing of the supporting containers created by Kubernetes",
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

		ctx, exp, cancel, err := explorerEnvironment(clictx)
		if err != nil {
			log.Fatal(err)
		}
		defer cancel()

		containers, err := exp.ListContainers(ctx)
		if err != nil {
			log.Fatal(err)
		}

		tw := tabwriter.NewWriter(os.Stdout, 1, 8, 1, '\t', 0)
		defer tw.Flush()

		displayFields := "NAMESPACE\tCONTAINER ID\tCONTAINER HOSTNAME\tIMAGE\tCREATED AT"
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

		for _, container := range containers {
			// skip kubernetes support containers created
			// by GKE, EKS, and AKS
			if clictx.Bool("skip-support-containers") {
				log.WithFields(log.Fields{
					"namespace":        container.Namespace,
					"containerid":      container.ID,
					"supportcontainer": container.SupportContainer,
				}).Debug("checking support container")

				if container.SupportContainer {
					continue
				}
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

			displayValues := fmt.Sprintf("%s\t%s\t%s\t%s\t%s",
				container.Namespace,
				container.ID,
				container.Hostname,
				container.Image,
				container.CreatedAt.Format(tsLayout),
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

		return nil
	},
}

var listImages = cli.Command{
	Name:        "images",
	Aliases:     []string{"image"},
	Usage:       "list images for all namespaces",
	Description: "list images for all namespaces",
	Action: func(clictx *cli.Context) error {

		ctx, exp, cancel, err := explorerEnvironment(clictx)
		if err != nil {
			log.Fatal(err)
		}
		defer cancel()

		images, err := exp.ListImages(ctx)
		if err != nil {
			log.Fatal(err)
		}

		tw := tabwriter.NewWriter(os.Stdout, 1, 8, 1, '\t', 0)
		defer tw.Flush()

		fmt.Fprintf(tw, "NAMESPACE\tNAME\tCREATED AT\tUPDATED AT\tDIGEST\tTYPE\n")
		for _, image := range images {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				image.Namespace,
				image.Name,
				image.CreatedAt.Format(tsLayout),
				image.UpdatedAt.Format(tsLayout),
				string(image.Target.Digest),
				image.Target.MediaType,
			)
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

		ctx, exp, cancel, err := explorerEnvironment(clictx)
		if err != nil {
			log.Fatal(err)
		}
		defer cancel()

		content, err := exp.ListContent(ctx)
		if err != nil {
			log.Fatal(err)
		}

		tw := tabwriter.NewWriter(os.Stdout, 1, 8, 1, '\t', 0)
		defer tw.Flush()

		fmt.Fprintf(tw, "\nNAMESPACE\tDIGEST\tSIZE\tCREATED AT\tUPDATED AT\tLABELS\n")
		for _, c := range content {
			fmt.Fprintf(tw, "%s\t%s\t%v\t%v\t%v\t%s\n",
				c.Namespace,
				c.Digest,
				c.Size,
				c.CreatedAt.Format(tsLayout),
				c.UpdatedAt.Format(tsLayout),
				labelString(c.Labels),
			)
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

		ctx, exp, cancel, err := explorerEnvironment(clictx)
		if err != nil {
			log.Fatal(err)
		}
		defer cancel()

		ss, err := exp.ListSnapshots(ctx)
		if err != nil {
			log.Fatal(err)
		}

		tw := tabwriter.NewWriter(os.Stdout, 1, 8, 1, '\t', 0)
		defer tw.Flush()

		displayFields := "NAMESPACE\tSNAPSHOTTER\tCREATED AT\tUPDATED AT\tKIND\tNAME\tPARENT\tLAYER PATH"
		if !clictx.Bool("no-labels") {
			displayFields = fmt.Sprintf("%s\tLABELS", displayFields)
		}
		fmt.Fprintf(tw, "\n%v\n", displayFields)

		for _, s := range ss {
			ssfilepath := s.OverlayPath
			if clictx.Bool("full-overlay-path") {
				ssfilepath = filepath.Join(exp.SnapshotRoot(s.Snapshotter), ssfilepath)
			}
			displayValue := fmt.Sprintf("%v\t%v\t%v\t%v\t%v\t%v\t%v\t%v",
				s.Namespace,
				s.Snapshotter,
				s.CreatedAt.Format(tsLayout),
				s.UpdatedAt.Format(tsLayout),
				s.Kind,
				s.Key,
				s.Parent,
				ssfilepath,
			)

			if !clictx.Bool("no-labels") {
				displayValue = fmt.Sprintf("%v\t%v", displayValue, labelString(s.Labels))
			}
			fmt.Fprintf(tw, "%v\n", displayValue)
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
