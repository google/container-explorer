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

package containerd

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/metadata"
	"github.com/containerd/containerd/namespaces"
	"github.com/gogo/protobuf/types"
	"github.com/google/container-explorer/explorers"
	"github.com/google/container-explorer/utils"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	oci "github.com/opencontainers/runtime-spec/specs-go"
	bolt "go.etcd.io/bbolt"
)

func TestNewExplorer(t *testing.T) {
	tmpDir := t.TempDir()

	// Case 1: Containerd root does not exist
	_, err := NewExplorer("", filepath.Join(tmpDir, "non_existent"), "", "", nil)
	if err == nil {
		t.Errorf("NewExplorer expected error for non-existent containerd root, got nil")
	}

	// Case 2: Containerd root exists, but manifest meta.db is missing
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	if err := os.Mkdir(containerdRoot, 0755); err != nil {
		t.Fatalf("failed to create containerd root: %v", err)
	}
	_, err = NewExplorer("", containerdRoot, "", "", nil)
	if err == nil {
		t.Errorf("NewExplorer expected error for missing meta.db, got nil")
	}

	// Case 3: Valid meta.db
	metaDir := filepath.Join(containerdRoot, "io.containerd.metadata.v1.bolt")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatalf("failed to create meta dir: %v", err)
	}
	dbPath := filepath.Join(metaDir, "meta.db")
	db, err := bolt.Open(dbPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to create empty meta.db: %v", err)
	}
	db.Close()

	exp, err := NewExplorer("", containerdRoot, "", "", nil)
	if err != nil {
		t.Fatalf("NewExplorer failed: %v", err)
	}
	defer exp.Close()

	if exp.Type() != "containerd" {
		t.Errorf("expected type 'containerd', got '%s'", exp.Type())
	}
}

func TestListNamespaces(t *testing.T) {
	tmpDir := t.TempDir()
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	metaDir := filepath.Join(containerdRoot, "io.containerd.metadata.v1.bolt")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatalf("failed to create meta dir: %v", err)
	}

	dbPath := filepath.Join(metaDir, "meta.db")
	db, err := bolt.Open(dbPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to open bolt db: %v", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		store := metadata.NewNamespaceStore(tx)
		if err := store.Create(context.Background(), "ns1", nil); err != nil {
			return err
		}
		if err := store.Create(context.Background(), "ns2", nil); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		db.Close()
		t.Fatalf("failed to populate namespaces: %v", err)
	}
	db.Close()

	exp, err := NewExplorer("", containerdRoot, "", "", nil)
	if err != nil {
		t.Fatalf("failed to create explorer: %v", err)
	}
	defer exp.Close()

	nss, err := exp.ListNamespaces(context.Background())
	if err != nil {
		t.Fatalf("ListNamespaces failed: %v", err)
	}

	if len(nss) != 2 {
		t.Errorf("expected 2 namespaces, got %d", len(nss))
	}

	found1, found2 := false, false
	for _, ns := range nss {
		if ns == "ns1" {
			found1 = true
		}
		if ns == "ns2" {
			found2 = true
		}
	}
	if !found1 || !found2 {
		t.Errorf("expected namespaces 'ns1' and 'ns2', got %v", nss)
	}
}

func TestListContainers(t *testing.T) {
	tmpDir := t.TempDir()
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	metaDir := filepath.Join(containerdRoot, "io.containerd.metadata.v1.bolt")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatalf("failed to create meta dir: %v", err)
	}

	dbPath := filepath.Join(metaDir, "meta.db")
	db, err := bolt.Open(dbPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to open bolt db: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)

	// We need to write the container spec as JSON inside types.Any
	ociSpec := oci.Spec{
		Linux: &oci.Linux{
			CgroupsPath: "/default/c1",
		},
		Process: &oci.Process{
			Args: []string{"sleep", "10"},
		},
	}
	specBytes, err := json.Marshal(ociSpec)
	if err != nil {
		db.Close()
		t.Fatalf("failed to marshal oci spec: %v", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		nsStore := metadata.NewNamespaceStore(tx)
		return nsStore.Create(context.Background(), "ns1", nil)
	})
	if err != nil {
		db.Close()
		t.Fatalf("failed to populate namespace: %v", err)
	}

	dbStore := metadata.NewDB(db, nil, nil)
	cStore := metadata.NewContainerStore(dbStore)

	ctx := namespaces.WithNamespace(context.Background(), "ns1")
	_, err = cStore.Create(ctx, containers.Container{
		ID:          "c1",
		Image:       "ubuntu:latest",
		Snapshotter: "overlayfs",
		SnapshotKey: "c1-key",
		CreatedAt:   now,
		Runtime: containers.RuntimeInfo{
			Name: "io.containerd.runc.v2",
		},
		Spec: &types.Any{
			TypeUrl: "types.containerd.io/opencontainers/runtime-spec/1/Spec",
			Value:   specBytes,
		},
	})
	if err != nil {
		db.Close()
		t.Fatalf("failed to populate container: %v", err)
	}
	db.Close()

	sc, _ := explorers.NewSupportContainer("")
	exp, err := NewExplorer("", containerdRoot, "", "", sc)
	if err != nil {
		t.Fatalf("failed to create explorer: %v", err)
	}
	defer exp.Close()

	ctrs, err := exp.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers failed: %v", err)
	}

	if len(ctrs) != 1 {
		t.Fatalf("expected 1 container, got %d", len(ctrs))
	}

	ctr := ctrs[0]
	if ctr.ID != "c1" {
		t.Errorf("expected ID 'c1', got '%s'", ctr.ID)
	}
	if ctr.Image != "ubuntu:latest" {
		t.Errorf("expected Image 'ubuntu:latest', got '%s'", ctr.Image)
	}
	if ctr.Namespace != "ns1" {
		t.Errorf("expected Namespace 'ns1', got '%s'", ctr.Namespace)
	}
	if time.Since(ctr.CreatedAt) > 5*time.Second {
		t.Errorf("expected CreatedAt to be recent (within 5s), got %v", ctr.CreatedAt)
	}
}

