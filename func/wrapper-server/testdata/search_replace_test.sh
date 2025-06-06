#!/bin/bash

# Copyright 2025 The kpt and Nephio Authors
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

if [ "$#" -ne 2 ]; then
  echo "Usage: $0 <search_term> <replace_term>"
  exit 1
fi

input=$(cat)

search_term=$1
replace_term=$2


printf "%s" "$input" | awk -v search="$search_term" -v replace="$replace_term" '{
  gsub(search, replace)
  print
}'
