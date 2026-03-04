// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
)

func TestUTXOIDVerifyNil(t *testing.T) {
	utxoID := (*UTXOID)(nil)
	err := utxoID.Verify()
	require.ErrorIs(t, err, errNilUTXOID)
}

func TestUTXOID(t *testing.T) {
	require := require.New(t)

	c := linearcodec.NewDefault()
	manager := codec.NewDefaultManager()
	require.NoError(manager.RegisterCodec(codecVersion, c))

	utxoID := UTXOID{
		TxID: ids.ID{
			0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
			0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
			0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
			0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
		},
		OutputIndex: 0x20212223,
	}

	require.NoError(utxoID.Verify())

	bytes, err := manager.Marshal(codecVersion, &utxoID)
	require.NoError(err)

	newUTXOID := UTXOID{}
	_, err = manager.Unmarshal(bytes, &newUTXOID)
	require.NoError(err)

	require.NoError(newUTXOID.Verify())
	require.Equal(utxoID.InputID(), newUTXOID.InputID())
}

func TestUTXOIDCompare(t *testing.T) {
	type test struct {
		name     string
		id1      UTXOID
		id2      UTXOID
		expected int
	}
	tests := []*test{
		{
			name:     "same",
			id1:      UTXOID{},
			id2:      UTXOID{},
			expected: 0,
		},
		{
			name: "id smaller",
			id1:  UTXOID{},
			id2: UTXOID{
				TxID: ids.ID{1},
			},
			expected: -1,
		},
		{
			name: "index smaller",
			id1:  UTXOID{},
			id2: UTXOID{
				OutputIndex: 1,
			},
			expected: -1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			require.Equal(tt.expected, tt.id1.Compare(&tt.id2))
			require.Equal(-tt.expected, tt.id2.Compare(&tt.id1))
		})
	}
}

func TestUTXOIDFromString(t *testing.T) {
	tests := []struct {
		description string
		utxoID      *UTXOID
		expectedStr string
		parseErr    error
	}{
		{
			description: "empty utxoID",
			utxoID:      &UTXOID{},
			expectedStr: "11111111111111111111111111111111LpoYY:0",
			parseErr:    nil,
		},
		{
			description: "a random utxoID",
			utxoID: &UTXOID{
				TxID:        ids.Empty.Prefix(2022),
				OutputIndex: 2022,
			},
			expectedStr: "PkHybKBmFvBkfumRvJZToECJp4oCiziLu95p86rU1THx1WAqa:2022",
			parseErr:    nil,
		},
		{
			description: "max output index utxoID",
			utxoID: &UTXOID{
				TxID:        ids.Empty.Prefix(1789),
				OutputIndex: math.MaxUint32,
			},
			expectedStr: "Y3sXNphGY121uVzj37rA8ooUAHrfuDZahzLrTq6UauAZTEqoX:4294967295",
			parseErr:    nil,
		},
		{
			description: "not enough tokens",
			utxoID:      &UTXOID{},
			expectedStr: "11111111111111111111111111111111LpoYY",
			parseErr:    errMalformedUTXOIDString,
		},
		{
			description: "not enough tokens",
			utxoID:      &UTXOID{},
			expectedStr: "11111111111111111111111111111111LpoYY:10:10",
			parseErr:    errMalformedUTXOIDString,
		},
		{
			description: "missing TxID",
			utxoID:      &UTXOID{},
			expectedStr: ":2022",
			parseErr:    errFailedDecodingUTXOIDTxID,
		},
		{
			description: "non TxID",
			utxoID:      &UTXOID{},
			expectedStr: "11:NOT_AN_INDEX",
			parseErr:    errFailedDecodingUTXOIDTxID,
		},
		{
			description: "missing index",
			utxoID:      &UTXOID{},
			expectedStr: "11111111111111111111111111111111LpoYY:",
			parseErr:    errFailedDecodingUTXOIDIndex,
		},
		{
			description: "non index",
			utxoID:      &UTXOID{},
			expectedStr: "11111111111111111111111111111111LpoYY:NOT_AN_INDEX",
			parseErr:    errFailedDecodingUTXOIDIndex,
		},
		{
			description: "negative index",
			utxoID:      &UTXOID{},
			expectedStr: "11111111111111111111111111111111LpoYY:-1",
			parseErr:    errFailedDecodingUTXOIDIndex,
		},
		{
			description: "index too large",
			utxoID:      &UTXOID{},
			expectedStr: "11111111111111111111111111111111LpoYY:4294967296",
			parseErr:    errFailedDecodingUTXOIDIndex,
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			require := require.New(t)

			retrievedUTXOID, err := UTXOIDFromString(test.expectedStr)
			require.ErrorIs(err, test.parseErr)
			if test.parseErr != nil {
				return
			}
			require.Equal(test.utxoID.InputID(), retrievedUTXOID.InputID())
			require.Equal(test.utxoID, retrievedUTXOID)
			require.Equal(test.utxoID.String(), retrievedUTXOID.String())
		})
	}
}