func TestListImages(t *testing.T) {
	tmpDir := t.TempDir()
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	metaDir := filepath.Join(containerdRoot, "io.containerd.metadata.v1.bolt")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatalf("failed to create meta dir: %v", err)
	}

	dbPath := filepath.Join(metaDir, "meta.db")
	db, err := bolt.Open(dbPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to open bolt db: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)

	err = db.Update(func(tx *bolt.Tx) error {
		nsStore := metadata.NewNamespaceStore(tx)
		return nsStore.Create(context.Background(), "ns1", nil)
	})
	if err != nil {
		db.Close()
		t.Fatalf("failed to populate namespace: %v", err)
	}

	dbStore := metadata.NewDB(db, nil, nil)
	imgStore := metadata.NewImageStore(dbStore)

	ctx := namespaces.WithNamespace(context.Background(), "ns1")
	_, err = imgStore.Create(ctx, images.Image{
		Name: "ubuntu:latest",
		Target: ocispec.Descriptor{
			Digest:    "sha256:abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234",
			MediaType: "application/vnd.docker.distribution.manifest.v2+json",
			Size:      1024,
		},
		CreatedAt: now,
	})
	if err != nil {
		db.Close()
		t.Fatalf("failed to populate image: %v", err)
	}
	db.Close()

	sc, _ := explorers.NewSupportContainer("")
	exp, err := NewExplorer("", containerdRoot, "", "", sc)
	if err != nil {
		t.Fatalf("failed to create explorer: %v", err)
	}
	defer exp.Close()

	imgs, err := exp.ListImages(context.Background())
	if err != nil {
		t.Fatalf("ListImages failed: %v", err)
	}

	if len(imgs) != 1 {
		t.Fatalf("expected 1 image, got %d", len(imgs))
	}

	img := imgs[0]
	if img.Name != "ubuntu:latest" {
		t.Errorf("expected Image Name 'ubuntu:latest', got '%s'", img.Name)
	}
	if string(img.Target.Digest) != "sha256:abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234" {
		t.Errorf("expected Image Digest 'sha256:abcd...', got '%s'", string(img.Target.Digest))
	}
	if img.Namespace != "ns1" {
		t.Errorf("expected Namespace 'ns1', got '%s'", img.Namespace)
	}
	if time.Since(img.CreatedAt) > 5*time.Second {
		t.Errorf("expected CreatedAt to be recent (within 5s), got %v", img.CreatedAt)
	}
}

func TestListContent(t *testing.T) {
	tmpDir := t.TempDir()
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	metaDir := filepath.Join(containerdRoot, "io.containerd.metadata.v1.bolt")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatalf("failed to create meta dir: %v", err)
	}

	dbPath := filepath.Join(metaDir, "meta.db")
	db, err := bolt.Open(dbPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to open bolt db: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	digest := "sha256:abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234"

	err = db.Update(func(tx *bolt.Tx) error {
		nsStore := metadata.NewNamespaceStore(tx)
		if err := nsStore.Create(context.Background(), "ns1", nil); err != nil {
			return err
		}

		// Write blob directly
		return createBlob(tx, "ns1", digest, 1024, now, map[string]string{"k1": "v1"})
	})
	if err != nil {
		db.Close()
		t.Fatalf("failed to populate blob: %v", err)
	}
	db.Close()

	sc, _ := explorers.NewSupportContainer("")
	exp, err := NewExplorer("", containerdRoot, "", "", sc)
	if err != nil {
		t.Fatalf("failed to create explorer: %v", err)
	}
	defer exp.Close()

	contents, err := exp.ListContent(context.Background())
	if err != nil {
		t.Fatalf("ListContent failed: %v", err)
	}

	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}

	content := contents[0]
	if string(content.Digest) != digest {
		t.Errorf("expected Digest '%s', got '%s'", digest, string(content.Digest))
	}
	if content.Size != 1024 {
		t.Errorf("expected Size 1024, got %d", content.Size)
	}
	if content.Namespace != "ns1" {
		t.Errorf("expected Namespace 'ns1', got '%s'", content.Namespace)
	}
	if !content.CreatedAt.Equal(now) {
		t.Errorf("expected CreatedAt %v, got %v", now, content.CreatedAt)
	}
	if content.Labels["k1"] != "v1" {
		t.Errorf("expected label k1='v1', got '%s'", content.Labels["k1"])
	}
}

func createBlob(tx *bolt.Tx, namespace, digest string, size int64, createdAt time.Time, labels map[string]string) error {
	v1Bkt, err := tx.CreateBucketIfNotExists([]byte("v1"))
	if err != nil {
		return err
	}
	nsBkt, err := v1Bkt.CreateBucketIfNotExists([]byte(namespace))
	if err != nil {
		return err
	}
	contentBkt, err := nsBkt.CreateBucketIfNotExists([]byte("content"))
	if err != nil {
		return err
	}
	blobBkt, err := contentBkt.CreateBucketIfNotExists([]byte("blob"))
	if err != nil {
		return err
	}
	digBkt, err := blobBkt.CreateBucketIfNotExists([]byte(digest))
	if err != nil {
		return err
	}

	// Size
	buf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutVarint(buf, size)
	if err := digBkt.Put([]byte("size"), buf[:n]); err != nil {
		return err
	}

	// Timestamps
	tBytes, err := createdAt.MarshalBinary()
	if err != nil {
		return err
	}
	if err := digBkt.Put([]byte("createdat"), tBytes); err != nil {
		return err
	}
	if err := digBkt.Put([]byte("updatedat"), tBytes); err != nil {
		return err
	}

	// Labels
	if len(labels) > 0 {
		labelsBkt, err := digBkt.CreateBucketIfNotExists([]byte("labels"))
		if err != nil {
			return err
		}
		for k, v := range labels {
			if err := labelsBkt.Put([]byte(k), []byte(v)); err != nil {
				return err
			}
		}
	}

	return nil
}

