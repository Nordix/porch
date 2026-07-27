// Copyright 2022, 2025-2026 The kpt Authors
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

package apiserver

import (
	"context"
	"fmt"
	"time"

	configapi "github.com/kptdev/porch/api/porchconfig/v1alpha1"
	cachetypes "github.com/kptdev/porch/pkg/cache/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const repoCacheFinalizer = "config.porch.kpt.dev/porch-server"

// RepoCacheReconciler watches Repository CRs and manages the porch-server
// in-memory cache:
//   - When the repo controller marks a Repository as Ready: opens it in the cache
//     and adds a finalizer to guarantee cleanup.
//   - When a Repository is being deleted (DeletionTimestamp set): evicts from cache
//     and removes the finalizer so the object can be garbage collected.
//   - When a spec change occurs (generation change): re-opens the repository.
type RepoCacheReconciler struct {
	client client.Client
	cache  cachetypes.Cache
}

func (r *RepoCacheReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	repo := &configapi.Repository{}
	if err := r.client.Get(ctx, req.NamespacedName, repo); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// Skip repos managed by v1alpha2 — porch-server should not handle these
	if repo.Annotations[configapi.AnnotationKeyV1Alpha2Migration] == configapi.AnnotationValueMigrationEnabled {
		return reconcile.Result{}, nil
	}

	// Handle deletion — evict from cache and remove finalizer
	if repo.DeletionTimestamp != nil {
		klog.Infof("Repo cache handler: evicting %s", req.NamespacedName)
		start := time.Now()

		if err := r.cache.EvictCachedRepository(ctx, req.Namespace, req.Name); err != nil {
			klog.Warningf("Repo cache handler: failed to evict %s: %v [%s]", req.NamespacedName, err, time.Since(start))
			return reconcile.Result{}, err
		}

		if controllerutil.ContainsFinalizer(repo, repoCacheFinalizer) {
			controllerutil.RemoveFinalizer(repo, repoCacheFinalizer)
			if err := r.client.Update(ctx, repo); err != nil {
				return reconcile.Result{}, fmt.Errorf("failed to remove finalizer from %s: %w", req.NamespacedName, err)
			}
		}

		klog.Infof("Repo cache handler: evicted %s [%s]", req.NamespacedName, time.Since(start))
		return reconcile.Result{}, nil
	}

	// Only act on repos that are Ready (repo controller has validated connectivity)
	if !isRepositoryReady(repo) {
		return reconcile.Result{}, nil
	}

	// Add finalizer first — guarantees eviction even if we crash after OpenRepository
	if !controllerutil.ContainsFinalizer(repo, repoCacheFinalizer) {
		controllerutil.AddFinalizer(repo, repoCacheFinalizer)
		if err := r.client.Update(ctx, repo); err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to add finalizer to %s: %w", req.NamespacedName, err)
		}
	}

	// Open (or re-open on spec change) the repository in the cache
	start := time.Now()
	klog.Infof("Repo cache handler: opening %s", req.NamespacedName)

	if _, err := r.cache.OpenRepository(ctx, repo); err != nil {
		klog.Warningf("Repo cache handler: failed to open %s: %v [%s]", req.NamespacedName, err, time.Since(start))
		return reconcile.Result{}, err
	}

	klog.Infof("Repo cache handler: opened %s [%s]", req.NamespacedName, time.Since(start))

	return reconcile.Result{}, nil
}

// setupRepoCacheController registers the repo cache controller with the given manager.
func setupRepoCacheController(mgr ctrl.Manager, cache cachetypes.Cache, maxConcurrency int) error {
	if maxConcurrency <= 0 {
		maxConcurrency = 20
	}
	r := &RepoCacheReconciler{
		client: mgr.GetClient(),
		cache:  cache,
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&configapi.Repository{}).
		WithEventFilter(repoCachePredicate()).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxConcurrency,
		}).
		Named("repo-cache").
		Complete(r)
}

// isRepositoryReady checks if the repo controller has marked this repository as Ready.
func isRepositoryReady(repo *configapi.Repository) bool {
	for _, c := range repo.Status.Conditions {
		if c.Type == configapi.RepositoryReady && c.Status == "True" {
			return true
		}
	}
	return false
}

// repoCachePredicate filters events for the repo cache controller.
// - Create: passed (handles startup resync and new repos)
// - Update: passed (handles Ready status, spec changes, DeletionTimestamp)
// - Delete: ignored (handled via finalizer on DeletionTimestamp update)
// - Generic: ignored
func repoCachePredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}
}
