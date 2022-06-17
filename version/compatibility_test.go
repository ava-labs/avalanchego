// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
	minUnmaskable := &Application{
		Major: 1,
		Minor: 2,
		Patch: 0,
	}
	minUnmaskableTime := time.Unix(7000, 0)
	prevMinUnmaskable := &Application{
		Major: 1,
		Minor: 1,
		Patch: 0,
	}

	compatibility := NewCompatibility(
		v,
		minCompatable,
		minCompatableTime,
		prevMinCompatable,
		minUnmaskable,
		minUnmaskableTime,
		prevMinUnmaskable,
	).(*compatibility)
	assert.Equal(t, v, compatibility.Version())
	assert.Equal(t, minUnmaskableTime, compatibility.MaskTime())

	tests := []struct {
		peer        *Application
		time        time.Time
		connectable bool
		compatible  bool
		unmaskable  bool
		wontMask    bool
	}{
		{
			peer: &Application{
				Major: 1,
				Minor: 5,
				Patch: 0,
			},
			time:        minCompatableTime,
			connectable: true,
			compatible:  true,
			unmaskable:  true,
			wontMask:    true,
		},
		{
			peer: &Application{
				Major: 1,
				Minor: 3,
				Patch: 5,
			},
			time:        time.Unix(8500, 0),
			connectable: true,
			compatible:  true,
			unmaskable:  true,
			wontMask:    true,
		},
		{
			peer: &Application{
				Major: 0,
				Minor: 1,
				Patch: 0,
			},
			time:        minCompatableTime,
			connectable: false,
			compatible:  false,
			unmaskable:  false,
			wontMask:    false,
		},
		{
			peer: &Application{
				Major: 1,
				Minor: 3,
				Patch: 5,
			},
			time:        minCompatableTime,
			connectable: true,
			compatible:  false,
			unmaskable:  false,
			wontMask:    false,
		},
		{
			peer: &Application{
				Major: 1,
				Minor: 2,
				Patch: 5,
			},
			time:        time.Unix(8500, 0),
			connectable: true,
			compatible:  false,
			unmaskable:  false,
			wontMask:    false,
		},
		{
			peer: &Application{
				Major: 1,
				Minor: 1,
				Patch: 5,
			},
			time:        time.Unix(7500, 0),
			connectable: true,
			compatible:  false,
			unmaskable:  false,
			wontMask:    false,
		},
		{
			peer: &Application{
				Major: 1,
				Minor: 1,
				Patch: 5,
			},
			time:        time.Unix(6500, 0),
			connectable: true,
			compatible:  false,
			unmaskable:  false,
			wontMask:    false,
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
			if err := compatibility.Unmaskable(peer); test.unmaskable && err != nil {
				t.Fatalf("incorrectly marked %s as un-maskable with %s", peer, err)
			} else if !test.unmaskable && err == nil {
				t.Fatalf("incorrectly marked %s as maskable", peer)
			}
			if err := compatibility.WontMask(peer); test.wontMask && err != nil {
				t.Fatalf("incorrectly marked %s as unmaskable with %s", peer, err)
			} else if !test.wontMask && err == nil {
				t.Fatalf("incorrectly marked %s as maskable", peer)
			}
		})
	}
}
