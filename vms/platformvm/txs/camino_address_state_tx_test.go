// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/require"
)

func TestAddressStateTxSyntacticVerify(t *testing.T) {
	require := require.New(t)
	ctx := snow.DefaultContextTest()
	signers := [][]*crypto.PrivateKeySECP256K1R{preFundedKeys}

	var (
		stx            *Tx
		addressStateTx *AddressStateTx
		err            error
	)

	// Case : signed tx is nil
	require.ErrorIs(stx.SyntacticVerify(ctx), ErrNilSignedTx)

	// Case : unsigned tx is nil
	require.ErrorIs(addressStateTx.SyntacticVerify(ctx), ErrNilTx)

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
	lockedOut := &avax.TransferableOutput{
		Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 't'}},
		Out: &locked.Out{
			IDs: locked.IDsEmpty,
			TransferableOut: &secp256k1fx.TransferOutput{
				Amt: uint64(1234),
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
				},
			},
		},
	}

	stakedOut := &avax.TransferableOutput{
		Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 't'}},
		Out: &stakeable.LockOut{
			TransferableOut: &secp256k1fx.TransferOutput{
				Amt: uint64(1234),
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
				},
			},
		},
	}

	addressStateTx = &AddressStateTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    ctx.NetworkID,
			BlockchainID: ctx.ChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         []byte{1, 2, 3, 4, 5, 6, 7, 8},
		}},
		Address: preFundedKeys[0].PublicKey().Address(),
		State:   AddressStateRoleAdmin,
		Remove:  false,
	}

	outputs = append(outputs, lockedOut)
	addressStateTxLocked := &AddressStateTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    ctx.NetworkID,
			BlockchainID: ctx.ChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         []byte{1, 2, 3, 4, 5, 6, 7, 8},
		}},
		Address: preFundedKeys[0].PublicKey().Address(),
		State:   AddressStateRoleAdmin,
		Remove:  false,
	}

	outputs[1] = stakedOut
	addressStateTxStaked := &AddressStateTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    ctx.NetworkID,
			BlockchainID: ctx.ChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         []byte{1, 2, 3, 4, 5, 6, 7, 8},
		}},
		Address: preFundedKeys[0].PublicKey().Address(),
		State:   AddressStateRoleAdmin,
		Remove:  false,
	}

	// Case: valid tx
	stx, err = NewSigned(addressStateTx, Codec, signers)
	require.NoError(err)
	require.NoError(stx.SyntacticVerify(ctx))

	// Case: Empty address
	addressStateTx.SyntacticallyVerified = false
	addressStateTx.Address = ids.ShortID{}
	stx, err = NewSigned(addressStateTx, Codec, signers)
	require.NoError(err)
	err = stx.SyntacticVerify(ctx)
	require.Error(err, ErrEmptyAddress)
	addressStateTx.Address = preFundedKeys[0].PublicKey().Address()

	// Invalid mode
	addressStateTx.SyntacticallyVerified = false
	addressStateTx.State = 99
	stx, err = NewSigned(addressStateTx, Codec, signers)
	require.NoError(err)
	err = stx.SyntacticVerify(ctx)
	require.Error(err, ErrInvalidState)
	addressStateTx.State = AddressStateRoleAdmin

	// Locked out
	stx, err = NewSigned(addressStateTxLocked, Codec, signers)
	require.NoError(err)
	err = stx.SyntacticVerify(ctx)
	require.Error(err)

	// Staked out
	stx, err = NewSigned(addressStateTxStaked, Codec, signers)
	require.NoError(err)
	err = stx.SyntacticVerify(ctx)
	require.Error(err)
}
