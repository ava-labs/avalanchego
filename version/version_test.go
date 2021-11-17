// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewDefaultVersion(t *testing.T) {
	v := NewDefaultVersion(1, 2, 3)

	assert.NotNil(t, v)
	assert.Equal(t, "v1.2.3", v.String())
	assert.Equal(t, 1, v.Major())
	assert.Equal(t, 2, v.Minor())
	assert.Equal(t, 3, v.Patch())
}
