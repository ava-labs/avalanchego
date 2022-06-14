// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestAtomicTxImports(t *testing.T) {
	assert := assert.New(t)

	h := newTestHelpersCollection(t)
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()
	h.ctx.Lock.Lock()

	utxoID := avax.UTXOID{
		TxID:        ids.Empty.Prefix(1),
		OutputIndex: 1,
	}
	amount := uint64(70000)
	recipientKey := preFundedKeys[1]

	m := &atomic.Memory{}
	err := m.Initialize(logging.NoLog{}, prefixdb.New([]byte{5}, h.baseDB))
	if err != nil {
		t.Fatal(err)
	}
	h.ctx.SharedMemory = m.NewSharedMemory(h.ctx.ChainID)
	h.atomicUtxosMan = avax.NewAtomicUTXOManager(h.ctx.SharedMemory, stateless.Codec)
	h.txBuilder.ResetAtomicUTXOManager(h.atomicUtxosMan)
	peerSharedMemory := m.NewSharedMemory(h.ctx.XChainID)
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
	utxoBytes, err := stateless.Codec.Marshal(stateless.PreForkVersion, utxo)
	if err != nil {
		t.Fatal(err)
	}
	inputID := utxo.InputID()
	if err := peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{
		h.ctx.ChainID: {PutRequests: []*atomic.Element{{
			Key:   inputID[:],
			Value: utxoBytes,
			Traits: [][]byte{
				recipientKey.PublicKey().Address().Bytes(),
			},
		}}},
	},
	); err != nil {
		t.Fatal(err)
	}

	tx, err := h.txBuilder.NewImportTx(
		h.ctx.XChainID,
		recipientKey.PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{recipientKey},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}
	h.fullState.SetTimestamp(h.cfg.ApricotPhase5Time.Add(100 * time.Second))

	h.BlockBuilder.AddDecisionTx(tx)
	b, err := h.BuildBlock()
	assert.NoError(err)
	// Test multiple verify calls work
	err = b.Verify()
	assert.NoError(err)
	err = b.Accept()
	assert.NoError(err)
	_, txStatus, err := h.fullState.GetTx(tx.ID())
	assert.NoError(err)
	// Ensure transaction is in the committed state
	assert.Equal(txStatus, status.Committed)
	// Ensure standard block contains one atomic transaction
	assert.Equal(b.(*StandardBlock).inputs.Len(), 1)
}
