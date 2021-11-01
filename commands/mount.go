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
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/containerd/containerd/metadata"
	"github.com/containerd/containerd/namespaces"
	"github.com/google/container-explorer/ctrmeta"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

var MountCommand = cli.Command{
	Name:        "mount",
	Usage:       "mount container",
	Description: "mount container image",
	ArgsUsage:   "[flags] container_id mount_point",
	Action: func(clictx *cli.Context) error {

		// check supported OS
		if runtime.GOOS != "linux" {
			fmt.Printf("Mounting container is currently supported only on Linux\n")
			return nil
		}

		var (
			ns          string // namespace where container exists
			containerid string
			mountpoint  string
			err         error
		)

		if clictx.NArg() < 2 {
			return fmt.Errorf("containerid and mountpoint are required")
		}

		ns = clictx.GlobalString("namespace")
		containerid = clictx.Args().First()
		mountpoint = clictx.Args().Get(1)

		log.WithFields(log.Fields{
			"namespace":   ns,
			"containerid": containerid,
			"mountpoint":  mountpoint,
		}).Debug("user mount command")

		if !isValidMountPoint(mountpoint) {
			return fmt.Errorf("invalid mount point")
		}

		ctx, _, db, cancel, err := ctrmeta.GetContainerEnvironment(clictx)
		if err != nil {
			return err
		}
		defer cancel()

		ctx = namespaces.WithNamespace(ctx, ns)

		containerstore := metadata.NewContainerStore(metadata.NewDB(db, nil, nil))
		cinfo, err := containerstore.Get(ctx, containerid)
		if err != nil {
			log.Error("error getting container information ", err)
			return err
		}

		log.WithFields(log.Fields{
			"id":          cinfo.ID,
			"snapshotter": cinfo.Snapshotter,
			"snapshotkey": cinfo.SnapshotKey,
			"image":       cinfo.Image,
		}).Debug("snapshot information for container ", containerid)

		snapshotroot, sdb, cancel, err := ctrmeta.ContainerSnapshotEnvironment(clictx, cinfo)
		if err != nil {
			return err
		}
		defer cancel()
		log.WithField("path", snapshotroot).Debug("snapshotter root directory")

		parents, err := ctrmeta.ContainerParents(ctx, db, cinfo)
		if err != nil {
			return err
		}

		lowerdir, upperdir, workdir := ctrmeta.ContainerOverlayPaths(ctx, parents, snapshotroot, sdb, cinfo)
		log.WithFields(log.Fields{
			"lowerdir": lowerdir,
			"upperdir": upperdir,
			"workdir":  workdir,
		}).Debug("overlay directories")

		if lowerdir == "" || upperdir == "" || workdir == "" {
			return fmt.Errorf("lowerdir is empty")
		}

		opts := fmt.Sprintf("ro,lowerdir=%s:%s", lowerdir, upperdir)

		// TODO (rmaskey): Use github.com/containerd/containerd/mount.Mount to mount the container
		// m := mount.Mount{
		//	Type:    "overlay",
		//	Source:  "overlay",
		//	Options: []string{"ro", opts},
		//}
		mountArgs := []string{"-t", "overlay", "overlay", "-o", opts, mountpoint}
		log.Debug("mount command options: ", mountArgs)
		cmd := exec.Command("mount", mountArgs...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			log.Error("error running mount command ", err)
			return err
		}

		if string(out) != "" {
			log.Info("mount command output ", string(out))
		}
		return nil
	},
}

