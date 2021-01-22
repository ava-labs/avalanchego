// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"fmt"
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

func TestNewDefaultApplicationVersion(t *testing.T) {
	v := NewDefaultApplicationVersion("avalanche", 1, 2, 3)

	assert.NotNil(t, v)
	assert.Equal(t, "avalanche/1.2.3", v.String())
	assert.Equal(t, "avalanche", v.App())
	assert.Equal(t, 1, v.Major())
	assert.Equal(t, 2, v.Minor())
	assert.Equal(t, 3, v.Patch())
	assert.NoError(t, v.Compatible(v))
	assert.False(t, v.Before(v))
}

func TestNewApplicationVersion(t *testing.T) {
	v := NewApplicationVersion("avalanche", ":", ",", 1, 2, 3)

	assert.NotNil(t, v)
	assert.Equal(t, "avalanche:1,2,3", v.String())
	assert.Equal(t, "avalanche", v.App())
	assert.Equal(t, 1, v.Major())
	assert.Equal(t, 2, v.Minor())
	assert.Equal(t, 3, v.Patch())
	assert.NoError(t, v.Compatible(v))
	assert.False(t, v.Before(v))
}

func TestComparingVersions(t *testing.T) {
	tests := []struct {
		myVersion   ApplicationVersion
		peerVersion ApplicationVersion
		compatible  bool
		before      bool
	}{
		{
			myVersion:   NewDefaultApplicationVersion("avalanche", 1, 2, 3),
			peerVersion: NewDefaultApplicationVersion("avalanche", 1, 2, 3),
			compatible:  true,
			before:      false,
		},
		{
			myVersion:   NewDefaultApplicationVersion("avalanche", 1, 2, 4),
			peerVersion: NewDefaultApplicationVersion("avalanche", 1, 2, 3),
			compatible:  true,
			before:      false,
		},
		{
			myVersion:   NewDefaultApplicationVersion("avalanche", 1, 2, 3),
			peerVersion: NewDefaultApplicationVersion("avalanche", 1, 2, 4),
			compatible:  true,
			before:      true,
		},
		{
			myVersion:   NewDefaultApplicationVersion("avalanche", 1, 3, 3),
			peerVersion: NewDefaultApplicationVersion("avalanche", 1, 2, 3),
			compatible:  true,
			before:      false,
		},
		{
			myVersion:   NewDefaultApplicationVersion("avalanche", 1, 2, 3),
			peerVersion: NewDefaultApplicationVersion("avalanche", 1, 3, 3),
			compatible:  true,
			before:      true,
		},
		{
			myVersion:   NewDefaultApplicationVersion("avalanche", 2, 2, 3),
			peerVersion: NewDefaultApplicationVersion("avalanche", 1, 2, 3),
			compatible:  false,
			before:      false,
		},
		{
			myVersion:   NewDefaultApplicationVersion("avalanche", 1, 2, 3),
			peerVersion: NewDefaultApplicationVersion("avalanche", 2, 2, 3),
			compatible:  true,
			before:      true,
		},
		{
			myVersion:   NewDefaultApplicationVersion("avax", 1, 2, 4),
			peerVersion: NewDefaultApplicationVersion("avalanche", 1, 2, 3),
			compatible:  false,
			before:      false,
		},
		{
			myVersion:   NewDefaultApplicationVersion("avalanche", 1, 2, 3),
			peerVersion: NewDefaultApplicationVersion("avax", 1, 2, 3),
			compatible:  false,
			before:      false,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%s %s", test.myVersion, test.peerVersion), func(t *testing.T) {
			err := test.myVersion.Compatible(test.peerVersion)
			if test.compatible && err != nil {
				t.Fatalf("Expected version to be compatible but returned: %s",
					err)
			} else if !test.compatible && err == nil {
				t.Fatalf("Expected version to be incompatible but returned no error")
			}
			before := test.myVersion.Before(test.peerVersion)
			if test.before && !before {
				t.Fatalf("Expected version to be before the peer version but wasn't")
			} else if !test.before && before {
				t.Fatalf("Expected version not to be before the peer version but was")
			}
		})
	}
}

func TestSortVersions(t *testing.T) {
	v0 := NewDefaultVersion(1, 0, 0)
	v1 := NewDefaultVersion(1, 1, 0)
	v2 := NewDefaultVersion(1, 2, 1)
	v3 := NewDefaultVersion(2, 1, 0)
	vers := []Version{
		v3,
		v0,
		v2,
		v1,
	}

	SortAscendingVersions(vers)

	assert.Len(t, vers, 4)

	assert.Equal(t, vers[0], v0)
	assert.Equal(t, vers[1], v1)
	assert.Equal(t, vers[2], v2)
	assert.Equal(t, vers[3], v3)

	SortDescendingVersions(vers)

	assert.Len(t, vers, 4)

	assert.Equal(t, vers[0], v3)
	assert.Equal(t, vers[1], v2)
	assert.Equal(t, vers[2], v1)
	assert.Equal(t, vers[3], v0)
}

func TestSortApplicationVersions(t *testing.T) {
	v0 := NewDefaultApplicationVersion("avalanche", 1, 0, 0)
	v1 := NewDefaultApplicationVersion("avalanche", 1, 1, 0)
	v2 := NewDefaultApplicationVersion("avalanche", 1, 2, 1)
	v3 := NewDefaultApplicationVersion("avalanche", 2, 1, 0)
	vers := []Version{
		v3,
		v0,
		v2,
		v1,
	}

	SortAscendingVersions(vers)

	assert.Len(t, vers, 4)

	assert.Equal(t, vers[0], v0)
	assert.Equal(t, vers[1], v1)
	assert.Equal(t, vers[2], v2)
	assert.Equal(t, vers[3], v3)

	SortDescendingVersions(vers)

	assert.Len(t, vers, 4)

	assert.Equal(t, vers[0], v3)
	assert.Equal(t, vers[1], v2)
	assert.Equal(t, vers[2], v1)
	assert.Equal(t, vers[3], v0)
}
