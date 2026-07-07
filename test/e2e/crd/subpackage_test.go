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

package crd

import (
	"strings"

	porchv1alpha2 "github.com/kptdev/porch/api/porch/v1alpha2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Subpackage", Ordered, Label("lifecycle"), func() {
	var env *testEnv

	BeforeAll(func() {
		env = sharedEnv()
	})

	Context("simple clone and upgrade off root", func() {
		It("should clone and upgrade a subpackage off root", func() {
			simpleSubpackageCloneAndUpgrade(env, "subpkg-off-root", "my-subpackage")
		})
	})

	Context("simple clone and upgrade down levels", func() {
		It("should clone and upgrade a subpackage down multiple directory levels", func() {
			simpleSubpackageCloneAndUpgrade(env, "subpkg-down-levels", "level1/level2/level3/level4/my-subpackage")
		})
	})

	Context("clone into root rejected", func() {
		It("should reject cloning a subpackage into root", func() {
			repo := "subpkg-clone-into-root"
			createGiteaRepo(repo)
			registerV1Alpha2Repo(env.Ctx, env.Namespace, repo)
			DeferCleanup(func() {
				cleanupRepo(env.Ctx, env.Namespace, repo)
				deleteGiteaRepo(repo)
			})

			cloneePR := createSubpkgPR(env, repo, "clonee-pkg", "v1")
			publishPackage(env.Ctx, cloneePR)
			DeferCleanup(deletePackage, env.Ctx, cloneePR)

			parentPR := createSubpkgPR(env, repo, "parent-pkg", "v1")
			DeferCleanup(deletePackage, env.Ctx, parentPR)

			err := cloneSubpackage(env.Ctx, parentPR, cloneePR.Name, "")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(SatisfyAny(
				ContainSubstring("subpackage directory"),
				ContainSubstring("is invalid"),
				ContainSubstring("subpackageDir"),
			))
		})
	})

	Context("clone into existing subpackage rejected", func() {
		It("should reject cloning into an existing or nested subpackage dir", func() {
			repo := "subpkg-clone-existing"
			createGiteaRepo(repo)
			registerV1Alpha2Repo(env.Ctx, env.Namespace, repo)
			DeferCleanup(func() {
				cleanupRepo(env.Ctx, env.Namespace, repo)
				deleteGiteaRepo(repo)
			})

			const (
				subpackageDir1 = "level1/level2/my-subpackage-1"
				subpackageDir2 = "level1/level2/my-subpackage-1/my-subpackage-2"
				subpackageDir3 = "level1/level2/my-subpackage-1"
				subpackageDir4 = "level1/level2/my-subpackage-1/"
			)

			cloneePR := createSubpkgPR(env, repo, "clonee-pkg", "v1")
			publishPackage(env.Ctx, cloneePR)
			DeferCleanup(deletePackage, env.Ctx, cloneePR)

			parentPR := createSubpkgPR(env, repo, "parent-pkg", "v1")
			DeferCleanup(deletePackage, env.Ctx, parentPR)

			By("cloning subpackage into dir1 succeeds")
			Expect(cloneSubpackage(env.Ctx, parentPR, cloneePR.Name, subpackageDir1)).To(Succeed())
			waitForReady(env.Ctx, parentPR)

			By("verifying subpackage Kptfile present")
			resources := getPRRResources(env.Ctx, env.Namespace, parentPR.Name)
			Expect(resources).To(HaveKey(subpackageDir1 + "/Kptfile"))

			By("cloning into nested dir inside existing subpackage fails")
			err := cloneSubpackage(env.Ctx, parentPR, cloneePR.Name, subpackageDir2)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(SatisfyAny(
				ContainSubstring("cannot clone subpackage into another subpackage"),
				ContainSubstring("cannot clone subpackage into parent"),
				ContainSubstring("already has"),
			))

			By("re-cloning the same dir fails")
			err = cloneSubpackage(env.Ctx, parentPR, cloneePR.Name, subpackageDir3)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(SatisfyAny(
				ContainSubstring("cannot clone subpackage into another subpackage"),
				ContainSubstring("cannot clone subpackage into parent"),
				ContainSubstring("already has"),
			))

			By("cloning with trailing slash fails validation")
			err = cloneSubpackage(env.Ctx, parentPR, cloneePR.Name, subpackageDir4)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("subpackageDir"))
		})
	})

	Context("upgrade nonexisting subpackage rejected", func() {
		It("should reject upgrading a subpackage dir that does not exist in the package", func() {
			repo := "subpkg-upgrade-nonexisting"
			createGiteaRepo(repo)
			registerV1Alpha2Repo(env.Ctx, env.Namespace, repo)
			DeferCleanup(func() {
				cleanupRepo(env.Ctx, env.Namespace, repo)
				deleteGiteaRepo(repo)
			})

			const (
				subpackageDir1 = "level1/level2/my-subpackage-1"
				subpackageDir2 = "level1/level2/my-subpackage-1/my-subpackage-2"
				subpackageDir3 = "level1/level2/my-subpackage-3"
			)

			cloneePRV1 := createSubpkgPR(env, repo, "clonee-pkg", "v1")
			publishPackage(env.Ctx, cloneePRV1)
			DeferCleanup(deletePackage, env.Ctx, cloneePRV1)

			cloneePRV2 := createSubpkgCopy(env, repo, cloneePRV1, "v2")
			publishPackage(env.Ctx, cloneePRV2)
			DeferCleanup(deletePackage, env.Ctx, cloneePRV2)

			parentPR := createSubpkgPR(env, repo, "parent-pkg", "v1")
			DeferCleanup(deletePackage, env.Ctx, parentPR)

			By("cloning subpackage into dir1")
			Expect(cloneSubpackage(env.Ctx, parentPR, cloneePRV1.Name, subpackageDir1)).To(Succeed())
			waitForReady(env.Ctx, parentPR)

			By("verifying Kptfile ref is v1")
			expectedName := strings.ReplaceAll(subpackageDir1, "/", ".")
			resources := getPRRResources(env.Ctx, env.Namespace, parentPR.Name)
			Expect(resources[subpackageDir1+"/Kptfile"]).To(ContainSubstring("name: " + expectedName))
			Expect(resources[subpackageDir1+"/Kptfile"]).To(ContainSubstring("ref: clonee-pkg/v1"))

			By("upgrading subpackage in dir1 succeeds")
			Expect(upgradeSubpackage(env.Ctx, parentPR, cloneePRV1.Name, cloneePRV2.Name, subpackageDir1)).To(Succeed())
			waitForReady(env.Ctx, parentPR)

			By("verifying Kptfile ref is v2")
			resources = getPRRResources(env.Ctx, env.Namespace, parentPR.Name)
			Expect(resources[subpackageDir1+"/Kptfile"]).To(ContainSubstring("ref: clonee-pkg/v2"))

			By("upgrading subpackage in nonexistent nested dir fails")
			err := upgradeSubpackage(env.Ctx, parentPR, cloneePRV1.Name, cloneePRV2.Name, subpackageDir2)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("does not have a subpackage at"))

			By("upgrading subpackage in nonexistent sibling dir fails")
			err = upgradeSubpackage(env.Ctx, parentPR, cloneePRV1.Name, cloneePRV2.Name, subpackageDir3)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("does not have a subpackage at"))
		})
	})

	Context("clone and upgrade non-overlapping subpackages", func() {
		It("should clone and upgrade multiple non-overlapping subpackages independently", func() {
			repo := "subpkg-clone-overlapping"
			createGiteaRepo(repo)
			registerV1Alpha2Repo(env.Ctx, env.Namespace, repo)
			DeferCleanup(func() {
				cleanupRepo(env.Ctx, env.Namespace, repo)
				deleteGiteaRepo(repo)
			})

			const (
				subpackageDir1 = "level1/level2/level3/my-subpackage-1"
				subpackageDir2 = "level1/level2/level3/my-subpackage-2"
				subpackageDir3 = "level1/my-subpackage-3"
				subpackageDir4 = "level1/level2/my-subpackage-4"
			)

			// Create 4 cloneable packages, each with v1/v2/v3
			cloneePRs := make([]*porchv1alpha2.PackageRevision, 4)
			cloneePRsV2 := make([]*porchv1alpha2.PackageRevision, 4)
			cloneePRsV3 := make([]*porchv1alpha2.PackageRevision, 4)
			subpkgDirs := []string{subpackageDir1, subpackageDir2, subpackageDir3, subpackageDir4}

			for i := 1; i <= 4; i++ {
				pkgName := "clonee-pkg-" + string(rune('0'+i))
				v1 := createSubpkgPR(env, repo, pkgName, "v1")
				publishPackage(env.Ctx, v1)
				DeferCleanup(deletePackage, env.Ctx, v1)

				v2 := createSubpkgCopy(env, repo, v1, "v2")
				publishPackage(env.Ctx, v2)
				DeferCleanup(deletePackage, env.Ctx, v2)

				v3 := createSubpkgCopy(env, repo, v2, "v3")
				publishPackage(env.Ctx, v3)
				DeferCleanup(deletePackage, env.Ctx, v3)

				cloneePRs[i-1] = v1
				cloneePRsV2[i-1] = v2
				cloneePRsV3[i-1] = v3
			}

			parentPR := createSubpkgPR(env, repo, "parent-pkg", "v1")
			DeferCleanup(deletePackage, env.Ctx, parentPR)

			By("cloning all 4 subpackages into parent")
			for i, dir := range subpkgDirs {
				Expect(cloneSubpackage(env.Ctx, parentPR, cloneePRs[i].Name, dir)).To(Succeed())
				waitForReady(env.Ctx, parentPR)
			}

			By("verifying all 4 subpackage Kptfiles reference v1")
			resources := getPRRResources(env.Ctx, env.Namespace, parentPR.Name)
			for i, dir := range subpkgDirs {
				pkgName := "clonee-pkg-" + string(rune('0'+i+1))
				expectedName := strings.ReplaceAll(dir, "/", ".")
				Expect(resources[dir+"/Kptfile"]).To(ContainSubstring("name: " + expectedName))
				Expect(resources[dir+"/Kptfile"]).To(ContainSubstring("ref: " + pkgName + "/v1"))
			}

			By("upgrading all 4 subpackages to v2")
			for i, dir := range subpkgDirs {
				Expect(upgradeSubpackage(env.Ctx, parentPR, cloneePRs[i].Name, cloneePRsV2[i].Name, dir)).To(Succeed())
				waitForReady(env.Ctx, parentPR)
			}

			By("verifying all 4 subpackage Kptfiles reference v2")
			resources = getPRRResources(env.Ctx, env.Namespace, parentPR.Name)
			for i, dir := range subpkgDirs {
				pkgName := "clonee-pkg-" + string(rune('0'+i+1))
				Expect(resources[dir+"/Kptfile"]).To(ContainSubstring("ref: " + pkgName + "/v2"))
			}

			By("publishing parent and copying to v2 workspace")
			publishPackage(env.Ctx, parentPR)
			parentPRV2 := createSubpkgCopy(env, repo, parentPR, "v2")
			DeferCleanup(deletePackage, env.Ctx, parentPRV2)

			By("upgrading all 4 subpackages to v3 in parent v2")
			for i, dir := range subpkgDirs {
				Expect(upgradeSubpackage(env.Ctx, parentPRV2, cloneePRsV2[i].Name, cloneePRsV3[i].Name, dir)).To(Succeed())
				waitForReady(env.Ctx, parentPRV2)
			}

			By("verifying all 4 subpackage Kptfiles reference v3")
			resources = getPRRResources(env.Ctx, env.Namespace, parentPRV2.Name)
			for i, dir := range subpkgDirs {
				pkgName := "clonee-pkg-" + string(rune('0'+i+1))
				Expect(resources[dir+"/Kptfile"]).To(ContainSubstring("ref: " + pkgName + "/v3"))
			}
		})
	})
})

