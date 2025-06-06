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

package porch

import (
	"context"
	"fmt"
	"sync"

	unversionedapi "github.com/nephio-project/porch/api/porch"
	api "github.com/nephio-project/porch/api/porch/v1alpha1"
	configapi "github.com/nephio-project/porch/api/porchconfig/v1alpha1"
	"github.com/nephio-project/porch/pkg/engine"
	"github.com/nephio-project/porch/pkg/repository"
	"github.com/nephio-project/porch/pkg/util"
	"go.opentelemetry.io/otel/trace"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const ConflictErrorMsgBase = "another request is already in progress %s"

var GenericConflictErrorMsg = fmt.Sprintf(ConflictErrorMsgBase, "on %s \"%s\"")

var mutexMapMutex sync.Mutex
var pkgRevOperationMutexes = map[string]*sync.Mutex{}

type packageCommon struct {
	// scheme holds our scheme, for type conversions etc
	scheme *runtime.Scheme

	cad engine.CaDEngine
	// coreClient is a client back to the core kubernetes API server, useful for querying CRDs etc
	coreClient     client.Client
	gr             schema.GroupResource
	updateStrategy SimpleRESTUpdateStrategy
	createStrategy SimpleRESTCreateStrategy
}

func (r *packageCommon) listPackageRevisions(ctx context.Context, filter packageRevisionFilter,
	selector labels.Selector, callback func(ctx context.Context, p repository.PackageRevision) error) error {
	ctx, span := tracer.Start(ctx, "packageCommon::listPackageRevisions", trace.WithAttributes())
	defer span.End()

	var opts []client.ListOption
	ns, namespaced := genericapirequest.NamespaceFrom(ctx)
	if namespaced && ns != "" {
		opts = append(opts, client.InNamespace(ns))

		if filter.Namespace != "" && ns != filter.Namespace {
			return fmt.Errorf("conflicting namespaces specified: %q and %q", ns, filter.Namespace)
		}
	}

	// TODO: Filter on filter.Repository?
	var repositories configapi.RepositoryList
	if err := r.coreClient.List(ctx, &repositories, opts...); err != nil {
		return fmt.Errorf("error listing repository objects: %w", err)
	}

	for i := range repositories.Items {
		repositoryObj := &repositories.Items[i]

		if filter.Repository != "" && filter.Repository != repositoryObj.GetName() {
			continue
		}

		revisions, err := r.cad.ListPackageRevisions(ctx, repositoryObj, filter.ListPackageRevisionFilter)
		if err != nil {
			klog.Warningf("error listing package revisions from repository %s/%s: %+v", repositoryObj.GetNamespace(), repositoryObj.GetName(), err)
			continue
		}
		for _, rev := range revisions {
			apiPkgRev, err := rev.GetPackageRevision(ctx)
			if err != nil {
				return err
			}

			if selector != nil && !selector.Matches(labels.Set(apiPkgRev.Labels)) {
				continue
			}

			if err := callback(ctx, rev); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *packageCommon) listPackages(ctx context.Context, filter packageFilter, callback func(p repository.Package) error) error {
	var opts []client.ListOption
	if ns, namespaced := genericapirequest.NamespaceFrom(ctx); namespaced {
		opts = append(opts, client.InNamespace(ns))

		if filter.Namespace != "" && ns != filter.Namespace {
			return fmt.Errorf("conflicting namespaces specified: %q and %q", ns, filter.Namespace)
		}
	}

	// TODO: Filter on filter.Repository?
	var repositories configapi.RepositoryList
	if err := r.coreClient.List(ctx, &repositories, opts...); err != nil {
		return fmt.Errorf("error listing repository objects: %w", err)
	}

	for i := range repositories.Items {
		repositoryObj := &repositories.Items[i]

		if filter.Repository != "" && filter.Repository != repositoryObj.GetName() {
			continue
		}

		revisions, err := r.cad.ListPackages(ctx, repositoryObj, filter.ListPackageFilter)
		if err != nil {
			klog.Warningf("error listing packages from repository %s/%s: %s", repositoryObj.GetNamespace(), repositoryObj.GetName(), err)
			continue
		}
		for _, rev := range revisions {
			if err := callback(rev); err != nil {
				return err
			}
		}
	}
	return nil
}

// namespaceFilteringWatcher wraps an ObjectWatcher and filters events by namespace
// Only events for the specified namespace are passed to the delegate watcher
type namespaceFilteringWatcher struct {
	ns       string
	delegate engine.ObjectWatcher
}

func (n *namespaceFilteringWatcher) OnPackageRevisionChange(eventType watch.EventType, obj repository.PackageRevision) bool {
	// Use KubeObjectNamespace for efficient namespace filtering
	if obj.KubeObjectNamespace() != n.ns {
		return true // skip, but keep watching
	}
	return n.delegate.OnPackageRevisionChange(eventType, obj)
}

func (r *packageCommon) watchPackages(ctx context.Context, filter packageRevisionFilter, callback engine.ObjectWatcher) error {
	ns, namespaced := genericapirequest.NamespaceFrom(ctx)
	wrappedCallback := callback
	if namespaced && ns != "" {
		wrappedCallback = &namespaceFilteringWatcher{
			ns:       ns,
			delegate: callback,
		}
	}
	if err := r.cad.ObjectCache().WatchPackageRevisions(ctx, filter.ListPackageRevisionFilter, wrappedCallback); err != nil {
		return err
	}

	return nil
}

func (r *packageCommon) getRepositoryObjFromName(ctx context.Context, name string) (*configapi.Repository, error) {
	ns, namespaced := genericapirequest.NamespaceFrom(ctx)
	if !namespaced {
		return nil, fmt.Errorf("namespace must be specified")
	}

	repoName, err := util.ParsePkgRevObjNameField(name, 0)
	if err != nil {
		return nil, apierrors.NewNotFound(r.gr, name)
	}

	return r.getRepositoryObj(ctx, types.NamespacedName{Name: repoName, Namespace: ns})
}

func (r *packageCommon) getRepositoryObj(ctx context.Context, repositoryID types.NamespacedName) (*configapi.Repository, error) {
	var repositoryObj configapi.Repository
	if err := r.coreClient.Get(ctx, repositoryID, &repositoryObj); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, apierrors.NewNotFound(configapi.TypeRepository.GroupResource(), repositoryID.Name)
		}
		return nil, apierrors.NewInternalError(fmt.Errorf("error getting repository %v: %w", repositoryID, err))
	}
	return &repositoryObj, nil
}

func (r *packageCommon) getRepoPkgRev(ctx context.Context, name string) (repository.PackageRevision, error) {
	ctx, span := tracer.Start(ctx, "packageCommon::getRepoPkgRev", trace.WithAttributes())
	defer span.End()

	repositoryObj, err := r.getRepositoryObjFromName(ctx, name)
	if err != nil {
		return nil, err
	}
	revisions, err := r.cad.ListPackageRevisions(ctx, repositoryObj, repository.ListPackageRevisionFilter{KubeObjectName: name})
	if err != nil {
		return nil, err
	}
	for _, rev := range revisions {
		if rev.KubeObjectName() == name {
			return rev, nil
		}
	}

	return nil, apierrors.NewNotFound(r.gr, name)
}

func (r *packageCommon) getPackage(ctx context.Context, name string) (repository.Package, error) {
	repositoryObj, err := r.getRepositoryObjFromName(ctx, name)
	if err != nil {
		return nil, err
	}

	revisions, err := r.cad.ListPackages(ctx, repositoryObj, repository.ListPackageFilter{KubeObjectName: name})
	if err != nil {
		return nil, err
	}
	for _, rev := range revisions {
		if rev.KubeObjectName() == name {
			return rev, nil
		}
	}

	return nil, apierrors.NewNotFound(r.gr, name)
}

// Common implementation of PackageRevision update logic.
func (r *packageCommon) updatePackageRevision(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo,
	createValidation rest.ValidateObjectFunc, updateValidation rest.ValidateObjectUpdateFunc, forceAllowCreate bool) (runtime.Object, bool, error) {
	ctx, span := tracer.Start(ctx, "packageCommon::updatePackageRevision", trace.WithAttributes())
	defer span.End()

	// TODO: Is this all boilerplate??

	ns, namespaced := genericapirequest.NamespaceFrom(ctx)
	if !namespaced {
		return nil, false, apierrors.NewBadRequest("namespace must be specified")
	}

	pkgMutexKey := getPackageMutexKey(ns, name)
	pkgMutex := getMutexForPackage(pkgMutexKey)

	locked := pkgMutex.TryLock()
	if !locked {
		return nil, false,
			apierrors.NewConflict(
				api.Resource("packagerevisions"),
				name,
				fmt.Errorf(GenericConflictErrorMsg, "package revision", pkgMutexKey))
	}
	defer pkgMutex.Unlock()

	// isCreate tracks whether this is an update that creates an object (this happens in server-side apply)
	isCreate := false
	oldRepoPkgRev, err := r.getRepoPkgRev(ctx, name)
	if err != nil {
		if forceAllowCreate && apierrors.IsNotFound(err) {
			// For server-side apply, we can create the object here
			isCreate = true
		} else {
			return nil, false, err
		}
	}

	// We have to be runtime.Object (and not *api.PackageRevision) or else nil-checks fail (because a nil object is not a nil interface)
	var oldApiPkgRev runtime.Object
	if !isCreate {
		oldApiPkgRev, err = oldRepoPkgRev.GetPackageRevision(ctx)
		if err != nil {
			return nil, false, err
		}
	}

	newRuntimeObj, err := objInfo.UpdatedObject(ctx, oldApiPkgRev)
	if err != nil {
		klog.Infof("update failed to construct UpdatedObject: %v", err)
		return nil, false, err
	}

	// This type conversion is necessary because mutations work with unversioned types
	// (mostly for historical reasons).  So the server-side-apply library returns an unversioned object.
	if unversioned, isUnversioned := newRuntimeObj.(*unversionedapi.PackageRevision); isUnversioned {
		klog.Warningf("converting from unversioned to versioned object")
		typed := &api.PackageRevision{}
		if err := r.scheme.Convert(unversioned, typed, nil); err != nil {
			return nil, false, fmt.Errorf("failed to convert %T to %T: %w", unversioned, typed, err)
		}
		newRuntimeObj = typed
	}

	if err := r.validateUpdate(ctx, newRuntimeObj, oldApiPkgRev, isCreate, createValidation,
		updateValidation, "PackageRevision", name); err != nil {
		return nil, false, err
	}

	newApiPkgRev, ok := newRuntimeObj.(*api.PackageRevision)
	if !ok {
		return nil, false, apierrors.NewBadRequest(fmt.Sprintf("expected PackageRevision object, got %T", newRuntimeObj))
	}

	repoName, err := util.ParsePkgRevObjNameField(name, 0)
	if err != nil {
		return nil, false, apierrors.NewBadRequest(fmt.Sprintf("invalid name %q", name))
	}
	if isCreate {
		repoName = newApiPkgRev.Spec.RepositoryName
		if repoName == "" {
			return nil, false, apierrors.NewBadRequest(fmt.Sprintf("invalid repositoryName %q", name))
		}
	}

	var repositoryObj configapi.Repository
	repositoryID := types.NamespacedName{Namespace: ns, Name: repoName}
	if err := r.coreClient.Get(ctx, repositoryID, &repositoryObj); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, apierrors.NewNotFound(configapi.TypeRepository.GroupResource(), repositoryID.Name)
		}
		return nil, false, apierrors.NewInternalError(fmt.Errorf("error getting repository %v: %w", repositoryID, err))
	}

	var parentPackage repository.PackageRevision
	if newApiPkgRev.Spec.Parent != nil && newApiPkgRev.Spec.Parent.Name != "" {
		p, err := r.getRepoPkgRev(ctx, newApiPkgRev.Spec.Parent.Name)
		if err != nil {
			return nil, false, fmt.Errorf("cannot get parent package %q: %w", newApiPkgRev.Spec.Parent.Name, err)
		}
		parentPackage = p
	}

	if isCreate {
		rev, err := r.cad.CreatePackageRevision(ctx, &repositoryObj, newApiPkgRev, parentPackage)
		if err != nil {
			klog.Infof("error creating package: %v", err)
			return nil, false, apierrors.NewInternalError(err)
		}
		createdApiPkgRev, err := rev.GetPackageRevision(ctx)
		if err != nil {
			return nil, false, apierrors.NewInternalError(err)
		}

		return createdApiPkgRev, true, nil
	}

	rev, err := r.cad.UpdatePackageRevision(ctx, 0, &repositoryObj, oldRepoPkgRev, oldApiPkgRev.(*api.PackageRevision), newApiPkgRev, parentPackage)
	if err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}

	updated, err := rev.GetPackageRevision(ctx)
	if err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}

	return updated, false, nil
}

