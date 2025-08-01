// Copyright 2025 The Nephio Authors
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
	"testing"

	api "github.com/nephio-project/porch/api/porch/v1alpha1"
	"github.com/nephio-project/porch/pkg/repository"
	"github.com/stretchr/testify/assert"
)

func createMockedResources() repository.PackageResources {
	return repository.PackageResources{
		Contents: map[string]string{
			"apiVersion": "kpt.dev/v1alpha1",
			"kind":       "Kptfile",
		},
	}
}

func TestApplyErrorInvalidUpstreamUprade(t *testing.T) {
	ctx := context.Background()

	// Mock resources and tasks with valid upstream but no resources to fetch
	resources := createMockedResources()
	updateTask := &api.Task{
		Type: api.TaskTypeUpgrade,
		Upgrade: &api.PackageUpgradeTaskSpec{
			OldUpstream: api.PackageRevisionRef{
				Name: "original",
			},
			NewUpstream: api.PackageRevisionRef{
				Name: "upstream",
			},
			LocalPackageRevisionRef: api.PackageRevisionRef{
				Name: "destination",
			},
		},
	}

	mutation := &upgradePackageMutation{
		upgradeTask: updateTask,
		pkgName:     "test-package",
	}

	_, _, err := mutation.apply(ctx, resources)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error fetching the resources for package")
}
