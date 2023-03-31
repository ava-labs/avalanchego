// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestFetchUTXOs(t *testing.T) {
	require := require.New(t)

	txID := ids.GenerateTestID()
	assetID := ids.GenerateTestID()
	addr := ids.GenerateTestShortID()
	addrs := set.Set[ids.ShortID]{}
	addrs.Add(addr)
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

	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&secp256k1fx.TransferOutput{}),
		manager.RegisterCodec(codecVersion, c),
	)
	require.NoError(errs.Err)

	db := memdb.New()
	s := NewUTXOState(db, manager)

	err := s.PutUTXO(utxo)
	require.NoError(err)

	utxos, err := GetAllUTXOs(s, addrs)
	require.NoError(err)
	require.Len(utxos, 1)
	require.Equal(utxo, utxos[0])

	balance, err := GetBalance(s, addrs)
	require.NoError(err)
	require.EqualValues(12345, balance)
}

// TestGetPaginatedUTXOs tests
// - Pagination when the total UTXOs exceed maxUTXOsToFetch (512)
// - Fetching all UTXOs when they exceed maxUTXOsToFetch (512)
func TestGetPaginatedUTXOs(t *testing.T) {
	require := require.New(t)

	addr0 := ids.GenerateTestShortID()
	addr1 := ids.GenerateTestShortID()
	addr2 := ids.GenerateTestShortID()
	addrs := set.Set[ids.ShortID]{}
	addrs.Add(addr0, addr1)

	c := linearcodec.NewDefault()
	manager := codec.NewDefaultManager()

	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&secp256k1fx.TransferOutput{}),
		manager.RegisterCodec(codecVersion, c),
	)
	require.NoError(errs.Err)

	db := memdb.New()
	s := NewUTXOState(db, manager)

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
		err := s.PutUTXO(utxo0)
		require.NoError(err)

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
		err = s.PutUTXO(utxo1)
		require.NoError(err)

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
		err = s.PutUTXO(utxo2)
		require.NoError(err)
	}

	var (
		fetchedUTXOs []*UTXO
		err          error
	)

	lastAddr := ids.ShortEmpty
	lastIdx := ids.Empty

	var totalUTXOs []*UTXO
	for i := 0; i <= 10; i++ {
		fetchedUTXOs, lastAddr, lastIdx, err = GetPaginatedUTXOs(s, addrs, lastAddr, lastIdx, 512)
		if err != nil {
			t.Fatal(err)
		}

		totalUTXOs = append(totalUTXOs, fetchedUTXOs...)
	}

	if len(totalUTXOs) != 2000 {
		t.Fatalf("Wrong number of utxos. Should have paginated through all. Expected (%d) returned (%d)", 2000, len(totalUTXOs))
	}

	// Fetch all UTXOs
	notPaginatedUTXOs, err := GetAllUTXOs(s, addrs)
	if err != nil {
		t.Fatal(err)
	}

	if len(notPaginatedUTXOs) != len(totalUTXOs) {
		t.Fatalf("Wrong number of utxos. Expected (%d) returned (%d)", len(totalUTXOs), len(notPaginatedUTXOs))
	}
}
