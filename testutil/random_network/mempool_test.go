// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package random_network

import (
	"context"
	"testing"

	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/simplex"
	"github.com/ava-labs/simplex/testutil"
	"github.com/stretchr/testify/require"
)

var emptyBlacklist = common.NewBlacklist(4, uint64(4), simplex.DefaultMaxBlacklistedRounds)

func TestMempoolVerifiesTx(t *testing.T) {
	logger := testutil.MakeLogger(t, 1)
	logger.Silence() // so we dont log errors/warns

	ctx := context.Background()
	require := require.New(t)
	round0MD := NewProtocolMetadata(0, 0, common.Digest{})
	config := DefaultFuzzConfig()

	tests := []struct {
		name      string
		expectErr error
		setup     func() (*Mempool, *Block, error)
	}{
		{
			name:      "ValidTx",
			expectErr: nil,
			setup: func() (*Mempool, *Block, error) {
				mempool := NewMempool(logger, config)
				tx := CreateNewTX()
				mempool.AddPendingTXs(tx)

				block := NewBlock(round0MD, emptyBlacklist, mempool, []*TX{tx})

				return mempool, block, nil
			},
		},
		{
			name:      "Duplicate Tx In Block",
			expectErr: errDuplicateTxInBlock,
			setup: func() (*Mempool, *Block, error) {
				mempool := NewMempool(logger, config)
				tx := CreateNewTX()
				mempool.AddPendingTXs(tx)

				block := NewBlock(round0MD, emptyBlacklist, mempool, []*TX{tx, tx})
				return mempool, block, nil
			},
		},
		{
			name:      "Already Accepted",
			expectErr: errDoubleBlockVerification,
			setup: func() (*Mempool, *Block, error) {
				mempool := NewMempool(logger, config)
				tx := CreateNewTX()
				mempool.AddPendingTXs(tx)

				block := NewBlock(round0MD, emptyBlacklist, mempool, []*TX{tx})
				mempool.AcceptBlock(block)

				mempool.AddPendingTXs(tx)
				return mempool, block, nil
			},
		},
		{
			name:      "Already In Chain",
			expectErr: errAlreadyInChain,
			setup: func() (*Mempool, *Block, error) {
				mempool := NewMempool(logger, config)
				tx := CreateNewTX()
				mempool.AddPendingTXs(tx)

				parentBlock := NewBlock(round0MD, emptyBlacklist, mempool, []*TX{tx})
				if err := mempool.VerifyBlock(ctx, parentBlock); err != nil {
					return nil, nil, err
				}

				mempool.AddPendingTXs(tx)
				md := NewProtocolMetadata(1, 1, parentBlock.digest)
				block := NewBlock(md, emptyBlacklist, mempool, []*TX{tx})
				return mempool, block, nil
			},
		},
		{
			name:      "Tx Verification Fails",
			expectErr: errTxVerification,
			setup: func() (*Mempool, *Block, error) {
				mempool := NewMempool(logger, config)
				tx := CreateNewTX()
				tx.SetShouldFailVerification()
				mempool.AddPendingTXs(tx)

				block := NewBlock(round0MD, emptyBlacklist, mempool, []*TX{tx})

				return mempool, block, nil
			},
		},
		{
			name:      "Correctly verifies transaction not in chain",
			expectErr: nil,
			setup: func() (*Mempool, *Block, error) {
				mempool := NewMempool(logger, config)
				tx1 := CreateNewTX()
				mempool.AddPendingTXs(tx1)

				blockWithSameTxButNotParent := NewBlock(round0MD, emptyBlacklist, mempool, []*TX{tx1})
				err := mempool.VerifyBlock(ctx, blockWithSameTxButNotParent)

				mempool.AddPendingTXs(tx1)
				block := NewBlock(NewProtocolMetadata(1, 1, common.Digest{}), emptyBlacklist, mempool, []*TX{tx1})
				return mempool, block, err
			},
		},
		{
			name:      "Double Block Verification",
			expectErr: errDoubleBlockVerification,
			setup: func() (*Mempool, *Block, error) {
				mempool := NewMempool(logger, config)
				tx := CreateNewTX()
				mempool.AddPendingTXs(tx)

				block := NewBlock(round0MD, emptyBlacklist, mempool, []*TX{tx})
				err := mempool.VerifyBlock(ctx, block)
				if err != nil {
					return nil, nil, err
				}

				mempool.AddPendingTXs(tx)
				return mempool, block, nil
			},
		},
		{
			name:      "Parent Previously Verified But Was Pruned",
			expectErr: errParentNotFound,
			setup: func() (*Mempool, *Block, error) {
				mempool := NewMempool(logger, config)
				tx1 := CreateNewTX()
				tx2 := CreateNewTX()
				childTx := CreateNewTX()
				mempool.AddPendingTXs(tx1)
				mempool.AddPendingTXs(tx2)

				// create & verify two siblings
				brother := NewBlock(round0MD, emptyBlacklist, mempool, []*TX{tx1})
				if err := mempool.VerifyBlock(ctx, brother); err != nil {
					return nil, nil, err
				}

				sister := NewBlock(round0MD, emptyBlacklist, mempool, []*TX{tx2})
				if err := mempool.VerifyBlock(ctx, sister); err != nil {
					return nil, nil, err
				}

				// accept the sister, so the brother should be pruned
				mempool.AcceptBlock(sister)

				childBlock := NewBlock(NewProtocolMetadata(1, 1, brother.digest), emptyBlacklist, mempool, []*TX{childTx})
				return mempool, childBlock, nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mempool, block, err := tt.setup()
			require.NoError(err)
			err = mempool.VerifyBlock(ctx, block)
			require.ErrorIs(err, tt.expectErr)
		})
	}
}
