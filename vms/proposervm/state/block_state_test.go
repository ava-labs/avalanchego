// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************
// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"

	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
)

func testBlockState(a *require.Assertions, bs BlockState) {
	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	innerBlockBytes := []byte{3}
	chainID := ids.ID{4}

	tlsCert, err := staking.NewTLSCert()
	a.NoError(err)

	cert := tlsCert.Leaf
	key := tlsCert.PrivateKey.(crypto.Signer)

	nodeIDBytes, err := secp256k1.RecoverSecp256PublicKey(cert)
	a.NoError(err)
	nodeID, err := ids.ToNodeID(nodeIDBytes)
	a.NoError(err)

	b, err := block.Build(
		parentID,
		timestamp,
		pChainHeight,
		nodeID,
		cert,
		innerBlockBytes,
		chainID,
		key,
	)
	a.NoError(err)

	_, _, err = bs.GetBlock(b.ID())
	a.Equal(database.ErrNotFound, err)

	_, _, err = bs.GetBlock(b.ID())
	a.Equal(database.ErrNotFound, err)

	err = bs.PutBlock(b, choices.Accepted)
	a.NoError(err)

	fetchedBlock, fetchedStatus, err := bs.GetBlock(b.ID())
	a.NoError(err)
	a.Equal(choices.Accepted, fetchedStatus)
	a.Equal(b.Bytes(), fetchedBlock.Bytes())

	fetchedBlock, fetchedStatus, err = bs.GetBlock(b.ID())
	a.NoError(err)
	a.Equal(choices.Accepted, fetchedStatus)
	a.Equal(b.Bytes(), fetchedBlock.Bytes())
}

func TestBlockState(t *testing.T) {
	a := require.New(t)

	db := memdb.New()
	bs := NewBlockState(db)

	testBlockState(a, bs)
}

func TestMeteredBlockState(t *testing.T) {
	a := require.New(t)

	db := memdb.New()
	bs, err := NewMeteredBlockState(db, "", prometheus.NewRegistry())
	a.NoError(err)

	testBlockState(a, bs)
}

func TestGetBlockWithUncachedBlock(t *testing.T) {
	a := require.New(t)
	db, bs, blk, err := initCommonTestData(a)
	a.NoError(err)

	blkWrapper := blockWrapper{
		Block:  blk.Bytes(),
		Status: choices.Accepted,
		block:  blk,
	}

	bytes, err := c.Marshal(version, &blkWrapper)
	a.NoError(err)

	blkID := blk.ID()
	err = db.Put(blkID[:], bytes)
	a.NoError(err)
	actualBlk, _, err := bs.GetBlock(blk.ID())
	a.Equal(blk, actualBlk)
	a.NoError(err)
}

func initCommonTestData(a *require.Assertions) (database.Database, BlockState, block.SignedBlock, error) {
	db := memdb.New()
	bs := NewBlockState(db)

	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	innerBlockBytes := []byte{3}
	chainID := ids.ID{4}

	tlsCert, _ := staking.NewTLSCert()

	cert := tlsCert.Leaf
	key := tlsCert.PrivateKey.(crypto.Signer)

	nodeIDBytes, err := secp256k1.RecoverSecp256PublicKey(cert)
	a.NoError(err)
	nodeID, err := ids.ToNodeID(nodeIDBytes)
	a.NoError(err)

	blk, err := block.Build(
		parentID,
		timestamp,
		pChainHeight,
		nodeID,
		cert,
		innerBlockBytes,
		chainID,
		key,
	)
	return db, bs, blk, err
}
