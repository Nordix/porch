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

package apiserver

import (
	"context"
	"fmt"
	"time"

	configapi "github.com/kptdev/porch/api/porchconfig/v1alpha1"
	cachetypes "github.com/kptdev/porch/pkg/cache/types"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	minReconnectDelay = 1 * time.Second
	maxReconnectDelay = 30 * time.Second
)

// repoCacheHandler watches Repository CRs for DELETE events and evicts them
// from porch-server's in-memory cache to prevent git clone leaks.
// ADD/MODIFIED are not handled — the cache is populated lazily on first API call.
type repoCacheHandler struct {
	coreClient client.WithWatch
	cache      cachetypes.Cache
	minBackoff time.Duration
	maxBackoff time.Duration
}

// runRepoCacheHandler starts the repo cache handler loop. It watches Repository CRs
// and evicts them from the cache when deleted.
// This function launches a goroutine and returns immediately.
func runRepoCacheHandler(ctx context.Context, coreClient client.WithWatch, cache cachetypes.Cache) {
	h := &repoCacheHandler{
		coreClient: coreClient,
		cache:      cache,
		minBackoff: minReconnectDelay,
		maxBackoff: maxReconnectDelay,
	}
	go h.run(ctx)
}

// run is the main loop: watches for Repository CR delete events.
func (h *repoCacheHandler) run(ctx context.Context) {
	klog.Infof("Repo cache handler starting (eviction only)")

	var events <-chan watch.Event
	var watcher watch.Interface
	var bookmark string
	var consecutiveFailures int

	defer func() {
		if watcher != nil {
			watcher.Stop()
		}
	}()

	reconnect := newBackoffTimer(h.minBackoff, h.maxBackoff)
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

	case watch.Deleted:
		if repo, ok := event.Object.(*configapi.Repository); ok {
			*consecutiveFailures = 0
			h.evictRepository(ctx, repo)
		}

	default:
		// ADDED/MODIFIED — ignored. Cache is populated lazily on first API call.
	}
}

// evictRepository removes a repository from the in-memory cache and releases the git clone.
func (h *repoCacheHandler) evictRepository(ctx context.Context, repo *configapi.Repository) {
	repoKey := fmt.Sprintf("%s/%s", repo.Namespace, repo.Name)
	start := time.Now()

	klog.Infof("Repo cache handler: starting eviction for %s", repoKey)

	if err := h.cache.EvictCachedRepository(ctx, repo); err != nil {
		klog.Warningf("Repo cache handler: failed to evict %s: %v [%s]", repoKey, err, time.Since(start))
	} else {
		klog.Infof("Repo cache handler: finished eviction for %s [%s]", repoKey, time.Since(start))
	}
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
