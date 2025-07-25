// Copyright 2022, 2024 The kpt and Nephio Authors
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

package porch

import (
	"context"
	"fmt"

	api "github.com/nephio-project/porch/api/porch/v1alpha1"
	"github.com/nephio-project/porch/pkg/async"
	"github.com/nephio-project/porch/pkg/repository"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/klog/v2"
)

var tracer = otel.Tracer("packagerevision")

type packageRevisions struct {
	packageCommon
	rest.TableConvertor
}

var _ rest.Storage = &packageRevisions{}
var _ rest.Lister = &packageRevisions{}
var _ rest.Getter = &packageRevisions{}
var _ rest.Scoper = &packageRevisions{}
var _ rest.Creater = &packageRevisions{}
var _ rest.Updater = &packageRevisions{}
var _ rest.GracefulDeleter = &packageRevisions{}
var _ rest.Watcher = &packageRevisions{}
var _ rest.SingularNameProvider = &packageRevisions{}

// GetSingularName implements the SingularNameProvider interface
func (r *packageRevisions) GetSingularName() string {
	return "packagerevision"
}

func (r *packageRevisions) New() runtime.Object {
	return &api.PackageRevision{}
}

func (r *packageRevisions) Destroy() {}

func (r *packageRevisions) NewList() runtime.Object {
	return &api.PackageRevisionList{}
}

func (r *packageRevisions) NamespaceScoped() bool {
	return true
}

// List selects resources in the storage which match to the selector. 'options' can be nil.
func (r *packageRevisions) List(ctx context.Context, options *metainternalversion.ListOptions) (runtime.Object, error) {
	ctx, span := tracer.Start(ctx, "[START]::packageRevisions::List", trace.WithAttributes())
	defer span.End()

	result := &api.PackageRevisionList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PackageRevisionList",
			APIVersion: api.SchemeGroupVersion.Identifier(),
		},
	}

	filter, err := parsePackageRevisionFieldSelector(options.FieldSelector)
	if err != nil {
		return nil, err
	}

	if err := r.packageCommon.listPackageRevisions(ctx, filter, options.LabelSelector, func(ctx context.Context, p repository.PackageRevision) error {
		item, err := p.GetPackageRevision(ctx)
		if err != nil {
			return err
		}
		result.Items = append(result.Items, *item)
		return nil
	}); err != nil {
		return nil, err
	}

	return result, nil
}

// Get implements the Getter interface
func (r *packageRevisions) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	ctx, span := tracer.Start(ctx, "[START]::packageRevisions::Get", trace.WithAttributes())
	defer span.End()

	namespace, namespaced := genericapirequest.NamespaceFrom(ctx)
	if !namespaced {
		return nil, apierrors.NewBadRequest("namespace must be specified")
	}

	repoPkgRev, err := r.getRepoPkgRev(ctx, name, namespace)
	if err != nil {
		return nil, err
	}

	apiPkgRev, err := repoPkgRev.GetPackageRevision(ctx)
	if err != nil {
		return nil, err
	}

	return apiPkgRev, nil
}

// Create implements the Creater interface.
func (r *packageRevisions) Create(ctx context.Context, runtimeObject runtime.Object, createValidation rest.ValidateObjectFunc,
	options *metav1.CreateOptions) (runtime.Object, error) {
	ctx, span := tracer.Start(ctx, "[START]::packageRevisions::Create", trace.WithAttributes())
	defer span.End()
	namespace, namespaced := genericapirequest.NamespaceFrom(ctx)
	if !namespaced {
		return nil, apierrors.NewBadRequest("namespace must be specified")
	}
	newApiPkgRev, ok := runtimeObject.(*api.PackageRevision)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected PackageRevision object, got %T", runtimeObject))
	}

	// TODO: Accept some form of client-provided name, for example using GenerateName
	// and figure out where we can store it (in Kptfile?). Porch can then append unique
	// suffix to the names while respecting client-provided value as well.
	if newApiPkgRev.Name != "" {
		klog.Warningf("Client provided metadata.name %q", newApiPkgRev.Name)
	}

	repositoryName := newApiPkgRev.Spec.RepositoryName
	if repositoryName == "" {
		return nil, apierrors.NewBadRequest("spec.repositoryName is required")
	}

	// Call a go routine to create the package revision
	go r.asyncCreatePackageRevision(repositoryName, namespace, newApiPkgRev)

	newApiPkgRev.Name = composePkgRevK8sName(newApiPkgRev)

	// Return the context created by the apiserver
	return newApiPkgRev, nil

}

