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
// Only applies to Draft packages; Proposed and Published are immutable (aligns with v1alpha1).
func (r *PackageRevisionReconciler) reconcilePackageMetadata(ctx context.Context, pr *porchv1alpha2.PackageRevision, repoKey repository.RepositoryKey) (*ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Check early exit conditions.
	if shouldSkipMetadataSync(pr) {
		if pr.Spec.Lifecycle != porchv1alpha2.PackageRevisionLifecycleDraft && hasUserModifiedMetadata(pr) {
			log.V(3).Info("skipping metadata sync: non-Draft lifecycle")
		} else if !hasUserModifiedMetadata(pr) && pr.Spec.Lifecycle == porchv1alpha2.PackageRevisionLifecycleDraft {
			log.V(3).Info("skipping metadata sync: no user modifications")
		}
		return nil, nil
	}

	// Skip if a render is already pending (from PRR push) to respect "PRR push wins" semantics.
	if pr.Annotations[porchv1alpha2.AnnotationRenderRequest] != pr.Status.ObservedPrrResourceVersion {
		log.V(3).Info("render pending from PRR push, skipping metadata sync")
		return nil, nil
	}

	// Read and parse current package content.
	kf, err := r.readAndParseKptfile(ctx, repoKey, pr)
	if err != nil {
		return nil, nil
	}

	// Apply metadata changes and sync to draft.
	result, err := r.applyAndWriteMetadata(ctx, repoKey, pr, kf)
	if err != nil {
		return nil, nil
	}

	// Trigger render if package is already rendered.
	if result {
		return r.triggerRenderIfNeeded(ctx, pr)
	}

	return nil, nil
}

// shouldSkipMetadataSync returns true if metadata sync should be skipped based on lifecycle and metadata state.
func shouldSkipMetadataSync(pr *porchv1alpha2.PackageRevision) bool {
	// Only for Draft packages. Proposed/Published are immutable (v1alpha1 alignment).
	if pr.Spec.Lifecycle != porchv1alpha2.PackageRevisionLifecycleDraft {
		return true
	}

	// Skip if user hasn't modified metadata.
	if !hasUserModifiedMetadata(pr) {
		return true
	}

	return false
}

// readAndParseKptfile reads package content and parses the Kptfile.
func (r *PackageRevisionReconciler) readAndParseKptfile(ctx context.Context, repoKey repository.RepositoryKey, pr *porchv1alpha2.PackageRevision) (kptfilev1.KptFile, error) {
	log := log.FromContext(ctx)

	content, err := r.ContentCache.GetPackageContent(ctx, repoKey, pr.Spec.PackageName, pr.Spec.WorkspaceName)
	if err != nil {
		log.Error(err, "failed to get package content")
		return kptfilev1.KptFile{}, err
	}

	resources, err := content.GetResourceContents(ctx)
	if err != nil {
		log.Error(err, "failed to read resources")
		return kptfilev1.KptFile{}, err
	}

	kf, err := kptfileFromResources(resources)
	if err != nil {
		log.Error(err, "failed to parse Kptfile")
		return kptfilev1.KptFile{}, err
	}

	return kf, nil
}

// applyAndWriteMetadata applies metadata changes and writes to a draft.
func (r *PackageRevisionReconciler) applyAndWriteMetadata(ctx context.Context, repoKey repository.RepositoryKey, pr *porchv1alpha2.PackageRevision, kf kptfilev1.KptFile) (bool, error) {
	log := log.FromContext(ctx)

	// Apply spec.packageMetadata to Kptfile (merge mode).
	if !applyPackageMetadataToKptfile(&kf, pr) {
		return false, nil // No changes, nothing to write
	}

	// Serialize Kptfile.
	updatedKfBytes, err := yaml.MarshalWithOptions(&kf, &yaml.EncoderOptions{SeqIndent: yaml.WideSequenceStyle})
	if err != nil {
		log.Error(err, "failed to serialize Kptfile")
		return false, err
	}

	// Create draft and write updated resources.
	draft, err := r.ContentCache.CreateDraftFromExisting(ctx, repoKey, pr.Spec.PackageName, pr.Spec.WorkspaceName)
	if err != nil {
		log.Error(err, "failed to create draft")
		return false, err
	}

	updatedResources := map[string]string{"Kptfile": string(updatedKfBytes)}
	if err := draft.UpdateResources(ctx, updatedResources, "metadata-sync"); err != nil {
		log.Error(err, "failed to write resources")
		return false, err
	}

	if err := r.ContentCache.CloseDraft(ctx, repoKey, draft, 0); err != nil {
		log.Error(err, "failed to close draft")
		return false, err
	}

	log.V(3).Info("metadata synced to draft")
	return true, nil
}

