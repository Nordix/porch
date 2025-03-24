// Copyright 2025 The kpt and Nephio Authors
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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetters(t *testing.T) {
	cells := make([]any, 1)
	cells[0] = int64(1234)

	_, retVal1 := getInt64Cell(cells, -1)
	assert.False(t, retVal1)

	val, retVal2 := getInt64Cell(cells, 0)
	assert.True(t, retVal2)
	assert.Equal(t, int64(1234), val)
}