func TestListSnapshots(t *testing.T) {
	tmpDir := t.TempDir()
	containerdRoot := filepath.Join(tmpDir, "containerd_root")

	// Create meta.db dir
	metaDir := filepath.Join(containerdRoot, "io.containerd.metadata.v1.bolt")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatalf("failed to create meta dir: %v", err)
	}

	// Create snapshotter dir
	snapshotterDir := filepath.Join(containerdRoot, "io.containerd.snapshotter.v1.overlayfs")
	if err := os.MkdirAll(snapshotterDir, 0755); err != nil {
		t.Fatalf("failed to create snapshotter dir: %v", err)
	}

	// Open and populate meta.db
	metaDBPath := filepath.Join(metaDir, "meta.db")
	metaDB, err := bolt.Open(metaDBPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to open meta.db: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)

	err = metaDB.Update(func(tx *bolt.Tx) error {
		nsStore := metadata.NewNamespaceStore(tx)
		if err := nsStore.Create(context.Background(), "ns1", nil); err != nil {
			return err
		}
		// Create meta snapshot in ns1, overlayfs, key="snap1"
		return createMetaSnapshot(tx, "ns1", "overlayfs", "snap1", "snapshot-name-1", "parent-1", now)
	})
	if err != nil {
		metaDB.Close()
		t.Fatalf("failed to populate meta.db: %v", err)
	}
	metaDB.Close()

	// Open and populate snapshotter metadata.db
	ssDBPath := filepath.Join(snapshotterDir, "metadata.db")
	ssDB, err := bolt.Open(ssDBPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to open snapshotter metadata.db: %v", err)
	}

	err = ssDB.Update(func(tx *bolt.Tx) error {
		// Create overlay snapshot for key="snap1", id=42, kind=1
		return createOverlaySnapshot(tx, "snapshot-name-1", 42, 2, "parent-1", 10240, now)
	})
	if err != nil {
		ssDB.Close()
		t.Fatalf("failed to populate snapshotter metadata.db: %v", err)
	}
	ssDB.Close()

	sc, _ := explorers.NewSupportContainer("")
	exp, err := NewExplorer("", containerdRoot, "", "", sc)
	if err != nil {
		t.Fatalf("failed to create explorer: %v", err)
	}
	defer exp.Close()

	snaps, err := exp.ListSnapshots(context.Background())
	if err != nil {
		t.Fatalf("ListSnapshots failed: %v", err)
	}

	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snaps))
	}

	snap := snaps[0]
	if snap.Key != "snap1" {
		t.Errorf("expected Key 'snap1', got '%s'", snap.Key)
	}
	if snap.Name != "snapshot-name-1" {
		t.Errorf("expected Name 'snapshot-name-1', got '%s'", snap.Name)
	}
	if snap.Parent != "parent-1" {
		t.Errorf("expected Parent 'parent-1', got '%s'", snap.Parent)
	}
	if snap.ID != 42 {
		t.Errorf("expected ID 42, got %d", snap.ID)
	}
	if snap.Kind != 2 {
		t.Errorf("expected Kind 2, got %d", snap.Kind)
	}
	if snap.Size != 10240 {
		t.Errorf("expected Size 10240, got %d", snap.Size)
	}
	if snap.Namespace != "ns1" {
		t.Errorf("expected Namespace 'ns1', got '%s'", snap.Namespace)
	}
	if !snap.CreatedAt.Equal(now) {
		t.Errorf("expected CreatedAt %v, got %v", now, snap.CreatedAt)
	}
}

func createMetaSnapshot(tx *bolt.Tx, namespace, snapshotter, snapshotKey, name, parent string, createdAt time.Time) error {
	v1Bkt, err := tx.CreateBucketIfNotExists([]byte("v1"))
	if err != nil {
		return err
	}
	nsBkt, err := v1Bkt.CreateBucketIfNotExists([]byte(namespace))
	if err != nil {
		return err
	}
	snapshotsBkt, err := nsBkt.CreateBucketIfNotExists([]byte("snapshots"))
	if err != nil {
		return err
	}
	sterBkt, err := snapshotsBkt.CreateBucketIfNotExists([]byte(snapshotter))
	if err != nil {
		return err
	}
	keyBkt, err := sterBkt.CreateBucketIfNotExists([]byte(snapshotKey))
	if err != nil {
		return err
	}

	if err := keyBkt.Put([]byte("name"), []byte(name)); err != nil {
		return err
	}
	if err := keyBkt.Put([]byte("parent"), []byte(parent)); err != nil {
		return err
	}

	tBytes, err := createdAt.MarshalBinary()
	if err != nil {
		return err
	}
	if err := keyBkt.Put([]byte("createdat"), tBytes); err != nil {
		return err
	}
	if err := keyBkt.Put([]byte("updatedat"), tBytes); err != nil {
		return err
	}

	return nil
}

func createOverlaySnapshot(tx *bolt.Tx, snapshotKey string, id uint64, kind uint8, parent string, size uint64, createdAt time.Time) error {
	v1Bkt, err := tx.CreateBucketIfNotExists([]byte("v1"))
	if err != nil {
		return err
	}
	snapshotsBkt, err := v1Bkt.CreateBucketIfNotExists([]byte("snapshots"))
	if err != nil {
		return err
	}
	keyBkt, err := snapshotsBkt.CreateBucketIfNotExists([]byte(snapshotKey))
	if err != nil {
		return err
	}

	// ID (uvarint)
	idBuf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(idBuf, id)
	if err := keyBkt.Put([]byte("id"), idBuf[:n]); err != nil {
		return err
	}

	// Kind (uvarint)
	kindBuf := make([]byte, binary.MaxVarintLen64)
	n = binary.PutUvarint(kindBuf, uint64(kind))
	if err := keyBkt.Put([]byte("kind"), kindBuf[:n]); err != nil {
		return err
	}

	// Parent (string)
	if err := keyBkt.Put([]byte("parent"), []byte(parent)); err != nil {
		return err
	}

	// Size (uvarint)
	sizeBuf := make([]byte, binary.MaxVarintLen64)
	n = binary.PutUvarint(sizeBuf, size)
	if err := keyBkt.Put([]byte("size"), sizeBuf[:n]); err != nil {
		return err
	}

	// Timestamps
	tBytes, err := createdAt.MarshalBinary()
	if err != nil {
		return err
	}
	if err := keyBkt.Put([]byte("createdat"), tBytes); err != nil {
		return err
	}
	if err := keyBkt.Put([]byte("updatedat"), tBytes); err != nil {
		return err
	}

	return nil
}

