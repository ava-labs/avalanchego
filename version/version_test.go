// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSemanticString(t *testing.T) {
	v := Semantic{
		Major: 1,
		Minor: 2,
		Patch: 3,
	}

	assert.Equal(t, "v1.2.3", v.String())
}
