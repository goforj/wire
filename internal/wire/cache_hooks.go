// Copyright 2026 The Wire Authors
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
	"encoding/json"
	"os"
)

var (
	osCreateTemp = os.CreateTemp
	osMkdirAll   = os.MkdirAll
	osReadFile   = os.ReadFile
	osRemove     = os.Remove
	osRemoveAll  = os.RemoveAll
	osRename     = os.Rename
	osStat       = os.Stat
	osTempDir    = os.TempDir

	jsonMarshal   = json.Marshal
	jsonUnmarshal = json.Unmarshal

	cacheKeyForPackageFunc      = cacheKeyForPackage
	detectOutputDirFunc         = detectOutputDir
	buildCacheFilesFunc         = buildCacheFiles
	buildCacheFilesFromMetaFunc = buildCacheFilesFromMeta
	rootPackageFilesFunc        = rootPackageFiles
	hashFilesFunc               = hashFiles
)
