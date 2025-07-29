// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// TODO use table tests here
func TestAddSubnetValidatorTxSyntacticVerify(t *testing.T) {
	require := require.New(t)
	clk := mockable.Clock{}
	ctx := snowtest.Context(t, snowtest.PChainID)
	signers := [][]*secp256k1.PrivateKey{preFundedKeys}

	var (
		stx                  *Tx
		addSubnetValidatorTx *AddSubnetValidatorTx
		err                  error
	)

	// Case : signed tx is nil
	err = stx.SyntacticVerify(ctx)
	require.ErrorIs(err, ErrNilSignedTx)

	// Case : unsigned tx is nil
	err = addSubnetValidatorTx.SyntacticVerify(ctx)
	require.ErrorIs(err, ErrNilTx)

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
				Addrs:     []ids.ShortID{preFundedKeys[0].Address()},
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
		SubnetValidator: SubnetValidator{
			Validator: Validator{
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
	require.NoError(err)
	require.NoError(stx.SyntacticVerify(ctx))

	// Case: Wrong network ID
	addSubnetValidatorTx.SyntacticallyVerified = false
	addSubnetValidatorTx.NetworkID++
	stx, err = NewSigned(addSubnetValidatorTx, Codec, signers)
	require.NoError(err)
	err = stx.SyntacticVerify(ctx)
	require.ErrorIs(err, avax.ErrWrongNetworkID)
	addSubnetValidatorTx.NetworkID--

	// Case: Specifies primary network SubnetID
	addSubnetValidatorTx.SyntacticallyVerified = false
	addSubnetValidatorTx.Subnet = ids.Empty
	stx, err = NewSigned(addSubnetValidatorTx, Codec, signers)
	require.NoError(err)
	err = stx.SyntacticVerify(ctx)
	require.ErrorIs(err, errAddPrimaryNetworkValidator)
	addSubnetValidatorTx.Subnet = subnetID

	// Case: No weight
	addSubnetValidatorTx.SyntacticallyVerified = false
	addSubnetValidatorTx.Wght = 0
	stx, err = NewSigned(addSubnetValidatorTx, Codec, signers)
	require.NoError(err)
	err = stx.SyntacticVerify(ctx)
	require.ErrorIs(err, ErrWeightTooSmall)
	addSubnetValidatorTx.Wght = validatorWeight

	// Case: Subnet auth indices not unique
	addSubnetValidatorTx.SyntacticallyVerified = false
	input := addSubnetValidatorTx.SubnetAuth.(*secp256k1fx.Input)
	oldInput := *input
	input.SigIndices[0] = input.SigIndices[1]
	stx, err = NewSigned(addSubnetValidatorTx, Codec, signers)
	require.NoError(err)
	err = stx.SyntacticVerify(ctx)
	require.ErrorIs(err, secp256k1fx.ErrInputIndicesNotSortedUnique)
	*input = oldInput

	// Case: adding to Primary Network
	addSubnetValidatorTx.SyntacticallyVerified = false
	addSubnetValidatorTx.Subnet = constants.PrimaryNetworkID
	stx, err = NewSigned(addSubnetValidatorTx, Codec, signers)
	require.NoError(err)
	err = stx.SyntacticVerify(ctx)
	require.ErrorIs(err, errAddPrimaryNetworkValidator)
}

func TestAddSubnetValidatorMarshal(t *testing.T) {
	require := require.New(t)
	clk := mockable.Clock{}
	ctx := snowtest.Context(t, snowtest.PChainID)
	signers := [][]*secp256k1.PrivateKey{preFundedKeys}

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
				Addrs:     []ids.ShortID{preFundedKeys[0].Address()},
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
		SubnetValidator: SubnetValidator{
			Validator: Validator{
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
	require.NoError(err)
	require.NoError(stx.SyntacticVerify(ctx))

	txBytes, err := Codec.Marshal(CodecVersion, stx)
	require.NoError(err)

	parsedTx, err := Parse(Codec, txBytes)
	require.NoError(err)

	require.NoError(parsedTx.SyntacticVerify(ctx))
	require.Equal(stx, parsedTx)
}

func TestAddSubnetValidatorTxNotValidatorTx(t *testing.T) {
	txIntf := any((*AddSubnetValidatorTx)(nil))
	_, ok := txIntf.(ValidatorTx)
	require.False(t, ok)
}

func TestAddSubnetValidatorTxNotDelegatorTx(t *testing.T) {
	txIntf := any((*AddSubnetValidatorTx)(nil))
	_, ok := txIntf.(DelegatorTx)
	require.False(t, ok)
}

func TestAddSubnetValidatorTxNotPermissionlessStaker(t *testing.T) {
	txIntf := any((*AddSubnetValidatorTx)(nil))
	_, ok := txIntf.(PermissionlessStaker)
	require.False(t, ok)
}
