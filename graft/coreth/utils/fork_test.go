// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsForked(t *testing.T) {
	type test struct {
		fork, block *big.Int
		isForked    bool
	}

	for name, test := range map[string]test{
		"nil fork at 0": {
			fork:     nil,
			block:    big.NewInt(0),
			isForked: false,
		},
		"nil fork at non-zero": {
			fork:     nil,
			block:    big.NewInt(100),
			isForked: false,
		},
		"zero fork at genesis": {
			fork:     big.NewInt(0),
			block:    big.NewInt(0),
			isForked: true,
		},
		"pre fork timestamp": {
			fork:     big.NewInt(100),
			block:    big.NewInt(50),
			isForked: false,
		},
		"at fork timestamp": {
			fork:     big.NewInt(100),
			block:    big.NewInt(100),
			isForked: true,
		},
		"post fork timestamp": {
			fork:     big.NewInt(100),
			block:    big.NewInt(150),
			isForked: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			res := IsForked(test.fork, test.block)
			assert.Equal(t, test.isForked, res)
		})
	}
}

func TestIsForkTransition(t *testing.T) {
	type test struct {
		fork, parent, current *big.Int
		transitioned          bool
	}

	for name, test := range map[string]test{
		"not active at genesis": {
			fork:         nil,
			parent:       nil,
			current:      big.NewInt(0),
			transitioned: false,
		},
		"activate at genesis": {
			fork:         big.NewInt(0),
			parent:       nil,
			current:      big.NewInt(0),
			transitioned: true,
		},
		"nil fork arbitrary transition": {
			fork:         nil,
			parent:       big.NewInt(100),
			current:      big.NewInt(101),
			transitioned: false,
		},
		"nil fork transition same timestamp": {
			fork:         nil,
			parent:       big.NewInt(100),
			current:      big.NewInt(100),
			transitioned: false,
		},
		"exact match on current timestamp": {
			fork:         big.NewInt(100),
			parent:       big.NewInt(99),
			current:      big.NewInt(100),
			transitioned: true,
		},
		"current same as parent does not transition twice": {
			fork:         big.NewInt(100),
			parent:       big.NewInt(101),
			current:      big.NewInt(101),
			transitioned: false,
		},
		"current, parent, and fork same should not transition twice": {
			fork:         big.NewInt(100),
			parent:       big.NewInt(100),
			current:      big.NewInt(100),
			transitioned: false,
		},
		"current transitions after fork": {
			fork:         big.NewInt(100),
			parent:       big.NewInt(99),
			current:      big.NewInt(101),
			transitioned: true,
		},
		"current and parent come after fork": {
			fork:         big.NewInt(100),
			parent:       big.NewInt(101),
			current:      big.NewInt(102),
			transitioned: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			res := IsForkTransition(test.fork, test.parent, test.current)
			assert.Equal(t, test.transitioned, res)
		})
	}
}
