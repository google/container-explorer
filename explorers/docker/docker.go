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

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/metadata"
	"github.com/google/container-explorer/explorers"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	log "github.com/sirupsen/logrus"
	bolt "go.etcd.io/bbolt"
)

const (
	configV2Filename     = "config.v2.json"
	containersDirName    = "containers"
	lowerdirName         = "lower"
	repositoriesDirName  = "image"
	repositoriesFileName = "repositories.json"
	storageOverlay2      = "overlay2"
)

var imagerepo map[string]string

type ImageName map[string]string

type ImageRepository struct {
	Repositories map[string]ImageName
}

type explorer struct {
	root          string // docker root directory
	contaierdroot string
	manifest      string
	snapshot      string
	mdb           *bolt.DB                    // manifest database file
	sc            *explorers.SupportContainer // support container object
}

// NewExplorer returns a ContainerExplorer interface to explorer docker managed
// containers.
func NewExplorer(root string, containerdroot string, manifest string, snapshot string, sc *explorers.SupportContainer) (explorers.ContainerExplorer, error) {
	var db *bolt.DB
	var err error

	if fileExists(containerdroot) {
		opt := &bolt.Options{
			ReadOnly: true,
		}
		db, err = bolt.Open(manifest, 0444, opt)
		if err != nil {
			return &explorer{}, err
		}
	}

	log.WithFields(log.Fields{
		"root":           root,
		"containerdroot": containerdroot,
		"manifest":       manifest,
		"snapshot":       snapshot,
	}).Debug("new docker explorer")

	return &explorer{
		root:          root,
		contaierdroot: containerdroot,
		manifest:      manifest,
		snapshot:      snapshot,
		mdb:           db,
		sc:            sc,
	}, nil
}

// SnapshotRoot returns the snapshot root director for docker managed
// containers.
func (e *explorer) SnapshotRoot(snapshotter string) string {
	// TODO(rmaskey): implement the function
	return ""
}

