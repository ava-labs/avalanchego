// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chainindex

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

type testBlock struct {
	height uint64
}

func (t *testBlock) GetID() ids.ID     { return hashing.ComputeHash256Array(t.GetBytes()) }
func (t *testBlock) GetHeight() uint64 { return t.height }
func (t *testBlock) GetBytes() []byte  { return binary.BigEndian.AppendUint64(nil, t.height) }

type parser struct{}

func (*parser) ParseBlock(_ context.Context, b []byte) (*testBlock, error) {
	if len(b) != wrappers.LongLen {
		return nil, fmt.Errorf("unexpected block length: %d", len(b))
	}
	height := binary.BigEndian.Uint64(b)
	return &testBlock{height: height}, nil
}

func newTestChainIndex(config Config, db database.Database) (*ChainIndex[*testBlock], error) {
	return New(logging.NoLog{}, prometheus.NewRegistry(), config, &parser{}, db)
}

func confirmBlockIndexed(r *require.Assertions, ctx context.Context, chainIndex *ChainIndex[*testBlock], expectedBlk *testBlock, expectedErr error) {
	blkByHeight, err := chainIndex.GetBlockByHeight(ctx, expectedBlk.height)
	r.ErrorIs(err, expectedErr)

	blkIDAtHeight, err := chainIndex.GetBlockIDAtHeight(ctx, expectedBlk.height)
	r.ErrorIs(err, expectedErr)

	blockIDHeight, err := chainIndex.GetBlockIDHeight(ctx, expectedBlk.GetID())
	r.ErrorIs(err, expectedErr)

	blk, err := chainIndex.GetBlock(ctx, expectedBlk.GetID())
	r.ErrorIs(err, expectedErr)

	if expectedErr != nil {
		return
	}

	r.Equal(blkByHeight.GetID(), expectedBlk.GetID())
	r.Equal(blkIDAtHeight, expectedBlk.GetID())
	r.Equal(blockIDHeight, expectedBlk.GetHeight())
	r.Equal(blk.GetID(), expectedBlk.GetID())
}

func confirmLastAcceptedHeight(r *require.Assertions, ctx context.Context, chainIndex *ChainIndex[*testBlock], expectedHeight uint64) {
	lastAcceptedHeight, err := chainIndex.GetLastAcceptedHeight(ctx)
	r.NoError(err)
	r.Equal(expectedHeight, lastAcceptedHeight)
}

func TestChainIndex(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	chainIndex, err := newTestChainIndex(NewDefaultConfig(), memdb.New())
	r.NoError(err)

	genesisBlk := &testBlock{height: 0}
	confirmBlockIndexed(r, ctx, chainIndex, genesisBlk, database.ErrNotFound)
	_, err = chainIndex.GetLastAcceptedHeight(ctx)
	r.ErrorIs(err, database.ErrNotFound)

	r.NoError(chainIndex.UpdateLastAccepted(ctx, genesisBlk))
	confirmBlockIndexed(r, ctx, chainIndex, genesisBlk, nil)
	confirmLastAcceptedHeight(r, ctx, chainIndex, genesisBlk.GetHeight())

	blk1 := &testBlock{height: 1}
	r.NoError(chainIndex.UpdateLastAccepted(ctx, blk1))
	confirmBlockIndexed(r, ctx, chainIndex, blk1, nil)
	confirmLastAcceptedHeight(r, ctx, chainIndex, blk1.GetHeight())
}

func TestChainIndexInvalidCompactionFrequency(t *testing.T) {
	_, err := newTestChainIndex(Config{BlockCompactionFrequency: 0}, memdb.New())
	require.ErrorIs(t, err, errBlockCompactionFrequencyZero)
}

func TestChainIndexExpiry(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	chainIndex, err := newTestChainIndex(Config{AcceptedBlockWindow: 1, BlockCompactionFrequency: 64}, memdb.New())
	r.NoError(err)

	genesisBlk := &testBlock{height: 0}
	r.NoError(chainIndex.UpdateLastAccepted(ctx, genesisBlk))
	confirmBlockIndexed(r, ctx, chainIndex, genesisBlk, nil)
	confirmLastAcceptedHeight(r, ctx, chainIndex, genesisBlk.GetHeight())

	blk1 := &testBlock{height: 1}
	r.NoError(chainIndex.UpdateLastAccepted(ctx, blk1))
	confirmBlockIndexed(r, ctx, chainIndex, genesisBlk, nil) // Confirm genesis is not un-indexed
	confirmBlockIndexed(r, ctx, chainIndex, blk1, nil)
	confirmLastAcceptedHeight(r, ctx, chainIndex, blk1.GetHeight())

	blk2 := &testBlock{height: 2}
	r.NoError(chainIndex.UpdateLastAccepted(ctx, blk2))
	confirmBlockIndexed(r, ctx, chainIndex, genesisBlk, nil) // Confirm genesis is not un-indexed
	confirmBlockIndexed(r, ctx, chainIndex, blk2, nil)
	confirmBlockIndexed(r, ctx, chainIndex, blk1, database.ErrNotFound)
	confirmLastAcceptedHeight(r, ctx, chainIndex, blk2.GetHeight())

	blk3 := &testBlock{height: 3}
	r.NoError(chainIndex.UpdateLastAccepted(ctx, blk3))
	confirmBlockIndexed(r, ctx, chainIndex, genesisBlk, nil) // Confirm genesis is not un-indexed
	confirmBlockIndexed(r, ctx, chainIndex, blk3, nil)
	confirmBlockIndexed(r, ctx, chainIndex, blk2, database.ErrNotFound)
	confirmLastAcceptedHeight(r, ctx, chainIndex, blk3.GetHeight())
}
