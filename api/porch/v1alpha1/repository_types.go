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

type RepositoryType string

const (
	RepositoryTypeGit RepositoryType = "git"
	RepositoryTypeOCI RepositoryType = "oci"
)

// UpstreamRepository repository may be specified directly or by referencing another Repository resource.
type UpstreamPackage struct {
	// Type of the repository (i.e. git). If empty, `upstreamRef` will be used.
	Type RepositoryType `json:"type,omitempty"`

	// Git upstream package specification. Required if `type` is `git`. Must be unspecified if `type` is not `git`.
	Git *GitPackage `json:"git,omitempty"`

	// OCI upstream package specification. Required if `type` is `oci`. Must be unspecified if `type` is not `oci`.
	Oci *OciPackage `json:"oci,omitempty"`

	// UpstreamRef is the reference to the package from a registered repository rather than external package.
	UpstreamRef *PackageRevisionRef `json:"upstreamRef,omitempty"`
}

type GitPackage struct {
	// Address of the Git repository, for example:
	//   `https://github.com/GoogleCloudPlatform/blueprints.git`
	Repo string `json:"repo"`

	// `Ref` is the git ref containing the package. Ref can be a branch, tag, or commit SHA.
	Ref string `json:"ref"`

	// Directory within the Git repository where the packages are stored. A subdirectory of this directory containing a Kptfile is considered a package.
	Directory string `json:"directory"`

	// Reference to secret containing authentication credentials. Optional.
	SecretRef SecretRef `json:"secretRef,omitempty"`
}

type SecretRef struct {
	// Name of the secret. The secret is expected to be located in the same namespace as the resource containing the reference.
	Name string `json:"name"`
}

// OciPackage describes a repository compatible with the Open Container Registry standard.
type OciPackage struct {
	// Image is the address of an OCI image.
	Image string `json:"image"`
}

// PackageRevisionRef is a reference to a package revision.
type PackageRevisionRef struct {
	// `Name` is the name of the referenced PackageRevision resource.
	Name string `json:"name"`
}

// RepositoryRef identifies a reference to a Repository resource.
type RepositoryRef struct {
	// Name of the Repository resource referenced.
	Name string `json:"name"`
}

// Selector corresponds to the `--match-???` set of flags of the `kpt fn eval` command:
// See https://kpt.dev/reference/cli/fn/eval/ for additional information.
type Selector struct {
	// APIVersion of the target resources
	APIVersion string `json:"apiVersion,omitempty"`
	// Kind of the target resources
	Kind string `json:"kind,omitempty"`
	// Name of the target resources
	Name string `json:"name,omitempty"`
	// Namespace of the target resources
	Namespace string `json:"namespace,omitempty"`
}
