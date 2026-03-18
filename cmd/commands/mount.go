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

	"github.com/google/container-explorer/explorers"
	"github.com/google/container-explorer/explorers/containerd"
	"github.com/google/container-explorer/explorers/docker"
	"github.com/google/container-explorer/explorers/podman"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

var MountCommand = cli.Command{
	Name:        "mount",
	Usage:       "mount a container to a mount point",
	Description: "mount a container to a mount point",
	ArgsUsage:   "ID MOUNTPOINT",
	Action: func(clictx *cli.Context) error {

		// Mounting a container is only supported on a Linux operating system.
		if runtime.GOOS != "linux" {
			return fmt.Errorf("mounting a container is only supported on Linux")
		}

		if clictx.NArg() < 2 {
			return fmt.Errorf("container id and mount point are required")
		}

		containerid := clictx.Args().First()
		mountpoint := clictx.Args().Get(1)

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

		dkrxplr, err := docker.NewExplorer(imageRootDir, containerdRootDir, dockerRootDir)
		if err != nil {
			log.Error("unable to get docker explorer")
		} else {
			matched, err := mountContainer(ctx, dkrxplr, containerid, mountpoint)
			if matched {
				return err
			}
		}

		pmxplr, err := podman.NewExplorer(imageRootDir)
		if err != nil {
			log.Error("unable to get podman explorer")
		} else {
			matched, err := mountContainer(ctx, pmxplr, containerid, mountpoint)
			if matched {
				return err
			}
		}

		ctrxplr, err := containerd.NewExplorer(imageRootDir, containerdRootDir, dockerRootDir, layercache, sc)
		if err != nil {
			log.Error("unable to get containerd explorer")
		} else {
			matched, err := mountContainer(ctx, ctrxplr, containerid, mountpoint)
			if matched {
				return err
			}
		}

		return nil
	},
}

func mountContainer(ctx context.Context, xplr explorers.ContainerExplorer, containerid string, mountpoint string) (bool, error) {
	container, err := xplr.GetContainerByID(ctx, containerid)
	if err != nil {
		return false, err
	}

	if container == nil {
		return false, fmt.Errorf("container is nil")
	}

	if err := xplr.MountContainer(ctx, containerid, mountpoint); err != nil {
		return true, fmt.Errorf("mounting %s: %w", containerid, err)
	}

	return true, nil
}
