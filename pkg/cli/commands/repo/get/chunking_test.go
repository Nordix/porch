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

type repoListReq struct {
	limit     string
	continue_ string
}

type repoChunkingServer struct {
	t             *testing.T
	mu            sync.Mutex
	listReqs      []repoListReq
	continueToken string
}

func (s *repoChunkingServer) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/apis", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"kind": "APIGroupList", "apiVersion": "v1",
			"groups": []any{map[string]any{
				"name": "config.porch.kpt.dev",
				"versions": []any{map[string]any{
					"groupVersion": "config.porch.kpt.dev/v1alpha1", "version": "v1alpha1",
				}},
				"preferredVersion": map[string]any{
					"groupVersion": "config.porch.kpt.dev/v1alpha1", "version": "v1alpha1",
				},
			}},
		})
	})

	mux.HandleFunc("/apis/config.porch.kpt.dev/v1alpha1", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"kind": "APIResourceList", "apiVersion": "v1",
			"groupVersion": "config.porch.kpt.dev/v1alpha1",
			"resources": []any{map[string]any{
				"name": "repositories", "namespaced": true, "kind": "Repository",
				"verbs": []any{"get", "list"},
			}},
		})
	})

	mux.HandleFunc("/apis/config.porch.kpt.dev/v1alpha1/namespaces/test-ns/repositories",
		func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			s.mu.Lock()
			s.listReqs = append(s.listReqs, repoListReq{limit: q.Get("limit"), continue_: q.Get("continue")})
			s.mu.Unlock()

			// Serve a server-side Table when the client negotiates one (the
			// default human `repo get` output); otherwise serve the plain list.
			asTable := strings.Contains(r.Header.Get("Accept"), "as=Table")
			first := q.Get("continue") == ""
			if !first {
				assert.Equal(s.t, s.continueToken, q.Get("continue"))
			}

			if asTable {
				if first {
					writeJSON(w, repositoryTable(s.continueToken, "repo-a", "repo-b"))
				} else {
					writeJSON(w, repositoryTable("", "repo-c"))
				}
				return
			}
			if first {
				writeJSON(w, repositoryList(s.continueToken, repo("repo-a"), repo("repo-b")))
			} else {
				writeJSON(w, repositoryList("", repo("repo-c")))
			}
		})

	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func repo(name string) map[string]any {
	return map[string]any{
		"apiVersion": "config.porch.kpt.dev/v1alpha1",
		"kind":       "Repository",
		"metadata":   map[string]any{"name": name, "namespace": "test-ns"},
		"spec":       map[string]any{"type": "git"},
	}
}

func repositoryList(continueToken string, items ...map[string]any) map[string]any {
	meta := map[string]any{}
	if continueToken != "" {
		meta["continue"] = continueToken
	}
	anyItems := make([]any, len(items))
	for i := range items {
		anyItems[i] = items[i]
	}
	return map[string]any{
		"apiVersion": "config.porch.kpt.dev/v1alpha1",
		"kind":       "RepositoryList",
		"metadata":   meta,
		"items":      anyItems,
	}
}

// repositoryTable builds a server-side meta.k8s.io/v1 Table carrying the
// Repository custom columns, one row per name. This mirrors what a Table-aware
// apiserver returns for `repo get` default output, so the test exercises the
// TablePrinter streaming path (decode Table page -> delegate -> columns).
func repositoryTable(continueToken string, names ...string) map[string]any {
	meta := map[string]any{}
	if continueToken != "" {
		meta["continue"] = continueToken
	}
	rows := make([]any, len(names))
	for i, n := range names {
		rows[i] = map[string]any{
			"cells": []any{n, "git", "Package", "main"},
		}
	}
	return map[string]any{
		"apiVersion": "meta.k8s.io/v1",
		"kind":       "Table",
		"metadata":   meta,
		"columnDefinitions": []any{
			map[string]any{"name": "NAME", "type": "string"},
			map[string]any{"name": "TYPE", "type": "string"},
			map[string]any{"name": "CONTENT", "type": "string"},
			map[string]any{"name": "BRANCH", "type": "string"},
		},
		"rows": rows,
	}
}

