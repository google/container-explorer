"""container-explorer testing utility"""

import json
import os
import re
import shlex
import shutil
import subprocess
import time

DISK1_MOUNTPOINT = "/mnt/d1"
DISK2_MOUNTPOINT = "/mnt/d2"
CONTAINER_MOUNTPOINT = "/tmp/mnt"
CONTAINER_EXPORT_DIR = "/tmp/export"


class ContainerExplorerTester:
    def __init__(self, mountpoint: str, disk: str):
        self.mountpoint = mountpoint
        self.disk = disk

    def print_header(self, msg: str) -> None:
        print(f"\n\033[96m[*] {msg}\033[0m")

    def print_info(self, msg: str) -> None:
        print(f"  [\033[94m*\033[0m] {msg}")

    def print_success(self, msg: str) -> None:
        print(f"  [\033[92m+\033[0m] {msg}")

    def print_warn(self, msg: str) -> None:
        print(f"  [\033[93m!\033[0m] {msg}")

    def print_error(self, msg: str) -> None:
        print(f"  [\033[91m-\033[0m] {msg}")

    def run_command(self, options: list[str]) -> subprocess.CompletedProcess:
        cmd = ["sudo", "go", "run", "cmd/main.go", "-i", self.mountpoint] + options
        return subprocess.run(cmd, capture_output=True)

    def load_json_output(self, file_path: str) -> list[dict]:
        if not os.path.exists(file_path):
            self.print_warn(f"No output file found: {file_path}")
            return []

        with open(file_path, "r", encoding="utf-8") as f:
            try:
                output = json.load(f)
            except json.JSONDecodeError:
                self.print_warn(f"Invalid JSON in output file: {file_path}")
                return []

        if not isinstance(output, list):
            return [output]
        return output

    def get_output_file(self, name: str) -> str:
        return os.path.join("test", "output", f"{self.disk}_{name}.json")

    def test_list_containers(self) -> None:
        self.print_header(f"Checking list containers command on {self.mountpoint}")
        result = self.run_command(["list", "containers"])
        if result.returncode != 0:
            self.print_error("Listing containers failed.")
            return

        containers = self.load_json_output(self.get_output_file("list_containers"))
        if not containers:
            self.print_warn("No output found.")
            return

        for container in containers:
            container_id = container.get("ID", "")
            if not container_id:
                self.print_warn("Missing container ID in output")
                continue

            if re.search(f"{container_id}", str(result.stdout)):
                self.print_success(f"Container {container_id} found.")
            else:
                self.print_error(f"Container {container_id} not found.")

    def test_list_images(self) -> None:
        self.print_header(f"Checking list images command on {self.mountpoint}")
        result = self.run_command(["list", "images"])
        if result.returncode != 0:
            self.print_error("Listing images failed.")
            return

        images = self.load_json_output(self.get_output_file("list_images"))
        if not images:
            self.print_warn("No output found.")
            return

        for image in images:
            name = image.get("Name", "")
            digest = image.get("Target", {}).get("digest", "")
            if not name or not digest:
                self.print_warn("Missing image name or digest in output")
                continue

            if re.search(f"{digest}", str(result.stdout)):
                self.print_success(f"Image {name} digest {digest} found.")
            else:
                self.print_error(f"Image {name} digest {digest} not found.")

    def test_list_contents(self) -> None:
        self.print_header(f"Checking list contents command on {self.mountpoint}")
        result = self.run_command(["list", "contents"])
        if result.returncode != 0:
            self.print_error("Listing contents failed.")
            return

        contents = self.load_json_output(self.get_output_file("list_contents"))
        if not contents:
            self.print_warn("No output found.")
            return

        for content in contents:
            digest = content.get("Digest", "")
            if not digest:
                self.print_warn("Missing content digest in output")
                continue

            if digest in str(result.stdout):
                self.print_success(f"Content {digest} found.")
            else:
                self.print_error(f"Content {digest} not found.")

    def test_list_snapshots(self) -> None:
        self.print_header(f"Checking list snapshots command on {self.mountpoint}")
        result = self.run_command(["list", "snapshots"])
        if result.returncode != 0:
            self.print_error("Listing snapshots failed.")
            return

        output = result.stdout.decode("utf-8")
        snapshots = self.load_json_output(self.get_output_file("list_snapshots"))
        if not snapshots:
            self.print_warn("No output found.")
            return

        for snapshot in snapshots:
            container_type = snapshot.get("ContainerType", "")
            namespace = snapshot.get("Namespace", "")
            snapshotter = snapshot.get("Snapshotter", "")
            key = snapshot.get("Key", "")

            if not container_type or not snapshotter or not key:
                self.print_warn(
                    "Missing snapshot container type, snapshotter or key in output"
                )
                continue

            snapshot_entry_re = re.compile(
                f"{container_type}\\s+{namespace}\\s+{snapshotter}\\s+.*{key}"
            )
            if snapshot_entry_re.search(output):
                self.print_success(f"Snapshot {key} found.")
            else:
                self.print_error(f"Snapshot {key} not found.")

    def test_list_tasks(self) -> None:
        self.print_header(f"Checking list tasks command on {self.mountpoint}")
        result = self.run_command(["list", "tasks"])
        if result.returncode != 0:
            self.print_error("Listing tasks failed.")
            return

        output = result.stdout.decode("utf-8")
        tasks = self.load_json_output(self.get_output_file("list_tasks"))
        if not tasks:
            self.print_warn("No output found.")
            return

        for task in tasks:
            container_type = task.get("ContainerType", "")
            namespace = task.get("Namespace", "")
            container_id = task.get("Name", "")

            if not container_type or not container_id:
                self.print_warn("Missing container type or container ID in output")
                return

            task_entry_re = re.compile(
                f"{container_type}\\s+{namespace}\\s+{container_id}"
            )
            if task_entry_re.search(output):
                self.print_success(f"Task {container_id} found.")
            else:
                self.print_error(f"Task {container_id} not found.")

    def test_info_container(self, container_id: str, output_file: str) -> None:
        self.print_info(f"Checking container information for {container_id}")
        result = self.run_command(["info", "container", container_id])
        if result.returncode != 0:
            self.print_error("Error running info container command")
            return
        if not result.stdout:
            self.print_error("No output for the command")
            return

        try:
            output_json = json.loads(result.stdout)
        except json.JSONDecodeError:
            self.print_error("Error unmarshalling info container output")
            return

        containers_info = self.load_json_output(output_file)
        try:
            container_info = containers_info[0]
        except IndexError:
            self.print_error("No expected container information found in file")
            return

        if not output_json and not container_info:
            self.print_success(
                f"Container information {container_id} is empty as expected"
            )
            return

        if output_json.get("ID") == container_info.get("ID"):
            self.print_success(f"Container information match found for {container_id}")
        else:
            self.print_error(f"Container information mismatch for {container_id}")

    def test_info_containers(self) -> None:
        self.print_header(f"Checking info container command on {self.mountpoint}")
        containers = self.load_json_output(self.get_output_file("list_containers"))

        if not containers:
            result = self.run_command(["list", "containers"])
            if result.returncode != 0:
                self.print_error("Error listing containers")
                return
            try:
                containers = json.loads(result.stdout)
            except json.JSONDecodeError:
                self.print_error("Error decoding JSON data")
                return

        if not containers:
            self.print_warn("No containers found to check info")
            return

        for container in containers:
            container_id = container.get("ID", "")
            if not container_id:
                self.print_warn("Missing container ID in output")
                continue
            container_info_file = self.get_output_file(f"info_container_{container_id}")
            self.test_info_container(container_id, container_info_file)

    def test_drift(self) -> None:
        self.print_header(f"Checking drift command on {self.mountpoint}")
        result = self.run_command(["drift"])
        if result.returncode != 0:
            self.print_error("Error running drift command")
            return
        if not result.stdout:
            self.print_error("Empty output for the command")
            return

        result_output = result.stdout.decode("utf-8")
        expected_output = self.load_json_output(self.get_output_file("drift"))
        for drift in expected_output:
            container_type = drift.get("ContainerType", "")
            container_id = drift.get("ContainerID", "")
            changed_files = drift.get("AddedOrModified", [])

            if not container_type or not container_id or not changed_files:
                self.print_warn("Drift record is empty or missing vital fields")
                continue

            changed_files_found = True
            for changed_file in changed_files:
                full_path = changed_file.get("full_path", "")
                changed_file_re = re.compile(
                    f"{container_type}\\s+{container_id}\\s+.*{re.escape(full_path)}"
                )
                if not changed_file_re.search(result_output):
                    changed_files_found = False

            if changed_files_found:
                self.print_success(f"Matched drift for {container_id}")
            else:
                self.print_error(f"No matching drift for {container_id}")

    def _umount(self, target_dir: str, cleanup_children: bool = False) -> None:
        """Unmounts target_dir or its subdirectories."""
        if not os.path.exists(target_dir):
            return

        if not cleanup_children:
            # Standard single unmount
            result = subprocess.run(["sudo", "umount", target_dir], capture_output=True)
            if result.returncode != 0:
                self.print_warn(
                    f"Error unmounting {target_dir}: {result.stderr.decode('utf-8')}"
                )
            return

        # Cleanup subdirectories (e.g. for mount-all tests)
        for item in os.listdir(target_dir):
            item_path = os.path.join(target_dir, item)
            if not os.path.isdir(item_path):
                continue

            result = subprocess.run(["sudo", "umount", item_path], capture_output=True)
            if result.returncode != 0:
                stderr = result.stderr.decode("utf-8").strip()
                self.print_warn(f"Error unmounting sub-path {item_path}: {stderr}")
            subprocess.run(["sudo", "rm", "-rf", item_path])

    def test_mount_command(self, container_mountpoint: str) -> None:
        self.print_header(f"Checking mount command on {self.mountpoint}")
        containers = self.load_json_output(self.get_output_file("list_containers"))
        if not containers:
            self.print_error("Failed reading container list")
            return

        for container in containers:
            container_id = container.get("ID", "")
            if not container_id:
                self.print_warn("Missing container ID in container record")
                continue

            result = self.run_command(["mount", container_id, container_mountpoint])
            if result.returncode != 0:
                self.print_error(f"Container {container_id} mount failed")
                continue

            container_dirs = os.listdir(container_mountpoint)
            if not container_dirs:
                self.print_error(f"Container {container_id} mount point is empty")
            else:
                self.print_success(f"Container {container_id} mounted successfully")

            self._umount(container_mountpoint)
            time.sleep(2)

    def test_mount_all_command(self, container_mountpoint: str) -> None:
        self.print_header(f"Checking mount-all command on {self.mountpoint}")
        result = self.run_command(["mount-all", container_mountpoint])
        if result.returncode != 0:
            self.print_error(f"Error running mount-all command on {self.mountpoint}")
            return

        container_dirs = os.listdir(container_mountpoint)
        expected_containers = self.load_json_output(
            self.get_output_file("list_containers")
        )
        if not expected_containers:
            self.print_error("Failed reading container list")
            return

        successful_mount_count = 0
        for expected_container in expected_containers:
            container_id = expected_container.get("ID", "")
            if not container_id:
                self.print_warn("Missing container ID in container record")
                continue

            if container_id not in container_dirs:
                self.print_error(f"Container {container_id} not mounted")
                continue
            try:
                container_files = os.listdir(
                    os.path.join(container_mountpoint, container_id)
                )
                if not container_files:
                    self.print_error(f"Container {container_id} is empty")
                    continue
                self.print_success(f"Container {container_id} mounted successfully")
                successful_mount_count += 1
            except FileNotFoundError:
                self.print_error(f"Container {container_id} not mounted")

        if successful_mount_count == len(expected_containers):
            self.print_success(f"Mount all on {self.mountpoint} successful")
        else:
            self.print_error(f"Mount all on {self.mountpoint} failed")

        self.print_info(f"Unmounting all subdirectories in {container_mountpoint}")
        self._umount(container_mountpoint, cleanup_children=True)
        time.sleep(2)

    def test_export_command(self, export_path: str) -> None:
        self.print_header(f"Checking export command on {self.mountpoint}")
        containers = self.load_json_output(self.get_output_file("list_containers"))
        if not containers:
            self.print_error("Failed reading container list")
            return

        for container in containers:
            container_id = container.get("ID", "")
            if not container_id:
                self.print_warn("Missing container ID in container record")
                continue

            result = self.run_command(
                ["export", "--image", "--archive", container_id, export_path]
            )
            if result.returncode != 0:
                self.print_error(f"Container {container_id} export failed")
                continue

            export_image_file = os.path.join(export_path, f"{container_id}.raw")
            export_archive_file = os.path.join(export_path, f"{container_id}.tar.gz")

            message = f"Container {container_id} export"
            if os.path.exists(export_image_file):
                self.print_success(f"{message} image successful")
            else:
                self.print_error(f"{message} image failed")

            if os.path.exists(export_archive_file):
                self.print_success(f"{message} archive successful")
            else:
                self.print_error(f"{message} archive failed")

            for dirname in os.listdir(export_path):
                subprocess.run(
                    ["sudo", "rm", "-rf", os.path.join(export_path, dirname)]
                )

    def test_export_all_command(self, export_path: str) -> None:
        self.print_header(f"Checking export-all command on {self.mountpoint}")

        if not os.path.exists(export_path):
            os.makedirs(export_path)

        result = self.run_command(["export-all", "--image", "--archive", export_path])
        if result.returncode != 0:
            self.print_error(f"Exporting containers in {self.mountpoint} failed")

        exported_containers = os.listdir(export_path)
        expected_containers = self.load_json_output(
            self.get_output_file("list_containers")
        )
        if not expected_containers:
            self.print_error("Failed reading container list")
            return

        for container in expected_containers:
            container_id = container.get("ID", "")
            if not container_id:
                self.print_warn("Container ID missing in container record")
                continue

            if (
                f"{container_id}.raw" in exported_containers
                and f"{container_id}.tar.gz" in exported_containers
            ):
                self.print_success(
                    f"Container {container_id} export completed successfully"
                )
            else:
                self.print_error(f"Container {container_id} export failed")

        for filename in os.listdir(export_path):
            subprocess.run(["sudo", "rm", "-f", os.path.join(export_path, filename)])


