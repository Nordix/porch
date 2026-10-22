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
	"context"

	porchv1alpha2 "github.com/kptdev/porch/api/porch/v1alpha2"
	"github.com/kptdev/porch/internal/telemetry"
)

// This file keeps the lifecycle telemetry wiring out of reconcileLifecycle so
// that the reconcile logic reads cleanly and each measurement point is a single
// deferred call. All of the telemetry-specific branching (PackageRevision vs
// PackageRevisionApproval resource naming, and resolving the lifecycle to report
// once the operation settles) lives here rather than in the hot path.

// measureLifecycleGet times the content-cache read that resolves the current
// lifecycle. The reported resource is always PackageRevision.
//
// Usage (single line in the caller):
//
//	defer measureLifecycleGet(ctx, pr)(&err)
func measureLifecycleGet(ctx context.Context, pr *porchv1alpha2.PackageRevision) func(errPtr *error) {
	finish := telemetry.MeasureControllerOperation(
		ctx, telemetry.Operations.Get, prTelemetryName, pr.Spec.Lifecycle, pr)
	return func(errPtr *error) { finish(errPtr, nil) }
}

// measureSourceOperation times a one-time source/subpackage draft operation.
// The operation type (e.g. "init", "clone", a subpackage hash) is parsed into a
// telemetry verb; the reported resource is always PackageRevision and the
// initial lifecycle is Draft.
//
// settled resolves the lifecycle to report once the operation finishes, given
// the final error — source and subpackage callers differ in how they report
// failure, so each passes its own resolver.
//
// Usage (single line in the caller):
//
//	defer measureSourceOperation(ctx, pr, opType, settled)(&err)
func measureSourceOperation(
	ctx context.Context,
	pr *porchv1alpha2.PackageRevision,
	operationType string,
	settled func(err error) porchv1alpha2.PackageRevisionLifecycle,
) func(errPtr *error) {
	finish := telemetry.MeasureControllerOperation(
		ctx, telemetry.ParseOperation(operationType), prTelemetryName,
		porchv1alpha2.PackageRevisionLifecycleDraft, pr)
	return func(errPtr *error) {
		finish(errPtr, func() porchv1alpha2.PackageRevisionLifecycle {
			var err error
			if errPtr != nil {
				err = *errPtr
			}
			return settled(err)
		})
	}
}

// measureLifecycleUpdate times a lifecycle transition. It selects the
// PackageRevisionApproval resource for published transitions (conceptually an
// approval, if not strictly API-wise) and reports the settled lifecycle:
// desired on success, current on failure.
//
// The reported lifecycle is derived from the same *error the measurement uses to
// classify success/failure, so the two can never disagree.
//
// Usage (single line in the caller):
//
//	defer measureLifecycleUpdate(ctx, pr, currentLifecycle, desiredLifecycle)(&err)
func measureLifecycleUpdate(
	ctx context.Context,
	pr *porchv1alpha2.PackageRevision,
	current, desired porchv1alpha2.PackageRevisionLifecycle,
) func(errPtr *error) {
	name := prTelemetryName
	if porchv1alpha2.LifecycleIsPublished(pr.Spec.Lifecycle) {
		name = praTelemetryName
	}
	finish := telemetry.MeasureControllerOperation(
		ctx, telemetry.Operations.Update, name, pr.Spec.Lifecycle, pr)
	return func(errPtr *error) {
		finish(errPtr, func() porchv1alpha2.PackageRevisionLifecycle {
			if errPtr != nil && *errPtr == nil {
				return desired
			}
			return current
		})
	}
}
