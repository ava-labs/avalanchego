// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ verify.Verifiable = (*WarpCredential)(nil)

	errNilWarpCredential  = errors.New("nil warp credential")
	errWrongWarpPayload   = errors.New("warp payload is not owner || height || unsigned tx bytes")
	errWarpOwnerMismatch  = errors.New("warp owner is not the UTXO owner")
	errUnknownWarpMessage = errors.New("warp credential is not a trusted message emitted by this chain")
)

// A warp credential's AddressedCall payload is
// owner (20) || emission height (8, big-endian) || unsigned tx bytes.
const (
	warpPayloadOwnerLen = common.AddressLength
	warpPayloadTxOffset = warpPayloadOwnerLen + 8
)

// WarpCredential authorizes an [Import] input on behalf of an EVM address:
// the owner called a trusted helper contract, which emitted this chain's own
// unsigned warp message naming msg.sender as the owner and carrying the exact
// tx bytes. No signature is needed because the message proves the owner sent
// an EVM tx asking for precisely this import.
type WarpCredential struct {
	Message []byte `serialize:"true" json:"message"`
}

func (cr *WarpCredential) Verify() error {
	if cr == nil {
		return errNilWarpCredential
	}
	return nil
}

// WarpAuth reports whether this chain emitted the warp message id from the
// helper at height, and whether that is acceptable for the caller: the block
// builder also requires height to be at or below the settled block so that
// every verifier has the message.
type WarpAuth func(id ids.ID, helper common.Address, height uint64) bool

// verifyWarpTransfer checks that cred authorizes in to spend out for the
// transaction with the given unsigned bytes.
func verifyWarpTransfer(unsigned []byte, in *secp256k1fx.TransferInput, cred *WarpCredential, out *secp256k1fx.TransferOutput, auth WarpAuth) error {
	if err := verify.All(in, cred, out); err != nil {
		return err
	}
	if out.Amt != in.Amt {
		return fmt.Errorf("%w: %d != %d", secp256k1fx.ErrMismatchedAmounts, out.Amt, in.Amt)
	}
	if out.Threshold != 1 || len(out.Addrs) != 1 || len(in.SigIndices) != 1 || in.SigIndices[0] != 0 {
		return fmt.Errorf("%w: warp credentials spend single-owner outputs only", errWarpOwnerMismatch)
	}
	msg, err := warp.ParseUnsignedMessage(cred.Message)
	if err != nil {
		return fmt.Errorf("%w: %w", errUnknownWarpMessage, err)
	}
	call, err := payload.ParseAddressedCall(msg.Payload)
	if err != nil {
		return fmt.Errorf("%w: %w", errWrongWarpPayload, err)
	}
	if len(call.Payload) < warpPayloadTxOffset || !bytes.Equal(call.Payload[warpPayloadTxOffset:], unsigned) {
		return errWrongWarpPayload
	}
	if owner := ids.ShortID(call.Payload[:warpPayloadOwnerLen]); owner != out.Addrs[0] {
		return fmt.Errorf("%w: %s != %s", errWarpOwnerMismatch, owner, out.Addrs[0])
	}
	height := binary.BigEndian.Uint64(call.Payload[warpPayloadOwnerLen:warpPayloadTxOffset])
	if auth == nil || !auth(msg.ID(), common.BytesToAddress(call.SourceAddress), height) {
		return fmt.Errorf("%w: %s", errUnknownWarpMessage, msg.ID())
	}
	return nil
}
