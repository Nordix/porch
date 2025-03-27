// Copyright 2022, 2024-2025 The kpt and Nephio Authors
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

package git

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/nephio-project/porch/api/porch/v1alpha1"
	configapi "github.com/nephio-project/porch/api/porchconfig/v1alpha1"
	"github.com/nephio-project/porch/pkg/errors"
	externalrepotypes "github.com/nephio-project/porch/pkg/externalrepo/types"
	kptfilev1 "github.com/nephio-project/porch/pkg/kpt/api/kptfile/v1"
	"github.com/nephio-project/porch/pkg/repository"
	"github.com/nephio-project/porch/pkg/util"
	pkgerrors "github.com/pkg/errors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/klog/v2"
)

var tracer = otel.Tracer("git")

const (
	DefaultMainReferenceName plumbing.ReferenceName = "refs/heads/main"
	OriginName               string                 = "origin"
)

type GitRepository interface {
	repository.Repository
	GetPackageRevision(ctx context.Context, ref, path string) (repository.PackageRevision, kptfilev1.GitLock, error)
	UpdateDeletionProposedCache() error
}

//go:generate go run golang.org/x/tools/cmd/stringer -type=MainBranchStrategy -linecomment
type MainBranchStrategy int

const (
	ErrorIfMissing   MainBranchStrategy = iota // ErrorIsMissing
	CreateIfMissing                            // CreateIfMissing
	SkipVerification                           // SkipVerification
)

type GitRepositoryOptions struct {
	externalrepotypes.ExternalRepoOptions
	MainBranchStrategy MainBranchStrategy
}

type gitUserInfoProvider struct {
	Author, Email string
}

func (prov *gitUserInfoProvider) GetUserInfo(context.Context) *repository.UserInfo {
	return &repository.UserInfo{
		Email: prov.Email,
		Name:  prov.Author,
	}
}

func OpenRepository(ctx context.Context, name, namespace string, spec *configapi.GitRepository, deployment bool, root string, opts GitRepositoryOptions) (GitRepository, error) {
	ctx, span := tracer.Start(ctx, "git.go::OpenRepository", trace.WithAttributes())
	defer span.End()

	replace := strings.NewReplacer("/", "-", ":", "-")
	dir := filepath.Join(root, replace.Replace(spec.Repo))

	// Cleanup the cache directory in case initialization fails.
	cleanup := dir
	defer func() {
		if cleanup != "" {
			os.RemoveAll(cleanup)
		}
	}()

	var repo *git.Repository

	if fi, err := os.Stat(dir); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}

		r, err := initEmptyRepository(dir)
		if err != nil {
			return nil, fmt.Errorf("error cloning git repository %q: %w", spec.Repo, err)
		}

		repo = r
	} else if !fi.IsDir() {
		// Internal error - corrupted cache. We will cleanup on the way out.
		return nil, fmt.Errorf("cannot clone git repository %q: %w", spec.Repo, err)
	} else {
		cleanup = "" // Existing directory; do not delete it.

		r, err := openRepository(dir)
		if err != nil {
			return nil, err
		}

		repo = r
	}

	// Create Remote
	if err := initializeOrigin(repo, spec.Repo); err != nil {
		return nil, fmt.Errorf("error cloning git repository %q, cannot create remote: %v", spec.Repo, err)
	}

	// NOTE: the spec.git.branch field in the Repository CRD (OpenAPI schema) is defined with
	//		 MinLength=1 validation and its default value is set to "main". This means that
	// 		 it should never be empty at this point. The following code is left here as a last resort failsafe.
	branch := MainBranch
	if spec.Branch != "" {
		branch = BranchName(spec.Branch)
	}

	if err := util.ValidateDirectoryName(string(branch), false); err != nil {
		return nil, fmt.Errorf("branch name %s invalid: %v", branch, err)
	}

	repository := &gitRepository{
		key: repository.RepositoryKey{
			Name:              name,
			Namespace:         namespace,
			Path:              strings.Trim(spec.Directory, "/"),
			PlaceholderWSname: string(branch),
		},
		repo:               repo,
		branch:             branch,
		secret:             spec.SecretRef.Name,
		credentialResolver: opts.ExternalRepoOptions.CredentialResolver,
		userInfoProvider:   makeUserInfoProvider(spec, opts.ExternalRepoOptions.UserInfoProvider),
		cacheDir:           dir,
		deployment:         deployment,
	}

	if opts.ExternalRepoOptions.UseUserDefinedCaBundle {
		if caBundle, err := opts.ExternalRepoOptions.CredentialResolver.ResolveCredential(ctx, namespace, namespace+"-ca-bundle"); err != nil {
			klog.Errorf("failed to obtain caBundle from secret %s/%s: %v", namespace, namespace+"-ca-bundle", err)
		} else {
			repository.caBundle = []byte(caBundle.ToString())
		}
	}
	//CHECK: This is probably needed
	if err := repository.fetchRemoteRepository(ctx); err != nil {
		return nil, err
	}

	if err := repository.verifyRepository(ctx, &opts); err != nil {
		return nil, err
	}

	cleanup = "" // Success. Keep the git directory.

	return repository, nil
}

// makeUserInfoProvider creates a UserInfoProvider based on the provided GitRepository spec.
// If the spec contains no author or email, def will be returned instead.
// If only one of author or email is present, the one present will be used in the other's place.
func makeUserInfoProvider(spec *configapi.GitRepository, def repository.UserInfoProvider) repository.UserInfoProvider {
	if spec.Author != "" || spec.Email != "" {
		ui := &gitUserInfoProvider{
			Author: spec.Author,
			Email:  spec.Email,
		}
		if ui.Author == "" {
			ui.Author = spec.Email
		}
		return ui
	}
	return def
}

type gitRepository struct {
	key                repository.RepositoryKey
	secret             string     // Name of the k8s Secret resource containing credentials
	branch             BranchName // The main branch from repository registration (defaults to 'main' if unspecified)
	repo               *git.Repository
	credentialResolver repository.CredentialResolver
	userInfoProvider   repository.UserInfoProvider

	// Folder used for the local git cache.
	cacheDir string

	// deployment holds spec.deployment
	// TODO: Better caching here, support repository spec changes
	deployment bool

	// credential contains the information needed to authenticate against
	// a git repository.
	credential repository.Credential

	// deletionProposedCache contains the deletionProposed branches that
	// exist in the repo so that we can easily check them without iterating
	// through all the refs each time
	deletionProposedCache map[BranchName]bool

	mutex sync.Mutex

	// caBundle to use for TLS communication towards git
	caBundle []byte
}

var _ GitRepository = &gitRepository{}
var _ repository.Repository = &gitRepository{}

func (r *gitRepository) Key() repository.RepositoryKey {
	return r.key
}

func (r *gitRepository) Close() error {
	if err := os.RemoveAll(r.cacheDir); err != nil {
		return fmt.Errorf("error cleaning up local git cache for repo %s: %v", r.Key().Name, err)
	}
	return nil
}

// This method figures out the latest version of the local repository (optionally). Optionally it should pull the latest remote repository, as the cache invalidation
// happens when there when the version returned here is not the same as previously.
func (r *gitRepository) Version(ctx context.Context) (string, error) {
	ctx, span := tracer.Start(ctx, "gitRepository::Version", trace.WithAttributes())
	defer span.End()
	r.mutex.Lock()
	defer r.mutex.Unlock()
	//CHECK: This should be optional, as post-draft-close wants to save the new repository version. Contacting the remote at that point is not necessary
	// and can even be harmful because the remote can move on in the meantime.
	if err := r.fetchRemoteRepository(ctx); err != nil {
		return "", err
	}

	refs, err := r.repo.References()
	if err != nil {
		return "", err
	}

	b := bytes.Buffer{}
	for {
		ref, err := refs.Next()
		if err == io.EOF {
			break
		}

		b.WriteString(ref.String())
	}

	hash := sha256.Sum256(b.Bytes())
	return hex.EncodeToString(hash[:]), nil
}

