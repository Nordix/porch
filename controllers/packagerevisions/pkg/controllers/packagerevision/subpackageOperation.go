// Copyright 2026 The kpt Authors
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

package packagerevision

import (
	"context"
	"fmt"

	porchv1alpha2 "github.com/kptdev/porch/api/porch/v1alpha2"
)

// applySubpackageOperation executes the independent subpackage operation and returns the resulting resources.
// Returns nil, nil if no source operation to be applied (subpackage operation already executed).
func (r *PackageRevisionReconciler) applySubpackageOperaiton(ctx context.Context, pr *porchv1alpha2.PackageRevision) (map[string]string, string, error) {
	if pr.Spec.SubpackageOperation == nil {
		return nil, "", nil
	}
	if pr.Status.LastSubpackageOperation != nil && pr.Status.LastSubpackageOperation == pr.Spec.SubpackageOperation {
		return nil, "", nil
	}

	switch {
	case pr.Spec.SubpackageOperation.CloneFrom != nil:
		resources, err := r.clonePackage(ctx, pr)
		return resources, "clone", err
	case pr.Spec.SubpackageOperation.Upgrade != nil:
		resources, err := r.upgradePackage(ctx, pr)
		return resources, "upgrade", err
	default:
		return nil, "", fmt.Errorf("source has no fields set")
	}
}
