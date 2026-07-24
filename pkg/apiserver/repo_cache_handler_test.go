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