func TestContainerd_DatabaseClosedErrorCases(t *testing.T) {
	tmpDir := t.TempDir()
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	metaDir := filepath.Join(containerdRoot, "io.containerd.metadata.v1.bolt")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatalf("failed to create meta dir: %v", err)
	}

	dbPath := filepath.Join(metaDir, "meta.db")
	db, err := bolt.Open(dbPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to create empty meta.db: %v", err)
	}
	db.Close()

	sc, _ := explorers.NewSupportContainer("")
	exp, err := NewExplorer("", containerdRoot, "", "", sc)
	if err != nil {
		t.Fatalf("NewExplorer failed: %v", err)
	}
	// Close the explorer database immediately to trigger errors
	exp.Close()

	ctx := context.Background()

	if _, err := exp.ListNamespaces(ctx); err == nil {
		t.Errorf("ListNamespaces expected error when DB is closed, got nil")
	}

	if _, err := exp.ListContainers(ctx); err == nil {
		t.Errorf("ListContainers expected error when DB is closed, got nil")
	}

	if _, err := exp.ListImages(ctx); err == nil {
		t.Errorf("ListImages expected error when DB is closed, got nil")
	}

	if _, err := exp.ListContent(ctx); err == nil {
		t.Errorf("ListContent expected error when DB is closed, got nil")
	}

	if _, err := exp.ListSnapshots(ctx); err == nil {
		t.Errorf("ListSnapshots expected error when DB is closed, got nil")
	}
}

func TestListSnapshots_InvalidKind(t *testing.T) {
	tmpDir := t.TempDir()
	containerdRoot := filepath.Join(tmpDir, "containerd_root")

	// Create meta.db dir
	metaDir := filepath.Join(containerdRoot, "io.containerd.metadata.v1.bolt")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatalf("failed to create meta dir: %v", err)
	}

	// Create snapshotter dir
	snapshotterDir := filepath.Join(containerdRoot, "io.containerd.snapshotter.v1.overlayfs")
	if err := os.MkdirAll(snapshotterDir, 0755); err != nil {
		t.Fatalf("failed to create snapshotter dir: %v", err)
	}

	// Open and populate meta.db
	metaDBPath := filepath.Join(metaDir, "meta.db")
	metaDB, err := bolt.Open(metaDBPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to open meta.db: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)

	err = metaDB.Update(func(tx *bolt.Tx) error {
		nsStore := metadata.NewNamespaceStore(tx)
		if err := nsStore.Create(context.Background(), "ns1", nil); err != nil {
			return err
		}
		// Create meta snapshot in ns1, overlayfs, key="snap1"
		return createMetaSnapshot(tx, "ns1", "overlayfs", "snap1", "snapshot-name-1", "parent-1", now)
	})
	if err != nil {
		metaDB.Close()
		t.Fatalf("failed to populate meta.db: %v", err)
	}
	metaDB.Close()

	// Open and populate snapshotter metadata.db with invalid kind
	ssDBPath := filepath.Join(snapshotterDir, "metadata.db")
	ssDB, err := bolt.Open(ssDBPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to open snapshotter metadata.db: %v", err)
	}

	err = ssDB.Update(func(tx *bolt.Tx) error {
		v1Bkt, err := tx.CreateBucketIfNotExists([]byte("v1"))
		if err != nil {
			return err
		}
		snapshotsBkt, err := v1Bkt.CreateBucketIfNotExists([]byte("snapshots"))
		if err != nil {
			return err
		}
		keyBkt, err := snapshotsBkt.CreateBucketIfNotExists([]byte("snapshot-name-1"))
		if err != nil {
			return err
		}

		// Write ID
		idBuf := make([]byte, binary.MaxVarintLen64)
		n := binary.PutUvarint(idBuf, 42)
		if err := keyBkt.Put([]byte("id"), idBuf[:n]); err != nil {
			return err
		}

		// Write invalid Kind (256)
		kindBuf := make([]byte, binary.MaxVarintLen64)
		n = binary.PutUvarint(kindBuf, 256)
		if err := keyBkt.Put([]byte("kind"), kindBuf[:n]); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		ssDB.Close()
		t.Fatalf("failed to populate snapshotter metadata.db: %v", err)
	}
	ssDB.Close()

	sc, _ := explorers.NewSupportContainer("")
	exp, err := NewExplorer("", containerdRoot, "", "", sc)
	if err != nil {
		t.Fatalf("failed to create explorer: %v", err)
	}
	defer exp.Close()

	snaps, err := exp.ListSnapshots(context.Background())
	if err == nil {
		t.Errorf("ListSnapshots expected error when snapshot Kind is invalid (>255) in metadata.db, got nil. Snaps: %+v", snaps)
	}
}

type mockCommandCall struct {
	Name string
	Args []string
}

type mockCommandResponse struct {
	Output []byte
	Stdout string
	Stderr string
	Err    error
}

type mockCommandRunner struct {
	Calls     []mockCommandCall
	Responses map[string]mockCommandResponse
}

func (m *mockCommandRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	m.Calls = append(m.Calls, mockCommandCall{Name: name, Args: args})
	if r, ok := m.Responses[name]; ok {
		return r.Output, r.Err
	}
	return nil, nil
}

func (m *mockCommandRunner) RunSeparate(_ context.Context, name string, args []string, stdout, stderr io.Writer) error {
	m.Calls = append(m.Calls, mockCommandCall{Name: name, Args: args})
	if r, ok := m.Responses[name]; ok {
		if r.Stdout != "" {
			_, _ = stdout.Write([]byte(r.Stdout))
		}
		if r.Stderr != "" {
			_, _ = stderr.Write([]byte(r.Stderr))
		}
		return r.Err
	}
	return nil
}

