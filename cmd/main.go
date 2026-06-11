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

package main

import (
	"os"

	cecommands "github.com/google/container-explorer/cmd/commands"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

const (
	VERSION = "0.6.0"
)

func init() {
	log.SetFormatter(&log.TextFormatter{})
	log.SetOutput(os.Stdout)
	log.SetLevel(log.WarnLevel)
}

func main() {
	app := cli.NewApp()

	app.Name = "container-explorer"
	app.Version = VERSION
	app.Usage = "A standalone utility for exploring container details"
	app.Description = `A standalone utility for exploring container details.
	Container Explorer supports containers managed by containerd, Docker, podman,
	and Kubernetes.
	`
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "debug, d",
			Usage: "enable debug messages",
		},
		cli.StringFlag{
			Name:  "containerd-root, c",
			Usage: "specify containerd root directory",
		},
		cli.StringFlag{
			Name:  "image-root, i",
			Usage: "specify mount point for a disk image",
		},
		cli.BoolFlag{
			Name:  "use-layer-cache, u",
			Usage: "attempt to use cached layers where layers are symlinks",
		},
		cli.StringFlag{
			Name:  "layer-cache, l",
			Usage: "cached layer folder within the snapshot root",
			Value: "layers",
		},
		cli.StringFlag{
			Name:  "docker-root, D",
			Usage: "specify docker root directory",
		},
		cli.StringFlag{
			Name:  "support-container-data, s",
			Usage: "a yaml file containing information about support containers",
		},
		cli.StringFlag{
			Name:  "output",
			Usage: "output format in json, table. Default is table",
			Value: "table",
		},
		cli.StringFlag{
			Name:  "output-file, o",
			Usage: "output file to save the content",
		},
	}

	app.Commands = []cli.Command{
		cecommands.ListCommand,
		cecommands.InfoCommand,
		cecommands.InspectCommand,
		cecommands.MountCommand,
		cecommands.DriftCommand,
		cecommands.ExportCommand,
	}

	app.Before = func(clictx *cli.Context) error {
		if clictx.GlobalBool("debug") {
			log.SetLevel(log.DebugLevel)
		}
		return cecommands.InitializeRuntime(clictx)
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
