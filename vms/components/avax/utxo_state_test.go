// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

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

	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&secp256k1fx.MintOutput{}),
		c.RegisterType(&secp256k1fx.TransferOutput{}),
		c.RegisterType(&secp256k1fx.Input{}),
		c.RegisterType(&secp256k1fx.TransferInput{}),
		c.RegisterType(&secp256k1fx.Credential{}),
		manager.RegisterCodec(codecVersion, c),
	)
	require.NoError(errs.Err)

	db := memdb.New()
	s := NewUTXOState(db, manager)

	_, err := s.GetUTXO(utxoID)
	require.Equal(database.ErrNotFound, err)

	_, err = s.GetUTXO(utxoID)
	require.Equal(database.ErrNotFound, err)

	err = s.DeleteUTXO(utxoID)
	require.NoError(err)

	err = s.PutUTXO(utxo)
	require.NoError(err)

	utxoIDs, err := s.UTXOIDs(addr[:], ids.Empty, 5)
	require.NoError(err)
	require.Equal([]ids.ID{utxoID}, utxoIDs)

	readUTXO, err := s.GetUTXO(utxoID)
	require.NoError(err)
	require.Equal(utxo, readUTXO)

	err = s.DeleteUTXO(utxoID)
	require.NoError(err)

	_, err = s.GetUTXO(utxoID)
	require.Equal(database.ErrNotFound, err)

	err = s.PutUTXO(utxo)
	require.NoError(err)

	s = NewUTXOState(db, manager)

	readUTXO, err = s.GetUTXO(utxoID)
	require.NoError(err)
	require.Equal(utxoID, readUTXO.InputID())
	require.Equal(utxo, readUTXO)

	utxoIDs, err = s.UTXOIDs(addr[:], ids.Empty, 5)
	require.NoError(err)
	require.Equal([]ids.ID{utxoID}, utxoIDs)
}
