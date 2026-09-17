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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
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

	Context("identical subpackage operation repeated", func() {
		It("should execute the operation only once when the same SubpackageOperation is applied twice", func() {
			repo := "subpkg-idempotent"
			createGiteaRepo(repo)
			registerV1Alpha2Repo(env.Ctx, env.Namespace, repo)
			DeferCleanup(func() {
				cleanupRepo(env.Ctx, env.Namespace, repo)
				deleteGiteaRepo(repo)
			})

			const subpackageDir = "my-subpackage"

			cloneePR := createSubpkgPR(env, repo, "clonee-pkg", "v1")
			publishPackage(env.Ctx, cloneePR)
			DeferCleanup(deletePackage, env.Ctx, cloneePR)

			parentPR := createSubpkgPR(env, repo, "parent-pkg", "v1")
			DeferCleanup(deletePackage, env.Ctx, parentPR)

			By("cloning subpackage into parent (first time)")
			Expect(cloneSubpackage(env.Ctx, parentPR, cloneePR.Name, subpackageDir)).To(Succeed())
			waitForReady(env.Ctx, parentPR)

			By("verifying subpackage Kptfile is present after first clone")
			resources := getPRRResources(env.Ctx, env.Namespace, parentPR.Name)
			Expect(resources).To(HaveKey(subpackageDir + "/Kptfile"))
			filesBefore := len(resources)

			By("applying the identical SubpackageOperation a second time")
			Expect(cloneSubpackage(env.Ctx, parentPR, cloneePR.Name, subpackageDir)).To(Succeed())
			waitForReady(env.Ctx, parentPR)

			By("verifying resources are unchanged — operation was not re-executed")
			resources = getPRRResources(env.Ctx, env.Namespace, parentPR.Name)
			Expect(resources).To(HaveKey(subpackageDir + "/Kptfile"))
			Expect(len(resources)).To(Equal(filesBefore))
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
			Expect(err.Error()).To(ContainSubstring("subpackageDir"))
		})
	})

	Context("clone into existing subpackage rejected", func() {
		It("should reject cloning into a nested subpackage dir", func() {
			repo := "subpkg-clone-nested"
			createGiteaRepo(repo)
			registerV1Alpha2Repo(env.Ctx, env.Namespace, repo)
			DeferCleanup(func() {
				cleanupRepo(env.Ctx, env.Namespace, repo)
				deleteGiteaRepo(repo)
			})

			const (
				subpackageDir1 = "level1/level2/my-subpackage-1"
				subpackageDir2 = "level1/level2/my-subpackage-1/my-subpackage-2"
			)

			cloneePR := createSubpkgPR(env, repo, "clonee-pkg", "v1")
			publishPackage(env.Ctx, cloneePR)
			DeferCleanup(deletePackage, env.Ctx, cloneePR)

			parentPR := createSubpkgPR(env, repo, "parent-pkg", "v1")
			DeferCleanup(deletePackage, env.Ctx, parentPR)

			Expect(cloneSubpackage(env.Ctx, parentPR, cloneePR.Name, subpackageDir1)).To(Succeed())
			waitForReady(env.Ctx, parentPR)

			Expect(cloneSubpackage(env.Ctx, parentPR, cloneePR.Name, subpackageDir2)).To(Succeed())
			waitForReadyFalse(env.Ctx, parentPR)
			Expect(k8sClient.Get(env.Ctx, client.ObjectKeyFromObject(parentPR), parentPR)).To(Succeed())
			Expect(parentPR.Status.Conditions).To(ContainElement(SatisfyAll(
				HaveField("Type", Equal(porchv1alpha2.ConditionReady)),
				HaveField("Status", Equal(metav1.ConditionFalse)),
				HaveField("Message", ContainSubstring("cannot clone subpackage into another subpackage")),
			)))
		})

		It("should reject cloning a different upstream into an occupied dir", func() {
			repo := "subpkg-clone-occupied"
			createGiteaRepo(repo)
			registerV1Alpha2Repo(env.Ctx, env.Namespace, repo)
			DeferCleanup(func() {
				cleanupRepo(env.Ctx, env.Namespace, repo)
				deleteGiteaRepo(repo)
			})

			const subpackageDir = "level1/level2/my-subpackage-1"

			cloneePR := createSubpkgPR(env, repo, "clonee-pkg", "v1")
			publishPackage(env.Ctx, cloneePR)
			DeferCleanup(deletePackage, env.Ctx, cloneePR)

			cloneePR2 := createSubpkgPR(env, repo, "clonee-pkg-2", "v1")
			publishPackage(env.Ctx, cloneePR2)
			DeferCleanup(deletePackage, env.Ctx, cloneePR2)

			parentPR := createSubpkgPR(env, repo, "parent-pkg", "v1")
			DeferCleanup(deletePackage, env.Ctx, parentPR)

			Expect(cloneSubpackage(env.Ctx, parentPR, cloneePR.Name, subpackageDir)).To(Succeed())
			waitForReady(env.Ctx, parentPR)

			Expect(cloneSubpackage(env.Ctx, parentPR, cloneePR2.Name, subpackageDir)).To(Succeed())
			waitForReadyFalse(env.Ctx, parentPR)
			Expect(k8sClient.Get(env.Ctx, client.ObjectKeyFromObject(parentPR), parentPR)).To(Succeed())
			Expect(parentPR.Status.Conditions).To(ContainElement(SatisfyAll(
				HaveField("Type", Equal(porchv1alpha2.ConditionReady)),
				HaveField("Status", Equal(metav1.ConditionFalse)),
				HaveField("Message", ContainSubstring("cannot clone subpackage into parent")),
			)))
		})

		It("should reject cloning with a trailing slash", func() {
			repo := "subpkg-clone-trailing-slash"
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

			err := cloneSubpackage(env.Ctx, parentPR, cloneePR.Name, "level1/level2/my-subpackage-1/")
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
			Expect(upgradeSubpackage(env.Ctx, parentPR, cloneePRV1.Name, cloneePRV2.Name, subpackageDir2)).To(Succeed())
			waitForReadyFalse(env.Ctx, parentPR)
			Expect(k8sClient.Get(env.Ctx, client.ObjectKeyFromObject(parentPR), parentPR)).To(Succeed())
			Expect(parentPR.Status.Conditions).To(ContainElement(SatisfyAll(
				HaveField("Type", Equal(porchv1alpha2.ConditionReady)),
				HaveField("Status", Equal(metav1.ConditionFalse)),
				HaveField("Message", ContainSubstring("does not have a subpackage at")),
			)))

			By("upgrading subpackage in nonexistent sibling dir fails")
			Expect(upgradeSubpackage(env.Ctx, parentPR, cloneePRV1.Name, cloneePRV2.Name, subpackageDir3)).To(Succeed())
			waitForReadyFalse(env.Ctx, parentPR)
			Expect(k8sClient.Get(env.Ctx, client.ObjectKeyFromObject(parentPR), parentPR)).To(Succeed())
			Expect(parentPR.Status.Conditions).To(ContainElement(SatisfyAll(
				HaveField("Type", Equal(porchv1alpha2.ConditionReady)),
				HaveField("Status", Equal(metav1.ConditionFalse)),
				HaveField("Message", ContainSubstring("does not have a subpackage at")),
			)))
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
	Context("modify, rename and remove subpackages via PRR", func() {
		It("should persist modifications, handle rename, and reject upgrade of removed subpackage", func() {
			const (
				subpackageDir1   = "my-subpackage-1"
				subpackageDir2   = "my-subpackage-2"
				subpackageDir3   = "my-subpackage-3"
				renamedSubpkgDir = "renamed-subpackage"
			)
			repo := "subpkg-modify-rename-remove"
			createGiteaRepo(repo)
			registerV1Alpha2Repo(env.Ctx, env.Namespace, repo)
			DeferCleanup(func() {
				cleanupRepo(env.Ctx, env.Namespace, repo)
				deleteGiteaRepo(repo)
			})

			cloneePR1V1 := createSubpkgPR(env, repo, "clonee-pkg-1", "v1")
			publishPackage(env.Ctx, cloneePR1V1)
			DeferCleanup(deletePackage, env.Ctx, cloneePR1V1)
			cloneePR1V2 := createSubpkgCopy(env, repo, cloneePR1V1, "v2")
			publishPackage(env.Ctx, cloneePR1V2)
			DeferCleanup(deletePackage, env.Ctx, cloneePR1V2)

			cloneePR2V1 := createSubpkgPR(env, repo, "clonee-pkg-2", "v1")
			publishPackage(env.Ctx, cloneePR2V1)
			DeferCleanup(deletePackage, env.Ctx, cloneePR2V1)
			cloneePR2V2 := createSubpkgCopy(env, repo, cloneePR2V1, "v2")
			publishPackage(env.Ctx, cloneePR2V2)
			DeferCleanup(deletePackage, env.Ctx, cloneePR2V2)

			cloneePR3V1 := createSubpkgPR(env, repo, "clonee-pkg-3", "v1")
			publishPackage(env.Ctx, cloneePR3V1)
			DeferCleanup(deletePackage, env.Ctx, cloneePR3V1)
			cloneePR3V2 := createSubpkgCopy(env, repo, cloneePR3V1, "v2")
			publishPackage(env.Ctx, cloneePR3V2)
			DeferCleanup(deletePackage, env.Ctx, cloneePR3V2)

			parentPR := createSubpkgPR(env, repo, "parent-pkg", "v1")
			DeferCleanup(deletePackage, env.Ctx, parentPR)

			By("cloning 3 subpackages into parent")
			Expect(cloneSubpackage(env.Ctx, parentPR, cloneePR1V1.Name, subpackageDir1)).To(Succeed())
			waitForReady(env.Ctx, parentPR)
			Expect(cloneSubpackage(env.Ctx, parentPR, cloneePR2V1.Name, subpackageDir2)).To(Succeed())
			waitForReady(env.Ctx, parentPR)
			Expect(cloneSubpackage(env.Ctx, parentPR, cloneePR3V1.Name, subpackageDir3)).To(Succeed())
			waitForReady(env.Ctx, parentPR)

			resources := getPRRResources(env.Ctx, env.Namespace, parentPR.Name)
			Expect(resources).To(HaveKey(subpackageDir1 + "/Kptfile"))
			Expect(resources).To(HaveKey(subpackageDir2 + "/Kptfile"))
			Expect(resources).To(HaveKey(subpackageDir3 + "/Kptfile"))

			By("adding a file to subpackage-1, renaming subpackage-2, removing subpackage-3 via PRR")
			resources[subpackageDir1+"/extra.yaml"] = "# extra"
			for k, v := range resources {
				if strings.HasPrefix(k, subpackageDir2+"/") {
					resources[renamedSubpkgDir+"/"+strings.TrimPrefix(k, subpackageDir2+"/")] = v
					delete(resources, k)
				}
			}
			for k := range resources {
				if strings.HasPrefix(k, subpackageDir3+"/") {
					delete(resources, k)
				}
			}
			updatePRRResources(env.Ctx, env.Namespace, parentPR.Name, resources)
			waitForReady(env.Ctx, parentPR)

			By("verifying modifications persisted")
			resources = getPRRResources(env.Ctx, env.Namespace, parentPR.Name)
			Expect(resources).To(HaveKey(subpackageDir1 + "/extra.yaml"))
			Expect(resources).To(HaveKey(renamedSubpkgDir + "/Kptfile"))
			Expect(resources).NotTo(HaveKey(subpackageDir2 + "/Kptfile"))
			Expect(resources).NotTo(HaveKey(subpackageDir3 + "/Kptfile"))

			By("upgrading subpackage-1 succeeds")
			Expect(upgradeSubpackage(env.Ctx, parentPR, cloneePR1V1.Name, cloneePR1V2.Name, subpackageDir1)).To(Succeed())
			waitForReady(env.Ctx, parentPR)

			By("upgrading renamed subpackage succeeds")
			Expect(upgradeSubpackage(env.Ctx, parentPR, cloneePR2V1.Name, cloneePR2V2.Name, renamedSubpkgDir)).To(Succeed())
			waitForReady(env.Ctx, parentPR)

			By("upgrading removed subpackage-2 fails")
			Expect(upgradeSubpackage(env.Ctx, parentPR, cloneePR2V1.Name, cloneePR2V2.Name, subpackageDir2)).To(Succeed())
			waitForReadyFalse(env.Ctx, parentPR)
			Expect(k8sClient.Get(env.Ctx, client.ObjectKeyFromObject(parentPR), parentPR)).To(Succeed())
			Expect(parentPR.Status.Conditions).To(ContainElement(SatisfyAll(
				HaveField("Type", Equal(porchv1alpha2.ConditionReady)),
				HaveField("Status", Equal(metav1.ConditionFalse)),
				HaveField("Message", ContainSubstring("does not have a subpackage at")),
			)))

			By("upgrading removed subpackage-3 fails")
			Expect(upgradeSubpackage(env.Ctx, parentPR, cloneePR3V1.Name, cloneePR3V2.Name, subpackageDir3)).To(Succeed())
			waitForReadyFalse(env.Ctx, parentPR)
			Expect(k8sClient.Get(env.Ctx, client.ObjectKeyFromObject(parentPR), parentPR)).To(Succeed())
			Expect(parentPR.Status.Conditions).To(ContainElement(SatisfyAll(
				HaveField("Type", Equal(porchv1alpha2.ConditionReady)),
				HaveField("Status", Equal(metav1.ConditionFalse)),
				HaveField("Message", ContainSubstring("does not have a subpackage at")),
			)))
		})
	})

})

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
	Expect(resources[subpackageDir+"/Kptfile"]).NotTo(ContainSubstring("status:"))

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
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := k8sClient.Get(sharedCtx, client.ObjectKeyFromObject(pr), pr); err != nil {
			return err
		}
		pr.Spec.SubpackageOperation = &porchv1alpha2.SubpackageOperation{
			SubpackageDir: subpackageDir,
			CloneFrom: &porchv1alpha2.UpstreamPackage{
				UpstreamRef: &porchv1alpha2.PackageRevisionRef{
					Name: cloneePRName,
				},
			},
		}
		return k8sClient.Update(sharedCtx, pr)
	})
}

// upgradeSubpackage sets SubpackageOperation.Upgrade on an existing PackageRevision.
func upgradeSubpackage(ctx interface{ Done() <-chan struct{} }, pr *porchv1alpha2.PackageRevision, oldUpstreamName, newUpstreamName, subpackageDir string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := k8sClient.Get(sharedCtx, client.ObjectKeyFromObject(pr), pr); err != nil {
			return err
		}
		pr.Spec.SubpackageOperation = &porchv1alpha2.SubpackageOperation{
			SubpackageDir: subpackageDir,
			Upgrade: &porchv1alpha2.PackageUpgradeSpec{
				OldUpstream:    porchv1alpha2.PackageRevisionRef{Name: oldUpstreamName},
				NewUpstream:    porchv1alpha2.PackageRevisionRef{Name: newUpstreamName},
				CurrentPackage: porchv1alpha2.PackageRevisionRef{Name: pr.Name},
			},
		}
		return k8sClient.Update(sharedCtx, pr)
	})
}
