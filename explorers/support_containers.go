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

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// SupportContainer contains information about support container.
//
// A support container can be identified by:
// - Container or pod hostname
// - Image name
// - Container labels or annotations
type SupportContainer struct {
	ContainerNames []string `json:"names" yaml:"names"`
	ImageNames     []string `json:"images" yaml:"images"`
	Labels         []string `json:"labels" yaml:"labels"`
}

// NewSupportContainer returns the support container instance.
func NewSupportContainer(path string) (*SupportContainer, error) {
	sc, err := LoadSupportContainerFromFile(path)
	if err != nil {
		return nil, err
	}
	// default return
	return &sc, nil
}

// LoadSupportContainerFromFile loads the support container information from
// a yaml file on disk.
func LoadSupportContainerFromFile(path string) (SupportContainer, error) {
	var sc SupportContainer

	data, err := os.ReadFile(path)
	if err != nil {
		return SupportContainer{}, fmt.Errorf("reading file %s: %v", path, err)
	}

	if err := yaml.Unmarshal(data, &sc); err != nil {
		return SupportContainer{}, fmt.Errorf("unmarshalling %s: %v", path, err)
	}

	// default return
	return sc, nil
}

// SupportContainerImage returns true if the supplied image is a known support
// container image.
func (sc *SupportContainer) SupportContainerImage(image string) bool {
	if sc == nil {
		log.WithField("imagebase", image).Debug("support container data not initialized")
		return false
	}

	for _, scimage := range sc.ImageNames {
		/*
			if strings.ToLower(scimage) == strings.ToLower(image) {
				return true
			}
		*/
		if strings.Contains(strings.ToLower(image), strings.ToLower(scimage)) {
			log.WithField("imagebase", image).Debug("support container image found")
			return true
		}
	}
	// default
	log.WithField("imagebase", image).Debug("support container image not found")
	return false
}

// SupportContainerName returns true if the supplied name is a known support
// container name.
func (sc *SupportContainer) SupportContainerName(name string) bool {
	if sc == nil {
		log.WithField("name", name).Debug("support container data not initialized")
		return false
	}

	for _, scname := range sc.ContainerNames {
		/*
			if strings.ToLower(scname) == strings.ToLower(name) {
				return true
			}
		*/
		if strings.Contains(strings.ToLower(name), strings.ToLower(scname)) {
			return true
		}
	}

	// default
	return false
}

// SupportContainerLabel returns true if the supplied name is a known support
// container label
func (sc *SupportContainer) SupportContainerLabel(label string) bool {
	if sc == nil {
		log.WithField("label", label).Debug("support container data not initiazed")
		return false
	}

	for _, sclabel := range sc.Labels {
		//if strings.ToLower(sclabel) == strings.ToLower(label)
		if strings.EqualFold(sclabel, label) {
			return true
		}
	}
	// default
	return false
}

// IsSupportContainer returns if the support container
func (sc *SupportContainer) IsSupportContainer(ctr Container) bool {
	if sc.SupportContainerImage(ctr.ImageBase) {
		return true
	}

	if sc.SupportContainerName(ctr.Hostname) {
		return true
	}

	for k, v := range ctr.Labels {
		labelstring := fmt.Sprintf("%s=%s", k, v)
		if sc.SupportContainerLabel(labelstring) {
			return true
		}
	}

	// default
	return false
}

// JSON returns the data in json
func (sc *SupportContainer) JSON() string {
	data, _ := json.MarshalIndent(sc, "", " ")
	return string(data)
}
