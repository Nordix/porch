// Copyright 2022, 2025 The kpt and Nephio Authors
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

package kpt

import (
	"testing"

	kptfilev1 "github.com/nephio-project/porch/v4/pkg/kpt/api/kptfile/v1"
)

func TestNormalizeGitFields(t *testing.T) {
	// Test case 1: Add .git suffix and normalize directory path
	upstream := &kptfilev1.Upstream{
		Git: &kptfilev1.Git{
			Repo:      "https://github.com/example/repo",
			Directory: "/path/to/dir",
		},
	}
	normalizeGitFields(upstream)
	if upstream.Git.Repo != "https://github.com/example/repo.git" {
		t.Errorf("Expected .git suffix, got %q", upstream.Git.Repo)
	}
	if upstream.Git.Directory != "path/to/dir" {
		t.Errorf("Expected normalized path, got %q", upstream.Git.Directory)
	}

	// Test case 2: Already has .git suffix
	upstream = &kptfilev1.Upstream{
		Git: &kptfilev1.Git{
			Repo:      "https://github.com/example/repo.git",
			Directory: "path/to/dir",
		},
	}
	normalizeGitFields(upstream)
	if upstream.Git.Repo != "https://github.com/example/repo.git" {
		t.Errorf("Expected unchanged repo URL, got %q", upstream.Git.Repo)
	}
}

func TestNormalizeGitLockFields(t *testing.T) {
	// Test case 1: Add .git suffix and normalize directory path
	lock := &kptfilev1.UpstreamLock{
		Git: &kptfilev1.GitLock{
			Repo:      "https://github.com/example/repo",
			Directory: "/path/to/dir",
		},
	}
	normalizeGitLockFields(lock)
	if lock.Git.Repo != "https://github.com/example/repo.git" {
		t.Errorf("Expected .git suffix, got %q", lock.Git.Repo)
	}
	if lock.Git.Directory != "path/to/dir" {
		t.Errorf("Expected normalized path, got %q", lock.Git.Directory)
	}

	// Test case 2: Already has .git suffix
	lock = &kptfilev1.UpstreamLock{
		Git: &kptfilev1.GitLock{
			Repo:      "https://github.com/example/repo.git",
			Directory: "path/to/dir",
		},
	}
	normalizeGitLockFields(lock)
	if lock.Git.Repo != "https://github.com/example/repo.git" {
		t.Errorf("Expected unchanged repo URL, got %q", lock.Git.Repo)
	}
}
