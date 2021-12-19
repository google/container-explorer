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
	"os"
	"strings"
	"text/tabwriter"

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
		containerList,
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

var containerList = cli.Command{
	Name:        "list",
	Aliases:     []string{"ls"},
	Usage:       "list containers",
	Description: "list all containers",
	Flags: append([]cli.Flag{
		cli.BoolFlag{
			Name:  "skip-known-containers",
			Usage: "Skip known containers",
		},
	}),
	Action: func(clictx *cli.Context) error {

		// open bolt database
		ctx, _, db, cancel, err := ctrmeta.GetContainerEnvironment(clictx)
		if err != nil {
			log.Fatal(err)
		}
		defer cancel()

		store := metadata.NewContainerStore(metadata.NewDB(db, nil, nil))

		// use namespaces from the database
		nss, err := ctrmeta.GetNamespaces(ctx, db)
		if err != nil {
			log.Fatal(err)
		}
		if nss == nil {
			log.Info("namespace bucket does not exist")
		}

		tw := tabwriter.NewWriter(os.Stdout, 1, 8, 1, '\t', 0)
		defer tw.Flush()
		fmt.Fprintf(tw, "\nNAMESPACE\tCONTAINER NAME\tIMAGE\tCREATED AT\tLABELS\n")

		for _, ns := range nss {
			ctx = namespaces.WithNamespace(ctx, ns)
			var filters []string

			results, err := store.List(ctx, filters...)
			if err != nil {
				log.WithField("namespace", ns).Error(err)
				continue
			}

			// handle namespacess without containers
			if results == nil {
				fmt.Fprintf(tw, "%s\t%s\t%v\t%v\t%s\n",
					ns,
					"", // ID
					"", // Image
					"", // CreatedAt
					"") // labels

				continue
			}

			// handle namespaces with containers
			for _, result := range results {
				var labelStrings []string
				for k, v := range result.Labels {
					labelStrings = append(labelStrings, strings.Join([]string{k, v}, "="))
				}
				labels := strings.Join(labelStrings, ",")
				if labels == "" {
					labels = "-"
				}

				// Skip the known container images
				if clictx.Bool("skip-known-containers") {
					if isKnownContainerImage(result.Image) {
						continue
					}
				}

				fmt.Fprintf(tw, "%s\t%s\t%v\t%v\t%s\n",
					ns,
					result.ID,
					result.Image,
					result.CreatedAt.Format(tsLayout),
					labels)

			}
		} //__end_of_nss__

		// default return
		return nil
	},
}
