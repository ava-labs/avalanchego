// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewDefaultApplication(t *testing.T) {
	v := NewDefaultApplication("avalanche", 1, 2, 3)

	assert.NotNil(t, v)
	assert.Equal(t, "avalanche/1.2.3", v.String())
	assert.Equal(t, "avalanche", v.App())
	assert.Equal(t, 1, v.Major())
	assert.Equal(t, 2, v.Minor())
	assert.Equal(t, 3, v.Patch())
	assert.NoError(t, v.Compatible(v))
	assert.False(t, v.Before(v))
}

func TestNewApplication(t *testing.T) {
	v := NewApplication("avalanche", ":", ",", 1, 2, 3)

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
		myVersion   Application
		peerVersion Application
		compatible  bool
		before      bool
	}{
		{
			myVersion:   NewDefaultApplication("avalanche", 1, 2, 3),
			peerVersion: NewDefaultApplication("avalanche", 1, 2, 3),
			compatible:  true,
			before:      false,
		},
		{
			myVersion:   NewDefaultApplication("avalanche", 1, 2, 4),
			peerVersion: NewDefaultApplication("avalanche", 1, 2, 3),
			compatible:  true,
			before:      false,
		},
		{
			myVersion:   NewDefaultApplication("avalanche", 1, 2, 3),
			peerVersion: NewDefaultApplication("avalanche", 1, 2, 4),
			compatible:  true,
			before:      true,
		},
		{
			myVersion:   NewDefaultApplication("avalanche", 1, 3, 3),
			peerVersion: NewDefaultApplication("avalanche", 1, 2, 3),
			compatible:  true,
			before:      false,
		},
		{
			myVersion:   NewDefaultApplication("avalanche", 1, 2, 3),
			peerVersion: NewDefaultApplication("avalanche", 1, 3, 3),
			compatible:  true,
			before:      true,
		},
		{
			myVersion:   NewDefaultApplication("avalanche", 2, 2, 3),
			peerVersion: NewDefaultApplication("avalanche", 1, 2, 3),
			compatible:  false,
			before:      false,
		},
		{
			myVersion:   NewDefaultApplication("avalanche", 1, 2, 3),
			peerVersion: NewDefaultApplication("avalanche", 2, 2, 3),
			compatible:  true,
			before:      true,
		},
		{
			myVersion:   NewDefaultApplication("avax", 1, 2, 4),
			peerVersion: NewDefaultApplication("avalanche", 1, 2, 3),
			compatible:  false,
			before:      false,
		},
		{
			myVersion:   NewDefaultApplication("avalanche", 1, 2, 3),
			peerVersion: NewDefaultApplication("avax", 1, 2, 3),
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
