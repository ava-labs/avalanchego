// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ava

import (
	"testing"

	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/vms/components/codec"
	"github.com/stretchr/testify/assert"
)

func TestPrefixedFunds(t *testing.T) {
	db := memdb.New()
	cc := codec.NewDefault()

	cc.RegisterType(&TestAddressable{})

	st := NewPrefixedState(db, cc)

	avmUTXO := &UTXO{
		UTXOID: UTXOID{
			TxID:        ids.Empty,
			OutputIndex: 0,
		},
		Asset: Asset{
			ID: ids.Empty,
		},
		Out: &TestAddressable{
			Addrs: [][]byte{
				[]byte{0},
			},
		},
	}

	platformUTXO := &UTXO{
		UTXOID: UTXOID{
			TxID:        ids.Empty,
			OutputIndex: 1,
		},
		Asset: Asset{
			ID: ids.Empty,
		},
		Out: &TestAddressable{
			Addrs: [][]byte{
				[]byte{0},
			},
		},
	}

	assert.NoError(t, st.FundAVMUTXO(avmUTXO))
	assert.NoError(t, st.FundPlatformUTXO(platformUTXO))

	addrID := ids.NewID(hashing.ComputeHash256Array([]byte{0}))

	avmUTXOIDs, err := st.AVMFunds(addrID)
	assert.NoError(t, err)
	assert.Equal(t, []ids.ID{avmUTXO.InputID()}, avmUTXOIDs)

	platformUTXOIDs, err := st.PlatformFunds(addrID)
	assert.NoError(t, err)
	assert.Equal(t, []ids.ID{platformUTXO.InputID()}, platformUTXOIDs)

	assert.NoError(t, st.SpendAVMUTXO(avmUTXO.InputID()))

	avmUTXOIDs, err = st.AVMFunds(addrID)
	assert.NoError(t, err)
	assert.Len(t, avmUTXOIDs, 0)
}