func (m *mockCommandRunner) RunWithoutContext(name string, args ...string) ([]byte, error) {
	m.Calls = append(m.Calls, mockCommandCall{Name: name, Args: args})
	if r, ok := m.Responses[name]; ok {
		return r.Output, r.Err
	}
	return nil, nil
}

func TestExportContainer(t *testing.T) {
	tmpDir := t.TempDir()
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	_ = os.Mkdir(containerdRoot, 0755)
	metaDir := filepath.Join(containerdRoot, "io.containerd.metadata.v1.bolt")
	_ = os.MkdirAll(metaDir, 0755)
	dbPath := filepath.Join(metaDir, "meta.db")
	db, err := bolt.Open(dbPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to open meta.db: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)

	_ = db.Update(func(tx *bolt.Tx) error {
		nsStore := metadata.NewNamespaceStore(tx)
		return nsStore.Create(context.Background(), "ns1", nil)
	})

	dbStore := metadata.NewDB(db, nil, nil)
	cStore := metadata.NewContainerStore(dbStore)

	specObj := oci.Spec{
		Linux: &oci.Linux{
			CgroupsPath: "/default/container-1",
		},
		Process: &oci.Process{
			Args: []string{"sleep", "10"},
		},
	}
	specJSON, _ := json.Marshal(specObj)
	anySpec := &types.Any{
		TypeUrl: "types.containerd.io/opencontainers/runtime-spec/1/Spec",
		Value:   specJSON,
	}

	c := containers.Container{
		ID:          "container-1",
		Image:       "ubuntu:latest",
		Snapshotter: "overlayfs",
		SnapshotKey: "snap1",
		Runtime: containers.RuntimeInfo{
			Name: "io.containerd.runc.v2",
		},
		Spec: anySpec,
	}
	_, err = cStore.Create(namespaces.WithNamespace(context.Background(), "ns1"), c)
	if err != nil {
		db.Close()
		t.Fatalf("failed to create container: %v", err)
	}

	_ = db.Update(func(tx *bolt.Tx) error {
		// Create meta snapshots (need a parent so lowerdir is not empty)
		_ = createMetaSnapshot(tx, "ns1", "overlayfs", "snap1", "snapshot-name-1", "snapshot-name-parent", now)
		return createMetaSnapshot(tx, "ns1", "overlayfs", "snapshot-name-parent", "snapshot-name-parent", "", now)
	})
	db.Close()

	// Open and populate snapshotter metadata.db
	snapshotterDir := filepath.Join(containerdRoot, "io.containerd.snapshotter.v1.overlayfs")
	_ = os.MkdirAll(snapshotterDir, 0755)
	ssDBPath := filepath.Join(snapshotterDir, "metadata.db")
	ssDB, err := bolt.Open(ssDBPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to open snapshotter metadata.db: %v", err)
	}
	_ = ssDB.Update(func(tx *bolt.Tx) error {
		_ = createOverlaySnapshot(tx, "snapshot-name-1", 42, 2, "snapshot-name-parent", 10240, now)
		return createOverlaySnapshot(tx, "snapshot-name-parent", 41, 2, "", 10240, now)
	})
	ssDB.Close()

	// Create directories on disk so Glob matches
	_ = os.MkdirAll(filepath.Join(snapshotterDir, "snapshots", "42", "work"), 0755)
	_ = os.MkdirAll(filepath.Join(snapshotterDir, "snapshots", "42", "fs"), 0755)
	_ = os.MkdirAll(filepath.Join(snapshotterDir, "snapshots", "41", "fs"), 0755)

	sc, _ := explorers.NewSupportContainer("")
	exp, err := NewExplorer("", containerdRoot, "", "", sc)
	if err != nil {
		t.Fatalf("failed to create explorer: %v", err)
	}
	defer exp.Close()

	// Override runner
	origRunner := utils.Runner
	mockRunner := &mockCommandRunner{
		Responses: map[string]mockCommandResponse{
			"losetup": {Stdout: "/dev/loop123\n", Err: nil},
		},
	}
	utils.Runner = mockRunner
	defer func() { utils.Runner = origRunner }()

	outputDir := filepath.Join(tmpDir, "output")
	exportOptions := map[string]bool{
		"archive": true,
	}

	err = exp.ExportContainer(context.Background(), "container-1", outputDir, exportOptions)
	if err != nil {
		t.Fatalf("ExportContainer failed: %v", err)
	}

	hasMount := false
	hasUmount := false
	hasTar := false
	for _, c := range mockRunner.Calls {
		if c.Name == "mount" {
			hasMount = true
		}
		if c.Name == "umount" {
			hasUmount = true
		}
		if c.Name == "tar" {
			hasTar = true
		}
	}

	if !hasMount {
		t.Errorf("expected 'mount' to be executed")
	}
	if !hasUmount {
		t.Errorf("expected 'umount' to be executed")
	}
	if !hasTar {
		t.Errorf("expected 'tar' to be executed")
	}
}

