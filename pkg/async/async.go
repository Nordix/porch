package async

import (
	"context"

	cachetypes "github.com/nephio-project/porch/pkg/cache/types"
	"github.com/nephio-project/porch/pkg/repository"
)

type Job struct {
	Name_space, K8s_name, Status string
}

type Async interface {
	// SavePackageRevisionJob is run by the go routine to add a task in the db
	SavePackageRevisionJob(ctx context.Context, cacheOpts cachetypes.CacheOptions, namespace, pkgRevK8sName, status string) error

	// DeletePackageRevisionJob is run by the go routine to delete a task in the db
	DeletePackageRevisionJob(ctx context.Context, cacheOpts cachetypes.CacheOptions, namespace, pkgRevK8sName string) error

	// ListPackageRevisionJob lists the tasks in db which have failed.
	ListAllPackageRevisionJobs(ctx context.Context, cacheOpts cachetypes.CacheOptions, prk repository.PackageRevisionKey) ([]*Job, error)

	// GetPackageRevisionJob gets the task in db which has failed.
	GetPackageRevisionJob(ctx context.Context, cacheOpts cachetypes.CacheOptions, namespace, pkgRevK8sName string) (*Job, error)
}

func GetDefaultAsyncHandler() Async {
	return &AsyncHandler{}
}
