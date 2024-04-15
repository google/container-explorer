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
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/metadata/boltutil"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/snapshots"
	"github.com/google/container-explorer/explorers"
	log "github.com/sirupsen/logrus"
	bolt "go.etcd.io/bbolt"
)

type snapshotStore struct {
	root string // containerd root directory
	db   *bolt.DB
	sdb  *bolt.DB
}

// NewSnapshotStore returns snapshotStore which handles viewing of snapshot information
//
// In containerd, snapshot information is stored in metadata file meta.db and snapshot
// database file metadata.db.
//
// The meta.db file contains the following information:
//   - Container reference to container snapshot: meta.db/v1/<namespace>/containers/<container id>
//   - snapshotter
//   - snapshotKey
//   - Snapshot information in meta.db/v1/<namespace>/snapshots/<snapshotter>/<snapshot key>
//
// The metadata.db file contains additional information about a snapshot.
// Snapshot path in snapshot database: metadata.db/v1/snapshots/<snapshot key>
//   - id - Snapshot file system ID i.e. /var/lib/containerd/io.containerd.snapshotter.v1.overlayfs/snapshots/<id>/fs
//   - kind - ACTIVE vs COMMITTED
func NewSnaptshotStore(root string, db *bolt.DB, sdb *bolt.DB) *snapshotStore {
	return &snapshotStore{
		root: root,
		db:   db,
		sdb:  sdb,
	}
}

// List returns a structure that contains combined information from metadata
// and snapshot database snapshot key.
func (s *snapshotStore) List(ctx context.Context) ([]explorers.SnapshotKeyInfo, error) {
	namespace, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get namespace from context %v", err)
	}

	// Overlay snapshot bucket
	if s.sdb == nil {
		log.Warn("handle to snapshot database does not exist")
	}

	var skinfos []explorers.SnapshotKeyInfo

	// Read metadata database i.e. meta.db and extract relevant information
	// about the snapshots.
	if err := s.db.View(func(tx *bolt.Tx) error {
		bkt := getSnapshottersBucket(tx, namespace)
		if bkt == nil {
			return nil // empty store
		}

		// Handle each snapshotter
		// meta.db/v1/<namespace>/<snapshots>/<snapshotter>
		bkt.ForEach(func(k, v []byte) error {
			ssbkt := bkt.Bucket(k)
			if ssbkt == nil {
				return nil // empty snapshotter
			}

			// Handle each snapshot key
			// meta.db/v1/<namespace>/snapshots/<snapshotter>/<snapshot key>
			return ssbkt.ForEach(func(k1, v1 []byte) error {
				var (
					skinfo = explorers.SnapshotKeyInfo{
						Namespace:   namespace,
						Snapshotter: string(k),
						Key:         string(k1),
					}

					// snapshot key bucket that contains information about a
					// snapshot
					kbkt = ssbkt.Bucket(k1)
				)

				if err := readMetaSnapshotKey(&skinfo, kbkt); err != nil {
					return err
				}

				// Reading additional snapshot key information from metadata.db
				// snapshot key
				if s.sdb != nil {
					s.sdb.View(func(otx *bolt.Tx) error {
						log.WithFields(log.Fields{
							"snapshot key":  skinfo.Key,
							"snapshot name": skinfo.Name,
						}).Debug("meta.db snapshot key")
						skbkt := getOverlaySnapshotBucket(otx, skinfo.Name)
						if skbkt == nil {
							log.WithFields(log.Fields{
								"snapshot key": skinfo.Key,
							}).Info("empty metata.db snapshot key bucket")
							return nil
						}
						readOverlaySnapshotKey(&skinfo, skbkt)

						return nil
					})
				}

				skinfos = append(skinfos, skinfo)
				return nil
			})
		})

		return nil
	}); err != nil {
		return nil, err
	}

	return skinfos, nil
}

// NativePath returns the upperdir for a container.
func (s *snapshotStore) NativePath(ctx context.Context, container containers.Container) (string, error) {
	if s.sdb == nil {
		return "", fmt.Errorf("snapshot database handler (metadata.db) is nil")
	}

	snapshotkeys, err := s.SnapshotKeys(ctx, container)
	if err != nil {
		return "", fmt.Errorf("could not get snapshot keys for container ", container.ID)
	}

	var (
		upperdir     string
		snapshotroot string
	)

	snapshotroot = snapshotRootDir(s.root, container.Snapshotter)
	// Read snapshot metadata (metadata.db) snapshotkey bucket
	// and extract value of key "id".
	//
	// The value of "id" specifies snapshot path in overlayfs
	// i.e. /var/lib/containerd/io.containerd.snapshotter.v1.overlayfs/snapshots/<id>/fs
	if err := s.sdb.View(func(tx *bolt.Tx) error {
		upperdirID, err := getSnapshotID(tx, snapshotkeys[0])
		if err != nil {
			return err
		}
		upperdir = filepath.Join(snapshotroot, "snapshots", fmt.Sprintf("%d", upperdirID))
		return nil
	}); err != nil {
		return "", err
	}
	return upperdir, nil
}

