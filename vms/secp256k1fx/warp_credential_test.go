// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

func TestFxVerifyWarpCredential(t *testing.T) {
	require := require.New(t)

	vm := TestVM{Codec: linearcodec.NewDefault(), Log: logging.NoLog{}}
	fx := Fx{}
	require.NoError(fx.Initialize(&vm))

	tx := &TestTx{UnsignedBytes: []byte{0, 1, 2, 3}}
	sender := ids.ShortID{1}
	helper := WarpHelperAddresses.List()[0]
	other := ids.ShortID{0}
	good := append(sender[:], tx.UnsignedBytes...)

	newCred := func(sourceAddr []byte, msgPayload []byte) *WarpCredential {
		call, err := payload.NewAddressedCall(sourceAddr, msgPayload)
		require.NoError(err)
		unsigned, err := warp.NewUnsignedMessage(1, ids.GenerateTestID(), call.Bytes())
		require.NoError(err)
		msg, err := warp.NewMessage(unsigned, &warp.BitSetSignature{})
		require.NoError(err)
		return &WarpCredential{Message: msg.Bytes()}
	}

	owners := &OutputOwners{Threshold: 1, Addrs: []ids.ShortID{other, sender}}
	in := &Input{SigIndices: []uint32{1}}
	utxo := &TransferOutput{Amt: 1, OutputOwners: *owners}
	tin := &TransferInput{Amt: 1, Input: *in}

	// Owner sends the message itself.
	require.NoError(fx.VerifyTransfer(tx, tin, newCred(sender[:], good), utxo))
	require.NoError(fx.VerifyPermission(tx, in, newCred(sender[:], good), owners))
	// The trusted helper sends it on the owner's behalf.
	require.NoError(fx.VerifyTransfer(tx, tin, newCred(helper[:], good), utxo))

	require.ErrorIs(fx.VerifyTransfer(tx, tin, newCred(other[:], good), utxo), ErrWrongWarpSourceAddr)
	require.ErrorIs(fx.VerifyTransfer(tx, tin, newCred(sender[:], append(other[:], tx.UnsignedBytes...)), utxo), ErrWrongWarpSourceAddr)
	require.ErrorIs(fx.VerifyTransfer(tx, tin, newCred(helper[:], append(other[:], tx.UnsignedBytes...)), utxo), ErrWrongWarpSourceAddr)
	require.ErrorIs(fx.VerifyTransfer(tx, tin, newCred(sender[:], append(sender[:], 9)), utxo), ErrWrongWarpPayload)
	require.ErrorIs(fx.VerifyTransfer(tx, tin, newCred(sender[:], []byte("short")), utxo), ErrWrongWarpPayload)
	require.ErrorIs(fx.VerifyTransfer(tx, tin, newCred(sender[:3], good), utxo), ErrWrongWarpSourceAddrL)
	require.ErrorIs(fx.VerifyTransfer(tx, &TransferInput{Amt: 2, Input: *in}, newCred(sender[:], good), utxo), ErrMismatchedAmounts)
	require.ErrorIs(fx.VerifyTransfer(tx, &TransferInput{Amt: 1}, newCred(sender[:], good), utxo), ErrTooFewSigners)
}
