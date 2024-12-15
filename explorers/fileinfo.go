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
	"strings"
	"syscall"
	"time"
)

type FileInfo struct {
	FileName     string    `json:"file_name"`
	FullPath     string    `json:"full_path"`
	FileSize     int64     `json:"file_size"`
	FileModified time.Time `json:"file_modified"`
	FileAccessed time.Time `json:"file_accessed"`
	FileChanged  time.Time `json:"file_changed"`
	FileBirth    time.Time `json:"file_birth"`
	FileUid      string    `json:"file_uid,omitempty"`
	FileOwner    string    `json:"file_owner,omitempty"`
	FileGid      string    `json:"file_gid,omitempty"`
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