// triggerRenderIfNeeded triggers a render cycle for packages that have already been rendered.
// For new (unrendered) packages, render will occur via sourceTrigger without annotation patching.
func (r *PackageRevisionReconciler) triggerRenderIfNeeded(ctx context.Context, pr *porchv1alpha2.PackageRevision) (*ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Trigger render based on package state:
	// - Already-rendered packages: patch annotation to trigger render in next cycle via annotationTrigger
	// - New packages: skip annotation, rely on sourceTrigger (no requeue needed)
	if isRenderedTrue(pr) {
		if err := r.setRenderRequestAnnotation(ctx, pr); err != nil {
			log.Error(err, "failed to set render annotation")
			return nil, nil
		}
		log.Info("metadata updated in already-rendered draft, render queued via annotation")
		return &ctrl.Result{Requeue: true}, nil
	}

	log.Info("metadata synced to new package, render will trigger via sourceTrigger")
	return nil, nil
}

// hasUserModifiedMetadata checks if spec.packageMetadata was set by a non-controller field manager.
// Returns true only if packageMetadata is managed by someone other than the packagerev-controller.
func hasUserModifiedMetadata(pr *porchv1alpha2.PackageRevision) bool {
	if pr.Spec.PackageMetadata == nil {
		return false
	}

	// Check if any non-controller manager has touched this object.
	// If metadata exists and only the controller owns it, return false (no user changes).
	// If any other manager owns or co-owns the object, assume user set the metadata.
	for _, mf := range pr.ManagedFields {
		if mf.Manager != fieldManagerPRControllerKptfile {
			// Another manager (likely kubectl/user) has claimed fields on this object.
			// If metadata is present, user likely set it.
			return true
		}
	}

	// Only our field manager has touched the object, no user changes to metadata.
	return false
}

// applyPackageMetadataToKptfile applies labels and annotations to Kptfile (merge mode).
func applyPackageMetadataToKptfile(kf *kptfilev1.KptFile, pr *porchv1alpha2.PackageRevision) bool {
	if pr.Spec.PackageMetadata == nil {
		return false
	}

	var changed bool

	kf.Labels, changed = applyMetadataMap(kf.Labels, pr.Spec.PackageMetadata.Labels)

	var annotationsChanged bool
	kf.Annotations, annotationsChanged = applyMetadataMap(kf.Annotations, pr.Spec.PackageMetadata.Annotations)
	changed = changed || annotationsChanged

	return changed
}

// applyMetadataMap merges desired key-value pairs into current, returning the resulting map and whether any changes were made.
// Safe to call with nil current or desired maps.
func applyMetadataMap(current, desired map[string]string) (map[string]string, bool) {
	if len(desired) == 0 {
		return current, false
	}

	if current == nil {
		current = make(map[string]string, len(desired))
	}

	changed := false
	for k, v := range desired {
		if cv, exists := current[k]; !exists || cv != v {
			current[k] = v
			changed = true
		}
	}

	return current, changed
}

// setRenderRequestAnnotation triggers render by updating the render-request annotation with nanosecond precision.
func (r *PackageRevisionReconciler) setRenderRequestAnnotation(ctx context.Context, pr *porchv1alpha2.PackageRevision) error {
	// Capture original before mutation to generate a proper MergeFrom patch.
	original := pr.DeepCopy()

	if pr.Annotations == nil {
		pr.Annotations = make(map[string]string)
	}
	// Value just needs to differ from the previous one to trigger a reconcile.
	// Nanosecond precision avoids collisions on rapid successive updates; human-readable format aids debugging.
	pr.Annotations[porchv1alpha2.AnnotationRenderRequest] = metav1.Now().Format("2006-01-02T15:04:05.000000000Z07:00")
	return r.Patch(ctx, pr, client.MergeFrom(original))
}
