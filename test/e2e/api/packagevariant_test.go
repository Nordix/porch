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

package api

import (
	"context"
	"fmt"
	"time"

	porchapi "github.com/kptdev/porch/api/porch/v1alpha1"
	configapi "github.com/kptdev/porch/api/porchconfig/v1alpha1"
	configapi2 "github.com/kptdev/porch/api/porchconfig/v1alpha2"
	"github.com/kptdev/porch/test/e2e/suiteutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// This file exercises the PackageVariant controller for the golden-parameters
// use case:
//   - genesis: the controller creates a downstream package from the upstream
//   - upgrade: when the upstream publishes a new revision, the controller
//     creates a new downstream revision tracking it (the bug that was fixed)
//   - autoPublishPolicy=approve: the controller proposes+approves the drafts
//     it creates, so changes propagate without manual lifecycle steps
//   - upgradePolicy=forceDeleteReplace: the upstream is treated as source of
//     truth on upgrade

const (
	goldenPkg        = "golden-parameters"
	goldenParamFile  = "config.yaml"
	nodeTypeLabelKey = "node_type"
	nodeTypeLabelPCG = "pcg"
	pvReconcileWait  = 180 * time.Second
	pvReconcilePoll  = 2 * time.Second
)

// goldenConfig renders a tiny golden-parameters KRM document with the given
// license port value, so successive upstream revisions differ in content.
func goldenConfig(port int) string {
	return fmt.Sprintf(`apiVersion: example.com/v1alpha1
kind: Config
metadata:
  name: node-golden
spec:
  license:
    port: %d
`, port)
}

// publishGoldenUpstream creates (or edits) the golden-parameters package in the
// upstream repo with the given port value and publishes it, returning the
// published PackageRevision. When source is nil the first revision is created
// by initializing a new package; when source is a previously published
// revision of the same package, a new revision is created by editing it (you
// cannot init the same package twice).
func (t *PorchSuite) publishGoldenUpstream(repo, workspace string, port int, source *porchapi.PackageRevision) *porchapi.PackageRevision {
	t.T().Helper()

	var pr *porchapi.PackageRevision
	if source == nil {
		pr = t.CreatePackageDraftF(repo, goldenPkg, workspace)
	} else {
		pr = t.CreatePackageSkeleton(repo, goldenPkg, workspace)
		pr.Spec.Tasks = []porchapi.Task{
			{
				Type: porchapi.TaskTypeEdit,
				Edit: &porchapi.PackageEditTaskSpec{
					Source: &porchapi.PackageRevisionRef{Name: source.Name},
				},
			},
		}
		t.CreateF(pr)
	}

	// write the golden parameter content
	var prr porchapi.PackageRevisionResources
	t.GetF(client.ObjectKey{Namespace: t.Namespace, Name: pr.Name}, &prr)
	prr.Spec.Resources[goldenParamFile] = goldenConfig(port)
	t.UpdateF(&prr)

	// propose + approve
	t.GetF(client.ObjectKeyFromObject(pr), pr)
	pr.Spec.Lifecycle = porchapi.PackageRevisionLifecycleProposed
	t.UpdateF(pr)
	pr.Spec.Lifecycle = porchapi.PackageRevisionLifecyclePublished
	return t.UpdateApprovalF(pr)
}

// newGoldenPackageVariant builds a PackageVariant CR for the golden-parameters
// use case with the given opt-in policies.
func (t *PorchSuite) newGoldenPackageVariant(name, upstreamRepo, upstreamWorkspace, downstreamRepo string,
	upgradePolicy configapi.UpgradePolicy, autoPublish configapi.AutoPublishPolicy) *configapi.PackageVariant {
	return &configapi.PackageVariant{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PackageVariant",
			APIVersion: configapi.GroupVersion.Identifier(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: t.Namespace,
		},
		Spec: configapi.PackageVariantSpec{
			Upstream: &configapi.Upstream{
				Repo:          upstreamRepo,
				Package:       goldenPkg,
				WorkspaceName: upstreamWorkspace,
			},
			Downstream: &configapi.Downstream{
				Repo:    downstreamRepo,
				Package: goldenPkg + "-pcg",
			},
			UpgradePolicy:     upgradePolicy,
			AutoPublishPolicy: autoPublish,
		},
	}
}

// downstreamRevisions returns the package revisions in the downstream repo for
// the golden-parameters-pcg package.
func (t *PorchSuite) downstreamRevisions(downstreamRepo string) []porchapi.PackageRevision {
	t.T().Helper()
	var list porchapi.PackageRevisionList
	t.ListE(&list, client.InNamespace(t.Namespace))
	var out []porchapi.PackageRevision
	for _, pr := range list.Items {
		if pr.Spec.RepositoryName == downstreamRepo && pr.Spec.PackageName == goldenPkg+"-pcg" {
			out = append(out, pr)
		}
	}
	return out
}

// waitForPublishedDownstream polls until the downstream repo has a published
// golden-parameters-pcg revision at >= minRevision, returning it.
func (t *PorchSuite) waitForPublishedDownstream(downstreamRepo string, minRevision int) porchapi.PackageRevision {
	t.T().Helper()
	var found porchapi.PackageRevision
	err := wait.PollUntilContextTimeout(t.GetContext(), pvReconcilePoll, pvReconcileWait, true,
		func(ctx context.Context) (bool, error) {
			for _, pr := range t.downstreamRevisions(downstreamRepo) {
				if pr.Spec.Lifecycle == porchapi.PackageRevisionLifecyclePublished && pr.Spec.Revision >= minRevision {
					found = pr
					return true, nil
				}
			}
			return false, nil
		})
	require.NoErrorf(t.T(), err, "no published downstream revision >= %d appeared in %s", minRevision, downstreamRepo)
	return found
}

// TestPackageVariantGoldenParametersAutoPublish covers the end-to-end golden
// parameters flow with autoPublishPolicy=approve and
// upgradePolicy=forceDeleteReplace: genesis and upgrade both propagate to a
// published downstream revision without manual lifecycle steps.
func (t *PorchSuite) TestPackageVariantGoldenParametersAutoPublish() {
	const (
		upstreamRepo   = "golden-upstream-auto"
		downstreamRepo = "golden-downstream-auto"
	)

	// Each porch Repository must back onto a distinct git repo, so create
	// dedicated Gitea repos for upstream and downstream.
	upstreamURL := t.CreateGiteaRepo(upstreamRepo)
	downstreamURL := t.CreateGiteaRepo(downstreamRepo)

	// Upstream repo holding the golden parameters package.
	t.RegisterGitRepositoryF(upstreamURL, upstreamRepo, "", suiteutils.GiteaUser, suiteutils.GiteaPassword)
	// Downstream deployment repo, labeled for the pcg node type.
	t.RegisterGitRepositoryF(downstreamURL, downstreamRepo, "", suiteutils.GiteaUser, suiteutils.GiteaPassword,
		suiteutils.RepositoryOptions{
			RepOpts: func(repo *configapi.Repository) {
				if repo.Labels == nil {
					repo.Labels = map[string]string{}
				}
				repo.Labels[nodeTypeLabelKey] = nodeTypeLabelPCG
			},
		})

	// Publish v1 of the golden parameters upstream (workspace "v1").
	v1 := t.publishGoldenUpstream(upstreamRepo, "v1", 9999, nil)

	// Create the PackageVariant with the golden-parameters opt-ins, pinned to
	// the v1 upstream workspace.
	pv := t.newGoldenPackageVariant("golden-pv-auto", upstreamRepo, "v1", downstreamRepo,
		configapi.UpgradePolicyForceDeleteReplace, configapi.AutoPublishApprove)
	t.CreateF(pv)
	t.Cleanup(func() { t.DeleteE(pv) })

	// Genesis: a published downstream revision appears without manual approval.
	genesis := t.waitForPublishedDownstream(downstreamRepo, 1)
	t.Logf("genesis published downstream %s at revision %d", genesis.Name, genesis.Spec.Revision)

	// Upgrade: publish v2 of the upstream with a changed golden parameter, then
	// re-point the PackageVariant at the v2 upstream workspace.
	t.publishGoldenUpstream(upstreamRepo, "v2", 8888, v1)
	t.GetF(client.ObjectKeyFromObject(pv), pv)
	pv.Spec.Upstream.WorkspaceName = "v2"
	t.UpdateF(pv)

	// The controller must create and auto-publish a NEW downstream revision.
	upgraded := t.waitForPublishedDownstream(downstreamRepo, genesis.Spec.Revision+1)
	assert.Greater(t, upgraded.Spec.Revision, genesis.Spec.Revision,
		"upgrade should produce a higher downstream revision than genesis")

	// The upgraded downstream should carry the new golden parameter value.
	var prr porchapi.PackageRevisionResources
	t.GetF(client.ObjectKey{Namespace: t.Namespace, Name: upgraded.Name}, &prr)
	content := prr.Spec.Resources[goldenParamFile]
	assert.Contains(t, content, "port: 8888", "downstream should reflect upgraded golden parameter")
	assert.NotContains(t, content, "port: 9999", "forceDeleteReplace should not retain the old value")
}

// TestPackageVariantGoldenParametersManualPublish covers the default
// autoPublishPolicy=none: the controller creates a downstream DRAFT on genesis
// and leaves publishing to the operator.
func (t *PorchSuite) TestPackageVariantGoldenParametersManualPublish() {
	const (
		upstreamRepo   = "golden-upstream-manual"
		downstreamRepo = "golden-downstream-manual"
	)

	upstreamURL := t.CreateGiteaRepo(upstreamRepo)
	downstreamURL := t.CreateGiteaRepo(downstreamRepo)

	t.RegisterGitRepositoryF(upstreamURL, upstreamRepo, "", suiteutils.GiteaUser, suiteutils.GiteaPassword)
	t.RegisterGitRepositoryF(downstreamURL, downstreamRepo, "", suiteutils.GiteaUser, suiteutils.GiteaPassword)

	t.publishGoldenUpstream(upstreamRepo, "v1", 9999, nil)

	// Default policies (no upgradePolicy / autoPublishPolicy set).
	pv := t.newGoldenPackageVariant("golden-pv-manual", upstreamRepo, "v1", downstreamRepo, "", "")
	t.CreateF(pv)
	t.Cleanup(func() { t.DeleteE(pv) })

	// A downstream DRAFT must appear, and must NOT be auto-published.
	var draft porchapi.PackageRevision
	err := wait.PollUntilContextTimeout(t.GetContext(), pvReconcilePoll, pvReconcileWait, true,
		func(ctx context.Context) (bool, error) {
			for _, pr := range t.downstreamRevisions(downstreamRepo) {
				if pr.Spec.Lifecycle == porchapi.PackageRevisionLifecycleDraft {
					draft = pr
					return true, nil
				}
			}
			return false, nil
		})
	require.NoError(t.T(), err, "expected a downstream draft to be created")

	// Give the controller a moment; confirm it stays a draft (no auto-publish).
	time.Sleep(10 * time.Second)
	var still porchapi.PackageRevision
	t.GetF(client.ObjectKeyFromObject(&draft), &still)
	assert.Equal(t, porchapi.PackageRevisionLifecycleDraft, still.Spec.Lifecycle,
		"with autoPublishPolicy=none the downstream must remain a draft")
}

// TestPackageVariantSetGoldenParametersFanout covers the golden-parameters use
// case driven through a PackageVariantSet: a repository selector fans out a
// PackageVariant to every repo carrying the node_type=pcg label, and the
// template propagates the upgrade and auto-publish policies to the generated
// PackageVariant so the downstream auto-publishes.
func (t *PorchSuite) TestPackageVariantSetGoldenParametersFanout() {
	const (
		upstreamRepo   = "golden-upstream-pvs"
		downstreamRepo = "golden-downstream-pvs"
	)

	upstreamURL := t.CreateGiteaRepo(upstreamRepo)
	downstreamURL := t.CreateGiteaRepo(downstreamRepo)

	t.RegisterGitRepositoryF(upstreamURL, upstreamRepo, "", suiteutils.GiteaUser, suiteutils.GiteaPassword)
	// Downstream deployment repo, labeled so the PVS repository selector matches it.
	t.RegisterGitRepositoryF(downstreamURL, downstreamRepo, "", suiteutils.GiteaUser, suiteutils.GiteaPassword,
		suiteutils.RepositoryOptions{
			RepOpts: func(repo *configapi.Repository) {
				if repo.Labels == nil {
					repo.Labels = map[string]string{}
				}
				repo.Labels[nodeTypeLabelKey] = nodeTypeLabelPCG
			},
		})

	// Publish v1 of the golden parameters upstream (workspace "v1").
	t.publishGoldenUpstream(upstreamRepo, "v1", 9999, nil)

	// Build a PackageVariantSet with a repository selector and a template that
	// carries the golden-parameters policies through to the spawned
	// PackageVariant.
	downstreamPkg := goldenPkg + "-pcg"
	upgradePolicy := configapi.UpgradePolicyForceDeleteReplace
	autoPublish := configapi.AutoPublishApprove
	pvs := &configapi2.PackageVariantSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PackageVariantSet",
			APIVersion: configapi2.GroupVersion.Identifier(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "golden-pvs",
			Namespace: t.Namespace,
		},
		Spec: configapi2.PackageVariantSetSpec{
			Upstream: &configapi.Upstream{
				Repo:          upstreamRepo,
				Package:       goldenPkg,
				WorkspaceName: "v1",
			},
			Targets: []configapi2.Target{
				{
					RepositorySelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{nodeTypeLabelKey: nodeTypeLabelPCG},
					},
					Template: &configapi2.PackageVariantTemplate{
						Downstream: &configapi2.DownstreamTemplate{
							Package: &downstreamPkg,
						},
						UpgradePolicy:     &upgradePolicy,
						AutoPublishPolicy: &autoPublish,
					},
				},
			},
		},
	}
	t.CreateF(pvs)
	t.Cleanup(func() { t.DeleteE(pvs) })

	// The PVS must spawn a PackageVariant, which in turn auto-publishes a
	// downstream revision carrying the golden parameter value.
	published := t.waitForPublishedDownstream(downstreamRepo, 1)
	t.Logf("PVS fan-out published downstream %s at revision %d", published.Name, published.Spec.Revision)

	var prr porchapi.PackageRevisionResources
	t.GetF(client.ObjectKey{Namespace: t.Namespace, Name: published.Name}, &prr)
	assert.Contains(t, prr.Spec.Resources[goldenParamFile], "port: 9999",
		"fanned-out downstream should carry the golden parameter value")

	// Confirm a PackageVariant was actually created by the set.
	var pvList configapi.PackageVariantList
	t.ListE(&pvList, client.InNamespace(t.Namespace))
	assert.NotEmpty(t, pvList.Items, "PackageVariantSet should have spawned at least one PackageVariant")
}
