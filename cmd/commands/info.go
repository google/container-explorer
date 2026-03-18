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
	"context"
	"fmt"

	"github.com/google/container-explorer/explorers"
	"github.com/google/container-explorer/explorers/containerd"
	"github.com/google/container-explorer/explorers/docker"
	"github.com/google/container-explorer/explorers/podman"

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
			Name:  "spec",
			Usage: "show only container spec",
		},
	},
	Action: func(clictx *cli.Context) error {

		if clictx.NArg() < 1 {
			return fmt.Errorf("container id is required")
		}

		containerid := clictx.Args().First()

		ctx, runtimeConfig, err := parseRuntimeConfig(clictx)
		if err != nil {
			log.WithField("message", err).Error("setting container explorer environment")
			return nil
		}

		imageRootDir := runtimeConfig["imageRootDir"].(string)
		containerdRootDir := runtimeConfig["containerdRootDir"].(string)
		dockerRootDir := runtimeConfig["dockerRootDir"].(string)
		layercache := runtimeConfig["layercache"].(string)
		sc := runtimeConfig["supportContainerData"].(*explorers.SupportContainer)

		spec := clictx.Bool("spec")

		dkrxplr, err := docker.NewExplorer(imageRootDir, containerdRootDir, dockerRootDir)
		if err != nil {
			log.Error("unable to get docker explorer")
		} else {
			matched, info, err := getContainerInfo(ctx, dkrxplr, containerid, spec)
			if matched {
				if err != nil {
					log.Fatal(err)
				}
				printAsJSON(info)
				return nil
			}
		}

		pmxplr, err := podman.NewExplorer(imageRootDir)
		if err != nil {
			log.Error("unable to get podman explorer")
		} else {
			matched, info, err := getContainerInfo(ctx, pmxplr, containerid, spec)
			if matched {
				if err != nil {
					log.Fatal(err)
				}
				printAsJSON(info)
				return nil
			}
		}

		ctrxplr, err := containerd.NewExplorer(imageRootDir, containerdRootDir, dockerRootDir, layercache, sc)
		if err != nil {
			log.Error("unable to get containerd explorer")
		} else {
			matched, info, err := getContainerInfo(ctx, ctrxplr, containerid, spec)
			if matched {
				if err != nil {
					log.Fatal(err)
				}
				printAsJSON(info)
				return nil
			}
		}

		log.Errorf("container %s not found", containerid)
		return nil
	},
}

func getContainerInfo(ctx context.Context, xplr explorers.ContainerExplorer, containerid string, spec bool) (bool, interface{}, error) {
	container, err := xplr.GetContainerByID(ctx, containerid)
	if err != nil {
		return false, nil, err
	}

	if container == nil {
		return false, nil, fmt.Errorf("container is nil")
	}

	info, err := xplr.InfoContainer(ctx, containerid, spec)
	if err != nil {
		return true, nil, fmt.Errorf("getting info for %s: %w", containerid, err)
	}

	return true, info, nil
}

