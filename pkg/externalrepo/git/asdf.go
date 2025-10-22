package git

import (
	"context"

	"github.com/go-git/go-git/v5"
	"github.com/nephio-project/porch/pkg/repository"
)

// This is the current GitRepository, there should be one created per Repository CR
type gitPorchRepository interface {
	repository.Repository
	GitRepository
}

// This would be responsible for de-duplicating between Repository CRs and git filesystems.
// This would need to keep track of how many gitPorchRepositories are referring to the same
// gitRepository, so it can clean up the git filesystem if the reference number becomes 0.
type gitRepositoryFactory interface {
	OpenRepository()
	CloseRepository()
}

// This would be repsonsible for operating towards the git filesystem and the network as well.
// go-git assumes it's the only thing operating on the filesystem by default, and it does maintain some caches.
// The interface needs to take in all the gitPorchRepository specififs, like UserInfoProvider,
// CA secrets, branches, etc. So it can "context-switch" between handling requests from different
// Repository CR related gitPorchRepositories
type gitRepositoryv2 interface {
	//All repository.Repository function calls can be replicated here,
	//with an additional gitContext parameter
	ListPackageRevisions(ctx context.Context, gitContext gitPorchRepositoryContext, filter repository.ListPackageRevisionFilter) ([]repository.PackageRevision, error)
}

// This would be a context that's not modifiable from the gitRepositoryv2, but also passed in with each request.
type gitPorchRepositoryContext struct {
	key                repository.RepositoryKey
	secret             string     // Name of the k8s Secret resource containing credentials
	branch             BranchName // The main branch from repository registration (defaults to 'main' if unspecified)
	repo               *git.Repository
	credentialResolver repository.CredentialResolver
	userInfoProvider   repository.UserInfoProvider
	deployment         bool
	credential         repository.Credential
	caBundle           []byte
}
