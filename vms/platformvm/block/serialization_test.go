// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"fmt"
	"testing"

	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/stretchr/testify/require"
)

func TestBanffBlockCodecTypeIDs(t *testing.T) {
	type test struct {
		block  BanffBlock
		typeID []byte
	}

	tests := []test{
		{
			block: &BanffProposalBlock{
				ApricotProposalBlock: ApricotProposalBlock{
					Tx: &txs.Tx{
						Unsigned: &txs.AdvanceTimeTx{},
					},
				},
			},
			typeID: []byte{0x00, 0x00, 0x00, 0x1d},
		},
		{
			block: &BanffCommitBlock{
				ApricotCommitBlock: ApricotCommitBlock{},
			},
			typeID: []byte{0x00, 0x00, 0x00, 0x1f},
		},
		{
			block: &BanffAbortBlock{
				ApricotAbortBlock: ApricotAbortBlock{},
			},
			typeID: []byte{0x00, 0x00, 0x00, 0x1e},
		},
		{
			block: &BanffStandardBlock{
				ApricotStandardBlock: ApricotStandardBlock{
					Transactions: []*txs.Tx{},
				},
			},
			typeID: []byte{0x00, 0x00, 0x00, 0x20},
		},
	}

	for _, test := range tests {
		testName := fmt.Sprintf("typeID_%T", test.block)
		t.Run(testName, func(t *testing.T) {
			require := require.New(t)

			bytes, err := Codec.Marshal(Version, &test.block)
			require.NoError(err)

			require.Equal(test.typeID, bytes[2:6])
		})
	}
}
