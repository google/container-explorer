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

package containerd

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/metadata"
	"github.com/containerd/containerd/namespaces"
	"github.com/google/container-explorer/explorers"

	spec "github.com/opencontainers/runtime-spec/specs-go"

	log "github.com/sirupsen/logrus"
	bolt "go.etcd.io/bbolt"
)

type explorer struct {
	root     string   // containerd root
	manifest string   // path to manifest database file i.e. meta.db
	snapshot string   // path to snapshot database file i.e. metadata.db
	mdb      *bolt.DB // manifest database
}

// NewExplorer returns a ContainerExplorer interface to explore containerd.
func NewExplorer(root string, manifest string, snapshot string) (explorers.ContainerExplorer, error) {
	opt := &bolt.Options{
		ReadOnly: true,
	}
	db, err := bolt.Open(manifest, 0444, opt)
	if err != nil {
		return &explorer{}, err
	}

	return &explorer{
		root:     root,
		manifest: manifest,
		snapshot: snapshot,
		mdb:      db,
	}, nil
}

// SnapshotRoot returns the root directory containing snapshot information.
//
// Containerd requires snapshot database metadata.db which is stored within
// the snapshot root directory.
//
// The default snapshot root directrion location for containerd is
// /var/lib/containerd/io.containerd.snapshotter.v1.overlayfs
func (e *explorer) SnapshotRoot(snapshotter string) string {
	dirs, _ := filepath.Glob(filepath.Join(e.root, "*"))
	for _, dir := range dirs {
		fmt.Println(dir)
		if strings.Contains(strings.ToLower(dir), strings.ToLower(snapshotter)) {
			return dir
		}
	}
	return "unknown"
}

// ListNamespace returns namespaces.
//
// In containerd the namespace information is stored in metadata file meta.db.
func (e *explorer) ListNamespaces(ctx context.Context) ([]string, error) {
	var nss []string

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

	return nss, nil
}

// ListContainers returns the information about containers.
//
// In containerd the container information is stored in metadata file meta.db.
func (e *explorer) ListContainers(ctx context.Context) ([]explorers.Container, error) {
	var cecontainers []explorers.Container

	nss, err := e.ListNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	store := metadata.NewContainerStore(metadata.NewDB(e.mdb, nil, nil))

	for _, ns := range nss {
		ctx = namespaces.WithNamespace(ctx, ns)

		results, err := store.List(ctx)
		if err != nil {
			return nil, err
		}

		for _, result := range results {
			cecontainers = append(cecontainers, convertToContainerExplorerContainer(ns, result))
		}
	}
	return cecontainers, nil
}

// ListImages returns the information about content.
//
// In containerd, the image information is stored in metadata file meta.db.
func (e *explorer) ListImages(ctx context.Context) ([]explorers.Image, error) {
	var ceimages []explorers.Image

	nss, err := e.ListNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	store := metadata.NewImageStore(metadata.NewDB(e.mdb, nil, nil))

	for _, ns := range nss {
		ctx = namespaces.WithNamespace(ctx, ns)

		results, err := store.List(ctx)
		if err != nil {
			return nil, err
		}

		for _, result := range results {
			//ceimages = append(ceimages, convertToContainerExplorerImage(ns, result))
			ceimages = append(ceimages, explorers.Image{
				Namespace: ns,
				Image:     result,
			})
		}
	}
	return ceimages, nil
}

// ListContent returns the information about content.
//
// In containerd, the content information is stored in metadata file meta.db.
func (e *explorer) ListContent(ctx context.Context) ([]explorers.Content, error) {
	var cecontent []explorers.Content

	nss, err := e.ListNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	store := NewBlobStore(e.mdb)

	for _, ns := range nss {
		ctx = namespaces.WithNamespace(ctx, ns)

		results, err := store.List(ctx)
		if err != nil {
			return nil, err
		}

		for _, result := range results {
			cecontent = append(cecontent, explorers.Content{
				Namespace: ns,
				Info:      result,
			})
		}
	}

	return cecontent, nil
}

// ListSnapshots returns the snapshot information.
//
// In containerd, the snapshot information is stored in two different files:
// - metadata file (meta.db)
// - snapshot file (metadata.db)
//
// These files contain some overlapping fields.
//
// The metadata file meta.db contains snapshot information and container
// references the the snapshot information.
//
// The snapshot file metadata.db contains information about the snapshots only
// without reference to a container. This file also containers informations
// that are more relevant to manage snapshots.
//
// For Examples:
//   - Snapshot type i.e. active or committed
//   - Snapshot ID that refers to overlay path i.e /var/lib/containerd/io.containerd.snapshotter.v1.overlayfs/snapshots/<id>/fs
//
// Snapshot ID is required when mounting the container.
func (e *explorer) ListSnapshots(ctx context.Context) ([]explorers.SnapshotKeyInfo, error) {
	var cesnapshots []explorers.SnapshotKeyInfo

	nss, err := e.ListNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	// snapshot database
	opts := bolt.Options{
		ReadOnly: true,
	}
	ssdb, err := bolt.Open(e.snapshot, 0444, &opts)
	if err != nil {
		log.WithFields(log.Fields{
			"snapshotfile": e.snapshot,
		}).Error(err)
	}

	store := NewSnaptshotStore(e.root, e.mdb, ssdb)

	for _, ns := range nss {
		ctx = namespaces.WithNamespace(ctx, ns)

		results, err := store.List(ctx)
		if err != nil {
			return nil, err
		}

		cesnapshots = append(cesnapshots, results...)
	}

	return cesnapshots, nil
}

// Close releases the internal resources
func (e *explorer) Close() error {
	return e.mdb.Close()
}

// convertToContainerExplorerContainer returns a Container object which is
// superset of containers.Container object.
func convertToContainerExplorerContainer(ns string, ctr containers.Container) explorers.Container {
	var hostname string
	if ctr.Spec != nil && ctr.Spec.Value != nil {
		var v spec.Spec
		json.Unmarshal(ctr.Spec.Value, &v)

		if v.Hostname != "" {
			hostname = v.Hostname
		} else {
			for _, kv := range v.Process.Env {
				if strings.HasPrefix(kv, "HOSTNAME=") {
					hostname = strings.TrimSpace(strings.Split(kv, "=")[1])
					break
				}
			}
		}
	}

	return explorers.Container{
		Namespace:        ns,
		Hostname:         hostname,
		SupportContainer: isKubernetesSupportContainer(ctr),
		Container:        ctr,
	}
}

// isKubernetesSupportContainer returns true for a container that was created
// by Kubernetes to facilitate the management of containers.
//
// Example of such containers are kubeproxy, kube-dns etc.
func isKubernetesSupportContainer(ctr containers.Container) bool {
	var imagebase string = ctr.Image
	supportcontainer := false

	// Check for a Kubernetes support container based on a known image.
	// Example: gke.gcr.io/gke-metrics-agent:1.2.0-gke.0
	if strings.Contains(ctr.Image, "@") {
		imagebase = strings.Split(ctr.Image, "@")[0]
	}

	if strings.Contains(imagebase, ":") {
		imagebase = strings.Split(imagebase, ":")[0]
	}

	if _, found := explorers.KubernetesSupportContainers[imagebase]; found {
		supportcontainer = true
	}

	log.WithFields(log.Fields{
		"imagebase":        imagebase,
		"supportcontainer": supportcontainer,
	}).Debug("checking Kubernetes support container")

	// TODO (rmaskey): Check for a Kubernetes support container based on container ID.

	return supportcontainer
}