func TestExportAllContainers(t *testing.T) {
	tmpDir := t.TempDir()
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	_ = os.Mkdir(containerdRoot, 0755)

	metaDir := filepath.Join(containerdRoot, "io.containerd.metadata.v1.bolt")
	_ = os.MkdirAll(metaDir, 0755)
	dbPath := filepath.Join(metaDir, "meta.db")
	db, err := bolt.Open(dbPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to open meta.db: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)

	_ = db.Update(func(tx *bolt.Tx) error {
		nsStore := metadata.NewNamespaceStore(tx)
		return nsStore.Create(context.Background(), "ns1", nil)
	})

	dbStore := metadata.NewDB(db, nil, nil)
	cStore := metadata.NewContainerStore(dbStore)

	specObj := oci.Spec{
		Linux: &oci.Linux{
			CgroupsPath: "/default/container-1",
		},
		Process: &oci.Process{
			Args: []string{"sleep", "10"},
		},
	}
	specJSON, _ := json.Marshal(specObj)
	anySpec := &types.Any{
		TypeUrl: "types.containerd.io/opencontainers/runtime-spec/1/Spec",
		Value:   specJSON,
	}

	c := containers.Container{
		ID:          "container-1",
		Image:       "ubuntu:latest",
		Snapshotter: "overlayfs",
		SnapshotKey: "snap1",
		Runtime: containers.RuntimeInfo{
			Name: "io.containerd.runc.v2",
		},
		Spec: anySpec,
	}
	_, err = cStore.Create(namespaces.WithNamespace(context.Background(), "ns1"), c)
	if err != nil {
		db.Close()
		t.Fatalf("failed to create container: %v", err)
	}

	_ = db.Update(func(tx *bolt.Tx) error {
		_ = createMetaSnapshot(tx, "ns1", "overlayfs", "snap1", "snapshot-name-1", "snapshot-name-parent", now)
		return createMetaSnapshot(tx, "ns1", "overlayfs", "snapshot-name-parent", "snapshot-name-parent", "", now)
	})
	db.Close()

	snapshotterDir := filepath.Join(containerdRoot, "io.containerd.snapshotter.v1.overlayfs")
	_ = os.MkdirAll(snapshotterDir, 0755)
	ssDBPath := filepath.Join(snapshotterDir, "metadata.db")
	ssDB, err := bolt.Open(ssDBPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to open snapshotter metadata.db: %v", err)
	}
	_ = ssDB.Update(func(tx *bolt.Tx) error {
		_ = createOverlaySnapshot(tx, "snapshot-name-1", 42, 2, "snapshot-name-parent", 10240, now)
		return createOverlaySnapshot(tx, "snapshot-name-parent", 41, 2, "", 10240, now)
	})
	ssDB.Close()

	_ = os.MkdirAll(filepath.Join(snapshotterDir, "snapshots", "42", "work"), 0755)
	_ = os.MkdirAll(filepath.Join(snapshotterDir, "snapshots", "42", "fs"), 0755)
	_ = os.MkdirAll(filepath.Join(snapshotterDir, "snapshots", "41", "fs"), 0755)

	sc, _ := explorers.NewSupportContainer("")
	exp, err := NewExplorer("", containerdRoot, "", "", sc)
	if err != nil {
		t.Fatalf("failed to create explorer: %v", err)
	}
	defer exp.Close()

	// Override runner
	origRunner := utils.Runner
	mockRunner := &mockCommandRunner{
		Responses: map[string]mockCommandResponse{
			"losetup": {Stdout: "/dev/loop123\n", Err: nil},
		},
	}
	utils.Runner = mockRunner
	defer func() { utils.Runner = origRunner }()

	outputDir := filepath.Join(tmpDir, "output")
	exportOptions := map[string]bool{
		"archive": true,
	}

	err = exp.ExportAllContainers(namespaces.WithNamespace(context.Background(), "ns1"), outputDir, exportOptions, nil, false)
	if err != nil {
		t.Fatalf("ExportAllContainers failed: %v", err)
	}

	exportedArchive := filepath.Join(outputDir, "container-1.tar.gz")
	hasTar := false
	for _, c := range mockRunner.Calls {
		if c.Name == "tar" {
			hasTar = true
			foundOut := false
			for _, arg := range c.Args {
				if arg == exportedArchive {
					foundOut = true
				}
			}
			if !foundOut {
				t.Errorf("expected tar command to export to '%s', args were: %v", exportedArchive, c.Args)
			}
		}
	}
	if !hasTar {
		t.Errorf("expected 'tar' to be executed")
	}
}

func TestContainerDrift(t *testing.T) {
	tmpDir := t.TempDir()
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	_ = os.Mkdir(containerdRoot, 0755)

	metaDir := filepath.Join(containerdRoot, "io.containerd.metadata.v1.bolt")
	_ = os.MkdirAll(metaDir, 0755)
	dbPath := filepath.Join(metaDir, "meta.db")
	db, err := bolt.Open(dbPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to open meta.db: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)

	_ = db.Update(func(tx *bolt.Tx) error {
		nsStore := metadata.NewNamespaceStore(tx)
		return nsStore.Create(context.Background(), "ns1", nil)
	})

	dbStore := metadata.NewDB(db, nil, nil)
	cStore := metadata.NewContainerStore(dbStore)

	specObj := oci.Spec{
		Linux: &oci.Linux{
			CgroupsPath: "/default/container-1",
		},
		Process: &oci.Process{
			Args: []string{"sleep", "10"},
		},
	}
	specJSON, _ := json.Marshal(specObj)
	anySpec := &types.Any{
		TypeUrl: "types.containerd.io/opencontainers/runtime-spec/1/Spec",
		Value:   specJSON,
	}

	c := containers.Container{
		ID:          "container-1",
		Image:       "ubuntu:latest",
		Snapshotter: "overlayfs",
		SnapshotKey: "snap1",
		Runtime: containers.RuntimeInfo{
			Name: "io.containerd.runc.v2",
		},
		Spec: anySpec,
	}
	_, err = cStore.Create(namespaces.WithNamespace(context.Background(), "ns1"), c)
	if err != nil {
		db.Close()
		t.Fatalf("failed to create container: %v", err)
	}

	_ = db.Update(func(tx *bolt.Tx) error {
		_ = createMetaSnapshot(tx, "ns1", "overlayfs", "snap1", "snapshot-name-1", "snapshot-name-parent", now)
		return createMetaSnapshot(tx, "ns1", "overlayfs", "snapshot-name-parent", "snapshot-name-parent", "", now)
	})
	db.Close()

	snapshotterDir := filepath.Join(containerdRoot, "io.containerd.snapshotter.v1.overlayfs")
	_ = os.MkdirAll(snapshotterDir, 0755)
	ssDBPath := filepath.Join(snapshotterDir, "metadata.db")
	ssDB, err := bolt.Open(ssDBPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to open snapshotter metadata.db: %v", err)
	}
	_ = ssDB.Update(func(tx *bolt.Tx) error {
		_ = createOverlaySnapshot(tx, "snapshot-name-1", 42, 2, "snapshot-name-parent", 10240, now)
		return createOverlaySnapshot(tx, "snapshot-name-parent", 41, 2, "", 10240, now)
	})
	ssDB.Close()

	// Create directories on disk and put a drift file in the upperdir
	upperDir := filepath.Join(snapshotterDir, "snapshots", "42", "fs")
	_ = os.MkdirAll(upperDir, 0755)
	_ = os.MkdirAll(filepath.Join(snapshotterDir, "snapshots", "42", "work"), 0755)
	driftFile := filepath.Join(upperDir, "etc", "test.conf")
	_ = os.MkdirAll(filepath.Dir(driftFile), 0755)
	_ = os.WriteFile(driftFile, []byte("some config change"), 0600)

	sc, _ := explorers.NewSupportContainer("")
	exp, err := NewExplorer("", containerdRoot, "", "", sc)
	if err != nil {
		t.Fatalf("failed to create explorer: %v", err)
	}
	defer exp.Close()

	drifts, err := exp.ContainerDrift(namespaces.WithNamespace(context.Background(), "ns1"), "", false, "container-1")
	if err != nil {
		t.Fatalf("ContainerDrift failed: %v", err)
	}

	if len(drifts) != 1 {
		t.Fatalf("expected 1 drift, got %d", len(drifts))
	}

	drift := drifts[0]
	if drift.ContainerID != "container-1" {
		t.Errorf("expected drift ContainerID 'container-1', got '%s'", drift.ContainerID)
	}

	if len(drift.AddedOrModified) != 1 {
		t.Fatalf("expected 1 added/modified file, got %d", len(drift.AddedOrModified))
	}

	expectedPath := "/etc/test.conf"
	if drift.AddedOrModified[0].FullPath != expectedPath {
		t.Errorf("expected drift path '%s', got '%s'", expectedPath, drift.AddedOrModified[0].FullPath)
	}
}

