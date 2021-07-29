// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"math/rand"
	"testing"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestNewImportTx(t *testing.T) {
	vm, baseDB, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	type test struct {
		description   string
		sharedMemory  atomic.SharedMemory
		recipientKeys []*crypto.PrivateKeySECP256K1R
		shouldErr     bool
	}

	factory := crypto.FactorySECP256K1R{}
	recipientKeyIntf, err := factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	recipientKey := recipientKeyIntf.(*crypto.PrivateKeySECP256K1R)

	cnt := new(byte)

	// Returns a shared memory where GetDatabase returns a database
	// where [recipientKey] has a balance of [amt]
	fundedSharedMemory := func(amt uint64) atomic.SharedMemory {
		*cnt++
		m := &atomic.Memory{}
		err := m.Initialize(logging.NoLog{}, prefixdb.New([]byte{*cnt}, baseDB))
		if err != nil {
			t.Fatal(err)
		}

		sm := m.NewSharedMemory(vm.ctx.ChainID)
		peerSharedMemory := m.NewSharedMemory(avmID)

		// #nosec G404
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        ids.GenerateTestID(),
				OutputIndex: rand.Uint32(),
			},
			Asset: avax.Asset{ID: avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: amt,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Addrs:     []ids.ShortID{recipientKey.PublicKey().Address()},
					Threshold: 1,
				},
			},
		}
		utxoBytes, err := Codec.Marshal(codecVersion, utxo)
		if err != nil {
			t.Fatal(err)
		}
		inputID := utxo.InputID()
		if err := peerSharedMemory.Put(vm.ctx.ChainID, []*atomic.Element{{
			Key:   inputID[:],
			Value: utxoBytes,
			Traits: [][]byte{
				recipientKey.PublicKey().Address().Bytes(),
			},
		}}); err != nil {
			t.Fatal(err)
		}

		return sm
	}

	tests := []test{
		{
			description:   "recipient key can't pay fee;",
			sharedMemory:  fundedSharedMemory(vm.TxFee - 1),
			recipientKeys: []*crypto.PrivateKeySECP256K1R{recipientKey},
			shouldErr:     true,
		},
		{

			description:   "recipient key pays fee",
			sharedMemory:  fundedSharedMemory(vm.TxFee),
			recipientKeys: []*crypto.PrivateKeySECP256K1R{recipientKey},
			shouldErr:     false,
		},
	}

	to := ids.GenerateTestShortID()
	for _, tt := range tests {
		vm.AtomicUTXOManager = avax.NewAtomicUTXOManager(tt.sharedMemory, Codec)
		tx, err := vm.newImportTx(avmID, to, tt.recipientKeys, ids.ShortEmpty)
		if err != nil {
			if !tt.shouldErr {
				t.Fatalf("test '%s' unexpectedly errored with: %s", tt.description, err)
			}
			continue
		} else if tt.shouldErr {
			t.Fatalf("test '%s' didn't error but it should have", tt.description)
		}
		unsignedTx := tx.UnsignedTx.(*UnsignedImportTx)
		if len(unsignedTx.ImportedInputs) == 0 {
			t.Fatalf("in test '%s', tx has no imported inputs", tt.description)
		} else if len(tx.Creds) != len(unsignedTx.Ins)+len(unsignedTx.ImportedInputs) {
			t.Fatalf("in test '%s', should have same number of credentials as inputs", tt.description)
		}
		totalIn := uint64(0)
		for _, in := range unsignedTx.Ins {
			totalIn += in.Input().Amount()
		}
		for _, in := range unsignedTx.ImportedInputs {
			totalIn += in.Input().Amount()
		}
		totalOut := uint64(0)
		for _, out := range unsignedTx.Outs {
			totalOut += out.Out.Amount()
		}
		if totalIn-totalOut != vm.TxFee {
			t.Fatalf("in test '%s'. inputs (%d) != outputs (%d) + txFee (%d)", tt.description, totalIn, totalOut, vm.TxFee)
		}
	}
}
