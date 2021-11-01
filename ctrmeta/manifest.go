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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/metadata"
	"github.com/containerd/containerd/metadata/boltutil"
	"github.com/google/container-explorer/config"
	"github.com/urfave/cli"
	bolt "go.etcd.io/bbolt"

	log "github.com/sirupsen/logrus"
)

// The vars below are treated as constant
// This is taken from github.com/containerd/containerd/manifest/bucket.go
var (
	SchemaVersion             = "v1"
	BucketKeyVersion          = []byte(SchemaVersion)
	BucketKeyDBVersion        = []byte("version")    // stores the version of the schema
	BucketKeyObjectLabels     = []byte("labels")     // stores the labels for a namespace.
	BucketKeyObjectImages     = []byte("images")     // stores image objects
	BucketKeyObjectContainers = []byte("containers") // stores container objects
	BucketKeyObjectSnapshots  = []byte("snapshots")  // stores snapshot references
	BucketKeyObjectContent    = []byte("content")    // stores content references
	BucketKeyObjectBlob       = []byte("blob")       // stores content links
	BucketKeyObjectIngests    = []byte("ingests")    // stores ingest objects
	BucketKeyObjectLeases     = []byte("leases")     // stores leases

	BucketKeyDigest      = []byte("digest")
	BucketKeyMediaType   = []byte("mediatype")
	BucketKeySize        = []byte("size")
	BucketKeyImage       = []byte("image")
	BucketKeyRuntime     = []byte("runtime")
	BucketKeyName        = []byte("name")
	BucketKeyParent      = []byte("parent")
	BucketKeyChildren    = []byte("children")
	BucketKeyOptions     = []byte("options")
	BucketKeySpec        = []byte("spec")
	BucketKeySnapshotKey = []byte("snapshotKey")
	BucketKeySnapshotter = []byte("snapshotter")
	BucketKeyTarget      = []byte("target")
	BucketKeyExtensions  = []byte("extensions")
	BucketKeyCreatedAt   = []byte("createdat")
	BucketKeyExpected    = []byte("expected")
	BucketKeyRef         = []byte("ref")
	BucketKeyExpireAt    = []byte("expireat")
	BucketKeyID          = []byte("id")
)

// GetContainerEnvironment provides provides context and environment settings based on user specified
// options.
//
// This function determines the local structure ContainerConfig which contains
// ContainerRoot: Computed containerd root directory
// Metapath: Full path of meta.db
//
// This function also returns pointer to bolt.DB which points to metadata's meta.db
// i.e. /var/lib/containerd/io.containerd.metadata.v1.bolt/meta.db
//
func GetContainerEnvironment(clictx *cli.Context) (context.Context, *config.Config, *bolt.DB, func(), error) {
	ctx, cancel := context.WithCancel(context.Background())

	var cc config.Config

	manifestfile := clictx.GlobalString("manifest-file")
	containerroot := clictx.GlobalString("container-root")
	imageroot := clictx.GlobalString("image-root")

	log.WithFields(log.Fields{
		"image-root":     imageroot,
		"container-root": containerroot,
		"manifest-file":  manifestfile,
	}).Debug("container environment")

	// If user has specified metadata path, it takes
	// priority over evaluated meta.db location based
	// on containr-root and image-root switches.

	if manifestfile != "" {
		cc.ManifestFile = manifestfile
	} else {
		cc.RootDir = containerroot

		if imageroot != "" {
			cc.RootDir = filepath.Join(
				imageroot,
				strings.Replace(containerroot, "/", "", 1),
			)
		}

		containerDirs, err := filepath.Glob(filepath.Join(cc.RootDir, "*"))
		if err != nil {
			panic(err)
		}

		if containerDirs == nil {
			return ctx, nil, nil, func() { cancel() }, fmt.Errorf("empty container root directory %s", cc.RootDir)
		}

		var metadataDir string
		for _, ctrDir := range containerDirs {
			if strings.Contains(ctrDir, "io.containerd.metadata") {
				metadataDir = ctrDir
				break
			}
		} //__end_for__
		cc.ManifestFile = filepath.Join(metadataDir, "meta.db")

		log.WithField("path", cc.ManifestFile).Debug("updated metadata file")
	} //__end_else__

	if !fileExists(cc.ManifestFile) {
		return ctx, nil, nil, func() { cancel() }, fmt.Errorf("metadata file %s does not exist", cc.ManifestFile)
	}

	opt := &bolt.Options{
		ReadOnly: true,
	}

	db, err := bolt.Open(cc.ManifestFile, 0444, opt)
	if err != nil {
		//panic(err)
		return ctx, nil, nil, func() { cancel() }, fmt.Errorf("error opening database %v", err)
	}

	return ctx, &cc, db, func() {
		db.Close()
		cancel()
	}, nil
}

