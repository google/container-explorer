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

	"github.com/google/container-explorer/commands"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

const (
	VERSION = "0.0.1"
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
	app.Usage = "standalone container explorer"
	app.Description = "standalone container explorer"

	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "debug",
			Usage: "enable debug messages",
		},
		cli.StringFlag{
			Name:  "container-root, c",
			Usage: "containerd root directory",
			Value: "/var/lib/containerd",
		},
		cli.StringFlag{
			Name:  "image-root, i",
			Usage: "mounted virtual machine image root directory",
		},
		cli.StringFlag{
			Name:  "manifest-file, m",
			Usage: "containerd manifest meta.db",
		},
		cli.StringFlag{
			Name:  "snapshot-metadata-file, s",
			Usage: "containerd snapshot metadata database metadata.db",
		},
		cli.StringFlag{
			Name:  "namespace, n",
			Usage: "containerd namespace required with mount command",
			Value: "default",
		},
	}

	app.Commands = []cli.Command{
		commands.ListCommand,
		commands.MountCommand,
		commands.MountAllCommand,
	}

	app.Before = func(context *cli.Context) error {
		if context.GlobalBool("debug") {
			log.SetLevel(log.DebugLevel)
		}
		return nil
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
