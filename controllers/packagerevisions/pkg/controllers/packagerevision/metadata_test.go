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

package packagerevision

import (
	"fmt"
	"testing"

	kptfilev1 "github.com/kptdev/kpt/api/kptfile/v1"
	porchv1alpha2 "github.com/kptdev/porch/api/porch/v1alpha2"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

func TestApplyPackageMetadataToKptfile(t *testing.T) {
	tests := []struct {
		name          string
		kf            *kptfilev1.KptFile
		pr            *porchv1alpha2.PackageRevision
		expectChanged bool
		expectLabels  map[string]string
		expectAnnos   map[string]string
	}{
		{
			name: "no metadata in PR",
			kf:   &kptfilev1.KptFile{},
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: nil,
				},
			},
			expectChanged: false,
		},
		{
			name: "add labels to empty kptfile",
			kf:   &kptfilev1.KptFile{},
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{
						Labels: map[string]string{"app": "myapp"},
					},
				},
			},
			expectChanged: true,
			expectLabels:  map[string]string{"app": "myapp"},
		},
		{
			name: "merge labels",
			kf: &kptfilev1.KptFile{
				ResourceMeta: yaml.ResourceMeta{
					ObjectMeta: yaml.ObjectMeta{
						Labels: map[string]string{"existing": "label"},
					},
				},
			},
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{
						Labels: map[string]string{"app": "myapp"},
					},
				},
			},
			expectChanged: true,
			expectLabels:  map[string]string{"existing": "label", "app": "myapp"},
		},
		{
			name: "add annotations",
			kf:   &kptfilev1.KptFile{},
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{
						Annotations: map[string]string{"description": "my pkg"},
					},
				},
			},
			expectChanged: true,
			expectAnnos:   map[string]string{"description": "my pkg"},
		},
		{
			name: "update existing label",
			kf: &kptfilev1.KptFile{
				ResourceMeta: yaml.ResourceMeta{
					ObjectMeta: yaml.ObjectMeta{
						Labels: map[string]string{"app": "oldvalue"},
					},
				},
			},
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{
						Labels: map[string]string{"app": "newvalue"},
					},
				},
			},
			expectChanged: true,
			expectLabels:  map[string]string{"app": "newvalue"},
		},
		{
			name: "labels and annotations together",
			kf:   &kptfilev1.KptFile{},
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{
						Labels:      map[string]string{"tier": "frontend"},
						Annotations: map[string]string{"owner": "team-a"},
					},
				},
			},
			expectChanged: true,
			expectLabels:  map[string]string{"tier": "frontend"},
			expectAnnos:   map[string]string{"owner": "team-a"},
		},
		{
			name: "no change when labels already match",
			kf: &kptfilev1.KptFile{
				ResourceMeta: yaml.ResourceMeta{
					ObjectMeta: yaml.ObjectMeta{
						Labels: map[string]string{"app": "myapp"},
					},
				},
			},
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{
						Labels: map[string]string{"app": "myapp"},
					},
				},
			},
			expectChanged: false,
			expectLabels:  map[string]string{"app": "myapp"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := applyPackageMetadataToKptfile(tt.kf, tt.pr)
			assert.Equal(t, tt.expectChanged, changed, "changed flag mismatch")
			assert.Equal(t, tt.expectLabels, tt.kf.Labels, "labels mismatch")
			assert.Equal(t, tt.expectAnnos, tt.kf.Annotations, "annotations mismatch")
		})
	}
}

func TestHasUserModifiedMetadata(t *testing.T) {
	tests := []struct {
		name   string
		pr     *porchv1alpha2.PackageRevision
		expect bool
	}{
		{
			name: "no metadata, no managedFields",
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: nil,
				},
				ObjectMeta: metav1.ObjectMeta{
					ManagedFields: []metav1.ManagedFieldsEntry{},
				},
			},
			expect: false,
		},
		{
			name: "metadata exists, managed by controller kptfile",
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{
						Labels: map[string]string{"app": "test"},
					},
				},
				ObjectMeta: metav1.ObjectMeta{
					ManagedFields: []metav1.ManagedFieldsEntry{
						{Manager: fieldManagerPRControllerKptfile},
					},
				},
			},
			expect: false,
		},
		{
			name: "metadata exists, managed by different manager (user)",
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{
						Labels: map[string]string{"app": "test"},
					},
				},
				ObjectMeta: metav1.ObjectMeta{
					ManagedFields: []metav1.ManagedFieldsEntry{
						{Manager: "kubectl"},
					},
				},
			},
			expect: true,
		},
		{
			name: "no metadata, but other manager exists",
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: nil,
				},
				ObjectMeta: metav1.ObjectMeta{
					ManagedFields: []metav1.ManagedFieldsEntry{
						{Manager: "kubectl"},
					},
				},
			},
			expect: false,
		},
		{
			name: "metadata exists, multiple managers including controller",
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{
						Labels: map[string]string{"app": "test"},
					},
				},
				ObjectMeta: metav1.ObjectMeta{
					ManagedFields: []metav1.ManagedFieldsEntry{
						{Manager: fieldManagerPRControllerKptfile},
						{Manager: "kubectl"},
					},
				},
			},
			expect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasUserModifiedMetadata(tt.pr)
			assert.Equal(t, tt.expect, result, "hasUserModifiedMetadata mismatch")
		})
	}
}