func (r *gitRepository) ListPackages(ctx context.Context, filter repository.ListPackageFilter) ([]repository.Package, error) {
	//nolint:staticcheck
	_, span := tracer.Start(ctx, "gitRepository::ListPackages", trace.WithAttributes())
	defer span.End()

	// TODO
	return nil, fmt.Errorf("ListPackages not yet supported for git repos")
}

func (r *gitRepository) CreatePackage(ctx context.Context, obj *v1alpha1.PorchPackage) (repository.Package, error) {
	//nolint:staticcheck
	_, span := tracer.Start(ctx, "gitRepository::CreatePackage", trace.WithAttributes())
	defer span.End()

	// TODO: Create a 'Package' resource and an initial, empty 'PackageRevision'
	return nil, fmt.Errorf("CreatePackage not yet supported for git repos")
}

func (r *gitRepository) DeletePackage(ctx context.Context, obj repository.Package) error {
	//nolint:staticcheck
	_, span := tracer.Start(ctx, "gitRepository::DeletePackage", trace.WithAttributes())
	defer span.End()

	// TODO: Support package deletion using subresources (similar to the package revision approval flow)
	return fmt.Errorf("DeletePackage not yet supported for git repos")
}

func (r *gitRepository) ListPackageRevisions(ctx context.Context, filter repository.ListPackageRevisionFilter) ([]repository.PackageRevision, error) {
	ctx, span := tracer.Start(ctx, "gitRepository::ListPackageRevisions", trace.WithAttributes())
	defer span.End()

	r.mutex.Lock()
	defer r.mutex.Unlock()

	pkgRevs, err := r.listPackageRevisions(ctx, filter)
	if err != nil {
		return nil, err
	}
	var repoPkgRevs []repository.PackageRevision
	for i := range pkgRevs {
		repoPkgRevs = append(repoPkgRevs, pkgRevs[i])
	}
	return repoPkgRevs, nil
}

func (r *gitRepository) listPackageRevisions(ctx context.Context, filter repository.ListPackageRevisionFilter) ([]*gitPackageRevision, error) {
	ctx, span := tracer.Start(ctx, "gitRepository::listPackageRevisions", trace.WithAttributes())
	defer span.End()
	//CHECK: Is this needed?
	if err := r.fetchRemoteRepository(ctx); err != nil {
		return nil, err
	}

	refs, err := r.repo.References()
	if err != nil {
		return nil, err
	}

	var main *plumbing.Reference
	var drafts []*gitPackageRevision
	var result []*gitPackageRevision

	mainBranch := r.branch.RefInLocal() // Looking for the registered branch

	for ref, err := refs.Next(); err == nil; ref, err = refs.Next() {
		switch name := ref.Name(); {
		case name == mainBranch:
			main = ref
			continue

		case isProposedBranchNameInLocal(ref.Name()), isDraftBranchNameInLocal(ref.Name()):
			klog.Infof("Loading draft from %q", ref.Name())
			draft, err := r.loadDraft(ctx, ref)
			if err != nil {
				return nil, pkgerrors.Wrapf(err, "failed to load package draft %q", name.String())
			}
			if draft != nil {
				drafts = append(drafts, draft)
			} else {
				klog.Warningf("no package draft found for ref %v", ref)
			}
		case isTagInLocalRepo(ref.Name()):
			klog.Infof("Loading tag from %q", ref.Name())
			tagged, err := r.loadTaggedPackage(ctx, ref)
			if err != nil {
				if tagged == nil {
					// this tag is not associated with any package (e.g. could be a release tag)
					klog.Warningf("Failed to load tagged package from ref %q: %s", ref.Name(), err)
					continue
				}
				klog.Warningf("Error loading tagged package from ref %q: %s", ref.Name(), err)
			}
			if tagged != nil && filter.Matches(ctx, tagged) {
				result = append(result, tagged)
			}
		}
	}

	if main != nil {
		// TODO: ignore packages that are unchanged in main branch, compared to a tagged version?
		mainpkgs, err := r.discoverFinalizedPackages(ctx, main)
		if err != nil {
			if len(mainpkgs) == 0 {
				return nil, err
			}
			klog.Warningf("Error discovering finalized packages: %s", err)
		}
		for _, p := range mainpkgs {
			if filter.Matches(ctx, p) {
				result = append(result, p)
			}
		}
	}

	for _, p := range drafts {
		if filter.Matches(ctx, p) {
			result = append(result, p)
		}
	}

	return result, nil
}

func (r *gitRepository) CreatePackageRevision(ctx context.Context, obj *v1alpha1.PackageRevision) (repository.PackageRevisionDraft, error) {
	_, span := tracer.Start(ctx, "gitRepository::CreatePackageRevision", trace.WithAttributes())
	defer span.End()
	r.mutex.Lock()
	defer r.mutex.Unlock()

	var base plumbing.Hash
	refName := r.branch.RefInLocal()
	switch main, err := r.repo.Reference(refName, true); {
	case err == nil:
		base = main.Hash()
	case err == plumbing.ErrReferenceNotFound:
		// reference not found - empty repository. Package draft has no parent commit
	default:
		return nil, fmt.Errorf("error when resolving target branch for the package: %w", err)
	}

	if err := util.ValidPkgRevObjName(r.Key().Name, r.Key().Path, obj.Spec.PackageName, string(obj.Spec.WorkspaceName)); err != nil {
		return nil, fmt.Errorf("failed to create packagerevision: %w", err)
	}

	draftKey := repository.PackageRevisionKey{
		PkgKey:        repository.FromFullPathname(r.Key(), obj.Spec.PackageName),
		WorkspaceName: obj.Spec.WorkspaceName,
	}

	// TODO use git branches to leverage uniqueness
	draftBranchName := createDraftName(draftKey)

	// TODO: This should also create a new 'Package' resource if one does not already exist

	return &gitPackageRevisionDraft{
		prKey:     draftKey,
		repo:      r,
		lifecycle: v1alpha1.PackageRevisionLifecycleDraft,
		updated:   time.Now(),
		base:      nil, // Creating a new package
		tasks:     nil, // Creating a new package
		branch:    draftBranchName,
		commit:    base,
	}, nil
}

func (r *gitRepository) UpdatePackageRevision(ctx context.Context, old repository.PackageRevision) (repository.PackageRevisionDraft, error) {
	ctx, span := tracer.Start(ctx, "gitRepository::UpdatePackageRevision", trace.WithAttributes())
	defer span.End()
	r.mutex.Lock()
	defer r.mutex.Unlock()

	oldGitPackage, ok := old.(*gitPackageRevision)
	if !ok {
		return nil, fmt.Errorf("cannot update non-git package %T", old)
	}

	ref := oldGitPackage.ref
	if ref == nil {
		return nil, fmt.Errorf("cannot update final package")
	}

	head, err := r.repo.Reference(ref.Name(), true)
	if err != nil {
		return nil, fmt.Errorf("cannot find draft package branch %q: %w", ref.Name(), err)
	}

	rev, err := r.loadDraft(ctx, head)
	if err != nil {
		return nil, fmt.Errorf("cannot load draft package: %w", err)
	}
	if rev == nil {
		return nil, fmt.Errorf("cannot load draft package %q (package not found)", ref.Name())
	}

	// Fetch lifecycle directly from the repository rather than from the gitPackageRevision. This makes
	// sure we don't end up requesting the same lock twice.
	lifecycle := r.getLifecycle(oldGitPackage)

	return &gitPackageRevisionDraft{
		prKey:     oldGitPackage.prKey,
		repo:      r,
		lifecycle: lifecycle,
		updated:   rev.updated,
		base:      rev.ref,
		tree:      rev.tree,
		commit:    rev.commit,
		tasks:     rev.tasks,
	}, nil
}

