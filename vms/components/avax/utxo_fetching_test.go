// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestFetchUTXOs(t *testing.T) {
	require := require.New(t)

	txID := ids.GenerateTestID()
	assetID := ids.GenerateTestID()
	addr := ids.GenerateTestShortID()
	addrs := set.Of(addr)
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

	c := linearcodec.NewDefault()
	manager := codec.NewDefaultManager()

	require.NoError(c.RegisterType(&secp256k1fx.TransferOutput{}))
	require.NoError(manager.RegisterCodec(codecVersion, c))

	s, err := NewUTXOState(
		memdb.New(),
		memdb.New(),
		manager,
		trackChecksum,
	)
	require.NoError(err)

	require.NoError(s.PutUTXO(utxo))

	utxos, err := GetAllUTXOs(s, addrs)
	require.NoError(err)
	require.Len(utxos, 1)
	require.Equal(utxo, utxos[0])

	balance, err := GetBalance(s, addrs)
	require.NoError(err)
	require.Equal(uint64(12345), balance)
}

// TestGetPaginatedUTXOs tests
// - Pagination when the total UTXOs exceed maxUTXOsToFetch (512)
// - Fetching all UTXOs when they exceed maxUTXOsToFetch (512)
func TestGetPaginatedUTXOs(t *testing.T) {
	require := require.New(t)

	addr0 := ids.GenerateTestShortID()
	addr1 := ids.GenerateTestShortID()
	addr2 := ids.GenerateTestShortID()
	addrs := set.Of(addr0, addr1)

	c := linearcodec.NewDefault()
	manager := codec.NewDefaultManager()

	require.NoError(c.RegisterType(&secp256k1fx.TransferOutput{}))
	require.NoError(manager.RegisterCodec(codecVersion, c))

	db := memdb.New()
	s, err := NewUTXOState(
		prefixdb.New([]byte("foo"), db),
		prefixdb.New([]byte("bar"), db),
		manager,
		trackChecksum,
	)
	require.NoError(err)

	// Create 1000 UTXOs each on addr0, addr1, and addr2.
	for i := 0; i < 1000; i++ {
		txID := ids.GenerateTestID()
		assetID := ids.GenerateTestID()
		utxo0 := &UTXO{
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
					Addrs:     []ids.ShortID{addr0},
				},
			},
		}
		require.NoError(s.PutUTXO(utxo0))

		utxo1 := &UTXO{
			UTXOID: UTXOID{
				TxID:        txID,
				OutputIndex: 1,
			},
			Asset: Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  54321,
					Threshold: 1,
					Addrs:     []ids.ShortID{addr1},
				},
			},
		}
		require.NoError(s.PutUTXO(utxo1))

		utxo2 := &UTXO{
			UTXOID: UTXOID{
				TxID:        txID,
				OutputIndex: 2,
			},
			Asset: Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  54321,
					Threshold: 1,
					Addrs:     []ids.ShortID{addr2},
				},
			},
		}
		require.NoError(s.PutUTXO(utxo2))
	}

	var (
		fetchedUTXOs []*UTXO
		lastAddr     = ids.ShortEmpty
		lastIdx      = ids.Empty
		totalUTXOs   []*UTXO
	)
	for i := 0; i <= 10; i++ {
		fetchedUTXOs, lastAddr, lastIdx, err = GetPaginatedUTXOs(s, addrs, lastAddr, lastIdx, 512)
		require.NoError(err)

		totalUTXOs = append(totalUTXOs, fetchedUTXOs...)
	}

	require.Len(totalUTXOs, 2000)

	// Fetch all UTXOs
	notPaginatedUTXOs, err := GetAllUTXOs(s, addrs)
	require.NoError(err)
	require.Len(notPaginatedUTXOs, len(totalUTXOs))
}