func TestApplyMetadataMap(t *testing.T) {
	tests := []struct {
		name          string
		kf            *kptfilev1.KptFile
		desired       map[string]string
		isLabels      bool
		expectChanged bool
		checkField    func(*kptfilev1.KptFile) map[string]string
	}{
		{
			name:          "add labels to nil map",
			kf:            &kptfilev1.KptFile{},
			desired:       map[string]string{"app": "test"},
			isLabels:      true,
			expectChanged: true,
			checkField:    func(kf *kptfilev1.KptFile) map[string]string { return kf.Labels },
		},
		{
			name:          "add annotations to nil map",
			kf:            &kptfilev1.KptFile{},
			desired:       map[string]string{"doc": "readme"},
			isLabels:      false,
			expectChanged: true,
			checkField:    func(kf *kptfilev1.KptFile) map[string]string { return kf.Annotations },
		},
		{
			name: "merge into existing labels",
			kf: &kptfilev1.KptFile{
				ResourceMeta: yaml.ResourceMeta{
					ObjectMeta: yaml.ObjectMeta{
						Labels: map[string]string{"env": "prod"},
					},
				},
			},
			desired:       map[string]string{"app": "test"},
			isLabels:      true,
			expectChanged: true,
			checkField:    func(kf *kptfilev1.KptFile) map[string]string { return kf.Labels },
		},
		{
			name: "no change when identical labels",
			kf: &kptfilev1.KptFile{
				ResourceMeta: yaml.ResourceMeta{
					ObjectMeta: yaml.ObjectMeta{
						Labels: map[string]string{"app": "test"},
					},
				},
			},
			desired:       map[string]string{"app": "test"},
			isLabels:      true,
			expectChanged: false,
			checkField:    func(kf *kptfilev1.KptFile) map[string]string { return kf.Labels },
		},
		{
			name: "update existing label value",
			kf: &kptfilev1.KptFile{
				ResourceMeta: yaml.ResourceMeta{
					ObjectMeta: yaml.ObjectMeta{
						Labels: map[string]string{"app": "old"},
					},
				},
			},
			desired:       map[string]string{"app": "new"},
			isLabels:      true,
			expectChanged: true,
			checkField:    func(kf *kptfilev1.KptFile) map[string]string { return kf.Labels },
		},
		{
			name: "merge multiple labels",
			kf: &kptfilev1.KptFile{
				ResourceMeta: yaml.ResourceMeta{
					ObjectMeta: yaml.ObjectMeta{
						Labels: map[string]string{"a": "1", "b": "2"},
					},
				},
			},
			desired:       map[string]string{"c": "3", "d": "4"},
			isLabels:      true,
			expectChanged: true,
			checkField: func(kf *kptfilev1.KptFile) map[string]string {
				assert.Equal(t, "1", kf.Labels["a"])
				assert.Equal(t, "2", kf.Labels["b"])
				assert.Equal(t, "3", kf.Labels["c"])
				assert.Equal(t, "4", kf.Labels["d"])
				return kf.Labels
			},
		},
		{
			name: "partial update to existing labels (keep others)",
			kf: &kptfilev1.KptFile{
				ResourceMeta: yaml.ResourceMeta{
					ObjectMeta: yaml.ObjectMeta{
						Labels: map[string]string{"keep": "this", "update": "old"},
					},
				},
			},
			desired:       map[string]string{"update": "new"},
			isLabels:      true,
			expectChanged: true,
			checkField: func(kf *kptfilev1.KptFile) map[string]string {
				assert.Equal(t, "this", kf.Labels["keep"])
				assert.Equal(t, "new", kf.Labels["update"])
				return kf.Labels
			},
		},
		{
			name:          "empty desired map (no change)",
			kf:            &kptfilev1.KptFile{},
			desired:       map[string]string{},
			isLabels:      true,
			expectChanged: false,
			checkField:    func(kf *kptfilev1.KptFile) map[string]string { return kf.Labels },
		},
		{
			name: "annotations with empty desired (no change)",
			kf: &kptfilev1.KptFile{
				ResourceMeta: yaml.ResourceMeta{
					ObjectMeta: yaml.ObjectMeta{
						Annotations: map[string]string{"existing": "anno"},
					},
				},
			},
			desired:       map[string]string{},
			isLabels:      false,
			expectChanged: false,
			checkField:    func(kf *kptfilev1.KptFile) map[string]string { return kf.Annotations },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := applyMetadataMap(tt.kf, tt.desired, tt.isLabels)
			assert.Equal(t, tt.expectChanged, changed, "changed flag mismatch")
			field := tt.checkField(tt.kf)
			for k, v := range tt.desired {
				assert.Equal(t, v, field[k], "field value mismatch for key %s", k)
			}
		})
	}
}

