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
	"time"

	"github.com/containerd/containerd/snapshots"
)

// SnapshotKeyInfo provides information about snapshots.
//
// SnapshotKeyInfo contains information found in containerd
// metadata (meta.db) and snapshot database (metadata.db).
type SnapshotKeyInfo struct {
	Namespace   string            // namespace only used in meta.db
	Snapshotter string            // only used in meta.db
	Key         string            // snapshot key
	ID          uint64            // File system ID. Only used in metadata.db
	Name        string            // snapshot name. Only used in meta.db
	Parent      string            // snapshot parent
	Kind        snapshots.Kind    // snapshot kind
	Inodes      []int64           // Inode numbers. Only in metadata.db
	Size        uint64            // Only in metadata.db
	OverlayPath string            // Custom field added by container explorer
	Labels      map[string]string // mapped labels
	Children    []string          // array of <snapshot key>. Only in meta.db
	CreatedAt   time.Time         // created timestamp
	UpdatedAt   time.Time         // updated timestamp
}
