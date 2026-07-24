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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	configapi "github.com/kptdev/porch/api/porchconfig/v1alpha1"
	"github.com/kptdev/porch/pkg/repository"
	mockcache "github.com/kptdev/porch/test/mockery/mocks/porch/pkg/cache/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newGCTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = configapi.AddToScheme(scheme)
	return scheme
}

func TestNewRepositoryGC(t *testing.T) {
	mc := mockcache.NewMockCache(t)
	scheme := newGCTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	gc := newRepositoryGC(fakeClient, mc, 5*time.Minute)

	assert.NotNil(t, gc)
	assert.Equal(t, 5*time.Minute, gc.interval)
}

func TestNewRepositoryGCDefaultInterval(t *testing.T) {
	mc := mockcache.NewMockCache(t)
	scheme := newGCTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	gc := newRepositoryGC(fakeClient, mc, 0)

	assert.Equal(t, defaultGCInterval, gc.interval)
}

func TestGCRunNoOrphans(t *testing.T) {
	ctx := context.Background()
	mc := mockcache.NewMockCache(t)
	scheme := newGCTestScheme()

	// Repo exists in both DB and K8s — not orphaned
	repos := []configapi.Repository{
		{ObjectMeta: metav1.ObjectMeta{Name: "repo-1", Namespace: "ns-1"}},
	}
	objects := make([]runtime.Object, len(repos))
	for i := range repos {
		objects[i] = &repos[i]
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...).Build()

	dbKeys := []repository.RepositoryKey{
		{Namespace: "ns-1", Name: "repo-1"},
	}
	mc.EXPECT().ListDBRepositories(mock.Anything).Return(dbKeys, nil).Once()
	// No DeleteDBRepository call expected

	gc := newRepositoryGC(fakeClient, mc, 5*time.Minute)
	gc.run(ctx)
}

func TestGCRunDeletesOrphans(t *testing.T) {
	ctx := context.Background()
	mc := mockcache.NewMockCache(t)
	scheme := newGCTestScheme()

	// Only repo-1 exists in K8s, but DB has repo-1 and repo-2
	repos := []configapi.Repository{
		{ObjectMeta: metav1.ObjectMeta{Name: "repo-1", Namespace: "ns-1"}},
	}
	objects := make([]runtime.Object, len(repos))
	for i := range repos {
		objects[i] = &repos[i]
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...).Build()

	dbKeys := []repository.RepositoryKey{
		{Namespace: "ns-1", Name: "repo-1"},
		{Namespace: "ns-1", Name: "repo-2"},
	}
	mc.EXPECT().ListDBRepositories(mock.Anything).Return(dbKeys, nil).Once()
	mc.EXPECT().DeleteDBRepository(mock.Anything, repository.RepositoryKey{Namespace: "ns-1", Name: "repo-2"}).Return(nil).Once()

	gc := newRepositoryGC(fakeClient, mc, 5*time.Minute)
	gc.run(ctx)
}

func TestGCRunMultipleOrphans(t *testing.T) {
	ctx := context.Background()
	mc := mockcache.NewMockCache(t)
	scheme := newGCTestScheme()

	// No repos in K8s at all
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	dbKeys := []repository.RepositoryKey{
		{Namespace: "ns-1", Name: "repo-1"},
		{Namespace: "ns-2", Name: "repo-2"},
		{Namespace: "ns-1", Name: "repo-3"},
	}
	mc.EXPECT().ListDBRepositories(mock.Anything).Return(dbKeys, nil).Once()
	mc.EXPECT().DeleteDBRepository(mock.Anything, mock.Anything).Return(nil).Times(3)

	gc := newRepositoryGC(fakeClient, mc, 5*time.Minute)
	gc.run(ctx)
}

func TestGCRunEmptyDB(t *testing.T) {
	ctx := context.Background()
	mc := mockcache.NewMockCache(t)
	scheme := newGCTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mc.EXPECT().ListDBRepositories(mock.Anything).Return([]repository.RepositoryKey{}, nil).Once()
	// No further calls expected

	gc := newRepositoryGC(fakeClient, mc, 5*time.Minute)
	gc.run(ctx)
}

func TestGCRunListDBError(t *testing.T) {
	ctx := context.Background()
	mc := mockcache.NewMockCache(t)
	scheme := newGCTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mc.EXPECT().ListDBRepositories(mock.Anything).Return(nil, assert.AnError).Once()
	// Should not call DeleteDBRepository

	gc := newRepositoryGC(fakeClient, mc, 5*time.Minute)
	gc.run(ctx)
}

func TestGCRunListCRError(t *testing.T) {
	ctx := context.Background()
	mc := mockcache.NewMockCache(t)

	// Use a mock client that fails on List
	mockClient := &failingListClient{}

	dbKeys := []repository.RepositoryKey{
		{Namespace: "ns-1", Name: "repo-1"},
	}
	mc.EXPECT().ListDBRepositories(mock.Anything).Return(dbKeys, nil).Once()
	// Should not call DeleteDBRepository since List failed

	gc := newRepositoryGC(mockClient, mc, 5*time.Minute)
	gc.run(ctx)
}

// failingListClient is a minimal client.Client that fails on List
type failingListClient struct {
	client.Client
}

func (f *failingListClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return assert.AnError
}

func TestGCStartStopsOnContextCancel(t *testing.T) {
	mc := mockcache.NewMockCache(t)
	scheme := newGCTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// GC will run once immediately — DB is empty so no deletions
	mc.EXPECT().ListDBRepositories(mock.Anything).Return([]repository.RepositoryKey{}, nil).Maybe()

	gc := newRepositoryGC(fakeClient, mc, 100*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- gc.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	err := <-done
	require.NoError(t, err)
}
