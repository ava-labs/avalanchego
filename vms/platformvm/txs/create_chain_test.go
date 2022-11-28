// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestUnsignedCreateChainTxVerify(t *testing.T) {
	ctx := snow.DefaultContextTest()
	testSubnet1ID := ids.GenerateTestID()
	testSubnet1ControlKeys := []*crypto.PrivateKeySECP256K1R{
		preFundedKeys[0],
		preFundedKeys[1],
	}

	type test struct {
		description string
		shouldErr   bool
		subnetID    ids.ID
		genesisData []byte
		vmID        ids.ID
		fxIDs       []ids.ID
		chainName   string
		keys        []*crypto.PrivateKeySECP256K1R
		setup       func(*CreateChainTx) *CreateChainTx
	}

	tests := []test{
		{
			description: "tx is nil",
			shouldErr:   true,
			subnetID:    testSubnet1ID,
			genesisData: nil,
			vmID:        constants.AVMID,
			fxIDs:       nil,
			chainName:   "yeet",
			keys:        []*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
			setup: func(*CreateChainTx) *CreateChainTx {
				return nil
			},
		},
		{
			description: "vm ID is empty",
			shouldErr:   true,
			subnetID:    testSubnet1ID,
			genesisData: nil,
			vmID:        constants.AVMID,
			fxIDs:       nil,
			chainName:   "yeet",
			keys:        []*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
			setup: func(tx *CreateChainTx) *CreateChainTx {
				tx.VMID = ids.ID{}
				return tx
			},
		},
		{
			description: "subnet ID is empty",
			shouldErr:   true,
			subnetID:    testSubnet1ID,
			genesisData: nil,
			vmID:        constants.AVMID,
			fxIDs:       nil,
			chainName:   "yeet",
			keys:        []*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
			setup: func(tx *CreateChainTx) *CreateChainTx {
				tx.SubnetID = ids.ID{}
				return tx
			},
		},
		{
			description: "subnet ID is platform chain's ID",
			shouldErr:   true,
			subnetID:    testSubnet1ID,
			genesisData: nil,
			vmID:        constants.AVMID,
			fxIDs:       nil,
			chainName:   "yeet",
			keys:        []*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
			setup: func(tx *CreateChainTx) *CreateChainTx {
				tx.SubnetID = ctx.ChainID
				return tx
			},
		},
		{
			description: "chain name is too long",
			shouldErr:   true,
			subnetID:    testSubnet1ID,
			genesisData: nil,
			vmID:        constants.AVMID,
			fxIDs:       nil,
			chainName:   "yeet",
			keys:        []*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
			setup: func(tx *CreateChainTx) *CreateChainTx {
				tx.ChainName = string(make([]byte, MaxNameLen+1))
				return tx
			},
		},
		{
			description: "chain name has invalid character",
			shouldErr:   true,
			subnetID:    testSubnet1ID,
			genesisData: nil,
			vmID:        constants.AVMID,
			fxIDs:       nil,
			chainName:   "yeet",
			keys:        []*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
			setup: func(tx *CreateChainTx) *CreateChainTx {
				tx.ChainName = "⌘"
				return tx
			},
		},
		{
			description: "genesis data is too long",
			shouldErr:   true,
			subnetID:    testSubnet1ID,
			genesisData: nil,
			vmID:        constants.AVMID,
			fxIDs:       nil,
			chainName:   "yeet",
			keys:        []*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
			setup: func(tx *CreateChainTx) *CreateChainTx {
				tx.GenesisData = make([]byte, MaxGenesisLen+1)
				return tx
			},
		},
	}

	for _, test := range tests {
		inputs := []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        ids.ID{'t', 'x', 'I', 'D'},
				OutputIndex: 2,
			},
			Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 't'}},
			In: &secp256k1fx.TransferInput{
				Amt:   uint64(5678),
				Input: secp256k1fx.Input{SigIndices: []uint32{0}},
			},
		}}
		outputs := []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 't'}},
			Out: &secp256k1fx.TransferOutput{
				Amt: uint64(1234),
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
				},
			},
		}}
		subnetAuth := &secp256k1fx.Input{
			SigIndices: []uint32{0, 1},
		}

		createChainTx := &CreateChainTx{
			BaseTx: BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    ctx.NetworkID,
				BlockchainID: ctx.ChainID,
				Ins:          inputs,
				Outs:         outputs,
			}},
			SubnetID:    test.subnetID,
			ChainName:   test.chainName,
			VMID:        test.vmID,
			FxIDs:       test.fxIDs,
			GenesisData: test.genesisData,
			SubnetAuth:  subnetAuth,
		}

		signers := [][]*crypto.PrivateKeySECP256K1R{preFundedKeys}
		stx, err := NewSigned(createChainTx, Codec, signers)
		if err != nil {
			t.Fatal(err)
		}

		createChainTx.SyntacticallyVerified = false
		stx.Unsigned = test.setup(createChainTx)
		if err := stx.SyntacticVerify(ctx); err != nil && !test.shouldErr {
			t.Fatalf("test '%s' shouldn't have erred but got: %s", test.description, err)
		} else if err == nil && test.shouldErr {
			t.Fatalf("test '%s' didn't error but should have", test.description)
		}
	}
}
