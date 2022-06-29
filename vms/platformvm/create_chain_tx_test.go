// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestUnsignedCreateChainTxVerify(t *testing.T) {
	vm, _, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	type test struct {
		description string
		shouldErr   bool
		subnetID    ids.ID
		genesisData []byte
		vmID        ids.ID
		fxIDs       []ids.ID
		chainName   string
		keys        []*crypto.PrivateKeySECP256K1R
		setup       func(*txs.CreateChainTx) *txs.CreateChainTx
	}

	tests := []test{
		{
			description: "tx is nil",
			shouldErr:   true,
			subnetID:    testSubnet1.ID(),
			genesisData: nil,
			vmID:        constants.AVMID,
			fxIDs:       nil,
			chainName:   "yeet",
			keys:        []*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
			setup:       func(*txs.CreateChainTx) *txs.CreateChainTx { return nil },
		},
		{
			description: "vm ID is empty",
			shouldErr:   true,
			subnetID:    testSubnet1.ID(),
			genesisData: nil,
			vmID:        constants.AVMID,
			fxIDs:       nil,
			chainName:   "yeet",
			keys:        []*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
			setup:       func(tx *txs.CreateChainTx) *txs.CreateChainTx { tx.VMID = ids.ID{}; return tx },
		},
		{
			description: "subnet ID is empty",
			shouldErr:   true,
			subnetID:    testSubnet1.ID(),
			genesisData: nil,
			vmID:        constants.AVMID,
			fxIDs:       nil,
			chainName:   "yeet",
			keys:        []*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
			setup:       func(tx *txs.CreateChainTx) *txs.CreateChainTx { tx.SubnetID = ids.ID{}; return tx },
		},
		{
			description: "subnet ID is platform chain's ID",
			shouldErr:   true,
			subnetID:    testSubnet1.ID(),
			genesisData: nil,
			vmID:        constants.AVMID,
			fxIDs:       nil,
			chainName:   "yeet",
			keys:        []*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
			setup: func(tx *txs.CreateChainTx) *txs.CreateChainTx {
				tx.SubnetID = vm.ctx.ChainID
				return tx
			},
		},
		{
			description: "chain name is too long",
			shouldErr:   true,
			subnetID:    testSubnet1.ID(),
			genesisData: nil,
			vmID:        constants.AVMID,
			fxIDs:       nil,
			chainName:   "yeet",
			keys:        []*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
			setup: func(tx *txs.CreateChainTx) *txs.CreateChainTx {
				tx.ChainName = string(make([]byte, txs.MaxNameLen+1))
				return tx
			},
		},
		{
			description: "chain name has invalid character",
			shouldErr:   true,
			subnetID:    testSubnet1.ID(),
			genesisData: nil,
			vmID:        constants.AVMID,
			fxIDs:       nil,
			chainName:   "yeet",
			keys:        []*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
			setup: func(tx *txs.CreateChainTx) *txs.CreateChainTx {
				tx.ChainName = "⌘"
				return tx
			},
		},
		{
			description: "genesis data is too long",
			shouldErr:   true,
			subnetID:    testSubnet1.ID(),
			genesisData: nil,
			vmID:        constants.AVMID,
			fxIDs:       nil,
			chainName:   "yeet",
			keys:        []*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
			setup: func(tx *txs.CreateChainTx) *txs.CreateChainTx {
				tx.GenesisData = make([]byte, txs.MaxGenesisLen+1)
				return tx
			},
		},
	}

	for _, test := range tests {
		tx, err := vm.txBuilder.NewCreateChainTx(
			test.subnetID,
			test.genesisData,
			test.vmID,
			test.fxIDs,
			test.chainName,
			test.keys,
			ids.ShortEmpty, // change addr
		)
		if err != nil {
			t.Fatal(err)
		}

		createChainTx := tx.Unsigned.(*txs.CreateChainTx)
		createChainTx.SyntacticallyVerified = false
		tx.Unsigned = test.setup(createChainTx)
		if err := tx.SyntacticVerify(vm.ctx); err != nil && !test.shouldErr {
			t.Fatalf("test '%s' shouldn't have erred but got: %s", test.description, err)
		} else if err == nil && test.shouldErr {
			t.Fatalf("test '%s' didn't error but should have", test.description)
		}
	}
}

