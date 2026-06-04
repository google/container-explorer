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

	"github.com/google/container-explorer/explorers"

	log "github.com/sirupsen/logrus"

	"github.com/urfave/cli"
)

var InfoCommand = cli.Command{
	Name:        "info",
	Usage:       "show internal information",
	Description: "show internal information",
	Subcommands: cli.Commands{
		infoContainer,
	},
}

var infoContainer = cli.Command{
	Name:        "container",
	Usage:       "show container internal information",
	Description: "show container internal information",
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:  "spec, s",
			Usage: "show only container spec",
		},
	},
	Action: func(clictx *cli.Context) error {

		if clictx.NArg() < 1 {
			return fmt.Errorf("container id is required")
		}

		containerID := clictx.Args().First()
		spec := clictx.Bool("spec")

		matched, err := ForMatchingContainer(GlobalConfig.Context, containerID, func(xplr explorers.ContainerExplorer) error {
			info, err := xplr.InfoContainer(GlobalConfig.Context, containerID, spec)
			if err != nil {
				return err
			}
			printAsJSON(info)
			return nil
		})

		if !matched {
			log.Errorf("container %s not found", containerID)
		}
		return err
	},
}

var InspectCommand = cli.Command{
	Name:        "inspect",
	Usage:       "show container internal information",
	Description: "show container internal information",
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:  "spec, s",
			Usage: "show only container spec",
		},
	},
	Action: func(clictx *cli.Context) error {

		if clictx.NArg() < 1 {
			return fmt.Errorf("container id is required")
		}

		containerID := clictx.Args().First()
		spec := clictx.Bool("spec")

		matched, err := ForMatchingContainer(GlobalConfig.Context, containerID, func(xplr explorers.ContainerExplorer) error {
			info, err := xplr.InfoContainer(GlobalConfig.Context, containerID, spec)
			if err != nil {
				return err
			}
			printAsJSON(info)
			return nil
		})

		if !matched {
			log.Errorf("container %s not found", containerID)
		}
		return err
	},
}
