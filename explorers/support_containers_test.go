/*
Copyright 2026 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
/usr/local/google/home/jasonsolomon/git/container-explorer/explorers/support_containers_test.go
    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package explorers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/containerd/containerd/containers"
)

const sampleSupportContainerYAML = `
names:
  - "k8s_POD"
  - "pause"
images:
  - "gcr.io/google-containers/pause"
  - "k8s.gcr.io/pause"
labels:
  - "io.kubernetes.docker.type=podsandbox"
  - "annotation.kubernetes.io/config.source=api"
`

func TestLoadSupportContainerFromFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "support_containers.yaml")

	if err := os.WriteFile(yamlPath, []byte(sampleSupportContainerYAML), 0600); err != nil {
		t.Fatalf("failed to write sample YAML: %v", err)
	}

	// Test case 1: Valid file
	sc, err := LoadSupportContainerFromFile(yamlPath)
	if err != nil {
		t.Fatalf("LoadSupportContainerFromFile failed: %v", err)
	}

	expectedNames := []string{"k8s_POD", "pause"}
	expectedImages := []string{"gcr.io/google-containers/pause", "k8s.gcr.io/pause"}
	expectedLabels := []string{"io.kubernetes.docker.type=podsandbox", "annotation.kubernetes.io/config.source=api"}

	compareSlices(t, sc.ContainerNames, expectedNames, "ContainerNames")
	compareSlices(t, sc.ImageNames, expectedImages, "ImageNames")
	compareSlices(t, sc.Labels, expectedLabels, "Labels")

	// Test case 2: Non-existent file
	_, err = LoadSupportContainerFromFile(filepath.Join(tmpDir, "non_existent.yaml"))
	if err == nil {
		t.Error("LoadSupportContainerFromFile on non-existent file did not return error")
	}

	// Test case 3: Invalid YAML
	invalidYAMLPath := filepath.Join(tmpDir, "invalid.yaml")
	if err := os.WriteFile(invalidYAMLPath, []byte("invalid: yaml: :"), 0600); err != nil {
		t.Fatalf("failed to write invalid YAML: %v", err)
	}
	_, err = LoadSupportContainerFromFile(invalidYAMLPath)
	if err == nil {
		t.Error("LoadSupportContainerFromFile on invalid YAML did not return error")
	}
}

func TestNewSupportContainer(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "support_containers.yaml")

	if err := os.WriteFile(yamlPath, []byte(sampleSupportContainerYAML), 0600); err != nil {
		t.Fatalf("failed to write sample YAML: %v", err)
	}

	// Test case 1: Empty path (should return nil, nil)
	sc, err := NewSupportContainer("")
	if err != nil {
		t.Errorf("NewSupportContainer(\"\") returned error: %v", err)
	}
	if sc != nil {
		t.Errorf("NewSupportContainer(\"\") returned %v, expected nil", sc)
	}

	// Test case 2: Valid path
	sc, err = NewSupportContainer(yamlPath)
	if err != nil {
		t.Fatalf("NewSupportContainer(%q) failed: %v", yamlPath, err)
	}
	if sc == nil {
		t.Fatal("NewSupportContainer returned nil for valid path")
	}
	if len(sc.ContainerNames) != 2 {
		t.Errorf("expected 2 container names, got %d", len(sc.ContainerNames))
	}
}

func TestSupportContainerMatching(t *testing.T) {
	t.Parallel()

	sc := &SupportContainer{
		ContainerNames: []string{"k8s_POD", "pause"},
		ImageNames:     []string{"gcr.io/google-containers/pause", "k8s.gcr.io/pause"},
		Labels:         []string{"io.kubernetes.docker.type=podsandbox", "App=Pause"},
	}

	// Test SupportContainerImage
	imageTests := []struct {
		image string
		want  bool
	}{
		{"gcr.io/google-containers/pause-amd64:3.2", true},
		{"k8s.gcr.io/pause:3.5", true},
		{"docker.io/library/nginx:latest", false},
		{"GCR.IO/GOOGLE-CONTAINERS/PAUSE:3.2", true},
	}
	for _, tt := range imageTests {
		if got := sc.SupportContainerImage(tt.image); got != tt.want {
			t.Errorf("SupportContainerImage(%q) = %v, want %v", tt.image, got, tt.want)
		}
	}

	// Test SupportContainerName
	nameTests := []struct {
		name string
		want bool
	}{
		{"k8s_POD_some-pod-uid_some-namespace", true},
		{"pause-container", true},
		{"nginx-container", false},
		{"K8S_POD_suffix", true},
	}
	for _, tt := range nameTests {
		if got := sc.SupportContainerName(tt.name); got != tt.want {
			t.Errorf("SupportContainerName(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}

	// Test SupportContainerLabel
	labelTests := []struct {
		label string
		want  bool
	}{
		{"io.kubernetes.docker.type=podsandbox", true},
		{"io.kubernetes.docker.type=PODSANDBOX", true},
		{"app=pause", true},
		{"app=nginx", false},
		{"different-label=value", false},
	}
	for _, tt := range labelTests {
		if got := sc.SupportContainerLabel(tt.label); got != tt.want {
			t.Errorf("SupportContainerLabel(%q) = %v, want %v", tt.label, got, tt.want)
		}
	}

	// Test behavior when sc is nil
	var nilSc *SupportContainer
	if nilSc.SupportContainerImage("any") {
		t.Error("nil SupportContainer returned true for image match")
	}
	if nilSc.SupportContainerName("any") {
		t.Error("nil SupportContainer returned true for name match")
	}
	if nilSc.SupportContainerLabel("any") {
		t.Error("nil SupportContainer returned true for label match")
	}
}

func TestIsSupportContainer(t *testing.T) {
	t.Parallel()

	sc := &SupportContainer{
		ContainerNames: []string{"k8s_POD"},
		ImageNames:     []string{"gcr.io/google-containers/pause"},
		Labels:         []string{"io.kubernetes.docker.type=podsandbox"},
	}

	tests := []struct {
		name string
		ctr  Container
		want bool
	}{
		{
			name: "Matches image",
			ctr: Container{
				ImageBase: "gcr.io/google-containers/pause:3.2",
			},
			want: true,
		},
		{
			name: "Matches name (hostname)",
			ctr: Container{
				Hostname: "k8s_POD_abc123",
			},
			want: true,
		},
		{
			name: "Matches label",
			ctr: Container{
				Container: containers.Container{
					Labels: map[string]string{"io.kubernetes.docker.type": "podsandbox"},
				},
			},
			want: true,
		},
		{
			name: "No match",
			ctr: Container{
				ImageBase: "nginx",
				Hostname:  "webserver",
				Container: containers.Container{
					Labels: map[string]string{"app": "web"},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sc.IsSupportContainer(tt.ctr); got != tt.want {
				t.Errorf("IsSupportContainer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func compareSlices(t *testing.T, got, want []string, fieldName string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s length mismatch: got %d, want %d", fieldName, len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s[%d] mismatch: got %q, want %q", fieldName, i, got[i], want[i])
		}
	}
}
