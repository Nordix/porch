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

func (ah *AsyncHandler) SavePackageRevisionJob(ctx context.Context, cacheOpts cachetypes.CacheOptions, namespace, pkgRevK8sName, status string) error {
	_, span := tracer.Start(ctx, "asyncHandler::SavePackageRevisionJob", trace.WithAttributes())
	defer span.End()
	if cacheOpts.CacheType == "DB" {
		klog.V(3).Infof("SavePackageRevisionJob: writing package revision task in DB for %q", pkgRevK8sName)
		sqlStatement := `
        INSERT INTO async_jobs (name_space, k8s_name, status)
		VALUES ($1, $2, $3) ON CONFLICT (name_space, k8s_name) DO UPDATE SET status=$3`
		if _, err := dbhandler.GetDB().Db.Exec(
			sqlStatement,
			namespace, pkgRevK8sName, status); err == nil {
			klog.V(3).Infof("SavePackageRevisionJob: query succeeded, row created")
		} else {
			klog.V(3).Infof("SavePackageRevisionJob: query failed %q", err)
			return err
		}
	}
	return nil
}

func (ah *AsyncHandler) DeletePackageRevisionJob(ctx context.Context, cacheOpts cachetypes.CacheOptions, namespace, pkgRevK8sName string) error {
	_, span := tracer.Start(ctx, "asyncHandler::DeletePackageRevisionJob", trace.WithAttributes())
	defer span.End()
	if cacheOpts.CacheType == "DB" {
		klog.V(3).Infof("deletePkgRevJob: deleting package revision task in DB for %q", pkgRevK8sName)

		sqlStatement := `
        DELETE FROM async_jobs WHERE name_space=$1 AND k8s_name=$2`

		if _, err := dbhandler.GetDB().Db.Exec(
			sqlStatement,
			namespace, pkgRevK8sName); err == nil {
			klog.V(3).Infof("deletePkgRevJob: query succeeded, row deleted")
		} else {
			klog.V(3).Infof("deletePkgRevJob: query failed %q", err)
			return err
		}
	}
	return nil

}

func (ah *AsyncHandler) GetPackageRevisionJob(ctx context.Context, cacheOpts cachetypes.CacheOptions, namespace, pkgRevK8sName string) (*Job, error) {
	_, span := tracer.Start(ctx, "asyncHandler::GetPackageRevisionJob", trace.WithAttributes())
	defer span.End()

	if cacheOpts.CacheType == "DB" {
		klog.Infof("GetPackageRevisionJob: fetching job for %q", pkgRevK8sName)

		sqlStatement := `
            SELECT name_space, k8s_name, status
            FROM async_jobs
            WHERE name_space=$1 AND k8s_name=$2
            LIMIT 1
        `

		row := dbhandler.GetDB().Db.QueryRow(
			sqlStatement,
			namespace,
			pkgRevK8sName,
		)

		var job Job
		if err := row.Scan(
			&job.Name_space,
			&job.K8s_name,
			&job.Status,
		); err != nil {
			klog.Infof("GetPackageRevisionJob: query failed %q", err)
			return nil, err
		}

		return &job, nil
	}

	return nil, nil
}

func (ah *AsyncHandler) ListAllPackageRevisionJobs(ctx context.Context, cacheOpts cachetypes.CacheOptions, prk repository.PackageRevisionKey) ([]*Job, error) {
	_, span := tracer.Start(ctx, "asyncHandler::ListPackageRevisionJobs", trace.WithAttributes())
	defer span.End()
	if cacheOpts.CacheType == "DB" {
		klog.Infof("ListPackageRevisionJobs: listing package revision jobs in the DB for repo %q", prk.RKey().Name)
		sqlStatement := `SELECT * FROM async_jobs WHERE name_space=$1`
		tasks := make([]*Job, 0)
		if rows, err := dbhandler.GetDB().Db.Query(
			sqlStatement,
			prk.RKey().Namespace, prk.RKey().Name); err == nil {
			klog.Infof("listPkgRevJob: query succeeded")

			defer rows.Close()
			for rows.Next() {
				var t Job
				if err := rows.Scan(&t.Name_space, &t.K8s_name, &t.Status); err != nil {
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