func (r *gitRepository) DeletePackageRevision(ctx context.Context, old repository.PackageRevision) error {
	ctx, span := tracer.Start(ctx, "gitRepository::DeletePackageRevision", trace.WithAttributes())
	defer span.End()
	r.mutex.Lock()
	defer r.mutex.Unlock()

	oldGit, ok := old.(*gitPackageRevision)
	if !ok {
		return fmt.Errorf("cannot delete non-git package: %T", old)
	}

	ref := oldGit.ref
	if ref == nil {
		// This is an internal error. In some rare cases (see GetPackageRevision below) we create
		// package revisions without refs. They should never be returned via the API though.
		return fmt.Errorf("cannot delete package with no ref: %s", oldGit.Key().PkgKey.ToFullPathname())
	}

	// We can only delete packages which have their own ref. Refs that are shared with other packages
	// (main branch, tag that doesn't contain package path in its name, ...) cannot be deleted.

	refSpecs := newPushRefSpecBuilder()

	switch rn := ref.Name(); {
	case rn.IsTag():
		// Delete tag only if it is package-specific.
		name := createFinalTagNameInLocal(oldGit.Key())
		if rn != name {
			return fmt.Errorf("cannot delete package tagged with a tag that is not specific to the package: %s", rn)
		}

		// Delete the tag
		refSpecs.AddRefToDelete(ref)

		// If this revision was proposed for deletion, we need to delete the associated branch.
		if err := r.removeDeletionProposedBranchIfExists(ctx, oldGit.Key()); err != nil {
			return err
		}

	case isDraftBranchNameInLocal(rn), isProposedBranchNameInLocal(rn):
		// PackageRevision is proposed or draft; delete the branch directly.
		refSpecs.AddRefToDelete(ref)

	case isBranchInLocalRepo(rn):
		// Delete package from the branch
		commitHash, err := r.createPackageDeleteCommit(ctx, rn, oldGit)
		if err != nil {
			return err
		}

		// Remove the proposed for deletion branch. We end up here when users
		// try to delete the main branch version of a packagerevision.
		if err := r.removeDeletionProposedBranchIfExists(ctx, oldGit.Key()); err != nil {
			return err
		}

		// Update the reference
		refSpecs.AddRefToPush(commitHash, rn)

	default:
		return fmt.Errorf("cannot delete package with the ref name %s", rn)
	}

	// Update references
	if err := r.pushAndCleanup(ctx, refSpecs); err != nil {
		return fmt.Errorf("failed to update git references: %v", err)
	}

	return nil
}

func (r *gitRepository) removeDeletionProposedBranchIfExists(ctx context.Context, key repository.PackageRevisionKey) error {
	refSpecsForDeletionProposed := newPushRefSpecBuilder()
	deletionProposedBranch := createDeletionProposedName(key)
	refSpecsForDeletionProposed.AddRefToDelete(plumbing.NewHashReference(deletionProposedBranch.RefInLocal(), plumbing.ZeroHash))
	if err := r.pushAndCleanup(ctx, refSpecsForDeletionProposed); err != nil {
		if pkgerrors.Is(err, git.NoErrAlreadyUpToDate) {
			// the deletionProposed branch might not have existed, so we ignore this error
			klog.Warningf("branch %s does not exist", deletionProposedBranch)
		} else {
			klog.Errorf("unexpected error while removing deletionProposed branch: %v", err)
			return err
		}
	}
	delete(r.deletionProposedCache, deletionProposedBranch)

	return nil
}

func (r *gitRepository) GetPackageRevision(ctx context.Context, version, path string) (repository.PackageRevision, kptfilev1.GitLock, error) {
	ctx, span := tracer.Start(ctx, "gitRepository::GetPackageRevision", trace.WithAttributes())
	defer span.End()
	r.mutex.Lock()
	defer r.mutex.Unlock()

	var hash plumbing.Hash

	// Trim leading and trailing slashes
	path = strings.Trim(path, "/")

	// Versions map to gitRepo tags in one of two ways:
	//
	// * directly (tag=version)- but then this means that all packages in the repo must be versioned together.
	// * prefixed (tag=<packageDir/<version>) - solving the co-versioning problem.
	//
	// We have to check both forms when looking up a version.
	refNames := []string{}
	if path != "" {
		refNames = append(refNames, path+"/"+version)
		// HACK: Is this always refs/remotes/origin ?  Is it ever not (i.e. do we need both forms?)
		refNames = append(refNames, "refs/remotes/origin/"+path+"/"+version)
	}
	refNames = append(refNames, version)
	// HACK: Is this always refs/remotes/origin ?  Is it ever not (i.e. do we need both forms?)
	refNames = append(refNames, "refs/remotes/origin/"+version)

	for _, ref := range refNames {
		if resolved, err := r.repo.ResolveRevision(plumbing.Revision(ref)); err != nil {
			if pkgerrors.Is(err, plumbing.ErrReferenceNotFound) {
				continue
			}
			return nil, kptfilev1.GitLock{}, pkgerrors.Wrapf(err, "error resolving git reference %q", ref)
		} else {
			hash = *resolved
			break
		}
	}

	if hash.IsZero() {
		r.dumpAllRefs()

		return nil, kptfilev1.GitLock{}, pkgerrors.Errorf("cannot find git reference (tried %v)", refNames)
	}

	return r.loadPackageRevision(ctx, version, path, hash)
}

func (r *gitRepository) loadPackageRevision(ctx context.Context, version, path string, hash plumbing.Hash) (repository.PackageRevision, kptfilev1.GitLock, error) {
	ctx, span := tracer.Start(ctx, "gitRepository::loadPackageRevision", trace.WithAttributes())
	defer span.End()

	if !packageInDirectory(path, r.Key().Path) {
		return nil, kptfilev1.GitLock{}, fmt.Errorf("cannot find package %s@%s; package is not under the Repository.spec.directory", path, version)
	}

	origin, err := r.repo.Remote("origin")
	if err != nil {
		return nil, kptfilev1.GitLock{}, pkgerrors.Wrap(err, "cannot determine repository origin")
	}

	lock := kptfilev1.GitLock{
		Repo:      origin.Config().URLs[0],
		Directory: path,
		Ref:       version,
	}

	commit, err := r.repo.CommitObject(hash)
	if err != nil {
		return nil, lock, pkgerrors.Wrapf(err, "cannot resolve git reference %s (hash: %s) to commit", version, hash)
	}
	lock.Commit = commit.Hash.String()

	krmPackage, err := r.findPackage(commit, path)
	if err != nil {
		return nil, lock, err
	}

	if krmPackage == nil {
		return nil, lock, pkgerrors.Errorf("cannot find package %s@%s", path, version)
	}

	var ref *plumbing.Reference = nil // Cannot determine ref; this package will be considered final (immutable).

	var revisionStr, workspace string
	last := strings.LastIndex(version, "/")

	if strings.HasPrefix(version, "drafts/") || strings.HasPrefix(version, "proposed/") {
		// the passed in version is a ref to an unpublished package revision
		workspace = version[last+1:]
	} else {
		// the passed in version is a ref to a published package revision
		if version == string(r.branch) || last < 0 {
			revisionStr = version
		} else {
			revisionStr = version[last+1:]
		}
		workspace = getPkgWorkspace(commit, krmPackage, ref)
		if workspace == "" {
			workspace = revisionStr
		}
	}

	packageRevision, err := krmPackage.buildGitPackageRevision(ctx, revisionStr, workspace, ref)
	if err != nil {
		return nil, lock, err
	}
	return packageRevision, lock, nil
}

