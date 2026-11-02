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
	"testing"

	porchv1alpha2 "github.com/kptdev/porch/api/porch/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// V1Alpha2Scheme returns a runtime.Scheme with the v1alpha2 API registered.
// It calls t.Fatal on error.
func V1Alpha2Scheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := porchv1alpha2.AddToScheme(scheme); err != nil {
		t.Fatalf("error creating v1alpha2 scheme: %v", err)
	}
	return scheme
}

// NewV1Alpha2PackageRevision builds a v1alpha2 PackageRevision with the
// TypeMeta pre-filled. Callers set Spec/Status/ObjectMeta fields on the
// returned object as needed.
func NewV1Alpha2PackageRevision(ns, name string) *porchv1alpha2.PackageRevision {
	return &porchv1alpha2.PackageRevision{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PackageRevision",
			APIVersion: porchv1alpha2.SchemeGroupVersion.Identifier(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
		},
	}
}

// NewV1Alpha2FakeClientWithPkgRevIndexes builds a fake client that honors the
// spec.repository and spec.packageName PackageRevision field selectors,
// mirroring the field indexes the real porch apiserver pushes down to the
// database. The controller-runtime fake client ignores (in fact rejects)
// MatchingFields on an unregistered index, so any test exercising a
// field-selector-scoped List (e.g. PackageAlreadyExistsV1Alpha2) must build its
// client through this helper.
func NewV1Alpha2FakeClientWithPkgRevIndexes(scheme *runtime.Scheme, objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithIndex(&porchv1alpha2.PackageRevision{}, string(porchv1alpha2.PkgRevSelectorRepository),
			func(o client.Object) []string {
				return []string{o.(*porchv1alpha2.PackageRevision).Spec.RepositoryName}
			}).
		WithIndex(&porchv1alpha2.PackageRevision{}, string(porchv1alpha2.PkgRevSelectorPackageName),
			func(o client.Object) []string {
				return []string{o.(*porchv1alpha2.PackageRevision).Spec.PackageName}
			}).
		WithObjects(objs...).
		Build()
}
