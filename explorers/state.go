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

import "time"

// State holds runc state information.
//
// runc state file is located at `/run/containerd/runc/<namespace>/<container_id>/state.json`.
//
// The State structure only maps the required attributes form state.json.
type State struct {
	ID                  string                 `json:"state,omitempty"`
	InitProcessPid      int                    `json:"init_process_pid"`
	InitProcessstart    int                    `json:"init_process_start"`
	Created             time.Time              `json:"created"`
	Config              map[string]interface{} `json:"config"`
	Rootless            bool                   `json:"rootless"`
	CgroupPaths         map[string]string      `json:"cgroup_paths"`
	NamespacePaths      map[string]string      `json:"namespace_paths"`
	ExternalDescriptors []string               `json:"external_descriptors"`
	IntelRdtPath        string                 `json:"intel_rdt_path"`
}