func TestApplyPackageMetadataToKptfileComprehensive(t *testing.T) {
	tests := []struct {
		name          string
		kf            *kptfilev1.KptFile
		pr            *porchv1alpha2.PackageRevision
		expectChanged bool
		verify        func(*testing.T, *kptfilev1.KptFile)
	}{
		{
			name: "both labels and annotations changed",
			kf:   &kptfilev1.KptFile{},
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{
						Labels:      map[string]string{"l1": "v1"},
						Annotations: map[string]string{"a1": "v1"},
					},
				},
			},
			expectChanged: true,
			verify: func(t *testing.T, kf *kptfilev1.KptFile) {
				assert.Equal(t, "v1", kf.Labels["l1"])
				assert.Equal(t, "v1", kf.Annotations["a1"])
			},
		},
		{
			name: "only labels changed",
			kf:   &kptfilev1.KptFile{},
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{
						Labels: map[string]string{"l1": "v1"},
					},
				},
			},
			expectChanged: true,
			verify: func(t *testing.T, kf *kptfilev1.KptFile) {
				assert.Equal(t, "v1", kf.Labels["l1"])
				assert.Nil(t, kf.Annotations)
			},
		},
		{
			name: "only annotations changed",
			kf:   &kptfilev1.KptFile{},
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{
						Annotations: map[string]string{"a1": "v1"},
					},
				},
			},
			expectChanged: true,
			verify: func(t *testing.T, kf *kptfilev1.KptFile) {
				assert.Nil(t, kf.Labels)
				assert.Equal(t, "v1", kf.Annotations["a1"])
			},
		},
		{
			name: "both labels and annotations with existing values (merge)",
			kf: &kptfilev1.KptFile{
				ResourceMeta: yaml.ResourceMeta{
					ObjectMeta: yaml.ObjectMeta{
						Labels:      map[string]string{"existing-l": "val"},
						Annotations: map[string]string{"existing-a": "val"},
					},
				},
			},
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{
						Labels:      map[string]string{"new-l": "val"},
						Annotations: map[string]string{"new-a": "val"},
					},
				},
			},
			expectChanged: true,
			verify: func(t *testing.T, kf *kptfilev1.KptFile) {
				assert.Equal(t, "val", kf.Labels["existing-l"])
				assert.Equal(t, "val", kf.Labels["new-l"])
				assert.Equal(t, "val", kf.Annotations["existing-a"])
				assert.Equal(t, "val", kf.Annotations["new-a"])
			},
		},
		{
			name: "no change when labels and annotations are identical",
			kf: &kptfilev1.KptFile{
				ResourceMeta: yaml.ResourceMeta{
					ObjectMeta: yaml.ObjectMeta{
						Labels:      map[string]string{"l1": "v1"},
						Annotations: map[string]string{"a1": "v1"},
					},
				},
			},
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{
						Labels:      map[string]string{"l1": "v1"},
						Annotations: map[string]string{"a1": "v1"},
					},
				},
			},
			expectChanged: false,
			verify: func(t *testing.T, kf *kptfilev1.KptFile) {
				assert.Equal(t, "v1", kf.Labels["l1"])
				assert.Equal(t, "v1", kf.Annotations["a1"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := applyPackageMetadataToKptfile(tt.kf, tt.pr)
			assert.Equal(t, tt.expectChanged, changed, "changed flag mismatch")
			if tt.verify != nil {
				tt.verify(t, tt.kf)
			}
		})
	}
}

