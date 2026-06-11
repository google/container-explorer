# Container Explorer (`container-explorer`)

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

**Container Explorer** (built as `ce`) is a standalone Go utility for exploring,
analyzing, and performing forensics on container runtimes (such as **containerd**, **Docker**,
and **Podman**).

Container Explorer operates **completely offline**. It directly parses the low-level metadata databases,
storage directories, and snapshot layers on disk (e.g., in `/var/lib/containerd` or `/var/lib/docker`).
This design makes it a highly powerful utility for digital forensics, incident response, VM disk image analysis,
and low-level troubleshooting.

---

## Key Features

- **Offline Forensics**: Analyze container configurations and filesystems without needing container
  daemons (`dockerd`, `containerd`, or `podman`) to be running.
- **Multi-Runtime Support**: Auto-detects and supports **containerd** (Bbolt DB), **Docker** (JSON
  configurations), and **Podman** (SQLite database state).
- **Disk Image Analysis**: Point the tool to a mounted offline root filesystem (`--image-root`)
  from a VM or disk snapshot to explore containers on that image.
- **Filesystem Mounting**: Mount container filesystems (OverlayFS merged view) locally to inspect,
  search, or copy files.
- **Drift Detection**: Detect filesystem drift (modified, added, deleted, or executable files)
  between the running container and its original base image.
- **Container Exporting**: Export container filesystems as raw disk images (`.raw`) or tar archives
  (`.tar.gz`) for secondary analysis.
- **Kubernetes Awareness**: Filter out or isolate Kubernetes infrastructure/support containers
  (e.g., `pause` containers) using label filters or predefined configuration files.

---

## Architecture and Design

Container Explorer parses metadata databases directly:
- **containerd**: Reads the BoltDB metadata file (`io.containerd.metadata.v1.bolt/meta.db`).
- **Docker**: Reads container configurations (`config.v2.json`), repositories JSON, and image files directly
  from the Docker root directory—no SQLite database is used by the Docker explorer.
- **Podman**: Reads SQLite database state (`db.sql`) and storage configurations directly from Podman's storage
  root (typically `/var/lib/containers/storage`).

It reconstructs the container layer stack to perform file system operations like mounting, drift
detection, and exporting, completely bypassing the container engine.

### Filesystem & Snapshotter Support

- **OverlayFS**: For standard containers (Docker, containerd, Podman) using OverlayFS, the tool reconstructs
  the layered filesystem using the `lowerdir` and `upperdir` directories to establish a merged view.
- **Native Filesystem**: For containerd containers using the `native` snapshotter:
  - **Mounting**: Automatically resolved via its snapshot ID in the metadata database, and mounted
    as a read-only bind mount (`rbind, ro`) directly from the host's native snapshot directory
    (e.g., `/var/lib/containerd/io.containerd.snapshotter.v1.native/snapshots/<snapshot_id>`).
  - **Exporting**: Works out-of-the-box as it leverages the native bind mount mechanism.
  - **Drift Detection**: Since `native` snapshots represent complete directory copies rather than
    layered diff overlays, drift detection is bypassed.

---

## Installation

### Prerequisites

To compile or run Container Explorer, you need:
- **Go** (version 1.25 or later is recommended)
- **Linux operating system** (required for mount, export, and drift detection capabilities)

### Compiling from Source

Clone the repository and build the binary:

```bash
git clone https://github.com/google/container-explorer.git
cd container-explorer
go build -o ce cmd/main.go
```

The output binary is named `ce`.

### Using the Setup Script

A setup script is provided in the `script/` directory to download and install releases:

```bash
sudo ./script/setup.sh install
```

---

## Command Line Interface (CLI) Reference

### Global Flags

```text
GLOBAL FLAGS:
   --debug, -d                       Enable debug messages
   --containerd-root value, -c value Specify containerd root directory
   --docker-root value, -D value     Specify docker root directory
   --image-root value, -i value      Specify mount point for an offline disk image
   --use-layer-cache, -u             Attempt to use cached layers where layers are symlinks
   --layer-cache value, -l value     Cached layer folder within the snapshot root (default: "layers")
   --support-container-data value, -s value
                                     A yaml file containing criteria for Kubernetes support containers
   --output value                    Output format: json, table (default: "table")
   --output-file value, -o value     Output file to save the content
   --help, -h                        Show help
```

> [!IMPORTANT]
> The global flags `--containerd-root` and `--docker-root` do not have built-in CLI defaults.
> Instead, they are dynamically inferred relative to the `--image-root` flag (e.g., as `/var/lib/containerd` and
> `/var/lib/docker`) if it is set. If `--image-root` is not specified, you must supply these paths explicitly;
> otherwise, the tool cannot locate runtime database files and the corresponding explorer will fail to initialize.

---

## Commands

### 1. `list` (or `ls`)
Lists container-related objects and information. Output results can be printed as a table (default) or
exported in JSON format using global output flags.

#### Subcommands:
- `namespaces` (aliases: `namespace`, `ns`): List namespaces (only implemented for containerd).
- `containers` (aliases: `container`): List containers across runtimes.
  - `-f, --filter`: Comma-separated label filter (e.g., `key=value`).
  - `-s, --show-support-containers`: Show Kubernetes supporting/infra containers.
  - `-L, --no-labels`: Hide labels in table view.
  - `--updated`: Show container updated timestamp.
  - `-p, --ports`: Show exposed ports.
  - `-r, --running`: Placeholder flag (UI defined, but not wired in the backend).
