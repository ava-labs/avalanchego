// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/utils"
)

func TestIsTimestampForked(t *testing.T) {
	type test struct {
		fork     *uint64
		block    uint64
		isForked bool
	}

	for name, test := range map[string]test{
		"nil fork at 0": {
			fork:     nil,
			block:    0,
			isForked: false,
		},
		"nil fork at non-zero": {
			fork:     nil,
			block:    100,
			isForked: false,
		},
		"zero fork at genesis": {
			fork:     utils.NewUint64(0),
			block:    0,
			isForked: true,
		},
		"pre fork timestamp": {
			fork:     utils.NewUint64(100),
			block:    50,
			isForked: false,
		},
		"at fork timestamp": {
			fork:     utils.NewUint64(100),
			block:    100,
			isForked: true,
		},
		"post fork timestamp": {
			fork:     utils.NewUint64(100),
			block:    150,
			isForked: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			res := isTimestampForked(test.fork, test.block)
			require.Equal(t, test.isForked, res)
		})
	}
}

func TestIsForkTransition(t *testing.T) {
	type test struct {
		fork, parent *uint64
		current      uint64
		transitioned bool
	}

	for name, test := range map[string]test{
		"not active at genesis": {
			fork:         nil,
			parent:       nil,
			current:      0,
			transitioned: false,
		},
		"activate at genesis": {
			fork:         utils.NewUint64(0),
			parent:       nil,
			current:      0,
			transitioned: true,
		},
		"nil fork arbitrary transition": {
			fork:         nil,
			parent:       utils.NewUint64(100),
			current:      101,
			transitioned: false,
		},
		"nil fork transition same timestamp": {
			fork:         nil,
			parent:       utils.NewUint64(100),
			current:      100,
			transitioned: false,
		},
		"exact match on current timestamp": {
			fork:         utils.NewUint64(100),
			parent:       utils.NewUint64(99),
			current:      100,
			transitioned: true,
		},
		"current same as parent does not transition twice": {
			fork:         utils.NewUint64(100),
			parent:       utils.NewUint64(101),
			current:      101,
			transitioned: false,
		},
		"current, parent, and fork same should not transition twice": {
			fork:         utils.NewUint64(100),
			parent:       utils.NewUint64(100),
			current:      100,
			transitioned: false,
		},
		"current transitions after fork": {
			fork:         utils.NewUint64(100),
			parent:       utils.NewUint64(99),
			current:      101,
			transitioned: true,
		},
		"current and parent come after fork": {
			fork:         utils.NewUint64(100),
			parent:       utils.NewUint64(101),
			current:      102,
			transitioned: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			res := IsForkTransition(test.fork, test.parent, test.current)
			require.Equal(t, test.transitioned, res)
		})
	}
}
