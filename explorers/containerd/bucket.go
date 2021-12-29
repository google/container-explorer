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
	bolt "go.etcd.io/bbolt"
)

var (
	bucketKeyVersion         = []byte("v1")
	bucketKeyObjectSnapshots = []byte("snapshots") // stores snapshot references
	bucketKeyObjectContent   = []byte("content")   // stores content references
	bucketKeyObjectBlob      = []byte("blob")      // stores content links
	bucketKeySize            = []byte("size")
	bucketKeyName            = []byte("name")
	bucketKeyParent          = []byte("parent")
	bucketKeyKind            = []byte("kind")
	bucketKeyID              = []byte("id")
)

func getBucket(tx *bolt.Tx, keys ...[]byte) *bolt.Bucket {
	bkt := tx.Bucket(keys[0])

	for _, key := range keys[1:] {
		if bkt == nil {
			break
		}
		bkt = bkt.Bucket(key)
	}

	return bkt
}

func getBlobsBucket(tx *bolt.Tx, namespace string) *bolt.Bucket {
	return getBucket(tx, bucketKeyVersion, []byte(namespace), bucketKeyObjectContent, bucketKeyObjectBlob)
}

func getSnapshottersBucket(tx *bolt.Tx, namespace string) *bolt.Bucket {
	return getBucket(tx, bucketKeyVersion, []byte(namespace), bucketKeyObjectSnapshots)
}

func getSnapshotKeyBucket(tx *bolt.Tx, namespace, snapshotter, snapshotkey string) *bolt.Bucket {
	return getBucket(tx, bucketKeyVersion, []byte(namespace), bucketKeyObjectSnapshots, []byte(snapshotter), []byte(snapshotkey))
}

func getOverlaySnapshotBucket(tx *bolt.Tx, snapshotkey string) *bolt.Bucket {
	return getBucket(tx, bucketKeyVersion, bucketKeyObjectSnapshots, []byte(snapshotkey))
}
