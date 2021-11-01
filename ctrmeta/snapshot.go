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

package ctrmeta

import (
	"context"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/metadata/boltutil"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/snapshots"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	bolt "go.etcd.io/bbolt"
)

// SanpshotKeyInfo contains information extracted from manifest
// database i.e. meta.db/v1/<namespace>/snapshots/<snapshotter>/<snapshot_key>
// and path information i.e. namespace, snapshotter, key
type SnapshotKeyInfo struct {
	Namespace   string
	Snapshotter string
	Key         string
	Name        string
	Created     time.Time
	Updated     time.Time
	Parent      string
	Children    map[string]string
	Labels      map[string]string
	Unknown     map[string]interface{} // field to handl unknown attributes
}

// getContainerRootDir returns the effective root directory
// based the content of image-root and container-root
// command-line switches.
func getContainerRootDir(clictx *cli.Context) string {
	imageroot := clictx.GlobalString("image-root")
	containerroot := clictx.GlobalString("container-root")

	if imageroot != "" {
		return filepath.Join(imageroot, containerroot[1:])
	}
	return containerroot
}

// ContainerSnapshotEnvironment returns the root directory for snapshotter and
// database handler to metadata.db.
func ContainerSnapshotEnvironment(clictx *cli.Context, cinfo containers.Container) (string, *bolt.DB, func(), error) {
	var (
		root            string
		dbfile          string
		snapshotterroot string
	)

	snapshotdbfile := clictx.GlobalString("snapshot-metadata-file")

	if snapshotdbfile != "" {
		dbfile = snapshotdbfile
	} else {
		root = getContainerRootDir(clictx)
		log.WithField("path", root).Debug("container root directory")

		cpaths, err := filepath.Glob(filepath.Join(root, "*"))
		if err != nil {
			return "", nil, nil, err
		}

		for _, cpath := range cpaths {
			if strings.HasSuffix(cpath, cinfo.Snapshotter) {
				snapshotterroot = cpath
				break
			}
		}
		log.WithField("path", snapshotterroot).Debug("snapshotter root directory")

		dbfile = filepath.Join(snapshotterroot, "metadata.db")
	}

	log.WithField("path", dbfile).Debug("snapshotter database file")

	if !fileExists(dbfile) {
		return snapshotterroot, nil, nil, fmt.Errorf("snapshotter metadata file not found")
	}

	opt := &bolt.Options{
		ReadOnly: true,
	}

	db, err := bolt.Open(dbfile, 0555, opt)
	if err != nil {
		return snapshotterroot, nil, nil, err
	}

	return snapshotterroot, db, func() {
		db.Close()
	}, nil
}

// ListSnapshots returns an array of SnapshotInfo, which includes snapshot information
// for each namespace in meta.db
func ListSnapshots(ctx context.Context, db *bolt.DB) ([]SnapshotKeyInfo, error) {
	var sinfos []SnapshotKeyInfo

	ns, _ := namespaces.Namespace(ctx)
	if err := db.View(func(tx *bolt.Tx) error {
		ssbkt := GetBucket(tx, BucketKeyVersion, []byte(ns), BucketKeyObjectSnapshots)
		if ssbkt == nil {
			log.WithField("namespace", ns).Info("snapshotter bucket does no exist")
			sinfos = append(sinfos, SnapshotKeyInfo{
				Namespace: ns,
			})
			return nil
		}

		snapshotterNames, err := BucketKeys(ssbkt)
		if err != nil {
			log.Error("error listing snapshotter bukcet")
			return err
		}
		if snapshotterNames == nil {
			log.Info("no snapshotter keys")
			return nil
		}

		for _, snapshotterName := range snapshotterNames {
			// Snapshotter bucket
			sstrbkt := ssbkt.Bucket([]byte(snapshotterName))
			if sstrbkt == nil {
				log.WithField("bucket", snapshotterName).Info("empty bucket")
				return nil
			}

			snapshotKeys, err := BucketKeys(sstrbkt)
			if err != nil {
				log.WithFields(log.Fields{
					"namespace":   ns,
					"snapshotter": snapshotterName,
				}).Error("no bucket key names")
				return err
			}
			if snapshotKeys == nil {
				log.WithFields(log.Fields{
					"namespace":   ns,
					"snapshotter": snapshotterName,
				}).Info("empty bucket")
				return nil
			}

			for _, snapshotKey := range snapshotKeys {
				bkt := sstrbkt.Bucket([]byte(snapshotKey))

				sinfo := SnapshotKeyInfo{
					Namespace:   ns,
					Snapshotter: snapshotterName,
					Key:         snapshotKey,
					Name:        string(bkt.Get(BucketKeyName)),
					Parent:      string(bkt.Get(BucketKeyParent)),
				}

				err := boltutil.ReadTimestamps(bkt, &sinfo.Created, &sinfo.Updated)
				if err != nil {
					log.Error("error reading timestamp ", err)
					return err
				}
				sinfo.Labels, err = boltutil.ReadLabels(bkt)
				if err != nil {
					log.Error("error reading snapshot labels. ", err)
					return err
				}

				sinfos = append(sinfos, sinfo)
			}
		}
		// default return for db.View
		return nil
	}); err != nil {
		return nil, err
	}
	// default return
	return sinfos, nil
}

