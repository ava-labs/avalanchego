// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var _ BlockTimer = &dummyBlkTimer{}

type dummyBlkTimer struct{}

func (bt *dummyBlkTimer) ResetBlockTimer() {}

var preFundedKeys = crypto.BuildTestKeys()

// shows that valid tx is not added to mempool if this would exceed its maximum
// size
func TestBlockBuilderMaxMempoolSizeHandling(t *testing.T) {
	require := require.New(t)

	registerer := prometheus.NewRegistry()
	mpool, err := NewMempool("mempool", registerer, &dummyBlkTimer{})
	require.NoError(err)

	// create candidate tx
	utx := &txs.CreateChainTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    10,
			BlockchainID: ids.Empty.Prefix(1),
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID:        ids.ID{'t', 'x', 'I', 'D'},
					OutputIndex: 2,
				},
				Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 'r', 't'}},
				In: &secp256k1fx.TransferInput{
					Amt:   uint64(5678),
					Input: secp256k1fx.Input{SigIndices: []uint32{0}},
				},
			}},
			Outs: []*avax.TransferableOutput{{
				Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 'r', 't'}},
				Out: &secp256k1fx.TransferOutput{
					Amt: uint64(1234),
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
					},
				},
			}},
		}},
		SubnetID:    ids.ID{'s', 'u', 'b', 'n', 'e', 't'},
		ChainName:   "chainName",
		VMID:        ids.ID{'V', 'M', 'I', 'D'},
		FxIDs:       []ids.ID{{'f', 'x', 'I', 'D', 's'}},
		GenesisData: []byte{'g', 'e', 'n', 'D', 'a', 't', 'a'},
		SubnetAuth:  &secp256k1fx.Input{SigIndices: []uint32{1}},
	}
	tx, err := txs.NewSigned(utx, txs.Codec, nil)
	require.NoError(err)

	// shortcut to simulated almost filled mempool
	mpool.(*mempool).bytesAvailable = len(tx.Bytes()) - 1

	err = mpool.Add(tx)
	require.True(errors.Is(err, errMempoolFull), err, "max mempool size breached")

	// shortcut to simulated almost filled mempool
	mpool.(*mempool).bytesAvailable = len(tx.Bytes())

	err = mpool.Add(tx)
	require.NoError(err, "should have added tx to mempool")
}