// GetNamespaces returns namespaces from the manifest
// file meta.db/v1
func GetNamespaces(ctx context.Context, db *bolt.DB) ([]string, error) {
	var nss []string

	err := db.View(func(tx *bolt.Tx) error {
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

	return nss, err
}

// GetBucket returns the bucket for the provided keys
//
// This function is copied from github.com/containerd/containerd/metadata/bucket.go
func GetBucket(tx *bolt.Tx, keys ...[]byte) *bolt.Bucket {
	bkt := tx.Bucket(keys[0])

	for _, key := range keys[1:] {
		if bkt == nil {
			break
		}
		bkt = bkt.Bucket(key)
	}
	return bkt
}

// BucketKeys returns the key name for a given bucket
func BucketKeys(bkt *bolt.Bucket) ([]string, error) {
	var keysString []string

	if bkt == nil {
		return nil, fmt.Errorf("empty bucket")
	}

	err := bkt.ForEach(func(k, v []byte) error {
		keysString = append(keysString, string(k))
		return nil
	})
	if err != nil {
		return nil, err
	}

	// default return
	return keysString, nil
}

// GetBucketKeyNames returns key names in the specified bucket
func GetBucketKeyNames(db *bolt.DB, keys ...[]byte) ([]string, error) {
	var names []string

	for _, k := range keys {
		fmt.Println(string(k))
	}

	if err := db.View(func(tx *bolt.Tx) error {
		bkt := GetBucket(tx, keys...)
		if bkt != nil {
			return fmt.Errorf("empty bucket")
		}

		if err := bkt.ForEach(func(k, v []byte) error {
			names = append(names, string(k))
			// default return for bkt.ForEach
			return nil
		}); err != nil {
			return err
		}

		// default return for db.View
		return nil
	}); err != nil {
		return nil, err
	}

	// default
	return names, nil
}

// GetSnapshotterNames returns a list of snapshotter e.g. overlayfs from manifest file
// meta.db/v1/<namespace>/snapshots
func GetSnapshotterNames(db *bolt.DB, namespace string) ([]string, error) {
	return GetBucketKeyNames(db, BucketKeyVersion, []byte(namespace), BucketKeyObjectSnapshots)
}

// GetSnapshotNames returns a list of snapshot keys from meta.db/v1/<namespace>/snapshots/<snapshotter>
func GetSnapshotKeyNames(db *bolt.DB, namespace string, snapshotter string) ([]string, error) {
	return GetBucketKeyNames(db, BucketKeyVersion, []byte(namespace), BucketKeyObjectSnapshots, []byte(snapshotter))
}

// fileExists return true if a file or directory exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, os.ErrNotExist)
}

// ReadContentInfo reads Info structure from the specified bucket
//
// This code is implementation from github.com/containerd/containerd/metadata/buckets.go
func ReadContentInfo(info *content.Info, bkt *bolt.Bucket) error {
	if err := boltutil.ReadTimestamps(bkt, &info.CreatedAt, &info.UpdatedAt); err != nil {
		return err
	}

	labels, err := boltutil.ReadLabels(bkt)
	if err != nil {
		return err
	}
	info.Labels = labels

	if v := bkt.Get(BucketKeySize); len(v) > 0 {
		info.Size, _ = binary.Varint(v)
	}

	return nil
}
