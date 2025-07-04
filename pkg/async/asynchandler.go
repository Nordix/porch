package async

import (
	"context"

	cachetypes "github.com/nephio-project/porch/pkg/cache/types"
	"github.com/nephio-project/porch/pkg/dbhandler"
	"github.com/nephio-project/porch/pkg/repository"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/klog/v2"
)

var _ AsyncHandler = AsyncHandler{}

type AsyncHandler struct {
}

var tracer = otel.Tracer("asyncHandler")

func (ah *AsyncHandler) SavePackageRevisionJob(ctx context.Context, cacheOpts cachetypes.CacheOptions, prk repository.PackageRevisionKey, status string) error {
	_, span := tracer.Start(ctx, "asyncHandler::SavePackageRevisionJob", trace.WithAttributes())
	defer span.End()
	if cacheOpts.CacheType == "DB" {
		klog.Infof("SavePackageRevisionJob: writing package revision task in DB for %q", prk.PkgKey)
		sqlStatement := `
        INSERT INTO async_jobs (name_space, repo_name, package_name, package_rev, workspace_name, status)
		VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (name_space, repo_name, package_name, package_rev, workspace_name) DO UPDATE SET status=$6`
		if _, err := dbhandler.GetDB().Db.Exec(
			sqlStatement,
			prk.RKey().Namespace, prk.RKey().Name, prk.PkgKey.Package, prk.Revision, prk.WorkspaceName, status); err == nil {
			klog.Infof("SavePackageRevisionJob: query succeeded, row created")
		} else {
			klog.Infof("SavePackageRevisionJob: query failed %q", err)
			return err
		}
	}
	return nil
}

func (ah *AsyncHandler) DeletePackageRevisionJob(ctx context.Context, cacheOpts cachetypes.CacheOptions, prk repository.PackageRevisionKey) error {
	_, span := tracer.Start(ctx, "asyncHandler::DeletePackageRevisionJob", trace.WithAttributes())
	defer span.End()
	if cacheOpts.CacheType == "DB" {
		klog.Infof("deletePkgRevJob: deleting package revision task in DB for %q", prk.PkgKey)

		sqlStatement := `
        DELETE FROM async_jobs WHERE name_space=$1 AND repo_name=$2 AND package_name=$3 AND package_rev=$4 AND workspace_name=$5`

		if _, err := dbhandler.GetDB().Db.Exec(
			sqlStatement,
			prk.RKey().Namespace, prk.RKey().Name, prk.PkgKey.Package, prk.Revision, prk.WorkspaceName); err == nil {
			klog.Infof("deletePkgRevJob: query succeeded, row deleted")
		} else {
			klog.Infof("deletePkgRevJob: query failed %q", err)
			return err
		}
	}
	return nil

}

func (ah *AsyncHandler) ListPackageRevisionJobs(ctx context.Context, cacheOpts cachetypes.CacheOptions, prk repository.PackageRevisionKey) ([]*Job, error) {
	_, span := tracer.Start(ctx, "asyncHandler::ListPackageRevisionJobs", trace.WithAttributes())
	defer span.End()
	if cacheOpts.CacheType == "DB" {
		klog.Infof("ListPackageRevisionJobs: listing package revision jobs in the DB for repo %q", prk.RKey().Name)
		sqlStatement := `SELECT * FROM async_jobs WHERE name_space=$1 AND repo_name=$2`
		tasks := make([]*Job, 0)
		if rows, err := dbhandler.GetDB().Db.Query(
			sqlStatement,
			prk.RKey().Namespace, prk.RKey().Name); err == nil {
			klog.Infof("listPkgRevJob: query succeeded")

			defer rows.Close()
			for rows.Next() {
				var t Job
				if err := rows.Scan(&t.Name_space, &t.Repo_name, &t.Package_name, &t.Package_rev, &t.Workspace_name, &t.Status); err != nil {
					return nil, err
				}
				tasks = append(tasks, &t)
			}
		} else {
			klog.Infof("listPkgRevJob: query failed %q", err)
			return nil, err
		}
		return tasks, nil
	}
	return nil, nil
}
