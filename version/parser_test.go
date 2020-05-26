// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewDefaultParser(t *testing.T) {
	p := NewDefaultParser()

	v, err := p.Parse("ava/1.2.3")

	assert.NoError(t, err)
	assert.NotNil(t, v)
	assert.Equal(t, "ava/1.2.3", v.String())
	assert.Equal(t, "ava", v.App())
	assert.Equal(t, 1, v.Major())
	assert.Equal(t, 2, v.Minor())
	assert.Equal(t, 3, v.Patch())
	assert.NoError(t, v.Compatible(v))
	assert.False(t, v.Before(v))

	badVersions := []string{
		"",
		"ava/",
		"ava/z.0.0",
		"ava/0.z.0",
		"ava/0.0.z",
	}
	for _, badVersion := range badVersions {
		_, err := p.Parse(badVersion)
		assert.Error(t, err)
	}
}

func TestNewParser(t *testing.T) {
	p := NewParser(":", ",")

	v, err := p.Parse("ava:1,2,3")

	assert.NoError(t, err)
	assert.NotNil(t, v)
	assert.Equal(t, "ava:1,2,3", v.String())
	assert.Equal(t, "ava", v.App())
	assert.Equal(t, 1, v.Major())
	assert.Equal(t, 2, v.Minor())
	assert.Equal(t, 3, v.Patch())
	assert.NoError(t, v.Compatible(v))
	assert.False(t, v.Before(v))
}
