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

package explorers

import "github.com/containerd/containerd/containers"

// Container provides information about a container.
type Container struct {
	Namespace        string
	Hostname         string
	ImageBase        string
	SupportContainer bool
	ContainerType    string
	ProcessID        int
	Status           string

	// containerd specific fields
	containers.Container

	// docker specific fields
	Running      bool
	ExposedPorts []string
}

type Drift struct {
    ContainerID       string
    AddedOrModified   []string
    InaccessibleFiles []string
}