func (r *gitRepository) discoverFinalizedPackages(ctx context.Context, ref *plumbing.Reference) ([]*gitPackageRevision, error) {
	ctx, span := tracer.Start(ctx, "gitRepository::discoverFinalizedPackages", trace.WithAttributes())
	defer span.End()

	commit, err := r.repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, err
	}

	var revisionStr string
	if rev, ok := getBranchNameInLocalRepo(ref.Name()); ok {
		revisionStr = rev
	} else if rev, ok = getTagNameInLocalRepo(ref.Name()); ok {
		revisionStr = rev
	} else {
		// TODO: ignore the ref instead?
		return nil, pkgerrors.Errorf("cannot determine revision from ref: %q", rev)
	}

	krmPackages, err := r.discoverPackagesInTree(commit, DiscoverPackagesOptions{FilterPrefix: r.Key().Path, Recurse: true})
	if err != nil {
		return nil, err
	}

	ec := errors.NewErrorCollector().WithSeparator(";").WithFormat("{%s}")
	var result []*gitPackageRevision
	for _, krmPackage := range krmPackages.packages {
		workspace := getPkgWorkspace(commit, krmPackage, ref)
		if workspace == "" {
			klog.Warningf("Failed to get workspace name for package %q (will use revision name): %s", krmPackage.pkgKey, err)
		}
		packageRevision, err := krmPackage.buildGitPackageRevision(ctx, revisionStr, workspace, ref)
		if err != nil {
			ec.Add(pkgerrors.Wrapf(err, "failed to build git package revision for package %s", krmPackage.pkgKey))
			continue
		}
		result = append(result, packageRevision)
	}

	return result, ec.Join()
}

// loadDraft will load the draft package.  If the package isn't found (we now require a Kptfile), it will return (nil, nil)
func (r *gitRepository) loadDraft(ctx context.Context, ref *plumbing.Reference) (*gitPackageRevision, error) {
	ctx, span := tracer.Start(ctx, "gitRepository::loadDraft", trace.WithAttributes())
	defer span.End()

	pkgPathAndName, workspaceName, err := parseDraftName(ref)
	if err != nil {
		return nil, err
	}

	// Only load drafts in the directory specified at repository registration.
	if !packageInDirectory(pkgPathAndName, r.Key().Path) {
		return nil, nil
	}

	commit, err := r.repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, pkgerrors.Wrap(err, "cannot resolve draft branch to commit (corrupted repository?)")
	}

	krmPackage, err := r.findPackage(commit, pkgPathAndName)
	if err != nil {
		return nil, err
	}

	if krmPackage == nil {
		klog.Warningf("draft package %q was not found", pkgPathAndName)
		return nil, nil
	}

	packageRevision, err := krmPackage.buildGitPackageRevision(ctx, "0", workspaceName, ref)
	if err != nil {
		return nil, err
	}

	return packageRevision, nil
}

func (r *gitRepository) UpdateDeletionProposedCache() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.updateDeletionProposedCache()
}

func (r *gitRepository) updateDeletionProposedCache() error {
	r.deletionProposedCache = make(map[BranchName]bool)
	//CHECK: Is this needed?
	err := r.fetchRemoteRepository(context.Background())
	if err != nil {
		return err
	}
	refs, err := r.repo.References()
	if err != nil {
		return err
	}

	for {
		ref, err := refs.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			klog.Errorf("error getting next ref: %v", err)
			break
		}

		branch, isDeletionProposedBranch := getdeletionProposedBranchNameInLocal(ref.Name())
		if isDeletionProposedBranch {
			r.deletionProposedCache[deletionProposedPrefix+branch] = true
		}
	}

	return nil
}

func parseDraftName(draft *plumbing.Reference) (pkgPathAndName, workspaceName string, err error) {
	refName := draft.Name()
	var suffix string
	if b, ok := getDraftBranchNameInLocal(refName); ok {
		suffix = string(b)
	} else if b, ok = getProposedBranchNameInLocal(refName); ok {
		suffix = string(b)
	} else {
		return "", "", fmt.Errorf("invalid draft ref name: %q", refName)
	}

	revIndex := strings.LastIndex(suffix, "/")
	if revIndex <= 0 {
		return "", "", fmt.Errorf("invalid draft ref name; missing workspaceName suffix: %q", refName)
	}
	pkgPathAndName, workspaceName = suffix[:revIndex], suffix[revIndex+1:]
	return pkgPathAndName, workspaceName, nil
}

func (r *gitRepository) loadTaggedPackage(ctx context.Context, tag *plumbing.Reference) (*gitPackageRevision, error) {
	ctx, span := tracer.Start(ctx, "gitRepository::loadTaggedPackage", trace.WithAttributes())
	defer span.End()

	name, ok := getTagNameInLocalRepo(tag.Name())
	if !ok {
		return nil, pkgerrors.Errorf("invalid tag ref: %q", tag)
	}
	slash := strings.LastIndex(name, "/")

	if slash < 0 {
		// tag=<version>
		// could be a release tag or something else, we ignore these types of tags
		return nil, pkgerrors.Errorf("could not find slash in %q", name)
	}

	// tag=<package path>/version
	path, revisionStr := name[:slash], name[slash+1:]

	if !packageInDirectory(path, r.Key().Path) {
		return nil, nil
	}

	resolvedHash, err := r.repo.ResolveRevision(plumbing.Revision(tag.Hash().String()))
	if err != nil {
		return nil, pkgerrors.Wrapf(err, "cannot resolve tag %q to git revision", name)
	}
	commit, err := r.repo.CommitObject(*resolvedHash)
	if err != nil {
		return nil, pkgerrors.Wrapf(err, "cannot resolve tag %q (hash: %q) to commit (corrupted repository?)", name, resolvedHash)
	}

	krmPackage, err := r.findPackage(commit, path)
	if err != nil {
		return nil, pkgerrors.Wrapf(err, "skipping %q; cannot find %q (corrupted repository?)", name, path)
	}

	if krmPackage == nil {
		return nil, pkgerrors.Errorf("skipping %q: Kptfile not found", name)
	}

	workspaceName := getPkgWorkspace(commit, krmPackage, tag)
	if workspaceName == "" {
		klog.Warningf("Failed to get package workspace name for package %q (will use revision name)", name)
	}

	packageRevision, err := krmPackage.buildGitPackageRevision(ctx, revisionStr, workspaceName, tag)
	if err != nil {
		if packageRevision == nil {
			return nil, err
		}
		klog.Warningf("Error building git package revision %q: %s", path, err)
	}

	return packageRevision, nil
}

func (r *gitRepository) dumpAllRefs() {
	refs, err := r.repo.References()
	if err != nil {
		klog.Warningf("failed to get references: %v", err)
	} else {
		for {
			ref, err := refs.Next()
			if err != nil {
				if err != io.EOF {
					klog.Warningf("failed to get next reference: %v", err)
				}
				break
			}
			klog.Infof("ref %#v", ref.Name())
		}
	}

	branches, err := r.repo.Branches()
	if err != nil {
		klog.Warningf("failed to get branches: %v", err)
	} else {
		for {
			branch, err := branches.Next()
			if err != nil {
				if err != io.EOF {
					klog.Warningf("failed to get next branch: %v", err)
				}
				break
			}
			klog.Infof("branch %#v", branch.Name())
		}
	}
}

// getAuthMethod fetches the credentials for authenticating to git. It caches the
// credentials between calls and refresh credentials when the tokens have expired.
func (r *gitRepository) getAuthMethod(ctx context.Context, forceRefresh bool) (transport.AuthMethod, error) {
	// If no secret is provided, we try without any auth.
	if r.secret == "" {
		return nil, nil
	}

	if r.credential == nil || !r.credential.Valid() || forceRefresh {
		if cred, err := r.credentialResolver.ResolveCredential(ctx, r.Key().Namespace, r.secret); err != nil {
			return nil, fmt.Errorf("failed to obtain credential from secret %s/%s: %w", r.Key().Namespace, r.secret, err)
		} else {
			r.credential = cred
		}
	}

	return r.credential.ToAuthMethod(), nil
}

func (r *gitRepository) GetRepo() (string, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	origin, err := r.repo.Remote("origin")
	if err != nil {
		return "", fmt.Errorf("cannot determine repository origin: %w", err)
	}

	return origin.Config().URLs[0], nil
}

