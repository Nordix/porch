// Copyright 2022, 2024 The kpt and Nephio Authors
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

package task

import (
	"context"
	"errors"
	"testing"

	configapi "github.com/nephio-project/porch/api/porchconfig/v1alpha1"
	"github.com/nephio-project/porch/pkg/repository"
	mockrepo "github.com/nephio-project/porch/test/mockery/mocks/porch/pkg/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

func TestHasSubpackageOptIn(t *testing.T) {
	tests := []struct {
		name      string
		resources repository.PackageResources
		want      bool
	}{
		{
			name: "has subpackage annotation true",
			resources: repository.PackageResources{
				Contents: map[string]string{
					"Kptfile": `apiVersion: kpt.dev/v1
kind: Kptfile
metadata:
  name: test
  annotations:
    kpt.dev/subpackage: "true"
`,
				},
			},
			want: true,
		},
		{
			name: "has subpackage annotation false",
			resources: repository.PackageResources{
				Contents: map[string]string{
					"Kptfile": `apiVersion: kpt.dev/v1
kind: Kptfile
metadata:
  name: test
  annotations:
    kpt.dev/subpackage: "false"
`,
				},
			},
			want: false,
		},
		{
			name: "no annotations",
			resources: repository.PackageResources{
				Contents: map[string]string{
					"Kptfile": `apiVersion: kpt.dev/v1
kind: Kptfile
metadata:
  name: test
`,
				},
			},
			want: false,
		},
		{
			name: "no Kptfile",
			resources: repository.PackageResources{
				Contents: map[string]string{
					"resource.yaml": "kind: ConfigMap",
				},
			},
			want: false,
		},
		{
			name: "empty Kptfile",
			resources: repository.PackageResources{
				Contents: map[string]string{
					"Kptfile": "",
				},
			},
			want: false,
		},
		{
			name: "invalid yaml in Kptfile",
			resources: repository.PackageResources{
				Contents: map[string]string{
					"Kptfile": "not: valid: yaml: structure:",
				},
			},
			want: false,
		},
		{
			name: "other annotations present",
			resources: repository.PackageResources{
				Contents: map[string]string{
					"Kptfile": `apiVersion: kpt.dev/v1
kind: Kptfile
metadata:
  name: test
  annotations:
    other.annotation: "value"
`,
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasSubpackageOptIn(tt.resources)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestWriteCompositeOrSingle(t *testing.T) {

	tests := []struct {
		name      string
		mutation  *renderPackageMutation
		resources repository.PackageResources
		wantPath  string
		wantErr   bool
		checkFS   func(t *testing.T, fs filesys.FileSystem)
	}{
		{
			name:     "single package without subpackage opt-in",
			mutation: &renderPackageMutation{},
			resources: repository.PackageResources{
				Contents: map[string]string{
					"Kptfile":       "apiVersion: kpt.dev/v1\nkind: Kptfile",
					"resource.yaml": "kind: ConfigMap\nmetadata:\n  name: test",
				},
			},
			wantPath: "/",
			checkFS: func(t *testing.T, fs filesys.FileSystem) {
				data, err := fs.ReadFile("/Kptfile")
				require.NoError(t, err)
				assert.Contains(t, string(data), "kind: Kptfile")

				data, err = fs.ReadFile("/resource.yaml")
				require.NoError(t, err)
				assert.Contains(t, string(data), "kind: ConfigMap")
			},
		},
		{
			name: "single package with subpackage opt-in",
			mutation: &renderPackageMutation{
				repoName:          "myrepo",
				repoOpener:        mockrepo.NewMockRepositoryOpener(t),
				referenceResolver: mockrepo.NewMockReferenceResolver(t),
			},
			resources: repository.PackageResources{
				Contents: map[string]string{
					"Kptfile": `apiVersion: kpt.dev/v1
kind: Kptfile
metadata:
  name: test
  annotations:
    kpt.dev/subpackage: "true"
`,
					"resource.yaml": "kind: ConfigMap\nmetadata:\n  name: test",
				},
			},
			wantPath: "/",
			checkFS: func(t *testing.T, fs filesys.FileSystem) {
				data, err := fs.ReadFile("/Kptfile")
				require.NoError(t, err)
				assert.Contains(t, string(data), "kind: Kptfile")

				data, err = fs.ReadFile("/resource.yaml")
				require.NoError(t, err)
				assert.Contains(t, string(data), "kind: ConfigMap")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			fs := filesys.MakeFsInMemory()

			if tt.mutation.referenceResolver != nil && tt.mutation.repoOpener != nil {
				mockResolver := mockrepo.NewMockReferenceResolver(t)
				mockOpener := mockrepo.NewMockRepositoryOpener(t)

				repoSpec := &configapi.Repository{}
				mockResolver.On("ResolveReference", ctx, "", "myrepo", repoSpec).Return(errors.New("test error")).Once()

				mockRepo := mockrepo.NewMockRepository(t)
				mockOpener.On("OpenRepository", ctx, repoSpec).Return(mockRepo, nil).Maybe()

				tt.mutation.referenceResolver = mockResolver
				tt.mutation.repoOpener = mockOpener
			}

			gotPath, err := tt.mutation.writeCompositeOrSingle(ctx, fs, tt.resources)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantPath, gotPath)

			if tt.checkFS != nil {
				tt.checkFS(t, fs)
			}
		})
	}
}

func TestWriteResources(t *testing.T) {
	tests := []struct {
		name      string
		resources repository.PackageResources
		wantPath  string
		wantErr   bool
	}{
		{
			name: "writes resources and finds Kptfile at root",
			resources: repository.PackageResources{
				Contents: map[string]string{
					"Kptfile":       "kptfile content",
					"resource.yaml": "resource content",
				},
			},
			wantPath: "/",
		},
		{
			name: "finds Kptfile in subdirectory",
			resources: repository.PackageResources{
				Contents: map[string]string{
					"subdir/Kptfile":       "kptfile content",
					"subdir/resource.yaml": "resource content",
				},
			},
			wantPath: "subdir",
		},
		{
			name: "no Kptfile returns empty path",
			resources: repository.PackageResources{
				Contents: map[string]string{
					"resource.yaml": "resource content",
				},
			},
			wantPath: "",
		},
		{
			name: "handles deeply nested structures",
			resources: repository.PackageResources{
				Contents: map[string]string{
					"a/b/c/d/Kptfile":   "deep kptfile",
					"a/b/c/d/file.yaml": "deep file",
				},
			},
			wantPath: "a/b/c/d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := filesys.MakeFsInMemory()

			gotPath, err := writeResources(fs, tt.resources)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantPath, gotPath)

			// Verify all files were written
			for k, v := range tt.resources.Contents {
				data, err := fs.ReadFile("/" + k)
				require.NoError(t, err)
				assert.Equal(t, v, string(data))
			}
		})
	}
}

func TestReadResources(t *testing.T) {
	tests := []struct {
		name    string
		setupFS func(fs filesys.FileSystem)
		want    repository.PackageResources
		wantErr bool
	}{
		{
			name: "reads all regular files",
			setupFS: func(fs filesys.FileSystem) {
				fs.WriteFile("/Kptfile", []byte("kptfile"))
				fs.WriteFile("/resource.yaml", []byte("resource"))
				fs.MkdirAll("/subdir")
				fs.WriteFile("/subdir/nested.yaml", []byte("nested"))
			},
			want: repository.PackageResources{
				Contents: map[string]string{
					"Kptfile":            "kptfile",
					"resource.yaml":      "resource",
					"subdir/nested.yaml": "nested",
				},
			},
		},
		{
			name: "skips directories",
			setupFS: func(fs filesys.FileSystem) {
				fs.MkdirAll("/emptydir")
				fs.MkdirAll("/subdir")
				fs.WriteFile("/file.yaml", []byte("content"))
			},
			want: repository.PackageResources{
				Contents: map[string]string{
					"file.yaml": "content",
				},
			},
		},
		{
			name: "handles empty filesystem",
			setupFS: func(fs filesys.FileSystem) {
				// No files
			},
			want: repository.PackageResources{
				Contents: map[string]string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := filesys.MakeFsInMemory()
			tt.setupFS(fs)

			got, err := readResources(fs)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want.Contents, got.Contents)
		})
	}
}

func TestReadFilteredResources(t *testing.T) {
	tests := []struct {
		name         string
		mutation     *renderPackageMutation
		setupFS      func(fs filesys.FileSystem)
		wantContents map[string]string
		wantErr      bool
		path         string
	}{
		{
			name: "no current package returns all",
			mutation: &renderPackageMutation{
				current: nil,
			},
			setupFS: func(fs filesys.FileSystem) {
				fs.WriteFile("/Kptfile", []byte("root"))
				fs.WriteFile("/resource.yaml", []byte("resource"))
				fs.MkdirAll("/subdir")
				fs.WriteFile("/subdir/nested.yaml", []byte("nested"))
			},
			wantContents: map[string]string{
				"Kptfile":            "root",
				"resource.yaml":      "resource",
				"subdir/nested.yaml": "nested",
			},
			path: "",
		},
		{
			name: "filter to current package subtree",
			mutation: &renderPackageMutation{
				current: &mockrepo.MockPackageRevision{},
			},
			setupFS: func(fs filesys.FileSystem) {
				fs.WriteFile("/root.yaml", []byte("root"))
				fs.MkdirAll("/myapp")
				fs.WriteFile("/myapp/Kptfile", []byte("myapp kptfile"))
				fs.WriteFile("/myapp/resource.yaml", []byte("myapp resource"))
				fs.MkdirAll("/myapp/subdir")
				fs.WriteFile("/myapp/subdir/nested.yaml", []byte("nested"))
				fs.MkdirAll("/other")
				fs.WriteFile("/other/file.yaml", []byte("other"))
			},
			wantContents: map[string]string{
				"Kptfile":            "myapp kptfile",
				"resource.yaml":      "myapp resource",
				"subdir/nested.yaml": "nested",
			},
			path: "myapp",
		},
		{
			name: "no matching files returns all",
			mutation: &renderPackageMutation{
				current: &mockrepo.MockPackageRevision{},
			},
			setupFS: func(fs filesys.FileSystem) {
				fs.WriteFile("/Kptfile", []byte("root"))
				fs.WriteFile("/resource.yaml", []byte("resource"))
			},
			wantContents: map[string]string{
				"Kptfile":       "root",
				"resource.yaml": "resource",
			},
			path: "missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mockRev *mockrepo.MockPackageRevision
			if tt.mutation.current != nil {
				mockRev = mockrepo.NewMockPackageRevision(t)

				revKey := repository.PackageRevisionKey{
					PkgKey: repository.PackageKey{
						RepoKey: repository.RepositoryKey{
							Name: "myrepo",
						},
						Path: tt.path,
					},
					Revision: 1,
				}

				mockRev.On("Key").Return(revKey).Maybe()

				tt.mutation.current = mockRev
			}

			fs := filesys.MakeFsInMemory()
			tt.setupFS(fs)

			got, err := tt.mutation.readFilteredResources(fs)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantContents, got.Contents)
		})
	}
}
