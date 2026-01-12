// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCompatibility(t *testing.T) {
	v := &Application{
		Name:  Client,
		Major: 1,
		Minor: 4,
		Patch: 3,
	}
	minCompatibleAfterUpgrade := &Application{
		Name:  Client,
		Major: 1,
		Minor: 4,
		Patch: 0,
	}
	upgradeTime := time.Unix(9000, 0)
	minCompatible := &Application{
		Name:  Client,
		Major: 1,
		Minor: 3,
		Patch: 0,
	}

	compatibility := &Compatibility{
		Current:                   v,
		MinCompatibleAfterUpgrade: minCompatibleAfterUpgrade,
		MinCompatible:             minCompatible,
		UpgradeTime:               upgradeTime,
	}

	tests := []struct {
		peer     *Application
		time     time.Time
		expected bool
	}{
		{
			peer: &Application{
				Name:  Client,
				Major: 1,
				Minor: 5,
				Patch: 0,
			},
			time:     upgradeTime,
			expected: true,
		},
		{
			peer: &Application{
				Name:  Client,
				Major: 1,
				Minor: 3,
				Patch: 5,
			},
			time:     time.Unix(8500, 0),
			expected: true,
		},
		{
			peer: &Application{
				Name:  Client,
				Major: 0,
				Minor: 1,
				Patch: 0,
			},
			time:     upgradeTime,
			expected: false,
		},
		{
			peer: &Application{
				Name:  Client,
				Major: 2,
				Minor: 1,
				Patch: 0,
			},
			time:     upgradeTime,
			expected: false,
		},
		{
			peer: &Application{
				Name:  Client,
				Major: 1,
				Minor: 3,
				Patch: 5,
			},
			time:     upgradeTime,
			expected: false,
		},
		{
			peer: &Application{
				Name:  Client,
				Major: 1,
				Minor: 2,
				Patch: 5,
			},
			time:     time.Unix(8500, 0),
			expected: false,
		},
		{
			peer: &Application{
				Name:  Client,
				Major: 1,
				Minor: 1,
				Patch: 5,
			},
			time:     time.Unix(7500, 0),
			expected: false,
		},
	}
	for _, test := range tests {
		peer := test.peer
		compatibility.clock.Set(test.time)
		t.Run(fmt.Sprintf("%s-%s", peer, test.time), func(t *testing.T) {
			require.Equal(t, test.expected, compatibility.Compatible(peer))
		})
	}
}
