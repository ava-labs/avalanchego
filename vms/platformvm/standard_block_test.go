// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/chain4travel/caminogo/chains/atomic"
	"github.com/chain4travel/caminogo/database/prefixdb"
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/utils/crypto"
	"github.com/chain4travel/caminogo/utils/logging"
	"github.com/chain4travel/caminogo/vms/components/avax"
	"github.com/chain4travel/caminogo/vms/platformvm/status"
	"github.com/chain4travel/caminogo/vms/secp256k1fx"
)

func TestAtomicTxImports(t *testing.T) {
	vm, baseDB, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()
	assert := assert.New(t)

	utxoID := avax.UTXOID{
		TxID:        ids.Empty.Prefix(1),
		OutputIndex: 1,
	}
	amount := uint64(70000)
	recipientKey := keys[1]

	m := &atomic.Memory{}
	err := m.Initialize(logging.NoLog{}, prefixdb.New([]byte{5}, baseDB))
	if err != nil {
		t.Fatal(err)
	}
	vm.ctx.SharedMemory = m.NewSharedMemory(vm.ctx.ChainID)
	vm.AtomicUTXOManager = avax.NewAtomicUTXOManager(vm.ctx.SharedMemory, Codec)
	peerSharedMemory := m.NewSharedMemory(vm.ctx.XChainID)
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
	utxoBytes, err := Codec.Marshal(CodecVersion, utxo)
	if err != nil {
		t.Fatal(err)
	}
	inputID := utxo.InputID()
	if err := peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{vm.ctx.ChainID: {PutRequests: []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			recipientKey.PublicKey().Address().Bytes(),
		},
	}}}}); err != nil {
		t.Fatal(err)
	}

	tx, err := vm.newImportTx(
		vm.ctx.XChainID,
		recipientKey.PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{recipientKey},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}
	vm.internalState.SetTimestamp(vm.ApricotPhase5Time.Add(100 * time.Second))

	vm.mempool.AddDecisionTx(tx)
	b, err := vm.BuildBlock()
	assert.NoError(err)
	// Test multiple verify calls work
	err = b.Verify()
	assert.NoError(err)
	err = b.Accept()
	assert.NoError(err)
	_, txStatus, err := vm.internalState.GetTx(tx.ID())
	assert.NoError(err)
	// Ensure transaction is in the committed state
	assert.Equal(txStatus, status.Committed)
	// Ensure standard block contains one atomic transaction
	assert.Equal(b.(*StandardBlock).inputs.Len(), 1)
}
