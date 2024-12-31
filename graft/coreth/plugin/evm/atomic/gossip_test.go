// (c) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestGossipAtomicTxMarshaller(t *testing.T) {
	require := require.New(t)

	want := &GossipAtomicTx{
		Tx: &Tx{
			UnsignedAtomicTx: &UnsignedImportTx{},
			Creds:            []verify.Verifiable{},
		},
	}
	marshaller := GossipAtomicTxMarshaller{}

	key0, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	err = want.Tx.Sign(Codec, [][]*secp256k1.PrivateKey{{key0}})
	require.NoError(err)

	bytes, err := marshaller.MarshalGossip(want)
	require.NoError(err)

	got, err := marshaller.UnmarshalGossip(bytes)
	require.NoError(err)
	require.Equal(want.GossipID(), got.GossipID())
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
		name        string
		add         []*GossipAtomicTx
		f           func(tx *GossipAtomicTx) bool
		expectedTxs []*GossipAtomicTx
	}{
		{
			name: "func matches nothing",
			add:  txs,
			f: func(*GossipAtomicTx) bool {
				return false
			},
			expectedTxs: []*GossipAtomicTx{},
		},
		{
			name: "func matches all",
			add:  txs,
			f: func(*GossipAtomicTx) bool {
				return true
			},
			expectedTxs: txs,
		},
		{
			name: "func matches subset",
			add:  txs,
			f: func(tx *GossipAtomicTx) bool {
				return tx.Tx == txs[0].Tx
			},
			expectedTxs: []*GossipAtomicTx{txs[0]},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			m, err := NewMempool(&snow.Context{}, prometheus.NewRegistry(), 10, nil)
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

			require.ElementsMatch(tt.expectedTxs, matches)
		})
	}
}
