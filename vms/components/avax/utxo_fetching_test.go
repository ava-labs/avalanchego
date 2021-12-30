// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestFetchUTXOs(t *testing.T) {
	assert := assert.New(t)

	txID := ids.GenerateTestID()
	assetID := ids.GenerateTestID()
	addr := ids.GenerateTestShortID()
	addrs := ids.ShortSet{}
	addrs.Add(addr)
	utxoID := ids.GenerateTestID()
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
	assert.NoError(errs.Err)

	db := memdb.New()
	s := NewUTXOState(db, manager)

	err := s.PutUTXO(utxoID, utxo)
	assert.NoError(err)

	utxos, err := GetAllUTXOs(s, addrs)
	assert.NoError(err)
	assert.Len(utxos, 1)
	assert.Equal(utxo, utxos[0])

	balance, err := GetBalance(s, addrs)
	assert.NoError(err)
	assert.EqualValues(12345, balance)
}

// TestGetPaginatedUTXOs tests
// - Pagination when the total UTXOs exceed maxUTXOsToFetch (512)
// - Fetching all UTXOs when they exceed maxUTXOsToFetch (512)
func TestGetPaginatedUTXOs(t *testing.T) {
	assert := assert.New(t)

	addr0 := ids.GenerateTestShortID()
	addr1 := ids.GenerateTestShortID()
	addr2 := ids.GenerateTestShortID()
	addrs := ids.ShortSet{}
	addrs.Add(addr0, addr1)

	c := linearcodec.NewDefault()
	manager := codec.NewDefaultManager()

	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&secp256k1fx.TransferOutput{}),
		manager.RegisterCodec(codecVersion, c),
	)
	assert.NoError(errs.Err)

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
		err := s.PutUTXO(utxo0.InputID(), utxo0)
		assert.NoError(err)

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
		err = s.PutUTXO(utxo1.InputID(), utxo1)
		assert.NoError(err)

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
		err = s.PutUTXO(utxo2.InputID(), utxo2)
		assert.NoError(err)
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
