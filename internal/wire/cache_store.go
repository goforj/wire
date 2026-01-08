// Copyright 2018 The Wire Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package wire

import (
	"errors"
	"io/fs"
	"path/filepath"
)

// cacheDir returns the base directory for Wire cache files.
func cacheDir() string {
	return filepath.Join(osTempDir(), "wire-cache")
}

// CacheDir returns the directory used for Wire's cache.
func CacheDir() string {
	return cacheDir()
}

// ClearCache removes all cached data.
func ClearCache() error {
	return osRemoveAll(cacheDir())
}

// cachePath builds the on-disk path for a cached content hash.
func cachePath(key string) string {
	return filepath.Join(cacheDir(), key+".bin")
}

// readCache reads a cached content blob by key.
func readCache(key string) ([]byte, bool) {
	data, err := osReadFile(cachePath(key))
	if err != nil {
		return nil, false
	}
	return data, true
}

// writeCache persists a content blob for the provided cache key.
func writeCache(key string, content []byte) {
	dir := cacheDir()
	if err := osMkdirAll(dir, 0755); err != nil {
		return
	}
	path := cachePath(key)
	tmp, err := osCreateTemp(dir, key+".tmp-")
	if err != nil {
		return
	}
	_, writeErr := tmp.Write(content)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		osRemove(tmp.Name())
		return
	}
	if err := osRename(tmp.Name(), path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			osRemove(tmp.Name())
			return
		}
		osRemove(tmp.Name())
	}
}
