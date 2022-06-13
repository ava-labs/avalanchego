// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultVersionParser(t *testing.T) {
	v, err := DefaultParser.Parse("v1.2.3")

	assert.NoError(t, err)
	assert.NotNil(t, v)
	assert.Equal(t, "v1.2.3", v.String())
	assert.Equal(t, 1, v.Major())
	assert.Equal(t, 2, v.Minor())
	assert.Equal(t, 3, v.Patch())

	badVersions := []string{
		"",
		"1.2.3",
		"vz.2.3",
		"v1.z.3",
		"v1.2.z",
	}
	for _, badVersion := range badVersions {
		_, err := DefaultParser.Parse(badVersion)
		assert.Error(t, err)
	}
}

func TestDefaultApplicationParser(t *testing.T) {
	v, err := DefaultApplicationParser.Parse("avalanche/1.2.3")

	assert.NoError(t, err)
	assert.NotNil(t, v)
	assert.Equal(t, "avalanche/1.2.3", v.String())
	assert.Equal(t, "avalanche", v.App())
	assert.Equal(t, 1, v.Major())
	assert.Equal(t, 2, v.Minor())
	assert.Equal(t, 3, v.Patch())
	assert.NoError(t, v.Compatible(v))
	assert.False(t, v.Before(v))

	badVersions := []string{
		"",
		"avalanche/",
		"avalanche/z.0.0",
		"avalanche/0.z.0",
		"avalanche/0.0.z",
	}
	for _, badVersion := range badVersions {
		_, err := DefaultApplicationParser.Parse(badVersion)
		assert.Error(t, err)
	}
}

func TestNewApplicationParser(t *testing.T) {
	p := NewApplicationParser(":", ",")

	v, err := p.Parse("avalanche:1,2,3")

	assert.NoError(t, err)
	assert.NotNil(t, v)
	assert.Equal(t, "avalanche:1,2,3", v.String())
	assert.Equal(t, "avalanche", v.App())
	assert.Equal(t, 1, v.Major())
	assert.Equal(t, 2, v.Minor())
	assert.Equal(t, 3, v.Patch())
	assert.NoError(t, v.Compatible(v))
	assert.False(t, v.Before(v))
}
