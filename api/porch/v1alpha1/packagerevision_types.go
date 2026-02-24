// Copyright 2022-2026 The kpt and Nephio Authors
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +==============================================================================+
// |                          PackageRevision Resource                            |
// +==============================================================================+

// PackageRevision
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true
// +genclient:method=UpdateApproval,verb=update,subresource=approval,input=github.com/nephio-project/porch/api/porch/v1alpha1.PackageRevision,result=github.com/nephio-project/porch/api/porch/v1alpha1.PackageRevision
type PackageRevision struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PackageRevisionSpec   `json:"spec,omitempty"`
	Status PackageRevisionStatus `json:"status,omitempty"`
}

// PackageRevisionList
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type PackageRevisionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []PackageRevision `json:"items"`
}

// Key and value of the latest package revision label
const (
	LatestPackageRevisionKey   = "kpt.dev/latest-revision"
	LatestPackageRevisionValue = "true"
)

// Field selectors for PackageRevision
type PkgRevFieldSelector string

const (
	PkgRevSelectorName          PkgRevFieldSelector = "metadata.name"
	PkgRevSelectorNamespace     PkgRevFieldSelector = "metadata.namespace"
	PkgRevSelectorRevision      PkgRevFieldSelector = "spec.revision"
	PkgRevSelectorPackageName   PkgRevFieldSelector = "spec.packageName"
	PkgRevSelectorRepository    PkgRevFieldSelector = "spec.repository"
	PkgRevSelectorWorkspaceName PkgRevFieldSelector = "spec.workspaceName"
	PkgRevSelectorLifecycle     PkgRevFieldSelector = "spec.lifecycle"
)

var PackageRevisionSelectableFields = []PkgRevFieldSelector{
	PkgRevSelectorName,
	PkgRevSelectorNamespace,
	PkgRevSelectorRevision,
	PkgRevSelectorPackageName,
	PkgRevSelectorRepository,
	PkgRevSelectorWorkspaceName,
	PkgRevSelectorLifecycle,
}

// +==============================================================================+
// |                         PackageRevision Spec Types                           |
// +==============================================================================+

// PackageRevisionSpec defines the desired state of PackageRevision
type PackageRevisionSpec struct {
	// PackageName identifies the package in the repository.
	PackageName string `json:"packageName,omitempty"`

	// RepositoryName is the name of the Repository object containing this package.
	RepositoryName string `json:"repository,omitempty"`

	// WorkspaceName is a short, unique description of the changes contained in this package revision.
	WorkspaceName string `json:"workspaceName,omitempty"`

	// Revision identifies the version of the package.
	Revision int `json:"revision,omitempty"`

	// Deprecated. Parent references a package that provides resources to us
	Parent *ParentReference `json:"parent,omitempty"`

	Lifecycle PackageRevisionLifecycle `json:"lifecycle,omitempty"`

	// The task slice holds zero or more tasks that describe the operations
	// performed on the packagerevision. The are essentially a replayable history
	// of the packagerevision,
	//
	// Packagerevisions that were not created in Porch may have an
	// empty task list.
	//
	// Packagerevisions created and managed through Porch will always
	// have either an Init, Edit, or a Clone task as the first entry in their
	// task list. This represent packagerevisions created from scratch, based
	// a copy of a different revision in the same package, or a packagerevision
	// cloned from another package.
	// Each change to the packagerevision will result in a correspondig
	// task being added to the list of tasks. It will describe the operation
	// performed and will have a corresponding entry (commit or layer) in git
	// or oci.
	// The task slice describes the history of the packagerevision, so it
	// is an append only list (We might introduce some kind of compaction in the
	// future to keep the number of tasks at a reasonable number).
	Tasks []Task `json:"tasks,omitempty"`

	ReadinessGates []ReadinessGate `json:"readinessGates,omitempty"`

	PackageMetadata *PackageMetadata `json:"packageMetadata,omitempty"`
}

type PackageRevisionLifecycle string

const (
	PackageRevisionLifecycleDraft            PackageRevisionLifecycle = "Draft"
	PackageRevisionLifecycleProposed         PackageRevisionLifecycle = "Proposed"
	PackageRevisionLifecyclePublished        PackageRevisionLifecycle = "Published"
	PackageRevisionLifecycleDeletionProposed PackageRevisionLifecycle = "DeletionProposed"
)