// ListSnapshotKeyChildren returns snapshot keys of the children
func ListSnapshotKeyChildren(bkt *bolt.Bucket) ([]string, error) {
	childrenBkt := bkt.Bucket(BucketKeyChildren)
	if childrenBkt == nil {
		return nil, nil
	}

	return BucketKeys(childrenBkt)
}

// ContainerParents returns the container parents as an array
//
// The first item in the array is the upperdir
// The last item in the array is the starting layer of the lowerdir.
//
// The parent of the last item in the array should be empty.
func ContainerParents(ctx context.Context, db *bolt.DB, cinfo containers.Container) ([]string, error) {
	var snapshotKeys []string

	if err := db.View(func(tx *bolt.Tx) error {
		vbkt := tx.Bucket(BucketKeyVersion)
		if vbkt == nil {
			return fmt.Errorf("v1 bucket does not exist")
		}

		ns, match := namespaces.Namespace(ctx)
		if !match {
			return fmt.Errorf("failed to get namespace from context")
		}
		nsbkt := vbkt.Bucket([]byte(ns))
		if nsbkt == nil {
			return fmt.Errorf("namespace bucket is empty")
		}

		ssbkt := nsbkt.Bucket(BucketKeyObjectSnapshots)
		if ssbkt == nil {
			return fmt.Errorf("snapshots bucket is empty")
		}

		sstrbkt := ssbkt.Bucket([]byte(cinfo.Snapshotter))
		if sstrbkt == nil {
			return fmt.Errorf("%s snapshotter bucket does not exist", cinfo.Snapshotter)
		}

		//snapshot key bucket
		sskey := cinfo.SnapshotKey
		for {
			sskbkt := sstrbkt.Bucket([]byte(sskey))
			name := string(sskbkt.Get(BucketKeyName))
			parent := string(sskbkt.Get(BucketKeyParent))
			snapshotKeys = append(snapshotKeys, name)
			sskey = parent

			if sskey == "" {
				break
			}
		}

		return nil
	}); err != nil {
		return snapshotKeys, err
	}

	return snapshotKeys, nil
}

// ContainerOverlayPaths returns lowerdir, upperdir, and workdir for a specified container
func ContainerOverlayPaths(ctx context.Context, parents []string, root string, db *bolt.DB, cinfo containers.Container) (string, string, string) {
	var (
		lowerdir string
		upperdir string
		workdir  string
	)

	if err := db.View(func(tx *bolt.Tx) error {
		vbkt := tx.Bucket(BucketKeyVersion)
		if vbkt == nil {
			return fmt.Errorf("v1 bucket does not exit")
		}
		sbkt := vbkt.Bucket(BucketKeyObjectSnapshots)
		if sbkt == nil {
			return fmt.Errorf("snapshots bucket does not exit")
		}

		// calculate upper directory
		upperid := GetSnapshotID(sbkt.Bucket([]byte(parents[0])))
		upperdir = filepath.Join(root, "snapshots", fmt.Sprintf("%d", upperid), "fs")
		workdir = filepath.Join(root, "snapshots", fmt.Sprintf("%d", upperid), "work")

		// computer lower directory
		for _, key := range parents[1:] {
			id := GetSnapshotID(sbkt.Bucket([]byte(key)))
			ldir := filepath.Join(root, "snapshots", fmt.Sprintf("%d", id), "fs")
			if lowerdir == "" {
				lowerdir = ldir
				continue
			}
			lowerdir = fmt.Sprintf("%s:%s", ldir, lowerdir)
		}
		return nil
	}); err != nil {
		return "", "", ""
	}
	return lowerdir, upperdir, workdir
}

// GetSnapshotID returns the numeric value of snapshot ID
// that is used for overlayfs directories identification.
func GetSnapshotID(bkt *bolt.Bucket) uint64 {
	idbyte := bkt.Get([]byte("id"))
	id, _ := binary.Uvarint(idbyte)
	return id
}

// GetSnapshotInfo returns information contained in snapshot metadata.db bucket.
// The path is metadata.db/v1/snapshots/<id>.
func GetSnapshotInfo(bkt *bolt.Bucket, name string) (snapshots.Info, error) {
	var info snapshots.Info

	tbkt := bkt.Bucket([]byte(name))
	info.Name = name
	info.Parent = string(tbkt.Get(BucketKeyParent))

	kind, _ := binary.Uvarint(tbkt.Get([]byte("kind")))
	info.Kind = snapshots.Kind(uint8(kind))
	boltutil.ReadTimestamps(tbkt, &info.Created, &info.Updated)
	info.Labels, _ = boltutil.ReadLabels(tbkt)

	return info, nil
}