func (r *packageRevisions) asyncCreatePackageRevision(repoName, namespace string, newApiPkgRev *api.PackageRevision) {
	// Create a new context for the go routine
	goCtx, cancel := context.WithTimeout(context.Background(), r.cad.GetCtxTimeout())
	defer cancel()
	goCtx, span := tracer.Start(goCtx, "[START-GOROUTINE]::packageRevisions::callCreatePackageRevision", trace.WithAttributes())
	defer span.End()

	pkgRevK8sName := composePkgRevK8sName(newApiPkgRev)

	r.savePkgRevJobInDB(goCtx, namespace, pkgRevK8sName, "Package revision create in progress")

	repositoryObj, err := r.packageCommon.getRepositoryObj(goCtx, types.NamespacedName{Name: repoName, Namespace: namespace})
	if err != nil {
		klog.Error(err)
		r.savePkgRevJobInDB(goCtx, namespace, pkgRevK8sName, err.Error())
		return
	}

	fieldErrors := r.createStrategy.Validate(goCtx, newApiPkgRev)
	if len(fieldErrors) > 0 {
		err := apierrors.NewInvalid(api.SchemeGroupVersion.WithKind("PackageRevision").GroupKind(), newApiPkgRev.Name, fieldErrors)
		klog.Error(err)
		r.savePkgRevJobInDB(goCtx, namespace, pkgRevK8sName, err.Error())
		return
	}

	var parentPackage repository.PackageRevision
	if newApiPkgRev.Spec.Parent != nil && newApiPkgRev.Spec.Parent.Name != "" {
		p, err := r.packageCommon.getRepoPkgRev(goCtx, newApiPkgRev.Spec.Parent.Name, namespace)
		if err != nil {
			klog.Errorf("cannot get parent package %q: %v", newApiPkgRev.Spec.Parent.Name, err)
			r.savePkgRevJobInDB(goCtx, namespace, pkgRevK8sName, err.Error())
			return
		}
		parentPackage = p
	}

	pkgMutexKey := uncreatedPackageMutexKey(newApiPkgRev)
	pkgMutex := getMutexForPackage(pkgMutexKey)

	locked := pkgMutex.TryLock()
	if !locked {
		conflictError := creationConflictError(newApiPkgRev)
		err := apierrors.NewConflict(api.Resource("packagerevisions"), "(new creation)", conflictError)
		klog.Error(err)
		r.savePkgRevJobInDB(goCtx, namespace, pkgRevK8sName, err.Error())
		return
	}
	defer pkgMutex.Unlock()

	if _, err := r.cad.CreatePackageRevision(goCtx, repositoryObj, newApiPkgRev, parentPackage); err != nil {
		klog.Errorf("Create error for %s - %s", newApiPkgRev.Name, err)
		r.savePkgRevJobInDB(goCtx, namespace, pkgRevK8sName, err.Error())
		return
	}
	r.savePkgRevJobInDB(goCtx, namespace, pkgRevK8sName, "Package revision created successfully")
	goCtx.Done()
}

func (r *packageRevisions) savePkgRevJobInDB(ctx context.Context, namespace, pkgRevK8sName, status string) {
	asyncHandler := async.GetDefaultAsyncHandler()
	if err := asyncHandler.SavePackageRevisionJob(ctx, r.cad.GetCacheOpts(), namespace, pkgRevK8sName, status); err != nil {
		klog.Error(err)
	}
}

// Update implements the Updater interface.

// Update finds a resource in the storage and updates it. Some implementations
// may allow updates creates the object - they should set the created boolean
// to true.
func (r *packageRevisions) Update(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo, createValidation rest.ValidateObjectFunc,
	updateValidation rest.ValidateObjectUpdateFunc, forceAllowCreate bool, options *metav1.UpdateOptions) (runtime.Object, bool, error) {
	ctx, span := tracer.Start(ctx, "[START]::packageRevisions::Update", trace.WithAttributes())
	defer span.End()

	return r.packageCommon.updatePackageRevision(ctx, name, objInfo, createValidation, updateValidation, forceAllowCreate)
}

