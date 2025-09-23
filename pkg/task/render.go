// Copyright 2022, 2024 The kpt and Nephio Authors
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

package task

import (
	"context"
	"encoding/json"
	iofs "io/fs"
	"path"
	"strings"

	api "github.com/nephio-project/porch/api/porch/v1alpha1"
	configapi "github.com/nephio-project/porch/api/porchconfig/v1alpha1"
	"github.com/nephio-project/porch/internal/kpt/fnruntime"
	"github.com/nephio-project/porch/pkg/kpt"
	fnresult "github.com/nephio-project/porch/pkg/kpt/api/fnresult/v1"
	kptfilev1 "github.com/nephio-project/porch/pkg/kpt/api/kptfile/v1"
	"github.com/nephio-project/porch/pkg/kpt/fn"
	"github.com/nephio-project/porch/pkg/repository"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/klog/v2"
	"sigs.k8s.io/kustomize/kyaml/filesys"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

type renderPackageMutation struct {
	runtime       fn.FunctionRuntime
	runnerOptions fnruntime.RunnerOptions
	// For hierarchical rendering across subpackages
	repoOpener        repository.RepositoryOpener
	referenceResolver repository.ReferenceResolver
	// Context of the package being rendered
	namespace     string
	repoName      string
	packageName   string
	workspaceName string
	// Current package revision to anchor discovery
	current repository.PackageRevision
}

var _ mutation = &renderPackageMutation{}

func (m *renderPackageMutation) apply(ctx context.Context, resources repository.PackageResources) (repository.PackageResources, *api.TaskResult, error) {
	ctx, span := tracer.Start(ctx, "renderPackageMutation::apply", trace.WithAttributes())
	defer span.End()

	fs := filesys.MakeFsInMemory()
	taskResult := &api.TaskResult{
		RenderStatus: &api.RenderStatus{},
	}

	pkgPath, err := m.writeCompositeOrSingle(ctx, fs, resources)
	if err != nil {
		return repository.PackageResources{}, nil, err
	}

	if pkgPath == "" {
		// We need this for the no-resources case
		// TODO: we should handle this better
		klog.Warningf("skipping render as no package was found")
	} else {
		renderer := kpt.NewRenderer(m.runnerOptions)
		result, err := renderer.Render(ctx, fs, fn.RenderOptions{
			PkgPath: pkgPath,
			Runtime: m.runtime,
		})
		if result != nil {
			var rr api.ResultList
			err := convertResultList(result, &rr)
			if err != nil {
				return repository.PackageResources{}, taskResult, err
			}
			taskResult.RenderStatus.Result = rr
		}
		if err != nil {
			taskResult.RenderStatus.Err = err.Error()
			return repository.PackageResources{}, taskResult, err
		}
	}

	renderedResources, err := m.readFilteredResources(fs)
	if err != nil {
		return repository.PackageResources{}, taskResult, err
	}

	// TODO: There are internal tasks not represented in the API; Update the Apply interface to enable them.
	return renderedResources, taskResult, nil
}

func convertResultList(in *fnresult.ResultList, out *api.ResultList) error {
	if in == nil {
		return nil
	}
	srcBytes, err := json.Marshal(in)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(srcBytes, &out); err != nil {
		return err
	}
	return nil
}

// TODO: Implement filesystem abstraction directly rather than on top of PackageResources
func writeResources(fs filesys.FileSystem, resources repository.PackageResources) (string, error) {
	var packageDir string // path to the topmost directory containing Kptfile
	for k, v := range resources.Contents {
		dir := path.Dir(k)
		if dir == "." {
			dir = "/"
		}
		if err := fs.MkdirAll(dir); err != nil {
			return "", err
		}
		base := path.Base(k)
		if err := fs.WriteFile(path.Join(dir, base), []byte(v)); err != nil {
			return "", err
		}
		if base == "Kptfile" {
			// Found Kptfile. Check if the current directory is ancestor of the current
			// topmost package directory. If so, use it instead.
			if packageDir == "" || dir == "/" || strings.HasPrefix(packageDir, dir+"/") {
				packageDir = dir
			}
		}
	}
	// Return topmost directory containing Kptfile
	return packageDir, nil
}

func readResources(fs filesys.FileSystem) (repository.PackageResources, error) {
	contents := map[string]string{}

	if err := fs.Walk("/", func(path string, info iofs.FileInfo, err error) error {
		if info.Mode().IsRegular() {
			data, err := fs.ReadFile(path)
			if err != nil {
				return err
			}
			contents[strings.TrimPrefix(path, "/")] = string(data)
		}
		return nil
	}); err != nil {
		return repository.PackageResources{}, err
	}

	return repository.PackageResources{
		Contents: contents,
	}, nil
}

// writeCompositeOrSingle writes either a composite tree (root + subpackages) or falls back to single-package (default).
// It returns the root path to pass to the renderer.
func (m *renderPackageMutation) writeCompositeOrSingle(ctx context.Context, fs filesys.FileSystem, resources repository.PackageResources) (string, error) {
	// Only attempt composite rendering when we have a current revision and repo context,
	// and the Kptfile explicitly opts in via annotation porch.kpt.dev/subpackage: "true".
	if !hasSubpackageOptIn(resources) {
		klog.Infof("composite-render: skip - no Kptfile opt-in porch.kpt.dev/subpackage=true")
		return writeResources(fs, resources)
	}

	var repo repository.Repository
	if m.repoName == "" {
		if m.current != nil {
			m.repoName = m.current.Key().RKey().Name
		}
	}
	klog.Infof("composite-render: repoName=%q namespace=%q", m.repoName, m.namespace)

	if m.repoName != "" {
		repoSpec := &configapi.Repository{}
		if err := m.referenceResolver.ResolveReference(ctx, m.namespace, m.repoName, repoSpec); err != nil {
			klog.Infof("composite-render: reference resolve failed: %v (fallback single)", err)
			return writeResources(fs, resources)
		}
		var err error
		repo, err = m.repoOpener.OpenRepository(ctx, repoSpec)
		if err != nil {
			klog.Infof("composite-render: open repository failed: %v (fallback single)", err)
			return writeResources(fs, resources)
		}

		klog.Infof("composite-render: opened repo %s/%s", repo.Key().Namespace, repo.Key().Name)
	} else {
		klog.Infof("composite-render: empty repoName (fallback single)")
		return writeResources(fs, resources)
	}

	// Determine anchor and workspace
	var anchorKey repository.PackageRevisionKey
	var anchorFullPath string
	var workspaceName string
	if m.current != nil {
		anchorKey = m.current.Key()
		anchorFullPath = anchorKey.PKey().ToFullPathname()
		workspaceName = anchorKey.WorkspaceName
		klog.Infof("composite-render: anchor=%q workspace=%q (from current)", anchorFullPath, workspaceName)
	} else if m.packageName != "" && m.workspaceName != "" {
		anchorFullPath = m.packageName
		workspaceName = m.workspaceName
		klog.Infof("composite-render: anchor=%q workspace=%q (from apply params)", anchorFullPath, workspaceName)
	} else {
		klog.Infof("composite-render: insufficient anchor context (fallback single)")
		return writeResources(fs, resources)
	}

	// List all package revisions in repository and filter by same workspace
	prList, err := repo.ListPackageRevisions(ctx, repository.ListPackageRevisionFilter{})
	if err != nil {
		klog.Infof("composite-render: list package revisions failed: %v (fallback single)", err)
		return writeResources(fs, resources)
	}
	klog.Infof("composite-render: listed %d package revisions", len(prList))

	// Determine root: shortest full path that is prefix of anchor
	rootFullPath := anchorFullPath
	for _, pr := range prList {
		if pr.Key().WorkspaceName != workspaceName {
			continue
		}
		candidate := pr.Key().PKey().ToFullPathname()
		if strings.HasPrefix(anchorFullPath+"/", candidate+"/") {
			if len(candidate) < len(rootFullPath) {
				rootFullPath = candidate
			}
		}
	}
	klog.Infof("composite-render: computed rootFullPath=%q", rootFullPath)

	// Build composite set: all revisions under rootFullPath in same workspace
	type prWithRes struct {
		pr  repository.PackageRevision
		res *api.PackageRevisionResources
	}
	var tree []prWithRes
	for _, pr := range prList {
		if pr.Key().WorkspaceName != workspaceName {
			continue
		}
		full := pr.Key().PKey().ToFullPathname()
		if strings.HasPrefix(full+"/", rootFullPath+"/") {
			prRes, err := pr.GetResources(ctx)
			if err != nil {
				klog.Infof("composite-render: get resources failed for %v: %v (fallback single)", pr.Key(), err)
				return writeResources(fs, resources)
			}
			tree = append(tree, prWithRes{pr: pr, res: prRes})
		}
	}
	klog.Infof("composite-render: tree size=%d", len(tree))

	// Write composite: mount root at "/", subpackages at their relative subdir
	for _, node := range tree {
		full := node.pr.Key().PKey().ToFullPathname()
		relBase := strings.TrimPrefix(full, rootFullPath)
		relBase = strings.TrimPrefix(relBase, "/")
		for k, v := range node.res.Spec.Resources {
			var outPath string
			if relBase == "" {
				outPath = k
			} else {
				outPath = path.Join(relBase, k)
			}
			dir := path.Dir(outPath)
			if dir == "." {
				dir = "/"
			}
			if err := fs.MkdirAll(dir); err != nil {
				return "", err
			}
			if err := fs.WriteFile(path.Join(dir, path.Base(outPath)), []byte(v)); err != nil {
				return "", err
			}
		}
	}
	klog.Infof("composite-render: wrote composite resources for %d packages under root %q", len(tree), rootFullPath)

	// Finally, overlay the provided resources at the anchor path
	currRelBase := strings.TrimPrefix(anchorFullPath, rootFullPath)
	currRelBase = strings.TrimPrefix(currRelBase, "/")
	klog.Infof("composite-render: merging provided resources at currRelBase=%q", currRelBase)
	for k, v := range resources.Contents {
		outPath := k
		if currRelBase != "" {
			outPath = path.Join(currRelBase, k)
		}
		dir := path.Dir(outPath)
		if dir == "." {
			dir = "/"
		}
		if err := fs.MkdirAll(dir); err != nil {
			return "", err
		}
		if err := fs.WriteFile(path.Join(dir, path.Base(outPath)), []byte(v)); err != nil {
			return "", err
		}
	}

	// Root path is "/" (topmost package directory)
	return "/", nil
}

// hasSubpackageOptIn returns true if Kptfile contains annotation porch.kpt.dev/subpackage: "true"
func hasSubpackageOptIn(resources repository.PackageResources) bool {
	kptBytes, ok := resources.Contents["Kptfile"]
	if !ok || kptBytes == "" {
		return false
	}
	var kf kptfilev1.KptFile
	if err := yaml.Unmarshal([]byte(kptBytes), &kf); err != nil {
		return false
	}
	if kf.Annotations == nil {
		return false
	}
	v, ok := kf.Annotations["porch.kpt.dev/subpackage"]
	return ok && v == "true"
}

// readFilteredResources reads the rendered filesystem and trims it to the current package subtree
func (m *renderPackageMutation) readFilteredResources(fs filesys.FileSystem) (repository.PackageResources, error) {
	// If no current context, return all
	if m.current == nil {
		return readResources(fs)
	}
	// Compute subdir prefix
	anchorFullPath := m.current.Key().PKey().ToFullPathname()

	// Compute relBase by looking for a file unique to current package: we use package directory name.
	relBase := path.Base(anchorFullPath)

	contents := map[string]string{}
	if err := fs.Walk("/", func(p string, info iofs.FileInfo, err error) error {
		if info.Mode().IsRegular() {
			trimmed := strings.TrimPrefix(p, "/")
			// Match either top-level (if current is root) or under relBase/
			if strings.HasPrefix(trimmed, relBase+"/") || relBase == trimmed && !strings.Contains(trimmed, "/") {
				data, err := fs.ReadFile(p)
				if err != nil {
					return err
				}
				// Strip relBase/ prefix if present
				key := trimmed
				if newKey, ok := strings.CutPrefix(key, relBase+"/"); ok {
					key = newKey
				} else if key == relBase { // keep as-is
					key = path.Base(key)
				}

				contents[key] = string(data)
			}
		}
		return nil
	}); err != nil {
		return repository.PackageResources{}, err
	}

	// If nothing matched (e.g., current is root), return all
	if len(contents) == 0 {
		return readResources(fs)
	}
	return repository.PackageResources{Contents: contents}, nil
}
