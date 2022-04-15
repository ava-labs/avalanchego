// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package verify

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/snow"
)

var errMissing = errors.New("missing")

type snLookup struct {
	chainsToSubnet map[ids.ID]ids.ID
}

func (sn *snLookup) SubnetID(chainID ids.ID) (ids.ID, error) {
	subnetID, ok := sn.chainsToSubnet[chainID]
	if !ok {
		return ids.ID{}, errMissing
	}
	return subnetID, nil
}

func TestSameSubnet(t *testing.T) {
	subnet0 := ids.GenerateTestID()
	subnet1 := ids.GenerateTestID()
	chain0 := ids.GenerateTestID()
	chain1 := ids.GenerateTestID()

	tests := []struct {
		name    string
		ctx     *snow.Context
		chainID ids.ID
		result  error
	}{
		{
			name: "same chain",
			ctx: &snow.Context{
				SubnetID: subnet0,
				ChainID:  chain0,
				SNLookup: &snLookup{},
			},
			chainID: chain0,
			result:  errSameChainID,
		},
		{
			name: "unknown chain",
			ctx: &snow.Context{
				SubnetID: subnet0,
				ChainID:  chain0,
				SNLookup: &snLookup{},
			},
			chainID: chain1,
			result:  errMissing,
		},
		{
			name: "wrong subnet",
			ctx: &snow.Context{
				SubnetID: subnet0,
				ChainID:  chain0,
				SNLookup: &snLookup{
					chainsToSubnet: map[ids.ID]ids.ID{
						chain1: subnet1,
					},
				},
			},
			chainID: chain1,
			result:  errMismatchedSubnetIDs,
		},
		{
			name: "same subnet",
			ctx: &snow.Context{
				SubnetID: subnet0,
				ChainID:  chain0,
				SNLookup: &snLookup{
					chainsToSubnet: map[ids.ID]ids.ID{
						chain1: subnet0,
					},
				},
			},
			chainID: chain1,
			result:  nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := SameSubnet(test.ctx, test.chainID)
			assert.ErrorIs(t, result, test.result)
		})
	}
}
