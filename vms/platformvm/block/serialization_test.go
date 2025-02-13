// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func TestBanffBlockSerialization(t *testing.T) {
	type test struct {
		block BanffBlock
		bytes []byte
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
			bytes: []byte{
				// Codec version
				0x00, 0x00,
				// Type ID
				0x00, 0x00, 0x00, 0x1d,
				// Rest
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x13,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00,
			},
		},
		{
			block: &BanffCommitBlock{
				ApricotCommitBlock: ApricotCommitBlock{},
			},
			bytes: []byte{
				// Codec version
				0x00, 0x00,
				// Type ID
				0x00, 0x00, 0x00, 0x1f,
				// Rest
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
		},
		{
			block: &BanffAbortBlock{
				ApricotAbortBlock: ApricotAbortBlock{},
			},
			bytes: []byte{
				// Codec version
				0x00, 0x00,
				// Type ID
				0x00, 0x00, 0x00, 0x1e,
				// Rest
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
		},
		{
			block: &BanffStandardBlock{
				ApricotStandardBlock: ApricotStandardBlock{
					Transactions: []*txs.Tx{},
				},
			},
			bytes: []byte{
				// Codec version
				0x00, 0x00,
				// Type ID
				0x00, 0x00, 0x00, 0x20,
				// Rest
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00,
			},
		},
	}

	for _, test := range tests {
		testName := fmt.Sprintf("%T", test.block)
		block := test.block
		t.Run(testName, func(t *testing.T) {
			require := require.New(t)

			got, err := Codec.Marshal(CodecVersion, &block)
			require.NoError(err)
			require.Equal(test.bytes, got)
		})
	}
}

func TestBanffProposalBlockJSON(t *testing.T) {
	require := require.New(t)

	simpleBanffProposalBlock := &BanffProposalBlock{
		Time: 123456,
		ApricotProposalBlock: ApricotProposalBlock{
			CommonBlock: CommonBlock{
				PrntID:  ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'},
				Hght:    1337,
				BlockID: ids.ID{'b', 'l', 'o', 'c', 'k', 'I', 'D'},
			},
			Tx: &txs.Tx{
				Unsigned: &txs.AdvanceTimeTx{
					Time: 123457,
				},
			},
		},
	}

	simpleBanffProposalBlockBytes, err := json.MarshalIndent(simpleBanffProposalBlock, "", "\t")
	require.NoError(err)

	require.JSONEq(`{
	"time": 123456,
	"txs": null,
	"parentID": "rVcYrvnGXdoJBeYQRm5ZNaCGHeVyqcHHJu8Yd89kJcef6V5Eg",
	"height": 1337,
	"id": "kM6h4d2UKYEDzQXm7KNqyeBJLjhb42J24m4L4WACB5didf3pk",
	"tx": {
		"unsignedTx": {
			"time": 123457
		},
		"credentials": null,
		"id": "11111111111111111111111111111111LpoYY"
	}
}`, string(simpleBanffProposalBlockBytes))

	complexBanffProposalBlock := simpleBanffProposalBlock
	complexBanffProposalBlock.Transactions = []*txs.Tx{
		{
			Unsigned: &txs.BaseTx{
				BaseTx: avax.BaseTx{
					NetworkID:    constants.MainnetID,
					BlockchainID: constants.PlatformChainID,
					Outs:         []*avax.TransferableOutput{},
					Ins:          []*avax.TransferableInput{},
					Memo:         []byte("KilroyWasHere"),
				},
			},
		},
		{
			Unsigned: &txs.BaseTx{
				BaseTx: avax.BaseTx{
					NetworkID:    constants.MainnetID,
					BlockchainID: constants.PlatformChainID,
					Outs:         []*avax.TransferableOutput{},
					Ins:          []*avax.TransferableInput{},
					Memo:         []byte("KilroyWasHere2"),
				},
			},
		},
	}

	complexBanffProposalBlockBytes, err := json.MarshalIndent(complexBanffProposalBlock, "", "\t")
	require.NoError(err)

	require.JSONEq(`{
	"time": 123456,
	"txs": [
		{
			"unsignedTx": {
				"networkID": 1,
				"blockchainID": "11111111111111111111111111111111LpoYY",
				"outputs": [],
				"inputs": [],
				"memo": "0x4b696c726f7957617348657265"
			},
			"credentials": null,
			"id": "11111111111111111111111111111111LpoYY"
		},
		{
			"unsignedTx": {
				"networkID": 1,
				"blockchainID": "11111111111111111111111111111111LpoYY",
				"outputs": [],
				"inputs": [],
				"memo": "0x4b696c726f795761734865726532"
			},
			"credentials": null,
			"id": "11111111111111111111111111111111LpoYY"
		}
	],
	"parentID": "rVcYrvnGXdoJBeYQRm5ZNaCGHeVyqcHHJu8Yd89kJcef6V5Eg",
	"height": 1337,
	"id": "kM6h4d2UKYEDzQXm7KNqyeBJLjhb42J24m4L4WACB5didf3pk",
	"tx": {
		"unsignedTx": {
			"time": 123457
		},
		"credentials": null,
		"id": "11111111111111111111111111111111LpoYY"
	}
}`, string(complexBanffProposalBlockBytes))
}
