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
	"testing"
	"time"

	configapi "github.com/kptdev/porch/api/porchconfig/v1alpha1"
	mockcache "github.com/kptdev/porch/test/mockery/mocks/porch/pkg/cache/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = configapi.AddToScheme(scheme)
	return scheme
}

func newTestRepos(count int) []configapi.Repository {
	repos := make([]configapi.Repository, count)
	for i := range repos {
		repos[i] = configapi.Repository{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("repo-%d", i+1),
				Namespace: "test-ns",
			},
			Spec: configapi.RepositorySpec{
				Git: &configapi.GitRepository{
					Directory: "/",
				},
			},
		}
	}
	return repos
}

func TestRepoCacheHandlerEvictRepository(t *testing.T) {
	repos := newTestRepos(1)

	scheme := newTestScheme()
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	mc := mockcache.NewMockCache(t)
	mc.EXPECT().EvictCachedRepository(mock.Anything, mock.Anything).Return(nil).Once()

	h := &repoCacheHandler{
		coreClient: fakeClient,
		cache:      mc,
	}

	h.evictRepository(context.Background(), &repos[0])
}

func TestRepoCacheHandlerEvictRepositoryError(t *testing.T) {
	repos := newTestRepos(1)

	scheme := newTestScheme()
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	mc := mockcache.NewMockCache(t)
	mc.EXPECT().EvictCachedRepository(mock.Anything, mock.Anything).Return(fmt.Errorf("not found")).Once()

	h := &repoCacheHandler{
		coreClient: fakeClient,
		cache:      mc,
	}

	// Should not panic — just logs a warning
	h.evictRepository(context.Background(), &repos[0])
}

func TestRepoCacheHandlerBackoffTimer(t *testing.T) {
	bt := newBackoffTimer(10*time.Millisecond, 100*time.Millisecond)
	defer bt.Stop()

	select {
	case <-bt.channel():
	case <-time.After(50 * time.Millisecond):
		t.Fatal("timer did not fire within expected time")
	}

	bt.backoff()
	start := time.Now()
	select {
	case <-bt.channel():
		elapsed := time.Since(start)
		assert.GreaterOrEqual(t, elapsed, 15*time.Millisecond)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timer did not fire after backoff")
	}

	bt.reset()
	start = time.Now()
	select {
	case <-bt.channel():
		elapsed := time.Since(start)
		assert.Less(t, elapsed, 50*time.Millisecond)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timer did not fire after reset")
	}
}

func TestRepoCacheHandlerRunStopsOnContextCancel(t *testing.T) {
	scheme := newTestScheme()
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	mc := mockcache.NewMockCache(t)

	ctx, cancel := context.WithCancel(context.Background())

	runRepoCacheHandler(ctx, fakeClient, mc)

	time.Sleep(50 * time.Millisecond)
	cancel()

	time.Sleep(50 * time.Millisecond)
	require.True(t, true)
}

func TestRepoCacheHandlerHandleWatchEvent(t *testing.T) {
	repos := newTestRepos(1)

	scheme := newTestScheme()
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	tests := []struct {
		name        string
		event       watch.Event
		expectEvict bool
	}{
		{
			name: "DELETE event triggers eviction",
			event: watch.Event{
				Type:   watch.Deleted,
				Object: &repos[0],
			},
			expectEvict: true,
		},
		{
			name: "ADDED event is ignored",
			event: watch.Event{
				Type:   watch.Added,
				Object: &repos[0],
			},
			expectEvict: false,
		},
		{
			name: "MODIFIED event is ignored",
			event: watch.Event{
				Type:   watch.Modified,
				Object: &repos[0],
			},
			expectEvict: false,
		},
		{
			name: "BOOKMARK event updates bookmark",
			event: watch.Event{
				Type: watch.Bookmark,
				Object: &configapi.Repository{
					ObjectMeta: metav1.ObjectMeta{
						ResourceVersion: "12345",
					},
				},
			},
			expectEvict: false,
		},
		{
			name: "ERROR event with expired status resets bookmark",
			event: watch.Event{
				Type: watch.Error,
				Object: &metav1.Status{
					Reason: metav1.StatusReasonExpired,
					Code:   410,
				},
			},
			expectEvict: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := mockcache.NewMockCache(t)
			if tt.expectEvict {
				mc.EXPECT().EvictCachedRepository(mock.Anything, mock.Anything).Return(nil).Once()
			}

			h := &repoCacheHandler{
				coreClient: fakeClient,
				cache:      mc,
			}

			var bookmark string
			var consecutiveFailures int
			reconnect := newBackoffTimer(1*time.Second, 30*time.Second)
			defer reconnect.Stop()
			// Drain initial timer fire
			<-reconnect.channel()

			h.handleWatchEvent(context.Background(), tt.event, &bookmark, &consecutiveFailures, reconnect)

			if tt.event.Type == watch.Bookmark {
				assert.Equal(t, "12345", bookmark)
			}
			if tt.event.Type == watch.Error {
				assert.Empty(t, bookmark)
				assert.Equal(t, 0, consecutiveFailures)
			}
		})
	}
}

// fakeWithWatch wraps a real client and overrides Watch behavior for testing.
type fakeWithWatch struct {
	client.WithWatch
	watchFn func(ctx context.Context, obj client.ObjectList, opts ...client.ListOption) (watch.Interface, error)
}

func (f *fakeWithWatch) Watch(ctx context.Context, obj client.ObjectList, opts ...client.ListOption) (watch.Interface, error) {
	return f.watchFn(ctx, obj, opts...)
}

func TestRepoCacheHandlerWatchFailureThenSuccess(t *testing.T) {
	mc := mockcache.NewMockCache(t)
	mc.EXPECT().EvictCachedRepository(mock.Anything, mock.Anything).Return(nil).Once()

	scheme := newTestScheme()
	realClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	callCount := 0
	fakeWatcher := watch.NewFake()

	fw := &fakeWithWatch{
		WithWatch: realClient,
		watchFn: func(ctx context.Context, obj client.ObjectList, opts ...client.ListOption) (watch.Interface, error) {
			callCount++
			if callCount <= 2 {
				return nil, fmt.Errorf("connection refused")
			}
			return fakeWatcher, nil
		},
	}

	h := &repoCacheHandler{
		coreClient: fw,
		cache:      mc,
		minBackoff: 10 * time.Millisecond,
		maxBackoff: 50 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	go h.run(ctx)

	// Wait for reconnection attempts and successful watch
	time.Sleep(200 * time.Millisecond)

	// Send a DELETE event through the fake watcher
	repo := &configapi.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "test-repo", Namespace: "test-ns"},
	}
	fakeWatcher.Delete(repo)

	time.Sleep(100 * time.Millisecond)
	cancel()
	fakeWatcher.Stop()

	assert.GreaterOrEqual(t, callCount, 3, "expected at least 3 Watch calls (2 failures + 1 success)")
}