func TestListTasks(t *testing.T) {
	tmpDir := t.TempDir()
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	_ = os.Mkdir(containerdRoot, 0755)

	metaDir := filepath.Join(containerdRoot, "io.containerd.metadata.v1.bolt")
	_ = os.MkdirAll(metaDir, 0755)
	dbPath := filepath.Join(metaDir, "meta.db")
	db, err := bolt.Open(dbPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to open meta.db: %v", err)
	}

	_ = db.Update(func(tx *bolt.Tx) error {
		nsStore := metadata.NewNamespaceStore(tx)
		return nsStore.Create(context.Background(), "ns1", nil)
	})

	dbStore := metadata.NewDB(db, nil, nil)
	cStore := metadata.NewContainerStore(dbStore)

	specObj := oci.Spec{
		Linux: &oci.Linux{
			CgroupsPath: "/default/container-1",
		},
		Process: &oci.Process{
			Args: []string{"sleep", "10"},
		},
	}
	specJSON, _ := json.Marshal(specObj)
	anySpec := &types.Any{
		TypeUrl: "types.containerd.io/opencontainers/runtime-spec/1/Spec",
		Value:   specJSON,
	}

	c := containers.Container{
		ID:          "container-1",
		Image:       "ubuntu:latest",
		Snapshotter: "overlayfs",
		SnapshotKey: "snap1",
		Runtime: containers.RuntimeInfo{
			Name: "io.containerd.runc.v2",
		},
		Spec: anySpec,
	}
	_, err = cStore.Create(namespaces.WithNamespace(context.Background(), "ns1"), c)
	if err != nil {
		db.Close()
		t.Fatalf("failed to create container: %v", err)
	}
	db.Close()

	sc, _ := explorers.NewSupportContainer("")
	// Set imageRoot to non-empty
	exp, err := NewExplorer("some_image_root", containerdRoot, "", "", sc)
	if err != nil {
		t.Fatalf("failed to create explorer: %v", err)
	}
	defer exp.Close()

	tasks, err := exp.ListTasks(namespaces.WithNamespace(context.Background(), "ns1"))
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}

	// Should return 1 task with UNKNOWN status and 0 PID because cgroups path doesn't exist
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Name != "container-1" {
		t.Errorf("expected task name 'container-1', got '%s'", tasks[0].Name)
	}
	if tasks[0].Status != "UNKNOWN" {
		t.Errorf("expected task status 'UNKNOWN', got '%s'", tasks[0].Status)
	}
}

func TestGetContainerByID(t *testing.T) {
	tmpDir := t.TempDir()
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	_ = os.Mkdir(containerdRoot, 0755)

	metaDir := filepath.Join(containerdRoot, "io.containerd.metadata.v1.bolt")
	_ = os.MkdirAll(metaDir, 0755)
	dbPath := filepath.Join(metaDir, "meta.db")
	db, err := bolt.Open(dbPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to open meta.db: %v", err)
	}

	_ = db.Update(func(tx *bolt.Tx) error {
		nsStore := metadata.NewNamespaceStore(tx)
		return nsStore.Create(context.Background(), "ns1", nil)
	})

	dbStore := metadata.NewDB(db, nil, nil)
	cStore := metadata.NewContainerStore(dbStore)

	specObj := oci.Spec{
		Linux: &oci.Linux{
			CgroupsPath: "/default/container-1",
		},
		Process: &oci.Process{
			Args: []string{"sleep", "10"},
		},
	}
	specJSON, _ := json.Marshal(specObj)
	anySpec := &types.Any{
		TypeUrl: "types.containerd.io/opencontainers/runtime-spec/1/Spec",
		Value:   specJSON,
	}

	c := containers.Container{
		ID:          "container-1",
		Image:       "ubuntu:latest",
		Snapshotter: "overlayfs",
		SnapshotKey: "snap1",
		Runtime: containers.RuntimeInfo{
			Name: "io.containerd.runc.v2",
		},
		Spec: anySpec,
	}
	_, err = cStore.Create(namespaces.WithNamespace(context.Background(), "ns1"), c)
	if err != nil {
		db.Close()
		t.Fatalf("failed to create container: %v", err)
	}
	db.Close()

	sc, _ := explorers.NewSupportContainer("")
	exp, err := NewExplorer("some_image_root", containerdRoot, "", "", sc)
	if err != nil {
		t.Fatalf("failed to create explorer: %v", err)
	}
	defer exp.Close()

	// Case 1: success path
	ctr, err := exp.GetContainerByID(namespaces.WithNamespace(context.Background(), "ns1"), "container-1")
	if err != nil {
		t.Fatalf("GetContainerByID failed: %v", err)
	}
	if ctr.ID != "container-1" {
		t.Errorf("expected container ID 'container-1', got '%s'", ctr.ID)
	}

	// Case 2: container not found
	ctr, err = exp.GetContainerByID(namespaces.WithNamespace(context.Background(), "ns1"), "non-existent")
	if err == nil {
		t.Errorf("expected error for non-existent container ID, got nil")
	}
	if ctr != nil {
		t.Errorf("expected container to be nil on error, got %+v", ctr)
	}
}

