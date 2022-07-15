// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/validator"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestAddSubnetValidatorTxSyntacticVerify(t *testing.T) {
	assert := assert.New(t)
	clk := mockable.Clock{}
	ctx := snow.DefaultContextTest()
	signers := [][]*crypto.PrivateKeySECP256K1R{preFundedKeys}

	var (
		stx                  *Tx
		addSubnetValidatorTx *AddSubnetValidatorTx
		err                  error
	)

	// Case : signed tx is nil
	err = stx.SyntacticVerify(ctx)
	assert.True(errors.Is(err, errNilSignedTx))

	// Case : unsigned tx is nil
	err = addSubnetValidatorTx.SyntacticVerify(ctx)
	assert.True(errors.Is(err, ErrNilTx))

	validatorWeight := uint64(2022)
	subnetID := ids.ID{'s', 'u', 'b', 'n', 'e', 't', 'I', 'D'}
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
	addSubnetValidatorTx = &AddSubnetValidatorTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    ctx.NetworkID,
			BlockchainID: ctx.ChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         []byte{1, 2, 3, 4, 5, 6, 7, 8},
		}},
		Validator: validator.SubnetValidator{
			Validator: validator.Validator{
				NodeID: ctx.NodeID,
				Start:  uint64(clk.Time().Unix()),
				End:    uint64(clk.Time().Add(time.Hour).Unix()),
				Wght:   validatorWeight,
			},
			Subnet: subnetID,
		},
		SubnetAuth: subnetAuth,
	}

	// Case: valid tx
	stx, err = NewSigned(addSubnetValidatorTx, Codec, signers)
	assert.NoError(err)
	assert.NoError(stx.SyntacticVerify(ctx))

	// Case: Wrong network ID
	addSubnetValidatorTx.SyntacticallyVerified = false
	addSubnetValidatorTx.NetworkID++
	stx, err = NewSigned(addSubnetValidatorTx, Codec, signers)
	assert.NoError(err)
	err = stx.SyntacticVerify(ctx)
	assert.Error(err)
	addSubnetValidatorTx.NetworkID--

	// Case: Missing Subnet ID
	addSubnetValidatorTx.SyntacticallyVerified = false
	addSubnetValidatorTx.Validator.Subnet = ids.Empty
	stx, err = NewSigned(addSubnetValidatorTx, Codec, signers)
	assert.NoError(err)
	err = stx.SyntacticVerify(ctx)
	assert.Error(err)
	addSubnetValidatorTx.Validator.Subnet = subnetID

	// Case: No weight
	addSubnetValidatorTx.SyntacticallyVerified = false
	addSubnetValidatorTx.Validator.Wght = 0
	stx, err = NewSigned(addSubnetValidatorTx, Codec, signers)
	assert.NoError(err)
	err = stx.SyntacticVerify(ctx)
	assert.Error(err)
	addSubnetValidatorTx.Validator.Wght = validatorWeight

	// Case: Subnet auth indices not unique
	addSubnetValidatorTx.SyntacticallyVerified = false
	input := addSubnetValidatorTx.SubnetAuth.(*secp256k1fx.Input)
	input.SigIndices[0] = input.SigIndices[1]
	stx, err = NewSigned(addSubnetValidatorTx, Codec, signers)
	assert.NoError(err)
	err = stx.SyntacticVerify(ctx)
	assert.Error(err)
}

func TestAddSubnetValidatorMarshal(t *testing.T) {
	assert := assert.New(t)
	clk := mockable.Clock{}
	ctx := snow.DefaultContextTest()
	signers := [][]*crypto.PrivateKeySECP256K1R{preFundedKeys}

	var (
		stx                  *Tx
		addSubnetValidatorTx *AddSubnetValidatorTx
		err                  error
	)

	// create a valid tx
	validatorWeight := uint64(2022)
	subnetID := ids.ID{'s', 'u', 'b', 'n', 'e', 't', 'I', 'D'}
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
	addSubnetValidatorTx = &AddSubnetValidatorTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    ctx.NetworkID,
			BlockchainID: ctx.ChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         []byte{1, 2, 3, 4, 5, 6, 7, 8},
		}},
		Validator: validator.SubnetValidator{
			Validator: validator.Validator{
				NodeID: ctx.NodeID,
				Start:  uint64(clk.Time().Unix()),
				End:    uint64(clk.Time().Add(time.Hour).Unix()),
				Wght:   validatorWeight,
			},
			Subnet: subnetID,
		},
		SubnetAuth: subnetAuth,
	}

	// Case: valid tx
	stx, err = NewSigned(addSubnetValidatorTx, Codec, signers)
	assert.NoError(err)
	assert.NoError(stx.SyntacticVerify(ctx))

	txBytes, err := Codec.Marshal(Version, stx)
	assert.NoError(err)

	parsedTx, err := Parse(Codec, txBytes)
	assert.NoError(err)

	assert.NoError(parsedTx.SyntacticVerify(ctx))
	assert.Equal(stx, parsedTx)
}
