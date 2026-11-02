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

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DefaultListPageSize bounds each chunk of a paged List so no single response
// buffers the whole collection. Matches kubectl's default chunk size.
const DefaultListPageSize = int64(500)

// ListAllPaged lists every item matching opts into the supplied list, driving
// the limit/continue token itself. controller-runtime's client.Client exposes
// client.Limit/client.Continue and list.GetContinue() but, unlike client-go's
// tools/pager, ships no helper that loops them -- so this is that helper, in one
// place, for every controller-runtime List call site that must not buffer a
// whole-estate response in a single request.
//
// list is fully populated on return (items accumulated across all pages).
// pageSize <= 0 falls back to DefaultListPageSize. Any caller-supplied
// client.Limit/client.Continue in opts is overridden per page.
func ListAllPaged(ctx context.Context, c client.Client, list client.ObjectList,
	pageSize int64, opts ...client.ListOption) error {
	if pageSize <= 0 {
		pageSize = DefaultListPageSize
	}

	var all []runtime.Object
	continueToken := ""
	for {
		pageOpts := append([]client.ListOption{client.Limit(pageSize)}, opts...)
		if continueToken != "" {
			pageOpts = append(pageOpts, client.Continue(continueToken))
		}
		if err := c.List(ctx, list, pageOpts...); err != nil {
			return err
		}
		items, err := apimeta.ExtractList(list)
		if err != nil {
			return err
		}
		all = append(all, items...)
		continueToken = list.GetContinue()
		if continueToken == "" {
			break
		}
	}

	// Collapse the accumulated pages back into the caller's list and clear the
	// continue token so the result reads as a single complete list.
	if err := apimeta.SetList(list, all); err != nil {
		return err
	}
	list.SetContinue("")
	return nil
}
