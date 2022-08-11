// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestAtomicTxImports(t *testing.T) {
	assert := assert.New(t)

	env := newEnvironment(t)
	env.ctx.Lock.Lock()
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()

	utxoID := avax.UTXOID{
		TxID:        ids.Empty.Prefix(1),
		OutputIndex: 1,
	}
	amount := uint64(70000)
	recipientKey := preFundedKeys[1]

	m := atomic.NewMemory(prefixdb.New([]byte{5}, env.baseDB))

	env.msm.SharedMemory = m.NewSharedMemory(env.ctx.ChainID)
	peerSharedMemory := m.NewSharedMemory(env.ctx.XChainID)
	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: avaxAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: amount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{recipientKey.PublicKey().Address()},
			},
		},
	}
	utxoBytes, err := blocks.Codec.Marshal(txs.Version, utxo)
	assert.NoError(err)

	inputID := utxo.InputID()
	err = peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{
		env.ctx.ChainID: {PutRequests: []*atomic.Element{{
			Key:   inputID[:],
			Value: utxoBytes,
			Traits: [][]byte{
				recipientKey.PublicKey().Address().Bytes(),
			},
		}}},
	})
	assert.NoError(err)

	tx, err := env.txBuilder.NewImportTx(
		env.ctx.XChainID,
		recipientKey.PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{recipientKey},
		ids.ShortEmpty, // change addr
	)
	assert.NoError(err)

	env.state.SetTimestamp(env.config.ApricotPhase5Time.Add(100 * time.Second))

	env.Builder.AddDecisionTx(tx)
	b, err := env.BuildBlock()
	assert.NoError(err)
	// Test multiple verify calls work
	assert.NoError(b.Verify())
	assert.NoError(b.Accept())
	_, txStatus, err := env.state.GetTx(tx.ID())
	assert.NoError(err)
	// Ensure transaction is in the committed state
	assert.Equal(txStatus, status.Committed)
}
