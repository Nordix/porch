/*
 Copyright 2025 The Nephio Authors.

 Licensed under the Apache License, Version 2.0 (the "License");
 You may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package crcache

import (
	"context"
	"testing"

	"github.com/nephio-project/porch/api/porch/v1alpha1"
	configapi "github.com/nephio-project/porch/api/porchconfig/v1alpha1"
	fakemetastore "github.com/nephio-project/porch/pkg/cache/crcache/meta/fake"
	cachetypes "github.com/nephio-project/porch/pkg/cache/types"
	fakeextrepo "github.com/nephio-project/porch/pkg/externalrepo/fake"
	"github.com/nephio-project/porch/pkg/repository"
	"github.com/stretchr/testify/assert"
)

func TestCachedRepository(t *testing.T) {
	repoSpec := configapi.Repository{}
	fakeExternalRepo := fakeextrepo.Repository{}
	fakeMetaStore := fakemetastore.MemoryMetadataStore{}
	options := cachetypes.CacheOptions{}

	cr := newRepository("my-cached-repo", &repoSpec, &fakeExternalRepo, &fakeMetaStore, options)
	assert.Equal(t, cr.id, "my-cached-repo")

	err := cr.Refresh(context.TODO())
	assert.True(t, err == nil)

	err = cr.getRefreshError()
	assert.True(t, err == nil)

	pkgs1, err := cr.getPackages(context.TODO(), repository.ListPackageFilter{}, false)
	assert.True(t, err == nil)
	assert.Equal(t, 0, len(pkgs1))

	pkgs2, prs, err := cr.getCachedPackages(context.TODO(), false)
	assert.True(t, err == nil)
	assert.Equal(t, 0, len(pkgs2))
	assert.Equal(t, 0, len(prs))

	prd, err := cr.CreatePackageRevisionDraft(context.TODO(), &v1alpha1.PackageRevision{})
	assert.True(t, err == nil)
	assert.True(t, prd == nil)
}
