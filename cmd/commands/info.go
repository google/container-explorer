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

	log "github.com/sirupsen/logrus"

	"github.com/containerd/containerd/namespaces"
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

		var (
			namespace   string
			containerid string
		)

		namespace = clictx.GlobalString("namespace")
		containerid = clictx.Args().First()

		ctx, exp, cancel, err := explorerEnvironment(clictx)
		if err != nil {
			log.Fatal(err)
		}
		defer cancel()

		ctx = namespaces.WithNamespace(ctx, namespace)

		info, err := exp.InfoContainer(ctx, containerid, clictx.Bool("spec"))
		if err != nil {
			log.Fatal(err)
		}

		printAsJSON(info)

		return nil
	},
}

func printAsJSON(v interface{}) {
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		log.Error("error marshalling to JSON", err)
		return
	}

	fmt.Println(string(b))
}

func printAsJSONLine(v interface{}) {
	b, err := json.Marshal(v)
	if err != nil {
		log.Error("error marshalling to json_line", err)
		return
	}
	fmt.Println(string(b))
}