// Delete implements the GracefulDeleter interface.
// Delete finds a resource in the storage and deletes it.
// The delete attempt is validated by the deleteValidation first.
// If options are provided, the resource will attempt to honor them or return an invalid
// request error.
// Although it can return an arbitrary error value, IsNotFound(err) is true for the
// returned error value err when the specified resource is not found.
// Delete *may* return the object that was deleted, or a status object indicating additional
// information about deletion.
// It also returns a boolean which is set to true if the resource was instantly
// deleted or false if it will be deleted asynchronously.
func (r *packageRevisions) Delete(ctx context.Context, name string, deleteValidation rest.ValidateObjectFunc, options *metav1.DeleteOptions) (runtime.Object, bool, error) {
	ctx, span := tracer.Start(ctx, "[START]::packageRevisions::Delete", trace.WithAttributes())
	defer span.End()

	ns, namespaced := genericapirequest.NamespaceFrom(ctx)
	if !namespaced {
		return nil, false, apierrors.NewBadRequest("namespace must be specified")
	}
	apiPkgRev := api.PackageRevision{}
	apiPkgRev.Name = name

	// Call a go routine to delete the package revision
	go r.asyncDeletePackageRevision(name, ns, deleteValidation)

	// Return the context created by the apiserver
	return &apiPkgRev, true, nil
}

func (r *packageRevisions) asyncDeletePackageRevision(name, namespace string, deleteValidation rest.ValidateObjectFunc) {
	// Create a new context for the go routine
	goCtx, cancel := context.WithTimeout(context.Background(), r.cad.GetCtxTimeout())
	defer cancel()
	goCtx, span := tracer.Start(goCtx, "[START-GOROUTINE]::packageRevisions::callDeletePackageRevision", trace.WithAttributes())
	defer span.End()

	r.savePkgRevJobInDB(goCtx, namespace, name, "Package revision delete in progress")

	repoPkgRev, err := r.packageCommon.getRepoPkgRev(goCtx, name, namespace)
	if err != nil {
		r.savePkgRevJobInDB(goCtx, namespace, name, err.Error())
		klog.Error(err)
		return
	}

	apiPkgRev, err := repoPkgRev.GetPackageRevision(goCtx)
	if err != nil {
		err := apierrors.NewInternalError(err)
		repoPkgRev.SetError(goCtx, err.Error())
		r.savePkgRevJobInDB(goCtx, namespace, name, err.Error())
		klog.Error(err)
		return
	}

	repositoryObj, err := r.packageCommon.validateDelete(goCtx, deleteValidation, apiPkgRev, name, namespace)
	if err != nil {
		repoPkgRev.SetError(goCtx, err.Error())
		r.savePkgRevJobInDB(goCtx, namespace, name, err.Error())
		klog.Error(err)
		return
	}

	pkgMutexKey := getPackageMutexKey(namespace, name)
	pkgMutex := getMutexForPackage(pkgMutexKey)

	locked := pkgMutex.TryLock()
	if !locked {
		err := apierrors.NewConflict(api.Resource("packagerevisions"), name, fmt.Errorf(GenericConflictErrorMsg, "package revision", pkgMutexKey))
		repoPkgRev.SetError(goCtx, err.Error())
		klog.Error(err)
		return
	}
	defer pkgMutex.Unlock()

	if err := r.cad.DeletePackageRevision(goCtx, repositoryObj, repoPkgRev); err != nil {
		repoPkgRev.SetError(goCtx, err.Error())
		r.savePkgRevJobInDB(goCtx, namespace, name, err.Error())
		klog.Errorf("Delete error for %s - %s", repoPkgRev.Key().PkgKey.Package, err)
		return
	}
	r.savePkgRevJobInDB(goCtx, namespace, name, "Package revision deleted successfully")
	goCtx.Done()
}

func uncreatedPackageMutexKey(newApiPkgRev *api.PackageRevision) string {
	return fmt.Sprintf("%s-%s-%s-%s",
		newApiPkgRev.Namespace,
		newApiPkgRev.Spec.RepositoryName,
		newApiPkgRev.Spec.PackageName,
		newApiPkgRev.Spec.WorkspaceName,
	)
}

func creationConflictError(newApiPkgRev *api.PackageRevision) error {
	return fmt.Errorf(
		fmt.Sprintf(
			ConflictErrorMsgBase,
			"to create package revision with details namespace=%q, repository=%q, package=%q,workspace=%q",
		),
		newApiPkgRev.Namespace,
		newApiPkgRev.Spec.RepositoryName,
		newApiPkgRev.Spec.PackageName,
		newApiPkgRev.Spec.WorkspaceName,
	)
}

func composePkgRevK8sName(apiPkgRev *api.PackageRevision) string {
	return apiPkgRev.Spec.RepositoryName + "." + apiPkgRev.Spec.PackageName + "." + apiPkgRev.Spec.WorkspaceName
}
