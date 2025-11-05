// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewDefaultApplication(t *testing.T) {
	require := require.New(t)

	v := &Application{
		Name:  Client,
		Major: 1,
		Minor: 2,
		Patch: 3,
	}

	require.Equal("avalanchego/1.2.3", v.String())
	require.False(v.Before(v))
}

func TestComparingVersions(t *testing.T) {
	tests := []struct {
		myVersion   *Application
		peerVersion *Application
		before      bool
	}{
		{
			myVersion: &Application{
				Name:  Client,
				Major: 1,
				Minor: 2,
				Patch: 3,
			},
			peerVersion: &Application{
				Name:  Client,
				Major: 1,
				Minor: 2,
				Patch: 3,
			},
			before: false,
		},
		{
			myVersion: &Application{
				Name:  Client,
				Major: 1,
				Minor: 2,
				Patch: 4,
			},
			peerVersion: &Application{
				Name:  Client,
				Major: 1,
				Minor: 2,
				Patch: 3,
			},
			before: false,
		},
		{
			myVersion: &Application{
				Name:  Client,
				Major: 1,
				Minor: 2,
				Patch: 3,
			},
			peerVersion: &Application{
				Name:  Client,
				Major: 1,
				Minor: 2,
				Patch: 4,
			},
			before: true,
		},
		{
			myVersion: &Application{
				Name:  Client,
				Major: 1,
				Minor: 3,
				Patch: 3,
			},
			peerVersion: &Application{
				Name:  Client,
				Major: 1,
				Minor: 2,
				Patch: 3,
			},
			before: false,
		},
		{
			myVersion: &Application{
				Name:  Client,
				Major: 1,
				Minor: 2,
				Patch: 3,
			},
			peerVersion: &Application{
				Name:  Client,
				Major: 1,
				Minor: 3,
				Patch: 3,
			},
			before: true,
		},
		{
			myVersion: &Application{
				Name:  Client,
				Major: 2,
				Minor: 2,
				Patch: 3,
			},
			peerVersion: &Application{
				Name:  Client,
				Major: 1,
				Minor: 2,
				Patch: 3,
			},
			before: false,
		},
		{
			myVersion: &Application{
				Name:  Client,
				Major: 1,
				Minor: 2,
				Patch: 3,
			},
			peerVersion: &Application{
				Name:  Client,
				Major: 2,
				Minor: 2,
				Patch: 3,
			},
			before: true,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%s %s", test.myVersion, test.peerVersion), func(t *testing.T) {
			require := require.New(t)
			require.Equal(test.before, test.myVersion.Before(test.peerVersion))
		})
	}
}
