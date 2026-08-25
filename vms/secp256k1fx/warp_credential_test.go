// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
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
	txHash := hashing.ComputeHash256(tx.UnsignedBytes)
	sender := ids.ShortID{1}
	other := ids.ShortID{0}

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

	require.NoError(fx.VerifyTransfer(tx, tin, newCred(sender[:], txHash), utxo))
	require.NoError(fx.VerifyPermission(tx, in, newCred(sender[:], txHash), owners))

	require.ErrorIs(fx.VerifyTransfer(tx, tin, newCred(other[:], txHash), utxo), ErrWrongWarpSourceAddr)
	require.ErrorIs(fx.VerifyTransfer(tx, tin, newCred(sender[:], []byte("nope")), utxo), ErrWrongWarpPayload)
	require.ErrorIs(fx.VerifyTransfer(tx, tin, newCred(sender[:3], txHash), utxo), ErrWrongWarpSourceAddrL)
	require.ErrorIs(fx.VerifyTransfer(tx, &TransferInput{Amt: 2, Input: *in}, newCred(sender[:], txHash), utxo), ErrMismatchedAmounts)
	require.ErrorIs(fx.VerifyTransfer(tx, &TransferInput{Amt: 1}, newCred(sender[:], txHash), utxo), ErrTooFewSigners)
}