func (r *gitRepository) fetchRemoteRepository(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "gitRepository::fetchRemoteRepository", trace.WithAttributes())
	defer span.End()

	// Fetch
	switch err := r.doGitWithAuth(ctx, func(auth transport.AuthMethod) error {
		return r.repo.Fetch(&git.FetchOptions{
			RemoteName: OriginName,
			Auth:       auth,
			Prune:      true,
			CABundle:   r.caBundle,
		})
	}); err {
	case nil: // OK
	case git.NoErrAlreadyUpToDate:
	case transport.ErrEmptyRemoteRepository:

	default:
		return fmt.Errorf("cannot fetch repository %s/%s: %w", r.Key().Namespace, r.Key().Name, err)
	}

	return nil
}

// Verifies repository. Repository must be fetched already.
func (r *gitRepository) verifyRepository(ctx context.Context, opts *GitRepositoryOptions) error {
	// When opening a temporary repository, such as for cloning a package
	// from unregistered upstream, we won't be pushing into the remote so
	// we don't need to verify presence of the main branch.
	if opts.MainBranchStrategy == SkipVerification {
		return nil
	}

	if _, err := r.repo.Reference(r.branch.RefInLocal(), false); err != nil {
		switch opts.MainBranchStrategy {
		case ErrorIfMissing:
			return fmt.Errorf("branch %q doesn't exist: %v", r.branch, err)
		case CreateIfMissing:
			klog.Infof("Creating branch %s in repository %s", r.branch, r.Key().Name)
			if err := r.createBranch(ctx, r.branch); err != nil {
				return fmt.Errorf("error creating main branch %q: %v", r.branch, err)
			}
		default:
			return fmt.Errorf("unknown main branch strategy %q", opts.MainBranchStrategy.String())
		}
	}
	return nil
}

const (
	fileContent   = "Created by porch"
	fileName      = "README.md"
	commitMessage = "Initial commit for main branch by porch"
)

// createBranch creates the provided branch by creating a commit containing
// a README.md file on the root of the repo and then pushing it to the branch.
func (r *gitRepository) createBranch(ctx context.Context, branch BranchName) error {
	fileHash, err := r.storeBlob(fileContent)
	if err != nil {
		return err
	}

	tree := &object.Tree{}
	tree.Entries = append(tree.Entries, object.TreeEntry{
		Name: fileName,
		Mode: filemode.Regular,
		Hash: fileHash,
	})

	treeEo := r.repo.Storer.NewEncodedObject()
	if err := tree.Encode(treeEo); err != nil {
		return err
	}

	treeHash, err := r.repo.Storer.SetEncodedObject(treeEo)
	if err != nil {
		return err
	}

	now := time.Now()
	commit := &object.Commit{
		Author: object.Signature{
			Name:  porchSignatureName,
			Email: porchSignatureEmail,
			When:  now,
		},
		Committer: object.Signature{
			Name:  porchSignatureName,
			Email: porchSignatureEmail,
			When:  now,
		},
		Message:  commitMessage,
		TreeHash: treeHash,
	}
	commitHash, err := r.storeCommit(commit)
	if err != nil {
		return err
	}

	refSpecs := newPushRefSpecBuilder()
	refSpecs.AddRefToPush(commitHash, branch.RefInLocal())
	return r.pushAndCleanup(ctx, refSpecs)
}

func (r *gitRepository) getCommit(h plumbing.Hash) (*object.Commit, error) {
	return object.GetCommit(r.repo.Storer, h)
}

func (r *gitRepository) storeCommit(commit *object.Commit) (plumbing.Hash, error) {
	eo := r.repo.Storer.NewEncodedObject()
	if err := commit.Encode(eo); err != nil {
		return plumbing.Hash{}, err
	}
	return r.repo.Storer.SetEncodedObject(eo)
}

// Creates a commit which deletes the package from the branch, and returns its commit hash.
// If the branch doesn't exist, will return zero hash and no error.
func (r *gitRepository) createPackageDeleteCommit(ctx context.Context, branch plumbing.ReferenceName, pkg *gitPackageRevision) (plumbing.Hash, error) {
	var zero plumbing.Hash

	local, err := refInRemoteFromRefInLocal(branch)
	if err != nil {
		return zero, err
	}
	// Fetch the branch
	// TODO: Fetch only as part of conflict resolution & Retry
	switch err := r.doGitWithAuth(ctx, func(auth transport.AuthMethod) error {
		return r.repo.Fetch(&git.FetchOptions{
			RemoteName: OriginName,
			RefSpecs:   []config.RefSpec{config.RefSpec(fmt.Sprintf("+%s:%s", local, branch))},
			Auth:       auth,
			Tags:       git.NoTags,
			CABundle:   r.caBundle,
		})
	}); err {
	case nil, git.NoErrAlreadyUpToDate:
		// ok
	default:
		return zero, fmt.Errorf("failed to fetch remote repository: %w", err)
	}

	// find the branch
	ref, err := r.repo.Reference(branch, true)
	if err != nil {
		// branch doesn't exist, and therefore package doesn't exist either.
		klog.Infof("Branch %q no longer exist, deleting a package from it is unnecessary", branch)
		return zero, nil
	}
	commit, err := r.repo.CommitObject(ref.Hash())
	if err != nil {
		return zero, fmt.Errorf("failed to resolve main branch to commit: %w", err)
	}
	root, err := commit.Tree()
	if err != nil {
		return zero, fmt.Errorf("failed to find commit tree for %s: %w", ref, err)
	}

	// Find the package in the tree
	switch _, err := root.FindEntry(pkg.Key().PkgKey.ToFullPathname()); err {
	case object.ErrEntryNotFound:
		// Package doesn't exist; no need to delete it
		return zero, nil
	case nil:
		// found
	default:
		return zero, fmt.Errorf("failed to find package %q in the repositrory ref %q: %w,", pkg.Key().PkgKey.ToFullPathname(), ref, err)
	}

	// Create commit helper. Use zero hash for the initial package tree. Commit helper will initialize trees
	// without TreeEntry for this package present - the package is deleted.
	ch, err := newCommitHelper(r, r.userInfoProvider, commit.Hash, pkg.Key().PkgKey.ToFullPathname(), zero)
	if err != nil {
		return zero, fmt.Errorf("failed to initialize commit of package %q to %q: %w", pkg.Key().PkgKey.ToFullPathname(), ref, err)
	}

	message := fmt.Sprintf("Delete %s", pkg.Key().PkgKey.ToFullPathname())
	commitHash, _, err := ch.commit(ctx, message, pkg.Key().PkgKey.ToFullPathname())
	if err != nil {
		return zero, fmt.Errorf("failed to commit package %q to %q: %w", pkg.Key().PkgKey.ToFullPathname(), ref, err)
	}
	return commitHash, nil
}

func (r *gitRepository) PushAndCleanup(ctx context.Context, ph *pushRefSpecBuilder) error {
	ctx, span := tracer.Start(ctx, "gitRepository::PushAndCleanup", trace.WithAttributes())
	defer span.End()

	r.mutex.Lock()
	defer r.mutex.Unlock()

	return r.pushAndCleanup(ctx, ph)
}

func (r *gitRepository) pushAndCleanup(ctx context.Context, ph *pushRefSpecBuilder) error {
	ctx, span := tracer.Start(ctx, "gitRepository::pushAndCleanup", trace.WithAttributes())
	defer span.End()

	specs, require, err := ph.BuildRefSpecs()
	if err != nil {
		return err
	}

	klog.Infof("pushing refs: %v", specs)

	if err := r.doGitWithAuth(ctx, func(auth transport.AuthMethod) error {
		//CHECK: Would a push and fail-rebase work here?
		return r.repo.Push(&git.PushOptions{
			RemoteName:        OriginName,
			RefSpecs:          specs,
			Auth:              auth,
			RequireRemoteRefs: require,
			// TODO(justinsb): Need to ensure this is a compare-and-swap
			Force:    true,
			CABundle: r.caBundle,
		})
	}); err != nil {
		return err
	}
	return nil
}

