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

package podman

type containerMetadata struct {
	ImageName string `json:"image-name"`
	ImageID   string `json:"image-id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created-at"`
}

type containerFlag struct {
	MountLabel   string
	ProcessLabel string
}

type containerConfig struct {
	ID       string        `json:"id"`
	Names    []string      `json:"names"`
	Image    string        `json:"image"`
	Layer    string        `json:"layer"`
	Metadata string        `json:"metadata"`
	Created  string        `json:"created"`
	Flags    containerFlag `json:"flags"`
}

type containerImage struct {
	ID             string         `json:"id"`
	Digest         string         `json:"digest"`
	Names          []string       `json:"names"`
	NameHistory    []string       `json:"name-history"`
	Layer          string         `json:"layer"`
	Metadata       map[string]any `json:"metadata"`
	BigDataNames   []string       `json:"big-data-names"`
	BigDataSizes   []string       `json:"big-data-sizes"`
	BigDataDigests []string       `json:"big-data-digests"`
	Created        string         `json:"created"`
}
