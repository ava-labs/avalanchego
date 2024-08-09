// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/vms/avm/block"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const trackChecksums = false

var (
	parser             block.Parser
	populatedUTXO      *avax.UTXO
	populatedUTXOID    ids.ID
	populatedTx        *txs.Tx
	populatedTxID      ids.ID
	populatedBlk       block.Block
	populatedBlkHeight uint64
	populatedBlkID     ids.ID
)

func init() {
	var err error
	parser, err = block.NewParser(
		[]fxs.Fx{
			&secp256k1fx.Fx{},
		},
	)
	if err != nil {
		panic(err)
	}

	populatedUTXO = &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID: ids.GenerateTestID(),
		},
		Asset: avax.Asset{
			ID: ids.GenerateTestID(),
		},
		Out: &secp256k1fx.TransferOutput{
			Amt: 1,
		},
	}
	populatedUTXOID = populatedUTXO.InputID()

	populatedTx = &txs.Tx{Unsigned: &txs.BaseTx{BaseTx: avax.BaseTx{
		BlockchainID: ids.GenerateTestID(),
	}}}
	err = populatedTx.Initialize(parser.Codec())
	if err != nil {
		panic(err)
	}
	populatedTxID = populatedTx.ID()

	populatedBlk, err = block.NewStandardBlock(
		ids.GenerateTestID(),
		1,
		time.Now(),
		[]*txs.Tx{
			{
				Unsigned: &txs.BaseTx{BaseTx: avax.BaseTx{
					BlockchainID: ids.GenerateTestID(),
				}},
			},
		},
		parser.Codec(),
	)
	if err != nil {
		panic(err)
	}
	populatedBlkHeight = populatedBlk.Height()
	populatedBlkID = populatedBlk.ID()
}

type versions struct {
	chains map[ids.ID]Chain
}

func (v *versions) GetState(blkID ids.ID) (Chain, bool) {
	c, ok := v.chains[blkID]
	return c, ok
}

func TestState(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	vdb := versiondb.New(db)
	s, err := New(vdb, parser, prometheus.NewRegistry(), trackChecksums)
	require.NoError(err)

	s.AddUTXO(populatedUTXO)
	s.AddTx(populatedTx)
	s.AddBlock(populatedBlk)
	require.NoError(s.Commit())

	s, err = New(vdb, parser, prometheus.NewRegistry(), trackChecksums)
	require.NoError(err)

	ChainUTXOTest(t, s)
	ChainTxTest(t, s)
	ChainBlockTest(t, s)
}

func TestDiff(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	vdb := versiondb.New(db)
	s, err := New(vdb, parser, prometheus.NewRegistry(), trackChecksums)
	require.NoError(err)

	s.AddUTXO(populatedUTXO)
	s.AddTx(populatedTx)
	s.AddBlock(populatedBlk)
	require.NoError(s.Commit())

	parentID := ids.GenerateTestID()
	d, err := NewDiff(parentID, &versions{
		chains: map[ids.ID]Chain{
			parentID: s,
		},
	})
	require.NoError(err)

	ChainUTXOTest(t, d)
	ChainTxTest(t, d)
	ChainBlockTest(t, d)
}

func ChainUTXOTest(t *testing.T, c Chain) {
	require := require.New(t)

	fetchedUTXO, err := c.GetUTXO(populatedUTXOID)
	require.NoError(err)

	// Compare IDs because [fetchedUTXO] isn't initialized
	require.Equal(populatedUTXO.InputID(), fetchedUTXO.InputID())

	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID: ids.GenerateTestID(),
		},
		Asset: avax.Asset{
			ID: ids.GenerateTestID(),
		},
		Out: &secp256k1fx.TransferOutput{
			Amt: 1,
		},
	}
	utxoID := utxo.InputID()

	_, err = c.GetUTXO(utxoID)
	require.ErrorIs(err, database.ErrNotFound)

	c.AddUTXO(utxo)

	fetchedUTXO, err = c.GetUTXO(utxoID)
	require.NoError(err)
	require.Equal(utxo, fetchedUTXO)

	c.DeleteUTXO(utxoID)

	_, err = c.GetUTXO(utxoID)
	require.ErrorIs(err, database.ErrNotFound)
}

