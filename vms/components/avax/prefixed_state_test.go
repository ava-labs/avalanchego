// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"math"
	"testing"

	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/codec"
	"github.com/stretchr/testify/assert"
)

func TestPrefixedFunds(t *testing.T) {
	db := memdb.New()
	cc := codec.NewDefault()

	cc.RegisterType(&TestAddressable{})

	st := NewPrefixedState(db, cc)

	addr := ids.GenerateTestShortID()
	addrBytes := addr.Bytes()

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
				addrBytes,
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
				addrBytes,
			},
		},
	}

	assert.NoError(t, st.FundAVMUTXO(avmUTXO))
	assert.NoError(t, st.FundPlatformUTXO(platformUTXO))

	avmUTXOIDs, err := st.AVMFunds(addr.Bytes(), ids.Empty, math.MaxInt32)
	assert.NoError(t, err)
	assert.Equal(t, []ids.ID{avmUTXO.InputID()}, avmUTXOIDs)

	platformUTXOIDs, err := st.PlatformFunds(addr.Bytes(), ids.Empty, math.MaxInt32)
	assert.NoError(t, err)
	assert.Equal(t, []ids.ID{platformUTXO.InputID()}, platformUTXOIDs)

	assert.NoError(t, st.SpendAVMUTXO(avmUTXO.InputID()))

	avmUTXOIDs, err = st.AVMFunds(addr.Bytes(), ids.Empty, math.MaxInt32)
	assert.NoError(t, err)
	assert.Len(t, avmUTXOIDs, 0)
}
