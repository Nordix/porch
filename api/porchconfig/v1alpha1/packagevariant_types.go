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

package v1alpha1

import (
	kptfilev1 "github.com/kptdev/kpt/api/kptfile/v1"
	porchapi "github.com/kptdev/porch/api/porch/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:ac:generate=true

// PackageVariant represents an upstream and downstream porch package pair.
// The upstream package should already exist. The PackageVariant controller is
// responsible for creating the downstream package revisions based on the spec.
type PackageVariant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PackageVariantSpec   `json:"spec,omitempty"`
	Status PackageVariantStatus `json:"status,omitempty"`
}

func (o *PackageVariant) GetSpec() *PackageVariantSpec {
	if o == nil {
		return nil
	}
	return &o.Spec
}

type AdoptionPolicy string
type DeletionPolicy string

// UpgradePolicy controls the merge strategy the controller uses when it
// upgrades a downstream package to a new upstream revision.
type UpgradePolicy string

// AutoPublishPolicy controls whether the controller automatically advances the
// drafts it creates through the package revision lifecycle (propose then
// approve/publish) instead of leaving them as drafts for a human or another
// controller to publish.
type AutoPublishPolicy string

const (
	AdoptionPolicyAdoptExisting AdoptionPolicy = "adoptExisting"
	AdoptionPolicyAdoptNone     AdoptionPolicy = "adoptNone"

	DeletionPolicyDelete DeletionPolicy = "delete"
	DeletionPolicyOrphan DeletionPolicy = "orphan"

	// UpgradePolicyResourceMerge merges upstream changes into the downstream,
	// preserving local edits (default).
	UpgradePolicyResourceMerge UpgradePolicy = "resourceMerge"
	// UpgradePolicyForceDeleteReplace replaces the downstream with the upstream,
	// discarding local edits.
	UpgradePolicyForceDeleteReplace UpgradePolicy = "forceDeleteReplace"

	// AutoPublishNone leaves created drafts as drafts (default).
	AutoPublishNone AutoPublishPolicy = "none"
	// AutoPublishApprove proposes and publishes created drafts.
	AutoPublishApprove AutoPublishPolicy = "approve"

	Finalizer = "config.porch.kpt.dev/packagevariants"
)

// PackageVariantSpec defines the desired state of PackageVariant
type PackageVariantSpec struct {
	Upstream   *Upstream   `json:"upstream,omitempty"`
	Downstream *Downstream `json:"downstream,omitempty"`

	AdoptionPolicy AdoptionPolicy `json:"adoptionPolicy,omitempty"`
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`

	// UpgradePolicy selects the merge strategy used when upgrading a downstream
	// package to a new upstream revision. Defaults to "resourceMerge".
	// Use "forceDeleteReplace" to treat the upstream as the source of truth and
	// overwrite local downstream changes (golden-parameters use case).
	// +optional
	UpgradePolicy UpgradePolicy `json:"upgradePolicy,omitempty"`

	// AutoPublishPolicy controls whether the controller automatically proposes
	// and approves (publishes) the drafts it creates. Defaults to "none", which
	// leaves drafts for manual publishing. Set to "approve" to have the
	// controller drive the full lifecycle so upstream changes propagate to the
	// node without manual intervention.
	// +optional
	AutoPublishPolicy AutoPublishPolicy `json:"autoPublishPolicy,omitempty"`

	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`

	PackageContext *PackageContext     `json:"packageContext,omitempty"`
	Pipeline       *kptfilev1.Pipeline `json:"pipeline,omitempty"`
	Injectors      []InjectionSelector `json:"injectors,omitempty"`
}

type Upstream struct {
	Repo          string `json:"repo,omitempty"`
	Package       string `json:"package,omitempty"`
	Revision      int    `json:"revision,omitempty"`
	WorkspaceName string `json:"workspaceName,omitempty"`
}

type Downstream struct {
	Repo    string `json:"repo,omitempty"`
	Package string `json:"package,omitempty"`
}

// PackageContext defines the data to be added or removed from the
// kptfile.kpt.dev ConfigMap during reconciliation.
type PackageContext struct {
	Data       map[string]string `json:"data,omitempty"`
	RemoveKeys []string          `json:"removeKeys,omitempty"`
}

// InjectionSelector specifies how to select in-cluster objects for
// resolving injection points.
type InjectionSelector struct {
	Group   *string `json:"group,omitempty"`
	Version *string `json:"version,omitempty"`
	Kind    *string `json:"kind,omitempty"`
	Name    string  `json:"name"`
}

// PackageVariantStatus defines the observed state of PackageVariant
type PackageVariantStatus struct {
	// Conditions describes the reconciliation state of the object.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// DownstreamTargets contains the downstream targets that the PackageVariant
	// either created or adopted.
	DownstreamTargets []DownstreamTarget `json:"downstreamTargets,omitempty"`
}

type DownstreamTarget struct {
	Name         string                `json:"name,omitempty"`
	RenderStatus porchapi.RenderStatus `json:"renderStatus,omitempty"`
}

//+kubebuilder:object:root=true

// PackageVariantList contains a list of PackageVariant
type PackageVariantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PackageVariant `json:"items"`
}