type PackageMetadata struct {
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type ReadinessGate struct {
	ConditionType string `json:"conditionType,omitempty"`
}

// Deprecated. ParentReference is a reference to a parent package
type ParentReference struct {
	// TODO: Should this be a revision or a package?

	// Name is the name of the parent PackageRevision
	Name string `json:"name"`
}

// +==============================================================================+
// |                              Task Types                                      |
// +==============================================================================+

type Task struct {
	Type    TaskType                `json:"type"`
	Init    *PackageInitTaskSpec    `json:"init,omitempty"`
	Clone   *PackageCloneTaskSpec   `json:"clone,omitempty"`
	Edit    *PackageEditTaskSpec    `json:"edit,omitempty"`
	Upgrade *PackageUpgradeTaskSpec `json:"upgrade,omitempty"`
}

type TaskType string

const (
	TaskTypeInit    TaskType = "init"
	TaskTypeClone   TaskType = "clone"
	TaskTypeEdit    TaskType = "edit"
	TaskTypeUpgrade TaskType = "upgrade"
	TaskTypeRender  TaskType = "render"
	TaskTypePush    TaskType = "push"
	TaskTypeNone    TaskType = ""
)

// PackageInitTaskSpec defines the package initialization task.
type PackageInitTaskSpec struct {
	// `Subpackage` is a directory path to a subpackage to initialize. If unspecified, the main package will be initialized.
	Subpackage string `json:"subpackage,omitempty"`
	// `Description` is a short description of the package.
	Description string `json:"description,omitempty"`
	// `Keywords` is a list of keywords describing the package.
	Keywords []string `json:"keywords,omitempty"`
	// `Site` is a link to page with information about the package.
	Site string `json:"site,omitempty"`
}

type PackageCloneTaskSpec struct {
	// // `Subpackage` is a path to a directory where to clone the upstream package.
	// Subpackage string `json:"subpackage,omitempty"`

	// `Upstream` is the reference to the upstream package to clone.
	Upstream UpstreamPackage `json:"upstreamRef,omitempty"`
}

type PackageEditTaskSpec struct {
	Source *PackageRevisionRef `json:"sourceRef,omitempty"`
}

type PackageUpgradeTaskSpec struct {
	// `OldUpstream` is the reference to the original upstream package revision that is
	// the common ancestor of the local package and the new upstream package revision.
	OldUpstream PackageRevisionRef `json:"oldUpstreamRef,omitempty"`

	// `NewUpstream` is the reference to the new upstream package revision that the
	// local package will be upgraded to.
	NewUpstream PackageRevisionRef `json:"newUpstreamRef,omitempty"`

	// `LocalPackageRevisionRef` is the reference to the local package revision that
	// contains all the local changes on top of the `OldUpstream` package revision.
	LocalPackageRevisionRef PackageRevisionRef `json:"localPackageRevisionRef,omitempty"`

	// 	Defines which strategy should be used to update the package. It defaults to 'resource-merge'.
	//  * resource-merge: Perform a structural comparison of the original /
	//    updated resources, and merge the changes into the local package.
	//  * fast-forward: Fail without updating if the local package was modified
	//    since it was fetched.
	//  * force-delete-replace: Wipe all the local changes to the package and replace
	//    it with the remote version.
	//  * copy-merge: Copy all the remote changes to the local package.
	Strategy PackageMergeStrategy `json:"strategy,omitempty"`
}

type PackageMergeStrategy string

const (
	ResourceMerge      PackageMergeStrategy = "resource-merge"
	FastForward        PackageMergeStrategy = "fast-forward"
	ForceDeleteReplace PackageMergeStrategy = "force-delete-replace"
	CopyMerge          PackageMergeStrategy = "copy-merge"
)

// +==============================================================================+
// |                        PackageRevision Status Types                          |
// +==============================================================================+

// PackageRevisionStatus defines the observed state of PackageRevision
type PackageRevisionStatus struct {
	// UpstreamLock identifies the upstream data for this package.
	UpstreamLock *Locator `json:"upstreamLock,omitempty"`

	// SelfLock identifies the location of the current package's data
	SelfLock *Locator `json:"selfLock,omitempty"`

	// PublishedBy is the identity of the user who approved the packagerevision.
	PublishedBy string `json:"publishedBy,omitempty"`

	// PublishedAt is the time when the packagerevision were approved.
	PublishedAt metav1.Time `json:"publishTimestamp,omitempty"`

	// Deployment is true if this is a deployment package (in a deployment repository).
	Deployment bool `json:"deployment,omitempty"`

	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// The following types (Locator, OriginType, and GitLock) are duplicates from the kpt library.
// While kpt issue #3297 is resolved and no cyclic dependency exists, we intentionally keep
// these duplicates to maintain API independence and semantic clarity.

type OriginType string

// Locator is a resolved locator for the last fetch of the package.
type Locator struct {
	// Type is the type of origin.
	Type OriginType `json:"type,omitempty"`

	// Git is the resolved locator for a package on Git.
	Git *GitLock `json:"git,omitempty"`
}

// GitLock is the resolved locator for a package on Git.
type GitLock struct {
	// Repo is the git repository that was fetched.
	// e.g. 'https://github.com/kubernetes/examples.git'
	Repo string `json:"repo,omitempty"`

	// Directory is the sub directory of the git repository that was fetched.
	// e.g. 'staging/cockroachdb'
	Directory string `json:"directory,omitempty"`

	// Ref can be a Git branch, tag, or a commit SHA-1 that was fetched.
	// e.g. 'master'
	Ref string `json:"ref,omitempty"`

	// Commit is the SHA-1 for the last fetch of the package.
	// This is set by kpt for bookkeeping purposes.
	Commit string `json:"commit,omitempty"`
}

type TaskResult struct {
	Task         *Task         `json:"task"`
	RenderStatus *RenderStatus `json:"renderStatus,omitempty"`
}

// RenderStatus represents the result of performing render operation
// on a package resources.
type RenderStatus struct {
	Result ResultList `json:"result,omitempty"`
	Err    string     `json:"error"`
}