func repoKubeconfig(t *testing.T, server string) string {
	t.Helper()
	content := fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: test
contexts:
- context: {cluster: test, namespace: test-ns}
  name: test
clusters:
- cluster: {server: %s}
  name: test
users:
- name: test
  user: {}
`, server)
	p := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(p, []byte(content), 0600))
	return p
}

// TestRepoGetChunkSizeStreaming verifies `repo get` pages the Repository list:
// first request carries ?limit=N with no continue, the second carries the
// continue token, and all repositories across both pages are printed.
func TestRepoGetChunkSizeStreaming(t *testing.T) {
	ctx := context.Background()

	srv := &repoChunkingServer{t: t, continueToken: "page2"}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	kubeconfig := repoKubeconfig(t, ts.URL)
	nameFmt := "name"
	pf := get.NewGetPrintFlags()
	pf.OutputFormat = &nameFmt
	ns := "test-ns"

	r := &runner{
		ctx:        ctx,
		getFlags:   cmdutil.Options{ConfigFlags: &genericclioptions.ConfigFlags{KubeConfig: &kubeconfig, Namespace: &ns}},
		printFlags: pf,
		chunkSize:  2,
	}
	out := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(out)

	require.NoError(t, r.runE(cmd, []string{}))

	srv.mu.Lock()
	reqs := append([]repoListReq(nil), srv.listReqs...)
	srv.mu.Unlock()

	require.Len(t, reqs, 2, "expected two chunked LIST requests, got %+v", reqs)
	assert.Equal(t, "2", reqs[0].limit)
	assert.Empty(t, reqs[0].continue_)
	assert.Equal(t, "page2", reqs[1].continue_)

	o := out.String()
	for _, n := range []string{"repo-a", "repo-b", "repo-c"} {
		assert.Contains(t, o, n)
	}
}

// TestRepoGetChunkSizeStreamingTable verifies the DEFAULT human (table) output
// streams correctly across pages: the server returns one Table per page, and the
// streamed TablePrinter must render every row from both pages with the custom
// Repository columns while printing the column header exactly ONCE (the header
// dedup relies on reusing a single TablePrinter instance across pages).
func TestRepoGetChunkSizeStreamingTable(t *testing.T) {
	ctx := context.Background()

	srv := &repoChunkingServer{t: t, continueToken: "page2"}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	kubeconfig := repoKubeconfig(t, ts.URL)
	ns := "test-ns"

	r := &runner{
		ctx:          ctx,
		getFlags:     cmdutil.Options{ConfigFlags: &genericclioptions.ConfigFlags{KubeConfig: &kubeconfig, Namespace: &ns}},
		printFlags:   get.NewGetPrintFlags(),
		chunkSize:    2,
		requestTable: true, // default human output -> server-side Table per page
	}
	out := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(out)

	require.NoError(t, r.runE(cmd, []string{}))

	srv.mu.Lock()
	reqs := append([]repoListReq(nil), srv.listReqs...)
	srv.mu.Unlock()

	// Both pages fetched, following the continue token.
	require.Len(t, reqs, 2, "expected two chunked Table LIST requests, got %+v", reqs)
	assert.Equal(t, "2", reqs[0].limit)
	assert.Empty(t, reqs[0].continue_)
	assert.Equal(t, "page2", reqs[1].continue_)

	o := out.String()

	// All rows from BOTH pages render with the custom columns.
	for _, n := range []string{"repo-a", "repo-b", "repo-c"} {
		assert.Contains(t, o, n, "row %q from a paged Table must render", n)
	}
	assert.Contains(t, o, "CONTENT", "custom Repository columns must render, not generic fallback")

	// The column header must appear exactly ONCE across both pages (cross-page
	// dedup via the reused TablePrinter). Count the NAME header token on header
	// lines only -- a data row also contains a repo name but not the TYPE/CONTENT
	// header tokens, so match the header by a column unique to the header row.
	headerCount := strings.Count(o, "CONTENT")
	assert.Equal(t, 1, headerCount, "table header must print once across pages, got %d:\n%s", headerCount, o)
}
