// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const trackChecksum = false

func TestUTXOState(t *testing.T) {
	require := require.New(t)

	txID := ids.GenerateTestID()
	assetID := ids.GenerateTestID()
	addr := ids.GenerateTestShortID()
	utxo := &UTXO{
		UTXOID: UTXOID{
			TxID:        txID,
			OutputIndex: 0,
		},
		Asset: Asset{ID: assetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: 12345,
			OutputOwners: secp256k1fx.OutputOwners{
				Locktime:  54321,
				Threshold: 1,
				Addrs:     []ids.ShortID{addr},
			},
		},
	}
	utxoID := utxo.InputID()

	c := linearcodec.NewDefault()
	manager := codec.NewDefaultManager()

	require.NoError(c.RegisterType(&secp256k1fx.MintOutput{}))
	require.NoError(c.RegisterType(&secp256k1fx.TransferOutput{}))
	require.NoError(c.RegisterType(&secp256k1fx.Input{}))
	require.NoError(c.RegisterType(&secp256k1fx.TransferInput{}))
	require.NoError(c.RegisterType(&secp256k1fx.Credential{}))
	require.NoError(manager.RegisterCodec(codecVersion, c))

	db := memdb.New()
	s, err := NewUTXOState(
		prefixdb.New([]byte("foo"), db),
		prefixdb.New([]byte("bar"), db),
		manager,
		trackChecksum)
	require.NoError(err)

	_, err = s.GetUTXO(utxoID)
	require.Equal(database.ErrNotFound, err)

	_, err = s.GetUTXO(utxoID)
	require.Equal(database.ErrNotFound, err)

	require.NoError(s.DeleteUTXO(utxoID))

	require.NoError(s.PutUTXO(utxo))

	utxoIDs, err := s.UTXOIDs(addr[:], ids.Empty, 5)
	require.NoError(err)
	require.Equal([]ids.ID{utxoID}, utxoIDs)

	readUTXO, err := s.GetUTXO(utxoID)
	require.NoError(err)
	require.Equal(utxo, readUTXO)

	require.NoError(s.DeleteUTXO(utxoID))

	_, err = s.GetUTXO(utxoID)
	require.Equal(database.ErrNotFound, err)

	require.NoError(s.PutUTXO(utxo))

	s, err = NewUTXOState(
		prefixdb.New([]byte("foo"), db),
		prefixdb.New([]byte("bar"), db),
		manager,
		trackChecksum,
	)
	require.NoError(err)

	readUTXO, err = s.GetUTXO(utxoID)
	require.NoError(err)
	require.Equal(utxoID, readUTXO.InputID())
	require.Equal(utxo, readUTXO)

	utxoIDs, err = s.UTXOIDs(addr[:], ids.Empty, 5)
	require.NoError(err)
	require.Equal([]ids.ID{utxoID}, utxoIDs)
}
