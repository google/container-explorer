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
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
)

// GetTaskStatus returns task status
func GetTaskStatus(cgrouppath string) (string, error) {
	populated, frozen, err := ReadCgroupEvents(cgrouppath)
	if err != nil {
		return "UNKNOWN", fmt.Errorf("reading group.events: %w", err)
	}

	if populated == 0 && frozen == 0 {
		return "STOPPED", nil
	} else if populated == 1 && frozen == 0 {
		return "RUNNING", nil
	} else if populated == 1 && frozen == 1 {
		return "PAUSED", nil
	}

	return "UNKNOWN", fmt.Errorf("unknown status with values populated: %d, frozen: %d", populated, frozen)
}

// GetTaskPID returns process ID of the containers
func GetTaskPID(path string) int {
	pidfile := filepath.Join(path, "cgroup.procs")
	if !PathExists(pidfile, true) {
		return -1
	}

	data, err := os.ReadFile(pidfile)
	if err != nil {
		log.WithField("path", pidfile).Error("reading cgroup.procs: ", err)
		return -1
	}

	pid, err := strconv.Atoi(strings.Split(string(data), "\n")[0])
	if err != nil {
		log.WithField("path", pidfile).Info("converting to int: ", err)
		return -1
	}
	return pid
}

// ReadCgroupEvents returns populated and frozen status
func ReadCgroupEvents(path string) (int, int, error) {
	data, err := os.ReadFile(filepath.Join(path, "cgroup.events"))
	if err != nil {
		return -1, -1, err
	}

	populated := -1
	frozen := -1

	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "populated ") {
			val := strings.Replace(line, "populated ", "", -1)
			val = strings.TrimSpace(val)

			populated, err = strconv.Atoi(val)
			if err != nil {
				populated = -1
			}
		}

		if strings.Contains(line, "frozen ") {
			val := strings.Replace(line, "frozen ", "", -1)
			val = strings.TrimSpace(val)

			frozen, err = strconv.Atoi(val)
			if err != nil {
				frozen = -1
			}
		}
	}
	return populated, frozen, nil
}

// PathExists returns true if the path exists
func PathExists(path string, isfile bool) bool {
	finfo, err := os.Stat(path)
	if err != nil {
		return false
	}

	if isfile {
		return !finfo.IsDir()
	}
	return finfo.IsDir()
}
