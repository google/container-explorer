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

package utils

import "github.com/google/container-explorer/explorers"

func IgnoreContainer(container explorers.Container, filter map[string]string) bool {
	ignore := false

	for k, v := range filter {
		containerLabel, ok := container.Labels[k]
		if !ok {
			// Container label does not exist. Check next label.
			continue
		}
		if containerLabel == v {
			ignore = true
			break
		}
	}

	return ignore
}