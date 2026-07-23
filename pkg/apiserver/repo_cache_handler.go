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
	"sync"
	"time"

	configapi "github.com/kptdev/porch/api/porchconfig/v1alpha1"
	cachetypes "github.com/kptdev/porch/pkg/cache/types"
	"golang.org/x/sync/semaphore"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	minReconnectDelay     = 1 * time.Second
	maxReconnectDelay     = 30 * time.Second
	defaultMaxConcurrency = 5
)

// repoCacheHandler watches Repository CRs and keeps the in-memory cache in sync
// by opening repos on add/modify and closing them on delete.
type repoCacheHandler struct {
	coreClient      client.WithWatch
	cache           cachetypes.Cache
	workerSemaphore *semaphore.Weighted
	timeoutPerRepo  time.Duration
	// Per-repo mutexes ensure events for the same repo are processed in order.
	repoMutexes     map[string]*sync.Mutex
	repoMutexesLock sync.Mutex
}

// runRepoCacheHandler starts the repo handler loop. It watches Repository CRs
// and opens/closes them in the cache as they appear or disappear.
// maxConcurrency controls how many repo operations can run in parallel.
// timeoutPerRepo is the max duration for a single repo open/close operation.
// This function launches a goroutine and returns immediately.
func runRepoCacheHandler(ctx context.Context, coreClient client.WithWatch, cache cachetypes.Cache, maxConcurrency int, timeoutPerRepo time.Duration) {
	if maxConcurrency <= 0 {
		maxConcurrency = defaultMaxConcurrency
	}
	h := &repoCacheHandler{
		coreClient:      coreClient,
		cache:           cache,
		workerSemaphore: semaphore.NewWeighted(int64(maxConcurrency)),
		timeoutPerRepo:  timeoutPerRepo,
		repoMutexes:     make(map[string]*sync.Mutex),
	}
	go h.run(ctx)
}

// run is the main loop: watches for Repository CR events and opens/closes the cache.
func (h *repoCacheHandler) run(ctx context.Context) {
	klog.Infof("Repo cache handler starting")

	var events <-chan watch.Event
	var watcher watch.Interface
	var bookmark string
	var consecutiveFailures int

	defer func() {
		if watcher != nil {
			watcher.Stop()
		}
	}()

	reconnect := newBackoffTimer(minReconnectDelay, maxReconnectDelay)
	defer reconnect.Stop()

loop:
	for {
		select {
		case <-reconnect.channel():
			if consecutiveFailures >= 3 {
				klog.Warningf("Repo cache handler: resetting bookmark after %d consecutive failures, was %q", consecutiveFailures, bookmark)
				bookmark = ""
				consecutiveFailures = 0
			}

			klog.Infof("Repo cache handler: starting Repository watch (bookmark=%q)", bookmark)
			var obj configapi.RepositoryList
			var err error
			watcher, err = h.coreClient.Watch(ctx, &obj, &client.ListOptions{
				Raw: &v1.ListOptions{
					AllowWatchBookmarks: true,
					ResourceVersion:     bookmark,
				},
			})
			if err != nil {
				consecutiveFailures++
				if apierrors.IsResourceExpired(err) || apierrors.IsGone(err) {
					klog.Warningf("Repo cache handler: watch start failed (expired/gone): %v. Resetting bookmark.", err)
					bookmark = ""
					consecutiveFailures = 0
				} else {
					klog.Errorf("Repo cache handler: cannot start Repository watch: %v; will retry", err)
				}
				reconnect.backoff()
			} else {
				klog.Infof("Repo cache handler: Repository watch started successfully")
				events = watcher.ResultChan()
			}

		case event, eventOk := <-events:
			if eventOk {
				h.handleWatchEvent(ctx, event, &bookmark, &consecutiveFailures, reconnect)
			} else {
				klog.Errorf("Repo cache handler: watch event stream closed. Will restart from bookmark %q", bookmark)
				watcher.Stop()
				events = nil
				watcher = nil
				consecutiveFailures++
				reconnect.backoff()
			}

		case <-ctx.Done():
			klog.Infof("Repo cache handler exiting: %v", ctx.Err())
			break loop
		}
	}
}

// handleWatchEvent processes a single watch event.
func (h *repoCacheHandler) handleWatchEvent(ctx context.Context, event watch.Event, bookmark *string, consecutiveFailures *int, reconnect *backoffTimer) {
	switch event.Type {
	case watch.Bookmark:
		if repository, ok := event.Object.(*configapi.Repository); ok {
			*consecutiveFailures = 0
			*bookmark = repository.ResourceVersion
			klog.V(2).Infof("Repo cache handler: bookmark updated to %q", *bookmark)
		}

	case watch.Error:
		if status, ok := event.Object.(*v1.Status); ok {
			if status.Reason == v1.StatusReasonExpired || status.Reason == v1.StatusReasonGone {
				klog.Warningf("Repo cache handler: watch error %s (code %d): %s. Resetting bookmark.", status.Reason, status.Code, status.Message)
				*bookmark = ""
				*consecutiveFailures = 0
			} else {
				klog.Errorf("Repo cache handler: watch error %s (code %d): %s", status.Reason, status.Code, status.Message)
				(*consecutiveFailures)++
			}
		} else {
			klog.Errorf("Repo cache handler: watch error with unexpected object type: %T", event.Object)
			(*consecutiveFailures)++
		}
		reconnect.reset()

	default: // ADDED, MODIFIED, DELETED
		if repository, ok := event.Object.(*configapi.Repository); ok {
			*consecutiveFailures = 0
			h.processEvent(ctx, event.Type, repository)
		} else {
			klog.V(5).Infof("Repo cache handler: unexpected watch event object type: %T", event.Object)
		}
	}
}

