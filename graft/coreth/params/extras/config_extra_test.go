// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
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
			fork:     utils.PointerTo[uint64](0),
			block:    0,
			isForked: true,
		},
		"pre fork timestamp": {
			fork:     utils.PointerTo[uint64](100),
			block:    50,
			isForked: false,
		},
		"at fork timestamp": {
			fork:     utils.PointerTo[uint64](100),
			block:    100,
			isForked: true,
		},
		"post fork timestamp": {
			fork:     utils.PointerTo[uint64](100),
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
