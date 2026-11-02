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

package get

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kptdev/kpt/pkg/lib/util/cmdutil"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/kubectl/pkg/cmd/get"
)

// getPrintFlagsWithName returns PrintFlags configured for `-o name` output, so
// the test can assert on object names streamed to stdout.
func getPrintFlagsWithName() *get.PrintFlags {
	pf := get.NewGetPrintFlags()
	nameFormat := "name"
	pf.OutputFormat = &nameFormat
	return pf
}

// listRequest records the query parameters of one LIST issued by the resource builder.
type listRequest struct {
	limit     string
	continue_ string
}

// chunkingAPIServer is a fake apiserver that serves just enough discovery and a
// two-page chunked PackageRevision list to exercise --chunk-size end to end.
type chunkingAPIServer struct {
	t *testing.T

	mu       sync.Mutex
	listReqs []listRequest

	// continueToken is the token handed out on page 1 and expected back on page 2.
	continueToken string
}

func (s *chunkingAPIServer) handler() http.Handler {
	mux := http.NewServeMux()

	// Root discovery: advertise the porch.kpt.dev API group.
	mux.HandleFunc("/apis", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"kind":       "APIGroupList",
			"apiVersion": "v1",
			"groups": []any{
				map[string]any{
					"name": "porch.kpt.dev",
					"versions": []any{
						map[string]any{"groupVersion": "porch.kpt.dev/v1alpha1", "version": "v1alpha1"},
					},
					"preferredVersion": map[string]any{"groupVersion": "porch.kpt.dev/v1alpha1", "version": "v1alpha1"},
				},
			},
		})
	})

	// Group-version discovery: advertise the packagerevisions resource.
	mux.HandleFunc("/apis/porch.kpt.dev/v1alpha1", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"kind":         "APIResourceList",
			"apiVersion":   "v1",
			"groupVersion": "porch.kpt.dev/v1alpha1",
			"resources": []any{
				map[string]any{
					"name":       "packagerevisions",
					"namespaced": true,
					"kind":       "PackageRevision",
					"verbs":      []any{"get", "list"},
				},
			},
		})
	})

	// The LIST endpoint: serve page 1 (with a continue token) then page 2 (final).
	mux.HandleFunc("/apis/porch.kpt.dev/v1alpha1/namespaces/test-ns/packagerevisions",
		func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			s.mu.Lock()
			s.listReqs = append(s.listReqs, listRequest{limit: q.Get("limit"), continue_: q.Get("continue")})
			s.mu.Unlock()

			if q.Get("continue") == "" {
				// Page 1: two items + a continue token.
				writeJSON(w, packageRevisionList(s.continueToken,
					pr("test-repo.pkg-a.v1", "pkg-a"),
					pr("test-repo.pkg-b.v1", "pkg-b"),
				))
				return
			}
			// Page 2: the final item, no further continue token.
			assert.Equal(s.t, s.continueToken, q.Get("continue"), "page 2 must echo the page-1 continue token")
			writeJSON(w, packageRevisionList("",
				pr("test-repo.pkg-c.v1", "pkg-c"),
			))
		})

	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// pr builds an unstructured PackageRevision object for a list response.
func pr(name, pkgName string) map[string]any {
	return map[string]any{
		"apiVersion": "porch.kpt.dev/v1alpha1",
		"kind":       "PackageRevision",
		"metadata":   map[string]any{"name": name, "namespace": "test-ns"},
		"spec": map[string]any{
			"packageName":   pkgName,
			"repository":    "test-repo",
			"revision":      int64(1),
			"workspaceName": "v1",
		},
	}
}

func packageRevisionList(continueToken string, items ...map[string]any) map[string]any {
	meta := map[string]any{}
	if continueToken != "" {
		meta["continue"] = continueToken
	}
	return map[string]any{
		"apiVersion": "porch.kpt.dev/v1alpha1",
		"kind":       "PackageRevisionList",
		"metadata":   meta,
		"items":      toAnySlice(items),
	}
}