func (r *gitRepository) loadTasks(_ context.Context, startCommit *object.Commit, key repository.PackageRevisionKey) ([]v1alpha1.Task, error) {

	var logOptions = git.LogOptions{
		From:  startCommit.Hash,
		Order: git.LogOrderCommitterTime,
	}

	// NOTE: We don't prune the commits with the filepath; this is because it's a relatively expensive operation,
	// as we have to visit the whole trees.  Visiting the commits is comparatively fast.
	// // Prune the commits we visit a bit - though the actual gate is on the gitAnnotation
	// if packagePath != "" {
	// 	if !strings.HasSuffix(packagePath, "/") {
	// 		packagePath += "/"
	// 	}
	// 	pathFilter := func(p string) bool {
	// 		matchesPackage := strings.HasPrefix(p, packagePath)
	// 		return matchesPackage
	// 	}
	// 	logOptions.PathFilter = pathFilter
	// }

	commits, err := r.repo.Log(&logOptions)
	if err != nil {
		return nil, pkgerrors.Wrap(err, "error walking commits")
	}

	var tasks []v1alpha1.Task

	done := false
	visitCommit := func(commit *object.Commit) error {
		if done {
			return nil
		}

		gitAnnotations, err := ExtractGitAnnotations(commit)
		if err != nil {
			klog.Warningf("Error extracting (some) git annotations from commit %q: %s", commit.Hash, err)
		}

		for _, gitAnnotation := range gitAnnotations {
			packageMatches := gitAnnotation.PackagePath == key.PkgKey.ToFullPathname()
			workspaceNameMatches := gitAnnotation.WorkspaceName == key.WorkspaceName ||
				// this is needed for porch package revisions created before the workspaceName field existed
				(gitAnnotation.Revision == string(key.WorkspaceName) && gitAnnotation.WorkspaceName == "")

			if packageMatches && workspaceNameMatches {
				// We are iterating through the commits in reverse order.
				// Tasks that are read from separate commits will be recorded in
				// reverse order.
				// The entire `tasks` slice will get reversed later, which will give us the
				// tasks in chronological order.
				if gitAnnotation.Task != nil {
					tasks = append(tasks, *gitAnnotation.Task)
				}

				if gitAnnotation.Task != nil && (gitAnnotation.Task.Type == v1alpha1.TaskTypeClone || gitAnnotation.Task.Type == v1alpha1.TaskTypeInit) {
					// we have reached the beginning of this package revision and don't need to
					// continue further
					done = true
					break
				}
			}
		}

		// TODO: If a commit has no annotations defined, we should treat it like a patch.
		// This will allow direct manipulation of the git repo.
		// We should also probably _not_ record an annotation for a patch task, so we
		// can allow direct editing.
		return nil
	}

	err = visitCommitsCollectErrors(commits, visitCommit)

	// We need to reverse the tasks so they appear in chronological order
	util.SafeReverse(tasks)

	return tasks, pkgerrors.Wrapf(err, "errors loading tasks")
}

func visitCommitsCollectErrors(iterator object.CommitIter, callback commitCallback) error {
	ec := errors.NewErrorCollector().WithSeparator(";").WithFormat("{%s}")
	for c, itErr := iterator.Next(); itErr == nil; c, itErr = iterator.Next() {
		err := callback(c)
		if err != nil {
			if pkgerrors.Is(err, storer.ErrStop) {
				break
			}
			ec.Add(pkgerrors.Wrapf(err, "error visiting commit %s", c.Hash))
		}
	}

	return ec.Join()
}

func (r *gitRepository) GetResources(hash plumbing.Hash) (map[string]string, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	resources := map[string]string{}

	tree, err := r.repo.TreeObject(hash)
	if err == nil {
		// Files() iterator iterates recursively over all files in the tree.
		fit := tree.Files()
		defer fit.Close()
		for {
			file, err := fit.Next()
			if err == io.EOF {
				break
			} else if err != nil {
				return nil, fmt.Errorf("failed to load package resources: %w", err)
			}

			content, err := file.Contents()
			if err != nil {
				return nil, fmt.Errorf("failed to read package file contents: %q, %w", file.Name, err)
			}

			// TODO: decide whether paths should include package directory or not.
			resources[file.Name] = content
			//resources[path.Join(p.path, file.Name)] = content
		}
	}
	return resources, nil
}

// findLatestPackageCommit returns the latest commit from the history that pertains
// to the package given by the packagePath. If no commit is found, it will return nil and an error.
func (r *gitRepository) findLatestPackageCommit(startCommit *object.Commit, key repository.PackageKey) (*object.Commit, error) {
	var logOptions = git.LogOptions{
		From:  startCommit.Hash,
		Order: git.LogOrderCommitterTime,
	}

	commits, err := r.repo.Log(&logOptions)
	if err != nil {
		return nil, pkgerrors.Wrap(err, "error walking commits")
	}

	for c, err := commits.Next(); err == nil; c, err = commits.Next() {
		gitAnnotations, err := ExtractGitAnnotations(c)
		if err != nil {
			klog.Warningf("Error extracting (some) annotations from commit %q: %s", c.Hash, err)
		}

		for _, ann := range gitAnnotations {
			if ann.PackagePath == key.ToFullPathname() {
				return c, nil
			}
		}
	}

	return nil, pkgerrors.Errorf("could not find latest commit for package %s", key.ToFullPathname())
}

func (r *gitRepository) blobObject(h plumbing.Hash) (*object.Blob, error) {
	return r.repo.BlobObject(h)
}

// commitCallback is the function type that needs to be provided to the history iterator functions.
type commitCallback func(*object.Commit) error

// StoreBlob is a helper method to write a blob to the git store.
func (r *gitRepository) storeBlob(value string) (plumbing.Hash, error) {
	data := []byte(value)
	eo := r.repo.Storer.NewEncodedObject()
	eo.SetType(plumbing.BlobObject)
	eo.SetSize(int64(len(data)))

	w, err := eo.Writer()
	if err != nil {
		return plumbing.Hash{}, err
	}

	if _, err := w.Write(data); err != nil {
		w.Close()
		return plumbing.Hash{}, err
	}

	if err := w.Close(); err != nil {
		return plumbing.Hash{}, err
	}

	return r.repo.Storer.SetEncodedObject(eo)
}

func (r *gitRepository) getTree(h plumbing.Hash) (*object.Tree, error) {
	return object.GetTree(r.repo.Storer, h)
}

func (r *gitRepository) storeTree(tree *object.Tree) (plumbing.Hash, error) {
	eo := r.repo.Storer.NewEncodedObject()
	if err := tree.Encode(eo); err != nil {
		return plumbing.Hash{}, err
	}

	treeHash, err := r.repo.Storer.SetEncodedObject(eo)
	if err != nil {
		return plumbing.Hash{}, err
	}
	return treeHash, nil
}

func (r *gitRepository) GetLifecycle(ctx context.Context, pkgRev *gitPackageRevision) v1alpha1.PackageRevisionLifecycle {
	_, span := tracer.Start(ctx, "gitRepository::GetLifecycle", trace.WithAttributes())
	defer span.End()
	r.mutex.Lock()
	defer r.mutex.Unlock()

	return r.getLifecycle(pkgRev)
}

func (r *gitRepository) getLifecycle(pkgRev *gitPackageRevision) v1alpha1.PackageRevisionLifecycle {
	switch ref := pkgRev.ref; {
	case ref == nil:
		return r.checkPublishedLifecycle(pkgRev)
	case isDraftBranchNameInLocal(ref.Name()):
		return v1alpha1.PackageRevisionLifecycleDraft
	case isProposedBranchNameInLocal(ref.Name()):
		return v1alpha1.PackageRevisionLifecycleProposed
	default:
		return r.checkPublishedLifecycle(pkgRev)
	}
}

