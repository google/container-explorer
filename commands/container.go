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
	"encoding/json"
	"fmt"

	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/metadata"
	"github.com/containerd/containerd/namespaces"
	"github.com/gogo/protobuf/types"
	"github.com/google/container-explorer/ctrmeta"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/urfave/cli"

	log "github.com/sirupsen/logrus"
)

var ContainerCommand = cli.Command{
	Name:    "container",
	Aliases: []string{"containers"},
	Usage:   "provide container related information",
	Subcommands: cli.Commands{
		containerInfo,
	},
}

var containerInfo = cli.Command{
	Name:        "info",
	Usage:       "show container info",
	Description: "show container configuration information",
	ArgsUsage:   "[flags] CONTAINER",
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:  "spec",
			Usage: "only display the spec",
		},
	},
	Action: func(clictx *cli.Context) error {

		var (
			ns          string // namespace
			containerid string
		)

		if clictx.NArg() < 1 {
			return fmt.Errorf("container id must be provided")
		}

		ns = clictx.GlobalString("namespace")
		containerid = clictx.Args().First()
		log.WithFields(log.Fields{
			"namespace":   ns,
			"containerid": containerid,
		}).Debug("info command")

		ctx, _, db, cancel, err := ctrmeta.GetContainerEnvironment(clictx)
		if err != nil {
			return err
		}
		defer cancel()

		ctx = namespaces.WithNamespace(ctx, ns)
		containerstore := metadata.NewContainerStore(metadata.NewDB(db, nil, nil))

		cinfo, err := containerstore.Get(ctx, containerid)
		if err != nil {
			log.Error("error getting container information", err)
			return err
		}
		log.WithFields(log.Fields{
			"id":    cinfo.ID,
			"image": cinfo.Image,
		}).Debug("snapshot information")

		// get container info

		if clictx.Bool("spec") {
			// print spec only
			v, err := parseSpec(cinfo.Spec)
			if err != nil {
				return err
			}
			printAsJSON(v)

			return nil
		}

		if cinfo.Spec != nil && cinfo.Spec.Value != nil {
			v, err := parseSpec(cinfo.Spec)
			if err != nil {
				return err
			}
			printAsJSON(struct {
				containers.Container
				Spec interface{} `json:"Spec,omitempty"`
			}{
				Container: cinfo,
				Spec:      v,
			})
		}
		return nil
	},
}

func parseSpec(any *types.Any) (interface{}, error) {
	var v spec.Spec
	json.Unmarshal(any.Value, &v)
	return v, nil
}

func printAsJSON(v interface{}) {
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		log.Error("error marshalling to JSON ", err)
		return
	}

	fmt.Println(string(b))
}
