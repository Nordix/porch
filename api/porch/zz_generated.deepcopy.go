//go:build !ignore_autogenerated
// +build !ignore_autogenerated

// Copyright 2022-2025 The kpt and Nephio Authors
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

// Code generated by deepcopy-gen. DO NOT EDIT.

package porch

import (
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Condition) DeepCopyInto(out *Condition) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Condition.
func (in *Condition) DeepCopy() *Condition {
	if in == nil {
		return nil
	}
	out := new(Condition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Field) DeepCopyInto(out *Field) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Field.
func (in *Field) DeepCopy() *Field {
	if in == nil {
		return nil
	}
	out := new(Field)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *File) DeepCopyInto(out *File) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new File.
func (in *File) DeepCopy() *File {
	if in == nil {
		return nil
	}
	out := new(File)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GitLock) DeepCopyInto(out *GitLock) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GitLock.
func (in *GitLock) DeepCopy() *GitLock {
	if in == nil {
		return nil
	}
	out := new(GitLock)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GitPackage) DeepCopyInto(out *GitPackage) {
	*out = *in
	out.SecretRef = in.SecretRef
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GitPackage.
func (in *GitPackage) DeepCopy() *GitPackage {
	if in == nil {
		return nil
	}
	out := new(GitPackage)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NameMeta) DeepCopyInto(out *NameMeta) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NameMeta.
func (in *NameMeta) DeepCopy() *NameMeta {
	if in == nil {
		return nil
	}
	out := new(NameMeta)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *OciPackage) DeepCopyInto(out *OciPackage) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new OciPackage.
func (in *OciPackage) DeepCopy() *OciPackage {
	if in == nil {
		return nil
	}
	out := new(OciPackage)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PackageCloneTaskSpec) DeepCopyInto(out *PackageCloneTaskSpec) {
	*out = *in
	in.Upstream.DeepCopyInto(&out.Upstream)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PackageCloneTaskSpec.
func (in *PackageCloneTaskSpec) DeepCopy() *PackageCloneTaskSpec {
	if in == nil {
		return nil
	}
	out := new(PackageCloneTaskSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PackageEditTaskSpec) DeepCopyInto(out *PackageEditTaskSpec) {
	*out = *in
	if in.Source != nil {
		in, out := &in.Source, &out.Source
		*out = new(PackageRevisionRef)
		**out = **in
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PackageEditTaskSpec.
func (in *PackageEditTaskSpec) DeepCopy() *PackageEditTaskSpec {
	if in == nil {
		return nil
	}
	out := new(PackageEditTaskSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PackageInitTaskSpec) DeepCopyInto(out *PackageInitTaskSpec) {
	*out = *in
	if in.Keywords != nil {
		in, out := &in.Keywords, &out.Keywords
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PackageInitTaskSpec.
func (in *PackageInitTaskSpec) DeepCopy() *PackageInitTaskSpec {
	if in == nil {
		return nil
	}
	out := new(PackageInitTaskSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PackageRevision) DeepCopyInto(out *PackageRevision) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PackageRevision.
func (in *PackageRevision) DeepCopy() *PackageRevision {
	if in == nil {
		return nil
	}
	out := new(PackageRevision)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *PackageRevision) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PackageRevisionList) DeepCopyInto(out *PackageRevisionList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]PackageRevision, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PackageRevisionList.
func (in *PackageRevisionList) DeepCopy() *PackageRevisionList {
	if in == nil {
		return nil
	}
	out := new(PackageRevisionList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *PackageRevisionList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PackageRevisionRef) DeepCopyInto(out *PackageRevisionRef) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PackageRevisionRef.
func (in *PackageRevisionRef) DeepCopy() *PackageRevisionRef {
	if in == nil {
		return nil
	}
	out := new(PackageRevisionRef)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PackageRevisionResources) DeepCopyInto(out *PackageRevisionResources) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PackageRevisionResources.
func (in *PackageRevisionResources) DeepCopy() *PackageRevisionResources {
	if in == nil {
		return nil
	}
	out := new(PackageRevisionResources)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *PackageRevisionResources) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PackageRevisionResourcesList) DeepCopyInto(out *PackageRevisionResourcesList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]PackageRevisionResources, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PackageRevisionResourcesList.
func (in *PackageRevisionResourcesList) DeepCopy() *PackageRevisionResourcesList {
	if in == nil {
		return nil
	}
	out := new(PackageRevisionResourcesList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *PackageRevisionResourcesList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PackageRevisionResourcesSpec) DeepCopyInto(out *PackageRevisionResourcesSpec) {
	*out = *in
	if in.Resources != nil {
		in, out := &in.Resources, &out.Resources
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PackageRevisionResourcesSpec.
func (in *PackageRevisionResourcesSpec) DeepCopy() *PackageRevisionResourcesSpec {
	if in == nil {
		return nil
	}
	out := new(PackageRevisionResourcesSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PackageRevisionResourcesStatus) DeepCopyInto(out *PackageRevisionResourcesStatus) {
	*out = *in
	in.RenderStatus.DeepCopyInto(&out.RenderStatus)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PackageRevisionResourcesStatus.
func (in *PackageRevisionResourcesStatus) DeepCopy() *PackageRevisionResourcesStatus {
	if in == nil {
		return nil
	}
	out := new(PackageRevisionResourcesStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PackageRevisionSpec) DeepCopyInto(out *PackageRevisionSpec) {
	*out = *in
	if in.Parent != nil {
		in, out := &in.Parent, &out.Parent
		*out = new(ParentReference)
		**out = **in
	}
	if in.Tasks != nil {
		in, out := &in.Tasks, &out.Tasks
		*out = make([]Task, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.ReadinessGates != nil {
		in, out := &in.ReadinessGates, &out.ReadinessGates
		*out = make([]ReadinessGate, len(*in))
		copy(*out, *in)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PackageRevisionSpec.
func (in *PackageRevisionSpec) DeepCopy() *PackageRevisionSpec {
	if in == nil {
		return nil
	}
	out := new(PackageRevisionSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PackageRevisionStatus) DeepCopyInto(out *PackageRevisionStatus) {
	*out = *in
	if in.UpstreamLock != nil {
		in, out := &in.UpstreamLock, &out.UpstreamLock
		*out = new(UpstreamLock)
		(*in).DeepCopyInto(*out)
	}
	in.PublishedAt.DeepCopyInto(&out.PublishedAt)
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]Condition, len(*in))
		copy(*out, *in)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PackageRevisionStatus.
func (in *PackageRevisionStatus) DeepCopy() *PackageRevisionStatus {
	if in == nil {
		return nil
	}
	out := new(PackageRevisionStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PackageSpec) DeepCopyInto(out *PackageSpec) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PackageSpec.
func (in *PackageSpec) DeepCopy() *PackageSpec {
	if in == nil {
		return nil
	}
	out := new(PackageSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PackageStatus) DeepCopyInto(out *PackageStatus) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PackageStatus.
func (in *PackageStatus) DeepCopy() *PackageStatus {
	if in == nil {
		return nil
	}
	out := new(PackageStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PackageUpgradeTaskSpec) DeepCopyInto(out *PackageUpgradeTaskSpec) {
	*out = *in
	out.OldUpstream = in.OldUpstream
	out.NewUpstream = in.NewUpstream
	out.LocalPackageRevisionRef = in.LocalPackageRevisionRef
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PackageUpgradeTaskSpec.
func (in *PackageUpgradeTaskSpec) DeepCopy() *PackageUpgradeTaskSpec {
	if in == nil {
		return nil
	}
	out := new(PackageUpgradeTaskSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ParentReference) DeepCopyInto(out *ParentReference) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ParentReference.
func (in *ParentReference) DeepCopy() *ParentReference {
	if in == nil {
		return nil
	}
	out := new(ParentReference)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PatchSpec) DeepCopyInto(out *PatchSpec) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PatchSpec.
func (in *PatchSpec) DeepCopy() *PatchSpec {
	if in == nil {
		return nil
	}
	out := new(PatchSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PorchPackage) DeepCopyInto(out *PorchPackage) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	out.Status = in.Status
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PorchPackage.
func (in *PorchPackage) DeepCopy() *PorchPackage {
	if in == nil {
		return nil
	}
	out := new(PorchPackage)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *PorchPackage) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PorchPackageList) DeepCopyInto(out *PorchPackageList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]PorchPackage, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PorchPackageList.
func (in *PorchPackageList) DeepCopy() *PorchPackageList {
	if in == nil {
		return nil
	}
	out := new(PorchPackageList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *PorchPackageList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ReadinessGate) DeepCopyInto(out *ReadinessGate) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ReadinessGate.
func (in *ReadinessGate) DeepCopy() *ReadinessGate {
	if in == nil {
		return nil
	}
	out := new(ReadinessGate)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RenderStatus) DeepCopyInto(out *RenderStatus) {
	*out = *in
	in.Result.DeepCopyInto(&out.Result)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RenderStatus.
func (in *RenderStatus) DeepCopy() *RenderStatus {
	if in == nil {
		return nil
	}
	out := new(RenderStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RepositoryRef) DeepCopyInto(out *RepositoryRef) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RepositoryRef.
func (in *RepositoryRef) DeepCopy() *RepositoryRef {
	if in == nil {
		return nil
	}
	out := new(RepositoryRef)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ResourceIdentifier) DeepCopyInto(out *ResourceIdentifier) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	out.NameMeta = in.NameMeta
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ResourceIdentifier.
func (in *ResourceIdentifier) DeepCopy() *ResourceIdentifier {
	if in == nil {
		return nil
	}
	out := new(ResourceIdentifier)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Result) DeepCopyInto(out *Result) {
	*out = *in
	if in.Results != nil {
		in, out := &in.Results, &out.Results
		*out = make([]ResultItem, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Result.
func (in *Result) DeepCopy() *Result {
	if in == nil {
		return nil
	}
	out := new(Result)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ResultItem) DeepCopyInto(out *ResultItem) {
	*out = *in
	if in.ResourceRef != nil {
		in, out := &in.ResourceRef, &out.ResourceRef
		*out = new(ResourceIdentifier)
		**out = **in
	}
	if in.Field != nil {
		in, out := &in.Field, &out.Field
		*out = new(Field)
		**out = **in
	}
	if in.File != nil {
		in, out := &in.File, &out.File
		*out = new(File)
		**out = **in
	}
	if in.Tags != nil {
		in, out := &in.Tags, &out.Tags
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ResultItem.
func (in *ResultItem) DeepCopy() *ResultItem {
	if in == nil {
		return nil
	}
	out := new(ResultItem)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ResultList) DeepCopyInto(out *ResultList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]*Result, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(Result)
				(*in).DeepCopyInto(*out)
			}
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ResultList.
func (in *ResultList) DeepCopy() *ResultList {
	if in == nil {
		return nil
	}
	out := new(ResultList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SecretRef) DeepCopyInto(out *SecretRef) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SecretRef.
func (in *SecretRef) DeepCopy() *SecretRef {
	if in == nil {
		return nil
	}
	out := new(SecretRef)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Selector) DeepCopyInto(out *Selector) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Selector.
func (in *Selector) DeepCopy() *Selector {
	if in == nil {
		return nil
	}
	out := new(Selector)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Task) DeepCopyInto(out *Task) {
	*out = *in
	if in.Init != nil {
		in, out := &in.Init, &out.Init
		*out = new(PackageInitTaskSpec)
		(*in).DeepCopyInto(*out)
	}
	if in.Clone != nil {
		in, out := &in.Clone, &out.Clone
		*out = new(PackageCloneTaskSpec)
		(*in).DeepCopyInto(*out)
	}
	if in.Edit != nil {
		in, out := &in.Edit, &out.Edit
		*out = new(PackageEditTaskSpec)
		(*in).DeepCopyInto(*out)
	}
	if in.Upgrade != nil {
		in, out := &in.Upgrade, &out.Upgrade
		*out = new(PackageUpgradeTaskSpec)
		**out = **in
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Task.
func (in *Task) DeepCopy() *Task {
	if in == nil {
		return nil
	}
	out := new(Task)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TaskResult) DeepCopyInto(out *TaskResult) {
	*out = *in
	if in.Task != nil {
		in, out := &in.Task, &out.Task
		*out = new(Task)
		(*in).DeepCopyInto(*out)
	}
	if in.RenderStatus != nil {
		in, out := &in.RenderStatus, &out.RenderStatus
		*out = new(RenderStatus)
		(*in).DeepCopyInto(*out)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TaskResult.
func (in *TaskResult) DeepCopy() *TaskResult {
	if in == nil {
		return nil
	}
	out := new(TaskResult)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *UpstreamLock) DeepCopyInto(out *UpstreamLock) {
	*out = *in
	if in.Git != nil {
		in, out := &in.Git, &out.Git
		*out = new(GitLock)
		**out = **in
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new UpstreamLock.
func (in *UpstreamLock) DeepCopy() *UpstreamLock {
	if in == nil {
		return nil
	}
	out := new(UpstreamLock)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *UpstreamPackage) DeepCopyInto(out *UpstreamPackage) {
	*out = *in
	if in.Git != nil {
		in, out := &in.Git, &out.Git
		*out = new(GitPackage)
		**out = **in
	}
	if in.Oci != nil {
		in, out := &in.Oci, &out.Oci
		*out = new(OciPackage)
		**out = **in
	}
	if in.UpstreamRef != nil {
		in, out := &in.UpstreamRef, &out.UpstreamRef
		*out = new(PackageRevisionRef)
		**out = **in
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new UpstreamPackage.
func (in *UpstreamPackage) DeepCopy() *UpstreamPackage {
	if in == nil {
		return nil
	}
	out := new(UpstreamPackage)
	in.DeepCopyInto(out)
	return out
}