def main() -> None:
    """Main entry point"""
    test_suite = [
        ContainerExplorerTester(DISK1_MOUNTPOINT, "d1"),
        ContainerExplorerTester(DISK2_MOUNTPOINT, "d2"),
    ]

    if not os.path.exists(CONTAINER_MOUNTPOINT):
        os.makedirs(CONTAINER_MOUNTPOINT)
    if not os.path.exists(CONTAINER_EXPORT_DIR):
        os.makedirs(CONTAINER_EXPORT_DIR)

    for tester in test_suite:
        tester.test_list_containers()
    for tester in test_suite:
        tester.test_list_images()
    for tester in test_suite:
        tester.test_list_contents()
    for tester in test_suite:
        tester.test_list_snapshots()
    for tester in test_suite:
        tester.test_list_tasks()
    for tester in test_suite:
        tester.test_info_containers()
    for tester in test_suite:
        tester.test_drift()
    for tester in test_suite:
        tester.test_mount_command(CONTAINER_MOUNTPOINT)
    for tester in test_suite:
        tester.test_mount_all_command(CONTAINER_MOUNTPOINT)
    for tester in test_suite:
        tester.test_export_command(CONTAINER_EXPORT_DIR)
    for tester in test_suite:
        tester.test_export_all_command(CONTAINER_EXPORT_DIR)


if __name__ == "__main__":
    main()