var MountAllCommand = cli.Command{
	Name:        "mount-all",
	Usage:       "mount all containers",
	Description: "mount all containers",
	ArgsUsage:   "mount-all mount_point",
	Action: func(clictx *cli.Context) error {
		// check supported OS
		if runtime.GOOS != "linux" {
			fmt.Printf("Mount containers are currently supported only on Linux\n")
			return nil
		}

		// mountpoint where subdirectories are created
		// for each container and mounted on the respective
		// container.
		//
		// For example, if the mountpoint is /mnt/container
		// then following subdirectories are created
		//     - container nginx-demo -> /mnt/container/nginx-demo
		//     - container redis-demo -> /mnt/container/redis-demo
		var mountpoint string
		var err error

		if clictx.NArg() != 1 {
			return fmt.Errorf("mount-all command requires mount_point")
		}

		mountpoint = clictx.Args().First()
		log.WithField("mountpoint", mountpoint).Debug("user mount command")

		ctx, _, db, cancel, err := ctrmeta.GetContainerEnvironment(clictx)
		if err != nil {
			return nil
		}
		defer cancel()

		nss, err := ctrmeta.GetNamespaces(ctx, db)
		if err != nil {
			return nil
		}
		if nss == nil {
			return fmt.Errorf("no namespaces in the bucket")
		}

		containerstore := metadata.NewContainerStore(metadata.NewDB(db, nil, nil))

		for _, ns := range nss {
			ctx = namespaces.WithNamespace(ctx, ns)
			cinfos, err := containerstore.List(ctx)
			if err != nil {
				log.WithField("namespace", ns).Error("error encountered listing containers ", err)
				continue
			}
			if cinfos == nil {
				log.WithField("namespace", ns).Warn("no container information")
				continue
			}

			// Each container may have a different
			// snapshotter so iterating over each
			// container.
			for _, cinfo := range cinfos {
				snapshotroot, sdb, cancel, err := ctrmeta.ContainerSnapshotEnvironment(clictx, cinfo)
				if err != nil {
					log.WithFields(log.Fields{
						"namespace":     ns,
						"containerid":   cinfo.ID,
						"snapshootroot": snapshotroot,
						"sdb":           sdb,
					}).Error("error getting snapshot environment")

					// close handle to sdb and continue
					cancel()
					continue
				}
				log.WithFields(log.Fields{
					"namespace":    ns,
					"containerid":  cinfo.ID,
					"snapshotroot": snapshotroot,
				}).Debug("snapshotter root directory")

				parents, err := ctrmeta.ContainerParents(ctx, db, cinfo)
				if err != nil {
					log.WithFields(log.Fields{
						"namespace":   ns,
						"containerid": cinfo.ID,
					}).Error(err)

					// close and continue
					cancel()
					continue
				}
				if parents == nil {
					log.WithFields(log.Fields{
						"namespace":    ns,
						"contianerid":  cinfo.ID,
						"snapshotroot": snapshotroot,
					}).Warn("empty container parent")

					// close and continue
					cancel()
					continue
				}

				lowerdir, upperdir, workdir := ctrmeta.ContainerOverlayPaths(ctx, parents, snapshotroot, sdb, cinfo)
				log.WithFields(log.Fields{
					"namespace":   ns,
					"containerid": cinfo.ID,
					"lowerdir":    lowerdir,
					"upperdir":    upperdir,
					"workdir":     workdir,
				}).Debug("overlay directories")
				if lowerdir == "" {
					log.Error("lowerdir is empty")
					// close and continue
					cancel()
					continue
				}

				opts := fmt.Sprintf("ro,lowerdir=%s:%s", lowerdir, upperdir)

				ctrmountpoint := filepath.Join(mountpoint, cinfo.ID)
				err = os.MkdirAll(ctrmountpoint, 0755)
				if err != nil {
					log.WithFields(log.Fields{
						"namespace":   ns,
						"containerid": cinfo.ID,
						"mountpoint":  ctrmountpoint,
					}).Error("creating mountpoint for container")

					// close and exit
					//
					// The most likey reason for not able to create
					// the ctrmountpoint is permission.
					cancel()
					return err
				}

				mountArgs := []string{"-t", "overlay", "overlay", "-o", opts, ctrmountpoint}
				log.Debug("mount command options: ", mountArgs)
				cmd := exec.Command("mount", mountArgs...)
				out, err := cmd.CombinedOutput()
				if err != nil {
					log.Error("mounting a container ", err)
					// close and continue
					cancel()
					continue
				}
				if string(out) != "" {
					log.Info("mounting a container ", string(out))
				}

				// close database
				cancel()
			}

		}
		return nil
	},
}

// isValidMountPoint checks if the user provided mountpoint
// exists.
func isValidMountPoint(mountpoint string) bool {
	info, err := os.Stat(mountpoint)
	if err != nil {
		log.Error("error validating mountpoint ", err)
		return false
	}
	if info.IsDir() {
		return true
	}
	return false
}
