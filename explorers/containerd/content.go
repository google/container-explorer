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

	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/metadata/boltutil"
	"github.com/containerd/containerd/namespaces"
	"github.com/opencontainers/go-digest"
	bolt "go.etcd.io/bbolt"
)

type blobStore struct {
	db *bolt.DB
}

// NewBlobStore returns blob store used for content operation
//
// In containerd, content information is stored in metadata file meta.db.
// i.e. meta.db/v1/<namespace>/content/blob/<blob digest>
func NewBlobStore(db *bolt.DB) *blobStore {
	return &blobStore{
		db: db,
	}
}

// List returns contents information.
func (c *blobStore) List(ctx context.Context) ([]content.Info, error) {
	namespace, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return nil, err
	}

	var infos []content.Info

	if err := c.db.View(func(tx *bolt.Tx) error {
		bkt := getBlobsBucket(tx, namespace)
		if bkt == nil {
			return nil // empty blob
		}

		return bkt.ForEach(func(k, v []byte) error {
			var (
				info = content.Info{
					Digest: digest.Digest(string(k)),
				}
				kbkt = bkt.Bucket(k)
			)

			if err := readBlob(&info, kbkt); err != nil {
				return err
			}

			infos = append(infos, info)
			return nil
		})
	}); err != nil {
		return nil, err
	}

	return infos, nil
}

func readBlob(info *content.Info, bkt *bolt.Bucket) error {
	if err := boltutil.ReadTimestamps(bkt, &info.CreatedAt, &info.UpdatedAt); err != nil {
		return err
	}

	labels, err := boltutil.ReadLabels(bkt)
	if err != nil {
		return err
	}
	info.Labels = labels

	if v := bkt.Get(bucketKeySize); len(v) > 0 {
		info.Size, _ = binary.Varint(v)
	}

	return nil
}
