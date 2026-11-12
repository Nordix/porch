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

package packagevariant

import (
	"context"
	"testing"

	porchapi "github.com/kptdev/porch/api/porch/v1alpha1"
	api "github.com/kptdev/porch/api/porchconfig/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestIsUpToDate_UpgradeDetection covers isUpToDate, including the regression
// where a published downstream with a missing/empty upstream lock was wrongly
// reported up-to-date, suppressing the upgrade.
func TestIsUpToDate_UpgradeDetection(t *testing.T) {
	// Target upstream revision; the cases vary the downstream's upstreamLock.
	pvWithTargetRevision := func(rev int) *api.PackageVariant {
		return &api.PackageVariant{
			Spec: api.PackageVariantSpec{
				Upstream: &api.Upstream{
					Repo:     "blueprints",
					Package:  "golden-parameters",
					Revision: rev,
				},
			},
		}
	}

	downstream := func(lifecycle porchapi.PackageRevisionLifecycle, lock *porchapi.Locator) *porchapi.PackageRevision {
		return &porchapi.PackageRevision{
			Spec: porchapi.PackageRevisionSpec{
				PackageName:    "golden-parameters-pcg",
				RepositoryName: "deployments",
				Lifecycle:      lifecycle,
			},
			Status: porchapi.PackageRevisionStatus{
				UpstreamLock: lock,
			},
		}
	}
	published := porchapi.PackageRevisionLifecyclePublished
	draft := porchapi.PackageRevisionLifecycleDraft

	gitLock := func(ref string) *porchapi.Locator {
		return &porchapi.Locator{
			Git: &porchapi.GitLock{
				Repo: "https://example.com/blueprints.git",
				Ref:  ref,
			},
		}
	}

	testCases := map[string]struct {
		pv             *api.PackageVariant
		downstream     *porchapi.PackageRevision
		expectUpToDate bool
	}{
		"published, nil upstreamLock -> upgrade": {
			pv:             pvWithTargetRevision(2),
			downstream:     downstream(published, nil),
			expectUpToDate: false,
		},
		"published, empty git ref -> upgrade": {
			pv:             pvWithTargetRevision(2),
			downstream:     downstream(published, gitLock("")),
			expectUpToDate: false,
		},
		"draft, nil upstreamLock -> left alone": {
			pv:             pvWithTargetRevision(2),
			downstream:     downstream(draft, nil),
			expectUpToDate: true,
		},
		"downstream on older revision triggers upgrade": {
			pv:             pvWithTargetRevision(2),
			downstream:     downstream(published, gitLock("refs/tags/golden-parameters/v1")),
			expectUpToDate: false,
		},
		"downstream on target revision is up to date": {
			pv:             pvWithTargetRevision(2),
			downstream:     downstream(published, gitLock("refs/tags/golden-parameters/v2")),
			expectUpToDate: true,
		},
		"downstream on draft upstream triggers upgrade": {
			pv:             pvWithTargetRevision(2),
			downstream:     downstream(published, gitLock("drafts/golden-parameters/packagevariant-1")),
			expectUpToDate: false,
		},
	}

	r := &PackageVariantReconciler{}
	for tn, tc := range testCases {
		t.Run(tn, func(t *testing.T) {
			actual := r.isUpToDate(tc.pv, tc.downstream)
			require.Equal(t, tc.expectUpToDate, actual)
		})
	}
}

// TestMergeStrategyForPolicy verifies the UpgradePolicy -> PackageMergeStrategy
// mapping used by createUpgradeDraft.
func TestMergeStrategyForPolicy(t *testing.T) {
	testCases := map[string]struct {
		policy   api.UpgradePolicy
		expected porchapi.PackageMergeStrategy
	}{
		"default empty -> resource-merge": {
			policy:   "",
			expected: porchapi.ResourceMerge,
		},
		"resourceMerge -> resource-merge": {
			policy:   api.UpgradePolicyResourceMerge,
			expected: porchapi.ResourceMerge,
		},
		"forceDeleteReplace -> force-delete-replace": {
			policy:   api.UpgradePolicyForceDeleteReplace,
			expected: porchapi.ForceDeleteReplace,
		},
	}
	for tn, tc := range testCases {
		t.Run(tn, func(t *testing.T) {
			require.Equal(t, tc.expected, mergeStrategyForPolicy(tc.policy))
		})
	}
}

// TestEnsureAutoPublish verifies the opt-in propose/approve lifecycle stepping.
func TestEnsureAutoPublish(t *testing.T) {
	ownerRef := func() metav1.OwnerReference {
		tr := true
		return metav1.OwnerReference{
			APIVersion: "config.porch.kpt.dev/v1alpha1",
			Kind:       "PackageVariant",
			Name:       "my-pv",
			UID:        "pv-uid",
			Controller: &tr,
		}
	}

	owned := func(name string, lifecycle porchapi.PackageRevisionLifecycle) *porchapi.PackageRevision {
		return &porchapi.PackageRevision{
			ObjectMeta: metav1.ObjectMeta{
				Name:            name,
				OwnerReferences: []metav1.OwnerReference{ownerRef()},
			},
			Spec: porchapi.PackageRevisionSpec{Lifecycle: lifecycle},
		}
	}

	foreign := func(name string, lifecycle porchapi.PackageRevisionLifecycle) *porchapi.PackageRevision {
		return &porchapi.PackageRevision{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       porchapi.PackageRevisionSpec{Lifecycle: lifecycle},
		}
	}

	pv := func(policy api.AutoPublishPolicy) *api.PackageVariant {
		return &api.PackageVariant{
			ObjectMeta: metav1.ObjectMeta{Name: "my-pv", UID: "pv-uid"},
			Spec:       api.PackageVariantSpec{AutoPublishPolicy: policy},
		}
	}

	draft := porchapi.PackageRevisionLifecycleDraft
	proposed := porchapi.PackageRevisionLifecycleProposed
	published := porchapi.PackageRevisionLifecyclePublished

	testCases := map[string]struct {
		pv              *api.PackageVariant
		targets         []*porchapi.PackageRevision
		expectRequeue   bool
		expectOutput    []string
		expectLifecycle map[string]porchapi.PackageRevisionLifecycle
	}{
		"policy none is a no-op": {
			pv:              pv(api.AutoPublishNone),
			targets:         []*porchapi.PackageRevision{owned("pr-1", draft)},
			expectRequeue:   false,
			expectOutput:    nil,
			expectLifecycle: map[string]porchapi.PackageRevisionLifecycle{"pr-1": draft},
		},
		"approve proposes a draft and requeues": {
			pv:              pv(api.AutoPublishApprove),
			targets:         []*porchapi.PackageRevision{owned("pr-1", draft)},
			expectRequeue:   true,
			expectOutput:    []string{"updating object: pr-1"},
			expectLifecycle: map[string]porchapi.PackageRevisionLifecycle{"pr-1": proposed},
		},
		"approve publishes a proposed revision without requeue": {
			pv:              pv(api.AutoPublishApprove),
			targets:         []*porchapi.PackageRevision{owned("pr-1", proposed)},
			expectRequeue:   false,
			expectOutput:    []string{"updating approval of object: pr-1"},
			expectLifecycle: map[string]porchapi.PackageRevisionLifecycle{"pr-1": published},
		},
		"approve leaves published revisions alone": {
			pv:              pv(api.AutoPublishApprove),
			targets:         []*porchapi.PackageRevision{owned("pr-1", published)},
			expectRequeue:   false,
			expectOutput:    nil,
			expectLifecycle: map[string]porchapi.PackageRevisionLifecycle{"pr-1": published},
		},
		"approve never touches revisions we don't own": {
			pv:              pv(api.AutoPublishApprove),
			targets:         []*porchapi.PackageRevision{foreign("pr-1", draft)},
			expectRequeue:   false,
			expectOutput:    nil,
			expectLifecycle: map[string]porchapi.PackageRevisionLifecycle{"pr-1": draft},
		},
	}

	for tn, tc := range testCases {
		t.Run(tn, func(t *testing.T) {
			fc := &fakeClient{}
			r := &PackageVariantReconciler{Client: fc}

			requeue, err := r.ensureAutoPublish(context.TODO(), tc.pv, tc.targets)
			require.NoError(t, err)
			require.Equal(t, tc.expectRequeue, requeue)
			require.Equal(t, tc.expectOutput, fc.output)
			for _, tgt := range tc.targets {
				require.Equal(t, tc.expectLifecycle[tgt.Name], tgt.Spec.Lifecycle)
			}
		})
	}

	// Error paths: propose (Draft update) and approve (approval subresource)
	// failures should be wrapped and surfaced.
	t.Run("propose failure is surfaced", func(t *testing.T) {
		fc := &fakeClient{updateErr: assert.AnError}
		r := &PackageVariantReconciler{Client: fc}
		_, err := r.ensureAutoPublish(context.TODO(), pv(api.AutoPublishApprove),
			[]*porchapi.PackageRevision{owned("pr-1", draft)})
		require.Error(t, err)
		require.Contains(t, err.Error(), "auto-publish: failed to propose")
	})

	t.Run("approve failure is surfaced", func(t *testing.T) {
		fc := &fakeClient{subResourceUpdateErr: assert.AnError}
		r := &PackageVariantReconciler{Client: fc}
		_, err := r.ensureAutoPublish(context.TODO(), pv(api.AutoPublishApprove),
			[]*porchapi.PackageRevision{owned("pr-1", proposed)})
		require.Error(t, err)
		require.Contains(t, err.Error(), "auto-publish: failed to approve")
	})
}

// TestValidateNewPolicyFields verifies defaulting and validation for the new
// upgradePolicy and autoPublishPolicy fields.
func TestValidateNewPolicyFields(t *testing.T) {
	base := func() *api.PackageVariant {
		return &api.PackageVariant{
			Spec: api.PackageVariantSpec{
				Upstream:   &api.Upstream{Repo: "blueprints", Package: "foo"},
				Downstream: &api.Downstream{Repo: "deployments", Package: "foo"},
			},
		}
	}

	t.Run("defaults are applied", func(t *testing.T) {
		pv := base()
		errs := validatePackageVariant(pv)
		require.Empty(t, errs)
		require.Equal(t, api.UpgradePolicyResourceMerge, pv.Spec.UpgradePolicy)
		require.Equal(t, api.AutoPublishNone, pv.Spec.AutoPublishPolicy)
	})

	t.Run("valid explicit values pass", func(t *testing.T) {
		pv := base()
		pv.Spec.UpgradePolicy = api.UpgradePolicyForceDeleteReplace
		pv.Spec.AutoPublishPolicy = api.AutoPublishApprove
		require.Empty(t, validatePackageVariant(pv))
	})

	t.Run("invalid upgradePolicy is rejected", func(t *testing.T) {
		pv := base()
		pv.Spec.UpgradePolicy = "bogus"
		errs := validatePackageVariant(pv)
		require.Contains(t, combineErrors(errs), "spec.upgradePolicy can only be")
	})

	t.Run("invalid autoPublishPolicy is rejected", func(t *testing.T) {
		pv := base()
		pv.Spec.AutoPublishPolicy = "bogus"
		errs := validatePackageVariant(pv)
		require.Contains(t, combineErrors(errs), "spec.autoPublishPolicy can only be")
	})
}
