// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

func testCertSignedBlockState(a *require.Assertions, bs BlockState) {
	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	innerBlockBytes := []byte{3}
	chainID := ids.ID{4}

	tlsCert, err := staking.NewTLSCert()
	a.NoError(err)

	tlsSigner, err := crypto.NewTLSSigner(tlsCert)
	a.NoError(err)

	b, err := block.BuildCertSigned(
		parentID,
		timestamp,
		pChainHeight,
		tlsCert.Leaf,
		innerBlockBytes,
		chainID,
		&tlsSigner,
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

func testBlsSignedBlockState(a *require.Assertions, bs BlockState) {
	nodeID := ids.NodeID{'n', 'o', 'd', 'e'}
	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	innerBlockBytes := []byte{3}
	chainID := ids.ID{4}

	sk, err := bls.NewSecretKey()
	a.NoError(err)
	blsSigner := crypto.BLSKeySigner{
		SecretKey: sk,
	}

	b, err := block.BuildBlsSigned(
		parentID,
		timestamp,
		pChainHeight,
		nodeID,
		innerBlockBytes,
		chainID,
		&blsSigner,
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

	{
		db := memdb.New()
		bs := NewBlockState(db)
		testCertSignedBlockState(a, bs)
	}

	{
		db := memdb.New()
		bs := NewBlockState(db)
		testBlsSignedBlockState(a, bs)
	}
}

func TestMeteredBlockState(t *testing.T) {
	a := require.New(t)

	{
		db := memdb.New()
		bs, err := NewMeteredBlockState(db, "", prometheus.NewRegistry())
		a.NoError(err)

		testCertSignedBlockState(a, bs)
	}

	{
		db := memdb.New()
		bs, err := NewMeteredBlockState(db, "", prometheus.NewRegistry())
		a.NoError(err)

		testBlsSignedBlockState(a, bs)
	}
}
