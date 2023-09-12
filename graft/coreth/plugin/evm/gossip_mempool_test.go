// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestGossipAtomicTxMarshal(t *testing.T) {
	require := require.New(t)

	expected := &GossipAtomicTx{
		Tx: &Tx{
			UnsignedAtomicTx: &UnsignedImportTx{},
			Creds:            []verify.Verifiable{},
		},
	}

	key0 := testKeys[0]
	require.NoError(expected.Tx.Sign(Codec, [][]*secp256k1.PrivateKey{{key0}}))

	bytes, err := expected.Marshal()
	require.NoError(err)

	actual := &GossipAtomicTx{}
	require.NoError(actual.Unmarshal(bytes))

	require.NoError(err)
	require.Equal(expected.GetID(), actual.GetID())
}

func TestAtomicMempoolIterate(t *testing.T) {
	txs := []*GossipAtomicTx{
		{
			Tx: &Tx{
				UnsignedAtomicTx: &TestUnsignedTx{
					IDV: ids.GenerateTestID(),
				},
			},
		},
		{
			Tx: &Tx{
				UnsignedAtomicTx: &TestUnsignedTx{
					IDV: ids.GenerateTestID(),
				},
			},
		},
	}

	tests := []struct {
		name           string
		add            []*GossipAtomicTx
		f              func(tx *GossipAtomicTx) bool
		possibleValues []*GossipAtomicTx
		expectedLen    int
	}{
		{
			name: "func matches nothing",
			add:  txs,
			f: func(*GossipAtomicTx) bool {
				return false
			},
			possibleValues: nil,
		},
		{
			name: "func matches all",
			add:  txs,
			f: func(*GossipAtomicTx) bool {
				return true
			},
			possibleValues: txs,
			expectedLen:    2,
		},
		{
			name: "func matches subset",
			add:  txs,
			f: func(tx *GossipAtomicTx) bool {
				return tx.Tx == txs[0].Tx
			},
			possibleValues: txs,
			expectedLen:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			m, err := NewMempool(ids.Empty, 10)
			require.NoError(err)

			for _, add := range tt.add {
				require.NoError(m.Add(add))
			}

			matches := make([]*GossipAtomicTx, 0)
			f := func(tx *GossipAtomicTx) bool {
				match := tt.f(tx)

				if match {
					matches = append(matches, tx)
				}

				return match
			}

			m.Iterate(f)

			require.Len(matches, tt.expectedLen)
			require.Subset(tt.possibleValues, matches)
		})
	}
}
