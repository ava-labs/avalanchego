// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/cchaintest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"

	cparams "github.com/ava-labs/avalanchego/graft/coreth/params"
)

func TestConsensusGettersAfterRestart(t *testing.T) {
	key := txtest.NewKey(t)
	alloc := withMaxAllocFor(key.EthAddress())
	db := memdb.New()

	ctx, node := newSUT(t, alloc, withDB(db))
	w := newWallet(key, node.ctx, node.Client)

	genesis, err := node.GetBlock(ctx, node.lastAccepted(ctx, t))
	require.NoErrorf(t, err, "%T.GetBlock()", node.VM)
	const numBlocks = 3
	want := make([]*blocks.Block, 0, numBlocks+1)
	want = append(want, genesis)
	for range numBlocks {
		blk := node.issueAndExecute(ctx, t, w.newMinimalTx(t))
		want = append(want, blk)
	}
	require.NoErrorf(t, node.Shutdown(ctx), "%T.Shutdown()", node.VM)

	for _, state := range []snow.State{
		snow.StateSyncing,
		snow.Bootstrapping,
		snow.NormalOp,
	} {
		t.Run(state.String(), func(t *testing.T) {
			ctx, s := newSUT(t, alloc, withDB(db), withState(state))

			wantLastAccepted := want[len(want)-1].ID()
			gotLastAccepted, err := s.LastAccepted(ctx)
			require.NoErrorf(t, err, "%T.LastAccepted()", s)
			assert.Equalf(t, wantLastAccepted, gotLastAccepted, "%T.LastAccepted()", s)

			for _, b := range want {
				gotID, err := s.GetBlockIDAtHeight(ctx, b.Height())
				require.NoErrorf(t, err, "%T.GetBlockIDAtHeight(%d)", s, b.Height())
				assert.Equalf(t, b.ID(), gotID, "%T.GetBlockIDAtHeight(%d)", s, b.Height())

				gotBlock, err := s.GetBlock(ctx, b.ID())
				require.NoErrorf(t, err, "%T.GetBlock(%s)", s, b.ID())
				assert.Equalf(t, b.ID(), gotBlock.ID(), "%T.GetBlock(%s).ID()", s, b.ID())
				assert.Equalf(t, b.Height(), gotBlock.Height(), "%T.GetBlock(%s).Height()", s, b.ID())
			}
		})
	}
}

func TestConsensusGettersNoState(t *testing.T) {
	ctx, sut := newSUT(t, withState(snow.StateSyncing))
	hash, err := sut.LastAccepted(ctx)
	require.NoErrorf(t, err, "%T.LastAccepted()", sut)
	require.NotZerof(t, hash, "%T.LastAccepted()", sut)

	gotID, err := sut.GetBlockIDAtHeight(ctx, 0)
	require.NoErrorf(t, err, "%T.GetBlockIDAtHeight(%d)", sut, 0)
	assert.Equalf(t, hash, gotID, "%T.GetBlockIDAtHeight(%d)", sut, 0)

	gotBlock, err := sut.GetBlock(ctx, hash)
	require.NoErrorf(t, err, "%T.GetBlock(%s)", sut, hash)
	assert.Equalf(t, hash, gotBlock.ID(), "%T.GetBlock(%s).ID()", sut, hash)
	assert.Equalf(t, uint64(0), gotBlock.Height(), "%T.GetBlock(%s).Height()", sut, hash)
}

// TestWaitForEventInitializing tests that WaitForEvent blocks if the VM isn't
// bootstrapped or state syncing.
func TestWaitForEventInitializing(t *testing.T) {
	ctx, sut := newSUT(t, withState(snow.Initializing))

	ctx, cancel := context.WithCancel(ctx)
	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		_, err := sut.WaitForEvent(egCtx)
		return err
	})
	cancel()
	require.ErrorIs(t, eg.Wait(), context.Canceled)
}

