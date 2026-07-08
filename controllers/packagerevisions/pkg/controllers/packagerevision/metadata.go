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
	"maps"

	kptfilev1 "github.com/kptdev/kpt/api/kptfile/v1"

	porchv1alpha2 "github.com/kptdev/porch/api/porch/v1alpha2"
	"github.com/kptdev/porch/pkg/repository"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// reconcilePackageMetadata syncs user-set spec.packageMetadata to the Kptfile.
// Applies to Draft and Proposed; Published packages are immutable.
func (r *PackageRevisionReconciler) reconcilePackageMetadata(ctx context.Context, pr *porchv1alpha2.PackageRevision, repoKey repository.RepositoryKey) (*ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Only for Draft and Proposed packages.
	lifecycle := porchv1alpha2.PackageRevisionLifecycle(pr.Spec.Lifecycle)
	if lifecycle != porchv1alpha2.PackageRevisionLifecycleDraft && lifecycle != porchv1alpha2.PackageRevisionLifecycleProposed {
		return nil, nil
	}

	// Skip if user hasn't modified metadata.
	if !hasUserModifiedMetadata(pr) {
		return nil, nil
	}

	// Read current content.
	content, err := r.ContentCache.GetPackageContent(ctx, repoKey, pr.Spec.PackageName, pr.Spec.WorkspaceName)
	if err != nil {
		log.Error(err, "failed to get package content")
		return nil, nil
	}

	resources, err := content.GetResourceContents(ctx)
	if err != nil {
		log.Error(err, "failed to read resources")
		return nil, nil
	}

	kf, err := kptfileFromResources(resources)
	if err != nil {
		log.Error(err, "failed to parse Kptfile")
		return nil, nil
	}

	// Apply spec.packageMetadata to Kptfile (merge mode).
	if !applyPackageMetadataToKptfile(&kf, pr) {
		return nil, nil
	}

	// Serialize and write back.
	updatedKfBytes, err := yaml.MarshalWithOptions(&kf, &yaml.EncoderOptions{SeqIndent: yaml.WideSequenceStyle})
	if err != nil {
		log.Error(err, "failed to serialize Kptfile")
		return nil, nil
	}

	updatedResources := maps.Clone(resources)
	updatedResources["Kptfile"] = string(updatedKfBytes)

	draft, err := r.ContentCache.CreateDraftFromExisting(ctx, repoKey, pr.Spec.PackageName, pr.Spec.WorkspaceName)
	if err != nil {
		log.Error(err, "failed to create draft")
		return nil, nil
	}

	if err := draft.UpdateResources(ctx, updatedResources, "metadata-sync"); err != nil {
		log.Error(err, "failed to write resources")
		return nil, nil
	}

	if err := r.ContentCache.CloseDraft(ctx, repoKey, draft, 0); err != nil {
		log.Error(err, "failed to close draft")
		return nil, nil
	}

	// Trigger render by patching annotation.
	if err := r.setRenderRequestAnnotation(ctx, pr); err != nil {
		log.Error(err, "failed to set render annotation")
		return nil, nil
	}

	log.Info("metadata updated in draft, render queued")
	return &ctrl.Result{Requeue: true}, nil
}

// hasUserModifiedMetadata checks if spec.packageMetadata was set by a non-controller field manager.
func hasUserModifiedMetadata(pr *porchv1alpha2.PackageRevision) bool {
	if pr.Spec.PackageMetadata == nil {
		return false
	}

	// If no managedFields, or a manager other than ours owns it, user modified it.
	for _, mf := range pr.ManagedFields {
		if mf.Manager != fieldManagerPRControllerKptfile {
			return true
		}
	}

	// Only our field manager owns it — no user changes.
	return false
}

// applyPackageMetadataToKptfile applies labels and annotations to Kptfile (merge mode).
func applyPackageMetadataToKptfile(kf *kptfilev1.KptFile, pr *porchv1alpha2.PackageRevision) bool {
	if pr.Spec.PackageMetadata == nil {
		return false
	}

	var changed bool

	if pr.Spec.PackageMetadata.Labels != nil {
		if applyMetadataMap(kf, pr.Spec.PackageMetadata.Labels, true) {
			changed = true
		}
	}

	if pr.Spec.PackageMetadata.Annotations != nil {
		if applyMetadataMap(kf, pr.Spec.PackageMetadata.Annotations, false) {
			changed = true
		}
	}

	return changed
}

// applyMetadataMap applies labels (isLabels=true) or annotations (isLabels=false) to Kptfile in merge mode.
func applyMetadataMap(kf *kptfilev1.KptFile, desired map[string]string, isLabels bool) bool {
	var current map[string]string
	if isLabels {
		current = kf.Labels
		if current == nil {
			current = make(map[string]string)
			kf.Labels = current
		}
	} else {
		current = kf.Annotations
		if current == nil {
			current = make(map[string]string)
			kf.Annotations = current
		}
	}

	changed := false
	for k, v := range desired {
		if cv, exists := current[k]; !exists || cv != v {
			current[k] = v
			changed = true
		}
	}

	return changed
}

// setRenderRequestAnnotation triggers render by updating the render-request annotation.
func (r *PackageRevisionReconciler) setRenderRequestAnnotation(ctx context.Context, pr *porchv1alpha2.PackageRevision) error {
	if pr.Annotations == nil {
		pr.Annotations = make(map[string]string)
	}
	pr.Annotations[porchv1alpha2.AnnotationRenderRequest] = metav1.Now().Format("2006-01-02T15:04:05Z07:00")
	return r.Patch(ctx, pr, client.MergeFrom(pr))
}
