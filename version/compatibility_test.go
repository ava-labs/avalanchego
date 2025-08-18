// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
	minCompatible := &Application{
		Name:  Client,
		Major: 1,
		Minor: 4,
		Patch: 0,
	}
	minCompatibleTime := time.Unix(9000, 0)
	prevMinCompatible := &Application{
		Name:  Client,
		Major: 1,
		Minor: 3,
		Patch: 0,
	}

	compatibility := NewCompatibility(
		v,
		minCompatible,
		minCompatibleTime,
		prevMinCompatible,
	).(*compatibility)
	require.Equal(t, v, compatibility.Version())

	tests := []struct {
		peer        *Application
		time        time.Time
		expectedErr error
	}{
		{
			peer: &Application{
				Name:  Client,
				Major: 1,
				Minor: 5,
				Patch: 0,
			},
			time: minCompatibleTime,
		},
		{
			peer: &Application{
				Name:  Client,
				Major: 1,
				Minor: 3,
				Patch: 5,
			},
			time: time.Unix(8500, 0),
		},
		{
			peer: &Application{
				Name:  Client,
				Major: 0,
				Minor: 1,
				Patch: 0,
			},
			time:        minCompatibleTime,
			expectedErr: errDifferentMajor,
		},
		{
			peer: &Application{
				Name:  Client,
				Major: 1,
				Minor: 3,
				Patch: 5,
			},
			time:        minCompatibleTime,
			expectedErr: errIncompatible,
		},
		{
			peer: &Application{
				Name:  Client,
				Major: 1,
				Minor: 2,
				Patch: 5,
			},
			time:        time.Unix(8500, 0),
			expectedErr: errIncompatible,
		},
		{
			peer: &Application{
				Name:  Client,
				Major: 1,
				Minor: 1,
				Patch: 5,
			},
			time:        time.Unix(7500, 0),
			expectedErr: errIncompatible,
		},
	}
	for _, test := range tests {
		peer := test.peer
		compatibility.clock.Set(test.time)
		t.Run(fmt.Sprintf("%s-%s", peer, test.time), func(t *testing.T) {
			err := compatibility.Compatible(peer)
			require.ErrorIs(t, err, test.expectedErr)
		})
	}
}
