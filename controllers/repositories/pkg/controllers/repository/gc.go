// Copyright 2026 The kpt Authors
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

package repository

import (
	"context"
	"time"

	api "github.com/kptdev/porch/api/porchconfig/v1alpha1"
	cachetypes "github.com/kptdev/porch/pkg/cache/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// repositoryGC periodically removes orphaned repositories from the cache/DB
// that no longer have a corresponding Repository CR in Kubernetes.
type repositoryGC struct {
	client   client.Client
	cache    cachetypes.Cache
	interval time.Duration
}

func newRepositoryGC(client client.Client, cache cachetypes.Cache, interval time.Duration) *repositoryGC {
	if interval <= 0 {
		interval = defaultGCInterval
	}
	return &repositoryGC{
		client:   client,
		cache:    cache,
		interval: interval,
	}
}

// Start implements manager.Runnable. It runs the GC loop until the context is cancelled.
func (gc *repositoryGC) Start(ctx context.Context) error {
	log := log.FromContext(ctx).WithName("repository-gc")
	log.Info("Repository GC started", "interval", gc.interval)

	// Run immediately on startup
	gc.run(ctx)

	ticker := time.NewTicker(gc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("Repository GC stopped")
			return nil
		case <-ticker.C:
			gc.run(ctx)
		}
	}
}

func (gc *repositoryGC) run(ctx context.Context) {
	log := log.FromContext(ctx).WithName("repository-gc")

	// Get all repo keys from the database
	dbRepoKeys, err := gc.cache.ListDBRepositories(ctx)
	if err != nil {
		log.Error(err, "Failed to list DB repository keys for GC")
		return
	}
	if len(dbRepoKeys) == 0 {
		return
	}

	// List all Repository CRs from Kubernetes
	var repoList api.RepositoryList
	if err := gc.client.List(ctx, &repoList); err != nil {
		log.Error(err, "Failed to list Repository CRs for GC")
		return
	}

	// Build a set of existing CRs (namespace/name)
	existingRepos := make(map[string]struct{}, len(repoList.Items))
	for i := range repoList.Items {
		key := repoList.Items[i].Namespace + "/" + repoList.Items[i].Name
		existingRepos[key] = struct{}{}
	}

	// Find orphaned repos (in DB but no CR)
	for _, repoKey := range dbRepoKeys {
		key := repoKey.Namespace + "/" + repoKey.Name
		if _, exists := existingRepos[key]; !exists {
			log.Info("GC: deleting orphaned repository from DB", "repository", key)
			if err := gc.cache.DeleteDBRepository(ctx, repoKey); err != nil {
				log.Error(err, "GC: failed to delete orphaned repository", "repository", key)
			}
		}
	}
}
