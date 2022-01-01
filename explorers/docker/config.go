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

package docker

import "time"

// State holds attribute about docker container state
type State struct {
	Running           bool
	Paused            bool
	Resarting         bool
	OOMKilled         bool
	RemovalInProgress bool
	Dead              bool
	Pid               int64
	ExitCode          int64
	Error             string
	StartedAt         time.Time
	FinishedAt        time.Time
	Health            interface{}
}

// Config holds docker runtime config
type Config struct {
	ExposedPorts map[string]interface{}
	Hostname     string
	Domainname   string
	User         string
	AttachStdin  bool
	AttachStdout bool
	AttachStderr bool
	Tty          bool
	OpenStdin    bool
	StdinOnce    bool
	Env          []string
	Cmd          []string
	Image        string
	Volumes      interface{}
	WorkingDir   interface{}
	EntryPoint   interface{}
	OnBuild      interface{}
	Labels       map[string]string
}

// Bridge represents docker networks bridge structure
type Bridge struct {
	IPAMConfig        interface{}
	Links             interface{}
	Aliases           interface{}
	NetworkID         string
	EndpointID        string
	Gateway           string
	IPAddresses       string
	IPPrefixLen       int
	IPv6Gateway       string
	GlobalIPv6Address string
	GlobalIPPrefixLen int
	MacAddresses      string
	IPAMOperational   bool
}

// NetworkSettings represents docker network settings
type NetworkSettings struct {
	Bridge                 string
	SandboxID              string
	HairpinMode            bool
	LinkLocalIPv6Address   string
	LinkLocalIPv6PrefixLen int
	Networks               map[string]interface{}
	Service                map[string]interface{}
	Ports                  map[string]interface{}
	SandboxKey             string
	SecondaryIPAddresses   interface{}
	SecondaryIPv6Addresses interface{}
	IsAnonymousEndpoint    bool
	HasSwarmEndpoint       bool
}

// ConfigFile represents docker config.v2.json structure
type ConfigFile struct {
	StreamConfig           map[string]interface{}
	State                  State
	ID                     string
	Created                time.Time
	Managed                bool
	Path                   string
	Args                   []string
	ContainerConfig        map[string]interface{}
	Config                 Config
	Image                  string
	NetworkSettings        NetworkSettings
	LogPath                string
	Name                   string
	Driver                 string
	MountLabel             string
	ProcessLabel           string
	RestartCount           int64
	HasBeenRestartedBefore bool
	HasBeenManuallyStopped bool
	MountPoints            map[string]interface{}
	SecretReferences       interface{}
	AppArmorProfile        string
	HostnamePath           string
	HostsPath              string
	ShmPath                string
	ResolvConfPath         string
	SeccompProfile         string
	NoNewPrivileges        bool
}
