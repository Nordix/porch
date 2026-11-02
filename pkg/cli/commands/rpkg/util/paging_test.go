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

package util

import (
	"context"
	"testing"

	porchv1alpha2 "github.com/kptdev/porch/api/porch/v1alpha2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// TestListAllPaged verifies the helper drives limit/continue itself: it follows
// the server's continue token across pages, sends client.Limit each page, and
// accumulates every item into the caller's list with a cleared continue token.
func TestListAllPaged(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, porchv1alpha2.AddToScheme(scheme))

	pr := func(name string) *porchv1alpha2.PackageRevision {
		return &porchv1alpha2.PackageRevision{
			TypeMeta: metav1.TypeMeta{
				Kind:       "PackageRevision",
				APIVersion: porchv1alpha2.SchemeGroupVersion.Identifier(),
			},
			ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: name},
		}
	}

	// The fake client has no real pagination, so an interceptor simulates a
	// two-page server: page 1 (two items + continue token), page 2 (one item,
	// no token). It records the limit/continue seen each call.
	type call struct {
		limit     int64
		continue_ string
	}
	var calls []call
	page := 0
	funcs := interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			lo := &client.ListOptions{}
			for _, o := range opts {
				o.ApplyToList(lo)
			}
			calls = append(calls, call{limit: lo.Limit, continue_: lo.Continue})
			prl := list.(*porchv1alpha2.PackageRevisionList)
			page++
			if page == 1 {
				prl.Items = []porchv1alpha2.PackageRevision{*pr("a"), *pr("b")}
				prl.Continue = "token-2"
			} else {
				prl.Items = []porchv1alpha2.PackageRevision{*pr("c")}
				prl.Continue = ""
			}
			return nil
		},
	}

	c := NewV1Alpha2FakeClientWithPkgRevIndexesAndInterceptor(scheme, funcs)

	var out porchv1alpha2.PackageRevisionList
	require.NoError(t, ListAllPaged(context.Background(), c, &out, 2, client.InNamespace("ns")))

	// Two pages fetched, following the continue token.
	require.Len(t, calls, 2)
	assert.Equal(t, int64(2), calls[0].limit)
	assert.Empty(t, calls[0].continue_)
	assert.Equal(t, int64(2), calls[1].limit)
	assert.Equal(t, "token-2", calls[1].continue_)

	// All items accumulated, continue token cleared on the result.
	names := make([]string, 0, len(out.Items))
	for _, it := range out.Items {
		names = append(names, it.Name)
	}
	assert.ElementsMatch(t, []string{"a", "b", "c"}, names)
	assert.Empty(t, out.GetContinue())
}

// TestListAllPagedDefaultPageSize verifies pageSize <= 0 falls back to the default.
func TestListAllPagedDefaultPageSize(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, porchv1alpha2.AddToScheme(scheme))

	var seenLimit int64
	funcs := interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			lo := &client.ListOptions{}
			for _, o := range opts {
				o.ApplyToList(lo)
			}
			seenLimit = lo.Limit
			return nil
		},
	}
	c := NewV1Alpha2FakeClientWithPkgRevIndexesAndInterceptor(scheme, funcs)

	var out porchv1alpha2.PackageRevisionList
	require.NoError(t, ListAllPaged(context.Background(), c, &out, 0))
	assert.Equal(t, DefaultListPageSize, seenLimit)
}