// TestParseBlock verifies that ParseBlock accepts well-formed blocks and
// rejects blocks with an unsupported (non-zero) version or whose extData does
// not match the ExtDataHash committed in the header, in every VM mode: both
// before bootstrapping (via the statesync SummaryHandler) and after (via the
// embedded SAE VM).
func TestParseBlock(t *testing.T) {
	ctx, sut := newSUT(t, withNetworkID(constants.FujiID))

	genesisID, err := sut.LastAccepted(ctx)
	require.NoError(t, err, "vm.LastAccepted()")
	genesisBlk, err := sut.GetBlock(ctx, genesisID)
	require.NoError(t, err, "vm.GetBlock(genesisID)")

	key := txtest.NewKey(t)
	w := newWallet(key, sut.ctx, nil)
	stx := w.newMinimalTx(t)

	ap1Time := *cparams.GetExtra(sut.chainConfig).ApricotPhase1BlockTimestamp

	const (
		// Heights with and without an entry in [extDataHashes] for Fuji.
		preAP1WithDataHeight    = 1
		preAP1WithoutDataHeight = 3
	)
	tests := []struct {
		name    string
		block   *types.Block
		wantErr error
	}{
		{
			name: "invalid_version",
			block: cchaintest.NewTestBlock(t,
				cchaintest.WithBlockVersion(1),
			),
			wantErr: errInvalidBlockVersion,
		},
		{
			name:  "genesis",
			block: genesisBlk.EthBlock(),
		},
		{
			name: "genesis_with_nonzero_header",
			block: cchaintest.NewTestBlock(t,
				cchaintest.WithNumber(0),
			),
			wantErr: errExtDataHashMismatch,
		},
		{
			name: "genesis_with_extdata",
			block: cchaintest.NewTestBlock(t,
				cchaintest.WithNumber(0),
				cchaintest.WithCrossChainTxs(stx),
			),
			wantErr: errExtDataUnexpectedHash,
		},
		{
			name: "pre_ap1_with_extdata",
			block: cchaintest.NewTestBlock(t,
				cchaintest.WithNumber(preAP1WithDataHeight),
				cchaintest.WithTimestamp(ap1Time-1),
				cchaintest.WithExtDataHash(common.Hash{}),
				// See Fuji block #1's canonical representation for the source
				// of the bytes.
				cchaintest.WithExtData(common.FromHex("0x000000000000000000057fc93d85c6d62c5b2ac0b519c87010ea5294012d1e407030d6acd0021cac10d5ab68eb1ee142a05cfe768c36e11f0b596db5a3c6c77aabe665dad9e638ca94f70000000106eb57070eed14d04c3e6fcfec2b670c7bbece079ad1ff97dd407e416796aea6000000013d9bdac0ed1d761330cf680efdeb1a42159eb387d6d2950c96f7d28f61bbe2aa00000005000000003b9aca00000000010000000000000001572f4d80f10f663b5049f789546f25f70bb62a7f000000003b9aca003d9bdac0ed1d761330cf680efdeb1a42159eb387d6d2950c96f7d28f61bbe2aa000000010000000900000001c1b8fcb9824bf9fde4d506768250a40fde0027a7eed23ad89ea49a87fce892df5b082103b08bbc5d20b3c107ad33dfc880fbbb96cfa0bf8752e5c93b979bad6200")),
			),
		},
		{
			name: "pre_ap1_missing_extdata",
			block: cchaintest.NewTestBlock(t,
				cchaintest.WithNumber(preAP1WithDataHeight),
				cchaintest.WithTimestamp(ap1Time-1),
				cchaintest.WithExtDataHash(common.Hash{}),
			),
			wantErr: errExtDataUnexpectedHash,
		},
		{
			name: "pre_ap1_without_extdata",
			block: cchaintest.NewTestBlock(t,
				cchaintest.WithNumber(preAP1WithoutDataHeight),
				cchaintest.WithTimestamp(ap1Time-1),
				cchaintest.WithExtDataHash(common.Hash{}),
			),
		},
		{
			name: "pre_ap1_unexpected_extdata",
			block: cchaintest.NewTestBlock(t,
				cchaintest.WithNumber(preAP1WithoutDataHeight),
				cchaintest.WithTimestamp(ap1Time-1),
				cchaintest.WithExtDataHash(common.Hash{}),
				cchaintest.WithCrossChainTxs(stx),
			),
			wantErr: errExtDataUnexpectedHash,
		},
		{
			name: "post_ap1_without_data",
			block: cchaintest.NewTestBlock(t,
				cchaintest.WithTimestamp(ap1Time),
			),
		},
		{
			name: "post_ap1_with_data",
			block: cchaintest.NewTestBlock(t,
				cchaintest.WithTimestamp(ap1Time),
				cchaintest.WithCrossChainTxs(stx),
			),
		},
		{
			name: "post_ap1_with_extdata_hash_mismatch",
			block: cchaintest.NewTestBlock(t,
				cchaintest.WithTimestamp(ap1Time),
				cchaintest.WithCrossChainTxs(stx),
				cchaintest.WithExtDataHash(common.Hash{1}),
			),
			wantErr: errExtDataHashMismatch,
		},
	}

	states := []snow.State{
		snow.Initializing,
		snow.StateSyncing,
		snow.Bootstrapping,
		snow.NormalOp,
	}

	for _, mode := range states {
		t.Run(mode.String(), func(t *testing.T) {
			sut.mode.Set(mode)
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					buf, err := rlp.EncodeToBytes(tt.block)
					require.NoError(t, err, "rlp.EncodeToBytes(block)")

					got, err := sut.ParseBlock(ctx, buf)
					require.ErrorIs(t, err, tt.wantErr, "vm.ParseBlock(buf)")
					if tt.wantErr != nil {
						return
					}

					require.Equal(t, tt.block.Hash(), got.EthBlock().Hash(), "vm.ParseBlock() block hash")
				})
			}
		})
	}
}
