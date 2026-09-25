// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"crypto"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

func testBlockState(require *require.Assertions, bs BlockState) {
	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	innerBlockBytes := []byte{3}
	chainID := ids.ID{4}

	tlsCert, err := staking.NewTLSCert()
	require.NoError(err)

	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(err)
	key := tlsCert.PrivateKey.(crypto.Signer)

	b, err := block.Build(
		parentID,
		timestamp,
		pChainHeight,
		block.Epoch{},
		cert,
		innerBlockBytes,
		chainID,
		key,
	)
	require.NoError(err)

	_, err = bs.GetBlock(b.ID())
	require.Equal(database.ErrNotFound, err)

	_, err = bs.GetBlock(b.ID())
	require.Equal(database.ErrNotFound, err)

	// GetBlockBytes observes the cached miss from GetBlock.
	_, err = bs.GetBlockBytes(b.ID())
	require.Equal(database.ErrNotFound, err)

	require.NoError(bs.PutBlock(b))

	fetchedBlock, err := bs.GetBlock(b.ID())
	require.NoError(err)
	require.Equal(b.Bytes(), fetchedBlock.Bytes())

	fetchedBlock, err = bs.GetBlock(b.ID())
	require.NoError(err)
	require.Equal(b.Bytes(), fetchedBlock.Bytes())

	// GetBlockBytes returns the cached block's bytes.
	fetchedBytes, err := bs.GetBlockBytes(b.ID())
	require.NoError(err)
	require.Equal(b.Bytes(), fetchedBytes)

	_, err = bs.GetBlockBytes(ids.GenerateTestID())
	require.Equal(database.ErrNotFound, err)
}

func newTestBlock(require *require.Assertions) block.Block {
	tlsCert, err := staking.NewTLSCert()
	require.NoError(err)

	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(err)
	key := tlsCert.PrivateKey.(crypto.Signer)

	b, err := block.Build(
		ids.ID{1},
		time.Unix(123, 0),
		2,
		block.Epoch{},
		cert,
		[]byte{3},
		ids.ID{4},
		key,
	)
	require.NoError(err)
	return b
}

// testBlockStateUncachedBytes verifies that GetBlockBytes reads from the
// database when the block isn't cached, and that doing so doesn't cache a
// wrapper without a parsed block, which would break GetBlock.
func testBlockStateUncachedBytes(require *require.Assertions, db database.Database) {
	b := newTestBlock(require)

	require.NoError(NewBlockState(db).PutBlock(b))

	// A new block state has an empty cache, so the block must be read from
	// the database.
	bs := NewBlockState(db)
	fetchedBytes, err := bs.GetBlockBytes(b.ID())
	require.NoError(err)
	require.Equal(b.Bytes(), fetchedBytes)

	fetchedBlock, err := bs.GetBlock(b.ID())
	require.NoError(err)
	require.Equal(b.Bytes(), fetchedBlock.Bytes())
}

func TestBlockState(t *testing.T) {
	a := require.New(t)

	db := memdb.New()
	bs := NewBlockState(db)

	testBlockState(a, bs)
	testBlockStateUncachedBytes(a, memdb.New())
}

func TestMeteredBlockState(t *testing.T) {
	a := require.New(t)

	db := memdb.New()
	bs, err := NewMeteredBlockState(db, "", prometheus.NewRegistry())
	a.NoError(err)

	testBlockState(a, bs)
	testBlockStateUncachedBytes(a, memdb.New())
}