func ChainTxTest(t *testing.T, c Chain) {
	require := require.New(t)

	fetchedTx, err := c.GetTx(populatedTxID)
	require.NoError(err)

	// Compare IDs because [fetchedTx] differs between nil and empty fields
	require.Equal(populatedTx.ID(), fetchedTx.ID())

	// Pull again for the cached path
	fetchedTx, err = c.GetTx(populatedTxID)
	require.NoError(err)
	require.Equal(populatedTx.ID(), fetchedTx.ID())

	tx := &txs.Tx{Unsigned: &txs.BaseTx{BaseTx: avax.BaseTx{
		BlockchainID: ids.GenerateTestID(),
	}}}
	require.NoError(tx.Initialize(parser.Codec()))
	txID := tx.ID()

	_, err = c.GetTx(txID)
	require.ErrorIs(err, database.ErrNotFound)

	// Pull again for the cached path
	_, err = c.GetTx(txID)
	require.ErrorIs(err, database.ErrNotFound)

	c.AddTx(tx)

	fetchedTx, err = c.GetTx(txID)
	require.NoError(err)
	require.Equal(tx, fetchedTx)
}

func ChainBlockTest(t *testing.T, c Chain) {
	require := require.New(t)

	fetchedBlkID, err := c.GetBlockIDAtHeight(populatedBlkHeight)
	require.NoError(err)
	require.Equal(populatedBlkID, fetchedBlkID)

	fetchedBlk, err := c.GetBlock(populatedBlkID)
	require.NoError(err)
	require.Equal(populatedBlk.ID(), fetchedBlk.ID())

	// Pull again for the cached path
	fetchedBlkID, err = c.GetBlockIDAtHeight(populatedBlkHeight)
	require.NoError(err)
	require.Equal(populatedBlkID, fetchedBlkID)

	fetchedBlk, err = c.GetBlock(populatedBlkID)
	require.NoError(err)
	require.Equal(populatedBlk.ID(), fetchedBlk.ID())

	blk, err := block.NewStandardBlock(
		ids.GenerateTestID(),
		10,
		time.Now(),
		[]*txs.Tx{
			{
				Unsigned: &txs.BaseTx{BaseTx: avax.BaseTx{
					BlockchainID: ids.GenerateTestID(),
				}},
			},
		},
		parser.Codec(),
	)
	if err != nil {
		panic(err)
	}
	blkID := blk.ID()
	blkHeight := blk.Height()

	_, err = c.GetBlockIDAtHeight(blkHeight)
	require.ErrorIs(err, database.ErrNotFound)

	_, err = c.GetBlock(blkID)
	require.ErrorIs(err, database.ErrNotFound)

	// Pull again for the cached path
	_, err = c.GetBlockIDAtHeight(blkHeight)
	require.ErrorIs(err, database.ErrNotFound)

	_, err = c.GetBlock(blkID)
	require.ErrorIs(err, database.ErrNotFound)

	c.AddBlock(blk)

	fetchedBlkID, err = c.GetBlockIDAtHeight(blkHeight)
	require.NoError(err)
	require.Equal(blkID, fetchedBlkID)

	fetchedBlk, err = c.GetBlock(blkID)
	require.NoError(err)
	require.Equal(blk, fetchedBlk)
}

func TestInitializeChainState(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	vdb := versiondb.New(db)
	s, err := New(vdb, parser, prometheus.NewRegistry(), trackChecksums)
	require.NoError(err)

	stopVertexID := ids.GenerateTestID()
	genesisTimestamp := upgrade.InitiallyActiveTime
	require.NoError(s.InitializeChainState(stopVertexID, genesisTimestamp))

	lastAcceptedID := s.GetLastAccepted()
	genesis, err := s.GetBlock(lastAcceptedID)
	require.NoError(err)
	require.Equal(stopVertexID, genesis.Parent())
	require.Equal(genesisTimestamp.UnixNano(), genesis.Timestamp().UnixNano())

	childBlock, err := block.NewStandardBlock(
		genesis.ID(),
		genesis.Height()+1,
		genesisTimestamp,
		nil,
		parser.Codec(),
	)
	require.NoError(err)

	s.AddBlock(childBlock)
	s.SetLastAccepted(childBlock.ID())
	require.NoError(s.Commit())

	require.NoError(s.InitializeChainState(stopVertexID, genesisTimestamp))

	lastAcceptedID = s.GetLastAccepted()
	lastAccepted, err := s.GetBlock(lastAcceptedID)
	require.NoError(err)
	require.Equal(genesis.ID(), lastAccepted.Parent())
}