// Ensure Execute fails when there are not enough control sigs
func TestCreateChainTxInsufficientControlSigs(t *testing.T) {
	vm, _, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	tx, err := vm.txBuilder.NewCreateChainTx(
		testSubnet1.ID(),
		nil,
		constants.AVMID,
		nil,
		"chain name",
		[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}

	// Remove a signature
	tx.Creds[0].(*secp256k1fx.Credential).Sigs = tx.Creds[0].(*secp256k1fx.Credential).Sigs[1:]

	executor := standardTxExecutor{
		vm: vm,
		state: state.NewDiff(
			vm.internalState,
			vm.internalState.CurrentStakers(),
			vm.internalState.PendingStakers(),
		),
		tx: tx,
	}
	err = tx.Unsigned.Visit(&executor)
	if err == nil {
		t.Fatal("should have erred because a sig is missing")
	}
}

// Ensure Execute fails when an incorrect control signature is given
func TestCreateChainTxWrongControlSig(t *testing.T) {
	vm, _, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	tx, err := vm.txBuilder.NewCreateChainTx( // create a tx
		testSubnet1.ID(),
		nil,
		constants.AVMID,
		nil,
		"chain name",
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}

	// Generate new, random key to sign tx with
	factory := crypto.FactorySECP256K1R{}
	key, err := factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}

	// Replace a valid signature with one from another key
	sig, err := key.SignHash(hashing.ComputeHash256(tx.Unsigned.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	copy(tx.Creds[0].(*secp256k1fx.Credential).Sigs[0][:], sig)

	executor := standardTxExecutor{
		vm: vm,
		state: state.NewDiff(
			vm.internalState,
			vm.internalState.CurrentStakers(),
			vm.internalState.PendingStakers(),
		),
		tx: tx,
	}
	err = tx.Unsigned.Visit(&executor)
	if err == nil {
		t.Fatal("should have failed verification because a sig is invalid")
	}
}

// Ensure Execute fails when the Subnet the blockchain specifies as
// its validator set doesn't exist
func TestCreateChainTxNoSuchSubnet(t *testing.T) {
	vm, _, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	tx, err := vm.txBuilder.NewCreateChainTx(
		testSubnet1.ID(),
		nil,
		constants.AVMID,
		nil,
		"chain name",
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}

	tx.Unsigned.(*txs.CreateChainTx).SubnetID = ids.GenerateTestID()

	executor := standardTxExecutor{
		vm: vm,
		state: state.NewDiff(
			vm.internalState,
			vm.internalState.CurrentStakers(),
			vm.internalState.PendingStakers(),
		),
		tx: tx,
	}
	err = tx.Unsigned.Visit(&executor)
	if err == nil {
		t.Fatal("should have failed because subnet doesn't exist")
	}
}

// Ensure valid tx passes semanticVerify
func TestCreateChainTxValid(t *testing.T) {
	vm, _, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	// create a valid tx
	tx, err := vm.txBuilder.NewCreateChainTx(
		testSubnet1.ID(),
		nil,
		constants.AVMID,
		nil,
		"chain name",
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}

	executor := standardTxExecutor{
		vm: vm,
		state: state.NewDiff(
			vm.internalState,
			vm.internalState.CurrentStakers(),
			vm.internalState.PendingStakers(),
		),
		tx: tx,
	}
	err = tx.Unsigned.Visit(&executor)
	if err != nil {
		t.Fatalf("expected tx to pass verification but got error: %v", err)
	}
}

func TestCreateChainTxAP3FeeChange(t *testing.T) {
	ap3Time := defaultGenesisTime.Add(time.Hour)
	tests := []struct {
		name         string
		time         time.Time
		fee          uint64
		expectsError bool
	}{
		{
			name:         "pre-fork - correctly priced",
			time:         defaultGenesisTime,
			fee:          0,
			expectsError: false,
		},
		{
			name:         "post-fork - incorrectly priced",
			time:         ap3Time,
			fee:          100*defaultTxFee - 1*units.NanoAvax,
			expectsError: true,
		},
		{
			name:         "post-fork - correctly priced",
			time:         ap3Time,
			fee:          100 * defaultTxFee,
			expectsError: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert := assert.New(t)

			vm, _, _, _ := defaultVM()
			vm.ApricotPhase3Time = ap3Time

			vm.ctx.Lock.Lock()
			defer func() {
				err := vm.Shutdown()
				assert.NoError(err)
				vm.ctx.Lock.Unlock()
			}()

			ins, outs, _, signers, err := vm.utxoHandler.Spend(keys, 0, test.fee, ids.ShortEmpty)
			assert.NoError(err)

			subnetAuth, subnetSigners, err := vm.utxoHandler.Authorize(vm.internalState, testSubnet1.ID(), keys)
			assert.NoError(err)

			signers = append(signers, subnetSigners)

			// Create the tx

			utx := &txs.CreateChainTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    vm.ctx.NetworkID,
					BlockchainID: vm.ctx.ChainID,
					Ins:          ins,
					Outs:         outs,
				}},
				SubnetID:   testSubnet1.ID(),
				VMID:       constants.AVMID,
				SubnetAuth: subnetAuth,
			}
			tx := &txs.Tx{Unsigned: utx}
			err = tx.Sign(Codec, signers)
			assert.NoError(err)

			state := state.NewDiff(
				vm.internalState,
				vm.internalState.CurrentStakers(),
				vm.internalState.PendingStakers(),
			)
			state.SetTimestamp(test.time)

			executor := standardTxExecutor{
				vm:    vm,
				state: state,
				tx:    tx,
			}
			err = tx.Unsigned.Visit(&executor)
			assert.Equal(test.expectsError, err != nil)
		})
	}
}
