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

import "strings"

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