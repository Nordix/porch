// Copyright 2022, 2026 The kpt Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Relevant: https://github.com/golang/go/issues/20126

// FilepathSafeJoin joins dir and relative, returning an error if relative is
// not a clean, canonical relative path within dir. It rejects path traversal
// (.. sequences), absolute paths, the bare "." and ".." entries, and any path
// that would be altered by filepath.Clean (e.g. leading "./", redundant
// separators, or internal "a/../b" segments).
func FilepathSafeJoin(dir, relative string) (string, error) {
	p := filepath.Join(dir, relative)
	p = filepath.Clean(p)

	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return "", fmt.Errorf("invalid relative path %q", relative)
	}
	if rel == "." || rel == ".." || rel != relative || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || strings.HasPrefix(rel, "."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid relative path %q", relative)
	}
	return p, nil
}

// ValidateResourcePaths checks that all keys in a resource map are valid
// relative file paths within a package. It rejects path traversal sequences,
// absolute paths, and non-canonical paths (e.g. leading "./", bare "." or "..").
// Returns an error on the first invalid key.
func ValidateResourcePaths(resources map[string]string) error {
	for k := range resources {
		if _, err := FilepathSafeJoin(".", k); err != nil {
			return fmt.Errorf("invalid resource path %q: %w", k, err)
		}
	}
	return nil
}
