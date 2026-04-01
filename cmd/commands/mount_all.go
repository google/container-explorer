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
	"runtime"
	"strings"

	"github.com/google/container-explorer/explorers"
	"github.com/google/container-explorer/explorers/containerd"
	"github.com/google/container-explorer/explorers/docker"
	"github.com/google/container-explorer/explorers/podman"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

var MountAllCommand = cli.Command{
	Name:        "mount-all",
	Aliases:     []string{"mount_all"},
	Usage:       "mount all containers",
	Description: "mount all containers to subdirectories with the specified mount point",
	ArgsUsage:   "[flag] MOUNT_POINT",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "container-engine, e",
			Usage: "support container engines are docker, containerd, and podman",
			Value: "all",
		},
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
			return fmt.Errorf("mounting a container is only supported on Linux")
		}

		if clictx.NArg() < 1 {
			return fmt.Errorf("mount point is required")
		}

		mountpoint := clictx.Args().First()
		containerEngine := clictx.String("container-engine")
		filter := clictx.String("filter")
		skipSupportContainer := !clictx.Bool("mount-support-containers")

		ctx, runtimeConfig, err := parseRuntimeConfig(clictx)
		if err != nil {
			return fmt.Errorf("setting container explorer environment")
		}

		imageRootDir := runtimeConfig["imageRootDir"].(string)
		containerdRootDir := runtimeConfig["containerdRootDir"].(string)
		dockerRootDir := runtimeConfig["dockerRootDir"].(string)
		layercache := runtimeConfig["layercache"].(string)
		sc := runtimeConfig["supportContainerData"].(*explorers.SupportContainer)

		log.WithFields(log.Fields{
			"containerEngine":      containerEngine,
			"filter":               filter,
			"skipSupportContainer": skipSupportContainer,
		}).Debug("mounting all containers")

		dkrxplr, err := docker.NewExplorer(imageRootDir, containerdRootDir, dockerRootDir)
		if err != nil {
			log.Errorf("getting docker explorer: %v", err)
		} else {
			if containerEngine == "all" || strings.ToLower(containerEngine) == "docker" {
				if err := mountAllContainers(ctx, dkrxplr, mountpoint, filter, skipSupportContainer); err != nil {
					log.Errorf("mounting docker containers: %v", err)
				}
			}
		}

		pmxplr, err := podman.NewExplorer(imageRootDir)
		if err != nil {
			log.Errorf("getting podman explorer: %v", err)
		} else {
			if containerEngine == "all" || strings.ToLower(containerEngine) == "podman" {
				if err := mountAllContainers(ctx, pmxplr, mountpoint, filter, skipSupportContainer); err != nil {
					log.Errorf("mounting podman containers: %v", err)
				}
			}
		}

		ctrxplr, err := containerd.NewExplorer(imageRootDir, containerdRootDir, dockerRootDir, layercache, sc)
		if err != nil {
			log.Errorf("getting containerd explorer: %v", err)
		} else {
			if containerEngine == "all" || strings.ToLower(containerEngine) == "containerd" {
				if err := mountAllContainers(ctx, ctrxplr, mountpoint, filter, skipSupportContainer); err != nil {
					log.Errorf("mounting containerd containers: %v", err)
				}
			}
		}

		return nil // default
	},
}

func mountAllContainers(ctx context.Context, xplr explorers.ContainerExplorer, mountpoint string, filter string, skipSupportContainer bool) error {
	return xplr.MountAllContainers(ctx, mountpoint, filter, skipSupportContainer)
}
