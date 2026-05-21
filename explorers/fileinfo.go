/*
Copyright 2024 Google LLC

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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
)

type FileInfo struct {
	FileName     string    `json:"file_name"`
	FullPath     string    `json:"full_path"`
	FileSize     int64     `json:"file_size"`
	FileModified time.Time `json:"file_modified"`
	FileAccessed time.Time `json:"file_accessed"`
	FileChanged  time.Time `json:"file_changed"`
	FileBirth    time.Time `json:"file_birth"`
	FileUID      string    `json:"file_uid,omitempty"`
	FileOwner    string    `json:"file_owner,omitempty"`
	FileGID      string    `json:"file_gid,omitempty"`
	FileType     string    `json:"file_type,omitempty"`
	FileSHA256   string    `json:"file_sha256,omitempty"`
}

func (f *FileInfo) AsJSON() ([]byte, error) {
	jsonData, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}

	return jsonData, nil
}

// FileSHA256Sum calculates SHA256 hash of the specified
// file.
func FileSHA256Sum(path string) (string, error) {
	var err error

	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return "", err
	}

	hashBytes := hash.Sum(nil)
	hashString := hex.EncodeToString(hashBytes)

	return hashString, nil
}

// GetFileInfo returns file information in drift detection.
func GetFileInfo(info os.FileInfo, path string, diffDir string) (*FileInfo, error) {
	diffFileInfo := FileInfo{
		FileName:     info.Name(),
		FullPath:     strings.Replace(path, diffDir, "", 1),
		FileSize:     info.Size(),
		FileModified: info.ModTime().UTC(),
	}

	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		diffFileInfo.FileAccessed = time.Unix(stat.Atim.Sec, stat.Atim.Nsec).UTC()
		diffFileInfo.FileChanged = time.Unix(stat.Mtim.Sec, stat.Ctim.Nsec).UTC()
		diffFileInfo.FileBirth = time.Unix(stat.Ctim.Sec, stat.Ctim.Nsec).UTC()
	}

	if hash, err := FileSHA256Sum(path); err == nil {
		diffFileInfo.FileSHA256 = hash
	}

	return &diffFileInfo, nil
}

// ScanDiffDirectory identifies added or modified files in the diff directory
func ScanDiffDirectory(diffDir string) (addedOrModified []FileInfo, inaccessibleFiles []FileInfo, err error) {
	log.WithField("path", diffDir).Debug("scanning drift directory")

	// Map to track canonical directory paths and prevent infinite loops/cycles from symlinks
	visited := make(map[string]bool)

	var walk func(string) error
	walk = func(path string) error {
		// 1. Get the Lstat info first to check if the entry itself is a symlink
		info, lstatErr := os.Lstat(path)
		if lstatErr != nil {
			// The file/link is completely unreadable
			log.WithFields(log.Fields{
				"upperDir": diffDir,
				"error":    lstatErr,
			}).Debug("reading drift directory")
			return nil
		}

		// 2. If it is a symlink, resolve it to point to the actual target file/directory
		if info.Mode()&os.ModeSymlink != 0 {
			targetInfo, err := os.Stat(path)
			if err != nil {
				// Broken symlink or target permission error; record as an inaccessible file
				if fileinfo, err := GetFileInfo(info, path, diffDir); err == nil {
					inaccessibleFiles = append(inaccessibleFiles, *fileinfo)
				}
				return nil
			}
			// Overwrite info with the target's FileInfo so metadata matches the actual file
			info = targetInfo
		}

		// 3. Handle Directories (and symlinks pointing to directories)
		if info.IsDir() {
			// Canonicalize the directory path to detect loops and avoid duplicate traversals
			realPath, err := filepath.EvalSymlinks(path)
			if err != nil {
				log.WithFields(log.Fields{"path": path, "error": err}).Debug("getting real path")
				return nil
			}
			if visited[realPath] {
				return nil // Already processed or ancestral loop detected; safely return
			}
			visited[realPath] = true

			// Read directory contents
			entries, err := os.ReadDir(path)
			if err != nil {
				// Directory exists but contents cannot be read
				if fileinfo, err := GetFileInfo(info, path, diffDir); err == nil {
					inaccessibleFiles = append(inaccessibleFiles, *fileinfo)
				}
				return nil
			}

			for _, entry := range entries {
				nextPath := filepath.Join(path, entry.Name())
				if err := walk(nextPath); err != nil {
					log.WithFields(log.Fields{"path": nextPath, "error": err}).Debug("walking drift directory")
					return err
				}
			}
		} else {
			// 4. Handle regular files, device files, or symlinks resolved to files
			fileinfo, err := GetFileInfo(info, path, diffDir)
			if err != nil {
				log.WithFields(log.Fields{"path": path, "error": err}).Debug("getting file information")
				return err
			}

			// Ensure the reported FileName matches the logical path name in the tree,
			// rather than the base name of the resolved target file.
			fileinfo.FileName = filepath.Base(path)

			// Check if the file is a whiteout file
			if info.Mode()&os.ModeCharDevice != 0 {
				if stat, ok := info.Sys().(*syscall.Stat_t); ok {
					rdev := stat.Rdev

					// Extract major and minor device numbers
					major := (rdev >> 8) & 0xfff
					minor := (rdev & 0xff) | ((rdev >> 12) & 0xfff00)

					if major == 0 && minor == 0 {
						inaccessibleFiles = append(inaccessibleFiles, *fileinfo)
						return nil
					}
				}
			}

			// Check if the target file has executable permissions
			mode := info.Mode().Perm()
			if mode&0111 != 0 {
				fileinfo.FileType = "executable"
			}

			addedOrModified = append(addedOrModified, *fileinfo)
		}
		return nil
	}

	err = walk(diffDir)
	return
}
