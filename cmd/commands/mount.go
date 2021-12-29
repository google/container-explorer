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

	"github.com/containerd/containerd/namespaces"
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

		namespace := clictx.GlobalString("namespace")
		containerid := clictx.Args().First()
		mountpoint := clictx.Args().Get(1)

		log.WithFields(log.Fields{
			"namespace":   namespace,
			"containerid": containerid,
			"mountpoint":  mountpoint,
		}).Debug("user provided mount options")

		ctx, exp, cancel, err := explorerEnvironment(clictx)
		if err != nil {
			return err
		}
		defer cancel()

		ctx = namespaces.WithNamespace(ctx, namespace)

		if err := exp.MountContainer(ctx, containerid, mountpoint); err != nil {
			return err
		}

		// default return
		return nil
	},
}