func TestResolveSnapshotter(t *testing.T) {
	tmpDir := t.TempDir()
	containerdRoot := filepath.Join(tmpDir, "containerd_root")
	_ = os.MkdirAll(filepath.Join(containerdRoot, "io.containerd.metadata.v1.bolt"), 0755)

	dbPath := filepath.Join(containerdRoot, "io.containerd.metadata.v1.bolt", "meta.db")
	db, err := bolt.Open(dbPath, 0644, nil)
	if err != nil {
		t.Fatalf("failed to open meta.db: %v", err)
	}

	now := time.Now()
	// Populate snapshot metadata in database
	_ = db.Update(func(tx *bolt.Tx) error {
		_ = createMetaSnapshot(tx, "ns-resolve", "overlayfs", "snap-resolved-key", "snapshot-actual-name", "snapshot-parent-name", now)
		_ = createMetaSnapshot(tx, "ns-resolve", "native", "native-snap-key", "native-snapshot-actual-name", "", now)
		return nil
	})
	db.Close()

	sc, _ := explorers.NewSupportContainer("")
	exp, err := NewExplorer("some_image_root", containerdRoot, "", "", sc)
	if err != nil {
		t.Fatalf("failed to create explorer: %v", err)
	}

	ctx := namespaces.WithNamespace(context.Background(), "ns-resolve")

	// Case 1: Snapshotter and key are already populated -> returns immediately
	c1 := containers.Container{
		ID:          "container-1",
		Snapshotter: "some-snapshotter",
		SnapshotKey: "some-key",
	}
	err = exp.(*explorer).resolveSnapshotter(ctx, &c1)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if c1.Snapshotter != "some-snapshotter" || c1.SnapshotKey != "some-key" {
		t.Errorf("expected no changes, got Snapshotter=%q, SnapshotKey=%q", c1.Snapshotter, c1.SnapshotKey)
	}

	// Case 2: Snapshotter is empty, SnapshotKey is provided -> finds matching snapshotter
	c2 := containers.Container{
		ID:          "container-2",
		Snapshotter: "",
		SnapshotKey: "snap-resolved-key",
	}
	err = exp.(*explorer).resolveSnapshotter(ctx, &c2)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if c2.Snapshotter != "overlayfs" {
		t.Errorf("expected Snapshotter to be resolved to 'overlayfs', got %q", c2.Snapshotter)
	}
	exp.Close() // Close explorer here so we can write to bolt DB below without blocking

	// Case 3: Snapshotter is empty, SnapshotKey is empty -> walks and matches by container.ID
	db, _ = bolt.Open(dbPath, 0644, nil)
	_ = db.Update(func(tx *bolt.Tx) error {
		return createMetaSnapshot(tx, "ns-resolve", "overlayfs", "container-3-key-pattern", "snap-name", "", now)
	})
	db.Close()

	// Recreate explorer
	exp, err = NewExplorer("some_image_root", containerdRoot, "", "", sc)
	if err != nil {
		t.Fatalf("failed to recreate explorer: %v", err)
	}
	defer exp.Close()

	c3 := containers.Container{
		ID:          "container-3",
		Snapshotter: "",
		SnapshotKey: "",
	}
	err = exp.(*explorer).resolveSnapshotter(ctx, &c3)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if c3.Snapshotter != "overlayfs" || c3.SnapshotKey != "container-3-key-pattern" {
		t.Errorf("expected Snapshotter='overlayfs' and SnapshotKey='container-3-key-pattern', got Snapshotter=%q, SnapshotKey=%q", c3.Snapshotter, c3.SnapshotKey)
	}

	// Case 4: No match -> fallback to overlayfs and container.ID
	c4 := containers.Container{
		ID:          "container-4",
		Snapshotter: "",
		SnapshotKey: "",
	}
	err = exp.(*explorer).resolveSnapshotter(ctx, &c4)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if c4.Snapshotter != "overlayfs" || c4.SnapshotKey != "container-4" {
		t.Errorf("expected fallback to Snapshotter='overlayfs' and SnapshotKey='container-4', got Snapshotter=%q, SnapshotKey=%q", c4.Snapshotter, c4.SnapshotKey)
	}

	// Case 5: Missing namespace -> fallback to default and returns error
	c5 := containers.Container{
		ID:          "container-5",
		Snapshotter: "",
		SnapshotKey: "",
	}
	err = exp.(*explorer).resolveSnapshotter(context.Background(), &c5)
	if err == nil {
		t.Errorf("expected error when namespace is missing from context")
	}
	if c5.Snapshotter != "overlayfs" || c5.SnapshotKey != "container-5" {
		t.Errorf("expected fallback on error, got Snapshotter=%q, SnapshotKey=%q", c5.Snapshotter, c5.SnapshotKey)
	}
}
