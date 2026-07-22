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
	"sync/atomic"
	"testing"
	"time"

	configapi "github.com/kptdev/porch/api/porchconfig/v1alpha1"
	"github.com/kptdev/porch/pkg/repository"
	mockcache "github.com/kptdev/porch/test/mockery/mocks/porch/pkg/cache/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBuildCompleteScheme(t *testing.T) {
	scheme, err := buildCompleteScheme()
	require.NoError(t, err)
	require.NotNil(t, scheme)

	// Test singleton behavior - calling again should return same instance
	scheme2, err := buildCompleteScheme()
	require.NoError(t, err)
	assert.Same(t, scheme, scheme2, "expected buildCompleteScheme to return singleton instance")
}

func TestBuildSchemeWithTypes(t *testing.T) {
	tests := []struct {
		name        string
		builders    []schemeBuilder
		expectError bool
	}{
		{
			name: "success with valid builders",
			builders: []schemeBuilder{
				func(s *runtime.Scheme) error {
					return corev1.AddToScheme(s)
				},
			},
			expectError: false,
		},
		{
			name: "error from first builder",
			builders: []schemeBuilder{
				func(s *runtime.Scheme) error {
					return fmt.Errorf("mock error")
				},
			},
			expectError: true,
		},
		{
			name: "error from second builder",
			builders: []schemeBuilder{
				func(s *runtime.Scheme) error {
					return corev1.AddToScheme(s)
				},
				func(s *runtime.Scheme) error {
					return fmt.Errorf("second builder error")
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme, err := buildSchemeWithTypes(tt.builders...)
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "error")
				assert.Nil(t, scheme)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, scheme)
			}
		})
	}
}

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
		}
	}
	return repos
}

func TestWarmupRepositories(t *testing.T) {
	tests := []struct {
		name               string
		repoCount          int
		maxConcurrentLists int
		timeout            time.Duration
		setupMock          func(*mockcache.MockCache, *atomic.Int32, *atomic.Int32)
		assertAfter        func(*testing.T, time.Duration, *atomic.Int32)
	}{
		{
			name:               "opens all repos",
			repoCount:          5,
			maxConcurrentLists: 10,
			timeout:            20 * time.Second,
			setupMock: func(mc *mockcache.MockCache, _, _ *atomic.Int32) {
				mc.EXPECT().OpenRepository(mock.Anything, mock.Anything).Return(nil, nil).Times(5)
			},
		},
		{
			name:               "no repos",
			repoCount:          0,
			maxConcurrentLists: 10,
			timeout:            20 * time.Second,
			setupMock: func(mc *mockcache.MockCache, _, _ *atomic.Int32) {
				// OpenRepository should never be called
			},
		},
		{
			name:               "continues on error",
			repoCount:          3,
			maxConcurrentLists: 10,
			timeout:            20 * time.Second,
			setupMock: func(mc *mockcache.MockCache, _, _ *atomic.Int32) {
				mc.EXPECT().OpenRepository(mock.Anything, mock.MatchedBy(func(r *configapi.Repository) bool {
					return r.Name == "repo-1"
				})).Return(nil, fmt.Errorf("connection refused")).Once()
				mc.EXPECT().OpenRepository(mock.Anything, mock.MatchedBy(func(r *configapi.Repository) bool {
					return r.Name != "repo-1"
				})).Return(nil, nil).Times(2)
			},
		},
		{
			name:               "respects timeout",
			repoCount:          1,
			maxConcurrentLists: 10,
			timeout:            100 * time.Millisecond,
			setupMock: func(mc *mockcache.MockCache, _, _ *atomic.Int32) {
				mc.EXPECT().OpenRepository(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, r *configapi.Repository) (repository.Repository, error) {
						select {
						case <-ctx.Done():
							return nil, ctx.Err()
						case <-time.After(5 * time.Second):
							return nil, nil
						}
					}).Once()
			},
			assertAfter: func(t *testing.T, elapsed time.Duration, _ *atomic.Int32) {
				assert.Less(t, elapsed, 1*time.Second)
			},
		},
		{
			name:               "respects concurrency limit",
			repoCount:          20,
			maxConcurrentLists: 5,
			timeout:            20 * time.Second,
			setupMock: func(mc *mockcache.MockCache, concurrent, maxConcurrent *atomic.Int32) {
				mc.EXPECT().OpenRepository(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, r *configapi.Repository) (repository.Repository, error) {
						cur := concurrent.Add(1)
						for {
							old := maxConcurrent.Load()
							if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
								break
							}
						}
						time.Sleep(10 * time.Millisecond)
						concurrent.Add(-1)
						return nil, nil
					}).Times(20)
			},
			assertAfter: func(t *testing.T, _ time.Duration, maxConcurrent *atomic.Int32) {
				assert.LessOrEqual(t, int(maxConcurrent.Load()), 5, "concurrency should not exceed MaxConcurrentLists")
			},
		},
		{
			name:               "zero concurrency means no limit",
			repoCount:          10,
			maxConcurrentLists: 0,
			timeout:            20 * time.Second,
			setupMock: func(mc *mockcache.MockCache, concurrent, maxConcurrent *atomic.Int32) {
				mc.EXPECT().OpenRepository(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, r *configapi.Repository) (repository.Repository, error) {
						cur := concurrent.Add(1)
						for {
							old := maxConcurrent.Load()
							if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
								break
							}
						}
						time.Sleep(10 * time.Millisecond)
						concurrent.Add(-1)
						return nil, nil
					}).Times(10)
			},
			assertAfter: func(t *testing.T, _ time.Duration, maxConcurrent *atomic.Int32) {
				assert.Greater(t, int(maxConcurrent.Load()), 5, "with no limit, concurrency should be high")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repos := newTestRepos(tt.repoCount)
			objects := make([]runtime.Object, len(repos))
			for i := range repos {
				objects[i] = &repos[i]
			}

			scheme := newTestScheme()
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...).Build()
			mc := mockcache.NewMockCache(t)

			var concurrent atomic.Int32
			var maxConcurrent atomic.Int32

			tt.setupMock(mc, &concurrent, &maxConcurrent)

			s := &PorchServer{
				coreClient:               fakeClient,
				cache:                    mc,
				MaxConcurrentLists:       tt.maxConcurrentLists,
				listTimeoutPerRepository: tt.timeout,
			}

			start := time.Now()
			s.warmupRepositories(context.Background())
			elapsed := time.Since(start)

			if tt.assertAfter != nil {
				tt.assertAfter(t, elapsed, &maxConcurrent)
			}
		})
	}
}
