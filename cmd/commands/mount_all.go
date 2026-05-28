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
	"runtime"
	"strings"

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

		log.WithFields(log.Fields{
			"containerEngine":      containerEngine,
			"filter":               filter,
			"skipSupportContainer": skipSupportContainer,
		}).Debug("mounting all containers")

		exps := GetExplorers()
		for _, xplr := range exps {
			engineName := xplr.Type()
			if containerEngine == "all" || strings.ToLower(containerEngine) == engineName {
				if err := xplr.MountAllContainers(GlobalConfig.Context, mountpoint, filter, skipSupportContainer); err != nil {
					log.Errorf("mounting %s containers: %v", engineName, err)
				}
			}
		}

		return nil // default
	},
}
