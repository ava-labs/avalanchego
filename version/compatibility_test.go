// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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
		Major: 1,
		Minor: 4,
		Patch: 3,
	}
	minCompatable := &Application{
		Major: 1,
		Minor: 4,
		Patch: 0,
	}
	minCompatableTime := time.Unix(9000, 0)
	prevMinCompatable := &Application{
		Major: 1,
		Minor: 3,
		Patch: 0,
	}

	compatibility := NewCompatibility(
		v,
		minCompatable,
		minCompatableTime,
		prevMinCompatable,
	).(*compatibility)
	require.Equal(t, v, compatibility.Version())

	tests := []struct {
		peer       *Application
		time       time.Time
		compatible bool
	}{
		{
			peer: &Application{
				Major: 1,
				Minor: 5,
				Patch: 0,
			},
			time:       minCompatableTime,
			compatible: true,
		},
		{
			peer: &Application{
				Major: 1,
				Minor: 3,
				Patch: 5,
			},
			time:       time.Unix(8500, 0),
			compatible: true,
		},
		{
			peer: &Application{
				Major: 0,
				Minor: 1,
				Patch: 0,
			},
			time:       minCompatableTime,
			compatible: false,
		},
		{
			peer: &Application{
				Major: 1,
				Minor: 3,
				Patch: 5,
			},
			time:       minCompatableTime,
			compatible: false,
		},
		{
			peer: &Application{
				Major: 1,
				Minor: 2,
				Patch: 5,
			},
			time:       time.Unix(8500, 0),
			compatible: false,
		},
		{
			peer: &Application{
				Major: 1,
				Minor: 1,
				Patch: 5,
			},
			time:       time.Unix(7500, 0),
			compatible: false,
		},
	}
	for _, test := range tests {
		peer := test.peer
		compatibility.clock.Set(test.time)
		t.Run(fmt.Sprintf("%s-%s", peer, test.time), func(t *testing.T) {
			if err := compatibility.Compatible(peer); test.compatible && err != nil {
				t.Fatalf("incorrectly marked %s as incompatible with %s", peer, err)
			} else if !test.compatible && err == nil {
				t.Fatalf("incorrectly marked %s as compatible", peer)
			}
		})
	}
}
