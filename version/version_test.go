// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewDefaultVersion(t *testing.T) {
	v := NewDefaultVersion("avalanche", 1, 2, 3)

	assert.NotNil(t, v)
	assert.Equal(t, "avalanche/1.2.3", v.String())
	assert.Equal(t, "avalanche", v.App())
	assert.Equal(t, 1, v.Major())
	assert.Equal(t, 2, v.Minor())
	assert.Equal(t, 3, v.Patch())
	assert.NoError(t, v.Compatible(v))
	assert.False(t, v.Before(v))
}

func TestNewVersion(t *testing.T) {
	v := NewVersion("avalanche", ":", ",", 1, 2, 3)

	assert.NotNil(t, v)
	assert.Equal(t, "avalanche:1,2,3", v.String())
	assert.Equal(t, "avalanche", v.App())
	assert.Equal(t, 1, v.Major())
	assert.Equal(t, 2, v.Minor())
	assert.Equal(t, 3, v.Patch())
	assert.NoError(t, v.Compatible(v))
	assert.False(t, v.Before(v))
}

func TestIncompatibleApps(t *testing.T) {
	v0 := NewDefaultVersion("avalanche", 1, 2, 3)
	v1 := NewDefaultVersion("notavalanche", 1, 2, 3)

	assert.NotNil(t, v0)
	assert.NotNil(t, v1)
	assert.Error(t, v0.Compatible(v1))
	assert.Error(t, v1.Compatible(v0))

	assert.False(t, v0.Before(v1))
	assert.False(t, v1.Before(v0))
}

func TestIncompatibleMajor(t *testing.T) {
	v0 := NewDefaultVersion("avalanche", 1, 2, 3)
	v1 := NewDefaultVersion("avalanche", 2, 2, 3)

	assert.NotNil(t, v0)
	assert.NotNil(t, v1)
	assert.Error(t, v0.Compatible(v1))
	assert.Error(t, v1.Compatible(v0))

	assert.True(t, v0.Before(v1))
	assert.False(t, v1.Before(v0))
}

func TestIncompatibleMinor(t *testing.T) {
	v0 := NewDefaultVersion("avalanche", 1, 2, 3)
	v1 := NewDefaultVersion("avalanche", 1, 3, 3)

	assert.NotNil(t, v0)
	assert.NotNil(t, v1)
	assert.Error(t, v0.Compatible(v1))
	assert.Error(t, v1.Compatible(v0))

	assert.True(t, v0.Before(v1))
	assert.False(t, v1.Before(v0))
}

func TestCompatiblePatch(t *testing.T) {
	v0 := NewDefaultVersion("avalanche", 1, 2, 3)
	v1 := NewDefaultVersion("avalanche", 1, 2, 4)

	assert.NotNil(t, v0)
	assert.NotNil(t, v1)
	assert.NoError(t, v0.Compatible(v1))
	assert.NoError(t, v1.Compatible(v0))

	assert.True(t, v0.Before(v1))
	assert.False(t, v1.Before(v0))
}
