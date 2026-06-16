// Copyright 2022-2024, 2026 The kpt Authors
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
// limitations under the License

package porch

import (
	"regexp"
	"strings"

	pkgerrors "github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/validation"
)

// Valid relative paths should not start with '/', should not contain '..' components,
// and should only contain valid path characters
var validRelativePathRegex = regexp.MustCompile(`^(?:[a-zA-Z0-9._-]+(?:/[a-zA-Z0-9._-]+)*)?$`)

// Check that there are no '..' components in a path
var noDoubleDots = regexp.MustCompile(`(^|/)\.\.(/|$)`)

// IsValidSubpackageDir returns true if subpackageDir is valid, false otherwise.
func IsValidSubpackageDir(subpackageDir string) error {
	// Empty string is invalid, a subpackage directory must be a relative path.
	if subpackageDir == "" {
		return pkgerrors.Errorf("subpackage directory %q is invalid", subpackageDir)
	}

	// Check basic format and ensure it doesn't contain '..' or start with '/' or end with '/'
	if subpackageDir[0] == '/' || strings.HasSuffix(subpackageDir, "/") || noDoubleDots.MatchString(subpackageDir) {
		return pkgerrors.Errorf("subpackage directory %q is invalid, it cannot contain '..' or start with '/' or end with '/'", subpackageDir)
	}

	// Reject any path segment equal to "." (for example ".", "./subpkg", or "subpkg/./nested").
	for _, segment := range strings.Split(subpackageDir, "/") {
		if segment == "." {
			return pkgerrors.Errorf("subpackage directory %q is invalid, it cannot contain '.'", subpackageDir)
		}
	}

	if !validRelativePathRegex.MatchString(subpackageDir) {
		return pkgerrors.Errorf("subpackage directory %q is invalid, it must match regular expression %q", subpackageDir, validRelativePathRegex.String())
	}

	if _, err := ComposeSubpkgObjName(subpackageDir); err != nil {
		return err
	}

	return nil
}

func ComposeSubpkgObjName(subpackageDir string) (string, error) {
	if subpackageDir == "" {
		return "", pkgerrors.Errorf("subpackage directory %q is invalid", subpackageDir)
	}

	subpackageName := strings.ReplaceAll(subpackageDir, "/", ".")

	objNameErrs := validation.IsDNS1123Subdomain(subpackageName)

	if len(objNameErrs) == 0 {
		return subpackageName, nil
	} else {
		return "", pkgerrors.Errorf("subpackage resource name %q invalid: %s", subpackageName, strings.Join(objNameErrs, ","))
	}
}