func (r *gitRepository) checkPublishedLifecycle(pkgRev *gitPackageRevision) v1alpha1.PackageRevisionLifecycle {
	if r.deletionProposedCache == nil {
		if err := r.updateDeletionProposedCache(); err != nil {
			klog.Errorf("failed to update deletionProposed cache: %v", err)
			return v1alpha1.PackageRevisionLifecyclePublished
		}
	}

	branchName := createDeletionProposedName(pkgRev.Key())
	if _, found := r.deletionProposedCache[branchName]; found {
		return v1alpha1.PackageRevisionLifecycleDeletionProposed
	}

	return v1alpha1.PackageRevisionLifecyclePublished
}

func (r *gitRepository) UpdateLifecycle(ctx context.Context, pkgRev *gitPackageRevision, newLifecycle v1alpha1.PackageRevisionLifecycle) error {
	ctx, span := tracer.Start(ctx, "gitRepository::UpdateLifecycle", trace.WithAttributes())
	defer span.End()

	r.mutex.Lock()
	defer r.mutex.Unlock()

	old := r.getLifecycle(pkgRev)
	if !v1alpha1.LifecycleIsPublished(old) {
		return fmt.Errorf("cannot update lifecycle for draft package revision")
	}
	refSpecs := newPushRefSpecBuilder()
	deletionProposedBranch := createDeletionProposedName(pkgRev.Key())

	if old == v1alpha1.PackageRevisionLifecyclePublished {
		if newLifecycle != v1alpha1.PackageRevisionLifecycleDeletionProposed {
			return fmt.Errorf("invalid new lifecycle value: %q", newLifecycle)
		}
		// Push the package revision into a deletionProposed branch.
		r.deletionProposedCache[deletionProposedBranch] = true
		refSpecs.AddRefToPush(pkgRev.commit, deletionProposedBranch.RefInLocal())
	}
	if old == v1alpha1.PackageRevisionLifecycleDeletionProposed {
		if newLifecycle != v1alpha1.PackageRevisionLifecyclePublished {
			return fmt.Errorf("invalid new lifecycle value: %q", newLifecycle)
		}

		// Delete the deletionProposed branch
		delete(r.deletionProposedCache, deletionProposedBranch)
		ref := plumbing.NewHashReference(deletionProposedBranch.RefInLocal(), pkgRev.commit)
		refSpecs.AddRefToDelete(ref)
	}

	if err := r.pushAndCleanup(ctx, refSpecs); err != nil {
		if !pkgerrors.Is(err, git.NoErrAlreadyUpToDate) {
			return err
		}
	}

	return nil
}

func (r *gitRepository) UpdateDraftResources(ctx context.Context, draft *gitPackageRevisionDraft, new *v1alpha1.PackageRevisionResources, change *v1alpha1.Task) error {
	ctx, span := tracer.Start(ctx, "gitRepository::UpdateResources", trace.WithAttributes())
	defer span.End()
	r.mutex.Lock()
	defer r.mutex.Unlock()

	ch, err := newCommitHelper(r, r.userInfoProvider, draft.commit, draft.Key().PkgKey.ToFullPathname(), plumbing.ZeroHash)
	if err != nil {
		return pkgerrors.Wrap(err, "failed to commit package:")
	}

	for k, v := range new.Spec.Resources {
		if err := ch.storeFile(filepath.Join(draft.Key().PkgKey.ToFullPathname(), k), v); err != nil {
			return err
		}
	}

	// Because we can't read the package back without a Kptfile, make sure one is present
	{
		p := filepath.Join(draft.Key().PkgKey.ToFullPathname(), "Kptfile")
		_, err := ch.readFile(p)
		if os.IsNotExist(err) {
			// We could write the file here; currently we return an error
			return pkgerrors.Wrap(err, "package must contain Kptfile at root")
		}
	}

	annotation := &gitAnnotation{
		PackagePath:   draft.Key().PkgKey.ToFullPathname(),
		WorkspaceName: draft.Key().WorkspaceName,
		Revision:      repository.Revision2Str(draft.Key().Revision),
		Task:          change,
	}
	message := "Intermediate commit"
	if change != nil {
		message += fmt.Sprintf(": %s", change.Type)
		draft.tasks = append(draft.tasks, *change)
	}
	message += "\n"

	message, err = AnnotateCommitMessage(message, annotation)
	if err != nil {
		return err
	}

	commitHash, packageTree, err := ch.commit(ctx, message, draft.Key().PkgKey.ToFullPathname())
	if err != nil {
		return pkgerrors.Wrap(err, "failed to commit package: %w")
	}

	draft.tree = packageTree
	draft.commit = commitHash
	return nil
}

func (r *gitRepository) ClosePackageRevisionDraft(ctx context.Context, prd repository.PackageRevisionDraft, version int) (repository.PackageRevision, error) {
	ctx, span := tracer.Start(ctx, "gitRepository::ClosePackageRevisionDraft", trace.WithAttributes())
	defer span.End()

	r.mutex.Lock()
	defer r.mutex.Unlock()

	d := prd.(*gitPackageRevisionDraft)

	refSpecs := newPushRefSpecBuilder()
	draftBranch := createDraftName(d.Key())
	proposedBranch := createProposedName(d.Key())

	var newRef *plumbing.Reference

	switch d.lifecycle {
	case v1alpha1.PackageRevisionLifecyclePublished, v1alpha1.PackageRevisionLifecycleDeletionProposed:

		if version == 0 {
			return nil, pkgerrors.New("Version cannot be empty for the next package revision")
		}
		d.prKey.Revision = version

		// Finalize the package revision. Commit it to main branch.
		commitHash, newTreeHash, commitBase, err := r.commitPackageToMain(ctx, d)
		if err != nil {
			return nil, err
		}

		tag := createFinalTagNameInLocal(d.Key())
		refSpecs.AddRefToPush(commitHash, r.branch.RefInLocal()) // Push new main branch
		refSpecs.AddRefToPush(commitHash, tag)                   // Push the tag
		refSpecs.RequireRef(commitBase)                          // Make sure main didn't advance

		// Delete base branch (if one exists and should be deleted)
		switch base := d.base; {
		case base == nil: // no branch to delete
		case base.Name() == draftBranch.RefInLocal(), base.Name() == proposedBranch.RefInLocal():
			refSpecs.AddRefToDelete(base)
		}

		// Update package draft
		d.commit = commitHash
		d.tree = newTreeHash
		newRef = plumbing.NewHashReference(tag, commitHash)

	case v1alpha1.PackageRevisionLifecycleProposed:
		// Push the package revision into a proposed branch.
		refSpecs.AddRefToPush(d.commit, proposedBranch.RefInLocal())

		// Delete base branch (if one exists and should be deleted)
		switch base := d.base; {
		case base == nil: // no branch to delete
		case base.Name() != proposedBranch.RefInLocal():
			refSpecs.AddRefToDelete(base)
		}

		// Update package referemce (commit and tree hash stay the same)
		newRef = plumbing.NewHashReference(proposedBranch.RefInLocal(), d.commit)

	case v1alpha1.PackageRevisionLifecycleDraft:
		// Push the package revision into a draft branch.
		refSpecs.AddRefToPush(d.commit, draftBranch.RefInLocal())
		// Delete base branch (if one exists and should be deleted)
		switch base := d.base; {
		case base == nil: // no branch to delete
		case base.Name() != draftBranch.RefInLocal():
			refSpecs.AddRefToDelete(base)
		}

		// Update package reference (commit and tree hash stay the same)
		newRef = plumbing.NewHashReference(draftBranch.RefInLocal(), d.commit)

	default:
		return nil, fmt.Errorf("package has unrecognized lifecycle: %q", d.lifecycle)
	}

	if err := d.repo.pushAndCleanup(ctx, refSpecs); err != nil {
		// No changes is fine. No need to return an error.
		if !pkgerrors.Is(err, git.NoErrAlreadyUpToDate) {
			return nil, err
		}
	}

	// for backwards compatibility with packages that existed before porch supported
	// descriptions, we populate the workspaceName as the revision number if it is empty
	if d.prKey.WorkspaceName == "" {
		d.prKey.WorkspaceName = "v" + repository.Revision2Str(d.Key().Revision)
	}

	return &gitPackageRevision{
		prKey:   d.prKey,
		repo:    d.repo,
		updated: d.updated,
		ref:     newRef,
		tree:    d.tree,
		commit:  newRef.Hash(),
		tasks:   d.tasks,
	}, nil
}

