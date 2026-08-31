// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"encoding/binary"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

func TestFxVerifyWarpCredential(t *testing.T) {
	require := require.New(t)

	vm := TestVM{Codec: linearcodec.NewDefault(), Log: logging.NoLog{}}
	helper := ids.ShortID{2}
	fx := Fx{WarpHelpers: set.Of(helper)}
	require.NoError(fx.Initialize(&vm))

	tx := &TestTx{UnsignedBytes: []byte{0, 1, 2, 3}}
	sender := ids.ShortID{1}
	other := ids.ShortID{0}
	// The emission height is ignored on the P-chain.
	height := binary.BigEndian.AppendUint64(nil, 42)
	good := slices.Concat(sender[:], height, tx.UnsignedBytes)

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

	// The trusted helper sends the message on the owner's behalf.
	require.NoError(fx.VerifyTransfer(tx, tin, newCred(helper[:], good), utxo))
	require.NoError(fx.VerifyPermission(tx, in, newCred(helper[:], good), owners))

	// The owner itself, a stranger, and a short address are not helpers.
	require.ErrorIs(fx.VerifyTransfer(tx, tin, newCred(sender[:], good), utxo), ErrWrongWarpSourceAddr)
	require.ErrorIs(fx.VerifyTransfer(tx, tin, newCred(other[:], good), utxo), ErrWrongWarpSourceAddr)
	require.ErrorIs(fx.VerifyTransfer(tx, tin, newCred(helper[:3], good), utxo), ErrWrongWarpSourceAddr)
	// The helper names someone other than the UTXO owner.
	require.ErrorIs(fx.VerifyTransfer(tx, tin, newCred(helper[:], slices.Concat(other[:], height, tx.UnsignedBytes)), utxo), ErrWrongWarpSourceAddr)
	require.ErrorIs(fx.VerifyTransfer(tx, tin, newCred(helper[:], slices.Concat(sender[:], height, []byte{9})), utxo), ErrWrongWarpPayload)
	// Owner without a height is too short.
	require.ErrorIs(fx.VerifyTransfer(tx, tin, newCred(helper[:], append(sender[:], tx.UnsignedBytes...)), utxo), ErrWrongWarpPayload)
	require.ErrorIs(fx.VerifyTransfer(tx, tin, newCred(helper[:], []byte("short")), utxo), ErrWrongWarpPayload)
	require.ErrorIs(fx.VerifyTransfer(tx, &TransferInput{Amt: 2, Input: *in}, newCred(helper[:], good), utxo), ErrMismatchedAmounts)
	require.ErrorIs(fx.VerifyTransfer(tx, &TransferInput{Amt: 1}, newCred(helper[:], good), utxo), ErrTooFewSigners)
}