// Common implementation of Package update logic.
func (r *packageCommon) updatePackage(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo,
	createValidation rest.ValidateObjectFunc, updateValidation rest.ValidateObjectUpdateFunc, forceAllowCreate bool) (runtime.Object, bool, error) {
	// TODO: Is this all boilerplate??

	ns, namespaced := genericapirequest.NamespaceFrom(ctx)
	if !namespaced {
		return nil, false, apierrors.NewBadRequest("namespace must be specified")
	}

	// isCreate tracks whether this is an update that creates an object (this happens in server-side apply)
	isCreate := false

	oldPackage, err := r.getPackage(ctx, name)
	if err != nil {
		if forceAllowCreate && apierrors.IsNotFound(err) {
			// For server-side apply, we can create the object here
			isCreate = true
		} else {
			return nil, false, err
		}
	}

	// We have to be runtime.Object (and not *api.PackageRevision) or else nil-checks fail (because a nil object is not a nil interface)
	var oldRuntimeObj runtime.Object
	if !isCreate {
		oldRuntimeObj = oldPackage.GetPackage(ctx)
	}

	newRuntimeObj, err := objInfo.UpdatedObject(ctx, oldRuntimeObj)
	if err != nil {
		klog.Infof("update failed to construct UpdatedObject: %v", err)
		return nil, false, err
	}

	// This type conversion is necessary because mutations work with unversioned types
	// (mostly for historical reasons).  So the server-side-apply library returns an unversioned object.
	if unversioned, isUnversioned := newRuntimeObj.(*unversionedapi.PackageRevision); isUnversioned {
		klog.Warningf("converting from unversioned to versioned object")
		typed := &api.PackageRevision{}
		if err := r.scheme.Convert(unversioned, typed, nil); err != nil {
			return nil, false, fmt.Errorf("failed to convert %T to %T: %w", unversioned, typed, err)
		}
		newRuntimeObj = typed
	}

	if err := r.validateUpdate(ctx, newRuntimeObj, oldRuntimeObj, isCreate, createValidation,
		updateValidation, "Package", name); err != nil {
		return nil, false, err
	}

	newObj, ok := newRuntimeObj.(*api.PorchPackage)
	if !ok {
		return nil, false, apierrors.NewBadRequest(fmt.Sprintf("expected Package object, got %T", newRuntimeObj))
	}

	repoName, err := util.ParsePkgRevObjNameField(name, 0)
	if err != nil {
		return nil, false, apierrors.NewBadRequest(fmt.Sprintf("invalid name %q", name))
	}
	repositoryName := repoName
	if isCreate {
		repositoryName = newObj.Spec.RepositoryName
		if repositoryName == "" {
			return nil, false, apierrors.NewBadRequest(fmt.Sprintf("invalid repositoryName %q", name))
		}
	}

	var repositoryObj configapi.Repository
	repositoryID := types.NamespacedName{Namespace: ns, Name: repositoryName}
	if err := r.coreClient.Get(ctx, repositoryID, &repositoryObj); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, apierrors.NewNotFound(configapi.TypeRepository.GroupResource(), repositoryID.Name)
		}
		return nil, false, apierrors.NewInternalError(fmt.Errorf("error getting repository %v: %w", repositoryID, err))
	}

	if !isCreate {
		rev, err := r.cad.UpdatePackage(ctx, &repositoryObj, oldPackage, oldRuntimeObj.(*api.PorchPackage), newObj)
		if err != nil {
			return nil, false, apierrors.NewInternalError(err)
		}

		updated := rev.GetPackage(ctx)

		return updated, false, nil
	} else {
		rev, err := r.cad.CreatePackage(ctx, &repositoryObj, newObj)
		if err != nil {
			klog.Infof("error creating package: %v", err)
			return nil, false, apierrors.NewInternalError(err)
		}

		created := rev.GetPackage(ctx)
		return created, true, nil
	}
}