- `images` (aliases: `image`, `img`): List container images.
- `contents` (aliases: `content`): List containerd content addressable stores (only implemented for containerd).
- `snapshots` (aliases: `snapshot`, `sn`): List container layers/snapshots (only implemented for containerd).
  - `-P, --full-overlay-path`: Display full OverlayFS directory paths on the host.
- `tasks` (aliases: `task`): List container execution tasks/processes.

*Example:*
```bash
# List all Docker and containerd containers
sudo ./ce --image-root /mnt/disk1 list containers

# List snapshots with full host OverlayFS paths
sudo ./ce --image-root /mnt/disk1 list snapshots -P

# Export the list of all containers to a JSON file
sudo ./ce --image-root /mnt/disk1 --output json --output-file output/container_list.json list containers
```

---

### 2. `info` / `inspect`
Retrieves internal metadata and OCI specifications for a container.

Both commands display the OCI specs for a target container. While `info container` requires a subcommand structure, the standalone `inspect` command acts as a shortcut that yields the same output.

```bash
sudo ./ce --image-root /mnt/disk1 info container <container-id>
# Or using the inspect shortcut:
sudo ./ce --image-root /mnt/disk1 inspect <container-id>
```

**Flags:**
- `-s, --spec`: Print only the container's OCI runtime configuration (`config.json` equivalent).

---

### 3. `mount`
Mounts a container's merged filesystem view using OverlayFS to a local target directory.

```bash
sudo ./ce --image-root /mnt/disk1 mount [flag] [container-id] <mountpoint>
```

**Flags / Arguments:**
- `--all`: Mount all matching containers under the target mount point.
- `-e, --container-engine`: Specify engine (`docker`, `containerd`, `podman`, `all`).
- `-f, --filter`: Filter by container label.
- `-s, --mount-support-containers`: Include Kubernetes support containers.

*Example:*
```bash
sudo mkdir /mnt/container_inspect
sudo ./ce --image-root /mnt/disk1 mount 4b8d7c2a /mnt/container_inspect
# You can now browse the live merged filesystem of the container under /mnt/container_inspect
```

---

### 4. `drift` (or `diff`)
Identifies filesystem drift (additions, modifications, deletions, and newly introduced executables)
in the container compared to its base image layer. Just like `list`, results can be exported in JSON
format using the global `--output json` flag.

```bash
sudo ./ce --image-root /mnt/disk1 drift <container-id>
```

**Flags:**
- `-f, --filter`: Comma-separated label filter.
- `-s, --mount-support-containers`: Analyze Kubernetes support containers.

*Example:*
```bash
# Print container drift to stdout in JSON format
sudo ./ce --image-root /mnt/disk1 --output json drift 4b8d7c2a

# Export container drift to a JSON file
sudo ./ce --image-root /mnt/disk1 --output json --output-file output/drift_report.json drift 4b8d7c2a
```

---

### 5. `export`
Exports container filesystems to a target directory as raw disk images or tar archives.

```bash
sudo ./ce --image-root /mnt/disk1 export [flag] <container-id> <output-directory>
```

**Flags:**
- `-i, --image`: Export container filesystem as raw `.img` file (default).
- `-a, --archive`: Export container filesystem as `.tar` archive.
- `--all`: Export all containers.
- `-e, --container-engine`: Choose container engine (`docker`, `containerd`, `podman`, `all`).
- `-f, --filter`: Label filter.
- `-s, --export-support-containers`: Export Kubernetes support containers.

*Example:*
```bash
sudo ./ce --image-root /mnt/disk1 export -a 4b8d7c2a /tmp/container_exports/
# Generates a tar archive of the container's filesystem in /tmp/container_exports/
```

---

## Limitations & Feature Matrix

Because Container Explorer operates as an offline forensic tool by reading filesystem stores directly, support for specific operations varies across container engines depending on database types and implementation status.

| Feature / Command | containerd | Docker | Podman |
| :--- | :--- | :--- | :--- |
| **`list namespaces`** | ✅ Supported | ❌ Not implemented (returns empty) | ❌ Not implemented (returns empty) |
| **`list containers`** | ✅ Supported | ✅ Supported | ✅ Supported |
| **`list images`** | ✅ Supported | ✅ Supported | ✅ Supported |
| **`list contents`** | ✅ Supported | ❌ Not implemented (stubbed) | ❌ Not implemented (stubbed) |
| **`list snapshots`** | ✅ Supported | ❌ Not implemented (stubbed) | ❌ Not implemented (stubbed) |
| **`list tasks`** | ✅ Supported | ✅ Supported | ✅ Supported |
| **`mount` (OverlayFS)** | ✅ Supported | ✅ Supported | ✅ Supported |
| **`mount` (Native FS)** | ✅ Supported | ➖ N/A | ➖ N/A |
| **`drift` (OverlayFS)** | ✅ Supported | ✅ Supported | ✅ Supported |
| **`drift` (Native FS)** | ➖ Bypassed | ➖ N/A | ➖ N/A |
| **`export`** | ✅ Supported | ✅ Supported | ✅ Supported |

---

## Forensic Investigation Guide

### Investigating a Mounted VM Disk
If you have mounted an external VM disk or disk snapshot at `/mnt/disk1`:

```bash
# Analyze containerd and Docker runtimes under the mounted disk image
sudo ./ce --image-root /mnt/disk1 list containers

# Check for container filesystem drift on the disk image
sudo ./ce --image-root /mnt/disk1 drift <container-id>
```

---

## Contributing

We welcome contributions to this project! Please read [CONTRIBUTING.md](CONTRIBUTING.md) for details on
our code of conduct and the submission process.

## License

This project is licensed under the Apache 2.0 License - see the [LICENSE](LICENSE) file for details.
