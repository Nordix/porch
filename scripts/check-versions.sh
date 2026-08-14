#!/usr/bin/env bash

# Copyright 2026 The kpt Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -e

# Script to check version consistency between source files and docs/config.toml
# Ensures public docs align with the code being built and tested.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

echo "Checking version consistency..."
echo ""

# Extract versions from sources
echo "=== Extracting versions from source files ==="
go_version=$(grep '^go ' go.mod | awk '{print $2}')
echo "Go version (go.mod): $go_version"

kpt_version=$(grep 'github.com/kptdev/kpt ' go.mod | grep -oP 'v\d+\.\d+\.\d+(-[a-zA-Z0-9.]+)?' | head -1)
echo "kpt version (go.mod): $kpt_version"

kind_version=$(awk '/helm\/kind-action@v1/,/version:/ {if (/version:/) print $2}' .github/workflows/porch-e2e-ci-jobs.yaml | head -1)
echo "kind version (.github/workflows): $kind_version"

echo ""
echo "=== Versions in docs/config.toml ==="

config_go=$(grep '^version_go = ' docs/config.toml | cut -d'"' -f2)
echo "Go version (config): $config_go"

config_kpt=$(grep '^version_kpt = ' docs/config.toml | cut -d'"' -f2)
echo "kpt version (config): $config_kpt"

config_kind=$(grep '^version_kind = ' docs/config.toml | cut -d'"' -f2)
echo "kind version (config): $config_kind"

config_kube=$(grep '^version_kube = ' docs/config.toml | cut -d'"' -f2)
echo "Kubernetes version (config): $config_kube"

echo ""
echo "=== Consistency Check ==="

errors=0

# Go version check (CRITICAL - we control this in go.mod)
if [ "$go_version" != "$config_go" ]; then
  echo "FAIL: Go version mismatch - source: $go_version, config: $config_go"
  errors=$((errors + 1))
else
  echo "✓ Go version matches: $go_version"
fi

# kpt version check (CRITICAL - core dependency in go.mod)
if [ "$kpt_version" != "v$config_kpt" ]; then
  echo "FAIL: kpt version mismatch - source: $kpt_version, config: v$config_kpt"
  errors=$((errors + 1))
else
  echo "✓ kpt version matches: $kpt_version"
fi

# kind version check (WARNING - test environment, controls k8s version)
if [ "$kind_version" != "v$config_kind" ]; then
  echo "WARN: kind version mismatch - source: $kind_version, config: v$config_kind"
  echo "      Consider updating docs/config.toml to match the test environment"
else
  echo "✓ kind version matches: $kind_version"
fi

echo "ℹ Kubernetes version (from config): v$config_kube (derived from kind)"

echo ""
if [ "$errors" -gt 0 ]; then
  echo "FAILED: $errors critical version mismatch(es) detected."
  echo ""
  echo "To fix, update docs/config.toml:"
  if [ "$go_version" != "$config_go" ]; then
    echo "  version_go = \"$go_version\""
  fi
  if [ "$kpt_version" != "v$config_kpt" ]; then
    echo "  version_kpt = \"${kpt_version#v}\""
  fi
  exit 1
fi

echo "SUCCESS: Critical versions are aligned with code."
exit 0
