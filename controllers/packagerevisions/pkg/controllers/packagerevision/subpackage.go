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
	"strings"

	porchv1alpha2 "github.com/kptdev/porch/api/porch/v1alpha2"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// applySubpackageOperation executes the independent subpackage operation and returns the resulting resources.
// Returns nil, nil if no source operation to be applied (subpackage operation already executed).
func (r *PackageRevisionReconciler) applySubpackageOperaiton(ctx context.Context, pr *porchv1alpha2.PackageRevision) (map[string]string, string, error) {
	if r.shouldSkipSubpackageOperation(pr) {
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
		return nil, "", fmt.Errorf("subpackageOperation has no fields set")
	}
}

// upsertSubpackageResourcesInDraft updates the resoruces of the package revision draft with a clone or an upgrade of an independent subpackage.
func (r *PackageRevisionReconciler) upsertSubpackageResourcesInDraftResources(ctx context.Context, pr *porchv1alpha2.PackageRevision, parentResources, subpackageResources map[string]string) (map[string]string, error) {
	if r.shouldSkipSubpackageOperation(pr) {
		return parentResources, nil
	}

	switch {
	case pr.Spec.SubpackageOperation.CloneFrom != nil:
		return r.insertSubpackageResourcesInDraftResources(ctx, pr, parentResources, subpackageResources)
	case pr.Spec.SubpackageOperation.Upgrade != nil:
		return r.upgradeSubpackageResourcesInDraftResources(ctx, pr, parentResources, subpackageResources)
	default:
		return nil, fmt.Errorf("source has no fields set")
	}
}

// insertSubpackageResourcesInDraftResources adds the resources of the independent subpackage to the parent package revision
// at `SubpackageDir`
func (r *PackageRevisionReconciler) insertSubpackageResourcesInDraftResources(ctx context.Context, pr *porchv1alpha2.PackageRevision, parentResources, subpackageResources map[string]string) (map[string]string, error) {
	subpackageDir := pr.Spec.SubpackageOperation.SubpackageDir

	log := log.FromContext(ctx)
	log.V(1).Info("cloning subpackage resources into parent at %q", subpackageDir)

	for resourceKey := range parentResources {
		if strings.HasPrefix(resourceKey, subpackageDir) {
			return nil, fmt.Errorf("cannot clone subpackage into parent, parent already has content at %q", subpackageDir)
		}
	}

	for subpackaageResourceKey, subpackageResourceValue := range subpackageResources {
		parentResources[subpackageDir+"/"+subpackaageResourceKey] = subpackageResourceValue
	}

	log.V(1).Info("cloned subpackage resources into parent at %q", subpackageDir)
	return parentResources, nil
}

// upgradeSubpackageResourcesInDraftResources updates the resources of the independent subpackage in the parent package revision
// at `SubpackageDir`
func (r *PackageRevisionReconciler) upgradeSubpackageResourcesInDraftResources(ctx context.Context, pr *porchv1alpha2.PackageRevision, parentResources, subpackageResources map[string]string) (map[string]string, error) {
	subpackageDir := pr.Spec.SubpackageOperation.SubpackageDir

	log := log.FromContext(ctx)
	log.V(1).Info("upgrading subpackage resources in parent at %q", subpackageDir)

	subpackageFound := false
	for resourceKey := range parentResources {
		if strings.HasPrefix(resourceKey, subpackageDir) {
			subpackageFound = true
			delete(parentResources, resourceKey)
		}
	}

	if !subpackageFound {
		return nil, fmt.Errorf("cannot subpackage subpackage in parent, parent does not have a subpackage at %q", subpackageDir)
	}

	for subpackaageResourceKey, subpackageResourceValue := range subpackageResources {
		parentResources[subpackageDir+"/"+subpackaageResourceKey] = subpackageResourceValue
	}

	log.V(1).Info("upgraded subpackage resources in parent at %q", subpackageDir)
	return parentResources, nil
}

// shouldSkipSubpackageOperation checks if the subpackage operation should be skipped
// Returns true if no operation is specified or if the operation has already been executed
func (r *PackageRevisionReconciler) shouldSkipSubpackageOperation(pr *porchv1alpha2.PackageRevision) bool {
	if pr.Spec.SubpackageOperation == nil {
		return true
	}
	if pr.Status.LastSubpackageOperation != nil && pr.Status.LastSubpackageOperation == pr.Spec.SubpackageOperation {
		return true
	}
	return false
}