// simpleSubpackageCloneAndUpgrade runs the full clone→upgrade→copy→upgrade scenario
// for a single subpackage in the given dir.
func simpleSubpackageCloneAndUpgrade(env *testEnv, repo, subpackageDir string) {
	createGiteaRepo(repo)
	registerV1Alpha2Repo(env.Ctx, env.Namespace, repo)
	DeferCleanup(func() {
		cleanupRepo(env.Ctx, env.Namespace, repo)
		deleteGiteaRepo(repo)
	})

	cloneePRV1 := createSubpkgPR(env, repo, "clonee-pkg", "v1")
	publishPackage(env.Ctx, cloneePRV1)
	DeferCleanup(deletePackage, env.Ctx, cloneePRV1)

	cloneePRV2 := createSubpkgCopy(env, repo, cloneePRV1, "v2")
	publishPackage(env.Ctx, cloneePRV2)
	DeferCleanup(deletePackage, env.Ctx, cloneePRV2)

	cloneePRV3 := createSubpkgCopy(env, repo, cloneePRV2, "v3")
	publishPackage(env.Ctx, cloneePRV3)
	DeferCleanup(deletePackage, env.Ctx, cloneePRV3)

	parentPR := createSubpkgPR(env, repo, "parent-pkg", "v1")
	DeferCleanup(deletePackage, env.Ctx, parentPR)

	By("cloning subpackage v1 into parent")
	Expect(cloneSubpackage(env.Ctx, parentPR, cloneePRV1.Name, subpackageDir)).To(Succeed())
	waitForReady(env.Ctx, parentPR)

	By("verifying subpackage Kptfile references clonee-pkg/v1")
	expectedName := strings.ReplaceAll(subpackageDir, "/", ".")
	resources := getPRRResources(env.Ctx, env.Namespace, parentPR.Name)
	Expect(resources[subpackageDir+"/Kptfile"]).To(ContainSubstring("name: " + expectedName))
	Expect(resources[subpackageDir+"/Kptfile"]).To(ContainSubstring("ref: clonee-pkg/v1"))

	By("upgrading subpackage from v1 to v2")
	Expect(upgradeSubpackage(env.Ctx, parentPR, cloneePRV1.Name, cloneePRV2.Name, subpackageDir)).To(Succeed())
	waitForReady(env.Ctx, parentPR)

	By("verifying subpackage Kptfile references clonee-pkg/v2")
	resources = getPRRResources(env.Ctx, env.Namespace, parentPR.Name)
	Expect(resources[subpackageDir+"/Kptfile"]).To(ContainSubstring("name: " + expectedName))
	Expect(resources[subpackageDir+"/Kptfile"]).To(ContainSubstring("ref: clonee-pkg/v2"))

	By("publishing parent and copying to v2 workspace")
	publishPackage(env.Ctx, parentPR)
	parentPRV2 := createSubpkgCopy(env, repo, parentPR, "v2")
	DeferCleanup(deletePackage, env.Ctx, parentPRV2)

	By("upgrading subpackage from v2 to v3 in parent v2")
	Expect(upgradeSubpackage(env.Ctx, parentPRV2, cloneePRV2.Name, cloneePRV3.Name, subpackageDir)).To(Succeed())
	waitForReady(env.Ctx, parentPRV2)

	By("verifying subpackage Kptfile references clonee-pkg/v3")
	resources = getPRRResources(env.Ctx, env.Namespace, parentPRV2.Name)
	Expect(resources[subpackageDir+"/Kptfile"]).To(ContainSubstring("name: " + expectedName))
	Expect(resources[subpackageDir+"/Kptfile"]).To(ContainSubstring("ref: clonee-pkg/v3"))
}

