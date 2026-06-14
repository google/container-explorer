/*
Copyright 2026 Google LLC

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

import (
	"testing"

	"github.com/containerd/containerd/containers"
	"github.com/google/container-explorer/explorers"
)

func TestIgnoreContainer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		container explorers.Container
		filter    map[string]string
		want      bool
	}{
		{
			name: "Empty filter",
			container: explorers.Container{
				Container: containers.Container{
					Labels: map[string]string{"app": "nginx"},
				},
			},
			filter: map[string]string{},
			want:   false,
		},
		{
			name: "Nil filter",
			container: explorers.Container{
				Container: containers.Container{
					Labels: map[string]string{"app": "nginx"},
				},
			},
			filter: nil,
			want:   false,
		},
		{
			name: "Filter matches",
			container: explorers.Container{
				Container: containers.Container{
					Labels: map[string]string{"app": "nginx", "env": "prod"},
				},
			},
			filter: map[string]string{"app": "nginx"},
			want:   true,
		},
		{
			name: "Filter does not match",
			container: explorers.Container{
				Container: containers.Container{
					Labels: map[string]string{"app": "nginx", "env": "prod"},
				},
			},
			filter: map[string]string{"app": "apache"},
			want:   false,
		},
		{
			name: "Container missing label",
			container: explorers.Container{
				Container: containers.Container{
					Labels: map[string]string{"env": "prod"},
				},
			},
			filter: map[string]string{"app": "nginx"},
			want:   false,
		},
		{
			name: "Multiple filters, one matches (OR logic)",
			container: explorers.Container{
				Container: containers.Container{
					Labels: map[string]string{"app": "nginx", "env": "dev"},
				},
			},
			filter: map[string]string{"app": "nginx", "env": "prod"},
			want:   true,
		},
		{
			name: "Multiple filters, none match",
			container: explorers.Container{
				Container: containers.Container{
					Labels: map[string]string{"app": "nginx", "env": "dev"},
				},
			},
			filter: map[string]string{"app": "apache", "env": "prod"},
			want:   false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IgnoreContainer(tt.container, tt.filter)
			if got != tt.want {
				t.Errorf("IgnoreContainer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIncludeContainer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		container explorers.Container
		filter    map[string]string
		want      bool
	}{
		{
			name: "Nil filter (should include)",
			container: explorers.Container{
				Container: containers.Container{
					Labels: map[string]string{"app": "nginx"},
				},
			},
			filter: nil,
			want:   true,
		},
		{
			name: "Empty filter (should not include)",
			container: explorers.Container{
				Container: containers.Container{
					Labels: map[string]string{"app": "nginx"},
				},
			},
			filter: map[string]string{},
			want:   false,
		},
		{
			name: "Filter matches",
			container: explorers.Container{
				Container: containers.Container{
					Labels: map[string]string{"app": "nginx", "env": "prod"},
				},
			},
			filter: map[string]string{"app": "nginx"},
			want:   true,
		},
		{
			name: "Filter does not match",
			container: explorers.Container{
				Container: containers.Container{
					Labels: map[string]string{"app": "nginx", "env": "prod"},
				},
			},
			filter: map[string]string{"app": "apache"},
			want:   false,
		},
		{
			name: "Container missing label",
			container: explorers.Container{
				Container: containers.Container{
					Labels: map[string]string{"env": "prod"},
				},
			},
			filter: map[string]string{"app": "nginx"},
			want:   false,
		},
		{
			name: "Multiple filters, one matches (OR logic)",
			container: explorers.Container{
				Container: containers.Container{
					Labels: map[string]string{"app": "nginx", "env": "dev"},
				},
			},
			filter: map[string]string{"app": "nginx", "env": "prod"},
			want:   true,
		},
		{
			name: "Multiple filters, none match",
			container: explorers.Container{
				Container: containers.Container{
					Labels: map[string]string{"app": "nginx", "env": "dev"},
				},
			},
			filter: map[string]string{"app": "apache", "env": "prod"},
			want:   false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IncludeContainer(tt.container, tt.filter)
			if got != tt.want {
				t.Errorf("IncludeContainer() = %v, want %v", got, tt.want)
			}
		})
	}
}
