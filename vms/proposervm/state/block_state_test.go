// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"crypto"
	"encoding/binary"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/logging"
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

	require.NoError(bs.PutBlock(b, ids.Empty))

	fetchedBlock, err := bs.GetBlock(b.ID())
	require.NoError(err)
	require.Equal(b.Bytes(), fetchedBlock.Bytes())

	fetchedBlock, err = bs.GetBlock(b.ID())
	require.NoError(err)
	require.Equal(b.Bytes(), fetchedBlock.Bytes())
}

func TestBlockState(t *testing.T) {
	a := require.New(t)

	db := memdb.New()
	bs := NewBlockState(db, nil, nil)

	testBlockState(a, bs)
}

func TestMeteredBlockState(t *testing.T) {
	a := require.New(t)

	db := memdb.New()
	bs, err := NewMeteredBlockState(db, "", prometheus.NewRegistry(), nil, nil)
	a.NoError(err)

	testBlockState(a, bs)
}

// TestBlockStateDedup exercises the deduplicated storage path: blocks are stored
// without their inner bytes and reconstructed on read by looking the inner bytes
// up by inner block ID.
func TestBlockStateDedup(t *testing.T) {
	require := require.New(t)

	innerID := ids.ID{9}
	// Real inner EVM blocks are KB-sized; dedup only nets a win when the stripped
	// inner bytes exceed the 32-byte inner-ID + codec overhead we add in their place.
	innerBlockBytes := make([]byte, 1024)
	for i := range innerBlockBytes {
		innerBlockBytes[i] = byte(i)
	}

	// getInnerBytes simulates the inner VM returning the inner block's bytes.
	getInnerBytes := func(id ids.ID) ([]byte, error) {
		require.Equal(innerID, id)
		return innerBlockBytes, nil
	}

	db := memdb.New()
	bs, err := NewMeteredBlockState(db, "", prometheus.NewRegistry(), getInnerBytes, logging.NoLog{})
	require.NoError(err)

	tlsCert, err := staking.NewTLSCert()
	require.NoError(err)
	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(err)
	key := tlsCert.PrivateKey.(crypto.Signer)

	b, err := block.Build(
		ids.ID{1},
		time.Unix(123, 0),
		uint64(2),
		block.Epoch{},
		cert,
		innerBlockBytes,
		ids.ID{4},
		key,
	)
	require.NoError(err)

	require.NoError(bs.PutBlock(b, innerID))

	// The write must have been deduplicated; a fallback here means stripping
	// stopped round-tripping for this block shape, which is a bug.
	state := bs.(*blockState)
	require.Equal(float64(1), testutil.ToFloat64(state.numDedupStored))
	require.Zero(testutil.ToFloat64(state.numDedupFallback))

	// On-disk record must not contain the full block bytes (inner bytes stripped).
	blkID := b.ID()
	stored, err := db.Get(blkID[:])
	require.NoError(err)
	require.Less(len(stored), len(b.Bytes()))

	// Reconstructed block must be byte-identical to the original (uncached and
	// cached paths).
	fetched, err := bs.GetBlock(b.ID())
	require.NoError(err)
	require.Equal(b.Bytes(), fetched.Bytes())
	require.Equal(b.ID(), fetched.ID())

	fetched, err = bs.GetBlock(b.ID())
	require.NoError(err)
	require.Equal(b.Bytes(), fetched.Bytes())
}

// corruptedBlock wraps a real block but reports bytes that cannot round-trip
// through strip/restore, forcing the dedup write path into its fallback. The
// alias avoids the embedded field name colliding with the Block() method.
type wrappedBlock = block.Block

type corruptedBlock struct {
	wrappedBlock
}

func (b corruptedBlock) Bytes() []byte {
	return append(b.wrappedBlock.Bytes(), 0)
}

// TestBlockStateDedupFallback exercises the write-time fallback: a block whose
// bytes do not round-trip must be stored in the legacy full format and counted,
// never dropped or stored deduplicated.
func TestBlockStateDedupFallback(t *testing.T) {
	require := require.New(t)

	getInnerBytes := func(ids.ID) ([]byte, error) {
		require.FailNow("getInnerBytes must not be called on the write path")
		return nil, nil
	}

	db := memdb.New()
	bs, err := NewMeteredBlockState(db, "", prometheus.NewRegistry(), getInnerBytes, logging.NoLog{})
	require.NoError(err)

	tlsCert, err := staking.NewTLSCert()
	require.NoError(err)
	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(err)
	key := tlsCert.PrivateKey.(crypto.Signer)

	b, err := block.Build(
		ids.ID{1},
		time.Unix(123, 0),
		uint64(2),
		block.Epoch{},
		cert,
		[]byte{3},
		ids.ID{4},
		key,
	)
	require.NoError(err)
	corrupted := corruptedBlock{wrappedBlock: b}

	require.NoError(bs.PutBlock(corrupted, ids.ID{9}))

	state := bs.(*blockState)
	require.Zero(testutil.ToFloat64(state.numDedupStored))
	require.Equal(float64(1), testutil.ToFloat64(state.numDedupFallback))

	// The fallback record must be the legacy full format holding the block's
	// reported bytes.
	blkID := corrupted.ID()
	stored, err := db.Get(blkID[:])
	require.NoError(err)
	require.Equal(uint16(CodecVersion), binary.BigEndian.Uint16(stored))

	blkWrapper := blockWrapper{}
	_, err = Codec.Unmarshal(stored, &blkWrapper)
	require.NoError(err)
	require.Equal(corrupted.Bytes(), blkWrapper.Block)
}
