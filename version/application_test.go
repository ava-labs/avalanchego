// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewDefaultApplication(t *testing.T) {
	v := &Application{
		Major: 1,
		Minor: 2,
		Patch: 3,
	}

	require.Equal(t, "avalanche/1.2.3", v.String())
	require.NoError(t, v.Compatible(v))
	require.False(t, v.Before(v))
}

func TestComparingVersions(t *testing.T) {
	tests := []struct {
		myVersion   *Application
		peerVersion *Application
		compatible  bool
		before      bool
	}{
		{
			myVersion: &Application{
				Major: 1,
				Minor: 2,
				Patch: 3,
			},
			peerVersion: &Application{
				Major: 1,
				Minor: 2,
				Patch: 3,
			},
			compatible: true,
			before:     false,
		},
		{
			myVersion: &Application{
				Major: 1,
				Minor: 2,
				Patch: 4,
			},
			peerVersion: &Application{
				Major: 1,
				Minor: 2,
				Patch: 3,
			},
			compatible: true,
			before:     false,
		},
		{
			myVersion: &Application{
				Major: 1,
				Minor: 2,
				Patch: 3,
			},
			peerVersion: &Application{
				Major: 1,
				Minor: 2,
				Patch: 4,
			},
			compatible: true,
			before:     true,
		},
		{
			myVersion: &Application{
				Major: 1,
				Minor: 3,
				Patch: 3,
			},
			peerVersion: &Application{
				Major: 1,
				Minor: 2,
				Patch: 3,
			},
			compatible: true,
			before:     false,
		},
		{
			myVersion: &Application{
				Major: 1,
				Minor: 2,
				Patch: 3,
			},
			peerVersion: &Application{
				Major: 1,
				Minor: 3,
				Patch: 3,
			},
			compatible: true,
			before:     true,
		},
		{
			myVersion: &Application{
				Major: 2,
				Minor: 2,
				Patch: 3,
			},
			peerVersion: &Application{
				Major: 1,
				Minor: 2,
				Patch: 3,
			},
			compatible: false,
			before:     false,
		},
		{
			myVersion: &Application{
				Major: 1,
				Minor: 2,
				Patch: 3,
			},
			peerVersion: &Application{
				Major: 2,
				Minor: 2,
				Patch: 3,
			},
			compatible: true,
			before:     true,
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
