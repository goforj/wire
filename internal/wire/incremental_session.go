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
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	"go/token"
	"path/filepath"
	"strings"
	"sync"
)

type incrementalSession struct {
	fset       *token.FileSet
	mu         sync.Mutex
	parsedDeps map[string]cachedParsedFile
}

type cachedParsedFile struct {
	hash string
	file *ast.File
}

var incrementalSessions sync.Map

func clearIncrementalSessions() {
	incrementalSessions.Range(func(key, _ any) bool {
		incrementalSessions.Delete(key)
		return true
	})
}

func sessionKey(wd string, env []string, tags string) string {
	var b strings.Builder
	b.WriteString(filepath.Clean(wd))
	b.WriteByte('\n')
	b.WriteString(tags)
	b.WriteByte('\n')
	for _, entry := range env {
		b.WriteString(entry)
		b.WriteByte('\x00')
	}
	return b.String()
}

func getIncrementalSession(wd string, env []string, tags string) *incrementalSession {
	key := sessionKey(wd, env, tags)
	if session, ok := incrementalSessions.Load(key); ok {
		return session.(*incrementalSession)
	}
	session := &incrementalSession{
		fset:       token.NewFileSet(),
		parsedDeps: make(map[string]cachedParsedFile),
	}
	actual, _ := incrementalSessions.LoadOrStore(key, session)
	return actual.(*incrementalSession)
}

func (s *incrementalSession) getParsedDep(filename string, src []byte) (*ast.File, bool) {
	if s == nil {
		return nil, false
	}
	hash := hashSource(src)
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.parsedDeps[filepath.Clean(filename)]
	if !ok || entry.hash != hash {
		return nil, false
	}
	return entry.file, true
}

func (s *incrementalSession) storeParsedDep(filename string, src []byte, file *ast.File) {
	if s == nil || file == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.parsedDeps[filepath.Clean(filename)] = cachedParsedFile{
		hash: hashSource(src),
		file: file,
	}
}

func hashSource(src []byte) string {
	sum := sha256.Sum256(src)
	return hex.EncodeToString(sum[:])
}