// createSubpkgPR creates an init'd PackageRevision for use in subpackage tests.
func createSubpkgPR(env *testEnv, repo, pkgName, workspace string) *porchv1alpha2.PackageRevision {
	pr := newPackageRevision(env.Namespace, repo, pkgName, workspace, withInit(pkgName))
	Expect(k8sClient.Create(env.Ctx, pr)).To(Succeed())
	waitForReady(env.Ctx, pr)
	return pr
}

// createSubpkgCopy copies a published PackageRevision to a new workspace.
func createSubpkgCopy(env *testEnv, repo string, src *porchv1alpha2.PackageRevision, workspace string) *porchv1alpha2.PackageRevision {
	pr := newPackageRevision(env.Namespace, repo, src.Spec.PackageName, workspace, withCopyFrom(src.Name))
	Expect(k8sClient.Create(env.Ctx, pr)).To(Succeed())
	waitForReady(env.Ctx, pr)
	return pr
}

// cloneSubpackage sets SubpackageOperation.CloneFrom on an existing PackageRevision.
func cloneSubpackage(ctx interface{ Done() <-chan struct{} }, pr *porchv1alpha2.PackageRevision, cloneePRName, subpackageDir string) error {
	Expect(k8sClient.Get(sharedCtx, client.ObjectKeyFromObject(pr), pr)).To(Succeed())
	pr.Spec.SubpackageOperation = &porchv1alpha2.SubpackageOperation{
		SubpackageDir: subpackageDir,
		CloneFrom: &porchv1alpha2.UpstreamPackage{
			UpstreamRef: &porchv1alpha2.PackageRevisionRef{
				Name: cloneePRName,
			},
		},
	}
	return k8sClient.Update(sharedCtx, pr)
}

// upgradeSubpackage sets SubpackageOperation.Upgrade on an existing PackageRevision.
func upgradeSubpackage(ctx interface{ Done() <-chan struct{} }, pr *porchv1alpha2.PackageRevision, oldUpstreamName, newUpstreamName, subpackageDir string) error {
	Expect(k8sClient.Get(sharedCtx, client.ObjectKeyFromObject(pr), pr)).To(Succeed())
	pr.Spec.SubpackageOperation = &porchv1alpha2.SubpackageOperation{
		SubpackageDir: subpackageDir,
		Upgrade: &porchv1alpha2.PackageUpgradeSpec{
			OldUpstream: porchv1alpha2.PackageRevisionRef{Name: oldUpstreamName},
			NewUpstream: porchv1alpha2.PackageRevisionRef{Name: newUpstreamName},
		},
	}
	return k8sClient.Update(sharedCtx, pr)
}