// doGitWithAuth fetches auth information for git and provides it
// to the provided function which performs the operation against a git repo.
func (r *gitRepository) doGitWithAuth(ctx context.Context, op func(transport.AuthMethod) error) error {
	auth, err := r.getAuthMethod(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to obtain git credentials: %w", err)
	}
	err = op(auth)
	if err != nil {
		if !pkgerrors.Is(err, transport.ErrAuthenticationRequired) {
			return err
		}
		klog.Infof("Authentication failed. Trying to refresh credentials")
		// TODO: Consider having some kind of backoff here.
		auth, err := r.getAuthMethod(ctx, true)
		if err != nil {
			return fmt.Errorf("failed to obtain git credentials: %w", err)
		}
		return op(auth)
	}
	return nil
}

func (r *gitRepository) commitPackageToMain(ctx context.Context, d *gitPackageRevisionDraft) (commitHash, newPackageTreeHash plumbing.Hash, base *plumbing.Reference, err error) {
	ctx, span := tracer.Start(ctx, "gitRepository::commitPackageToMain", trace.WithAttributes())
	defer span.End()
	branch := r.branch
	localRef := branch.RefInLocal()

	var zero plumbing.Hash

	// Fetch main
	switch err := r.doGitWithAuth(ctx, func(auth transport.AuthMethod) error {
		//CHECK: Why is this needed before writing to main? Can't we commit with a fast-forward merge strategy?
		return r.repo.Fetch(&git.FetchOptions{
			RemoteName: OriginName,
			RefSpecs:   []config.RefSpec{branch.ForceFetchSpec()},
			Auth:       auth,
			CABundle:   r.caBundle,
		})
	}); err {
	case nil, git.NoErrAlreadyUpToDate:
		// ok
	default:
		return zero, zero, nil, fmt.Errorf("failed to fetch remote repository: %w", err)
	}

	// Find localTarget branch
	localTarget, err := r.repo.Reference(localRef, false)
	if err != nil {
		// TODO: handle empty repositories - NotFound error
		return zero, zero, nil, fmt.Errorf("failed to find 'main' branch: %w", err)
	}
	headCommit, err := r.repo.CommitObject(localTarget.Hash())
	if err != nil {
		return zero, zero, nil, fmt.Errorf("failed to resolve main branch to commit: %w", err)
	}

	// TODO: Check for out-of-band update of the package in main branch
	// (compare package tree in target branch and common base)
	//CHECK: The above todo can be solved if we just run a merge
	ch, err := newCommitHelper(r, r.userInfoProvider, headCommit.Hash, d.Key().PkgKey.ToFullPathname(), d.tree)
	if err != nil {
		return zero, zero, nil, fmt.Errorf("failed to initialize commit of package %s to %s", d.Key().PkgKey.ToFullPathname(), localRef)
	}

	// Add a commit without changes to mark that the package revision is approved. The gitAnnotation is
	// included so that we can later associate the commit with the correct packagerevision.
	message, err := AnnotateCommitMessage(fmt.Sprintf("Approve %s/%d", d.Key().PkgKey.ToFullPathname(), d.Key().Revision), &gitAnnotation{
		PackagePath:   d.Key().PkgKey.ToFullPathname(),
		WorkspaceName: d.Key().WorkspaceName,
		Revision:      repository.Revision2Str(d.Key().Revision),
	})
	if err != nil {
		return zero, zero, nil, fmt.Errorf("failed annotation commit message for package %s: %v", d.Key().PkgKey.ToFullPathname(), err)
	}
	commitHash, newPackageTreeHash, err = ch.commit(ctx, message, d.Key().PkgKey.ToFullPathname(), d.commit)
	if err != nil {
		return zero, zero, nil, fmt.Errorf("failed to commit package %s to %s", d.Key().PkgKey.ToFullPathname(), localRef)
	}

	return commitHash, newPackageTreeHash, localTarget, nil
}

// findPackage finds the packages in the git repository, under commit, if it is exists at path.
// If no package is found at that path, returns nil, nil
func (r *gitRepository) findPackage(commit *object.Commit, packagePath string) (*packageListEntry, error) {
	t, err := r.discoverPackagesInTree(commit, DiscoverPackagesOptions{FilterPrefix: packagePath, Recurse: false})
	if err != nil {
		return nil, err
	}
	return t.packages[packagePath], nil
}

// discoverPackagesInTree finds the packages in the git repository, under commit.
// If filterPrefix is non-empty, only packages with the specified prefix will be returned.
// It is not an error if filterPrefix matches no packages or even is not a real directory name;
// we will simply return an empty list of packages.
func (r *gitRepository) discoverPackagesInTree(commit *object.Commit, opt DiscoverPackagesOptions) (*packageList, error) {
	t := &packageList{
		parent:   r,
		commit:   commit,
		packages: make(map[string]*packageListEntry),
	}

	rootTree, err := commit.Tree()
	if err != nil {
		return nil, pkgerrors.Wrapf(err, "cannot resolve commit %v to tree (corrupted repository?)", commit.Hash)
	}

	if opt.FilterPrefix != "" {
		tree, err := rootTree.Tree(opt.FilterPrefix)
		if err != nil {
			if err == object.ErrDirectoryNotFound {
				// We treat the filter prefix as a filter, the path doesn't have to exist
				klog.Warningf("could not find filterPrefix %q in commit %v; returning no packages", opt.FilterPrefix, commit.Hash)
				return t, nil
			} else {
				return nil, pkgerrors.Wrapf(err, "error getting tree %s", opt.FilterPrefix)
			}
		}
		rootTree = tree
	}

	if err := t.discoverPackages(r.Key(), rootTree, opt.FilterPrefix, opt.Recurse); err != nil {
		return nil, err
	}

	if opt.FilterPrefix == "" {
		klog.Infof("discovered %d packages @%v", len(t.packages), commit.Hash)
	}
	return t, nil
}

func (r *gitRepository) Refresh(_ context.Context) error {
	return r.UpdateDeletionProposedCache()
}

// getPkgWorkspace returns the workspace name as parsed from the kpt annotations from the latest commit for the package.
// If such a commit is not found, it returns an empty string.
func getPkgWorkspace(commit *object.Commit, p *packageListEntry, ref *plumbing.Reference) string {
	if ref == nil || (!isTagInLocalRepo(ref.Name()) && !isDraftBranchNameInLocal(ref.Name()) && !isProposedBranchNameInLocal(ref.Name())) {
		// packages on the main branch may have unrelated commits, we need to find the latest commit relevant to this package
		c, err := p.parent.parent.findLatestPackageCommit(p.parent.commit, p.pkgKey)
		if c == nil {
			if err != nil {
				klog.Warningf("Error searching for latest commit for package %s: %s", p.pkgKey, err)
			}
			return ""
		}
		commit = c
	}
	annotations, err := ExtractGitAnnotations(commit)
	if err != nil {
		klog.Warningf("Error extracting git annotations for package %s: %s", p.pkgKey, err)
	}
	for _, a := range annotations {
		if a.PackagePath != p.pkgKey.ToFullPathname() {
			continue
		}
		if a.WorkspaceName != "" {
			return a.WorkspaceName
		}
	}
	return ""
}
