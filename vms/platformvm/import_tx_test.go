// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

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
		sourceChainID ids.ID
		sharedMemory  atomic.SharedMemory
		sourceKeys    []*crypto.PrivateKeySECP256K1R
		timestamp     time.Time
		shouldErr     bool
		shouldVerify  bool
	}

	factory := crypto.FactorySECP256K1R{}
	sourceKeyIntf, err := factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	sourceKey := sourceKeyIntf.(*crypto.PrivateKeySECP256K1R)

	cnt := new(byte)

	// Returns a shared memory where GetDatabase returns a database
	// where [recipientKey] has a balance of [amt]
	fundedSharedMemory := func(peerChain ids.ID, amt uint64) atomic.SharedMemory {
		*cnt++
		m := &atomic.Memory{}
		err := m.Initialize(logging.NoLog{}, prefixdb.New([]byte{*cnt}, baseDB))
		if err != nil {
			t.Fatal(err)
		}

		sm := m.NewSharedMemory(vm.ctx.ChainID)
		peerSharedMemory := m.NewSharedMemory(peerChain)

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
					Addrs:     []ids.ShortID{sourceKey.PublicKey().Address()},
					Threshold: 1,
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
				sourceKey.PublicKey().Address().Bytes(),
			},
		}}}}); err != nil {
			t.Fatal(err)
		}

		return sm
	}

	tests := []test{
		{
			description:   "can't pay fee",
			sourceChainID: xChainID,
			sharedMemory:  fundedSharedMemory(xChainID, vm.TxFee-1),
			sourceKeys:    []*crypto.PrivateKeySECP256K1R{sourceKey},
			shouldErr:     true,
		},
		{
			description:   "can barely pay fee",
			sourceChainID: xChainID,
			sharedMemory:  fundedSharedMemory(xChainID, vm.TxFee),
			sourceKeys:    []*crypto.PrivateKeySECP256K1R{sourceKey},
			shouldErr:     false,
			shouldVerify:  true,
		},
		{
			description:   "attempting to import from C-chain",
			sourceChainID: cChainID,
			sharedMemory:  fundedSharedMemory(cChainID, vm.TxFee),
			sourceKeys:    []*crypto.PrivateKeySECP256K1R{sourceKey},
			timestamp:     vm.ApricotPhase5Time,
			shouldErr:     false,
			shouldVerify:  true,
		},
	}

	to := ids.GenerateTestShortID()
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			assert := assert.New(t)

			vm.ctx.SharedMemory = tt.sharedMemory
			vm.AtomicUTXOManager = avax.NewAtomicUTXOManager(tt.sharedMemory, Codec)
			tx, err := vm.newImportTx(tt.sourceChainID, to, tt.sourceKeys, ids.ShortEmpty)
			if tt.shouldErr {
				assert.Error(err)
				return
			}
			assert.NoError(err)

			unsignedTx := tx.UnsignedTx.(*UnsignedImportTx)
			assert.NotEmpty(unsignedTx.ImportedInputs)
			assert.Equal(len(tx.Creds), len(unsignedTx.Ins)+len(unsignedTx.ImportedInputs), "should have the same number of credentials as inputs")

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

			assert.Equal(vm.TxFee, totalIn-totalOut, "burned too much")

			// Get the preferred block (which we want to build off)
			preferred, err := vm.Preferred()
			assert.NoError(err)

			preferredDecision, ok := preferred.(decision)
			assert.True(ok)

			preferredState := preferredDecision.onAccept()
			fakedState := newVersionedState(
				preferredState,
				preferredState.CurrentStakerChainState(),
				preferredState.PendingStakerChainState(),
			)
			fakedState.SetTimestamp(tt.timestamp)

			err = tx.UnsignedTx.SemanticVerify(vm, fakedState, tx)
			if tt.shouldVerify {
				assert.NoError(err)
			} else {
				assert.Error(err)
			}
		})
	}
}