// processEvent dispatches a repo event into a goroutine with per-repo mutex ordering.
func (h *repoCacheHandler) processEvent(ctx context.Context, eventType watch.EventType, repo *configapi.Repository) {
	repoKey := fmt.Sprintf("%s/%s", repo.Namespace, repo.Name)

	go func() {
		mutex := h.getRepoMutex(repoKey)
		mutex.Lock()
		defer mutex.Unlock()

		h.handleRepositoryEvent(ctx, eventType, repo)
	}()
}

// getRepoMutex returns a per-repository mutex, creating one if it doesn't exist.
func (h *repoCacheHandler) getRepoMutex(repoKey string) *sync.Mutex {
	h.repoMutexesLock.Lock()
	defer h.repoMutexesLock.Unlock()

	mutex, exists := h.repoMutexes[repoKey]
	if !exists {
		mutex = &sync.Mutex{}
		h.repoMutexes[repoKey] = mutex
	}
	return mutex
}

// handleRepositoryEvent opens or closes a repository in the cache.
func (h *repoCacheHandler) handleRepositoryEvent(ctx context.Context, eventType watch.EventType, repo *configapi.Repository) {
	repoKey := fmt.Sprintf("%s/%s", repo.Namespace, repo.Name)

	// Apply per-repo timeout to prevent hung operations from blocking indefinitely
	if h.timeoutPerRepo > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, h.timeoutPerRepo)
		defer cancel()
	}

	switch eventType {
	case watch.Added, watch.Modified:
		// Skip repos that are being deleted — a DELETE event will follow shortly.
		if repo.DeletionTimestamp != nil {
			klog.V(3).Infof("Repo cache handler: skipping %s (deletion in progress)", repoKey)
			return
		}

		// Only cache repos that the controller has confirmed as Ready.
		// This avoids wasting time cloning repos with bad URLs or unreachable servers.
		if !isRepositoryReady(repo) {
			klog.V(3).Infof("Repo cache handler: skipping %s (not Ready)", repoKey)
			return
		}

		start := time.Now()
		klog.Infof("Repo cache handler: starting %s event for %s", eventType, repoKey)

		// Rate limit concurrent git operations
		if err := h.workerSemaphore.Acquire(ctx, 1); err != nil {
			klog.Warningf("Repo cache handler: context cancelled waiting for semaphore for %s: %v [%s]", repoKey, err, time.Since(start))
			return
		}
		defer h.workerSemaphore.Release(1)

		if err := h.cache.CreateCachedRepository(ctx, repo); err != nil {
			klog.Warningf("Repo cache handler: failed to cache %s: %v [%s]", repoKey, err, time.Since(start))
		} else {
			klog.Infof("Repo cache handler: finished %s event for %s [%s]", eventType, repoKey, time.Since(start))
		}

	case watch.Deleted:
		start := time.Now()
		klog.Infof("Repo cache handler: starting %s event for %s", eventType, repoKey)

		if err := h.workerSemaphore.Acquire(ctx, 1); err != nil {
			klog.Warningf("Repo cache handler: context cancelled waiting for semaphore for %s: %v [%s]", repoKey, err, time.Since(start))
			return
		}
		defer h.workerSemaphore.Release(1)

		if err := h.cache.EvictCachedRepository(ctx, repo); err != nil {
			klog.Warningf("Repo cache handler: failed to evict %s: %v [%s]", repoKey, err, time.Since(start))
		} else {
			klog.Infof("Repo cache handler: finished %s event for %s [%s]", eventType, repoKey, time.Since(start))
		}
	}
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

// backoffTimer implements exponential backoff for watch reconnection.
type backoffTimer struct {
	min, max, curr time.Duration
	timer          *time.Timer
}

func newBackoffTimer(min, max time.Duration) *backoffTimer {
	return &backoffTimer{
		min:   min,
		max:   max,
		curr:  min,
		timer: time.NewTimer(min),
	}
}

func (t *backoffTimer) Stop() bool {
	return t.timer.Stop()
}

func (t *backoffTimer) channel() <-chan time.Time {
	return t.timer.C
}

func (t *backoffTimer) reset() bool {
	t.curr = t.min
	return t.timer.Reset(t.curr)
}

func (t *backoffTimer) backoff() bool {
	curr := t.curr * 2
	if curr > t.max {
		curr = t.max
	}
	t.curr = curr
	return t.timer.Reset(curr)
}