func toAnySlice(items []map[string]any) []any {
	out := make([]any, len(items))
	for i := range items {
		out[i] = items[i]
	}
	return out
}

// chunkingKubeconfig writes a kubeconfig pointing at the fake apiserver.
func chunkingKubeconfig(t *testing.T, server string) string {
	t.Helper()
	content := fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: test
contexts:
- context:
    cluster: test
    namespace: test-ns
  name: test
clusters:
- cluster:
    server: %s
  name: test
users:
- name: test
  user: {}
`, server)
	p := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(p, []byte(content), 0600))
	return p
}

// TestChunkSizeStreaming verifies that --chunk-size drives a paginated LIST:
// the first request carries ?limit=N with no continue token, the second carries
// the server's continue token, and every item across both pages is printed.
func TestChunkSizeStreaming(t *testing.T) {
	ctx := context.Background()

	srv := &chunkingAPIServer{t: t, continueToken: "page2-token"}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	kubeconfig := chunkingKubeconfig(t, ts.URL)
	ns := "test-ns"

	r := &runner{
		ctx:          ctx,
		getFlags:     cmdutil.Options{ConfigFlags: &genericclioptions.ConfigFlags{KubeConfig: &kubeconfig, Namespace: &ns}},
		printFlags:   getPrintFlagsWithName(),
		chunkSize:    2,
		requestTable: false, // request unstructured objects, not server-side Table
		revision:     -2,    // -2 == "revision flag unset" sentinel
	}

	out := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(out)

	require.NoError(t, r.runE(cmd, []string{}))

	// Two LIST requests were made: page 1 (no continue) then page 2 (with token).
	srv.mu.Lock()
	reqs := append([]listRequest(nil), srv.listReqs...)
	srv.mu.Unlock()

	require.Len(t, reqs, 2, "expected exactly two chunked LIST requests, got %d: %+v", len(reqs), reqs)
	assert.Equal(t, "2", reqs[0].limit, "first request must carry ?limit=chunkSize")
	assert.Empty(t, reqs[0].continue_, "first request must not carry a continue token")
	assert.Equal(t, "2", reqs[1].limit, "second request must carry ?limit=chunkSize")
	assert.Equal(t, "page2-token", reqs[1].continue_, "second request must carry the page-1 continue token")

	// All three items across both pages were streamed to output.
	o := out.String()
	for _, name := range []string{"test-repo.pkg-a.v1", "test-repo.pkg-b.v1", "test-repo.pkg-c.v1"} {
		assert.Contains(t, o, name, "output must contain item %q from the paged list", name)
	}
}

// TestChunkSizeDisabledSingleRequest verifies that --chunk-size=0 issues a single
// unpaged LIST (no ?limit), preserving the pre-change behavior.
func TestChunkSizeDisabledSingleRequest(t *testing.T) {
	ctx := context.Background()

	srv := &chunkingAPIServer{t: t, continueToken: ""} // no continue -> single page regardless
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	kubeconfig := chunkingKubeconfig(t, ts.URL)
	ns := "test-ns"

	r := &runner{
		ctx:          ctx,
		getFlags:     cmdutil.Options{ConfigFlags: &genericclioptions.ConfigFlags{KubeConfig: &kubeconfig, Namespace: &ns}},
		printFlags:   getPrintFlagsWithName(),
		chunkSize:    0, // chunking disabled
		requestTable: false,
		revision:     -2,
	}

	out := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(out)

	require.NoError(t, r.runE(cmd, []string{}))

	srv.mu.Lock()
	reqs := append([]listRequest(nil), srv.listReqs...)
	srv.mu.Unlock()

	require.Len(t, reqs, 1, "chunk-size=0 must issue a single LIST, got %d: %+v", len(reqs), reqs)
	assert.Empty(t, reqs[0].limit, "chunk-size=0 must not send a ?limit")
	assert.True(t, strings.Contains(out.String(), "test-repo.pkg-a.v1"))
}
