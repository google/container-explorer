/*
Copyright 2025 Google LLC

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
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/google/container-explorer/explorers"
	"github.com/google/container-explorer/explorers/containerd"
	"github.com/google/container-explorer/explorers/docker"
	"github.com/google/container-explorer/explorers/podman"

	log "github.com/sirupsen/logrus"
)

// GetExplorers returns a slice of all initialized container explorers.
func GetExplorers() []explorers.ContainerExplorer {
	var allExplorers []explorers.ContainerExplorer

	// Docker
	dkrxplr, err := docker.NewExplorer(GlobalConfig.ImageRootDir, GlobalConfig.ContainerdRootDir, GlobalConfig.DockerRootDir)
	if err != nil {
		log.Debugf("unable to get docker explorer: %v", err)
	} else {
		allExplorers = append(allExplorers, dkrxplr)
	}

	// Podman
	pmxplr, err := podman.NewExplorer(GlobalConfig.ImageRootDir)
	if err != nil {
		log.Debugf("unable to get podman explorer: %v", err)
	} else {
		allExplorers = append(allExplorers, pmxplr)
	}

	// Containerd
	ctrxplr, err := containerd.NewExplorer(GlobalConfig.ImageRootDir, GlobalConfig.ContainerdRootDir, GlobalConfig.DockerRootDir, GlobalConfig.LayerCache, GlobalConfig.SupportContainerData)
	if err != nil {
		log.Debugf("unable to get containerd explorer: %v", err)
	} else {
		allExplorers = append(allExplorers, ctrxplr)
	}

	return allExplorers
}

// ForMatchingContainer finds an explorer that has the given containerID and executes the provided function.
func ForMatchingContainer(ctx context.Context, containerID string, fn func(explorers.ContainerExplorer) error) (bool, error) {
	exps := GetExplorers()
	for _, exp := range exps {
		container, err := exp.GetContainerByID(ctx, containerID)
		if err != nil {
			log.Debugf("error checking container ID %s in explorer: %v", containerID, err)
			continue
		}
		if container != nil {
			return true, fn(exp)
		}
	}
	return false, fmt.Errorf("container %s not found", containerID)
}

func getFilterMap(filter string) map[string]string {
	if filter == "" {
		return nil
	}

	filterMap := make(map[string]string)
	filters := strings.Split(filter, ",")
	for _, filter := range filters {
		parts := strings.Split(filter, "=")
		if len(parts) == 2 {
			filterMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return filterMap
}

func printAsJSON(v any) {
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		log.Error("error marshaling to JSON", err)
		return
	}

	fmt.Println(string(b))
}

func printAsJSONLine(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		log.Error("error marshaling to json_line", err)
		return
	}
	fmt.Println(string(b))
}

// labelString returns a string of comma separated key-value pairs.
func labelString(labels map[string]string) string {
	var lablestrings []string

	for k, v := range labels {
		lablestrings = append(lablestrings, strings.Join([]string{k, v}, "="))
	}
	return strings.Join(lablestrings, ",")
}

// arrayToString returns a string of comma separated value of an array.
func arrayToString(array []string) string {
	var result string

	for i, val := range array {
		if i == 0 {
			result = val
			continue
		}
		result = fmt.Sprintf("%s,%s", result, val)
	}

	return result
}

// writeOutputFile writes JSON data to specified file.
func writeOutputFile(v any, outputfile string) {
	data, _ := json.Marshal(v)
	os.WriteFile(outputfile, data, 0644)
}
