package async

import (
	"context"

	cachetypes "github.com/nephio-project/porch/pkg/cache/types"
	"github.com/nephio-project/porch/pkg/repository"
)

type Job struct {
	Name_space, Repo_name, Package_name, Package_rev, Workspace_name, Status string
}

type Async interface {
	// SavePackageRevisionJob is run by the go routine to add a task in the db
	SavePackageRevisionJob(ctx context.Context, cacheOpts cachetypes.CacheOptions, prk repository.PackageRevisionKey, status string) error

	// DeletePackageRevisionJob is run by the go routine to delete a task in the db
	DeletePackageRevisionJob(ctx context.Context, cacheOpts cachetypes.CacheOptions, prk repository.PackageRevisionKey) error

	// ListPackageRevisionJob lists the tasks in db which have failed.
	ListPackageRevisionJobs(ctx context.Context, cacheOpts cachetypes.CacheOptions, prk repository.PackageRevisionKey) ([]*Job, error)
}

func GetDefaultAsyncHandler() Async {
	return &AsyncHandler{}
}