func (r *packageCommon) validateDelete(ctx context.Context, deleteValidation rest.ValidateObjectFunc, obj runtime.Object,
	name string, ns string) (*configapi.Repository, error) {
	if deleteValidation != nil {
		err := deleteValidation(ctx, obj)
		if err != nil {
			klog.Infof("delete failed validation: %v", err)
			return nil, err
		}
	}
	repoName, err := util.ParsePkgRevObjNameField(name, 0)
	if err != nil {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("invalid name %q", name))
	}
	repositoryObj, err := r.getRepositoryObj(ctx, types.NamespacedName{Name: repoName, Namespace: ns})
	if err != nil {
		return nil, err
	}
	return repositoryObj, nil
}

func (r *packageCommon) validateUpdate(ctx context.Context, newRuntimeObj runtime.Object, oldRuntimeObj runtime.Object, create bool,
	createValidation rest.ValidateObjectFunc, updateValidation rest.ValidateObjectUpdateFunc, kind string, name string) error {
	r.updateStrategy.PrepareForUpdate(ctx, newRuntimeObj, oldRuntimeObj)

	if create {
		if createValidation != nil {
			err := createValidation(ctx, newRuntimeObj)
			if err != nil {
				klog.Infof("update failed create validation: %v", err)
				return err
			}
		}

		fieldErrors := r.createStrategy.Validate(ctx, newRuntimeObj)
		if len(fieldErrors) > 0 {
			return apierrors.NewInvalid(api.SchemeGroupVersion.WithKind(kind).GroupKind(), name, fieldErrors)
		}
	}

	if !create {
		if updateValidation != nil {
			err := updateValidation(ctx, newRuntimeObj, oldRuntimeObj)
			if err != nil {
				klog.Infof("update failed validation: %v", err)
				return err
			}
		}

		fieldErrors := r.updateStrategy.ValidateUpdate(ctx, newRuntimeObj, oldRuntimeObj)
		if len(fieldErrors) > 0 {
			return apierrors.NewInvalid(api.SchemeGroupVersion.WithKind(kind).GroupKind(), name, fieldErrors)
		}
	}

	r.updateStrategy.Canonicalize(newRuntimeObj)
	return nil
}

func getPackageMutexKey(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

func getMutexForPackage(pkgMutexKey string) *sync.Mutex {
	mutexMapMutex.Lock()
	defer mutexMapMutex.Unlock()
	pkgMutex, alreadyPresent := pkgRevOperationMutexes[pkgMutexKey]
	if !alreadyPresent {
		pkgMutex = &sync.Mutex{}
		pkgRevOperationMutexes[pkgMutexKey] = pkgMutex
	}
	return pkgMutex
}
