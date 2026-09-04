// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
)

func TestVerifyWarpMessages(t *testing.T) {
	var (
		validTx = &platform.Tx{
			Unsigned: &platform.BaseTx{},
		}
		invalidTx = &platform.Tx{
			Unsigned: &platform.RegisterL1ValidatorTx{},
		}
	)

	tests := []struct {
		name        string
		block       platform.Block
		expectedErr error
	}{
		{
			name:  "BanffAbortBlock",
			block: &platform.BanffAbortBlock{},
		},
		{
			name:  "BanffCommitBlock",
			block: &platform.BanffCommitBlock{},
		},
		{
			name: "BanffProposalBlock with invalid standard tx",
			block: &platform.BanffProposalBlock{
				Transactions: []*platform.Tx{
					invalidTx,
				},
				ApricotProposalBlock: platform.ApricotProposalBlock{
					Tx: validTx,
				},
			},
			expectedErr: codec.ErrCantUnpackVersion,
		},
		{
			name: "BanffProposalBlock with invalid proposal tx",
			block: &platform.BanffProposalBlock{
				ApricotProposalBlock: platform.ApricotProposalBlock{
					Tx: invalidTx,
				},
			},
			expectedErr: codec.ErrCantUnpackVersion,
		},
		{
			name: "BanffProposalBlock with valid proposal tx",
			block: &platform.BanffProposalBlock{
				ApricotProposalBlock: platform.ApricotProposalBlock{
					Tx: validTx,
				},
			},
		},
		{
			name: "BanffStandardBlock with invalid tx",
			block: &platform.BanffStandardBlock{
				ApricotStandardBlock: platform.ApricotStandardBlock{
					Transactions: []*platform.Tx{
						invalidTx,
					},
				},
			},
			expectedErr: codec.ErrCantUnpackVersion,
		},
		{
			name: "BanffStandardBlock with valid tx",
			block: &platform.BanffStandardBlock{
				ApricotStandardBlock: platform.ApricotStandardBlock{
					Transactions: []*platform.Tx{
						validTx,
					},
				},
			},
		},
		{
			name:  "ApricotAbortBlock",
			block: &platform.ApricotAbortBlock{},
		},
		{
			name:  "ApricotCommitBlock",
			block: &platform.ApricotCommitBlock{},
		},
		{
			name: "ApricotProposalBlock with invalid proposal tx",
			block: &platform.ApricotProposalBlock{
				Tx: invalidTx,
			},
			expectedErr: codec.ErrCantUnpackVersion,
		},
		{
			name: "ApricotProposalBlock with valid proposal tx",
			block: &platform.ApricotProposalBlock{
				Tx: validTx,
			},
		},
		{
			name: "ApricotStandardBlock with invalid tx",
			block: &platform.ApricotStandardBlock{
				Transactions: []*platform.Tx{
					invalidTx,
				},
			},
			expectedErr: codec.ErrCantUnpackVersion,
		},
		{
			name: "ApricotStandardBlock with valid tx",
			block: &platform.ApricotStandardBlock{
				Transactions: []*platform.Tx{
					validTx,
				},
			},
		},
		{
			name: "ApricotAtomicBlock with invalid proposal tx",
			block: &platform.ApricotAtomicBlock{
				Tx: invalidTx,
			},
			expectedErr: codec.ErrCantUnpackVersion,
		},
		{
			name: "ApricotAtomicBlock with valid proposal tx",
			block: &platform.ApricotAtomicBlock{
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