func TestHasUserModifiedMetadataComprehensive(t *testing.T) {
	tests := []struct {
		name   string
		pr     *porchv1alpha2.PackageRevision
		expect bool
		desc   string
	}{
		{
			name: "metadata nil, no managedFields",
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: nil,
				},
				ObjectMeta: metav1.ObjectMeta{
					ManagedFields: []metav1.ManagedFieldsEntry{},
				},
			},
			expect: false,
			desc:   "no metadata and no field managers",
		},
		{
			name: "metadata present but empty labels and annotations",
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{},
				},
				ObjectMeta: metav1.ObjectMeta{
					ManagedFields: []metav1.ManagedFieldsEntry{
						{Manager: fieldManagerPRControllerKptfile},
					},
				},
			},
			expect: false,
			desc:   "empty metadata with controller manager",
		},
		{
			name: "metadata with labels, controller manager only",
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{
						Labels: map[string]string{"l": "v"},
					},
				},
				ObjectMeta: metav1.ObjectMeta{
					ManagedFields: []metav1.ManagedFieldsEntry{
						{Manager: fieldManagerPRControllerKptfile},
					},
				},
			},
			expect: false,
			desc:   "controller-owned labels only",
		},
		{
			name: "metadata with labels, user manager only",
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{
						Labels: map[string]string{"l": "v"},
					},
				},
				ObjectMeta: metav1.ObjectMeta{
					ManagedFields: []metav1.ManagedFieldsEntry{
						{Manager: "kubectl"},
					},
				},
			},
			expect: true,
			desc:   "user-owned labels (kubectl manager)",
		},
		{
			name: "metadata nil with user manager (user didn't set it)",
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: nil,
				},
				ObjectMeta: metav1.ObjectMeta{
					ManagedFields: []metav1.ManagedFieldsEntry{
						{Manager: "kubectl"},
					},
				},
			},
			expect: false,
			desc:   "metadata is nil, so no user modification even with user manager",
		},
		{
			name: "metadata with labels, mixed managers (controller + kubectl)",
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{
						Labels: map[string]string{"l": "v"},
					},
				},
				ObjectMeta: metav1.ObjectMeta{
					ManagedFields: []metav1.ManagedFieldsEntry{
						{Manager: fieldManagerPRControllerKptfile},
						{Manager: "kubectl"},
					},
				},
			},
			expect: true,
			desc:   "metadata owned by both controller and user (user modification detected)",
		},
		{
			name: "metadata with multiple user managers",
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{
						Labels: map[string]string{"l": "v"},
					},
				},
				ObjectMeta: metav1.ObjectMeta{
					ManagedFields: []metav1.ManagedFieldsEntry{
						{Manager: "kubectl"},
						{Manager: "another-tool"},
					},
				},
			},
			expect: true,
			desc:   "multiple non-controller managers (user modified)",
		},
		{
			name: "metadata with annotations, controller manager",
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{
						Annotations: map[string]string{"a": "v"},
					},
				},
				ObjectMeta: metav1.ObjectMeta{
					ManagedFields: []metav1.ManagedFieldsEntry{
						{Manager: fieldManagerPRControllerKptfile},
					},
				},
			},
			expect: false,
			desc:   "controller-owned annotations only",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasUserModifiedMetadata(tt.pr)
			assert.Equal(t, tt.expect, result, "hasUserModifiedMetadata mismatch: %s", tt.desc)
		})
	}
}

func TestApplyMetadataMapEdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		kf            *kptfilev1.KptFile
		desired       map[string]string
		isLabels      bool
		expectChanged bool
		verify        func(*testing.T, *kptfilev1.KptFile)
	}{
		{
			name:          "very large label value",
			kf:            &kptfilev1.KptFile{},
			desired:       map[string]string{"large": string(make([]byte, 1000))},
			isLabels:      true,
			expectChanged: true,
			verify: func(t *testing.T, kf *kptfilev1.KptFile) {
				assert.Equal(t, 1000, len(kf.Labels["large"]))
			},
		},
		{
			name:          "label key with special characters",
			kf:            &kptfilev1.KptFile{},
			desired:       map[string]string{"app.io/env": "prod"},
			isLabels:      true,
			expectChanged: true,
			verify: func(t *testing.T, kf *kptfilev1.KptFile) {
				assert.Equal(t, "prod", kf.Labels["app.io/env"])
			},
		},
		{
			name:          "annotation with empty string value",
			kf:            &kptfilev1.KptFile{},
			desired:       map[string]string{"empty": ""},
			isLabels:      false,
			expectChanged: true,
			verify: func(t *testing.T, kf *kptfilev1.KptFile) {
				assert.Equal(t, "", kf.Annotations["empty"])
			},
		},
		{
			name: "overwrite annotation with empty string",
			kf: &kptfilev1.KptFile{
				ResourceMeta: yaml.ResourceMeta{
					ObjectMeta: yaml.ObjectMeta{
						Annotations: map[string]string{"key": "old-value"},
					},
				},
			},
			desired:       map[string]string{"key": ""},
			isLabels:      false,
			expectChanged: true,
			verify: func(t *testing.T, kf *kptfilev1.KptFile) {
				assert.Equal(t, "", kf.Annotations["key"])
			},
		},
		{
			name: "add many labels at once",
			kf:   &kptfilev1.KptFile{},
			desired: map[string]string{
				"l1": "v1", "l2": "v2", "l3": "v3", "l4": "v4", "l5": "v5",
			},
			isLabels:      true,
			expectChanged: true,
			verify: func(t *testing.T, kf *kptfilev1.KptFile) {
				for i := 1; i <= 5; i++ {
					key := fmt.Sprintf("l%d", i)
					val := fmt.Sprintf("v%d", i)
					assert.Equal(t, val, kf.Labels[key])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := applyMetadataMap(tt.kf, tt.desired, tt.isLabels)
			assert.Equal(t, tt.expectChanged, changed)
			if tt.verify != nil {
				tt.verify(t, tt.kf)
			}
		})
	}
}

func TestApplyPackageMetadataToKptfileEdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		kf            *kptfilev1.KptFile
		pr            *porchv1alpha2.PackageRevision
		expectChanged bool
		verify        func(*testing.T, *kptfilev1.KptFile)
	}{
		{
			name: "labels with nil but annotations with value",
			kf:   &kptfilev1.KptFile{},
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{
						Labels:      nil,
						Annotations: map[string]string{"a": "v"},
					},
				},
			},
			expectChanged: true,
			verify: func(t *testing.T, kf *kptfilev1.KptFile) {
				assert.Nil(t, kf.Labels)
				assert.Equal(t, "v", kf.Annotations["a"])
			},
		},
		{
			name: "both labels and annotations nil",
			kf: &kptfilev1.KptFile{
				ResourceMeta: yaml.ResourceMeta{
					ObjectMeta: yaml.ObjectMeta{
						Labels:      map[string]string{"l": "v"},
						Annotations: map[string]string{"a": "v"},
					},
				},
			},
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{
						Labels:      nil,
						Annotations: nil,
					},
				},
			},
			expectChanged: false,
			verify: func(t *testing.T, kf *kptfilev1.KptFile) {
				assert.Equal(t, "v", kf.Labels["l"])
				assert.Equal(t, "v", kf.Annotations["a"])
			},
		},
		{
			name: "empty label and annotation maps",
			kf:   &kptfilev1.KptFile{},
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{
						Labels:      map[string]string{},
						Annotations: map[string]string{},
					},
				},
			},
			expectChanged: false,
			verify: func(t *testing.T, kf *kptfilev1.KptFile) {
				// Empty maps shouldn't cause changes
				assert.Empty(t, kf.Labels)
				assert.Empty(t, kf.Annotations)
			},
		},
		{
			name: "only update one label out of many existing",
			kf: &kptfilev1.KptFile{
				ResourceMeta: yaml.ResourceMeta{
					ObjectMeta: yaml.ObjectMeta{
						Labels: map[string]string{
							"a": "1", "b": "2", "c": "3", "d": "4",
						},
					},
				},
			},
			pr: &porchv1alpha2.PackageRevision{
				Spec: porchv1alpha2.PackageRevisionSpec{
					PackageMetadata: &porchv1alpha2.PackageMetadata{
						Labels: map[string]string{"b": "updated"},
					},
				},
			},
			expectChanged: true,
			verify: func(t *testing.T, kf *kptfilev1.KptFile) {
				assert.Equal(t, "1", kf.Labels["a"])
				assert.Equal(t, "updated", kf.Labels["b"])
				assert.Equal(t, "3", kf.Labels["c"])
				assert.Equal(t, "4", kf.Labels["d"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := applyPackageMetadataToKptfile(tt.kf, tt.pr)
			assert.Equal(t, tt.expectChanged, changed)
			if tt.verify != nil {
				tt.verify(t, tt.kf)
			}
		})
	}
}