// OverlayPath returns the overlay paths lowerdir, upperdir, and workdir for a container.
func (s *snapshotStore) OverlayPath(ctx context.Context, container containers.Container) (string, string, string, error) {
	if s.sdb == nil {
		return "", "", "", fmt.Errorf("snapshot database handler (metadata.db) is nil")
	}

	snapshotkeys, err := s.SnapshotKeys(ctx, container)
	if err != nil {
		return "", "", "", fmt.Errorf("could not get snapshot keys for container ", container.ID)
	}

	var (
		lowerdir     string
		upperdir     string
		workdir      string
		snapshotroot string
	)

	snapshotroot = snapshotRootDir(s.root, container.Snapshotter)
	// Read snapshot metadata (metadata.db) snapshotkey bucket
	// and extract value of key "id".
	//
	// The value of "id" specifies snapshot path in overlayfs
	// i.e. /var/lib/containerd/io.containerd.snapshotter.v1.overlayfs/snapshots/<id>/fs
	if err := s.sdb.View(func(tx *bolt.Tx) error {
		upperdirID, err := getSnapshotID(tx, snapshotkeys[0])
		if err != nil {
			return err
		}
		upperdir = filepath.Join(snapshotroot, "snapshots", fmt.Sprintf("%d", upperdirID), "fs")
		workdir = filepath.Join(snapshotroot, "snapshots", fmt.Sprintf("%d", upperdirID), "work")

		// compute lowerdir
		for _, ssk := range snapshotkeys[1:] {
			id, err := getSnapshotID(tx, ssk)
			if err != nil {
				return err
			}
			ldir := filepath.Join(snapshotroot, "snapshots", fmt.Sprintf("%d", id), "fs")

			if lowerdir == "" {
				lowerdir = ldir
				continue
			}
			lowerdir = fmt.Sprintf("%s:%s", lowerdir, ldir)
		}
		return nil
	}); err != nil {
		return "", "", "", err
	}

	// default return
	return lowerdir, upperdir, workdir, nil
}

// SnapshotKeys returns the snapshot keys for a container.
func (s *snapshotStore) SnapshotKeys(ctx context.Context, container containers.Container) ([]string, error) {
	namespace, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get namespace from context %v", err)
	}
	var snapshotkeys []string

	if err := s.db.View(func(tx *bolt.Tx) error {
		ssk := container.SnapshotKey

		for {
			bkt := getSnapshotKeyBucket(tx, namespace, container.Snapshotter, ssk)
			log.WithFields(log.Fields{
				"namespace":   namespace,
				"snapshotter": container.Snapshotter,
				"snapshotkey": ssk,
			}).Debug("snapshot key bucket")
			if bkt == nil {
				return fmt.Errorf("empty meta.db snapshotkey bucket")
			}

			name := string(bkt.Get(bucketKeyName))
			parent := string(bkt.Get(bucketKeyParent))

			snapshotkeys = append(snapshotkeys, name)

			if parent == "" {
				break
			}
			ssk = parent
		}

		return nil
	}); err != nil {
		return nil, err
	}
	return snapshotkeys, nil
}

// readMetaSnapshotKey parses the snapshot key key-value pairs in meta.db
func readMetaSnapshotKey(skinfo *explorers.SnapshotKeyInfo, bkt *bolt.Bucket) error {
	boltutil.ReadTimestamps(bkt, &skinfo.CreatedAt, &skinfo.UpdatedAt)

	skinfo.Name = string(bkt.Get(bucketKeyName))
	skinfo.Parent = string(bkt.Get(bucketKeyParent))
	skinfo.Labels, _ = boltutil.ReadLabels(bkt)

	return nil
}

// readOverlaySnapshotKey parses the snapshot key key-value pairs in metadata.db
func readOverlaySnapshotKey(skinfo *explorers.SnapshotKeyInfo, bkt *bolt.Bucket) error {
	boltutil.ReadTimestamps(bkt, &skinfo.CreatedAt, &skinfo.UpdatedAt)

	parent := string(bkt.Get(bucketKeyParent))
	if skinfo.Parent == "" {
		skinfo.Parent = parent
	} else if skinfo.Parent != parent {
		log.WithFields(log.Fields{
			"old parent": skinfo.Parent,
			"new parent": parent,
		}).Info("overwriting old parent with new parent")
	}

	skinfo.ID, _ = binary.Uvarint(bkt.Get(bucketKeyID))
	skinfo.OverlayPath = fmt.Sprintf("snapshots/%d/fs", skinfo.ID)

	kind, _ := binary.Uvarint(bkt.Get(bucketKeyKind))
	skinfo.Kind = snapshots.Kind(uint8(kind))

	skinfo.Size, _ = binary.Uvarint(bkt.Get(bucketKeySize))

	// Handle if skinfo already has labels from meta.db
	labels, _ := boltutil.ReadLabels(bkt)
	for k, v := range labels {
		if val, found := skinfo.Labels[k]; found {
			if v != val {
				log.WithFields(log.Fields{
					"existing value": val,
					"new value":      v,
				}).Warn("over writing old lable with new label")
			}
		} else {
			skinfo.Labels[k] = v
		}
	}
	return nil
}

// getSnapshotID returns snapshot key ID.
//
// This returns value of metadata.db/v1/snapshots/<snapshot key>/id
func getSnapshotID(tx *bolt.Tx, snapshotkey string) (uint64, error) {
	bkt := getOverlaySnapshotBucket(tx, snapshotkey)
	if bkt == nil {
		return 0, fmt.Errorf("empty snapshotkey bucket %s", snapshotkey)
	}

	id, _ := binary.Uvarint(bkt.Get(bucketKeyID))
	return id, nil
}

// snapshotRootDir returns snapshot root directory.
//
// In containerd, the default snapshot root directory is
// /var/lib/containerd/io.containerd.snapshotter.v1.overlayfs
func snapshotRootDir(root string, snapshotter string) string {
	dirs, _ := filepath.Glob(filepath.Join(root, "*"))
	for _, dir := range dirs {
		if strings.Contains(strings.ToLower(dir), strings.ToLower(snapshotter)) {
			return dir
		}
	}
	return ""
}
