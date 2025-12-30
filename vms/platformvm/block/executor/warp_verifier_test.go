// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func TestVerifyWarpMessages(t *testing.T) {
	var (
		validTx = &txs.Tx{
			Unsigned: &txs.BaseTx{},
		}
		invalidTx = &txs.Tx{
			Unsigned: &txs.RegisterL1ValidatorTx{},
		}
	)

	tests := []struct {
		name        string
		block       block.Block
		expectedErr error
	}{
		{
			name:  "BanffAbortBlock",
			block: &block.BanffAbortBlock{},
		},
		{
			name:  "BanffCommitBlock",
			block: &block.BanffCommitBlock{},
		},
		{
			name: "BanffProposalBlock with invalid standard tx",
			block: &block.BanffProposalBlock{
				Transactions: []*txs.Tx{
					invalidTx,
				},
				ApricotProposalBlock: block.ApricotProposalBlock{
					Tx: validTx,
				},
			},
			expectedErr: codec.ErrCantUnpackVersion,
		},
		{
			name: "BanffProposalBlock with invalid proposal tx",
			block: &block.BanffProposalBlock{
				ApricotProposalBlock: block.ApricotProposalBlock{
					Tx: invalidTx,
				},
			},
			expectedErr: codec.ErrCantUnpackVersion,
		},
		{
			name: "BanffProposalBlock with valid proposal tx",
			block: &block.BanffProposalBlock{
				ApricotProposalBlock: block.ApricotProposalBlock{
					Tx: validTx,
				},
			},
		},
		{
			name: "BanffStandardBlock with invalid tx",
			block: &block.BanffStandardBlock{
				ApricotStandardBlock: block.ApricotStandardBlock{
					Transactions: []*txs.Tx{
						invalidTx,
					},
				},
			},
			expectedErr: codec.ErrCantUnpackVersion,
		},
		{
			name: "BanffStandardBlock with valid tx",
			block: &block.BanffStandardBlock{
				ApricotStandardBlock: block.ApricotStandardBlock{
					Transactions: []*txs.Tx{
						validTx,
					},
				},
			},
		},
		{
			name:  "ApricotAbortBlock",
			block: &block.ApricotAbortBlock{},
		},
		{
			name:  "ApricotCommitBlock",
			block: &block.ApricotCommitBlock{},
		},
		{
			name: "ApricotProposalBlock with invalid proposal tx",
			block: &block.ApricotProposalBlock{
				Tx: invalidTx,
			},
			expectedErr: codec.ErrCantUnpackVersion,
		},
		{
			name: "ApricotProposalBlock with valid proposal tx",
			block: &block.ApricotProposalBlock{
				Tx: validTx,
			},
		},
		{
			name: "ApricotStandardBlock with invalid tx",
			block: &block.ApricotStandardBlock{
				Transactions: []*txs.Tx{
					invalidTx,
				},
			},
			expectedErr: codec.ErrCantUnpackVersion,
		},
		{
			name: "ApricotStandardBlock with valid tx",
			block: &block.ApricotStandardBlock{
				Transactions: []*txs.Tx{
					validTx,
				},
			},
		},
		{
			name: "ApricotAtomicBlock with invalid proposal tx",
			block: &block.ApricotAtomicBlock{
				Tx: invalidTx,
			},
			expectedErr: codec.ErrCantUnpackVersion,
		},
		{
			name: "ApricotAtomicBlock with valid proposal tx",
			block: &block.ApricotAtomicBlock{
				Tx: validTx,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := VerifyWarpMessages(
				t.Context(),
				constants.UnitTestID,
				nil,
				0,
				test.block,
			)
			require.Equal(t, test.expectedErr, err)
		})
	}
}