// ListNamespaces returns namespaces for docker managed containers.
func (e *explorer) ListNamespaces(ctx context.Context) ([]string, error) {
	var nss []string

	// Namespaces in metadata file i.e. meta.db
	// in /var/lib/containerd/io.containerd.metadata.v1.bolt/meta.db
	if e.mdb != nil {
		err := e.mdb.View(func(tx *bolt.Tx) error {
			store := metadata.NewNamespaceStore(tx)
			results, err := store.List(ctx)
			if err != nil {
				return err
			}
			nss = results
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	// TODO(rmaskey): implement the function

	return nss, nil
}

// GetContainerIDs returns container ID
func (e *explorer) GetContainerIDs(ctx context.Context, containerdir string) ([]string, error) {
	containerpaths, err := filepath.Glob(filepath.Join(containerdir, "*"))
	if err != nil {
		return nil, err
	}

	var containerids []string
	for _, containerpath := range containerpaths {
		_, containerid := filepath.Split(containerpath)
		containerids = append(containerids, containerid)
	}
	return containerids, nil
}

// ListContainers returns container information.
func (e *explorer) ListContainers(ctx context.Context) ([]explorers.Container, error) {
	containersdir := filepath.Join(e.root, containersDirName)
	log.WithFields(log.Fields{
		"dockerroot":    e.root,
		"containersdir": containersdir,
	}).Debug("docker containers directory")

	containerids, err := e.GetContainerIDs(ctx, containersdir)
	if err != nil {
		return nil, err
	}

	var cecontainers []explorers.Container

	for _, containerid := range containerids {
		cectr, err := e.GetCEContainer(ctx, containerid)
		if err != nil {
			return nil, err
		}

		cecontainers = append(cecontainers, cectr)
	}

	return cecontainers, nil
}

// structure to hold limited docker image information
//
// The structure hold information from the file
// /var/lib/docker/image/overlay2/imagedb/content/sha256/<imageid>
//
// Reference to docker source code https://github.com/moby/moby/image/image.go

type rootfs struct {
	Rfstype string   `json:"type"`
	DiffIds []string `json:"diff_ids"`
}

// Refer to struct History
type historyItem struct {
	Created    time.Time `json:"created"`
	Author     string    `json:"author,omitempty"`
	CreatedBy  string    `json:"created_by,omitempty"`
	Comment    string    `json:"comment,omitempty"`
	EmptyLayer bool      `json:"empty_layer,omitempty"`
}

// Refer to structs Image and V1Image
type imageContentSummary struct {
	ID              string        `json:"id,omitempty"`
	Architecture    string        `json:"architecture"`
	Comment         string        `json:"comment,omitempty"`
	Config          Config        `json:"config"`
	Container       string        `json:"container"`
	ContainerConfig Config        `json:"container_config"`
	Created         time.Time     `json:"created"`
	DockerVersion   string        `json:"docker_version"`
	History         []historyItem `json:"history"`
	Os              string        `json:"os"`
	Parent          string        `json:"parent,omitempty"`
	Rootfs          rootfs        `json:"rootfs"`
}

// ListImages returns information about docker images.
func (e *explorer) ListImages(ctx context.Context) ([]explorers.Image, error) {
	// TODO (rmaskey): Handle docker version 1 images

	// Docker version 2
	//
	// Check for valid image repositories directory
	repositoriesdir := filepath.Join(e.root, repositoriesDirName)
	if !fileExists(repositoriesdir) {
		return nil, fmt.Errorf("valid image repositories directory %s not found", repositoriesdir)
	}

	storagedirs, err := filepath.Glob(filepath.Join(repositoriesdir, "*"))
	if err != nil {
		return nil, fmt.Errorf("listing storage directories %v", err)
	}

	var ceimages []explorers.Image

	for _, storagedir := range storagedirs {
		_, storagename := filepath.Split(storagedir)
		repositoriesfile := filepath.Join(storagedir, repositoriesFileName)

		log.WithFields(log.Fields{
			"storagename":      storagename,
			"storagedir":       storagedir,
			"repositoriesfile": repositoriesfile,
		}).Debug("image repository file")

		data, err := ioutil.ReadFile(repositoriesfile)
		if err != nil {
			return nil, fmt.Errorf("failed read repository file %v. %v", repositoriesfile, err)
		}

		var r ImageRepository
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("unmarshalling image repository file %s. %v", repositoriesfile, err)
		}

		for _, distvalue := range r.Repositories {
			for k, v := range distvalue {
				image := images.Image{
					Name: k,
					Target: ocispec.Descriptor{
						Digest: digest.Digest(v),
					},
				}

				if storagename == storageOverlay2 {
					imagecontent, err := readImageContent(storagename, storagedir, image.Target.Digest)
					if err != nil {
						log.Error("reading image content file ", err)
					} else {
						image.CreatedAt = imagecontent.Created
					}
				}

				ceimages = append(ceimages, explorers.Image{
					Image:                 image,
					SupportContainerImage: e.sc.SupportContainerImage(imageBasename(image.Name)),
				})
			}
		}
	}

	return ceimages, nil
}

// ListContent returns content information.
func (e *explorer) ListContent(ctx context.Context) ([]explorers.Content, error) {
	// TODO(rmaskey): implement the function
	fmt.Printf("INFO: listing content not implemented\n\n")

	return nil, nil
}

// ListSnapshots returns snapshot information.
func (e *explorer) ListSnapshots(ctx context.Context) ([]explorers.SnapshotKeyInfo, error) {
	// TODO(rmaskey): implement the function
	fmt.Printf("INFO: listing snapshots not implemented\n\n")

	return nil, nil
}

// ListTasks returns container task status
func (e *explorer) ListTasks(cxt context.Context) ([]explorers.Task, error) {
	// TODO(rmaskey): implement the function
	fmt.Printf("INFO: listing task status not implemented\n\n")

	var tasks []explorers.Task
	return tasks, nil
}

// InfoContainer returns container internal information.
func (e *explorer) InfoContainer(ctx context.Context, containerid string, spec bool) (interface{}, error) {
	// TODO(rmaskey): implement the function
	fmt.Printf("INFO: container info not implemented\n\n")

	return nil, nil
}

// MountContainer mounts a container to the specified path
func (e *explorer) MountContainer(ctx context.Context, containerid string, mountpoint string) error {
	container, err := e.GetContainer(ctx, containerid)
	if err != nil {
		return fmt.Errorf("getting container %v", err)
	}

	containerMountIDPath := filepath.Join(e.root, repositoriesDirName, container.Driver, "layerdb", "mounts", containerid, "mount-id")
	log.WithField("containerMountIDPath", containerMountIDPath).Debug("container mount-id path")

	mountIDByte, err := ioutil.ReadFile(containerMountIDPath)
	if err != nil {
		return fmt.Errorf("reading container mount-id")
	}
	mountID := string(mountIDByte)
	log.WithField("mount-id", mountID).Debug("container mount-id")

	// build container lower directory
	lowerdirpath := filepath.Join(e.root, container.Driver, mountID, lowerdirName)
	log.WithField("lowerdirpath", lowerdirpath).Debug("container lowerdir path")
	data, err := ioutil.ReadFile(lowerdirpath)
	if err != nil {
		return fmt.Errorf("reading lower file %v", err)
	}

	var lowerdir string
	for i, ldir := range strings.Split(string(data), ":") {
		ldirpath := filepath.Join(e.root, container.Driver, ldir)
		if i == 0 {
			lowerdir = ldirpath
			continue
		}
		lowerdir = fmt.Sprintf("%s:%s", lowerdir, ldirpath)
	}

	upperdir := filepath.Join(e.root, container.Driver, mountID, "diff")
	workdir := filepath.Join(e.root, container.Driver, mountID, "work")

	log.WithFields(log.Fields{
		"lowerdir": lowerdir,
		"upperdir": upperdir,
		"workdir":  workdir,
	}).Debug("container overlay directories")

	// mounting container
	mountopts := fmt.Sprintf("ro,lowerdir=%s:%s", upperdir, lowerdir)
	mountargs := []string{"-t", "overlay", "overlay", "-o", mountopts, mountpoint}

	cmd := exec.Command("mount", mountargs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Errorf("running mount command %v", mountargs)

		if strings.Contains(err.Error(), " 32") {
			return fmt.Errorf("invalid lowerdir path %v. Use --debug to view lowerdir path", err)
		}
		return fmt.Errorf("executing mount command %v", err)
	}

	if string(out) != "" {
		log.WithField("mount command", string(out)).Debug("container mount command")
	}

	return nil
}

// MountAllContainers mounts all the containers
func (e *explorer) MountAllContainers(ctx context.Context, mountpoint string, skipsupportcontainers bool) error {
	containersdir := filepath.Join(e.root, containersDirName)
	log.WithField("containersdir", containersdir).Debug("docker containers directory")

	containerids, err := e.GetContainerIDs(ctx, containersdir)
	if err != nil {
		return fmt.Errorf("failed listing containers ID %v", err)
	}
	if containerids == nil {
		return fmt.Errorf("no container ID returned")
	}

	for _, containerid := range containerids {
		cecontainer, err := e.GetCEContainer(ctx, containerid)
		if err != nil {
			log.WithField("containerid", containerid).Error("getting container details")
			log.WithField("containerid", containerid).Warn("skipping container mount")
			continue
		}

		if skipsupportcontainers && cecontainer.SupportContainer {
			log.WithFields(log.Fields{
				"namespace":   cecontainer.Namespace,
				"containerid": cecontainer.ID,
			}).Info("skip mounting Kubernetes support container")
			continue
		}

		// Create mountpoint for each container
		ctrmountpoint := filepath.Join(mountpoint, cecontainer.ID)
		if err := os.MkdirAll(ctrmountpoint, 0755); err != nil {
			log.WithFields(log.Fields{
				"namespace":   cecontainer.Namespace,
				"containerid": cecontainer.ID,
				"mountpoint":  ctrmountpoint,
			}).Error("creating mountpoint for container")
			log.WithField("containerid", containerid).Warn("skippoing container mount")
			continue
		}

		if err := e.MountContainer(ctx, containerid, ctrmountpoint); err != nil {
			log.WithFields(log.Fields{
				"containerid": containerid,
				"message":     err.Error(),
			}).Error("mounting container")
		}
	}

	// default
	return nil
}

// Close releases internal resources.
func (e *explorer) Close() error {
	return e.mdb.Close()
}

// GetContainer returns container configuration
func (e *explorer) GetContainer(ctx context.Context, containerid string) (ConfigFile, error) {
	containerdir := filepath.Join(e.root, containersDirName, containerid)
	log.WithField("containerdir", containerdir).Debug("container directory")
	if !fileExists(containerdir) {
		return ConfigFile{}, fmt.Errorf("container does not exist")
	}

	containerConfigFile := filepath.Join(containerdir, configV2Filename)
	log.WithField("containerConfigFile", containerConfigFile).Debug("container configuration file")
	if !fileExists(containerConfigFile) {
		return ConfigFile{}, fmt.Errorf("container config file %s does not exist", configV2Filename)
	}

	data, err := ioutil.ReadFile(containerConfigFile)
	if err != nil {
		return ConfigFile{}, fmt.Errorf("reading container config file %s %v", configV2Filename, err)
	}

	var container ConfigFile
	if err := json.Unmarshal(data, &container); err != nil {
		return ConfigFile{}, fmt.Errorf("unmarshalling container config %v", err)
	}

	return container, nil
}

// GetCEContainer returns ContainerExplorer container
func (e *explorer) GetCEContainer(ctx context.Context, containerid string) (explorers.Container, error) {
	if imagerepo == nil {
		imagerepo, _ = e.GetRepositories(ctx)
	}

	// Get docker container configuration based on container ID
	config, err := e.GetContainer(ctx, containerid)
	if err != nil {
		return explorers.Container{}, err
	}

	cectr := convertToContainerExplorerContainer(config)

	// Use image friendly name if exits
	if imagerepo != nil {
		if val, found := imagerepo[cectr.Image]; found {
			cectr.Image = val
		}
	}

	// Extrac imagebase name from image name
	cectr.ImageBase = imageBasename(cectr.Image)
	cectr.SupportContainer = e.sc.IsSupportContainer(cectr)

	return cectr, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// GetRepositories returns mapping of image ID to name
func (e *explorer) GetRepositories(ctx context.Context) (map[string]string, error) {
	repositoriesdir := filepath.Join(e.root, repositoriesDirName)
	if !fileExists(repositoriesdir) {
		return nil, fmt.Errorf("image repository directory %s does not exist", repositoriesdir)
	}

	storagedirs, err := filepath.Glob(filepath.Join(repositoriesdir, "*"))
	if err != nil {
		return nil, fmt.Errorf("listing storage directories. %v", err)
	}

	for _, storagedir := range storagedirs {
		_, storagename := filepath.Split(storagedir)

		if storagename != "overlay2" {
			// TODO(rmaskey): handle other storage
			log.Warn("storage ", storagename, " currently not supported")
			continue
		}

		// Handle overlay2 storage
		repositoriesfile := filepath.Join(storagedir, repositoriesFileName)
		data, err := ioutil.ReadFile(repositoriesfile)
		if err != nil {
			return nil, fmt.Errorf("failed reading repositories file %s. %v", repositoriesfile, err)
		}

		var r ImageRepository
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("unmarshalling repositories file")
		}

		repositories := make(map[string]string)
		for _, osdist := range r.Repositories {
			for k, v := range osdist {
				// repositories.json may contain multiple entries with same digest.
				// Using the record that contains the friendly name rather <distro>@<digest> pattern
				//
				// Example: Two labels have the same hash
				// "nginx": {
				//   "nginx:latest": "sha256:605c77e624ddb75e6110f997c58876baa13f8754486b461117934b24a9dc3a85",
				//   "nginx@sha256:0d17b565c37bcbd895e9d92315a05c1c3c9a29f762b011a10c54a66cd53c9b31": "sha256:605c77e624ddb75e6110f997c58876baa13f8754486b461117934b24a9dc3a85"
				// }
				if !strings.Contains(k, "@") {
					repositories[v] = k
				}
			}
		}
		return repositories, nil
	}

	return nil, nil
}

// convertToContainerExplorerContainer maps docker config data to container
// explorer container structure
func convertToContainerExplorerContainer(config ConfigFile) explorers.Container {
	var exposedports []string

	if config.Config.ExposedPorts != nil {
		for k := range config.Config.ExposedPorts {
			exposedports = append(exposedports, k)
		}
	}

	var status string
	const notStarted = "0001-01-01T00:00:00Z"

	if config.State.StartedAt.Format("2006-01-02T15:04:05Z") == notStarted {
		status = "CREATED"
	} else if config.State.Running && config.State.Paused {
		status = "PAUSED"
	} else if config.State.Running && !config.State.Paused {
		status = "RUNNING"
	} else if !config.State.Running && config.State.Paused {
		status = "UNKNOWN"
	} else if !config.State.Running && !config.State.Paused {
		status = "STOPPED"
	}

	return explorers.Container{
		Hostname:      config.Config.Hostname,
		ProcessID:     int(config.State.Pid),
		ContainerType: "docker",
		Container: containers.Container{
			ID:          config.ID,
			CreatedAt:   config.Created,
			Image:       config.Image,
			Snapshotter: config.Driver,
			Runtime: containers.RuntimeInfo{
				Name: config.Name,
			},
		},
		Running:      config.State.Running,
		ExposedPorts: exposedports,
		Status:       status,
	}
}

// readImageContent reads the content of overlay2 image content
func readImageContent(storagename string, storagepath string, digest digest.Digest) (imageContentSummary, error) {
	m := strings.Split(string(digest), ":")
	if len(m) != 2 {
		return imageContentSummary{}, fmt.Errorf("expecting two colon separated values")
	}
	algo := m[0]
	filename := m[1]

	imagecontentfile := filepath.Join(storagepath, "imagedb", "content", algo, filename)
	log.WithFields(log.Fields{
		"filename": imagecontentfile,
	}).Debug("reading docker image content file")

	data, err := ioutil.ReadFile(imagecontentfile)
	if err != nil {
		log.WithFields(log.Fields{
			"storage name": storagename,
			"algo":         algo,
			"filename":     filename,
		}).Debug("reading docker image content file")

		return imageContentSummary{}, err
	}

	var imagecontent imageContentSummary
	if err := json.Unmarshal(data, &imagecontent); err != nil {
		return imageContentSummary{}, err
	}

	return imagecontent, nil
}

// imageBasename returns the base name of an image
func imageBasename(name string) string {
	imagebase := strings.Replace(name, "\"", "", -1)

	if strings.Contains(imagebase, "@") {
		imagebase = strings.Split(imagebase, "@")[0]
	}

	log.WithFields(log.Fields{
		"imagename": name,
		"imagebase": imagebase,
	}).Debug("extracting imagebase from image")

	return imagebase
}
