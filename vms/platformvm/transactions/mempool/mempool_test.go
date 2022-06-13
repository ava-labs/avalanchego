// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

var _ BlockTimer = &dummyBlkTimer{}

type dummyBlkTimer struct{}

func (bt *dummyBlkTimer) ResetBlockTimer() {}

var preFundedKeys []*crypto.PrivateKeySECP256K1R

func init() {
	dummyCtx := snow.DefaultContextTest()
	preFundedKeys = make([]*crypto.PrivateKeySECP256K1R, 0)
	factory := crypto.FactorySECP256K1R{}
	for _, key := range []string{
		"24jUJ9vZexUM6expyMcT48LBx27k1m7xpraoV62oSQAHdziao5",
		"2MMvUMsxx6zsHSNXJdFD8yc5XkancvwyKPwpw4xUK3TCGDuNBY",
		"cxb7KpGWhDMALTjNNSJ7UQkkomPesyWAPUaWRGdyeBNzR6f35",
		"ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN",
		"2RWLv6YVEXDiWLpaCbXhhqxtLbnFaKQsWPSSMSPhpWo47uJAeV",
	} {
		privKeyBytes, err := formatting.Decode(formatting.CB58, key)
		dummyCtx.Log.AssertNoError(err)
		pk, err := factory.ToPrivateKey(privKeyBytes)
		dummyCtx.Log.AssertNoError(err)
		preFundedKeys = append(preFundedKeys, pk.(*crypto.PrivateKeySECP256K1R))
	}
}

// shows that valid tx is not added to mempool if this would exceed its maximum
// size
func TestBlockBuilderMaxMempoolSizeHandling(t *testing.T) {
	assert := assert.New(t)

	registerer := prometheus.NewRegistry()
	mpool, err := NewMempool("mempool", registerer, &dummyBlkTimer{})
	assert.NoError(err)

	// create candidate tx
	utx := &unsigned.CreateChainTx{
		BaseTx: unsigned.BaseTx{BaseTx: avax.BaseTx{
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
	tx, err := signed.NewSigned(utx, unsigned.Codec, nil)
	assert.NoError(err)

	// shortcut to simulated almost filled mempool
	mpool.(*mempool).bytesAvailable = len(tx.Bytes()) - 1

	err = mpool.Add(tx)
	assert.Equal(ErrMempoolFull, err, "max mempool size breached")

	// shortcut to simulated almost filled mempool
	mpool.(*mempool).bytesAvailable = len(tx.Bytes())

	err = mpool.Add(tx)
	assert.NoError(err, "should have added tx to mempool")
}
